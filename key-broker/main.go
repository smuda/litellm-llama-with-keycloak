package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
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
			slog.Error("missing env", "key", k)
			os.Exit(1)
		}
		return v
	}
	accessGroups := parseAccessGroups(env("ACCESS_GROUPS"))
	if len(accessGroups) == 0 {
		slog.Error("ACCESS_GROUPS has no groups")
		os.Exit(1)
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
		slog.Error("oidc discovery failed", "error", err)
		os.Exit(1)
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
		verifier:     provider.Verifier(&oidc.Config{ClientID: env("OIDC_CLIENT_ID")}),
		litellmURL:   env("LITELLM_URL"),
		masterKey:    env("LITELLM_MASTER_KEY"),
		wellknown:    env("WELLKNOWN_PATH"),
		client:       &http.Client{Timeout: 15 * time.Second},
		accessGroups: accessGroups,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/opencode", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		http.ServeFile(w, r, b.wellknown)
	})
	mux.HandleFunc("GET /login", b.handleLogin)
	mux.HandleFunc("GET /callback", b.handleCallback)
	mux.HandleFunc("POST /redeem", b.handleRedeem)
	mux.HandleFunc("GET /remote-config", b.handleRemoteConfig)
	slog.Info("Starting server on :8090")
	if err := http.ListenAndServe(":8090", mux); err != nil {
		slog.Error("server failed", "error", err)
		os.Exit(1)
	}
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
		slog.Error("code exchange failed", "error", err)
		http.Error(w, "login failed", http.StatusBadGateway)
		return
	}
	raw, _ := tok.Extra("id_token").(string)
	idt, err := b.verifier.Verify(r.Context(), raw)
	if err != nil {
		slog.Error("id token verify failed", "error", err)
		http.Error(w, "login failed", http.StatusBadGateway)
		return
	}
	var claims struct {
		Username string   `json:"preferred_username"`
		Email    string   `json:"email"`
		Groups   []string `json:"groups"`
	}
	if err := idt.Claims(&claims); err != nil || claims.Username == "" {
		http.Error(w, "missing claims", http.StatusBadGateway)
		return
	}
	granted := accessGroupsOf(claims.Groups, b.accessGroups)
	if len(granted) == 0 {
		slog.Warn("not in an access group", "user", claims.Username, "groups", claims.Groups, "access_groups", b.accessGroups)
		http.Error(w, "You are not in an access group for this service. Contact your administrator.", http.StatusForbidden)
		return
	}
	slog.Info(fmt.Sprintf("user %s successfully authorized", claims.Username), "groups", granted)
	code, err := randomString()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	put(b.st, b.st.codes, code, issued{user: claims.Username, email: claims.Email, challenge: p.challenge})
	http.Redirect(w, r, fmt.Sprintf("http://127.0.0.1:%s/done?code=%s", p.port, url.QueryEscape(code)), http.StatusFound)
}

// keyHolderRole can call the proxy but not manage keys.
const keyHolderRole = "internal_user_viewer"

func (b *broker) mintKey(ctx context.Context, user, email string) (string, error) {
	// LiteLLM answers 409 when the user exists, which a second login hits.
	_, err := b.litellm(ctx, "POST", "/user/new", map[string]any{"user_id": user, "user_email": email, "auto_create_key": false, "user_role": keyHolderRole})
	if errors.Is(err, errConflict) {
		// Users created before the role was set have NULL, which LiteLLM treats
		// as internal_user: able to mint keys itself and bypass the group gate.
		info, err := b.litellm(ctx, "GET", "/user/info?user_id="+url.QueryEscape(user), nil)
		if err != nil {
			return "", err
		}
		var u struct {
			UserID   string `json:"user_id"`
			UserInfo struct {
				UserRole *string `json:"user_role"`
			} `json:"user_info"`
		}
		if err := json.Unmarshal(info, &u); err != nil {
			return "", fmt.Errorf("parse /user/info: %w", err)
		}
		// LiteLLM can resolve the query to another user (a space, an invalid
		// id falling back to the admin); trusting that role would skip the fix.
		if u.UserID != user {
			return "", fmt.Errorf("/user/info answered for %q, not %q", u.UserID, user)
		}
		if r := u.UserInfo.UserRole; r == nil || *r == "" || *r == "internal_user" {
			if _, err := b.litellm(ctx, "POST", "/user/update", map[string]any{"user_id": user, "user_role": keyHolderRole}); err != nil {
				return "", err
			}
		}
	} else if err != nil {
		return "", err
	}
	out, err := b.litellm(ctx, "POST", "/key/generate", map[string]string{"user_id": user, "duration": "14d"})
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

var (
	errConflict = errors.New("status 409")
	errNotFound = errors.New("status 404")
)

func (b *broker) litellm(ctx context.Context, method, path string, body any) ([]byte, error) {
	var rd io.Reader
	if body != nil {
		buf, _ := json.Marshal(body)
		rd = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, b.litellmURL+path, rd)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+b.masterKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := b.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	out, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusConflict {
		return nil, fmt.Errorf("%s: %w", path, errConflict)
	}
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("%s: %w", path, errNotFound)
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("%s: status %d", path, resp.StatusCode)
	}
	return out, nil
}
