package tui

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/souProjet/trade-republic-exporter/internal/report"
)

const (
	minWidth   = 64
	minHeight  = 18
	wideLayout = 96
	labelWidth = 16
	timeWidth  = 6
)

type status int

const (
	statusPending status = iota
	statusRunning
	statusDone
	statusSkipped
	statusFailed
)

type row struct {
	label       string
	status      status
	detail      string
	done, total int
	started     time.Time
	elapsed     time.Duration
}

type level int

const (
	levelInfo level = iota
	levelWarn
	levelError
)

type logEntry struct {
	at    time.Time
	level level
	text  string
}

type askReply struct {
	value string
	err   error
}

type promptState struct {
	report.Prompt
	opened time.Time
	input  textinput.Model
	reply  chan<- askReply
}

// Messages sent by the reporter while the pipeline runs.
type (
	phaseMsg     struct{ name string }
	planMsg      struct{ labels []string }
	taskStartMsg struct {
		id    int
		label string
		at    time.Time
	}
	taskUpdateMsg struct {
		id     int
		detail string
	}
	taskProgressMsg struct{ id, done, total int }
	taskEndMsg      struct {
		id     int
		status status
		detail string
		at     time.Time
	}
	accountsMsg struct{ accounts []report.Account }
	logMsg      struct {
		level level
		text  string
	}
	askMsg struct {
		prompt report.Prompt
		reply  chan<- askReply
	}
	finishMsg struct{ summary report.Summary }
	doneMsg   struct{ err error }
	tickMsg   time.Time
)

type keyMap struct {
	Quit, Scroll, Open, Submit, Alternative, Cancel key.Binding
}

func newKeyMap() keyMap {
	return keyMap{
		Quit:        key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
		Scroll:      key.NewBinding(key.WithKeys("up", "down", "pgup", "pgdown", "k", "j"), key.WithHelp("↑/↓", "scroll log")),
		Open:        key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "open folder")),
		Submit:      key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "confirm")),
		Alternative: key.NewBinding(key.WithKeys("ctrl+s"), key.WithHelp("ctrl+s", "")),
		Cancel:      key.NewBinding(key.WithKeys("esc", "ctrl+c"), key.WithHelp("esc", "cancel")),
	}
}

// Model is the export dashboard.
type Model struct {
	version string
	info    [][2]string
	cancel  context.CancelFunc

	width, height int
	dark          bool
	pal           palette

	phase        string
	started, now time.Time

	session  []*row
	exports  []*row
	byID     map[int]*row
	accounts []report.Account

	logs    []logEntry
	logView viewport.Model
	follow  bool

	spinner spinner.Model
	help    help.Model
	keys    keyMap

	prompt   *promptState
	summary  *report.Summary
	err      error
	finished bool
}

func newModel(version string, info [][2]string, cancel context.CancelFunc) Model {
	now := time.Now()
	m := Model{
		version: version,
		info:    info,
		cancel:  cancel,
		dark:    true,
		pal:     newPalette(true),
		started: now,
		now:     now,
		byID:    make(map[int]*row),
		logView: viewport.New(),
		follow:  true,
		spinner: spinner.New(spinner.WithSpinner(spinner.MiniDot)),
		help:    help.New(),
		keys:    newKeyMap(),
	}
	m.help.Styles = help.DefaultStyles(true)
	return m
}

func tick() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

// Init starts the spinner and the clock, and asks the terminal for its
// background color to pick the light or dark palette.
func (m Model) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, tick(), tea.RequestBackgroundColor)
}

// Update applies one message.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.resizeLog()
	case tea.BackgroundColorMsg:
		m.dark = msg.IsDark()
		m.pal = newPalette(m.dark)
		m.help.Styles = help.DefaultStyles(m.dark)
		m.renderLog()
	case tickMsg:
		m.now = time.Time(msg)
		if !m.finished {
			return m, tick()
		}
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	case tea.KeyPressMsg:
		return m.handleKey(msg)

	case phaseMsg:
		m.phase = msg.name
	case planMsg:
		for _, label := range msg.labels {
			m.exports = append(m.exports, &row{label: label})
		}
	case taskStartMsg:
		r := m.claim(msg.label)
		r.status, r.started, r.detail, r.done, r.total = statusRunning, msg.at, "", 0, 0
		m.byID[msg.id] = r
	case taskUpdateMsg:
		if r := m.byID[msg.id]; r != nil {
			r.detail = msg.detail
		}
	case taskProgressMsg:
		if r := m.byID[msg.id]; r != nil {
			r.done, r.total = msg.done, msg.total
		}
	case taskEndMsg:
		if r := m.byID[msg.id]; r != nil {
			r.status, r.detail, r.elapsed = msg.status, msg.detail, msg.at.Sub(r.started)
			if msg.status == statusFailed {
				m.log(levelError, r.label+": "+msg.detail)
			}
		}
	case accountsMsg:
		m.accounts = msg.accounts
	case logMsg:
		m.log(msg.level, msg.text)
	case askMsg:
		m.prompt = m.newPrompt(msg)
		return m, m.prompt.input.Focus()
	case finishMsg:
		m.summary = &msg.summary
		m.log(levelInfo, fmt.Sprintf("Wrote %d files, %d rows, to %s", len(msg.summary.Files), msg.summary.Rows(), msg.summary.OutputDir))
	case doneMsg:
		m.finished, m.err, m.now = true, msg.err, time.Now()
		if m.prompt != nil {
			m.prompt.reply <- askReply{err: context.Canceled}
			m.prompt = nil
		}
		if msg.err != nil && !errors.Is(msg.err, context.Canceled) {
			m.log(levelError, msg.err.Error())
		}
	}
	return m, nil
}

// claim returns the row a starting task belongs to: its pending export row
// when the plan announced it, a new export row once the plan is known, and a
// session row before that.
func (m *Model) claim(label string) *row {
	for _, r := range m.exports {
		if r.label == label && r.status == statusPending {
			return r
		}
	}
	r := &row{label: label}
	if len(m.exports) > 0 {
		m.exports = append(m.exports, r)
	} else {
		m.session = append(m.session, r)
	}
	return r
}

func (m Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if p := m.prompt; p != nil {
		switch {
		case key.Matches(msg, m.keys.Cancel):
			p.reply <- askReply{err: context.Canceled}
			m.prompt = nil
			m.log(levelWarn, "Canceled by user")
			m.cancel()
			return m, nil
		case key.Matches(msg, m.keys.Submit):
			value := strings.TrimSpace(p.input.Value())
			if value == "" {
				return m, nil
			}
			p.reply <- askReply{value: value}
			m.prompt = nil
			return m, nil
		case key.Matches(msg, m.keys.Alternative) && p.Alternative != "":
			p.reply <- askReply{value: p.Alternative}
			m.prompt = nil
			return m, nil
		}
		var cmd tea.Cmd
		p.input, cmd = p.input.Update(msg)
		return m, cmd
	}

	switch {
	case key.Matches(msg, m.keys.Quit):
		m.cancel()
		return m, tea.Quit
	case key.Matches(msg, m.keys.Open) && m.summary != nil:
		return m, openFolder(m.summary.OutputDir)
	case key.Matches(msg, m.keys.Scroll):
		var cmd tea.Cmd
		m.logView, cmd = m.logView.Update(msg)
		m.follow = m.logView.AtBottom()
		return m, cmd
	}
	return m, nil
}

func (m Model) newPrompt(msg askMsg) *promptState {
	in := textinput.New()
	in.Prompt = ""
	in.Placeholder = msg.prompt.Placeholder
	in.CharLimit = 12
	in.SetWidth(24)
	if msg.prompt.Secret {
		in.EchoMode = textinput.EchoPassword
		in.EchoCharacter = '•'
	}
	return &promptState{Prompt: msg.prompt, opened: time.Now(), input: in, reply: msg.reply}
}

func openFolder(dir string) tea.Cmd {
	return func() tea.Msg {
		abs, err := filepath.Abs(dir)
		if err != nil {
			abs = dir
		}
		var cmd *exec.Cmd
		switch runtime.GOOS {
		case "darwin":
			cmd = exec.Command("open", abs)
		case "windows":
			cmd = exec.Command("explorer", abs)
		default:
			cmd = exec.Command("xdg-open", abs)
		}
		if err := cmd.Start(); err != nil {
			return logMsg{level: levelWarn, text: fmt.Sprintf("Could not open %s: %v", abs, err)}
		}
		return logMsg{level: levelInfo, text: "Opened " + abs}
	}
}

func (m *Model) log(l level, text string) {
	m.logs = append(m.logs, logEntry{at: time.Now(), level: l, text: text})
	m.renderLog()
}

func (m *Model) logHeight() int { return max(5, min(9, m.height/4)) }

func (m *Model) resizeLog() {
	m.logView.SetWidth(max(1, m.width-4))
	m.logView.SetHeight(max(1, m.logHeight()-2))
	m.renderLog()
}

func (m *Model) renderLog() {
	lines := make([]string, len(m.logs))
	for i, e := range m.logs {
		stamp := fg(m.pal.faint).Render(e.at.Format("15:04:05"))
		var text string
		switch e.level {
		case levelWarn:
			text = fg(m.pal.yellow).Render("! " + e.text)
		case levelError:
			text = fg(m.pal.red).Render("✗ " + e.text)
		default:
			text = fg(m.pal.muted).Render(e.text)
		}
		lines[i] = stamp + "  " + text
	}
	m.logView.SetContent(strings.Join(lines, "\n"))
	if m.follow {
		m.logView.GotoBottom()
	}
}

// View renders the dashboard in the alternate screen.
func (m Model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	return v
}

func (m Model) render() string {
	if m.width == 0 || m.height == 0 {
		return ""
	}
	if m.width < minWidth || m.height < minHeight {
		msg := fmt.Sprintf("Make the terminal at least %d×%d (now %d×%d)", minWidth, minHeight, m.width, m.height)
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, fg(m.pal.muted).Render(msg))
	}

	logH := m.logHeight()
	bodyH := m.height - 3 - logH

	var body string
	sessionActive := !m.finished && len(m.exports) == 0
	if m.width >= wideLayout {
		leftW := max(38, min(54, m.width*9/20))
		rightW := m.width - leftW - 1
		body = lipgloss.JoinHorizontal(lipgloss.Top,
			panel(m.pal, "Session", m.sessionLines(leftW-4, false), leftW, bodyH, sessionActive),
			" ",
			panel(m.pal, "Export", m.exportLines(rightW-4, bodyH-2), rightW, bodyH, !sessionActive && !m.finished),
		)
	} else {
		// Stacked: the session collapses once it is ready, and the export list
		// gets whatever height is left.
		sessionLines := m.sessionLines(m.width-4, !sessionActive)
		topH := max(3, min(len(sessionLines)+2, bodyH-5))
		body = lipgloss.JoinVertical(lipgloss.Left,
			panel(m.pal, "Session", sessionLines, m.width, topH, sessionActive),
			panel(m.pal, "Export", m.exportLines(m.width-4, bodyH-topH-2), m.width, bodyH-topH, !sessionActive && !m.finished),
		)
	}

	logPanel := panel(m.pal, "Log", strings.Split(m.logView.View(), "\n"), m.width, logH, false)
	screen := strings.Join([]string{m.header(), "", body, logPanel, m.footer()}, "\n")
	if m.prompt != nil {
		screen = m.overlay(screen)
	}
	return screen
}

func (m Model) header() string {
	brand := lipgloss.NewStyle().Foreground(m.pal.accent).Background(m.pal.accentBg).Bold(true).
		Render(" ◆ Trade Republic Exporter ")
	left := brand + " " + fg(m.pal.muted).Render(m.version)

	var state string
	switch {
	case m.finished && m.err != nil && errors.Is(m.err, context.Canceled):
		state = fg(m.pal.yellow).Render("○ Canceled")
	case m.finished && m.summary == nil && m.err != nil:
		state = fg(m.pal.red).Bold(true).Render("✗ Failed")
	case m.finished && m.summary != nil && m.summary.Failed > 0:
		state = fg(m.pal.yellow).Bold(true).Render(fmt.Sprintf("✓ Done · %d failed", m.summary.Failed))
	case m.finished:
		state = fg(m.pal.green).Bold(true).Render("✓ Done")
	case m.prompt != nil:
		state = fg(m.pal.accent).Bold(true).Render("● Waiting for you")
	default:
		phase := m.phase
		if phase == "" {
			phase = "Starting"
		}
		state = fg(m.pal.accent).Render(m.spinner.View()+" ") + fg(m.pal.text).Bold(true).Render(phase)
	}
	right := state + "  " + fg(m.pal.muted).Render(clock(m.now.Sub(m.started))) + " "

	gap := m.width - ansi.StringWidth(left) - ansi.StringWidth(right)
	if gap < 1 {
		return fit(left, m.width)
	}
	return left + strings.Repeat(" ", gap) + right
}

func (m Model) footer() string {
	var bindings []key.Binding
	switch {
	case m.prompt != nil:
		bindings = append(bindings, m.keys.Submit)
		if m.prompt.Alternative != "" {
			alt := m.keys.Alternative
			alt.SetHelp("ctrl+s", m.prompt.AlternativeLabel)
			bindings = append(bindings, alt)
		}
		bindings = append(bindings, m.keys.Cancel)
	case m.finished:
		if m.summary != nil && len(m.summary.Files) > 0 {
			bindings = append(bindings, m.keys.Open)
		}
		bindings = append(bindings, m.keys.Scroll, m.keys.Quit)
	default:
		bindings = append(bindings, m.keys.Scroll, m.keys.Quit)
	}
	return fit(" "+m.help.ShortHelpView(bindings), m.width)
}

// sessionLines lists the sign-in steps and the accounts. Compact mode, used
// when space is short and the session is ready, folds the steps into one line
// and each account into one line.
func (m Model) sessionLines(width int, compact bool) []string {
	var lines []string
	switch {
	case len(m.session) == 0:
		lines = append(lines, m.spinnerIcon()+" "+fg(m.pal.muted).Render("Starting…"))
	case compact && m.sessionSettled():
		lines = append(lines, fg(m.pal.green).Render("✓")+" "+fg(m.pal.text).Render("Signed in")+
			fg(m.pal.muted).Render(fmt.Sprintf(" · %d steps", len(m.session))))
	default:
		for _, r := range m.session {
			lines = append(lines, m.rowLine(r, width))
		}
	}
	if len(m.accounts) == 0 {
		return append(lines, m.infoLines(width, compact)...)
	}

	mark := fg(m.pal.accent).Render("▌ ")
	if compact {
		labelW := 0
		for _, a := range m.accounts {
			labelW = max(labelW, ansi.StringWidth(a.Label))
		}
		for _, a := range m.accounts {
			lines = append(lines, mark+fit(fg(m.pal.text).Bold(true).Render(a.Label), labelW)+"  "+
				fg(m.pal.muted).Render(strings.Join(nonEmpty(a.Securities, a.Currency), " · ")))
		}
		return lines
	}
	lines = append(lines, "", fg(m.pal.muted).Bold(true).Render("ACCOUNTS"))
	for _, a := range m.accounts {
		title := fg(m.pal.text).Bold(true).Render(a.Label)
		currency := fg(m.pal.muted).Render(a.Currency)
		gap := max(1, width-2-ansi.StringWidth(a.Label)-ansi.StringWidth(a.Currency))
		lines = append(lines,
			mark+title+strings.Repeat(" ", gap)+currency,
			mark+fg(m.pal.muted).Render(strings.Join(nonEmpty(a.Securities, a.Cash), " · ")),
		)
	}
	return append(lines, m.infoLines(width, compact)...)
}

// infoLines shows the settings of the run, so the user can tell at a glance
// where the files go and in which format.
func (m Model) infoLines(width int, compact bool) []string {
	if compact || len(m.info) == 0 {
		return nil
	}
	keyW := 0
	for _, kv := range m.info {
		keyW = max(keyW, ansi.StringWidth(kv[0]))
	}
	lines := []string{"", fg(m.pal.muted).Bold(true).Render("SETTINGS")}
	for _, kv := range m.info {
		lines = append(lines, fit(fg(m.pal.muted).Render(kv[0]), keyW+2)+
			fit(fg(m.pal.text).Render(kv[1]), width-keyW-2))
	}
	return lines
}

func (m Model) sessionSettled() bool {
	for _, r := range m.session {
		if r.status == statusRunning || r.status == statusPending {
			return false
		}
	}
	return true
}

// exportLines lists the datasets and, once finished, the summary. When the
// list is taller than the panel, it scrolls to keep the active row visible.
func (m Model) exportLines(width, height int) []string {
	if len(m.exports) == 0 {
		return []string{fg(m.pal.faint).Render("Waiting for the session…")}
	}
	lines := make([]string, 0, len(m.exports)+4)
	focus := 0
	for i, r := range m.exports {
		lines = append(lines, m.rowLine(r, width))
		if r.status != statusPending {
			focus = i
		}
	}
	if s := m.summary; s != nil {
		total := fg(m.pal.green).Bold(true).Render(fmt.Sprintf("%d files · %d rows", len(s.Files), s.Rows()))
		if s.Failed > 0 {
			total += fg(m.pal.red).Render(fmt.Sprintf(" · %d failed", s.Failed))
		}
		lines = append(lines, "", total+fg(m.pal.muted).Render(" · "+clock(s.Elapsed)),
			fg(m.pal.muted).Render("→ ")+fg(m.pal.text).Render(s.OutputDir))
		focus = len(lines) - 1
	}
	return window(lines, height, focus)
}

// window returns at most height lines of lines, keeping focus visible.
func window(lines []string, height, focus int) []string {
	if height <= 0 || len(lines) <= height {
		return lines
	}
	start := max(0, min(focus-height/2, len(lines)-height))
	return lines[start : start+height]
}

// rowLine renders a task: icon, label, then a progress bar or detail, then
// the elapsed time.
func (m Model) rowLine(r *row, width int) string {
	var icon, label, middle, right string
	labelStyle := fg(m.pal.text)

	switch r.status {
	case statusPending:
		icon = fg(m.pal.faint).Render("·")
		labelStyle = fg(m.pal.muted)
		middle = fg(m.pal.faint).Render("waiting")
	case statusRunning:
		icon = m.spinnerIcon()
		labelStyle = labelStyle.Bold(true)
		middle = fg(m.pal.muted).Render(r.detail)
		if d := m.now.Sub(r.started); d >= time.Second {
			right = fg(m.pal.faint).Render(duration(d))
		}
	case statusDone:
		icon = fg(m.pal.green).Render("✓")
		middle = fg(m.pal.muted).Render(r.detail)
		if r.elapsed >= time.Second {
			right = fg(m.pal.faint).Render(duration(r.elapsed))
		}
	case statusSkipped:
		icon = fg(m.pal.yellow).Render("○")
		middle = fg(m.pal.muted).Render(r.detail)
	case statusFailed:
		icon = fg(m.pal.red).Render("✗")
		middle = fg(m.pal.red).Render(r.detail)
	}
	label = labelStyle.Render(r.label)

	midW := width - 2 - labelWidth - 1 - timeWidth - 1
	if r.status == statusRunning && r.total > 0 {
		count := fmt.Sprintf(" %d/%d", r.done, r.total)
		barW := midW - len(count)
		if barW >= 6 {
			middle = bar(m.pal, float64(r.done)/float64(r.total), barW) + fg(m.pal.muted).Render(count)
		}
	}
	return icon + " " + fit(label, labelWidth) + " " + fit(middle, midW) + " " + lipgloss.PlaceHorizontal(timeWidth, lipgloss.Right, right)
}

func (m Model) spinnerIcon() string {
	return fg(m.pal.accent).Render(m.spinner.View())
}

// overlay draws the prompt as a modal centered over the dashboard.
func (m Model) overlay(screen string) string {
	box := m.promptBox()
	x := max(0, (m.width-lipgloss.Width(box))/2)
	y := max(0, (m.height-lipgloss.Height(box))/2)
	return lipgloss.NewCompositor(
		lipgloss.NewLayer(screen),
		lipgloss.NewLayer(box).X(x).Y(y).Z(1),
	).Render()
}

func (m Model) promptBox() string {
	p := m.prompt
	width := min(58, m.width-4)
	inner := width - 4

	var lines []string
	if p.Description != "" {
		wrapped := lipgloss.NewStyle().Width(inner).Render(p.Description)
		for _, l := range strings.Split(wrapped, "\n") {
			lines = append(lines, fg(m.pal.muted).Render(l))
		}
		lines = append(lines, "")
	}
	lines = append(lines, fg(m.pal.accent).Render("┃ ")+p.input.View(), "")

	if !p.Deadline.IsZero() {
		total := p.Deadline.Sub(p.opened)
		left := max(0, p.Deadline.Sub(m.now))
		ratio := 0.0
		if total > 0 {
			ratio = float64(left) / float64(total)
		}
		secs := fmt.Sprintf(" %2ds", int(left.Round(time.Second).Seconds()))
		lines = append(lines, bar(m.pal, ratio, inner-len(secs))+fg(m.pal.muted).Render(secs), "")
	}

	hints := []string{"enter confirm"}
	if p.Alternative != "" {
		hints = append(hints, "ctrl+s "+p.AlternativeLabel)
	}
	hints = append(hints, "esc cancel")
	lines = append(lines, fg(m.pal.faint).Render(strings.Join(hints, " · ")))

	return panel(m.pal, p.Title, lines, width, len(lines)+2, true)
}

func clock(d time.Duration) string {
	d = d.Round(time.Second)
	return fmt.Sprintf("%02d:%02d", int(d.Minutes()), int(d.Seconds())%60)
}

func duration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
}

func nonEmpty(values ...string) []string {
	var out []string
	for _, v := range values {
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}
