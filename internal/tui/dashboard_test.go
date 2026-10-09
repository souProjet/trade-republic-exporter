package tui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/souProjet/trade-republic-exporter/internal/report"
)

// drive feeds messages to a model the way the program would.
func drive(t *testing.T, m Model, msgs ...tea.Msg) Model {
	t.Helper()
	for _, msg := range msgs {
		next, _ := m.Update(msg)
		m = next.(Model)
	}
	return m
}

// scenario builds a dashboard midway through an export.
func scenario(t *testing.T, width, height int) Model {
	t.Helper()
	m := newModel("v0.2.0", [][2]string{{"Format", "CSV · european"}, {"Output", "out"}}, func() {})
	now := time.Now()
	return drive(t, m,
		tea.WindowSizeMsg{Width: width, Height: height},
		phaseMsg{name: "Authentication"},
		taskStartMsg{id: 1, label: "Device identity", at: now},
		taskEndMsg{id: 1, status: statusDone, detail: "reused", at: now},
		taskStartMsg{id: 2, label: "AWS WAF token", at: now},
		taskEndMsg{id: 2, status: statusDone, detail: "solved", at: now.Add(6 * time.Second)},
		phaseMsg{name: "Session"},
		taskStartMsg{id: 3, label: "Accounts", at: now},
		taskEndMsg{id: 3, status: statusDone, detail: "2 found", at: now},
		accountsMsg{accounts: []report.Account{
			{Label: "Securities account", Securities: "0123456789", Cash: "DE00123456789", Currency: "EUR"},
			{Label: "PEA", Securities: "1234567890", Cash: "FR00987654321", Currency: "EUR"},
		}},
		phaseMsg{name: "Export"},
		planMsg{labels: []string{"Accounts", "Positions", "Transactions", "Savings plans", "Open orders"}},
		taskStartMsg{id: 4, label: "Accounts", at: now},
		taskEndMsg{id: 4, status: statusDone, detail: "2 rows", at: now},
		taskStartMsg{id: 5, label: "Positions", at: now},
		taskEndMsg{id: 5, status: statusDone, detail: "37 rows", at: now.Add(2 * time.Second)},
		taskStartMsg{id: 6, label: "Transactions", at: now},
		taskUpdateMsg{id: 6, detail: "fetching details"},
		taskProgressMsg{id: 6, done: 412, total: 565},
		logMsg{level: levelWarn, text: "Activity log: partial data"},
	)
}

// assertFrame checks the layout invariant: the frame fills the terminal
// exactly, with no line wider than it.
func assertFrame(t *testing.T, frame string, width, height int) {
	t.Helper()
	lines := strings.Split(frame, "\n")
	if len(lines) != height {
		t.Errorf("frame has %d lines, want %d", len(lines), height)
	}
	for i, line := range lines {
		if w := ansi.StringWidth(line); w > width {
			t.Errorf("line %d is %d cells wide, terminal is %d: %q", i, w, width, ansi.Strip(line))
		}
	}
}

func TestDashboardLayouts(t *testing.T) {
	for _, size := range []struct{ w, h int }{{120, 36}, {100, 30}, {80, 28}, {64, 18}} {
		m := scenario(t, size.w, size.h)
		frame := m.render()
		assertFrame(t, frame, size.w, size.h)

		plain := ansi.Strip(frame)
		for _, want := range []string{"Trade Republic Exporter", "Session", "Export", "Securities account", "PEA", "412/565", "Activity log: partial data"} {
			if !strings.Contains(plain, want) {
				t.Errorf("%dx%d frame is missing %q", size.w, size.h, want)
			}
		}
	}
}

func TestDashboardTooSmall(t *testing.T) {
	m := scenario(t, 50, 12)
	if !strings.Contains(ansi.Strip(m.render()), "at least") {
		t.Error("a tiny terminal must show a resize hint")
	}
}

func TestPlanRowsAreClaimedInOrder(t *testing.T) {
	m := scenario(t, 120, 36)
	if got := len(m.session); got != 3 {
		t.Errorf("session rows = %d, want 3 (the Accounts discovery stays in the session)", got)
	}
	if got := m.exports[0].status; got != statusDone {
		t.Errorf("export Accounts row status = %v, want done", got)
	}
	if got := m.exports[3].status; got != statusPending {
		t.Errorf("Savings plans status = %v, want pending", got)
	}
}

func TestPromptSubmitAndAlternative(t *testing.T) {
	reply := make(chan askReply, 1)
	m := drive(t, scenario(t, 120, 36), askMsg{
		prompt: report.Prompt{Title: "Two-factor code", Alternative: "sms", AlternativeLabel: "send by SMS", Deadline: time.Now().Add(time.Minute)},
		reply:  reply,
	})

	frame := m.render()
	assertFrame(t, frame, 120, 36)
	if plain := ansi.Strip(frame); !strings.Contains(plain, "Two-factor code") || !strings.Contains(plain, "ctrl+s send by SMS") {
		t.Errorf("the modal is not rendered:\n%s", plain)
	}

	// An empty submit is ignored.
	m = drive(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.prompt == nil {
		t.Fatal("an empty answer must keep the prompt open")
	}

	m = drive(t, m, tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	if got := <-reply; got.value != "sms" || got.err != nil {
		t.Errorf("reply = %+v, want sms", got)
	}
	if m.prompt != nil {
		t.Error("the prompt must close after answering")
	}
}

func TestPromptTypedAnswer(t *testing.T) {
	reply := make(chan askReply, 1)
	m := drive(t, scenario(t, 120, 36), askMsg{prompt: report.Prompt{Title: "PIN", Secret: true}, reply: reply})
	m = drive(t, m,
		tea.KeyPressMsg{Code: '4', Text: "4"},
		tea.KeyPressMsg{Code: '7', Text: "7"},
	)
	if strings.Contains(ansi.Strip(m.render()), "47") {
		t.Error("a secret answer must be masked on screen")
	}
	drive(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if got := <-reply; got.value != "47" {
		t.Errorf("reply = %+v, want 47", got)
	}
}

func TestPromptCancel(t *testing.T) {
	canceled := false
	m := newModel("dev", nil, func() { canceled = true })
	reply := make(chan askReply, 1)
	m = drive(t, m, tea.WindowSizeMsg{Width: 100, Height: 30}, askMsg{prompt: report.Prompt{Title: "Code"}, reply: reply})
	drive(t, m, tea.KeyPressMsg{Code: tea.KeyEscape})

	if got := <-reply; !errors.Is(got.err, context.Canceled) {
		t.Errorf("reply = %+v, want a cancellation", got)
	}
	if !canceled {
		t.Error("canceling the prompt must cancel the run")
	}
}

func TestFinishedStates(t *testing.T) {
	m := drive(t, scenario(t, 120, 36),
		finishMsg{summary: report.Summary{Files: []report.File{{Name: "Positions", Path: "out/positions.csv", Rows: 37}}, Failed: 1, OutputDir: "out"}},
		doneMsg{err: errors.New("1 dataset(s) could not be exported")},
	)
	plain := ansi.Strip(m.render())
	for _, want := range []string{"Done · 1 failed", "1 files · 37 rows", "→ out", "open folder"} {
		if !strings.Contains(plain, want) {
			t.Errorf("finished frame is missing %q", want)
		}
	}

	failed := drive(t, scenario(t, 120, 36), doneMsg{err: errors.New("login: HTTP 403")})
	if !strings.Contains(ansi.Strip(failed.render()), "✗ Failed") {
		t.Error("a fatal error must show the failed state")
	}
}

func TestDoneClosesOpenPrompt(t *testing.T) {
	reply := make(chan askReply, 1)
	m := drive(t, scenario(t, 120, 36), askMsg{prompt: report.Prompt{Title: "Code"}, reply: reply})
	m = drive(t, m, doneMsg{err: context.Canceled})
	if m.prompt != nil {
		t.Error("the prompt must close when the job ends")
	}
	if got := <-reply; !errors.Is(got.err, context.Canceled) {
		t.Errorf("reply = %+v", got)
	}
}

func TestSettingsShownInWideLayout(t *testing.T) {
	plain := ansi.Strip(scenario(t, 120, 36).render())
	if !strings.Contains(plain, "SETTINGS") || !strings.Contains(plain, "CSV · european") {
		t.Errorf("the wide layout must list the settings:\n%s", plain)
	}
}
