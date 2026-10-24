package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
			b.st.codes["c1"] = issued{key: "sk-test", challenge: testChallenge}

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
