// Package trws speaks the Trade Republic WebSocket protocol used by the web
// app: text frames of the form "<id> <code> <payload>", where the code is A
// (answer), C (complete), D (delta) or E (error).
package trws

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

const (
	endpoint      = "wss://api.traderepublic.com"
	clientID      = "app.traderepublic.com"
	clientVersion = "3.151.3"
	locale        = "fr"
	readTimeout   = 30 * time.Second
	writeTimeout  = 10 * time.Second
)

// APIError is an error frame returned by Trade Republic for a subscription.
// It means the request was rejected, never that the result set is empty.
type APIError struct {
	Subscription string
	Payload      string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("subscription %q rejected by Trade Republic: %s", e.Subscription, e.Payload)
}

// Client is a single multiplexed connection. It is not safe for concurrent use.
type Client struct {
	conn    *websocket.Conn
	session string
	lastID  int
}

// Dial opens the WebSocket connection and performs the protocol handshake.
// session is the value of the tr_session cookie obtained by logging in.
func Dial(ctx context.Context, session string) (*Client, error) {
	conn, resp, err := websocket.DefaultDialer.DialContext(ctx, endpoint, nil)
	if err != nil {
		if resp != nil {
			return nil, fmt.Errorf("dial %s: %w (HTTP %d)", endpoint, err, resp.StatusCode)
		}
		return nil, fmt.Errorf("dial %s: %w", endpoint, err)
	}

	c := &Client{conn: conn, session: session}
	hello, err := json.Marshal(map[string]string{
		"locale":          locale,
		"platformId":      "webtrading",
		"platformVersion": "safari - 18.3.0",
		"clientId":        clientID,
		"clientVersion":   clientVersion,
	})
	if err != nil {
		conn.Close()
		return nil, err
	}
	if err := c.write("connect 31 " + string(hello)); err != nil {
		conn.Close()
		return nil, fmt.Errorf("handshake: %w", err)
	}
	if _, err := c.readRaw(ctx); err != nil {
		conn.Close()
		return nil, fmt.Errorf("handshake: %w", err)
	}
	return c, nil
}

// Close releases the connection.
func (c *Client) Close() error {
	if c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

func (c *Client) write(msg string) error {
	if err := c.conn.SetWriteDeadline(time.Now().Add(writeTimeout)); err != nil {
		return err
	}
	return c.conn.WriteMessage(websocket.TextMessage, []byte(msg))
}

func (c *Client) readRaw(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	deadline := time.Now().Add(readTimeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if err := c.conn.SetReadDeadline(deadline); err != nil {
		return "", err
	}
	_, data, err := c.conn.ReadMessage()
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// frame is one decoded protocol message.
type frame struct {
	id      int
	code    string
	payload string
}

// parseFrame decodes "<id> <code> [payload]". The handshake reply and other
// non-numeric messages yield an id of -1.
func parseFrame(raw string) frame {
	parts := strings.SplitN(strings.TrimSpace(raw), " ", 3)
	id, err := strconv.Atoi(parts[0])
	if err != nil {
		return frame{id: -1, code: parts[0]}
	}
	f := frame{id: id}
	if len(parts) > 1 {
		f.code = parts[1]
	}
	if len(parts) > 2 {
		f.payload = strings.TrimSpace(parts[2])
	}
	return f
}

// request subscribes, waits for the first answer, then unsubscribes. Frames
// belonging to earlier subscriptions are discarded.
func (c *Client) request(ctx context.Context, sub string, params map[string]any) (json.RawMessage, error) {
	payload := map[string]any{"type": sub, "token": c.session}
	maps.Copy(payload, params)

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	c.lastID++
	id := c.lastID
	if err := c.write(fmt.Sprintf("sub %d %s", id, body)); err != nil {
		return nil, fmt.Errorf("subscribe %s: %w", sub, err)
	}
	defer func() {
		// Best effort: the server stops publishing, and any frame still in
		// flight is skipped by the id check above.
		_ = c.write(fmt.Sprintf("unsub %d", id))
	}()

	for {
		raw, err := c.readRaw(ctx)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", sub, err)
		}
		f := parseFrame(raw)
		if f.id != id {
			continue
		}
		switch f.code {
		case "A", "D":
			return json.RawMessage(f.payload), nil
		case "C":
			return json.RawMessage("null"), nil
		case "E":
			return nil, &APIError{Subscription: sub, Payload: truncate(f.payload, 200)}
		default:
			return nil, fmt.Errorf("unexpected %s frame %q for %s", f.code, f.payload, sub)
		}
	}
}

// Account is one securities and cash account pair held by the customer.
type Account struct {
	ProductType             string `json:"productType"`
	SecuritiesAccountNumber string `json:"securitiesAccountNumber"`
	CashAccountNumber       string `json:"cashAccountNumber"`
	Currency                string `json:"currency"`
}

// Label is the human-readable account kind.
func (a Account) Label() string {
	switch a.ProductType {
	case "DEFAULT":
		return "Securities account"
	case "TAX_WRAPPER":
		return "PEA"
	case "":
		return "unknown"
	default:
		return a.ProductType
	}
}

// Accounts lists every account pair of the logged-in customer: the default
// securities account, a French PEA when present, and any other product type
// Trade Republic reports.
func (c *Client) Accounts(ctx context.Context) ([]Account, []map[string]any, error) {
	raw, err := c.request(ctx, "accountPairs", nil)
	if err != nil {
		return nil, nil, err
	}
	var payload struct {
		Accounts []Account `json:"accounts"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, nil, fmt.Errorf("parse accountPairs: %w", err)
	}
	rows, err := Rows(raw)
	if err != nil {
		return nil, nil, fmt.Errorf("parse accountPairs: %w", err)
	}
	return payload.Accounts, rows, nil
}

// Positions returns the holdings of one securities account, one row per
// position. Every asset class Trade Republic groups into a category is
// included, crypto among them.
func (c *Client) Positions(ctx context.Context, securitiesAccountNumber string) ([]map[string]any, error) {
	raw, err := c.request(ctx, "compactPortfolioByType", map[string]any{
		"secAccNo": securitiesAccountNumber,
	})
	if err != nil {
		return nil, err
	}
	var payload struct {
		Categories []map[string]any `json:"categories"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("parse compactPortfolioByType: %w", err)
	}
	if len(payload.Categories) == 0 {
		return Rows(raw)
	}
	return expandCategories(payload.Categories), nil
}

// expandCategories turns portfolio categories into position rows, keeping the
// category's own fields under a "category." prefix.
func expandCategories(categories []map[string]any) []map[string]any {
	var out []map[string]any
	for _, category := range categories {
		positions, key := nestedRows(category)
		if len(positions) == 0 {
			out = append(out, category)
			continue
		}
		meta := make(map[string]any, len(category))
		for k, v := range category {
			if k != key {
				meta["category."+k] = v
			}
		}
		for _, position := range positions {
			row := maps.Clone(meta)
			maps.Copy(row, position)
			out = append(out, row)
		}
	}
	return out
}

// nestedRows finds the slice of objects nested in a category, preferring the
// documented "positions" key but tolerating a renamed one.
func nestedRows(category map[string]any) ([]map[string]any, string) {
	keys := make([]string, 0, len(category)+1)
	keys = append(keys, "positions")
	for k := range category {
		if k != "positions" {
			keys = append(keys, k)
		}
	}
	for _, k := range keys {
		list, ok := category[k].([]any)
		if !ok || len(list) == 0 {
			continue
		}
		rows := make([]map[string]any, 0, len(list))
		for _, item := range list {
			row, ok := item.(map[string]any)
			if !ok {
				return nil, ""
			}
			rows = append(rows, row)
		}
		return rows, k
	}
	return nil, ""
}

// Cash returns the cash balance of one cash account. An empty account number
// returns the balance of the default account.
func (c *Client) Cash(ctx context.Context, cashAccountNumber string) ([]map[string]any, error) {
	params := map[string]any{}
	if cashAccountNumber != "" {
		params["accountNumber"] = cashAccountNumber
	}
	raw, err := c.request(ctx, "cash", params)
	if err != nil {
		return nil, err
	}
	return Rows(raw)
}

// Fetch runs a subscription that takes no parameters, such as availableCash,
// savingsPlans, orders or watchlist.
func (c *Client) Fetch(ctx context.Context, sub string) ([]map[string]any, error) {
	raw, err := c.request(ctx, sub, nil)
	if err != nil {
		return nil, err
	}
	return Rows(raw)
}

// Timeline pages through a cursor-based timeline subscription such as
// timelineTransactions or timelineActivityLog. progress, when non-nil, is
// called after each page with the number of pages and items read so far.
func (c *Client) Timeline(ctx context.Context, sub string, progress func(pages, items int)) ([]map[string]any, error) {
	var (
		all    []map[string]any
		cursor string
		pages  int
	)
	for {
		params := map[string]any{}
		if cursor != "" {
			params["after"] = cursor
		}
		raw, err := c.request(ctx, sub, params)
		if err != nil {
			return all, err
		}

		var page struct {
			Items   []map[string]any `json:"items"`
			Cursors struct {
				After string `json:"after"`
			} `json:"cursors"`
		}
		if err := json.Unmarshal(raw, &page); err != nil {
			return all, fmt.Errorf("parse %s: %w", sub, err)
		}

		all = append(all, page.Items...)
		pages++
		if progress != nil {
			progress(pages, len(all))
		}

		// Stop on an empty page, an exhausted cursor, or a cursor that does
		// not advance, which would otherwise loop forever.
		if len(page.Items) == 0 || page.Cursors.After == "" || page.Cursors.After == cursor {
			return all, nil
		}
		cursor = page.Cursors.After
	}
}

// TransactionDetails returns the "Transaction" section of a timeline event as
// flat key-value pairs, which is where fees, quantities and venues live.
func (c *Client) TransactionDetails(ctx context.Context, id string) (map[string]any, error) {
	raw, err := c.request(ctx, "timelineDetailV2", map[string]any{"id": id})
	if err != nil {
		return nil, err
	}

	var payload struct {
		Sections []struct {
			Title string `json:"title"`
			Data  []struct {
				Title  string `json:"title"`
				Detail struct {
					Text string `json:"text"`
				} `json:"detail"`
			} `json:"data"`
		} `json:"sections"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("parse timelineDetailV2: %w", err)
	}

	out := make(map[string]any)
	for _, section := range payload.Sections {
		if section.Title != "Transaction" {
			continue
		}
		for _, item := range section.Data {
			if item.Title != "" && item.Detail.Text != "" {
				out["detail."+item.Title] = item.Detail.Text
			}
		}
	}
	return out, nil
}

// Rows normalizes a payload into table rows: a JSON array becomes its
// elements, an object with an "items" or "accounts" list becomes that list, and
// any other object becomes a single row.
func Rows(raw json.RawMessage) ([]map[string]any, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return nil, nil
	}

	var list []map[string]any
	if err := json.Unmarshal(raw, &list); err == nil {
		return list, nil
	}

	var object map[string]any
	if err := json.Unmarshal(raw, &object); err != nil {
		return nil, fmt.Errorf("unexpected payload %s", truncate(trimmed, 120))
	}
	for _, key := range []string{"items", "accounts"} {
		nested, ok := object[key].([]any)
		if !ok {
			continue
		}
		rows := make([]map[string]any, 0, len(nested))
		for _, item := range nested {
			row, ok := item.(map[string]any)
			if !ok {
				break
			}
			rows = append(rows, row)
		}
		if len(rows) == len(nested) {
			return rows, nil
		}
	}
	return []map[string]any{object}, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
