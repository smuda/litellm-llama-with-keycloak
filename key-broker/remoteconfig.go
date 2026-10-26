package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const minKeyLife = 7 * 24 * time.Hour

// opencode treats an HTML response from remote_config as "log in again".
const loginPage = `<!doctype html><title>Login required</title><p>Your key is unknown or expires within 7 days. Run <code>opencode auth login</code> again.</p>`

func freshEnough(expires *time.Time, now time.Time) bool {
	return expires == nil || expires.Sub(now) >= minKeyLife
}

func (b *broker) keyExpires(ctx context.Context, key string) (*time.Time, error) {
	out, err := b.litellm(ctx, "GET", "/key/info?key="+url.QueryEscape(key), nil)
	if err != nil {
		return nil, err
	}
	var res struct {
		Info struct {
			Expires *time.Time `json:"expires"`
		} `json:"info"`
	}
	if err := json.Unmarshal(out, &res); err != nil {
		return nil, err
	}
	return res.Info.Expires, nil
}

func (b *broker) handleRemoteConfig(w http.ResponseWriter, r *http.Request) {
	key, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || key == "" {
		writeLoginPage(w)
		return
	}
	expires, err := b.keyExpires(r.Context(), key)
	if errors.Is(err, errNotFound) {
		writeLoginPage(w)
		return
	}
	if err != nil {
		// err embeds the request path, which carries the key.
		slog.Error("remote-config: key lookup failed")
		http.Error(w, "litellm unavailable", http.StatusBadGateway)
		return
	}
	if !freshEnough(expires, time.Now()) {
		writeLoginPage(w)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	http.ServeFile(w, r, b.wellknown)
}

func writeLoginPage(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(loginPage))
}
