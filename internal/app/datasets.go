package app

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/souProjet/trade-republic-exporter/internal/export"
	"github.com/souProjet/trade-republic-exporter/internal/i18n"
	"github.com/souProjet/trade-republic-exporter/internal/report"
	"github.com/souProjet/trade-republic-exporter/internal/trws"
)

type fetchFunc func(context.Context, *trws.Client, report.Task) ([]map[string]any, error)

// step is one dataset of the plan with the function that fetches it.
type step struct {
	export.Dataset
	fetch fetchFunc
}

// plan returns the selected datasets in catalog order. Without the account
// list, holdings cannot be scoped to an account and are left out.
func plan(opts Options, accounts []trws.Account) []step {
	fetchers := fetchers(opts, accounts)
	var steps []step
	for _, d := range export.Datasets {
		if len(opts.Datasets) > 0 && !slices.Contains(opts.Datasets, d.Name) {
			continue
		}
		if d.Name == "positions" && len(accounts) == 0 {
			continue
		}
		steps = append(steps, step{Dataset: d, fetch: fetchers[d.Name]})
	}
	return steps
}

// collect runs every planned dataset over the shared connection. A dataset
// that fails is reported and skipped; the others still get written.
func collect(ctx context.Context, client *trws.Client, opts Options, accounts []trws.Account, r report.Reporter) ([]report.File, int) {
	steps := plan(opts, accounts)
	labels := make([]string, len(steps))
	for i, s := range steps {
		labels[i] = i18n.T(s.Label)
	}
	r.Plan(labels)

	var (
		files    []report.File
		failures int
	)
	for _, s := range steps {
		if ctx.Err() != nil {
			break
		}
		task := r.Task(i18n.T(s.Label))

		rows, fetchErr := s.fetch(ctx, client, task)
		if fetchErr != nil && len(rows) == 0 {
			task.Fail(fetchErr)
			failures++
			continue
		}
		if rows == nil {
			rows = []map[string]any{}
		}

		path, err := export.Save(opts.OutputDir, opts.Format, opts.Dialect, s.Name, rows)
		if err != nil {
			task.Fail(err)
			failures++
			continue
		}
		task.Done(i18n.T("%d rows", len(rows)))

		// Warned only once the task has settled, so the interface never shows
		// the warning on a row that is still running.
		if fetchErr != nil {
			r.Warn(i18n.T("%s: partial data (%s)", i18n.T(s.Label), compact(fetchErr)))
		}
		files = append(files, report.File{Name: i18n.T(s.Label), Path: path, Rows: len(rows)})
	}
	return files, failures
}

func fetchers(opts Options, accounts []trws.Account) map[string]fetchFunc {
	return map[string]fetchFunc{
		"accounts": func(ctx context.Context, c *trws.Client, _ report.Task) ([]map[string]any, error) {
			_, rows, err := c.Accounts(ctx)
			return rows, err
		},
		"positions": func(ctx context.Context, c *trws.Client, task report.Task) ([]map[string]any, error) {
			return perAccount(ctx, accounts, task, func(a trws.Account) ([]map[string]any, error) {
				if a.SecuritiesAccountNumber == "" {
					return nil, nil
				}
				return c.Positions(ctx, a.SecuritiesAccountNumber)
			})
		},
		"cash": func(ctx context.Context, c *trws.Client, task report.Task) ([]map[string]any, error) {
			if len(accounts) == 0 {
				return c.Cash(ctx, "")
			}
			return perAccount(ctx, accounts, task, func(a trws.Account) ([]map[string]any, error) {
				return c.Cash(ctx, a.CashAccountNumber)
			})
		},
		"available_cash": simple("availableCash"),
		"transactions": func(ctx context.Context, c *trws.Client, task report.Task) ([]map[string]any, error) {
			rows, err := c.Timeline(ctx, "timelineTransactions", func(pages, items int) {
				task.Update(i18n.T("page %d · %d transactions", pages, items))
			})
			if err != nil || !opts.Details {
				return rows, err
			}
			return enrich(ctx, c, rows, task)
		},
		"activity_log": func(ctx context.Context, c *trws.Client, task report.Task) ([]map[string]any, error) {
			return c.Timeline(ctx, "timelineActivityLog", func(pages, items int) {
				task.Update(i18n.T("page %d · %d events", pages, items))
			})
		},
		"savings_plans": simple("savingsPlans"),
		"orders":        simple("orders"),
	}
}

func simple(sub string) fetchFunc {
	return func(ctx context.Context, c *trws.Client, _ report.Task) ([]map[string]any, error) {
		return c.Fetch(ctx, sub)
	}
}

// perAccount runs fetch for every account and tags each row with the account
// it belongs to, so one file covers the securities account, the PEA and any
// other product.
func perAccount(ctx context.Context, accounts []trws.Account, task report.Task, fetch func(trws.Account) ([]map[string]any, error)) ([]map[string]any, error) {
	var (
		all     []map[string]any
		lastErr error
	)
	for i, account := range accounts {
		if err := ctx.Err(); err != nil {
			return all, err
		}
		task.Update(i18n.T(account.Label()))
		task.Progress(i, len(accounts))

		rows, err := fetch(account)
		if err != nil {
			lastErr = fmt.Errorf("%s: %w", account.Label(), err)
			continue
		}
		all = append(all, tag(rows, account)...)
	}
	task.Progress(len(accounts), len(accounts))
	return all, lastErr
}

// tag adds the owning account's identifiers to each row.
func tag(rows []map[string]any, account trws.Account) []map[string]any {
	for _, row := range rows {
		row["account.type"] = account.Label()
		row["account.productType"] = account.ProductType
		if account.SecuritiesAccountNumber != "" {
			row["account.securitiesAccountNumber"] = account.SecuritiesAccountNumber
		}
		if account.CashAccountNumber != "" {
			row["account.cashAccountNumber"] = account.CashAccountNumber
		}
	}
	return rows
}

// maxDetailFailures is how many detail requests may fail before enrichment
// gives up. A changed payload shape fails on every row, and there is no point
// sending hundreds of doomed requests.
const maxDetailFailures = 5

// enrich adds the detail view of each transaction, one extra request per row,
// which is why it is opt-in. It returns the rows it managed to enrich along
// with the first error, so a broken detail endpoint never costs the timeline.
func enrich(ctx context.Context, c *trws.Client, rows []map[string]any, task report.Task) ([]map[string]any, error) {
	var (
		firstErr error
		failures int
		enriched int
	)
	task.Update(i18n.T("fetching details"))
	for i, row := range rows {
		if err := ctx.Err(); err != nil {
			return rows, err
		}
		task.Progress(i, len(rows))

		id, _ := row["id"].(string)
		if id == "" {
			continue
		}
		details, err := c.TransactionDetails(ctx, id)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			failures++
			if enriched == 0 && failures >= maxDetailFailures {
				return rows, fmt.Errorf("%s: %w", i18n.T("gave up enriching after %d failures", failures), err)
			}
			continue
		}
		maps.Copy(row, details)
		enriched++
	}
	task.Progress(len(rows), len(rows))
	return rows, firstErr
}
