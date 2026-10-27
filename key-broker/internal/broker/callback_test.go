package broker

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"golang.org/x/oauth2"
)

func TestCallbackGroupsClaim(t *testing.T) {
	const issuer = "https://idp.example"
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key}, (&jose.SignerOptions{}).WithType("JWT"))
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name   string
		claims map[string]any
		want   int
	}{
		{"configured claim lists the group", map[string]any{"key_broker_groups": []string{"/llm-users"}}, http.StatusFound},
		{"only the default groups claim", map[string]any{"groups": []string{"/llm-users"}}, http.StatusForbidden},
		{"claim is a string", map[string]any{"key_broker_groups": "/llm-users"}, http.StatusForbidden},
		{"claim has a non-string entry", map[string]any{"key_broker_groups": []any{"/llm-users", 1}}, http.StatusForbidden},
		{"claim is an object", map[string]any{"key_broker_groups": map[string]any{"a": "/llm-users"}}, http.StatusForbidden},
		{"claim is null", map[string]any{"key_broker_groups": nil}, http.StatusForbidden},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Now()
			std := jwt.Claims{Issuer: issuer, Audience: jwt.Audience{"key-broker"}, Subject: "u1", IssuedAt: jwt.NewNumericDate(now), Expiry: jwt.NewNumericDate(now.Add(time.Hour))}
			idToken, err := jwt.Signed(signer).Claims(std).Claims(map[string]any{"preferred_username": "alice"}).Claims(tc.claims).Serialize()
			if err != nil {
				t.Fatal(err)
			}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "x", "token_type": "Bearer", "id_token": idToken})
			}))
			defer srv.Close()

			b := &Broker{
				oauth:        &oauth2.Config{ClientID: "key-broker", Endpoint: oauth2.Endpoint{TokenURL: srv.URL + "/token"}},
				verifier:     oidc.NewVerifier(issuer, &oidc.StaticKeySet{PublicKeys: []crypto.PublicKey{&key.PublicKey}}, &oidc.Config{ClientID: "key-broker"}),
				groupsClaim:  "key_broker_groups",
				accessGroups: []string{"/llm-users"},
				st:           newStore(),
			}
			put(b.st, b.st.logins, "s1", pending{port: "1234", challenge: "c"})

			req := httptest.NewRequest("GET", "/callback?state=s1&code=abc", nil).WithContext(context.Background())
			rec := httptest.NewRecorder()
			b.HandleCallback(rec, req)

			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d (body %q)", rec.Code, tc.want, rec.Body.String())
			}
			if tc.want == http.StatusFound {
				if loc := rec.Header().Get("Location"); !strings.HasPrefix(loc, "http://127.0.0.1:1234/done?code=") {
					t.Errorf("Location = %q", loc)
				}
			} else if !strings.Contains(rec.Body.String(), "not in an access group") {
				t.Errorf("body = %q, want access group message", rec.Body.String())
			}
		})
	}
}
