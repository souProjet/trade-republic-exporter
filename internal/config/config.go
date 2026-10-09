// Package config stores the exporter settings in an INI file under the user's
// configuration directory, the PIN in the system keychain, and resolves the
// effective value of every setting from the environment, the file, the
// keychain and the defaults.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"gopkg.in/ini.v1"

	"github.com/souProjet/trade-republic-exporter/internal/export"
)

// Environment variables override the file and the keychain.
const (
	EnvPhoneNumber = "TR_PHONE_NUMBER"
	EnvPIN         = "TR_PIN"
	EnvWAFToken    = "TR_WAF_TOKEN"
	EnvDeviceInfo  = "TR_DEVICE_INFO"
)

const (
	appDir   = "trade-republic-exporter"
	fileName = "config.ini"
)

// Source tells where an effective value came from.
type Source string

const (
	SourceUnset    Source = "unset"
	SourceDefault  Source = "default"
	SourceFile     Source = "file"
	SourceEnv      Source = "env"
	SourceKeychain Source = "keychain"
)

// DefaultPath is $XDG_CONFIG_HOME/trade-republic-exporter/config.ini, falling
// back to ~/.config on Unix-like systems, macOS included, and to the roaming
// application data directory on Windows.
func DefaultPath() (string, error) {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, appDir, fileName), nil
	}
	if runtime.GOOS == "windows" {
		dir, err := os.UserConfigDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(dir, appDir, fileName), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", appDir, fileName), nil
}

// Store is the configuration file. Comments and unknown keys survive a save.
type Store struct {
	path   string
	file   *ini.File
	exists bool
}

// Open loads the file at path. A missing file yields an empty store.
func Open(path string) (*Store, error) {
	_, err := os.Stat(path)
	exists := err == nil
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	file := ini.Empty()
	if exists {
		if file, err = ini.Load(path); err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
	}
	return &Store{path: path, file: file, exists: exists}, nil
}

// Path is the file location.
func (s *Store) Path() string { return s.path }

// Exists reports whether the file is on disk.
func (s *Store) Exists() bool { return s.exists }

// fileValue is the raw value stored in the file.
func (s *Store) fileValue(k Key) string {
	if !s.file.HasSection(k.section()) {
		return ""
	}
	return strings.TrimSpace(s.file.Section(k.section()).Key(k.field()).String())
}

// Set validates and writes a value. The PIN goes to the keychain, never to the
// file. Call Save to persist file values.
func (s *Store) Set(name, value string) error {
	k, err := Lookup(name)
	if err != nil {
		return err
	}
	value, err = k.Normalize(value)
	if err != nil {
		return err
	}
	if k.Kind == KindSecret {
		return SavePIN(s.Resolve("account.phone_number").Value, value)
	}
	s.file.Section(k.section()).Key(k.field()).SetValue(value)
	return nil
}

// Unset removes a value so its default applies again.
func (s *Store) Unset(name string) error {
	k, err := Lookup(name)
	if err != nil {
		return err
	}
	if k.Kind == KindSecret {
		if err := DeletePIN(s.Resolve("account.phone_number").Value); err != nil {
			return err
		}
	}
	if s.file.HasSection(k.section()) {
		s.file.Section(k.section()).DeleteKey(k.field())
	}
	return nil
}

// Save writes the file atomically with owner-only permissions.
func (s *Store) Save() error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, ".config-*.ini")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())

	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := s.file.WriteTo(tmp); err != nil {
		tmp.Close()
		return fmt.Errorf("write %s: %w", s.path, err)
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), s.path); err != nil {
		return fmt.Errorf("write %s: %w", s.path, err)
	}
	s.exists = true
	return nil
}

// Value is the effective value of a setting and where it came from.
type Value struct {
	Key    Key
	Value  string
	Source Source
}

// Resolve returns the effective value of a setting: the environment wins,
// then the file, then the keychain for the PIN, then the default.
func (s *Store) Resolve(name string) Value {
	k, err := Lookup(name)
	if err != nil {
		return Value{Source: SourceUnset}
	}
	if k.Env != "" {
		if v := strings.TrimSpace(os.Getenv(k.Env)); v != "" {
			return Value{Key: k, Value: v, Source: SourceEnv}
		}
	}
	if k.Kind == KindSecret {
		// A PIN left in the file by hand or by v0.1 still works, but the
		// keychain takes precedence.
		pin, err := LoadPIN(s.Resolve("account.phone_number").Value)
		if err == nil && pin != "" {
			return Value{Key: k, Value: pin, Source: SourceKeychain}
		}
	}
	if v := s.fileValue(k); v != "" {
		return Value{Key: k, Value: v, Source: SourceFile}
	}
	if k.Default != "" {
		return Value{Key: k, Value: k.Default, Source: SourceDefault}
	}
	return Value{Key: k, Source: SourceUnset}
}

// Settings is the typed, validated configuration of a run.
type Settings struct {
	PhoneNumber string
	PIN         string
	DeviceInfo  string
	WAFToken    string
	Format      export.Format
	Dialect     export.Dialect
	OutputDir   string
	Details     bool
	// Datasets is empty when every dataset is selected.
	Datasets  []string
	Interface string
}

// Settings resolves and validates every setting. Missing credentials are not
// an error here: the caller decides whether to prompt or fail.
func (s *Store) Settings() (Settings, error) {
	var out Settings
	var errs []error

	value := func(name string) string {
		v := s.Resolve(name)
		if v.Source == SourceUnset || v.Source == SourceDefault {
			return v.Value
		}
		normalized, err := v.Key.Normalize(v.Value)
		if err != nil {
			errs = append(errs, fmt.Errorf("%w (from %s)", err, v.Source))
			return v.Key.Default
		}
		return normalized
	}

	out.PhoneNumber = value("account.phone_number")
	out.PIN = value("account.pin")
	out.DeviceInfo = value("account.device_info")
	out.WAFToken = strings.TrimSpace(os.Getenv(EnvWAFToken))
	out.Format = export.Format(value("export.format"))
	out.Dialect, _ = export.ParseDialect(value("export.csv_dialect"))
	out.OutputDir = expandHome(value("export.output_dir"))
	out.Details, _ = strconv.ParseBool(value("export.details"))
	if list := value("export.datasets"); list != allDatasets {
		out.Datasets = strings.Split(list, ",")
	}
	out.Interface = value("interface.mode")

	return out, errors.Join(errs...)
}

func expandHome(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(path, "~"))
		}
	}
	return path
}

// Migration is the outcome of importing a v0.1 configuration file.
type Migration struct {
	From      string
	PINStored bool
	PINError  error
}

// MigrateLegacy imports a v0.1 config.ini, with its [secret] and [general]
// sections, into the store and moves the PIN to the keychain. The legacy file
// is left untouched. It returns nil when there is nothing to import.
func (s *Store) MigrateLegacy(legacyPath string) (*Migration, error) {
	if s.exists {
		return nil, nil
	}
	if _, err := os.Stat(legacyPath); err != nil {
		return nil, nil
	}
	legacy, err := ini.Load(legacyPath)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", legacyPath, err)
	}
	if !legacy.HasSection("secret") && !legacy.HasSection("general") {
		return nil, nil
	}

	secret, general := legacy.Section("secret"), legacy.Section("general")
	mapping := map[string]string{
		"account.phone_number": secret.Key("phone_number").String(),
		"account.device_info":  secret.Key("device_info").String(),
		"export.format":        general.Key("output_format").String(),
		"export.output_dir":    general.Key("output_folder").String(),
		"export.details":       general.Key("extract_details").String(),
	}
	for _, k := range Keys {
		v := strings.TrimSpace(mapping[k.Name])
		if v == "" {
			continue
		}
		if err := s.Set(k.Name, v); err != nil {
			return nil, fmt.Errorf("migrate %s: %w", legacyPath, err)
		}
	}
	if err := s.Save(); err != nil {
		return nil, err
	}

	m := &Migration{From: legacyPath}
	if pin := strings.TrimSpace(secret.Key("pin").String()); pin != "" {
		m.PINError = s.Set("account.pin", pin)
		m.PINStored = m.PINError == nil
	}
	return m, nil
}
