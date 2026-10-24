package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

func TestHandleRedeem(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantStatus int
	}{
		{"correct verifier", `{"code":"c1","verifier":"playground-verifier"}`, http.StatusOK},
		{"wrong verifier", `{"code":"c1","verifier":"nope"}`, http.StatusForbidden},
		{"unknown code", `{"code":"zzz","verifier":"playground-verifier"}`, http.StatusForbidden},
		{"malformed json", `{"code":`, http.StatusBadRequest},
		{"missing verifier", `{"code":"c1"}`, http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := &broker{st: newStore()}
			put(b.st, b.st.codes, "c1", issued{key: "sk-test", challenge: testChallenge})

			req := httptest.NewRequest(http.MethodPost, "/redeem", strings.NewReader(tt.body))
			rec := httptest.NewRecorder()
			b.handleRedeem(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body %q", rec.Code, tt.wantStatus, rec.Body.String())
			}
			if tt.wantStatus != http.StatusOK {
				if strings.Contains(rec.Body.String(), "sk-test") {
					t.Errorf("error response leaks key: %q", rec.Body.String())
				}
				return
			}
			if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
				t.Errorf("Content-Type = %q, want application/json", ct)
			}
			var got struct{ Key string }
			if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if got.Key != "sk-test" {
				t.Errorf("key = %q, want sk-test", got.Key)
			}
		})
	}
}

func TestRedeemSequence(t *testing.T) {
	good := `{"code":"c1","verifier":"playground-verifier"}`
	bad := `{"code":"c1","verifier":"nope"}`
	type step struct {
		body       string
		advance    time.Duration
		wantStatus int
	}
	tests := []struct {
		name  string
		steps []step
	}{
		{"code is single use", []step{{good, 0, 200}, {good, 0, 403}}},
		{"wrong verifier burns code", []step{{bad, 0, 403}, {good, 0, 403}}},
		{"expired at exactly ttl", []step{{good, ttl, 403}}},
		{"valid just before ttl", []step{{good, ttl - time.Nanosecond, 200}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t0 := time.Unix(1_000_000, 0)
			now := t0
			b := &broker{st: newStore()}
			b.st.now = func() time.Time { return now }
			put(b.st, b.st.codes, "c1", issued{key: "sk-test", challenge: testChallenge})

			for i, s := range tt.steps {
				now = now.Add(s.advance)
				rec := httptest.NewRecorder()
				b.handleRedeem(rec, httptest.NewRequest(http.MethodPost, "/redeem", strings.NewReader(s.body)))
				if rec.Code != s.wantStatus {
					t.Fatalf("step %d: status = %d, want %d; body %q", i, rec.Code, s.wantStatus, rec.Body.String())
				}
				if s.wantStatus == http.StatusForbidden && strings.Contains(rec.Body.String(), "sk-test") {
					t.Errorf("step %d: 403 leaks key: %q", i, rec.Body.String())
				}
			}
		})
	}
}

func TestHandleCallbackState(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	t0 := time.Unix(1_000_000, 0)
	now := t0
	b := &broker{st: newStore(), oauth: &oauth2.Config{Endpoint: oauth2.Endpoint{TokenURL: srv.URL}}}
	b.st.now = func() time.Time { return now }
	put(b.st, b.st.logins, "ok", pending{port: "1234", challenge: testChallenge})
	put(b.st, b.st.logins, "old", pending{port: "1234", challenge: testChallenge})

	call := func(state string) int {
		rec := httptest.NewRecorder()
		b.handleCallback(rec, httptest.NewRequest(http.MethodGet, "/callback?code=x&state="+state, nil))
		return rec.Code
	}
	if got := call("nope"); got != http.StatusBadRequest {
		t.Fatalf("unknown state = %d, want 400", got)
	}
	if got := call("ok"); got != http.StatusBadGateway {
		t.Fatalf("first use = %d, want 502 (past state check)", got)
	}
	if got := call("ok"); got != http.StatusBadRequest {
		t.Fatalf("reused state = %d, want 400", got)
	}
	now = t0.Add(ttl)
	if got := call("old"); got != http.StatusBadRequest {
		t.Fatalf("expired state = %d, want 400", got)
	}
}

func TestPutSweepsExpired(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	b := &broker{st: newStore()}
	b.st.now = func() time.Time { return now }
	put(b.st, b.st.codes, "old", issued{key: "k1"})
	now = now.Add(ttl + time.Second)
	put(b.st, b.st.codes, "new", issued{key: "k2"})
	if _, ok := b.st.codes["old"]; ok || len(b.st.codes) != 1 {
		t.Fatalf("codes = %v, want only \"new\"", b.st.codes)
	}
}
