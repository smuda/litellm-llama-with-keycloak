package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

func newVerifier() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func challengeFor(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

var client = &http.Client{Timeout: 30 * time.Second}

func redeem(brokerURL, code, verifier string) (string, error) {
	body, _ := json.Marshal(map[string]string{"code": code, "verifier": verifier})
	resp, err := client.Post(strings.TrimRight(brokerURL, "/")+"/redeem", "application/json", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("redeem: status %d", resp.StatusCode)
	}
	var out struct {
		Key string `json:"key"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil || out.Key == "" {
		return "", errors.New("redeem: no key in response")
	}
	return out.Key, nil
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: login-helper <broker-url>")
		os.Exit(2)
	}
	if err := run(strings.TrimRight(os.Args[1], "/")); err != nil {
		fmt.Fprintln(os.Stderr, "login-helper:", err)
		os.Exit(1)
	}
}

func run(broker string) error {
	verifier, err := newVerifier()
	if err != nil {
		return err
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	type result struct {
		key string
		err error
	}
	results := make(chan result, 1)
	var once sync.Once
	mux := http.NewServeMux()
	mux.HandleFunc("GET /done", func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		if code == "" {
			http.Error(w, "missing code", http.StatusBadRequest)
			return
		}
		once.Do(func() {
			key, err := redeem(broker, code, verifier)
			if err != nil {
				http.Error(w, "Login failed, see the terminal.", http.StatusBadGateway)
			} else {
				fmt.Fprint(w, "Login complete. You can close this tab.")
			}
			results <- result{key, err}
		})
	})
	srv := &http.Server{Handler: mux}
	go srv.Serve(ln)
	defer srv.Shutdown(context.Background())

	loginURL := fmt.Sprintf("%s/login?port=%d&challenge=%s", broker,
		ln.Addr().(*net.TCPAddr).Port, url.QueryEscape(challengeFor(verifier)))
	fmt.Fprintln(os.Stderr, "Open this URL to log in:", loginURL)
	exec.Command("open", loginURL).Start()

	select {
	case res := <-results:
		if res.err != nil {
			return res.err
		}
		fmt.Println(res.key)
		return nil
	case <-time.After(5 * time.Minute):
		return errors.New("timed out waiting for login")
	}
}
