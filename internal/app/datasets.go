package app

import (
	"context"
	"fmt"

	"github.com/souProjet/trade-republic-exporter/internal/config"
	"github.com/souProjet/trade-republic-exporter/internal/export"
	"github.com/souProjet/trade-republic-exporter/internal/trws"
	"github.com/souProjet/trade-republic-exporter/internal/ui"
)

// dataset is one exported file: a label, the base file name and a fetch step.
type dataset struct {
	name  string
	file  string
	fetch func(context.Context, *trws.Client, *ui.Task) ([]map[string]any, error)
}

// result is a dataset that made it to disk.
type result struct {
	name string
	path string
	rows int
}

// collect runs every dataset in order over the shared connection. A dataset
// that fails is reported and skipped; the others still get written.
func collect(ctx context.Context, client *trws.Client, cfg *config.Config, accounts []trws.Account, out *ui.UI) ([]result, int) {
	out.Section("Export")

	var (
		results  []result
		failures int
	)
	for _, ds := range datasets(cfg, accounts) {
		task := out.Task(ds.name)

		rows, err := ds.fetch(ctx, client, task)
		if err != nil && len(rows) == 0 {
			task.Fail(err)
			failures++
			continue
		}
		if err != nil {
			out.Warn("%s: partial data (%s)", ds.name, compact(err))
		}
		if rows == nil {
			rows = []map[string]any{}
		}

		path, err := export.Save(cfg.OutputDir, cfg.Format, ds.file, rows)
		if err != nil {
			task.Fail(err)
			failures++
			continue
		}
		task.Done("%d rows", len(rows))
		results = append(results, result{name: ds.name, path: path, rows: len(rows)})
	}
	return results, failures
}

// datasets is the export plan: per-account holdings and balances, then the
// customer-wide timelines and plans.
func datasets(cfg *config.Config, accounts []trws.Account) []dataset {
	plan := []dataset{
		{
			name: "Accounts",
			file: "accounts",
			fetch: func(ctx context.Context, c *trws.Client, _ *ui.Task) ([]map[string]any, error) {
				_, rows, err := c.Accounts(ctx)
				return rows, err
			},
		},
		{
			name: "Positions",
			file: "positions",
			fetch: func(ctx context.Context, c *trws.Client, task *ui.Task) ([]map[string]any, error) {
				return perAccount(ctx, accounts, task, func(account trws.Account) ([]map[string]any, error) {
					if account.SecuritiesAccountNumber == "" {
						return nil, nil
					}
					return c.Positions(ctx, account.SecuritiesAccountNumber)
				})
			},
		},
		{
			name: "Cash balances",
			file: "cash",
			fetch: func(ctx context.Context, c *trws.Client, task *ui.Task) ([]map[string]any, error) {
				if len(accounts) == 0 {
					return c.Cash(ctx, "")
				}
				return perAccount(ctx, accounts, task, func(account trws.Account) ([]map[string]any, error) {
					return c.Cash(ctx, account.CashAccountNumber)
				})
			},
		},
		{
			name:  "Available cash",
			file:  "available_cash",
			fetch: simple("availableCash"),
		},
		{
			name: "Transactions",
			file: "transactions",
			fetch: func(ctx context.Context, c *trws.Client, task *ui.Task) ([]map[string]any, error) {
				rows, err := c.Timeline(ctx, "timelineTransactions", func(pages, items int) {
					task.Update("page %d, %d transactions", pages, items)
				})
				if err != nil || !cfg.ExtractDetails {
					return rows, err
				}
				return enrich(ctx, c, rows, task)
			},
		},
		{
			name: "Activity log",
			file: "activity_log",
			fetch: func(ctx context.Context, c *trws.Client, task *ui.Task) ([]map[string]any, error) {
				return c.Timeline(ctx, "timelineActivityLog", func(pages, items int) {
					task.Update("page %d, %d events", pages, items)
				})
			},
		},
		{name: "Savings plans", file: "savings_plans", fetch: simple("savingsPlans")},
		{name: "Open orders", file: "orders", fetch: simple("orders")},
	}

	if len(accounts) == 0 {
		// Without the account list, holdings cannot be scoped to an account.
		return append(plan[:1:1], plan[2:]...)
	}
	return plan
}

func simple(sub string) func(context.Context, *trws.Client, *ui.Task) ([]map[string]any, error) {
	return func(ctx context.Context, c *trws.Client, _ *ui.Task) ([]map[string]any, error) {
		return c.Fetch(ctx, sub)
	}
}

// perAccount runs fetch for every account and tags each row with the account
// it belongs to, so one file covers the securities account, the PEA and any
// other product.
func perAccount(ctx context.Context, accounts []trws.Account, task *ui.Task, fetch func(trws.Account) ([]map[string]any, error)) ([]map[string]any, error) {
	var (
		all     []map[string]any
		lastErr error
	)
	for i, account := range accounts {
		if err := ctx.Err(); err != nil {
			return all, err
		}
		task.Update("%s (%d/%d)", account.Label(), i+1, len(accounts))

		rows, err := fetch(account)
		if err != nil {
			lastErr = fmt.Errorf("%s: %w", account.Label(), err)
			continue
		}
		all = append(all, tag(rows, account)...)
	}
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

// enrich adds the detail section of each transaction, one extra request per
// row, which is why it is opt-in.
func enrich(ctx context.Context, c *trws.Client, rows []map[string]any, task *ui.Task) ([]map[string]any, error) {
	var lastErr error
	for i, row := range rows {
		if err := ctx.Err(); err != nil {
			return rows, err
		}
		id, _ := row["id"].(string)
		if id == "" {
			continue
		}
		task.Update("details %d/%d", i+1, len(rows))

		details, err := c.TransactionDetails(ctx, id)
		if err != nil {
			lastErr = err
			continue
		}
		for k, v := range details {
			row[k] = v
		}
	}
	return rows, lastErr
}
