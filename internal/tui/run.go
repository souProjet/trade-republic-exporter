package tui

import (
	"context"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/souProjet/trade-republic-exporter/internal/report"
)

// Job is the work the dashboard displays.
type Job func(ctx context.Context, r report.Reporter) error

// Result is what remains once the dashboard closes, so it can be printed to
// the regular screen.
type Result struct {
	Summary  *report.Summary
	Warnings []string
	Err      error
	// Interrupted is true when the user quit before the job finished.
	Interrupted bool
}

// Run shows the dashboard while job runs, and returns when the user quits.
// info lists the settings of the run as label and value pairs.
func Run(ctx context.Context, version string, info [][2]string, job Job) (Result, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	program := tea.NewProgram(newModel(version, info, cancel))
	reporter := &teaReporter{ctx: ctx, send: program.Send}

	finished := make(chan error, 1)
	go func() {
		err := job(ctx, reporter)
		finished <- err
		program.Send(doneMsg{err: err})
	}()

	final, runErr := program.Run()
	cancel()

	// Let the job unwind, so headless Chrome is closed before the process
	// exits, without hanging on a stuck network read.
	select {
	case <-finished:
	case <-time.After(3 * time.Second):
	}

	m, _ := final.(Model)
	result := Result{Summary: m.summary, Err: m.err, Interrupted: !m.finished}
	for _, e := range m.logs {
		if e.level == levelWarn {
			result.Warnings = append(result.Warnings, e.text)
		}
	}
	return result, runErr
}

// teaReporter forwards pipeline events to the program as messages.
type teaReporter struct {
	ctx  context.Context
	send func(tea.Msg)
	next atomic.Int64
}

func (r *teaReporter) Phase(name string)    { r.send(phaseMsg{name: name}) }
func (r *teaReporter) Plan(labels []string) { r.send(planMsg{labels: labels}) }
func (r *teaReporter) Warn(msg string)      { r.send(logMsg{level: levelWarn, text: msg}) }

func (r *teaReporter) Accounts(accounts []report.Account) {
	r.send(accountsMsg{accounts: accounts})
}

func (r *teaReporter) Finish(s report.Summary) { r.send(finishMsg{summary: s}) }

func (r *teaReporter) Task(label string) report.Task {
	id := int(r.next.Add(1))
	r.send(taskStartMsg{id: id, label: label, at: time.Now()})
	return &teaTask{r: r, id: id}
}

func (r *teaReporter) Ask(p report.Prompt) (string, error) {
	reply := make(chan askReply, 1)
	r.send(askMsg{prompt: p, reply: reply})
	select {
	case a := <-reply:
		return a.value, a.err
	case <-r.ctx.Done():
		return "", r.ctx.Err()
	}
}

type teaTask struct {
	r  *teaReporter
	id int
}

func (t *teaTask) Update(detail string) { t.r.send(taskUpdateMsg{id: t.id, detail: detail}) }

func (t *teaTask) Progress(done, total int) {
	t.r.send(taskProgressMsg{id: t.id, done: done, total: total})
}

func (t *teaTask) Done(detail string) { t.end(statusDone, detail) }
func (t *teaTask) Skip(detail string) { t.end(statusSkipped, detail) }
func (t *teaTask) Fail(err error)     { t.end(statusFailed, err.Error()) }

func (t *teaTask) end(s status, detail string) {
	t.r.send(taskEndMsg{id: t.id, status: s, detail: detail, at: time.Now()})
}
