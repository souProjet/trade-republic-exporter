package app

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"time"

	"github.com/souProjet/trade-republic-exporter/internal/export"
	"github.com/souProjet/trade-republic-exporter/internal/i18n"
	"github.com/souProjet/trade-republic-exporter/internal/report"
)

// Demo plays a complete export with sample data and no network access, to
// preview the interface. It writes no files and accepts any two-factor code.
func Demo(ctx context.Context, opts Options, r report.Reporter) error {
	started := time.Now()
	pause := func(d time.Duration) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(d):
			return nil
		}
	}
	run := func(label, detail string, d time.Duration) error {
		t := r.Task(label)
		if err := pause(d); err != nil {
			t.Fail(err)
			return err
		}
		t.Done(detail)
		return nil
	}

	r.Phase(i18n.T(PhaseAuthentication))
	if err := run(i18n.T("Device identity"), i18n.T("reused"), 300*time.Millisecond); err != nil {
		return err
	}
	waf := r.Task(i18n.T("AWS WAF token"))
	waf.Update(i18n.T("solving the challenge in headless Chrome"))
	if err := pause(1800 * time.Millisecond); err != nil {
		return err
	}
	waf.Done(i18n.T("solved"))
	if err := run(i18n.T("Login"), RedactPhone("+33612345678"), 500*time.Millisecond); err != nil {
		return err
	}
	if _, err := r.Ask(report.Prompt{
		Title:            i18n.T("Two-factor code"),
		Description:      i18n.T("Demo mode: type any code."),
		Placeholder:      i18n.T("code"),
		Deadline:         time.Now().Add(60 * time.Second),
		Alternative:      "sms",
		AlternativeLabel: i18n.T("send by SMS"),
	}); err != nil {
		return err
	}
	if err := run(i18n.T("Two-factor code"), i18n.T("session established"), 400*time.Millisecond); err != nil {
		return err
	}

	r.Phase(i18n.T(PhaseSession))
	if err := run(i18n.T("WebSocket"), i18n.T("connected"), 300*time.Millisecond); err != nil {
		return err
	}
	if err := run(i18n.T("Accounts"), i18n.T("%d found", 2), 400*time.Millisecond); err != nil {
		return err
	}
	r.Accounts([]report.Account{
		{Label: i18n.T("Securities account"), Securities: "0123456789", Cash: "DE00123456789", Currency: "EUR"},
		{Label: i18n.T("PEA"), Securities: "1234567890", Cash: "FR00987654321", Currency: "EUR"},
	})

	r.Phase(i18n.T(PhaseExport))
	sample := map[string]int{
		"accounts": 2, "positions": 37, "cash": 2, "available_cash": 2,
		"transactions": 565, "activity_log": 388, "savings_plans": 0, "orders": 1,
	}
	var steps []export.Dataset
	for _, d := range export.Datasets {
		if len(opts.Datasets) == 0 || slices.Contains(opts.Datasets, d.Name) {
			steps = append(steps, d)
		}
	}
	labels := make([]string, len(steps))
	for i, d := range steps {
		labels[i] = i18n.T(d.Label)
	}
	r.Plan(labels)

	var (
		files  []report.File
		failed int
	)
	for _, d := range steps {
		rows := sample[d.Name]
		t := r.Task(i18n.T(d.Label))
		switch d.Name {
		case "transactions", "activity_log":
			for page := 1; page*50 < rows+50; page++ {
				t.Update(i18n.T("page %d · %d transactions", page, min(page*50, rows)))
				if err := pause(120 * time.Millisecond); err != nil {
					t.Fail(err)
					return err
				}
			}
			if d.Name == "transactions" && opts.Details {
				t.Update(i18n.T("fetching details"))
				for i := 0; i <= rows; i += 15 {
					t.Progress(i, rows)
					if err := pause(40 * time.Millisecond); err != nil {
						t.Fail(err)
						return err
					}
				}
			}
		case "positions", "cash":
			for i, account := range []string{"Securities account", "PEA"} {
				t.Update(i18n.T(account))
				t.Progress(i, 2)
				if err := pause(500 * time.Millisecond); err != nil {
					t.Fail(err)
					return err
				}
			}
		case "orders":
			if err := pause(300 * time.Millisecond); err != nil {
				return err
			}
			t.Fail(errors.New(i18n.T("rejected by Trade Republic (demo)")))
			failed++
			continue
		default:
			if err := pause(350 * time.Millisecond); err != nil {
				t.Fail(err)
				return err
			}
		}
		if rows == 0 {
			t.Skip(i18n.T("none reported"))
			continue
		}
		t.Done(i18n.T("%d rows", rows))
		if d.Name == "activity_log" {
			r.Warn(i18n.T("%s: partial data (%s)", i18n.T(d.Label), i18n.T("rejected by Trade Republic (demo)")))
		}
		path := filepath.Join(opts.OutputDir, d.Name+"."+string(opts.Format))
		files = append(files, report.File{Name: i18n.T(d.Label), Path: path, Rows: rows})
	}

	r.Finish(report.Summary{Files: files, Failed: failed, Elapsed: time.Since(started), OutputDir: opts.OutputDir})
	return nil
}
