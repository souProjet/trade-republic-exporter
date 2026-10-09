package ui

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func newTestUI(quiet bool, input string) (*UI, *bytes.Buffer) {
	var buf bytes.Buffer
	return New(&buf, strings.NewReader(input), quiet), &buf
}

func TestNewWithoutTerminalDisablesStyling(t *testing.T) {
	u, _ := newTestUI(false, "")
	if u.color || u.anim {
		t.Errorf("color = %v, anim = %v, want both false off a terminal", u.color, u.anim)
	}
}

func TestTaskOutcomes(t *testing.T) {
	u, buf := newTestUI(false, "")
	u.Task("Positions").Done("37 rows")
	u.Task("Accounts").Skip("not available")
	u.Task("Cash").Fail(errors.New("rejected"))

	got := buf.String()
	for _, want := range []string{"✓ Positions", "37 rows", "○ Accounts", "not available", "✗ Cash", "rejected"} {
		if !strings.Contains(got, want) {
			t.Errorf("output %q is missing %q", got, want)
		}
	}
	if strings.Contains(got, "\x1b[") {
		t.Error("output must not contain escape codes off a terminal")
	}
}

func TestQuietHidesSuccessButKeepsProblems(t *testing.T) {
	u, buf := newTestUI(true, "")
	u.Title("app", "dev")
	u.Section("Export")
	u.Task("Positions").Done("37 rows")
	u.Task("Cash").Fail(errors.New("rejected"))
	u.Warn("partial data")

	got := buf.String()
	if strings.Contains(got, "Positions") || strings.Contains(got, "Export") {
		t.Errorf("quiet output leaked progress: %q", got)
	}
	for _, want := range []string{"Cash", "rejected", "partial data"} {
		if !strings.Contains(got, want) {
			t.Errorf("quiet output %q is missing %q", got, want)
		}
	}
}

func TestTableAligns(t *testing.T) {
	u, buf := newTestUI(false, "")
	u.Table([][]string{
		{"PEA", "1234", "EUR"},
		{"Securities account", "5678901234", "EUR"},
	})

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2", len(lines))
	}
	if strings.Index(lines[0], "1234") != strings.Index(lines[1], "5678901234") {
		t.Errorf("columns are not aligned:\n%q\n%q", lines[0], lines[1])
	}
}

func TestAsk(t *testing.T) {
	u, buf := newTestUI(false, " 123456 \n")
	answer, err := u.Ask("Two-factor code:")
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if answer != "123456" {
		t.Errorf("answer = %q, want %q", answer, "123456")
	}
	if !strings.Contains(buf.String(), "Two-factor code:") {
		t.Errorf("prompt was not printed: %q", buf.String())
	}
}

func TestAskWithoutInput(t *testing.T) {
	u, _ := newTestUI(false, "")
	if _, err := u.Ask("Code:"); err == nil {
		t.Error("want an error when stdin is closed")
	}
}
