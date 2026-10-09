package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/souProjet/trade-republic-exporter/internal/export"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.ini")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadFile(t *testing.T) {
	path := writeConfig(t, `
[secret]
phone_number = +33612345678
pin = 1234

[general]
output_format = json
output_folder = exports
extract_details = true
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.PhoneNumber != "+33612345678" || cfg.PIN != "1234" {
		t.Errorf("credentials = %q/%q", cfg.PhoneNumber, cfg.PIN)
	}
	if cfg.Format != export.JSON {
		t.Errorf("Format = %q, want json", cfg.Format)
	}
	if cfg.OutputDir != "exports" || !cfg.ExtractDetails {
		t.Errorf("OutputDir = %q, ExtractDetails = %v", cfg.OutputDir, cfg.ExtractDetails)
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate: %v", err)
	}
}

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load(writeConfig(t, "[secret]\nphone_number = +33612345678\npin = 1234\n"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Format != export.CSV || cfg.OutputDir != "out" || cfg.ExtractDetails {
		t.Errorf("unexpected defaults: %+v", cfg)
	}
}

func TestLoadEnvOverridesFile(t *testing.T) {
	t.Setenv(EnvPhoneNumber, "+33700000000")
	t.Setenv(EnvPIN, "9999")

	cfg, err := Load(writeConfig(t, "[secret]\nphone_number = +33612345678\npin = 1234\n"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.PhoneNumber != "+33700000000" || cfg.PIN != "9999" {
		t.Errorf("environment did not win: %q/%q", cfg.PhoneNumber, cfg.PIN)
	}
}

func TestLoadMissingFileWithEnv(t *testing.T) {
	t.Setenv(EnvPhoneNumber, "+33700000000")
	t.Setenv(EnvPIN, "9999")

	cfg, err := Load(filepath.Join(t.TempDir(), "absent.ini"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate: %v", err)
	}
}

func TestLoadInvalidFormat(t *testing.T) {
	if _, err := Load(writeConfig(t, "[general]\noutput_format = xlsx\n")); err == nil {
		t.Error("want an error for an unsupported output format")
	}
}

func TestValidate(t *testing.T) {
	for name, cfg := range map[string]*Config{
		"no credentials": {Format: export.CSV, OutputDir: "out"},
		"local number":   {PhoneNumber: "0612345678", PIN: "1234", Format: export.CSV, OutputDir: "out"},
		"no output dir":  {PhoneNumber: "+33612345678", PIN: "1234", Format: export.CSV},
		"bad format":     {PhoneNumber: "+33612345678", PIN: "1234", Format: "xml", OutputDir: "out"},
	} {
		if err := cfg.Validate(); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}
