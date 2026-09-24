package auth

import (
	"errors"
	"fmt"
	"sync"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"
)

const (
	// MinPasswordLen : The shortest password accepted. Length is the only
	// thing that reliably makes a password hard to guess, so it is the only
	// rule imposed; composition rules mostly produce predictable
	// substitutions.
	MinPasswordLen = 10

	// maxPasswordBytes : bcrypt silently ignores everything past 72 bytes, so
	// a longer password is refused rather than quietly truncated, which would
	// make two different passwords equivalent.
	maxPasswordBytes = 72

	// DefaultPasswordCost : bcrypt's work factor. Chosen so a single check
	// takes roughly two hundred milliseconds, which is unnoticeable to a
	// person logging in and ruinous to anyone trying passwords in bulk.
	DefaultPasswordCost = 12
)

// PasswordCost : The work factor actually used.
//
// Lower it only in tests. bcrypt is deliberately slow, and the race detector
// makes it more than ten times slower again, so a suite that hashes a
// password per case spends minutes proving nothing about the cost.
// Production must leave this alone: the slowness is the entire defence.
var PasswordCost = DefaultPasswordCost

// Errors reported when a password is unusable.
var (
	// ErrPasswordTooShort : The password was shorter than MinPasswordLen.
	ErrPasswordTooShort = errors.New("auth: password is too short")
	// ErrPasswordTooLong : The password exceeded what bcrypt will consider.
	ErrPasswordTooLong = errors.New("auth: password is too long")
)

// HashPassword : Returns the stored form of a password.
//
// bcrypt rather than the plain hash used for tokens: a password is chosen by
// a person and therefore guessable, so the cost of a slow hash is precisely
// the point, where for 256 bits of randomness it would buy nothing.
func HashPassword(password string) (string, error) {
	if err := CheckPasswordPolicy(password); err != nil {
		return "", err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), PasswordCost)
	if err != nil {
		return "", fmt.Errorf("auth: hashing password: %w", err)
	}
	return string(hash), nil
}

// CheckPasswordPolicy : Reports whether a password may be used.
func CheckPasswordPolicy(password string) error {
	if utf8.RuneCountInString(password) < MinPasswordLen {
		return fmt.Errorf("%w: at least %d characters", ErrPasswordTooShort, MinPasswordLen)
	}
	if len(password) > maxPasswordBytes {
		return fmt.Errorf("%w: at most %d bytes", ErrPasswordTooLong, maxPasswordBytes)
	}
	return nil
}

// PasswordMatches : Reports whether password produced hash.
//
// bcrypt's own comparison is used, which takes the same time whether the
// password is wrong in the first character or the last.
func PasswordMatches(hash, password string) bool {
	if hash == "" || password == "" {
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// dummyHash : A hash to compare against when there is no real one, built
// once at whatever cost is configured.
//
// It must be built at the configured cost rather than fixed, or the dummy
// check and the real one take visibly different times, which is the very
// thing it exists to prevent.
var dummyHash = sync.OnceValue(func() []byte {
	hash, err := bcrypt.GenerateFromPassword([]byte("bcrypt placeholder"), PasswordCost)
	if err != nil {
		// Only reachable with an invalid cost, which is a programming error.
		panic("auth: cannot build the placeholder hash: " + err.Error())
	}
	return hash
})

// DummyPasswordCheck : Spends the time a real check would, for a username
// that does not exist.
//
// Without it, logging in as an unknown user answers noticeably faster than
// logging in with a wrong password, which tells whoever is guessing which
// usernames are real.
func DummyPasswordCheck() {
	_ = bcrypt.CompareHashAndPassword(dummyHash(), []byte("not the password"))
}
