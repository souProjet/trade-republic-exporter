// Package ui renders the command-line interface: titles, spinners, tables and
// prompts. Everything is written to stderr so stdout stays free for data.
package ui

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/souProjet/trade-republic-exporter/internal/i18n"
)

const (
	reset  = "\x1b[0m"
	bold   = "\x1b[1m"
	dim    = "\x1b[2m"
	red    = "\x1b[31m"
	green  = "\x1b[32m"
	yellow = "\x1b[33m"
	cyan   = "\x1b[36m"

	labelWidth  = 24
	clearLine   = "\r\x1b[2K"
	spinnerTick = 90 * time.Millisecond
)

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// UI is the terminal front end. It is safe for sequential use from one
// goroutine; a running task renders from its own goroutine.
type UI struct {
	mu     sync.Mutex
	out    io.Writer
	in     io.Reader
	reader *bufio.Reader
	color  bool
	anim   bool
	quiet  bool
}

// New builds a UI on w, reading prompt answers from r. Colors and animation are
// enabled only for a terminal that did not opt out through NO_COLOR or TERM.
func New(w io.Writer, r io.Reader, quiet bool) *UI {
	tty := isTerminal(w) && os.Getenv("TERM") != "dumb"
	if _, ok := os.LookupEnv("NO_COLOR"); ok {
		tty = false
	}
	return &UI{out: w, in: r, reader: bufio.NewReader(r), color: tty, anim: tty && !quiet, quiet: quiet}
}

func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func (u *UI) paint(style, s string) string {
	if !u.color || s == "" {
		return s
	}
	return style + s + reset
}

func (u *UI) printf(format string, args ...any) {
	fmt.Fprintf(u.out, format, args...)
}

// Title prints the program banner.
func (u *UI) Title(name, version string) {
	if u.quiet {
		return
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	u.printf("\n  %s %s\n", u.paint(bold, name), u.paint(dim, version))
}

// Section prints a group heading.
func (u *UI) Section(label string) {
	if u.quiet {
		return
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	u.printf("\n  %s\n", u.paint(bold, label))
}

// Warn reports a recoverable problem.
func (u *UI) Warn(format string, args ...any) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.printf("  %s %s\n", u.paint(yellow, "!"), fmt.Sprintf(format, args...))
}

// Error reports a fatal problem.
func (u *UI) Error(err error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.printf("\n  %s %v\n\n", u.paint(red, "✗"), err)
}

// Detail prints an indented informational line.
func (u *UI) Detail(format string, args ...any) {
	if u.quiet {
		return
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	u.printf("    %s\n", u.paint(dim, fmt.Sprintf(format, args...)))
}

// Table prints rows aligned on their widest cell. The first column is
// highlighted and trailing columns are dimmed.
func (u *UI) Table(rows [][]string) {
	if u.quiet || len(rows) == 0 {
		return
	}
	u.mu.Lock()
	defer u.mu.Unlock()

	widths := make([]int, 0, 4)
	for _, row := range rows {
		for i, cell := range row {
			for len(widths) <= i {
				widths = append(widths, 0)
			}
			if n := len([]rune(cell)); n > widths[i] {
				widths[i] = n
			}
		}
	}
	for _, row := range rows {
		var b strings.Builder
		b.WriteString("    ")
		for i, cell := range row {
			pad := strings.Repeat(" ", widths[i]-len([]rune(cell)))
			switch i {
			case 0:
				b.WriteString(cell + pad)
			case len(row) - 1:
				b.WriteString("  " + u.paint(dim, cell+pad))
			default:
				b.WriteString("  " + cell + pad)
			}
		}
		u.printf("%s\n", strings.TrimRight(b.String(), " "))
	}
}

// Ask prints a prompt and reads one line of input.
func (u *UI) Ask(label string) (string, error) {
	u.mu.Lock()
	u.printf("  %s %s ", u.paint(cyan, "?"), label)
	u.mu.Unlock()

	line, err := u.reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("%s: %w", i18n.T("could not read the answer"), err)
	}
	return strings.TrimSpace(line), nil
}

// Task is a unit of work shown as a spinner while it runs and as a single
// result line once it settles.
type Task struct {
	ui     *UI
	label  string
	start  time.Time
	stop   chan struct{}
	closed sync.Once

	mu     sync.Mutex
	detail string
}

// Task starts a labeled task. Exactly one of Done, Skip or Fail must be called.
func (u *UI) Task(label string) *Task {
	t := &Task{ui: u, label: label, start: time.Now(), stop: make(chan struct{})}
	if u.anim {
		go t.animate()
	}
	return t
}

func (t *Task) animate() {
	ticker := time.NewTicker(spinnerTick)
	defer ticker.Stop()
	for frame := 0; ; frame++ {
		select {
		case <-t.stop:
			return
		case <-ticker.C:
			t.mu.Lock()
			detail := t.detail
			t.mu.Unlock()

			t.ui.mu.Lock()
			t.ui.printf("%s  %s %s %s", clearLine,
				t.ui.paint(cyan, spinnerFrames[frame%len(spinnerFrames)]),
				pad(t.label), t.ui.paint(dim, detail))
			t.ui.mu.Unlock()
		}
	}
}

// Update replaces the progress detail shown next to the label.
func (t *Task) Update(format string, args ...any) {
	t.mu.Lock()
	t.detail = fmt.Sprintf(format, args...)
	t.mu.Unlock()
}

// Done settles the task as successful.
func (t *Task) Done(format string, args ...any) {
	t.settle(green, "✓", fmt.Sprintf(format, args...), true)
}

// Skip settles the task as not applicable.
func (t *Task) Skip(format string, args ...any) {
	t.settle(yellow, "○", fmt.Sprintf(format, args...), false)
}

// Fail settles the task as failed. The error is reported, not returned.
func (t *Task) Fail(err error) {
	t.settle(red, "✗", err.Error(), false)
}

func (t *Task) settle(style, mark, detail string, showElapsed bool) {
	t.closed.Do(func() { close(t.stop) })

	elapsed := ""
	if showElapsed && time.Since(t.start) >= time.Second {
		elapsed = fmt.Sprintf(" (%.1fs)", time.Since(t.start).Seconds())
	}

	t.ui.mu.Lock()
	defer t.ui.mu.Unlock()
	if t.ui.quiet && style == green {
		return
	}
	if t.ui.anim {
		t.ui.printf(clearLine)
	}
	t.ui.printf("  %s %s %s\n", t.ui.paint(style, mark), pad(t.label),
		t.ui.paint(dim, detail+elapsed))
}

func pad(label string) string {
	if n := len([]rune(label)); n < labelWidth {
		return label + strings.Repeat(" ", labelWidth-n)
	}
	return label
}
