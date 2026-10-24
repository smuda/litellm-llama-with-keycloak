package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMintKeyExistingUser(t *testing.T) {
	for _, tc := range []struct {
		name       string
		userStatus int
		wantErr    bool
	}{
		{"new user", http.StatusOK, false},
		{"existing user", http.StatusConflict, false},
		{"user/new fails", http.StatusInternalServerError, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/user/new":
					w.WriteHeader(tc.userStatus)
				case "/key/generate":
					w.Write([]byte(`{"key":"sk-new"}`))
				default:
					http.NotFound(w, r)
				}
			}))
			defer srv.Close()
			b := &broker{litellmURL: srv.URL, client: srv.Client()}

			key, err := b.mintKey(context.Background(), "alice", "alice@example.com")
			if tc.wantErr {
				if err == nil {
					t.Fatalf("got key %q, want error", key)
				}
				return
			}
			if err != nil || key != "sk-new" {
				t.Fatalf("got %q, %v; want sk-new", key, err)
			}
		})
	}
}
