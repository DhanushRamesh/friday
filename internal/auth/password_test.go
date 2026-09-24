package auth_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/DhanushRamesh/personal-assistant/internal/auth"
)

func TestHashAndMatch(t *testing.T) {
	const password = "correct horse battery staple"

	hash, err := auth.HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if strings.Contains(hash, password) {
		t.Error("the hash contains the password")
	}
	if !auth.PasswordMatches(hash, password) {
		t.Error("the password did not match its own hash")
	}
	if auth.PasswordMatches(hash, password+"x") {
		t.Error("a different password matched")
	}
	if auth.PasswordMatches(hash, "") {
		t.Error("an empty password matched")
	}
	if auth.PasswordMatches("", password) {
		t.Error("an empty hash matched")
	}
}

// Two hashes of one password must differ, or identical passwords would be
// visible as identical hashes.
func TestHashesAreSalted(t *testing.T) {
	const password = "correct horse battery staple"

	first, err := auth.HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	second, err := auth.HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}

	if first == second {
		t.Error("hashing the same password twice gave the same hash")
	}
	if !auth.PasswordMatches(second, password) {
		t.Error("the second hash does not verify")
	}
}

func TestPasswordPolicy(t *testing.T) {
	short := strings.Repeat("a", auth.MinPasswordLen-1)
	if err := auth.CheckPasswordPolicy(short); !errors.Is(err, auth.ErrPasswordTooShort) {
		t.Errorf("error = %v, want ErrPasswordTooShort", err)
	}
	if err := auth.CheckPasswordPolicy(strings.Repeat("a", auth.MinPasswordLen)); err != nil {
		t.Errorf("a password of exactly the minimum was refused: %v", err)
	}

	// bcrypt ignores anything past 72 bytes, so a longer one is refused
	// rather than quietly truncated into an equivalent of its prefix.
	long := strings.Repeat("a", 73)
	if err := auth.CheckPasswordPolicy(long); !errors.Is(err, auth.ErrPasswordTooLong) {
		t.Errorf("error = %v, want ErrPasswordTooLong", err)
	}
	if _, err := auth.HashPassword(long); !errors.Is(err, auth.ErrPasswordTooLong) {
		t.Errorf("HashPassword accepted an over-long password: %v", err)
	}
}

// Hashing must be deliberately slow, which is a property of the cost factor
// rather than of the clock: the race detector inflates bcrypt by more than
// tenfold, so timing it in a test measures the detector, not the setting.
//
// At the configured cost a single hash takes roughly two hundred
// milliseconds unraced: unnoticeable to someone logging in, ruinous to
// anyone trying passwords in bulk.
func TestHashingUsesADeliberateCost(t *testing.T) {
	hash, err := auth.HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}

	cost, err := bcrypt.Cost([]byte(hash))
	if err != nil {
		t.Fatalf("bcrypt.Cost: %v", err)
	}
	if cost < 11 {
		t.Errorf("cost = %d, too low to slow down guessing", cost)
	}
	if cost > 13 {
		t.Errorf("cost = %d, so high that logging in would be unpleasant", cost)
	}
	if cost <= bcrypt.DefaultCost {
		t.Errorf("cost = %d, want more than bcrypt's default of %d", cost, bcrypt.DefaultCost)
	}
}

// An unknown username must not answer faster than a wrong password, or
// whoever is guessing learns which usernames exist.
func TestDummyCheckCostsAboutTheSame(t *testing.T) {
	hash, err := auth.HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}

	start := time.Now()
	auth.PasswordMatches(hash, "wrong password entirely")
	real := time.Since(start)

	start = time.Now()
	auth.DummyPasswordCheck()
	dummy := time.Since(start)

	// Within an order of magnitude is enough to hide which path was taken.
	if dummy*10 < real {
		t.Errorf("the dummy check took %v against a real %v, which is distinguishable", dummy, real)
	}
}
