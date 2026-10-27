package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/smuda/litellm-llama-with-keycloak/key-broker/internal/access"
	"github.com/smuda/litellm-llama-with-keycloak/key-broker/internal/broker"
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
	accessGroups := access.ParseGroups(env("ACCESS_GROUPS"))
	if len(accessGroups) == 0 {
		slog.Error("ACCESS_GROUPS has no groups")
		os.Exit(1)
	}
	groupsClaim := env("GROUPS_CLAIM")
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
	b := broker.New(
		&oauth2.Config{
			ClientID:     env("OIDC_CLIENT_ID"),
			ClientSecret: env("OIDC_CLIENT_SECRET"),
			Endpoint:     provider.Endpoint(),
			RedirectURL:  env("PUBLIC_URL") + "/callback",
			Scopes:       []string{oidc.ScopeOpenID, "email", "profile"},
		},
		provider.Verifier(&oidc.Config{ClientID: env("OIDC_CLIENT_ID")}),
		env("LITELLM_URL"),
		env("LITELLM_MASTER_KEY"),
		env("WELLKNOWN_PATH"),
		accessGroups,
		groupsClaim,
	)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/opencode", b.HandleWellKnown)
	mux.HandleFunc("GET /login", b.HandleLogin)
	mux.HandleFunc("GET /callback", b.HandleCallback)
	mux.HandleFunc("POST /redeem", b.HandleRedeem)
	mux.HandleFunc("GET /remote-config", b.HandleRemoteConfig)
	slog.Info("Starting server on :8090")
	if err := http.ListenAndServe(":8090", mux); err != nil {
		slog.Error("server failed", "error", err)
		os.Exit(1)
	}
}
