package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/zalando/go-keyring"

	"github.com/souProjet/trade-republic-exporter/internal/config"
	"github.com/souProjet/trade-republic-exporter/internal/i18n"
)

func newTestStore(t *testing.T, body string) *config.Store {
	t.Helper()
	keyring.MockInit()
	for _, env := range []string{config.EnvPhoneNumber, config.EnvPIN, config.EnvDeviceInfo} {
		t.Setenv(env, "")
	}
	path := filepath.Join(t.TempDir(), "config.ini")
	if body != "" {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	store, err := config.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func press(t *testing.T, m settingsModel, keys ...string) settingsModel {
	t.Helper()
	for _, k := range keys {
		var msg tea.KeyPressMsg
		switch k {
		case "enter":
			msg = tea.KeyPressMsg{Code: tea.KeyEnter}
		case "esc":
			msg = tea.KeyPressMsg{Code: tea.KeyEscape}
		case "up":
			msg = tea.KeyPressMsg{Code: tea.KeyUp}
		case "down":
			msg = tea.KeyPressMsg{Code: tea.KeyDown}
		case "right":
			msg = tea.KeyPressMsg{Code: tea.KeyRight}
		case "space":
			msg = tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
		case "ctrl+s":
			msg = tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl}
		case "backspace":
			msg = tea.KeyPressMsg{Code: tea.KeyBackspace}
		default:
			r := []rune(k)[0]
			msg = tea.KeyPressMsg{Code: r, Text: k}
		}
		next, _ := m.Update(msg)
		m = next.(settingsModel)
	}
	return m
}

func typeText(t *testing.T, m settingsModel, s string) settingsModel {
	t.Helper()
	keys := make([]string, 0, len(s))
	for _, r := range s {
		keys = append(keys, string(r))
	}
	return press(t, m, keys...)
}

func sized(t *testing.T, m settingsModel, w, h int) settingsModel {
	t.Helper()
	next, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return next.(settingsModel)
}

func TestSettingsLayouts(t *testing.T) {
	for _, size := range []struct{ w, h int }{{140, 40}, {100, 30}, {80, 24}, {64, 18}} {
		m := sized(t, newSettings(newTestStore(t, "[account]\nphone_number = +33612345678\n"), false), size.w, size.h)
		frame := m.render()
		assertFrame(t, frame, size.w, size.h)
		if !strings.Contains(ansi.Strip(frame), "+33612345678") {
			t.Errorf("%dx%d: the phone number is not shown", size.w, size.h)
		}

		checklist := press(t, m, "down", "down", "down", "down", "down", "enter")
		assertFrame(t, checklist.render(), size.w, size.h)
	}
}

func TestSettingsCycleChoiceAndSave(t *testing.T) {
	store := newTestStore(t, "[account]\nphone_number = +33612345678\n")
	m := sized(t, newSettings(store, false), 100, 30)

	m = press(t, m, "down", "down") // Format
	if got := m.items[m.focus].key.Name; got != "export.format" {
		t.Fatalf("focus = %s", got)
	}
	m = press(t, m, "right")
	if m.items[m.focus].value != "json" || m.dirtyCount() != 1 {
		t.Fatalf("format = %q, dirty = %d", m.items[m.focus].value, m.dirtyCount())
	}
	if !strings.Contains(ansi.Strip(m.render()), "1 unsaved change") {
		t.Error("the header must count unsaved changes")
	}

	m = press(t, m, "ctrl+s")
	if !m.saved {
		t.Fatalf("not saved: %s", m.err)
	}
	if got := store.Resolve("export.format").Value; got != "json" {
		t.Errorf("stored format = %q", got)
	}
}

func TestSettingsEditPhoneValidates(t *testing.T) {
	m := sized(t, newSettings(newTestStore(t, ""), false), 100, 30)
	m = press(t, m, "enter")
	if m.mode != modeEdit {
		t.Fatal("enter must start editing a text setting")
	}
	m = typeText(t, m, "0612")
	m = press(t, m, "enter")
	if m.mode != modeEdit || m.err == "" {
		t.Fatalf("an invalid number must keep the editor open with an error, mode=%v err=%q", m.mode, m.err)
	}
	if strings.Contains(m.err, "account.phone_number") {
		t.Errorf("the error must not repeat the setting name: %q", m.err)
	}

	for range 4 {
		m = press(t, m, "backspace")
	}
	m = typeText(t, m, "+33 6 12 34 56 78")
	m = press(t, m, "enter")
	if m.mode != modeBrowse || m.items[0].value != "+33612345678" {
		t.Errorf("mode = %v, phone = %q, err = %q", m.mode, m.items[0].value, m.err)
	}
}

func TestSettingsPINGoesToKeychain(t *testing.T) {
	store := newTestStore(t, "[account]\nphone_number = +33612345678\n")
	m := sized(t, newSettings(store, false), 100, 30)
	m = press(t, m, "down", "enter")
	m = typeText(t, m, "4791")
	if strings.Contains(ansi.Strip(m.render()), "4791") {
		t.Error("the PIN must be masked while typed")
	}
	m = press(t, m, "enter", "ctrl+s")
	if !m.saved {
		t.Fatalf("not saved: %s", m.err)
	}
	if v := store.Resolve("account.pin"); v.Value != "4791" || v.Source != config.SourceKeychain {
		t.Errorf("pin = %+v", v)
	}
	raw, _ := os.ReadFile(store.Path())
	if strings.Contains(string(raw), "4791") {
		t.Error("the PIN must never be written to the file")
	}
}

func TestSettingsChecklist(t *testing.T) {
	store := newTestStore(t, "")
	m := sized(t, newSettings(store, false), 100, 30)
	m = press(t, m, "down", "down", "down", "down", "down", "enter") // Datasets
	if m.mode != modeChecklist {
		t.Fatal("enter on datasets must open the checklist")
	}
	m = press(t, m, "a", "enter")
	if m.mode != modeChecklist || m.err == "" {
		t.Error("an empty selection must be refused")
	}
	m = press(t, m, "space", "down", "space", "enter")
	if got := m.items[m.focus].value; got != "accounts,positions" {
		t.Errorf("datasets = %q", got)
	}
}

func TestSettingsFirstRunRequiresPhone(t *testing.T) {
	m := sized(t, newSettings(newTestStore(t, ""), true), 100, 30)
	if !strings.Contains(ansi.Strip(m.render()), "Welcome") {
		t.Error("the first run must show the welcome banner")
	}
	m = press(t, m, "down", "down", "right", "ctrl+s")
	if m.saved || m.err == "" || m.items[m.focus].key.Name != "account.phone_number" {
		t.Errorf("saving without a phone must fail and focus it: saved=%v err=%q focus=%s", m.saved, m.err, m.items[m.focus].key.Name)
	}
}

func TestSettingsQuitWithChangesAsks(t *testing.T) {
	m := sized(t, newSettings(newTestStore(t, ""), false), 100, 30)
	m = press(t, m, "down", "down", "right", "esc")
	if m.mode != modeConfirmQuit {
		t.Fatal("quitting with unsaved changes must ask first")
	}
	m = press(t, m, "esc")
	if m.mode != modeBrowse {
		t.Error("esc must return to editing")
	}
}

func TestSettingsLanguageSwitchesLive(t *testing.T) {
	defer i18n.Set(i18n.English)
	i18n.Set(i18n.English)
	m := sized(t, newSettings(newTestStore(t, ""), false), 100, 30)
	m = press(t, m, "up") // wraps to Language
	if m.items[m.focus].key.Name != "interface.language" {
		t.Fatalf("focus = %s", m.items[m.focus].key.Name)
	}
	m = press(t, m, "right", "right") // auto -> en -> fr
	if i18n.Current() != i18n.French {
		t.Fatalf("language = %s, want fr", i18n.Current())
	}
	if !strings.Contains(ansi.Strip(m.render()), "Réglages") {
		t.Error("the screen must switch to French right away")
	}
}
