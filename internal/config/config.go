// Package config loads the exporter settings from an INI file and the
// environment.
package config

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"gopkg.in/ini.v1"

	"github.com/souProjet/trade-republic-exporter/internal/export"
)

// Environment variables override the file, so credentials can stay out of it.
const (
	EnvPhoneNumber = "TR_PHONE_NUMBER"
	EnvPIN         = "TR_PIN"
	EnvWAFToken    = "TR_WAF_TOKEN"
	EnvDeviceInfo  = "TR_DEVICE_INFO"
)

// Config is the resolved configuration of one run.
type Config struct {
	PhoneNumber    string
	PIN            string
	WAFToken       string
	DeviceInfo     string
	Format         export.Format
	OutputDir      string
	ExtractDetails bool
}

// Load reads path, then applies environment overrides. A missing file is not an
// error as long as the credentials come from the environment.
func Load(path string) (*Config, error) {
	c := &Config{Format: export.CSV, OutputDir: "out"}

	switch file, err := ini.Load(path); {
	case err == nil:
		secret, general := file.Section("secret"), file.Section("general")
		c.PhoneNumber = strings.TrimSpace(secret.Key("phone_number").String())
		c.PIN = strings.TrimSpace(secret.Key("pin").String())
		c.WAFToken = strings.TrimSpace(secret.Key("waf_token").String())
		c.DeviceInfo = strings.TrimSpace(secret.Key("device_info").String())
		c.OutputDir = general.Key("output_folder").MustString(c.OutputDir)
		c.ExtractDetails = general.Key("extract_details").MustBool(false)

		if raw := general.Key("output_format").String(); raw != "" {
			format, err := export.ParseFormat(raw)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", path, err)
			}
			c.Format = format
		}
	case errors.Is(err, os.ErrNotExist):
		// Tolerated: the environment may carry everything.
	default:
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	override(&c.PhoneNumber, EnvPhoneNumber)
	override(&c.PIN, EnvPIN)
	override(&c.WAFToken, EnvWAFToken)
	override(&c.DeviceInfo, EnvDeviceInfo)
	return c, nil
}

func override(field *string, env string) {
	if v := strings.TrimSpace(os.Getenv(env)); v != "" {
		*field = v
	}
}

// Validate reports whether the configuration can run, and is called after
// command-line flags have been applied.
func (c *Config) Validate() error {
	if c.PhoneNumber == "" || c.PIN == "" {
		return fmt.Errorf("missing credentials: set phone_number and pin in the config file, or %s and %s in the environment",
			EnvPhoneNumber, EnvPIN)
	}
	if !strings.HasPrefix(c.PhoneNumber, "+") {
		return fmt.Errorf("phone_number must be in international format, for example +33612345678")
	}
	if _, err := export.ParseFormat(string(c.Format)); err != nil {
		return err
	}
	if c.OutputDir == "" {
		return errors.New("output folder must not be empty")
	}
	return nil
}
