package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/smuda/litellm-llama-with-keycloak/login-helper/internal/login"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: login-helper <broker-url>")
		os.Exit(2)
	}
	if err := login.Run(strings.TrimRight(os.Args[1], "/")); err != nil {
		fmt.Fprintln(os.Stderr, "login-helper:", err)
		os.Exit(1)
	}
}
