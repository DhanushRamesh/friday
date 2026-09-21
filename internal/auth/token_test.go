package auth_test

import (
	"strings"
	"testing"

	"github.com/DhanushRamesh/friday/internal/auth"
)

func TestNewTokenIsUniqueAndWellFormed(t *testing.T) {
	const n = 1000
	seen := make(map[string]bool, n)

	for i := 0; i < n; i++ {
		token, hash, err := auth.NewToken()
		if err != nil {
			t.Fatalf("NewToken: %v", err)
		}
		if seen[token] {
			t.Fatalf("duplicate token generated: %s", token)
		}
		seen[token] = true

		if !strings.HasPrefix(token, auth.TokenPrefix) {
			t.Errorf("token %q has no prefix", token)
		}
		if !auth.LooksLikeToken(token) {
			t.Errorf("token %q is not recognised as one", token)
		}
		if len(hash) != auth.HashLen {
			t.Errorf("hash length = %d, want %d", len(hash), auth.HashLen)
		}
	}
}

// The stored form must not contain the token, or a database dump hands over
// every device.
func TestHashDoesNotRevealTheToken(t *testing.T) {
	token, hash, err := auth.NewToken()
	if err != nil {
		t.Fatalf("NewToken: %v", err)
	}

	if strings.Contains(hash, strings.TrimPrefix(token, auth.TokenPrefix)) {
		t.Error("the hash contains the token")
	}
	if hash == token {
		t.Error("the hash is the token")
	}
}

func TestHashIsStable(t *testing.T) {
	token, hash, err := auth.NewToken()
	if err != nil {
		t.Fatalf("NewToken: %v", err)
	}
	if again := auth.HashToken(token); again != hash {
		t.Errorf("hashing twice gave %q then %q", hash, again)
	}
	other, _, _ := auth.NewToken()
	if auth.HashToken(other) == hash {
		t.Error("two different tokens hashed alike")
	}
}

func TestLooksLikeTokenRejectsRubbish(t *testing.T) {
	valid, _, _ := auth.NewToken()

	cases := map[string]bool{
		valid:                             true,
		"":                                false,
		"fri_":                            false,
		"fri_short":                       false,
		strings.TrimPrefix(valid, "fri_"): false,
		"xxx_" + strings.TrimPrefix(valid, "fri_"): false,
		valid + "extra": false,
	}
	for s, want := range cases {
		if got := auth.LooksLikeToken(s); got != want {
			t.Errorf("LooksLikeToken(%q) = %v, want %v", s, got, want)
		}
	}
}

func TestBearerToken(t *testing.T) {
	cases := map[string]string{
		"Bearer fri_abc":  "fri_abc",
		"bearer fri_abc":  "fri_abc", // the scheme is case-insensitive
		"BEARER fri_abc":  "fri_abc",
		"Bearer  fri_abc": "fri_abc",
		"":                "",
		"fri_abc":         "", // no scheme
		"Basic fri_abc":   "",
		"Bearer":          "",
	}
	for header, want := range cases {
		if got := auth.BearerToken(header); got != want {
			t.Errorf("BearerToken(%q) = %q, want %q", header, got, want)
		}
	}
}

func TestSecretMatches(t *testing.T) {
	if !auth.SecretMatches("s3cret", "s3cret") {
		t.Error("an identical secret did not match")
	}
	if auth.SecretMatches("s3cret", "wrong") {
		t.Error("a different secret matched")
	}
	if auth.SecretMatches("s3cret", "s3cre") {
		t.Error("a prefix of the secret matched")
	}
	// An unset secret must never match, or leaving it blank opens the door.
	if auth.SecretMatches("", "") {
		t.Error("an unset secret matched an empty presentation")
	}
	if auth.SecretMatches("", "anything") {
		t.Error("an unset secret matched")
	}
}
