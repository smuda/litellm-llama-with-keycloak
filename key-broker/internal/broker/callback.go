package broker

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"

	"github.com/smuda/litellm-llama-with-keycloak/key-broker/internal/access"
	"github.com/smuda/litellm-llama-with-keycloak/key-broker/internal/pkce"
)

func (b *Broker) HandleLogin(w http.ResponseWriter, r *http.Request) {
	port, challenge := r.URL.Query().Get("port"), r.URL.Query().Get("challenge")
	if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 || challenge == "" {
		http.Error(w, "bad port or challenge", http.StatusBadRequest)
		return
	}
	state, err := pkce.RandomString()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	put(b.st, b.st.logins, state, pending{port: port, challenge: challenge})
	http.Redirect(w, r, b.oauth.AuthCodeURL(state), http.StatusFound)
}

func (b *Broker) HandleCallback(w http.ResponseWriter, r *http.Request) {
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
		Username string `json:"preferred_username"`
		Email    string `json:"email"`
	}
	if err := idt.Claims(&claims); err != nil || claims.Username == "" {
		http.Error(w, "missing claims", http.StatusBadGateway)
		return
	}
	// The claim name is configured, so it can't be a struct tag. Any
	// malformed value means no groups, which ends in the 403 below.
	var groups []string
	var all map[string]json.RawMessage
	if err := idt.Claims(&all); err != nil || json.Unmarshal(all[b.groupsClaim], &groups) != nil {
		groups = nil
	}
	granted := access.GroupsOf(groups, b.accessGroups)
	if len(granted) == 0 {
		slog.Warn("not in an access group", "user", claims.Username, "groups", groups, "access_groups", b.accessGroups)
		http.Error(w, "You are not in an access group for this service. Contact your administrator.", http.StatusForbidden)
		return
	}
	slog.Info(fmt.Sprintf("user %s successfully authorized", claims.Username), "groups", granted)
	code, err := pkce.RandomString()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	put(b.st, b.st.codes, code, issued{user: claims.Username, email: claims.Email, challenge: p.challenge})
	http.Redirect(w, r, fmt.Sprintf("http://127.0.0.1:%s/done?code=%s", p.port, url.QueryEscape(code)), http.StatusFound)
}
