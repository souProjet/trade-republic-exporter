package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/souProjet/trade-republic-exporter/internal/config"
	"github.com/souProjet/trade-republic-exporter/internal/export"
	"github.com/souProjet/trade-republic-exporter/internal/i18n"
)

// ErrAborted is returned when the settings screen closes without saving.
var ErrAborted = errors.New("settings closed without saving")

const settingsMaxWidth = 100

// EditSettings opens the settings screen on store. Changes are written when
// the user saves; the PIN goes to the system keychain. On the first run,
// a phone number is required before saving.
func EditSettings(ctx context.Context, store *config.Store, firstRun bool) error {
	language := i18n.Current()
	final, err := tea.NewProgram(newSettings(store, firstRun), tea.WithContext(ctx)).Run()
	if err != nil {
		i18n.Set(language)
		return err
	}
	if m, ok := final.(settingsModel); !ok || !m.saved {
		i18n.Set(language)
		return ErrAborted
	}
	return nil
}

// setting is one editable row.
type setting struct {
	key     config.Key
	section string
	label   string
	help    string
	value   string
	initial string
	source  config.Source
}

func (s setting) dirty() bool {
	if s.key.Kind == config.KindSecret {
		return s.value != ""
	}
	return s.value != s.initial
}

type settingsMode int

const (
	modeBrowse settingsMode = iota
	modeEdit
	modeChecklist
	modeConfirmQuit
)

type settingsModel struct {
	store    *config.Store
	firstRun bool

	width, height int
	dark          bool
	pal           palette

	items  []setting
	focus  int
	mode   settingsMode
	input  textinput.Model
	err    string
	saved  bool
	detect i18n.Language

	// checklist state for export.datasets
	checks []bool
	cursor int
}

func newSettings(store *config.Store, firstRun bool) settingsModel {
	row := func(section, label, help, name string) setting {
		v := store.Resolve(name)
		value := v.Value
		if v.Key.Kind == config.KindSecret {
			value = ""
		}
		return setting{key: v.Key, section: section, label: label, help: help, value: value, initial: value, source: v.Source}
	}
	m := settingsModel{
		store:    store,
		firstRun: firstRun,
		dark:     true,
		pal:      newPalette(true),
		detect:   i18n.Detect(),
		items: []setting{
			row(i18n.N("Account"), i18n.N("Phone number"), i18n.N("The number you sign in to Trade Republic with, in international format, for example +33612345678."), "account.phone_number"),
			row(i18n.N("Account"), i18n.N("PIN"), i18n.N("Your 4-digit Trade Republic PIN. It is kept in the %s, never in the settings file. Leave it unset to be asked at every run."), "account.pin"),
			row(i18n.N("Export"), i18n.N("Format"), i18n.N("CSV opens in spreadsheets. JSON keeps the full structure for scripts and tools."), "export.format"),
			row(i18n.N("Export"), i18n.N("CSV dialect"), i18n.N("European: semicolons, 12,50 and 31/12/2026 18:30, for Excel and Numbers in Europe.\nStandard: commas, 12.50 and ISO 8601 dates, for English locales, pandas and databases."), "export.csv_dialect"),
			row(i18n.N("Export"), i18n.N("Output folder"), i18n.N("Where the files are written. A relative path starts from the folder you run the command in; ~ is your home folder."), "export.output_dir"),
			row(i18n.N("Data"), i18n.N("Datasets"), i18n.N("Which files to export. Each one is a separate request to Trade Republic."), "export.datasets"),
			row(i18n.N("Data"), i18n.N("Transaction details"), i18n.N("Adds fees, quantities and the execution venue to every transaction. One extra request per transaction, so the export gets slower."), "export.details"),
			row(i18n.N("Interface"), i18n.N("Display"), i18n.N("Auto shows the dashboard in a terminal and plain lines when the output is redirected, for logs and cron jobs."), "interface.mode"),
			row(i18n.N("Interface"), i18n.N("Language"), i18n.N("Language of the interface. Automatic follows your system."), "interface.language"),
		},
	}
	m.input = textinput.New()
	m.input.Prompt = ""
	m.input.CharLimit = 200
	return m
}

func (m settingsModel) Init() tea.Cmd { return tea.RequestBackgroundColor }

func (m settingsModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case tea.BackgroundColorMsg:
		m.dark = msg.IsDark()
		m.pal = newPalette(m.dark)
		return m, nil
	case tea.KeyPressMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		switch m.mode {
		case modeEdit:
			return m.updateEdit(msg)
		case modeChecklist:
			return m.updateChecklist(msg)
		case modeConfirmQuit:
			return m.updateConfirm(msg)
		default:
			return m.updateBrowse(msg)
		}
	}
	if m.mode == modeEdit {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m settingsModel) updateBrowse(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	it := &m.items[m.focus]
	m.err = ""
	switch msg.String() {
	case "up", "k", "shift+tab":
		m.focus = (m.focus + len(m.items) - 1) % len(m.items)
	case "down", "j", "tab":
		m.focus = (m.focus + 1) % len(m.items)
	case "left", "h":
		m.cycle(it, -1)
	case "right", "l", "space":
		m.cycle(it, 1)
	case "enter":
		switch it.key.Kind {
		case config.KindChoice, config.KindBool:
			m.cycle(it, 1)
		case config.KindList:
			m.openChecklist(it)
		default:
			return m.startEdit(it)
		}
	case "ctrl+s":
		return m.trySave()
	case "esc", "q":
		if m.dirtyCount() > 0 {
			m.mode = modeConfirmQuit
			return m, nil
		}
		return m, tea.Quit
	}
	return m, nil
}

// cycle moves a choice to the previous or next value, or flips a boolean.
func (m *settingsModel) cycle(it *setting, step int) {
	switch it.key.Kind {
	case config.KindBool:
		if it.value == "true" {
			it.value = "false"
		} else {
			it.value = "true"
		}
	case config.KindChoice:
		i := slices.Index(it.key.Choices, it.value)
		it.value = it.key.Choices[(i+step+len(it.key.Choices))%len(it.key.Choices)]
		if it.key.Name == "interface.language" {
			// Switch right away, so the user sees the result.
			i18n.Set(i18n.Language(it.value))
		}
	}
}

func (m settingsModel) startEdit(it *setting) (tea.Model, tea.Cmd) {
	m.mode = modeEdit
	m.input.SetValue("")
	m.input.EchoMode = textinput.EchoNormal
	m.input.Placeholder = ""
	if it.key.Kind == config.KindSecret {
		m.input.EchoMode = textinput.EchoPassword
		m.input.EchoCharacter = '•'
		m.input.CharLimit = 4
		m.input.Placeholder = i18n.T("4 digits")
	} else {
		m.input.CharLimit = 200
		m.input.SetValue(it.value)
		m.input.CursorEnd()
		if it.key.Name == "account.phone_number" {
			m.input.Placeholder = "+33612345678"
		}
	}
	return m, m.input.Focus()
}

func (m settingsModel) updateEdit(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	it := &m.items[m.focus]
	switch msg.String() {
	case "esc":
		m.mode, m.err = modeBrowse, ""
		m.input.Blur()
		return m, nil
	case "enter":
		value := strings.TrimSpace(m.input.Value())
		if value == "" && (it.key.Kind == config.KindSecret || it.key.Name == "account.phone_number") {
			// An empty secret keeps the stored PIN; an empty phone clears it.
			if it.key.Kind != config.KindSecret {
				it.value = ""
			}
			m.mode, m.err = modeBrowse, ""
			m.input.Blur()
			return m, nil
		}
		normalized, err := it.key.Normalize(value)
		if err != nil {
			m.err = reason(err)
			return m, nil
		}
		it.value = normalized
		m.mode, m.err = modeBrowse, ""
		m.input.Blur()
		return m, nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m *settingsModel) openChecklist(it *setting) {
	m.mode, m.cursor = modeChecklist, 0
	m.checks = make([]bool, len(export.Datasets))
	selected := strings.Split(it.value, ",")
	for i, d := range export.Datasets {
		m.checks[i] = it.value == "all" || slices.Contains(selected, d.Name)
	}
}

func (m settingsModel) updateChecklist(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		m.cursor = (m.cursor + len(m.checks) - 1) % len(m.checks)
	case "down", "j":
		m.cursor = (m.cursor + 1) % len(m.checks)
	case "space", "x":
		m.checks[m.cursor] = !m.checks[m.cursor]
	case "a":
		all := !slices.Contains(m.checks, false)
		for i := range m.checks {
			m.checks[i] = !all
		}
	case "esc":
		m.mode, m.err = modeBrowse, ""
	case "enter":
		var names []string
		for i, on := range m.checks {
			if on {
				names = append(names, export.Datasets[i].Name)
			}
		}
		if len(names) == 0 {
			m.err = i18n.T("Pick at least one dataset.")
			return m, nil
		}
		value, _ := m.items[m.focus].key.Normalize(strings.Join(names, ","))
		m.items[m.focus].value = value
		m.mode, m.err = modeBrowse, ""
	}
	return m, nil
}

func (m settingsModel) updateConfirm(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "s", "ctrl+s", "enter":
		m.mode = modeBrowse
		return m.trySave()
	case "d":
		return m, tea.Quit
	case "esc", "n":
		m.mode = modeBrowse
	}
	return m, nil
}

// trySave writes every change, or points at the first problem.
func (m settingsModel) trySave() (tea.Model, tea.Cmd) {
	if err := m.save(); err != nil {
		m.err = err.Error()
		return m, nil
	}
	m.saved = true
	return m, tea.Quit
}

func (m *settingsModel) save() error {
	if m.firstRun && m.find("account.phone_number").value == "" {
		m.focus = m.index("account.phone_number")
		return errors.New(i18n.T("Enter your phone number to continue."))
	}
	for _, it := range m.items {
		if it.key.Kind == config.KindSecret || !it.dirty() {
			continue
		}
		var err error
		if it.value == "" {
			err = m.store.Unset(it.key.Name)
		} else {
			err = m.store.Set(it.key.Name, it.value)
		}
		if err != nil {
			m.focus = m.index(it.key.Name)
			return errors.New(reason(err))
		}
	}
	if err := m.store.Save(); err != nil {
		return err
	}
	if pin := m.find("account.pin"); pin.value != "" {
		if err := m.store.Set(pin.key.Name, pin.value); err != nil {
			m.focus = m.index(pin.key.Name)
			return err
		}
	}
	return nil
}

func (m *settingsModel) find(name string) *setting { return &m.items[m.index(name)] }

func (m *settingsModel) index(name string) int {
	return slices.IndexFunc(m.items, func(s setting) bool { return s.key.Name == name })
}

func (m settingsModel) dirtyCount() int {
	n := 0
	for _, it := range m.items {
		if it.dirty() {
			n++
		}
	}
	return n
}

// reason strips the setting name from a validation error, since the screen
// already shows which setting is being edited.
func reason(err error) string {
	if r := errors.Unwrap(err); r != nil {
		return r.Error()
	}
	return err.Error()
}

// View renders the settings screen.
func (m settingsModel) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	return v
}

func (m settingsModel) render() string {
	if m.width == 0 || m.height == 0 {
		return ""
	}
	if m.width < minWidth || m.height < minHeight {
		msg := i18n.T("Make the terminal at least %d×%d (now %d×%d)", minWidth, minHeight, m.width, m.height)
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, fg(m.pal.muted).Render(msg))
	}

	width := min(m.width, settingsMaxWidth)
	detail := m.detailLines(width - 4)
	detailH := min(len(detail)+2, m.height/2)
	list := m.listLines(width-4, m.height)
	listH := min(len(list)+2, m.height-3-detailH)
	// Both panels fit their content; the footer stays at the bottom.
	gap := m.height - 3 - detailH - listH

	banner := ""
	if m.firstRun {
		banner = fg(m.pal.accent).Render(" " + i18n.T("Welcome! Fill in your phone number and PIN, then press ctrl+s to save and start the export."))
	}

	title := m.items[m.focus].label
	if m.mode == modeChecklist {
		title = i18n.N("Datasets")
	}
	body := strings.Join([]string{
		m.header(width),
		fit(banner, width),
		panel(m.pal, i18n.T("Settings"), m.listLines(width-4, listH-2), width, listH, m.mode == modeBrowse),
		panel(m.pal, i18n.T(title), detail, width, detailH, m.mode != modeBrowse),
	}, "\n") + strings.Repeat("\n", gap+1) + strings.Join([]string{
		m.footer(width),
	}, "\n")

	// Center the block on wide terminals.
	pad := strings.Repeat(" ", (m.width-width)/2)
	lines := strings.Split(body, "\n")
	for i := range lines {
		lines[i] = pad + lines[i]
	}
	return strings.Join(lines, "\n")
}

func (m settingsModel) header(width int) string {
	brand := lipgloss.NewStyle().Foreground(m.pal.accent).Background(m.pal.accentBg).Bold(true).
		Render(" ◆ Trade Republic Exporter ")
	left := brand + " " + fg(m.pal.text).Bold(true).Render(i18n.T("Settings"))

	var right string
	switch n := m.dirtyCount(); {
	case n == 1:
		right = fg(m.pal.accent).Render("● " + i18n.T("1 unsaved change"))
	case n > 1:
		right = fg(m.pal.accent).Render("● " + i18n.T("%d unsaved changes", n))
	default:
		right = fg(m.pal.muted).Render(shortPath(m.store.Path()))
	}
	gap := width - ansi.StringWidth(left) - ansi.StringWidth(right) - 1
	if gap < 1 {
		return fit(left, width)
	}
	return left + strings.Repeat(" ", gap) + right + " "
}

func (m settingsModel) footer(width int) string {
	hint := func(k, label string) string {
		return fg(m.pal.text).Render(k) + " " + fg(m.pal.muted).Render(i18n.T(label))
	}
	var parts []string
	switch m.mode {
	case modeEdit:
		parts = []string{hint("enter", i18n.N("confirm")), hint("esc", i18n.N("cancel"))}
	case modeChecklist:
		parts = []string{hint("↑↓", i18n.N("move")), hint("space", i18n.N("toggle")), hint("a", i18n.N("all")), hint("enter", i18n.N("done")), hint("esc", i18n.N("cancel"))}
	case modeConfirmQuit:
		parts = []string{hint("s", i18n.N("save and quit")), hint("d", i18n.N("discard changes")), hint("esc", i18n.N("keep editing"))}
	default:
		parts = []string{hint("↑↓", i18n.N("move")), hint("enter", i18n.N("change")), hint("←→", i18n.N("switch value")), hint("ctrl+s", i18n.N("save and quit")), hint("esc", i18n.N("quit"))}
	}
	return fit(" "+strings.Join(parts, fg(m.pal.faint).Render(" · ")), width)
}

// listLines renders every setting under its section heading, scrolled to
// keep the focused row visible.
func (m settingsModel) listLines(width, height int) []string {
	labelW := 0
	for _, it := range m.items {
		labelW = max(labelW, ansi.StringWidth(i18n.T(it.label))+3)
	}
	var lines []string
	focusLine := 0
	section := ""
	for i, it := range m.items {
		if it.section != section {
			if section != "" {
				lines = append(lines, "")
			}
			section = it.section
			lines = append(lines, fg(m.pal.muted).Bold(true).Render(strings.ToUpper(i18n.T(section))))
		}

		marker, labelStyle := "  ", fg(m.pal.text)
		if i == m.focus {
			marker, labelStyle = fg(m.pal.accent).Render("▸ "), fg(m.pal.accent).Bold(true)
			focusLine = len(lines)
		}
		value := m.valueCell(it, i == m.focus)
		tag := m.sourceTag(it)
		valueW := width - 2 - labelW - ansi.StringWidth(tag) - 1
		lines = append(lines, marker+fit(labelStyle.Render(i18n.T(it.label)), labelW)+fit(value, valueW)+" "+tag)
	}
	return window(lines, height, focusLine)
}

func (m settingsModel) valueCell(it setting, focused bool) string {
	if focused && m.mode == modeEdit {
		return fg(m.pal.accent).Render("┃ ") + m.input.View()
	}
	muted, text, faint := fg(m.pal.muted), fg(m.pal.text), fg(m.pal.faint)

	switch it.key.Kind {
	case config.KindSecret:
		switch {
		case it.value != "":
			return fg(m.pal.accent).Render("•••• ") + muted.Render(i18n.T("new, saved to the %s", config.KeychainName()))
		case it.source == config.SourceKeychain:
			return text.Render("•••• ") + muted.Render(config.KeychainName())
		case it.source == config.SourceEnv:
			return text.Render("•••• ") + muted.Render(config.EnvPIN)
		case it.source == config.SourceFile:
			return fg(m.pal.yellow).Render("•••• " + i18n.T("in clear text in the file, enter it again to secure it"))
		default:
			return faint.Render(i18n.T("not set, asked at every run"))
		}
	case config.KindBool:
		if it.value == "true" {
			return fg(m.pal.green).Render("✓ " + i18n.T("Yes"))
		}
		return muted.Render("○ " + i18n.T("No"))
	case config.KindChoice:
		arrows := func(s string) string {
			if focused {
				return fg(m.pal.accent).Render("‹ ") + text.Bold(true).Render(s) + fg(m.pal.accent).Render(" ›")
			}
			return text.Render(s)
		}
		cell := arrows(m.choiceLabel(it))
		if hint := m.choiceHint(it); hint != "" {
			cell += "  " + muted.Render(hint)
		}
		return cell
	case config.KindList:
		if it.value == "all" || it.value == "" {
			return text.Render(i18n.T("All")) + muted.Render(fmt.Sprintf(" (%d)", len(export.Datasets)))
		}
		names := strings.Split(it.value, ",")
		labels := make([]string, len(names))
		for i, n := range names {
			d, _ := export.LookupDataset(n)
			labels[i] = i18n.T(d.Label)
		}
		return text.Render(i18n.T("%d of %d", len(names), len(export.Datasets))) + muted.Render("  "+strings.Join(labels, ", "))
	}
	if it.value == "" {
		return faint.Render(i18n.T("not set"))
	}
	return text.Render(it.value)
}

func (m settingsModel) choiceLabel(it setting) string {
	switch it.key.Name + "=" + it.value {
	case "export.format=csv":
		return "CSV"
	case "export.format=json":
		return "JSON"
	case "export.csv_dialect=european":
		return i18n.T("European")
	case "export.csv_dialect=standard":
		return i18n.T("Standard")
	case "interface.mode=auto":
		return i18n.T("Automatic")
	case "interface.mode=fullscreen":
		return i18n.T("Full screen")
	case "interface.mode=plain":
		return i18n.T("Plain lines")
	case "interface.language=auto":
		return i18n.T("Automatic")
	case "interface.language=en":
		return "English"
	case "interface.language=fr":
		return "Français"
	}
	return it.value
}

func (m settingsModel) choiceHint(it setting) string {
	switch it.key.Name + "=" + it.value {
	case "export.csv_dialect=european":
		return "; · 12,50 · 31/12/2026 18:30"
	case "export.csv_dialect=standard":
		return ", · 12.50 · 2026-12-31T18:30"
	case "interface.language=auto":
		detected := "English"
		if m.detect == i18n.French {
			detected = "Français"
		}
		return i18n.T("detected: %s", detected)
	}
	return ""
}

func (m settingsModel) sourceTag(it setting) string {
	if it.dirty() {
		return fg(m.pal.accent).Render("● " + i18n.T("modified"))
	}
	label := i18n.T(string(it.source))
	if it.source == config.SourceUnset {
		label = ""
	}
	if it.source == config.SourceEnv {
		return fg(m.pal.yellow).Render(label)
	}
	return fg(m.pal.faint).Render(label)
}

// detailLines explains the focused setting, or shows the dataset checklist.
func (m settingsModel) detailLines(width int) []string {
	wrap := func(s string, style lipgloss.Style) []string {
		var out []string
		for _, l := range strings.Split(lipgloss.NewStyle().Width(width).Render(s), "\n") {
			out = append(out, style.Render(l))
		}
		return out
	}

	if m.mode == modeChecklist {
		labelW := 0
		for _, d := range export.Datasets {
			labelW = max(labelW, ansi.StringWidth(i18n.T(d.Label))+2)
		}
		lines := []string{}
		for i, d := range export.Datasets {
			box := fg(m.pal.faint).Render("[ ]")
			if m.checks[i] {
				box = fg(m.pal.green).Render("[✓]")
			}
			label := fg(m.pal.text).Render(i18n.T(d.Label))
			if i == m.cursor {
				label = fg(m.pal.accent).Bold(true).Render(i18n.T(d.Label))
				box = fg(m.pal.accent).Render("▸") + box
			} else {
				box = " " + box
			}
			lines = append(lines, box+" "+fit(label, labelW)+fg(m.pal.muted).Render(i18n.T(d.Description)))
		}
		if m.err != "" {
			lines = append(lines, "", fg(m.pal.red).Render("✗ "+m.err))
		}
		return lines
	}

	if m.mode == modeConfirmQuit {
		return wrap(i18n.T("You have unsaved changes. Save them before quitting?"), fg(m.pal.text))
	}

	it := m.items[m.focus]
	help := i18n.T(it.help)
	if it.key.Kind == config.KindSecret {
		help = i18n.T(it.help, config.KeychainName())
	}
	var lines []string
	for _, paragraph := range strings.Split(help, "\n") {
		lines = append(lines, wrap(paragraph, fg(m.pal.muted))...)
	}

	if it.source == config.SourceEnv && it.key.Env != "" {
		lines = append(lines, "")
		lines = append(lines, wrap(i18n.T("%s is set in your environment and takes precedence over this setting.", it.key.Env), fg(m.pal.yellow))...)
	}
	if m.err != "" {
		lines = append(lines, "", fg(m.pal.red).Render("✗ "+m.err))
	}
	return lines
}

func shortPath(path string) string {
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(path, home) {
		return "~" + strings.TrimPrefix(path, home)
	}
	return path
}
