package task

import (
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
)

const (
	// UserIDPrefix : Marks an identifier as belonging to a user.
	UserIDPrefix = "usr_"
	// userIDLen : The length of a prefixed user identifier.
	userIDLen = len(UserIDPrefix) + ulid.EncodedSize

	// MinUsernameLen : The shortest username accepted.
	MinUsernameLen = 2
	// MaxUsernameLen : The longest username accepted.
	MaxUsernameLen = 64
)

// usernamePattern : What a username may contain. Deliberately narrow: a
// username is typed at a login prompt and compared for uniqueness, and
// characters that render alike invite one account being mistaken for another.
var usernamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)

// Errors reported when a user cannot be created.
var (
	// ErrInvalidUsername : The username was empty, too long, or held
	// characters that are not allowed.
	ErrInvalidUsername = errors.New("task: username is not valid")
	// ErrUsernameTaken : Another user already has that username.
	ErrUsernameTaken = errors.New("task: username is taken")
)

// User : The person FRIDAY belongs to.
//
// Sessions belong to a user rather than to any one client, so an
// exchange begun on a phone can be continued at a desk.
type User struct {
	// ID : The identifier, a UserIDPrefix followed by a ULID.
	ID string
	// Username : What they log in as, lowercase.
	Username string
	// PasswordHash : The bcrypt hash of their password.
	PasswordHash string

	CreatedAt time.Time
	UpdatedAt time.Time
}

// NewUser : Creates a user. The username is lowercased and must consist of
// letters, digits, dots, dashes or underscores, beginning with a letter or
// digit.
func NewUser(username, passwordHash string) (*User, error) {
	username = strings.ToLower(strings.TrimSpace(username))
	if len(username) < MinUsernameLen || len(username) > MaxUsernameLen {
		return nil, ErrInvalidUsername
	}
	if !usernamePattern.MatchString(username) {
		return nil, ErrInvalidUsername
	}
	if passwordHash == "" {
		return nil, errors.New("task: a password hash is required")
	}

	created := now()
	return &User{
		ID:           NewUserID(),
		Username:     username,
		PasswordHash: passwordHash,
		CreatedAt:    created,
		UpdatedAt:    created,
	}, nil
}

// NewUserID : Returns a fresh user identifier.
func NewUserID() string { return UserIDPrefix + ulid.Make().String() }

// ValidUserID : Reports whether id is shaped like a user identifier.
func ValidUserID(id string) bool {
	if len(id) != userIDLen || !strings.HasPrefix(id, UserIDPrefix) {
		return false
	}
	_, err := ulid.ParseStrict(strings.TrimPrefix(id, UserIDPrefix))
	return err == nil
}

// NormaliseUsername : Returns the form a username is stored and compared in.
func NormaliseUsername(username string) string {
	return strings.ToLower(strings.TrimSpace(username))
}
