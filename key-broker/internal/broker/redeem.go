package broker

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/smuda/litellm-llama-with-keycloak/key-broker/internal/pkce"
)

type pending struct{ port, challenge string }
type issued struct{ user, email, challenge string }

// Login state and one-time codes expire this long after creation.
const ttl = 5 * time.Minute

type entry[V any] struct {
	v       V
	expires time.Time
}

type store struct {
	mu     sync.Mutex
	now    func() time.Time
	logins map[string]entry[pending]
	codes  map[string]entry[issued]
}

func newStore() *store {
	return &store{now: time.Now, logins: map[string]entry[pending]{}, codes: map[string]entry[issued]{}}
}

// put sweeps expired entries from m on every insert so unredeemed ones don't pile up.
// ponytail: O(n) sweep per insert, fine at playground volume.
func put[V any](s *store, m map[string]entry[V], k string, v V) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	for key, e := range m {
		if !now.Before(e.expires) {
			delete(m, key)
		}
	}
	m[k] = entry[V]{v: v, expires: now.Add(ttl)}
}

// take removes k whatever the outcome, so a state or code works at most once.
func take[V any](s *store, m map[string]entry[V], k string) (V, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := m[k]
	delete(m, k)
	if !ok || !s.now().Before(e.expires) {
		var zero V
		return zero, false
	}
	return e.v, true
}

type Broker struct {
	st           *store
	oauth        *oauth2.Config
	verifier     *oidc.IDTokenVerifier
	litellmURL   string
	masterKey    string
	wellknown    string
	client       *http.Client
	accessGroups []string
	groupsClaim  string
}

func New(oauth *oauth2.Config, verifier *oidc.IDTokenVerifier,
	litellmURL, masterKey, wellknown string,
	accessGroups []string, groupsClaim string) *Broker {
	return &Broker{
		st:           newStore(),
		oauth:        oauth,
		verifier:     verifier,
		litellmURL:   litellmURL,
		masterKey:    masterKey,
		wellknown:    wellknown,
		client:       &http.Client{Timeout: 15 * time.Second},
		accessGroups: accessGroups,
		groupsClaim:  groupsClaim,
	}
}

func (b *Broker) HandleRedeem(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code     string `json:"code"`
		Verifier string `json:"verifier"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Code == "" || req.Verifier == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	c, ok := take(b.st, b.st.codes, req.Code)
	if !ok || !pkce.Verify(req.Verifier, c.challenge) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	key, err := b.mintKey(r.Context(), c.user, c.email)
	if err != nil {
		slog.Error("mint key for user failed", "user", c.user, "error", err)
		http.Error(w, "could not mint key", http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]string{"key": key}); err != nil {
		slog.Error("write key response failed", "user", c.user, "error", err)
	}
}
