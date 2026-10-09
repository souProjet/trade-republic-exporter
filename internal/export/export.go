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
// The directory is created if it does not exist.
func Save(dir string, format Format, basename string, rows []map[string]any) (string, error) {
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
		err = writeCSV(f, rows)
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

// writeCSV flattens nested rows into a single header row. The dialect targets
// spreadsheet software in European locales: UTF-8 BOM, semicolon separator and
// comma decimal separator.
func writeCSV(f *os.File, rows []map[string]any) error {
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

	// UTF-8 BOM so spreadsheet software detects the encoding.
	if _, err := f.Write([]byte{0xEF, 0xBB, 0xBF}); err != nil {
		return err
	}

	w := csv.NewWriter(f)
	w.Comma = ';'
	if err := w.Write(header); err != nil {
		return err
	}
	record := make([]string, len(header))
	for _, row := range flat {
		for i, k := range header {
			record[i] = formatValue(k, row[k])
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

// formatValue renders a cell for the European CSV dialect: local date and time
// for timestamps, comma decimal separator for monetary values.
func formatValue(key, value string) string {
	switch {
	case value == "":
		return ""
	case key == "timestamp" || strings.HasSuffix(key, "Timestamp") || strings.HasSuffix(key, ".timestamp"):
		if t, ok := parseTimestamp(value); ok {
			return t.Local().Format("02/01/2006 15:04")
		}
	case strings.HasSuffix(key, ".value"), strings.HasSuffix(key, ".fractionDigits"):
		if f, err := strconv.ParseFloat(value, 64); err == nil {
			return strings.ReplaceAll(strconv.FormatFloat(f, 'f', -1, 64), ".", ",")
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
