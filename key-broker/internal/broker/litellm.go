package broker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// keyHolderRole can call the proxy but not manage keys.
const keyHolderRole = "internal_user_viewer"

func (b *Broker) mintKey(ctx context.Context, user, email string) (string, error) {
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

func (b *Broker) litellm(ctx context.Context, method, path string, body any) ([]byte, error) {
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
