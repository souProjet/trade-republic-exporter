// Package export writes timeline rows to CSV or JSON files.
package export

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Format is a supported output format.
type Format string

const (
	CSV  Format = "csv"
	JSON Format = "json"
)

// Dialect controls how CSV cells are written.
type Dialect struct {
	Name         string
	Comma        rune
	DecimalComma bool
	BOM          bool
	TimeLayout   string
}

var (
	// European suits spreadsheet software in most European locales.
	European = Dialect{Name: "european", Comma: ';', DecimalComma: true, BOM: true, TimeLayout: "02/01/2006 15:04"}
	// Standard is RFC 4180 CSV with ISO 8601 timestamps, for tools and
	// spreadsheets in English locales.
	Standard = Dialect{Name: "standard", Comma: ',', TimeLayout: time.RFC3339}
)

// Dialects lists the supported CSV dialects.
var Dialects = []Dialect{European, Standard}

// ParseDialect validates a user-supplied dialect name.
func ParseDialect(s string) (Dialect, error) {
	name := strings.ToLower(strings.TrimSpace(s))
	for _, d := range Dialects {
		if d.Name == name {
			return d, nil
		}
	}
	return Dialect{}, fmt.Errorf("invalid CSV dialect %q (want %q or %q)", s, European.Name, Standard.Name)
}

// ParseFormat validates a user-supplied format name.
func ParseFormat(s string) (Format, error) {
	switch f := Format(strings.ToLower(strings.TrimSpace(s))); f {
	case CSV, JSON:
		return f, nil
	default:
		return "", fmt.Errorf("invalid output format %q (want %q or %q)", s, CSV, JSON)
	}
}

// Save writes rows to dir/basename.<format> and returns the path it wrote.
// The directory is created if it does not exist. The dialect only applies to
// CSV.
func Save(dir string, format Format, dialect Dialect, basename string, rows []map[string]any) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create output directory: %w", err)
	}
	path := filepath.Join(dir, basename+"."+string(format))

	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return "", err
	}
	defer f.Close()

	if format == JSON {
		err = writeJSON(f, rows)
	} else {
		err = writeCSV(f, dialect, rows)
	}
	if err != nil {
		return "", fmt.Errorf("write %s: %w", path, err)
	}
	return path, f.Close()
}

func writeJSON(f *os.File, rows []map[string]any) error {
	if rows == nil {
		rows = []map[string]any{}
	}
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(rows)
}

// writeCSV flattens nested rows into a single header row, sorted so files
// from different runs line up.
func writeCSV(f *os.File, dialect Dialect, rows []map[string]any) error {
	flat := make([]map[string]string, 0, len(rows))
	columns := make(map[string]struct{})
	for _, row := range rows {
		r := flatten(row, "")
		for k := range r {
			columns[k] = struct{}{}
		}
		flat = append(flat, r)
	}

	header := make([]string, 0, len(columns))
	for k := range columns {
		header = append(header, k)
	}
	sort.Strings(header)

	if dialect.BOM {
		// UTF-8 BOM so spreadsheet software detects the encoding.
		if _, err := f.Write([]byte{0xEF, 0xBB, 0xBF}); err != nil {
			return err
		}
	}

	w := csv.NewWriter(f)
	w.Comma = dialect.Comma
	if err := w.Write(header); err != nil {
		return err
	}
	record := make([]string, len(header))
	for _, row := range flat {
		for i, k := range header {
			record[i] = formatValue(dialect, k, row[k])
		}
		if err := w.Write(record); err != nil {
			return err
		}
	}
	w.Flush()
	return w.Error()
}

// flatten turns a nested value into dotted-key string pairs. Slices are kept as
// compact JSON because their length varies between rows.
func flatten(v any, prefix string) map[string]string {
	out := make(map[string]string)
	switch t := v.(type) {
	case map[string]any:
		for k, val := range t {
			key := k
			if prefix != "" {
				key = prefix + "." + k
			}
			for sk, sv := range flatten(val, key) {
				out[sk] = sv
			}
		}
	case []any:
		b, err := json.Marshal(t)
		if err != nil {
			b = []byte("[]")
		}
		out[prefix] = string(b)
	case nil:
		if prefix != "" {
			out[prefix] = ""
		}
	default:
		out[prefix] = fmt.Sprint(t)
	}
	return out
}

// timestampLayouts covers the shapes Trade Republic returns, including the
// numeric offset without a colon that RFC 3339 does not accept.
var timestampLayouts = []string{
	time.RFC3339Nano,
	"2006-01-02T15:04:05.999999999-0700",
	"2006-01-02T15:04:05-0700",
	"2006-01-02",
}

// formatValue renders a cell for the dialect: timestamps in its layout and
// local time, monetary values with its decimal separator.
func formatValue(dialect Dialect, key, value string) string {
	switch {
	case value == "":
		return ""
	case key == "timestamp" || strings.HasSuffix(key, "Timestamp") || strings.HasSuffix(key, ".timestamp"):
		if t, ok := parseTimestamp(value); ok {
			return t.Local().Format(dialect.TimeLayout)
		}
	case strings.HasSuffix(key, ".value"), strings.HasSuffix(key, ".fractionDigits"):
		if f, err := strconv.ParseFloat(value, 64); err == nil {
			s := strconv.FormatFloat(f, 'f', -1, 64)
			if dialect.DecimalComma {
				s = strings.ReplaceAll(s, ".", ",")
			}
			return s
		}
	}
	return value
}

func parseTimestamp(value string) (time.Time, bool) {
	for _, layout := range timestampLayouts {
		if t, err := time.Parse(layout, value); err == nil {
			return t, true
		}
	}
	// Some payloads carry Unix milliseconds instead of a date string.
	if ms, err := strconv.ParseInt(value, 10, 64); err == nil && ms > 1e11 {
		return time.UnixMilli(ms), true
	}
	return time.Time{}, false
}

// Dataset is one exportable file. Its name is the file's base name and the
// identifier used in the configuration.
type Dataset struct {
	Name        string
	Label       string
	Description string
}

// Datasets is the catalog of exportable files, in export order.
var Datasets = []Dataset{
	{Name: "accounts", Label: "Accounts", Description: "Account pairs: securities account, PEA, cash accounts"},
	{Name: "positions", Label: "Positions", Description: "Holdings of every account, crypto included"},
	{Name: "cash", Label: "Cash balances", Description: "Cash balance of each account"},
	{Name: "available_cash", Label: "Available cash", Description: "Cash available for trading"},
	{Name: "transactions", Label: "Transactions", Description: "Full transaction history"},
	{Name: "activity_log", Label: "Activity log", Description: "Logins, documents, account changes"},
	{Name: "savings_plans", Label: "Savings plans", Description: "Recurring investment plans"},
	{Name: "orders", Label: "Open orders", Description: "Orders not yet executed"},
}

// LookupDataset finds a dataset by name.
func LookupDataset(name string) (Dataset, bool) {
	for _, d := range Datasets {
		if d.Name == name {
			return d, true
		}
	}
	return Dataset{}, false
}
