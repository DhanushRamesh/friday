package task

import (
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/oklog/ulid/v2"
)

const (
	// ClientIDPrefix : Marks an identifier as belonging to a client.
	ClientIDPrefix = "cli_"
	// clientIDLen : The length of a prefixed client identifier.
	clientIDLen = len(ClientIDPrefix) + ulid.EncodedSize
	// MaxClientNameRunes : The longest name a client may be given. It is a
	// label for a person reading a list of their clients, not a field to hold
	// data in.
	MaxClientNameRunes = 100
)

// ErrClientNameTooLong : The name exceeded MaxClientNameRunes.
var ErrClientNameTooLong = errors.New("task: client name is too long")

// Client : One thing a user talks to FRIDAY through, such as a phone, a
// laptop or a speaker.
//
// A client is a credential and nothing more: it owns no sessions and is
// not a boundary between anyone. Its user is. Several exist per user so that
// a lost phone is one revocation rather than a password change.
//
// Each client holds its own active session, because a person may be
// speaking to a speaker in one room while typing at a laptop in another.
// Which sessions exist is a property of the user; which one a client is
// currently in is a property of the client.
type Client struct {
	// ID : The identifier, a ClientIDPrefix followed by a ULID.
	ID string
	// UserID : Whose client it is.
	UserID string
	// Name : What to call it in a listing. May be empty.
	Name string
	// TokenHash : The stored form of the token this client authenticates
	// with. The token itself exists only at the moment of logging in.
	TokenHash string
	// RevokedAt : When the client was revoked, or nil while it is usable.
	RevokedAt *time.Time
	// ActiveSessionID : Where a prompt from this client lands. Empty
	// only before the first session is created.
	ActiveSessionID string

	CreatedAt time.Time
	UpdatedAt time.Time
}

// NewClient : Creates a client belonging to a user, authenticating with the
// given token hash. The name is optional and is trimmed.
func NewClient(userID, name, tokenHash string) (*Client, error) {
	if userID == "" {
		return nil, errors.New("task: a client must belong to a user")
	}
	name = strings.TrimSpace(name)
	if utf8.RuneCountInString(name) > MaxClientNameRunes {
		return nil, ErrClientNameTooLong
	}

	registered := now()
	return &Client{
		ID:        NewClientID(),
		UserID:    userID,
		Name:      name,
		TokenHash: tokenHash,
		CreatedAt: registered,
		UpdatedAt: registered,
	}, nil
}

// Revoked : Reports whether the client may no longer authenticate.
func (d *Client) Revoked() bool { return d.RevokedAt != nil }

// NewClientID : Returns a fresh client identifier.
func NewClientID() string { return ClientIDPrefix + ulid.Make().String() }

// ValidClientID : Reports whether id is shaped like a client identifier. It
// checks the form only; no such client need exist.
func ValidClientID(id string) bool {
	if len(id) != clientIDLen || !strings.HasPrefix(id, ClientIDPrefix) {
		return false
	}
	_, err := ulid.ParseStrict(strings.TrimPrefix(id, ClientIDPrefix))
	return err == nil
}
