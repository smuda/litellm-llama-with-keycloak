package main

import (
	"encoding/json"
	"net/http"
	"sync"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

type pending struct{ port, challenge string }
type issued struct{ key, challenge string }

// ponytail: entries are never removed, so memory grows and a code can be
// redeemed again until restart; TTL and single-use are #15.
type store struct {
	mu     sync.Mutex
	logins map[string]pending
	codes  map[string]issued
}

func newStore() *store {
	return &store{logins: map[string]pending{}, codes: map[string]issued{}}
}

type broker struct {
	st         *store
	oauth      *oauth2.Config
	verifier   *oidc.IDTokenVerifier
	litellmURL string
	masterKey  string
	wellknown  string
	client     *http.Client
}

func (b *broker) handleRedeem(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code     string `json:"code"`
		Verifier string `json:"verifier"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Code == "" || req.Verifier == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	b.st.mu.Lock()
	c, ok := b.st.codes[req.Code]
	b.st.mu.Unlock()
	if !ok || !verifyChallenge(req.Verifier, c.challenge) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"key": c.key})
}
