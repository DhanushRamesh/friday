// Package auth : Issues and verifies the tokens clients authenticate with.
//
// A token identifies a client and proves it is that client, so it replaces
// rather than accompanies any identifier sent alongside.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
)

const (
	// TokenPrefix : Marks a string as a client token, so one is recognisable
	// if it turns up somewhere it should not be.
	//
	// Changing it invalidates every token already issued: Require refuses
	// anything without it before the hash is ever looked up.
	TokenPrefix = "pa_"

	// tokenBytes : How much randomness a token carries. Two hundred and
	// fifty-six bits is far beyond guessing, which is what lets the stored
	// form be a plain hash rather than a slow password hash.
	tokenBytes = 32

	// HashLen : The length of a stored token hash, as hex.
	HashLen = sha256.Size * 2
)

// NewToken : Returns a fresh token and the hash to store for it.
//
// The token is the only copy that will ever exist: it is shown to the client
// once and never recoverable afterwards, because only its hash is kept.
func NewToken() (token, hash string, err error) {
	raw := make([]byte, tokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", "", fmt.Errorf("auth: generating token: %w", err)
	}

	token = TokenPrefix + base64.RawURLEncoding.EncodeToString(raw)
	return token, HashToken(token), nil
}

// HashToken : Returns the stored form of a token.
//
// A plain SHA-256 rather than a password hash: a token is 256 bits of
// randomness, so there is no dictionary to try and nothing for a slow hash to
// defend against, while bcrypt on every request would cost a hundred
// milliseconds for nothing.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// LooksLikeToken : Reports whether s has the shape of a token. It says nothing
// about whether such a token was ever issued.
func LooksLikeToken(s string) bool {
	if !strings.HasPrefix(s, TokenPrefix) {
		return false
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(s, TokenPrefix))
	return err == nil && len(raw) == tokenBytes
}

// BearerToken : Extracts the token from an Authorization header value,
// returning empty if the header is absent or not a bearer credential.
func BearerToken(header string) string {
	const scheme = "bearer "
	if len(header) < len(scheme) || !strings.EqualFold(header[:len(scheme)], scheme) {
		return ""
	}
	return strings.TrimSpace(header[len(scheme):])
}

// SecretMatches : Reports whether presented equals expected, in constant time.
//
// Used for the registration secret, which is one fixed value compared in
// process. A naive comparison returns faster the earlier it differs, which
// leaks the secret a character at a time to anyone able to measure it.
func SecretMatches(expected, presented string) bool {
	if expected == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(expected), []byte(presented)) == 1
}
