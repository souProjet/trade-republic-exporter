package app

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"time"

	"github.com/souProjet/trade-republic-exporter/internal/export"
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

	r.Phase(PhaseAuthentication)
	if err := run("Device identity", "reused", 300*time.Millisecond); err != nil {
		return err
	}
	waf := r.Task("AWS WAF token")
	waf.Update("solving the challenge in headless Chrome")
	if err := pause(1800 * time.Millisecond); err != nil {
		return err
	}
	waf.Done("solved")
	if err := run("Login", RedactPhone("+33612345678"), 500*time.Millisecond); err != nil {
		return err
	}
	if _, err := r.Ask(report.Prompt{
		Title:            "Two-factor code",
		Description:      "Demo mode: type any code.",
		Placeholder:      "code",
		Deadline:         time.Now().Add(60 * time.Second),
		Alternative:      "sms",
		AlternativeLabel: "send by SMS",
	}); err != nil {
		return err
	}
	if err := run("Two-factor code", "session established", 400*time.Millisecond); err != nil {
		return err
	}

	r.Phase(PhaseSession)
	if err := run("WebSocket", "connected", 300*time.Millisecond); err != nil {
		return err
	}
	if err := run("Accounts", "2 found", 400*time.Millisecond); err != nil {
		return err
	}
	r.Accounts([]report.Account{
		{Label: "Securities account", Securities: "0123456789", Cash: "DE00123456789", Currency: "EUR"},
		{Label: "PEA", Securities: "1234567890", Cash: "FR00987654321", Currency: "EUR"},
	})

	r.Phase(PhaseExport)
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
		labels[i] = d.Label
	}
	r.Plan(labels)

	var (
		files  []report.File
		failed int
	)
	for _, d := range steps {
		rows := sample[d.Name]
		t := r.Task(d.Label)
		switch d.Name {
		case "transactions", "activity_log":
			for page := 1; page*50 < rows+50; page++ {
				t.Update(fmt.Sprintf("page %d · %d items", page, min(page*50, rows)))
				if err := pause(120 * time.Millisecond); err != nil {
					t.Fail(err)
					return err
				}
			}
			if d.Name == "transactions" && opts.Details {
				t.Update("fetching details")
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
				t.Update(account)
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
			t.Fail(errors.New(`subscription "orders" rejected by Trade Republic (demo)`))
			failed++
			continue
		default:
			if err := pause(350 * time.Millisecond); err != nil {
				t.Fail(err)
				return err
			}
		}
		if rows == 0 {
			t.Skip("none reported")
			continue
		}
		t.Done(fmt.Sprintf("%d rows", rows))
		if d.Name == "activity_log" {
			r.Warn("Activity log: partial data (rejected by Trade Republic, demo)")
		}
		path := filepath.Join(opts.OutputDir, d.Name+"."+string(opts.Format))
		files = append(files, report.File{Name: d.Label, Path: path, Rows: rows})
	}

	r.Finish(report.Summary{Files: files, Failed: failed, Elapsed: time.Since(started), OutputDir: opts.OutputDir})
	return nil
}
