package main

import "testing"

const (
	testVerifier  = "playground-verifier"
	testChallenge = "xV-H5naeMh3Vvh-GBIURNUinSkKCrG9nYXDjUSrs7Io"
)

func TestVerifyChallenge(t *testing.T) {
	tests := []struct {
		name      string
		verifier  string
		challenge string
		want      bool
	}{
		{"matching vector", testVerifier, testChallenge, true},
		{"wrong verifier", "other-verifier", testChallenge, false},
		{"empty verifier", "", testChallenge, false},
		{"padded challenge", testVerifier, testChallenge + "=", false},
		{"standard base64 alphabet", testVerifier, "xV+H5naeMh3Vvh+GBIURNUinSkKCrG9nYXDjUSrs7Io", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := verifyChallenge(tt.verifier, tt.challenge); got != tt.want {
				t.Errorf("verifyChallenge(%q, %q) = %v, want %v", tt.verifier, tt.challenge, got, tt.want)
			}
		})
	}
}
