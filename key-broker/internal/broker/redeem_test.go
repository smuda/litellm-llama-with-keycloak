package broker

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

// Only stored and compared by the broker tests; PKCE math is tested in internal/pkce.
const testChallenge = "xV-H5naeMh3Vvh-GBIURNUinSkKCrG9nYXDjUSrs7Io"

type fakeLiteLLM struct {
	mu           sync.Mutex
	keyStatus    int
	generatedFor []string
}

func newRedeemBroker(t *testing.T, keyStatus int) (*Broker, *fakeLiteLLM) {
	t.Helper()
	f := &fakeLiteLLM{keyStatus: keyStatus}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/key/generate" {
			return
		}
		var req struct {
			UserID string `json:"user_id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		f.mu.Lock()
		f.generatedFor = append(f.generatedFor, req.UserID)
		f.mu.Unlock()
		if f.keyStatus != http.StatusOK {
			http.Error(w, "boom", f.keyStatus)
			return
		}
		_, _ = w.Write([]byte(`{"key":"sk-test"}`))
	}))
	t.Cleanup(srv.Close)
	return &Broker{st: newStore(), litellmURL: srv.URL, client: srv.Client()}, f
}

func (f *fakeLiteLLM) checkMints(t *testing.T, want int) {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.generatedFor) != want {
		t.Errorf("/key/generate calls = %d, want %d", len(f.generatedFor), want)
	}
	for _, u := range f.generatedFor {
		if u != "alice" {
			t.Errorf("minted for user_id %q, want alice", u)
		}
	}
}

func TestHandleRedeem(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantStatus int
		wantMints  int
	}{
		{"correct verifier", `{"code":"c1","verifier":"playground-verifier"}`, http.StatusOK, 1},
		{"wrong verifier", `{"code":"c1","verifier":"nope"}`, http.StatusForbidden, 0},
		{"unknown code", `{"code":"zzz","verifier":"playground-verifier"}`, http.StatusForbidden, 0},
		{"malformed json", `{"code":`, http.StatusBadRequest, 0},
		{"missing verifier", `{"code":"c1"}`, http.StatusBadRequest, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, f := newRedeemBroker(t, http.StatusOK)
			put(b.st, b.st.codes, "c1", issued{user: "alice", email: "alice@example.com", challenge: testChallenge})

			req := httptest.NewRequest(http.MethodPost, "/redeem", strings.NewReader(tt.body))
			rec := httptest.NewRecorder()
			b.HandleRedeem(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body %q", rec.Code, tt.wantStatus, rec.Body.String())
			}
			f.checkMints(t, tt.wantMints)
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
		name      string
		keyStatus int
		steps     []step
		wantMints int
	}{
		{"code is single use", 200, []step{{good, 0, 200}, {good, 0, 403}}, 1},
		{"wrong verifier burns code", 200, []step{{bad, 0, 403}, {good, 0, 403}}, 0},
		{"expired at exactly ttl", 200, []step{{good, ttl, 403}}, 0},
		{"valid just before ttl", 200, []step{{good, ttl - time.Nanosecond, 200}}, 1},
		{"mint failure burns code", 500, []step{{good, 0, 502}, {good, 0, 403}}, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t0 := time.Unix(1_000_000, 0)
			now := t0
			b, f := newRedeemBroker(t, tt.keyStatus)
			b.st.now = func() time.Time { return now }
			put(b.st, b.st.codes, "c1", issued{user: "alice", email: "alice@example.com", challenge: testChallenge})

			for i, s := range tt.steps {
				now = now.Add(s.advance)
				rec := httptest.NewRecorder()
				b.HandleRedeem(rec, httptest.NewRequest(http.MethodPost, "/redeem", strings.NewReader(s.body)))
				if rec.Code != s.wantStatus {
					t.Fatalf("step %d: status = %d, want %d; body %q", i, rec.Code, s.wantStatus, rec.Body.String())
				}
				if s.wantStatus == http.StatusBadGateway && !strings.Contains(rec.Body.String(), "could not mint key") {
					t.Errorf("step %d: 502 body = %q, want it to contain %q", i, rec.Body.String(), "could not mint key")
				}
				if s.wantStatus != http.StatusOK && strings.Contains(rec.Body.String(), "sk-test") {
					t.Errorf("step %d: error response leaks key: %q", i, rec.Body.String())
				}
			}
			f.checkMints(t, tt.wantMints)
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
	b := &Broker{st: newStore(), oauth: &oauth2.Config{Endpoint: oauth2.Endpoint{TokenURL: srv.URL}}}
	b.st.now = func() time.Time { return now }
	put(b.st, b.st.logins, "ok", pending{port: "1234", challenge: testChallenge})
	put(b.st, b.st.logins, "old", pending{port: "1234", challenge: testChallenge})

	call := func(state string) int {
		rec := httptest.NewRecorder()
		b.HandleCallback(rec, httptest.NewRequest(http.MethodGet, "/callback?code=x&state="+state, nil))
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
	b := &Broker{st: newStore()}
	b.st.now = func() time.Time { return now }
	put(b.st, b.st.codes, "old", issued{user: "u1"})
	now = now.Add(ttl + time.Second)
	put(b.st, b.st.codes, "new", issued{user: "u2"})
	if _, ok := b.st.codes["old"]; ok || len(b.st.codes) != 1 {
		t.Fatalf("codes = %v, want only \"new\"", b.st.codes)
	}
}
