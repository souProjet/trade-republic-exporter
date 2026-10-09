package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newTestClient points a client at a stub server.
func newTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	client := New("waf-token", "device-info")
	client.baseURL = server.URL
	return client
}

func TestStartSendsHeadersAndCredentials(t *testing.T) {
	var got struct {
		PhoneNumber string `json:"phoneNumber"`
		PIN         string `json:"pin"`
	}
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/auth/web/login" || r.Method != http.MethodPost {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		for header, want := range map[string]string{
			"x-aws-waf-token":  "waf-token",
			"x-tr-device-info": "device-info",
			"x-tr-platform":    "web",
		} {
			if r.Header.Get(header) != want {
				t.Errorf("header %s = %q, want %q", header, r.Header.Get(header), want)
			}
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(`{"processId":"abc","countdownInSeconds":42}`))
	})

	process, err := client.Start(context.Background(), "+33612345678", "1234")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if process.ID != "abc" || process.CountdownSeconds != 42 {
		t.Errorf("process = %+v", process)
	}
	if got.PhoneNumber != "+33612345678" || got.PIN != "1234" {
		t.Errorf("credentials sent = %+v", got)
	}
}

func TestStartRejectsEmptyProcess(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{}`))
	})
	if _, err := client.Start(context.Background(), "+33612345678", "1234"); err == nil {
		t.Error("want an error when no process id is returned")
	}
}

func TestStartSurfacesHTTPError(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"errors":[{"errorCode":"FORBIDDEN"}]}`))
	})
	_, err := client.Start(context.Background(), "+33612345678", "1234")
	if err == nil || !strings.Contains(err.Error(), "403") {
		t.Errorf("error = %v, want it to mention the status code", err)
	}
}

func TestVerifyReturnsSession(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if want := "/api/v1/auth/web/login/abc/123456"; r.URL.Path != want {
			t.Errorf("path = %q, want %q", r.URL.Path, want)
		}
		http.SetCookie(w, &http.Cookie{Name: sessionName, Value: "session-token"})
		w.Write([]byte(`{}`))
	})

	process := &Process{client: client, ID: "abc"}
	session, err := process.Verify(context.Background(), "123456")
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if session != "session-token" {
		t.Errorf("session = %q, want %q", session, "session-token")
	}
}

func TestVerifyFallsBackToRawSetCookie(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Set-Cookie", sessionName+"=raw-token; Path=/; SameSite=Weird")
		w.Write([]byte(`{}`))
	})

	process := &Process{client: client, ID: "abc"}
	session, err := process.Verify(context.Background(), "123456")
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if session != "raw-token" {
		t.Errorf("session = %q, want %q", session, "raw-token")
	}
}

func TestVerifyWithoutSessionCookie(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{}`))
	})

	process := &Process{client: client, ID: "abc"}
	if _, err := process.Verify(context.Background(), "123456"); err == nil {
		t.Error("want an error when the session cookie is absent")
	}
}

func TestResend(t *testing.T) {
	called := false
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		called = true
		if want := "/api/v1/auth/web/login/abc/resend"; r.URL.Path != want {
			t.Errorf("path = %q, want %q", r.URL.Path, want)
		}
		w.Write([]byte(`{}`))
	})

	process := &Process{client: client, ID: "abc"}
	if err := process.Resend(context.Background()); err != nil {
		t.Fatalf("Resend: %v", err)
	}
	if !called {
		t.Error("the resend endpoint was not called")
	}
}
