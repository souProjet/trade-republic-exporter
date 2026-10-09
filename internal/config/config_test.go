package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"

	"github.com/souProjet/trade-republic-exporter/internal/export"
)

func TestMain(m *testing.M) {
	keyring.MockInit()
	os.Exit(m.Run())
}

func clearEnv(t *testing.T) {
	t.Helper()
	for _, env := range []string{EnvPhoneNumber, EnvPIN, EnvWAFToken, EnvDeviceInfo} {
		t.Setenv(env, "")
	}
}

func openTemp(t *testing.T, body string) *Store {
	t.Helper()
	clearEnv(t)
	path := filepath.Join(t.TempDir(), "config.ini")
	if body != "" {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return s
}

func TestDefaultPathHonorsXDG(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg")
	path, err := DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join("/tmp/xdg", appDir, fileName); path != want {
		t.Errorf("DefaultPath = %q, want %q", path, want)
	}
}

func TestDefaults(t *testing.T) {
	s := openTemp(t, "")
	if s.Exists() {
		t.Error("a missing file must not exist")
	}
	settings, err := s.Settings()
	if err != nil {
		t.Fatalf("Settings: %v", err)
	}
	if settings.Format != export.CSV || settings.Dialect.Name != "european" || settings.OutputDir != "out" ||
		settings.Details || settings.Datasets != nil || settings.Interface != ModeAuto {
		t.Errorf("unexpected defaults: %+v", settings)
	}
}

func TestSetSaveReload(t *testing.T) {
	s := openTemp(t, "")
	for name, value := range map[string]string{
		"account.phone_number": "+33 6 12 34 56 78",
		"export.format":        "JSON",
		"export.details":       "yes",
		"export.datasets":      "positions, transactions,positions",
		"interface.mode":       "plain",
	} {
		if name == "export.details" {
			value = "true"
		}
		if err := s.Set(name, value); err != nil {
			t.Fatalf("Set(%s): %v", name, err)
		}
	}
	if err := s.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	info, err := os.Stat(s.Path())
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("file permissions = %o, want 600", perm)
	}

	reloaded, err := Open(s.Path())
	if err != nil {
		t.Fatal(err)
	}
	settings, err := reloaded.Settings()
	if err != nil {
		t.Fatalf("Settings: %v", err)
	}
	if settings.PhoneNumber != "+33612345678" {
		t.Errorf("PhoneNumber = %q", settings.PhoneNumber)
	}
	if settings.Format != export.JSON || !settings.Details || settings.Interface != ModePlain {
		t.Errorf("unexpected settings: %+v", settings)
	}
	if strings.Join(settings.Datasets, ",") != "positions,transactions" {
		t.Errorf("Datasets = %v", settings.Datasets)
	}
}

func TestSetRejectsInvalidValues(t *testing.T) {
	s := openTemp(t, "")
	for name, value := range map[string]string{
		"account.phone_number": "0612345678",
		"export.format":        "xlsx",
		"export.details":       "maybe",
		"export.datasets":      "positions,stocks",
		"export.output_dir":    " ",
		"interface.mode":       "fancy",
		"no.such_key":          "x",
	} {
		if err := s.Set(name, value); err == nil {
			t.Errorf("Set(%s, %q) succeeded, want an error", name, value)
		}
	}
}

func TestDatasetsListCollapsesToAll(t *testing.T) {
	k, err := Lookup("export.datasets")
	if err != nil {
		t.Fatal(err)
	}
	all := strings.Join(datasetNames(), ",")
	if got, _ := k.Normalize(all); got != "all" {
		t.Errorf("Normalize(every dataset) = %q, want all", got)
	}
}

func TestPINLivesInKeychain(t *testing.T) {
	s := openTemp(t, "")
	if err := s.Set("account.pin", "4791"); err == nil {
		t.Fatal("storing a PIN without a phone number must fail")
	}
	if err := s.Set("account.phone_number", "+33612345678"); err != nil {
		t.Fatal(err)
	}
	if err := s.Set("account.pin", "4791"); err != nil {
		t.Fatalf("Set(pin): %v", err)
	}
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(s.Path())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "4791") {
		t.Error("the PIN must never be written to the file")
	}
	if v := s.Resolve("account.pin"); v.Value != "4791" || v.Source != SourceKeychain {
		t.Errorf("Resolve(pin) = %+v", v)
	}

	if err := s.Unset("account.pin"); err != nil {
		t.Fatal(err)
	}
	if v := s.Resolve("account.pin"); v.Source != SourceUnset {
		t.Errorf("PIN still resolved after Unset: %+v", v)
	}
}

func TestEnvironmentWins(t *testing.T) {
	s := openTemp(t, "[account]\nphone_number = +33612345678\n")
	t.Setenv(EnvPhoneNumber, "+33700000000")
	t.Setenv(EnvPIN, "9999")

	if v := s.Resolve("account.phone_number"); v.Value != "+33700000000" || v.Source != SourceEnv {
		t.Errorf("phone = %+v", v)
	}
	if v := s.Resolve("account.pin"); v.Value != "9999" || v.Source != SourceEnv {
		t.Errorf("pin = %+v", v)
	}
}

func TestSettingsReportsInvalidFileValues(t *testing.T) {
	s := openTemp(t, "[export]\nformat = xlsx\n")
	settings, err := s.Settings()
	if err == nil {
		t.Fatal("want an error for an invalid value in the file")
	}
	if settings.Format != export.CSV {
		t.Errorf("an invalid value must fall back to the default, got %q", settings.Format)
	}
}

func TestSavePreservesComments(t *testing.T) {
	s := openTemp(t, "# my notes\n[export]\nformat = csv\n")
	if err := s.Set("export.format", "json"); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(s.Path())
	if !strings.Contains(string(raw), "my notes") {
		t.Errorf("comment lost:\n%s", raw)
	}
}

func TestMigrateLegacy(t *testing.T) {
	s := openTemp(t, "")
	legacy := filepath.Join(t.TempDir(), "config.ini")
	body := "[secret]\nphone_number = +33612345678\npin = 4321\ndevice_info = abc\n\n[general]\noutput_format = json\noutput_folder = exports\nextract_details = true\n"
	if err := os.WriteFile(legacy, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	m, err := s.MigrateLegacy(legacy)
	if err != nil {
		t.Fatalf("MigrateLegacy: %v", err)
	}
	if m == nil || !m.PINStored {
		t.Fatalf("migration = %+v", m)
	}

	settings, err := s.Settings()
	if err != nil {
		t.Fatal(err)
	}
	if settings.PhoneNumber != "+33612345678" || settings.PIN != "4321" || settings.DeviceInfo != "abc" ||
		settings.Format != export.JSON || settings.OutputDir != "exports" || !settings.Details {
		t.Errorf("migrated settings = %+v", settings)
	}

	raw, _ := os.ReadFile(s.Path())
	if strings.Contains(string(raw), "4321") {
		t.Error("the migrated file must not contain the PIN")
	}
	if again, _ := s.MigrateLegacy(legacy); again != nil {
		t.Error("an existing store must not be migrated twice")
	}
}

func TestMigrateLegacyIgnoresUnrelatedFiles(t *testing.T) {
	s := openTemp(t, "")
	other := filepath.Join(t.TempDir(), "config.ini")
	if err := os.WriteFile(other, []byte("[database]\nhost = x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if m, err := s.MigrateLegacy(other); m != nil || err != nil {
		t.Errorf("MigrateLegacy = %+v, %v; want nil, nil", m, err)
	}
}
