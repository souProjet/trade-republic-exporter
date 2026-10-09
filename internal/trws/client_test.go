package trws

import (
	"encoding/json"
	"testing"
)

func TestParseFrame(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want frame
	}{
		{raw: `7 A {"items":[]}`, want: frame{id: 7, code: "A", payload: `{"items":[]}`}},
		{raw: "7 C", want: frame{id: 7, code: "C"}},
		{raw: `12 E {"errors":[{"errorCode":"NO_DATA"}]}`, want: frame{id: 12, code: "E", payload: `{"errors":[{"errorCode":"NO_DATA"}]}`}},
		{raw: "connect", want: frame{id: -1, code: "connect"}},
		{raw: `3 A {"a":1} {"b":2}`, want: frame{id: 3, code: "A", payload: `{"a":1} {"b":2}`}},
	} {
		if got := parseFrame(tc.raw); got != tc.want {
			t.Errorf("parseFrame(%q) = %+v, want %+v", tc.raw, got, tc.want)
		}
	}
}

func TestRows(t *testing.T) {
	for _, tc := range []struct {
		name    string
		payload string
		want    int
	}{
		{name: "array", payload: `[{"a":1},{"a":2}]`, want: 2},
		{name: "items", payload: `{"items":[{"a":1}],"cursors":{"after":"x"}}`, want: 1},
		{name: "accounts", payload: `{"accounts":[{"productType":"DEFAULT"},{"productType":"TAX_WRAPPER"}]}`, want: 2},
		{name: "single object", payload: `{"amount":12.5}`, want: 1},
		{name: "null", payload: `null`, want: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rows, err := Rows(json.RawMessage(tc.payload))
			if err != nil {
				t.Fatalf("Rows: %v", err)
			}
			if len(rows) != tc.want {
				t.Errorf("got %d rows, want %d", len(rows), tc.want)
			}
		})
	}

	if _, err := Rows(json.RawMessage(`"oops"`)); err == nil {
		t.Error("want an error for a scalar payload")
	}
}

func TestExpandCategories(t *testing.T) {
	var payload struct {
		Categories []map[string]any `json:"categories"`
	}
	raw := `{"categories":[
		{"categoryName":"Stocks","positions":[{"instrumentId":"US0378331005"},{"instrumentId":"FR0000120271"}]},
		{"categoryName":"Crypto","positions":[{"instrumentId":"XF000BTC0017"}]},
		{"categoryName":"Empty","positions":[]}
	]}`
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatal(err)
	}

	rows := expandCategories(payload.Categories)
	if len(rows) != 4 {
		t.Fatalf("got %d rows, want 4", len(rows))
	}
	if got := rows[0]["category.categoryName"]; got != "Stocks" {
		t.Errorf("category.categoryName = %v, want Stocks", got)
	}
	if got := rows[2]["instrumentId"]; got != "XF000BTC0017" {
		t.Errorf("crypto position missing, got %v", got)
	}
	if _, ok := rows[0]["positions"]; ok {
		t.Error("the positions slice must not be kept on the row")
	}
	if got := rows[3]["categoryName"]; got != "Empty" {
		t.Errorf("an empty category must be kept as one row, got %v", got)
	}
}

func TestAccountLabel(t *testing.T) {
	for _, tc := range []struct{ productType, want string }{
		{"DEFAULT", "Securities account"},
		{"TAX_WRAPPER", "PEA"},
		{"", "unknown"},
		{"FUTURE_PRODUCT", "FUTURE_PRODUCT"},
	} {
		if got := (Account{ProductType: tc.productType}).Label(); got != tc.want {
			t.Errorf("Label(%q) = %q, want %q", tc.productType, got, tc.want)
		}
	}
}

func TestAPIError(t *testing.T) {
	err := &APIError{Subscription: "cash", Payload: `{"errors":[]}`}
	if got := err.Error(); got == "" || !contains(got, "cash") {
		t.Errorf("Error() = %q, want it to name the subscription", got)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// TestParseDetailsMixedSections uses the section shapes the live API returns:
// an object-shaped header, a table of labeled rows, a nested text wrapper and
// a documents section whose detail is a bare string. An earlier version
// assumed every section carried a list and failed on the first event.
func TestParseDetailsMixedSections(t *testing.T) {
	raw := json.RawMessage(`{
		"id": "tx-1",
		"sections": [
			{"title": "Overview", "type": "header", "data": {"icon": "logos/US0378331005", "status": "executed", "timestamp": "2026-03-04T10:11:12.000+0000"}},
			{"title": "Transaction", "type": "table", "data": [
				{"title": "Shares", "detail": {"type": "text", "text": "1.5"}},
				{"title": "Share price", "detail": {"type": "text", "text": "172,40 EUR"}},
				{"title": "Fee", "detail": {"type": "badge", "text": {"type": "text", "text": "Free"}}},
				{"title": "Venue", "detail": {"type": "iconWithText", "icon": "lsx", "text": "LS Exchange"}},
				{"title": "Empty", "detail": {}},
				{"title": "", "detail": {"text": "no title"}}
			]},
			{"title": "Documents", "type": "documents", "data": [
				{"title": "Settlement", "detail": "4 Mar 2026"}
			]},
			{"title": "Banner", "type": "banner", "data": {"text": "ignored"}}
		]
	}`)

	details, err := parseDetails(raw)
	if err != nil {
		t.Fatalf("parseDetails: %v", err)
	}

	want := map[string]any{
		"detail.Transaction.Shares":      "1.5",
		"detail.Transaction.Share price": "172,40 EUR",
		"detail.Transaction.Fee":         "Free",
		"detail.Transaction.Venue":       "LS Exchange",
		"detail.Documents.Settlement":    "4 Mar 2026",
	}
	for key, value := range want {
		if details[key] != value {
			t.Errorf("details[%q] = %v, want %v", key, details[key], value)
		}
	}
	if len(details) != len(want) {
		t.Errorf("got %d details, want %d: %v", len(details), len(want), details)
	}
}

func TestParseDetailsKeepsUnknownShapes(t *testing.T) {
	raw := json.RawMessage(`{"sections":[{"title":"Transaction","type":"table","data":[
		{"title":"Breakdown","detail":{"type":"chart","series":[1,2]}},
		{"title":"Tags","detail":["a","b"]}
	]}]}`)

	details, err := parseDetails(raw)
	if err != nil {
		t.Fatalf("parseDetails: %v", err)
	}
	if got := details["detail.Transaction.Breakdown"]; got != `{"type":"chart","series":[1,2]}` {
		t.Errorf("an unknown shape must be kept verbatim, got %v", got)
	}
	if got := details["detail.Transaction.Tags"]; got != "a, b" {
		t.Errorf("details[Tags] = %v, want %q", got, "a, b")
	}
}

func TestParseDetailsRejectsGarbage(t *testing.T) {
	if _, err := parseDetails(json.RawMessage(`not json`)); err == nil {
		t.Error("want an error for an undecodable payload")
	}
}

func TestPlainText(t *testing.T) {
	for _, tc := range []struct{ payload, want string }{
		{`"  spaced  "`, "spaced"},
		{`{"text":"direct"}`, "direct"},
		{`{"displayValue":"fallback"}`, "fallback"},
		{`{"text":{"text":{"text":"deep"}}}`, "deep"},
		{`12.5`, "12.5"},
		{`true`, "true"},
		{`null`, ""},
		{`{}`, ""},
		{`[]`, ""},
	} {
		if got := plainText(json.RawMessage(tc.payload)); got != tc.want {
			t.Errorf("plainText(%s) = %q, want %q", tc.payload, got, tc.want)
		}
	}
}

func TestSanitizeKey(t *testing.T) {
	if got := detailKey("Transaction", "table", "P.R.U."); got != "detail.Transaction.P R U" {
		t.Errorf("detailKey = %q", got)
	}
	if got := detailKey("", "documents", "Settlement"); got != "detail.documents.Settlement" {
		t.Errorf("detailKey without a title = %q", got)
	}
	if got := detailKey("", "", "Fee"); got != "detail.Fee" {
		t.Errorf("detailKey without a section = %q", got)
	}
}
