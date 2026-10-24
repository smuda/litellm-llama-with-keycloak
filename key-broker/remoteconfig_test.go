package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFreshEnough(t *testing.T) {
	now := time.Now()
	at := func(d time.Duration) *time.Time { t := now.Add(d); return &t }
	day := 24 * time.Hour
	for _, tc := range []struct {
		name    string
		expires *time.Time
		want    bool
	}{
		{"no expiry", nil, true},
		{"14 days", at(14 * day), true},
		{"exactly 7 days", at(7 * day), true},
		{"7 days minus 1s", at(7*day - time.Second), false},
		{"6 days", at(6 * day), false},
		{"expired", at(-time.Hour), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := freshEnough(tc.expires, now); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestHandleRemoteConfig(t *testing.T) {
	const wellknown = `{"config":{"model":"litellm/medium"}}`
	day := 24 * time.Hour
	expiry := func(d time.Duration) string {
		return fmt.Sprintf(`{"info":{"expires":%q}}`, time.Now().Add(d).UTC().Format(time.RFC3339))
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/key/info" || r.Header.Get("Authorization") != "Bearer master" {
			http.Error(w, "bad request", http.StatusUnauthorized)
			return
		}
		switch r.URL.Query().Get("key") {
		case "sk-fresh":
			w.Write([]byte(expiry(14 * day)))
		case "sk-stale":
			w.Write([]byte(expiry(6 * day)))
		case "sk-forever":
			w.Write([]byte(`{"info":{"expires":null}}`))
		case "sk-broken":
			w.WriteHeader(http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	path := filepath.Join(t.TempDir(), "opencode.json")
	if err := os.WriteFile(path, []byte(wellknown), 0o600); err != nil {
		t.Fatal(err)
	}
	b := &broker{litellmURL: srv.URL, masterKey: "master", client: srv.Client(), wellknown: path}

	for _, tc := range []struct {
		name   string
		auth   string
		key    string
		status int
		ctype  string // prefix
		body   string // prefix, or full body when json
	}{
		{"fresh", "Bearer sk-fresh", "sk-fresh", http.StatusOK, "application/json", wellknown},
		{"no expiry", "Bearer sk-forever", "sk-forever", http.StatusOK, "application/json", wellknown},
		{"stale", "Bearer sk-stale", "sk-stale", http.StatusOK, "text/html", "<!doctype html>"},
		{"unknown key", "Bearer sk-unknown", "sk-unknown", http.StatusOK, "text/html", "<!doctype html>"},
		{"no header", "", "", http.StatusOK, "text/html", "<!doctype html>"},
		{"empty bearer", "Bearer ", "", http.StatusOK, "text/html", "<!doctype html>"},
		{"litellm error", "Bearer sk-broken", "sk-broken", http.StatusBadGateway, "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/remote-config", nil)
			if tc.auth != "" {
				req.Header.Set("Authorization", tc.auth)
			}
			rec := httptest.NewRecorder()
			b.handleRemoteConfig(rec, req)

			if rec.Code != tc.status {
				t.Fatalf("status %d, want %d", rec.Code, tc.status)
			}
			body := rec.Body.String()
			if tc.ctype != "" && !strings.HasPrefix(rec.Header().Get("Content-Type"), tc.ctype) {
				t.Errorf("Content-Type %q, want prefix %q", rec.Header().Get("Content-Type"), tc.ctype)
			}
			if tc.ctype == "application/json" && body != tc.body {
				t.Errorf("body %q, want %q", body, tc.body)
			}
			if tc.ctype == "text/html" && !strings.HasPrefix(body, tc.body) {
				t.Errorf("body %q, want prefix %q", body, tc.body)
			}
			if tc.key != "" && strings.Contains(body, tc.key) {
				t.Errorf("body leaks key %q", tc.key)
			}
		})
	}
}
