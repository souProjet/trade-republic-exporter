package tui

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"

	"github.com/souProjet/trade-republic-exporter/internal/config"
	"github.com/souProjet/trade-republic-exporter/internal/export"
)

// ErrAborted is returned when the user leaves the editor without saving.
var ErrAborted = errors.New("editor closed without saving")

// EditConfig opens the full-screen settings editor on store and saves the
// result. The PIN goes to the system keychain.
func EditConfig(ctx context.Context, store *config.Store) error {
	form, values := newEditor(store)
	if err := form.RunWithContext(ctx); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return ErrAborted
		}
		return err
	}
	return values.save(store)
}

// editorValues are the form fields, bound to the inputs.
type editorValues struct {
	phone, pin, format, dialect, outputDir, mode string
	details                                      bool
	datasets                                     []string
}

// newEditor builds the form, prefilled with the effective settings.
func newEditor(store *config.Store) (*huh.Form, *editorValues) {
	value := func(name string) string { return store.Resolve(name).Value }
	v := &editorValues{
		phone:     value("account.phone_number"),
		format:    value("export.format"),
		dialect:   value("export.csv_dialect"),
		outputDir: value("export.output_dir"),
		details:   value("export.details") == "true",
		mode:      value("interface.mode"),
	}

	settings, _ := store.Settings()
	v.datasets = settings.Datasets
	if len(v.datasets) == 0 {
		for _, d := range export.Datasets {
			v.datasets = append(v.datasets, d.Name)
		}
	}
	datasetOptions := make([]huh.Option[string], len(export.Datasets))
	for i, d := range export.Datasets {
		datasetOptions[i] = huh.NewOption(fmt.Sprintf("%-15s %s", d.Label, d.Description), d.Name).
			Selected(slices.Contains(v.datasets, d.Name))
	}

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Phone number").
				Description(envNote(store, "account.phone_number", "International format, for example +33612345678.")).
				Placeholder("+33612345678").
				Value(&v.phone).
				Validate(validator("account.phone_number", true)),
			huh.NewInput().
				Title("PIN").
				Description(pinDescription(store)).
				Placeholder("leave empty to keep the current one").
				EchoMode(huh.EchoModePassword).
				CharLimit(4).
				Value(&v.pin).
				Validate(validator("account.pin", true)),
		).Title("Account").Description("Who to sign in as."),

		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Format").
				Options(
					huh.NewOption("CSV   for spreadsheets", string(export.CSV)),
					huh.NewOption("JSON  for scripts and tools", string(export.JSON)),
				).
				Value(&v.format),
			huh.NewSelect[string]().
				Title("CSV dialect").
				Description("How numbers and dates are written.").
				Options(
					huh.NewOption("European   ;  12,50  31/12/2026 18:30", export.European.Name),
					huh.NewOption("Standard   ,  12.50  2026-12-31T18:30:00Z", export.Standard.Name),
				).
				Value(&v.dialect),
			huh.NewInput().
				Title("Output directory").
				Description("Relative to where you run the command, unless absolute. ~ works.").
				Value(&v.outputDir).
				Validate(validator("export.output_dir", false)),
		).Title("Output").Description("Where the files go and what they look like."),

		huh.NewGroup(
			huh.NewMultiSelect[string]().
				Title("Datasets").
				Description("Space toggles, a selects all.").
				Options(datasetOptions...).
				Value(&v.datasets).
				Validate(func(v []string) error {
					if len(v) == 0 {
						return errors.New("pick at least one dataset")
					}
					return nil
				}),
			huh.NewConfirm().
				Title("Transaction details").
				Description("Adds fees, quantities and venues. One extra request per transaction, so slower.").
				Affirmative("Fetch them").
				Negative("Skip").
				Value(&v.details),
		).Title("Data").Description("What to export."),

		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Interface").
				Options(
					huh.NewOption("Auto         full screen in a terminal, plain lines otherwise", config.ModeAuto),
					huh.NewOption("Full screen  always the dashboard", config.ModeFullscreen),
					huh.NewOption("Plain        line by line, like a log", config.ModePlain),
				).
				Value(&v.mode),
		).Title("Interface"),
	).
		WithTheme(huh.ThemeFunc(editorTheme)).
		WithShowHelp(true).
		WithViewHook(func(view tea.View) tea.View {
			view.AltScreen = true
			view.Content = editorHeader(store.Path()) + indent(view.Content, "  ")
			return view
		})
	return form, v
}

// save writes the values to the store, and the PIN to the keychain.
func (v *editorValues) save(store *config.Store) error {
	updates := map[string]string{
		"account.phone_number": v.phone,
		"export.format":        v.format,
		"export.csv_dialect":   v.dialect,
		"export.output_dir":    v.outputDir,
		"export.datasets":      strings.Join(v.datasets, ","),
		"export.details":       fmt.Sprint(v.details),
		"interface.mode":       v.mode,
	}
	for _, k := range config.Keys {
		v, ok := updates[k.Name]
		if !ok {
			continue
		}
		if v == "" {
			if err := store.Unset(k.Name); err != nil {
				return err
			}
			continue
		}
		if err := store.Set(k.Name, v); err != nil {
			return err
		}
	}
	if err := store.Save(); err != nil {
		return err
	}
	if v.pin != "" {
		return store.Set("account.pin", v.pin)
	}
	return nil
}

// validator checks a field with the setting's own rules.
func validator(name string, optional bool) func(string) error {
	return func(v string) error {
		if strings.TrimSpace(v) == "" && optional {
			return nil
		}
		k, err := config.Lookup(name)
		if err != nil {
			return err
		}
		if _, err := k.Normalize(v); err != nil {
			// The field already names itself; keep only the reason.
			if reason := errors.Unwrap(err); reason != nil {
				return reason
			}
			return err
		}
		return nil
	}
}

func pinDescription(store *config.Store) string {
	switch store.Resolve("account.pin").Source {
	case config.SourceKeychain:
		return "Stored in the " + config.KeychainName() + "."
	case config.SourceEnv:
		return "Set by " + config.EnvPIN + ", which wins over the keychain."
	case config.SourceFile:
		return "Currently in the config file in clear text. Enter it here to move it to the " + config.KeychainName() + "."
	default:
		return "Saved to the " + config.KeychainName() + ". Leave empty to be asked at every run."
	}
}

func envNote(store *config.Store, name, description string) string {
	if v := store.Resolve(name); v.Source == config.SourceEnv {
		return description + " Overridden by " + v.Key.Env + " right now."
	}
	return description
}

func editorHeader(path string) string {
	pal := newPalette(true)
	brand := lipgloss.NewStyle().Foreground(pal.accent).Background(pal.accentBg).Bold(true).Render(" ◆ Trade Republic Exporter ")
	return "\n " + brand + " " + fg(pal.muted).Render("Settings · "+path) + "\n\n"
}

func indent(s, prefix string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = prefix + l
	}
	return strings.Join(lines, "\n")
}

// editorTheme is the Charm theme with the dashboard accent.
func editorTheme(isDark bool) *huh.Styles {
	t := huh.ThemeCharm(isDark)
	pal := newPalette(isDark)

	t.Focused.Base = t.Focused.Base.BorderForeground(pal.accent)
	t.Focused.Title = t.Focused.Title.Foreground(pal.accent).Bold(true)
	t.Focused.NoteTitle = t.Focused.NoteTitle.Foreground(pal.accent).Bold(true)
	t.Focused.SelectSelector = t.Focused.SelectSelector.Foreground(pal.accent)
	t.Focused.MultiSelectSelector = t.Focused.MultiSelectSelector.Foreground(pal.accent)
	t.Focused.SelectedPrefix = t.Focused.SelectedPrefix.Foreground(pal.green)
	t.Focused.FocusedButton = t.Focused.FocusedButton.Background(pal.accent)
	t.Focused.TextInput.Cursor = t.Focused.TextInput.Cursor.Foreground(pal.accent)
	t.Focused.TextInput.Prompt = t.Focused.TextInput.Prompt.Foreground(pal.accent)
	t.Group.Title = t.Group.Title.Foreground(pal.accent).Bold(true)
	t.Blurred.Title = t.Blurred.Title.Foreground(pal.muted)
	t.Blurred.SelectSelector = t.Blurred.SelectSelector.Foreground(pal.faint)
	t.Blurred.SelectedPrefix = t.Blurred.SelectedPrefix.Foreground(pal.green)
	return t
}
