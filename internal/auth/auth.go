// Package auth performs the Trade Republic web login: phone number and PIN,
// then a two-factor code, in exchange for a session cookie.
package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	baseURL     = "https://api.traderepublic.com"
	appVersion  = "13.40.5"
	userAgent   = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0.0.0 Safari/537.36"
	sessionName = "tr_session"
)

// Client calls the authentication endpoints.
type Client struct {
	http       *http.Client
	baseURL    string
	wafToken   string
	deviceInfo string
}

// New builds a client. wafToken is the AWS WAF challenge token and deviceInfo
// the x-tr-device-info value; both are required by the API.
func New(wafToken, deviceInfo string) *Client {
	return &Client{
		http:       &http.Client{Timeout: 30 * time.Second},
		baseURL:    baseURL,
		wafToken:   wafToken,
		deviceInfo: deviceInfo,
	}
}

// Process is a login attempt waiting for its two-factor code.
type Process struct {
	client           *Client
	ID               string `json:"processId"`
	CountdownSeconds int    `json:"countdownInSeconds"`
}

// Start submits the credentials and returns the pending login process.
func (c *Client) Start(ctx context.Context, phone, pin string) (*Process, error) {
	body, err := json.Marshal(map[string]string{"phoneNumber": phone, "pin": pin})
	if err != nil {
		return nil, err
	}

	raw, err := c.do(ctx, http.MethodPost, "/api/v1/auth/web/login", body)
	if err != nil {
		return nil, fmt.Errorf("login: %w", err)
	}

	process := &Process{client: c}
	if err := json.Unmarshal(raw, process); err != nil {
		return nil, fmt.Errorf("login: unexpected response: %w", err)
	}
	if process.ID == "" {
		return nil, fmt.Errorf("login: no process id returned, check the phone number and PIN")
	}
	return process, nil
}

// Resend asks Trade Republic to send the two-factor code by SMS.
func (p *Process) Resend(ctx context.Context) error {
	_, err := p.client.do(ctx, http.MethodPost, "/api/v1/auth/web/login/"+p.ID+"/resend", nil)
	if err != nil {
		return fmt.Errorf("resend code: %w", err)
	}
	return nil
}

// Verify submits the two-factor code and returns the session token.
func (p *Process) Verify(ctx context.Context, code string) (string, error) {
	req, err := p.client.request(ctx, http.MethodPost,
		fmt.Sprintf("/api/v1/auth/web/login/%s/%s", p.ID, code), nil)
	if err != nil {
		return "", err
	}

	resp, err := p.client.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("verify code: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("verify code: HTTP %d: %s", resp.StatusCode, truncate(string(body), 200))
	}
	for _, cookie := range resp.Cookies() {
		if cookie.Name == sessionName {
			return cookie.Value, nil
		}
	}
	if session := cookieValue(resp.Header, sessionName); session != "" {
		return session, nil
	}
	return "", fmt.Errorf("verify code: no %s cookie in the response", sessionName)
}

func (c *Client) request(ctx context.Context, method, path string, body []byte) (*http.Request, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Language", "fr")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("x-aws-waf-token", c.wafToken)
	req.Header.Set("x-tr-app-version", appVersion)
	req.Header.Set("x-tr-device-info", c.deviceInfo)
	req.Header.Set("x-tr-platform", "web")
	return req, nil
}

func (c *Client) do(ctx context.Context, method, path string, body []byte) ([]byte, error) {
	req, err := c.request(ctx, method, path, body)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s (an expired WAF token or wrong credentials both look like this)",
			resp.StatusCode, truncate(string(raw), 200))
	}
	return raw, nil
}

// cookieValue reads a cookie from raw Set-Cookie headers, for servers whose
// attributes net/http refuses to parse.
func cookieValue(h http.Header, name string) string {
	for _, line := range h.Values("Set-Cookie") {
		pair := strings.SplitN(strings.TrimSpace(strings.Split(line, ";")[0]), "=", 2)
		if len(pair) == 2 && pair[0] == name {
			return pair[1]
		}
	}
	return ""
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
