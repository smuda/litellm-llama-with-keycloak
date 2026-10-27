package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestMintKey(t *testing.T) {
	const viewer = "internal_user_viewer"
	for _, tc := range []struct {
		name       string
		newStatus  int
		infoStatus int
		role       string // JSON literal for user_role
		updStatus  int
		wantErr    bool
		wantPaths  []string
		wantUpdate bool
		infoUser   string // user_id /user/info answers for
	}{
		{"new user", 200, 0, "", 0, false, []string{"/user/new", "/key/generate"}, false, "alice"},
		{"existing, null role", 409, 200, `null`, 200, false, []string{"/user/new", "/user/info", "/user/update", "/key/generate"}, true, "alice"},
		{"existing, empty role", 409, 200, `""`, 200, false, []string{"/user/new", "/user/info", "/user/update", "/key/generate"}, true, "alice"},
		{"existing, internal_user", 409, 200, `"internal_user"`, 200, false, []string{"/user/new", "/user/info", "/user/update", "/key/generate"}, true, "alice"},
		{"existing, proxy_admin", 409, 200, `"proxy_admin"`, 0, false, []string{"/user/new", "/user/info", "/key/generate"}, false, "alice"},
		{"existing, viewer", 409, 200, `"internal_user_viewer"`, 0, false, []string{"/user/new", "/user/info", "/key/generate"}, false, "alice"},
		{"update fails", 409, 200, `null`, 500, true, []string{"/user/new", "/user/info", "/user/update"}, true, "alice"},
		{"info fails", 409, 500, "", 0, true, []string{"/user/new", "/user/info"}, false, "alice"},
		{"info for another user", 409, 200, `"proxy_admin"`, 0, true, []string{"/user/new", "/user/info"}, false, "admin"},
		{"user/new fails", 500, 0, "", 0, true, []string{"/user/new"}, false, "alice"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var paths []string
			bodies := map[string]map[string]any{}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				paths = append(paths, r.URL.Path)
				if r.Body != nil {
					var m map[string]any
					if json.NewDecoder(r.Body).Decode(&m) == nil {
						bodies[r.URL.Path] = m
					}
				}
				switch r.URL.Path {
				case "/user/new":
					w.WriteHeader(tc.newStatus)
				case "/user/info":
					if r.URL.Query().Get("user_id") != "alice" {
						t.Errorf("user/info user_id = %q, want alice", r.URL.Query().Get("user_id"))
					}
					if tc.infoStatus != 200 {
						w.WriteHeader(tc.infoStatus)
						return
					}
					_, _ = w.Write([]byte(`{"user_id":"` + tc.infoUser + `","user_info":{"user_role":` + tc.role + `}}`))
				case "/user/update":
					w.WriteHeader(tc.updStatus)
				case "/key/generate":
					_, _ = w.Write([]byte(`{"key":"sk-new"}`))
				default:
					http.NotFound(w, r)
				}
			}))
			defer srv.Close()
			b := &broker{litellmURL: srv.URL, client: srv.Client()}

			key, err := b.mintKey(context.Background(), "alice", "alice@example.com")
			if !reflect.DeepEqual(paths, tc.wantPaths) {
				t.Errorf("paths = %v, want %v", paths, tc.wantPaths)
			}
			if tc.wantErr {
				if err == nil {
					t.Fatalf("got key %q, want error", key)
				}
			} else if err != nil || key != "sk-new" {
				t.Fatalf("got %q, %v; want sk-new", key, err)
			}
			if got := bodies["/user/new"]["user_role"]; got != viewer {
				t.Errorf("/user/new user_role = %v, want %s", got, viewer)
			}
			if tc.wantUpdate {
				want := map[string]any{"user_id": "alice", "user_role": viewer}
				if !reflect.DeepEqual(bodies["/user/update"], want) {
					t.Errorf("/user/update body = %v, want %v", bodies["/user/update"], want)
				}
			}
		})
	}
}
