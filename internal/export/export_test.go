package export

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseFormat(t *testing.T) {
	for _, tc := range []struct {
		in      string
		want    Format
		wantErr bool
	}{
		{in: "csv", want: CSV},
		{in: " JSON ", want: JSON},
		{in: "xlsx", wantErr: true},
		{in: "", wantErr: true},
	} {
		got, err := ParseFormat(tc.in)
		if (err != nil) != tc.wantErr {
			t.Fatalf("ParseFormat(%q) error = %v, wantErr = %v", tc.in, err, tc.wantErr)
		}
		if err == nil && got != tc.want {
			t.Errorf("ParseFormat(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestSaveCSV(t *testing.T) {
	dir := t.TempDir()
	rows := []map[string]any{
		{
			"id":        "tx-1",
			"timestamp": "2026-03-04T10:11:12.000+0000",
			"amount":    map[string]any{"value": 12.5, "currency": "EUR"},
			"tags":      []any{"a", "b"},
		},
		{"id": "tx-2", "note": nil},
	}

	path, err := Save(dir, CSV, "transactions", rows)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if want := filepath.Join(dir, "transactions.csv"); path != want {
		t.Errorf("path = %q, want %q", path, want)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(raw)
	if !strings.HasPrefix(content, "\xef\xbb\xbf") {
		t.Error("missing UTF-8 BOM")
	}

	lines := strings.Split(strings.TrimRight(strings.TrimPrefix(content, "\xef\xbb\xbf"), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3: %q", len(lines), lines)
	}
	if got, want := strings.TrimRight(lines[0], "\r"), "amount.currency;amount.value;id;note;tags;timestamp"; got != want {
		t.Errorf("header = %q, want %q", got, want)
	}
	wantRow := `EUR;12,5;tx-1;;"[""a"",""b""]";` + expectedTimestamp(t, "2026-03-04T10:11:12.000+0000")
	if got := strings.TrimRight(lines[1], "\r"); got != wantRow {
		t.Errorf("row = %q, want %q", got, wantRow)
	}
}

// expectedTimestamp renders the reference instant the way the CSV writer does,
// so the test does not depend on the machine's time zone.
func expectedTimestamp(t *testing.T, value string) string {
	t.Helper()
	parsed, ok := parseTimestamp(value)
	if !ok {
		t.Fatalf("parseTimestamp(%q) failed", value)
	}
	return parsed.Local().Format("02/01/2006 15:04")
}

func TestParseTimestamp(t *testing.T) {
	for _, value := range []string{
		"2026-03-04T10:11:12.000+0000",
		"2026-03-04T10:11:12Z",
		"2026-03-04T10:11:12+0100",
		"2026-03-04",
		"1772619072000",
	} {
		if _, ok := parseTimestamp(value); !ok {
			t.Errorf("parseTimestamp(%q) = false, want true", value)
		}
	}
	if _, ok := parseTimestamp("not a date"); ok {
		t.Error(`parseTimestamp("not a date") = true, want false`)
	}
}

func TestSaveJSONEmpty(t *testing.T) {
	dir := t.TempDir()
	path, err := Save(dir, JSON, "cash", nil)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var decoded []map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if len(decoded) != 0 {
		t.Errorf("decoded %d rows, want 0", len(decoded))
	}
}

func TestSaveCreatesDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "out")
	if _, err := Save(dir, JSON, "accounts", []map[string]any{{"id": 1}}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "accounts.json")); err != nil {
		t.Fatal(err)
	}
}
