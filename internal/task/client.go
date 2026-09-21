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
	// MaxClientNameRunes : The longest name a client may give itself. It is a
	// label for a person reading a list, not a field to hold data in.
	MaxClientNameRunes = 100
)

// ErrClientNameTooLong : The name exceeded MaxClientNameRunes.
var ErrClientNameTooLong = errors.New("task: client name is too long")

// Client : One thing that talks to FRIDAY, such as a phone or a speaker.
//
// Conversations belong to a client, and one of them is active: a prompt lands
// there unless the caller says otherwise. The active conversation is held here
// rather than sent with every prompt, so a voice client need only say what it
// wants, not where it belongs.
type Client struct {
	// ID : The identifier, a ClientIDPrefix followed by a ULID.
	ID string
	// Name : What to call this client in a listing. May be empty.
	Name string
	// TokenHash : The stored form of the token this client authenticates
	// with. The token itself exists only at registration.
	TokenHash string
	// RevokedAt : When the client was revoked, or nil while it is usable.
	RevokedAt *time.Time
	// ActiveConversationID : Where a prompt from this client lands. Empty
	// only before the first conversation is created.
	ActiveConversationID string

	CreatedAt time.Time
	UpdatedAt time.Time
}

// NewClient : Creates a client that authenticates with the given token hash.
// The name is optional and is trimmed.
func NewClient(name, tokenHash string) (*Client, error) {
	name = strings.TrimSpace(name)
	if utf8.RuneCountInString(name) > MaxClientNameRunes {
		return nil, ErrClientNameTooLong
	}

	registered := now()
	return &Client{
		ID:        NewClientID(),
		Name:      name,
		TokenHash: tokenHash,
		CreatedAt: registered,
		UpdatedAt: registered,
	}, nil
}

// Revoked : Reports whether the client may no longer authenticate.
func (c *Client) Revoked() bool { return c.RevokedAt != nil }

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
