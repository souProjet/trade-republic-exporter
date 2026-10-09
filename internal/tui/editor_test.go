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

func TestEditorPrefillsAndRenders(t *testing.T) {
	store := newTestStore(t, "[account]\nphone_number = +33612345678\n[export]\nformat = json\ndatasets = positions\n")
	form, v := newEditor(store)
	if v.phone != "+33612345678" || v.format != "json" || len(v.datasets) != 1 {
		t.Errorf("prefilled values = %+v", v)
	}

	form.Init()
	form.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	view := ansi.Strip(form.View())
	for _, want := range []string{"Phone number", "+33612345678", "PIN"} {
		if !strings.Contains(view, want) {
			t.Errorf("editor view is missing %q:\n%s", want, view)
		}
	}
}

func TestEditorSave(t *testing.T) {
	store := newTestStore(t, "")
	_, v := newEditor(store)
	v.phone = "+33 6 12 34 56 78"
	v.pin = "4791"
	v.format = "json"
	v.datasets = []string{"transactions"}
	v.details = true

	if err := v.save(store); err != nil {
		t.Fatalf("save: %v", err)
	}
	settings, err := store.Settings()
	if err != nil {
		t.Fatal(err)
	}
	if settings.PhoneNumber != "+33612345678" || settings.PIN != "4791" || !settings.Details ||
		strings.Join(settings.Datasets, ",") != "transactions" {
		t.Errorf("saved settings = %+v", settings)
	}

	raw, _ := os.ReadFile(store.Path())
	if strings.Contains(string(raw), "4791") {
		t.Error("the PIN must not land in the file")
	}
}

func TestEditorValidator(t *testing.T) {
	check := validator("account.phone_number", true)
	if err := check(""); err != nil {
		t.Errorf("an empty optional field must pass, got %v", err)
	}
	err := check("0612345678")
	if err == nil {
		t.Fatal("a local number must be rejected")
	}
	if strings.Contains(err.Error(), "account.phone_number") {
		t.Errorf("the message must not repeat the setting name: %q", err)
	}
}
