package ui

import (
	"fmt"
	"os"
	"strings"
	"time"

	"golang.org/x/term"

	"github.com/souProjet/trade-republic-exporter/internal/report"
)

// Reporter adapts the line renderer to report.Reporter, for pipes, logs and
// terminals where the full-screen interface is turned off.
func (u *UI) Reporter() report.Reporter {
	return &plainReporter{ui: u}
}

type plainReporter struct {
	ui *UI
}

func (r *plainReporter) Phase(name string) { r.ui.Section(name) }

func (r *plainReporter) Plan([]string) {}

func (r *plainReporter) Task(label string) report.Task {
	return &plainTask{task: r.ui.Task(label)}
}

func (r *plainReporter) Accounts(accounts []report.Account) {
	rows := make([][]string, len(accounts))
	for i, a := range accounts {
		rows[i] = []string{a.Label, a.Securities, a.Cash, a.Currency}
	}
	r.ui.Table(rows)
}

func (r *plainReporter) Warn(msg string) { r.ui.Warn("%s", msg) }

func (r *plainReporter) Ask(p report.Prompt) (string, error) {
	if p.Description != "" {
		r.ui.Detail("%s", p.Description)
	}
	label := p.Title
	if !p.Deadline.IsZero() {
		label += fmt.Sprintf(", %ds left", int(time.Until(p.Deadline).Seconds()))
	}
	if p.Alternative != "" {
		label += fmt.Sprintf(" (or type %s to %s)", p.Alternative, p.AlternativeLabel)
	}
	label += ":"

	if p.Secret {
		if f, ok := r.ui.in.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
			r.ui.mu.Lock()
			r.ui.printf("  %s %s ", r.ui.paint(cyan, "?"), label)
			r.ui.mu.Unlock()
			secret, err := term.ReadPassword(int(f.Fd()))
			r.ui.printf("\n")
			if err != nil {
				return "", fmt.Errorf("read answer: %w", err)
			}
			return strings.TrimSpace(string(secret)), nil
		}
	}
	return r.ui.Ask(label)
}

func (r *plainReporter) Finish(s report.Summary) {
	r.ui.Section("Files")
	rows := make([][]string, len(s.Files))
	for i, f := range s.Files {
		rows[i] = []string{f.Name, fmt.Sprintf("%d rows", f.Rows), f.Path}
	}
	r.ui.Table(rows)

	line := fmt.Sprintf("%d files, %d rows, %.1fs", len(s.Files), s.Rows(), s.Elapsed.Seconds())
	if s.Failed > 0 {
		line += fmt.Sprintf(", %d failed", s.Failed)
	}
	r.ui.Section("Done")
	r.ui.Detail("%s", line)
}

type plainTask struct {
	task   *Task
	detail string
}

func (t *plainTask) Update(detail string) {
	t.detail = detail
	t.task.Update("%s", detail)
}

func (t *plainTask) Progress(done, total int) {
	if total <= 0 {
		return
	}
	if t.detail == "" {
		t.task.Update("%d/%d", done, total)
		return
	}
	t.task.Update("%s · %d/%d", t.detail, done, total)
}

func (t *plainTask) Done(detail string) { t.task.Done("%s", detail) }
func (t *plainTask) Skip(detail string) { t.task.Skip("%s", detail) }
func (t *plainTask) Fail(err error)     { t.task.Fail(err) }
