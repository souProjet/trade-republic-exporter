package config

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/souProjet/trade-republic-exporter/internal/export"
	"github.com/souProjet/trade-republic-exporter/internal/i18n"
)

// Kind is the type of a setting's value.
type Kind int

const (
	KindString Kind = iota
	KindBool
	KindChoice
	KindList
	KindSecret
)

// Key describes one setting: where it lives, what it accepts and how it can be
// overridden. Description is in English and translated for display.
type Key struct {
	Name        string
	Description string
	Kind        Kind
	Default     string
	Choices     []string
	Env         string
	normalize   func(string) string
	validate    func(string) error
}

// Interface modes.
const (
	ModeAuto       = "auto"
	ModeFullscreen = "fullscreen"
	ModePlain      = "plain"
)

// allDatasets is the value of export.datasets that selects every dataset.
const allDatasets = "all"

// Keys is every supported setting, in display order.
var Keys = []Key{
	{
		Name:        "account.phone_number",
		Description: "Phone number in international format",
		Env:         EnvPhoneNumber,
		normalize:   func(v string) string { return strings.Join(strings.Fields(v), "") },
		validate:    validatePhone,
	},
	{
		Name:        "account.pin",
		Description: "Trade Republic PIN, kept in the system keychain",
		Kind:        KindSecret,
		Env:         EnvPIN,
		validate:    validatePIN,
	},
	{
		Name:        "account.device_info",
		Description: "Device identity, generated and saved on first login",
		Env:         EnvDeviceInfo,
	},
	{
		Name:        "export.format",
		Description: "Output format",
		Kind:        KindChoice,
		Default:     string(export.CSV),
		Choices:     []string{string(export.CSV), string(export.JSON)},
	},
	{
		Name:        "export.csv_dialect",
		Description: "CSV flavor: european (; and decimal comma) or standard (, and decimal point)",
		Kind:        KindChoice,
		Default:     export.European.Name,
		Choices:     []string{export.European.Name, export.Standard.Name},
	},
	{
		Name:        "export.output_dir",
		Description: "Destination directory, relative to the working directory unless absolute",
		Default:     "out",
		validate:    validateNotEmpty,
	},
	{
		Name:        "export.datasets",
		Description: "Datasets to export, comma separated, or all",
		Kind:        KindList,
		Default:     allDatasets,
		Choices:     datasetNames(),
		validate:    validateDatasets,
	},
	{
		Name:        "export.details",
		Description: "Fetch the detail view of every transaction (slower)",
		Kind:        KindBool,
		Default:     "false",
	},
	{
		Name:        "interface.mode",
		Description: "auto picks the full-screen interface in a terminal and plain output otherwise",
		Kind:        KindChoice,
		Default:     ModeAuto,
		Choices:     []string{ModeAuto, ModeFullscreen, ModePlain},
	},
	{
		Name:        "interface.language",
		Description: "Interface language: auto follows the system",
		Kind:        KindChoice,
		Default:     string(i18n.Auto),
		Choices:     languageNames(),
	},
}

func languageNames() []string {
	names := make([]string, len(i18n.Languages))
	for i, l := range i18n.Languages {
		names[i] = string(l)
	}
	return names
}

// Lookup finds a key by name.
func Lookup(name string) (Key, error) {
	for _, k := range Keys {
		if k.Name == name {
			return k, nil
		}
	}
	names := make([]string, len(Keys))
	for i, k := range Keys {
		names[i] = k.Name
	}
	return Key{}, errors.New(i18n.T("unknown setting %q (valid: %s)", name, strings.Join(names, ", ")))
}

// Normalize validates a value and returns it in canonical form.
func (k Key) Normalize(value string) (string, error) {
	value = strings.TrimSpace(value)
	if k.normalize != nil {
		value = k.normalize(value)
	}
	switch k.Kind {
	case KindBool:
		b, err := strconv.ParseBool(value)
		if err != nil {
			return "", k.invalid(errors.New(i18n.T("expected true or false, got %q", value)))
		}
		return strconv.FormatBool(b), nil
	case KindChoice:
		value = strings.ToLower(value)
		if !slices.Contains(k.Choices, value) {
			return "", k.invalid(errors.New(i18n.T("expected one of %s, got %q", strings.Join(k.Choices, ", "), value)))
		}
	case KindList:
		value = normalizeList(value)
	}
	if k.validate != nil {
		if err := k.validate(value); err != nil {
			return "", k.invalid(err)
		}
	}
	return value, nil
}

// invalid wraps the reason a value was rejected; errors.Unwrap returns the
// reason alone, for fields that already show the setting's name.
func (k Key) invalid(reason error) error {
	return fmt.Errorf("%s: %w", i18n.T("invalid %s", k.Name), reason)
}

func (k Key) section() string {
	section, _, _ := strings.Cut(k.Name, ".")
	return section
}

func (k Key) field() string {
	_, field, _ := strings.Cut(k.Name, ".")
	return field
}

var phonePattern = regexp.MustCompile(`^\+[1-9][0-9]{6,14}$`)

func validatePhone(v string) error {
	if !phonePattern.MatchString(strings.ReplaceAll(v, " ", "")) {
		return errors.New(i18n.T("%q is not an international phone number, for example +33612345678", v))
	}
	return nil
}

var pinPattern = regexp.MustCompile(`^[0-9]{4}$`)

func validatePIN(v string) error {
	if !pinPattern.MatchString(v) {
		return errors.New(i18n.T("the PIN must be 4 digits"))
	}
	return nil
}

func validateNotEmpty(v string) error {
	if v == "" {
		return errors.New(i18n.T("must not be empty"))
	}
	return nil
}

func validateDatasets(v string) error {
	if v == allDatasets {
		return nil
	}
	for _, name := range strings.Split(v, ",") {
		if _, ok := export.LookupDataset(name); !ok {
			return errors.New(i18n.T("unknown dataset %q (valid: %s, or all)", name, strings.Join(datasetNames(), ", ")))
		}
	}
	return nil
}

// normalizeList lowercases, trims and deduplicates a comma-separated list, and
// collapses a list naming every dataset to "all".
func normalizeList(v string) string {
	var items []string
	for _, item := range strings.Split(strings.ToLower(v), ",") {
		item = strings.TrimSpace(item)
		if item != "" && !slices.Contains(items, item) {
			items = append(items, item)
		}
	}
	if len(items) == 0 || slices.Contains(items, allDatasets) || len(items) == len(export.Datasets) {
		return allDatasets
	}
	return strings.Join(items, ",")
}

func datasetNames() []string {
	names := make([]string, len(export.Datasets))
	for i, d := range export.Datasets {
		names[i] = d.Name
	}
	return names
}
