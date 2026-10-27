package login

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
)

func TestChallengeFor(t *testing.T) {
	got := challengeFor("playground-verifier")
	want := "xV-H5naeMh3Vvh-GBIURNUinSkKCrG9nYXDjUSrs7Io"
	if got != want {
		t.Errorf("challengeFor = %q, want %q", got, want)
	}
}

func TestNewVerifier(t *testing.T) {
	alphabet := regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)

	v1, err := newVerifier()
	if err != nil {
		t.Fatalf("newVerifier: %v", err)
	}
	v2, err := newVerifier()
	if err != nil {
		t.Fatalf("newVerifier: %v", err)
	}
	for _, v := range []string{v1, v2} {
		if !alphabet.MatchString(v) {
			t.Errorf("verifier %q is not 43 base64url chars", v)
		}
	}
	if v1 == v2 {
		t.Errorf("two verifiers are equal: %q", v1)
	}

	sum := sha256.Sum256([]byte(v1))
	want := base64.RawURLEncoding.EncodeToString(sum[:])
	if got := challengeFor(v1); got != want {
		t.Errorf("challengeFor(verifier) = %q, want %q", got, want)
	}
}

func TestRedeem(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		var gotPath, gotMethod string
		var gotBody map[string]string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPath, gotMethod = r.URL.Path, r.Method
			_ = json.NewDecoder(r.Body).Decode(&gotBody)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"key":"sk-test"}`))
		}))
		defer srv.Close()

		key, err := redeem(srv.URL, "c1", "v1")
		if err != nil {
			t.Fatalf("redeem: %v", err)
		}
		if key != "sk-test" {
			t.Errorf("key = %q, want sk-test", key)
		}
		if gotMethod != http.MethodPost || gotPath != "/redeem" {
			t.Errorf("request = %s %s, want POST /redeem", gotMethod, gotPath)
		}
		if gotBody["code"] != "c1" || gotBody["verifier"] != "v1" {
			t.Errorf("body = %v, want code=c1 verifier=v1", gotBody)
		}
	})

	errTests := []struct {
		name   string
		status int
		body   string
	}{
		{"forbidden", http.StatusForbidden, "forbidden"},
		{"ok with invalid json", http.StatusOK, "not json"},
	}
	for _, tt := range errTests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer srv.Close()

			if key, err := redeem(srv.URL, "c1", "v1"); err == nil {
				t.Errorf("redeem = %q, nil error; want error", key)
			}
		})
	}
}
