package chat

import (
	"errors"
	"fmt"
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
var ErrClientNameTooLong = errors.New("chat: client name is too long")

// Client : One thing a user talks to the server through, such as a phone, a
// laptop or a speaker.
//
// A client is a credential and nothing more: it owns no conversations and is
// not a boundary between anyone. Its user is. Several exist per user so that
// a lost phone is one revocation rather than a password change.
//
// Each client holds its own active conversation, because a person may be
// speaking to a speaker in one room while typing at a laptop in another.
// Which conversations exist is a property of the user; which one a client is
// currently in is a property of the client.
type Client struct {
	// ID : The identifier, a ClientIDPrefix followed by a ULID.
	ID string
	// UserID : Whose client it is.
	UserID string
	// Name : What to call it in a listing. May be empty.
	Name string

	// Channel : How this client's prompts arrive, and so what the assistant
	// may do about them.
	//
	// Declared when the client registers, because it is a property of the
	// thing holding the token and not of the endpoint it calls. A satellite
	// with a microphone cannot show what is about to happen and wait; a
	// client with a screen can, whatever wire format it speaks.
	Channel Channel
	// Model : Which model answers this client's prompts. The zero value
	// leaves it to the server's configuration.
	//
	// Per client rather than per account, because what suits one does not
	// suit another: a spoken answer has to arrive before the satellite gives
	// up waiting, while a browser can wait for a slower and better one.
	Model Model
	// TokenHash : The stored form of the token this client authenticates
	// with. The token itself exists only at the moment of logging in.
	TokenHash string
	// RevokedAt : When the client was revoked, or nil while it is usable.
	RevokedAt *time.Time
	// ActiveConversationID : Where a prompt from this client lands. Empty
	// only before the first conversation is created.
	ActiveConversationID string

	CreatedAt time.Time
	UpdatedAt time.Time
}

// NewClient : Creates a client belonging to a user, authenticating with the
// given token hash. The name is optional and is trimmed.
func NewClient(userID, name, tokenHash string, channel Channel) (*Client, error) {
	if userID == "" {
		return nil, errors.New("chat: a client must belong to a user")
	}
	if !channel.Valid() {
		return nil, fmt.Errorf("%w: %q", ErrUnknownChannel, channel)
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
		Channel:   channel,
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
