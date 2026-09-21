package api

import (
	"os"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"github.com/DhanushRamesh/friday/internal/auth"
)

// TestMain : Lowers the password work factor for this package's tests.
//
// bcrypt at its configured cost takes a fifth of a second, and more than two
// seconds under the race detector. Every test here creates an account and
// logs in, so the real cost turns the suite into minutes of waiting while
// proving nothing that internal/auth does not already assert.
func TestMain(m *testing.M) {
	auth.PasswordCost = bcrypt.MinCost
	os.Exit(m.Run())
}
