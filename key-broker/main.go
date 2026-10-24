package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

func main() {
	env := func(k string) string {
		v := os.Getenv(k)
		if v == "" {
			log.Fatalf("missing env %s", k)
		}
		return v
	}
	// The issuer is the browser-facing URL; discovery goes over the compose network.
	ctx := oidc.InsecureIssuerURLContext(context.Background(), env("OIDC_ISSUER"))
	var provider *oidc.Provider
	var err error
	// Keycloak starts slower than the broker.
	for i := 0; i < 60; i++ {
		if provider, err = oidc.NewProvider(ctx, env("OIDC_DISCOVERY_URL")); err == nil {
			break
		}
		time.Sleep(2 * time.Second)
	}
	if err != nil {
		log.Fatalf("oidc discovery: %v", err)
	}
	b := &broker{
		st: newStore(),
		oauth: &oauth2.Config{
			ClientID:     env("OIDC_CLIENT_ID"),
			ClientSecret: env("OIDC_CLIENT_SECRET"),
			Endpoint:     provider.Endpoint(),
			RedirectURL:  env("PUBLIC_URL") + "/callback",
			Scopes:       []string{oidc.ScopeOpenID, "email", "profile"},
		},
		verifier:   provider.Verifier(&oidc.Config{ClientID: env("OIDC_CLIENT_ID")}),
		litellmURL: env("LITELLM_URL"),
		masterKey:  env("LITELLM_MASTER_KEY"),
		wellknown:  env("WELLKNOWN_PATH"),
		client:     &http.Client{Timeout: 15 * time.Second},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/opencode", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		http.ServeFile(w, r, b.wellknown)
	})
	mux.HandleFunc("GET /login", b.handleLogin)
	mux.HandleFunc("GET /callback", b.handleCallback)
	mux.HandleFunc("POST /redeem", b.handleRedeem)
	log.Fatal(http.ListenAndServe(":8090", mux))
}

func (b *broker) handleLogin(w http.ResponseWriter, r *http.Request) {
	port, challenge := r.URL.Query().Get("port"), r.URL.Query().Get("challenge")
	if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 || challenge == "" {
		http.Error(w, "bad port or challenge", http.StatusBadRequest)
		return
	}
	state, err := randomString()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	put(b.st, b.st.logins, state, pending{port: port, challenge: challenge})
	http.Redirect(w, r, b.oauth.AuthCodeURL(state), http.StatusFound)
}

func (b *broker) handleCallback(w http.ResponseWriter, r *http.Request) {
	p, ok := take(b.st, b.st.logins, r.URL.Query().Get("state"))
	if !ok {
		http.Error(w, "unknown state", http.StatusBadRequest)
		return
	}
	tok, err := b.oauth.Exchange(r.Context(), r.URL.Query().Get("code"))
	if err != nil {
		log.Printf("code exchange failed: %v", err)
		http.Error(w, "login failed", http.StatusBadGateway)
		return
	}
	raw, _ := tok.Extra("id_token").(string)
	idt, err := b.verifier.Verify(r.Context(), raw)
	if err != nil {
		log.Printf("id token verify failed: %v", err)
		http.Error(w, "login failed", http.StatusBadGateway)
		return
	}
	var claims struct {
		Username string `json:"preferred_username"`
		Email    string `json:"email"`
	}
	if err := idt.Claims(&claims); err != nil || claims.Username == "" {
		http.Error(w, "missing claims", http.StatusBadGateway)
		return
	}
	key, err := b.mintKey(r.Context(), claims.Username, claims.Email)
	if err != nil {
		log.Printf("mint key for %s failed: %v", claims.Username, err)
		http.Error(w, "could not mint key", http.StatusBadGateway)
		return
	}
	code, err := randomString()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	put(b.st, b.st.codes, code, issued{key: key, challenge: p.challenge})
	http.Redirect(w, r, fmt.Sprintf("http://127.0.0.1:%s/done?code=%s", p.port, url.QueryEscape(code)), http.StatusFound)
}

func (b *broker) mintKey(ctx context.Context, user, email string) (string, error) {
	// LiteLLM answers 409 when the user exists, which a second login hits.
	if _, err := b.litellm(ctx, "/user/new", map[string]any{"user_id": user, "user_email": email, "auto_create_key": false}); err != nil && !errors.Is(err, errConflict) {
		return "", err
	}
	out, err := b.litellm(ctx, "/key/generate", map[string]string{"user_id": user, "duration": "14d"})
	if err != nil {
		return "", err
	}
	var res struct {
		Key string `json:"key"`
	}
	if err := json.Unmarshal(out, &res); err != nil || res.Key == "" {
		return "", fmt.Errorf("no key in /key/generate response")
	}
	return res.Key, nil
}

var errConflict = errors.New("status 409")

func (b *broker) litellm(ctx context.Context, path string, body any) ([]byte, error) {
	buf, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, "POST", b.litellmURL+path, bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+b.masterKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := b.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusConflict {
		return nil, fmt.Errorf("%s: %w", path, errConflict)
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("%s: status %d", path, resp.StatusCode)
	}
	return out, nil
}
