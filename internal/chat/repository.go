package chat

import (
	"context"
	"errors"
	"time"
)

var (
	// ErrNotFound : Nothing exists with the given identifier.
	ErrNotFound = errors.New("chat: not found")
	// ErrRevoked : The client exists but may no longer authenticate.
	ErrRevoked = errors.New("chat: client is revoked")
	// ErrNotOwned : It exists but belongs to a different user.
	//
	// Distinguished from ErrNotFound inside FRIDAY so that a mistake is
	// diagnosable; at the edge both are answered the same way, because
	// telling one user that another's session exists reveals more than
	// it should.
	ErrNotOwned = errors.New("chat: belongs to another user")
)

// Message : One thing recorded during a chat's run.
//
// Only transient messages are stored here. A chat's result lives in its
// Response and a failure in its Error, so a large answer is held once rather
// than twice.
type Message struct {
	// ChatID : The chat the message belongs to.
	ChatID string
	// Seq : Position within the chat's stream, starting at 1.
	Seq int
	// Kind : What sort of message this is, holding a provider.Kind value.
	Kind string
	// Text : What was said, written to be spoken aloud.
	Text string
	// CreatedAt : When it was recorded.
	CreatedAt time.Time
}

// SessionSummary : A session with the chats belonging to it.
type SessionSummary struct {
	Session Session
	Chats   []Summary
}

// Summary : A chat without its response body.
//
// Listing chats and checking on one both read far more often than they need
// the answer itself, and a response can run to megabytes.
type Summary struct {
	ID         string
	SessionID  string
	Prompt     string
	Status     Status
	Error      string
	CreatedAt  time.Time
	UpdatedAt  time.Time
	StartedAt  *time.Time
	FinishedAt *time.Time
}

// Filter : Narrows a listing of chats.
type Filter struct {
	// Status : Restricts the listing to one status. Empty means any.
	Status Status
	// SessionID : Restricts the listing to one session. Empty means
	// any.
	SessionID string
	// UserID : Restricts the listing to one user's chats. Empty means any.
	UserID string
	// Limit : The greatest number of chats to return. Zero selects
	// DefaultListLimit.
	Limit int
}

const (
	// DefaultListLimit : How many chats a listing returns when no limit is
	// given.
	DefaultListLimit = 50
	// MaxListLimit : The largest listing that will be returned, whatever is
	// asked for.
	MaxListLimit = 200
)

// Repository : Stores and retrieves chats and the messages produced while
// running them.
//
// Implementations are safe for concurrent use.
type Repository interface {
	// Create : Stores a new chat. It reports an error if one already exists
	// with the same identifier.
	Create(ctx context.Context, t *Chat) error

	// Get : Returns the chat with the given identifier, including its
	// response. It reports ErrNotFound if there is none.
	Get(ctx context.Context, id string) (*Chat, error)

	// Update : Writes a chat's current state over the stored one. It reports
	// ErrNotFound if the chat has since been removed.
	Update(ctx context.Context, t *Chat) error

	// List : Returns chats in reverse order of creation, newest first,
	// without their responses.
	List(ctx context.Context, f Filter) ([]Summary, error)

	// AppendMessage : Records a message against a chat, assigning it the next
	// position in that chat's stream. It reports ErrNotFound if the chat does
	// not exist.
	AppendMessage(ctx context.Context, chatID, kind, text string) (Message, error)

	// Messages : Returns a chat's messages in the order they were produced.
	Messages(ctx context.Context, chatID string) ([]Message, error)

	// CreateUser : Stores a new user. It reports ErrUsernameTaken if the
	// username is already in use.
	CreateUser(ctx context.Context, u *User) error

	// UserByUsername : Returns the user with the given username. It reports
	// ErrNotFound if there is none.
	UserByUsername(ctx context.Context, username string) (*User, error)

	// GetUser : Returns a user by identifier. It reports ErrNotFound if
	// there is none.
	GetUser(ctx context.Context, id string) (*User, error)

	// CreateClient : Stores a new client.
	CreateClient(ctx context.Context, d *Client) error

	// ClientByTokenHash : Returns the client authenticating with the given
	// token hash. It reports ErrNotFound if there is none, and ErrRevoked if
	// the client was revoked.
	ClientByTokenHash(ctx context.Context, tokenHash string) (*Client, error)

	// ListClients : Returns a user's clients, newest first, including
	// revoked ones so that a revocation is visible.
	ListClients(ctx context.Context, userID string) ([]Client, error)

	// RevokeClient : Stops a client authenticating. It reports ErrNotFound if
	// there is none, and ErrNotOwned if it belongs to another user. Revoking
	// one already revoked changes nothing.
	RevokeClient(ctx context.Context, userID, clientID string) error

	// SetActiveSession : Makes a session the one a prompt from this
	// client lands in. It reports ErrNotFound if either does not exist, and
	// ErrNotOwned if the session belongs to another user.
	SetActiveSession(ctx context.Context, userID, clientID, sessionID string) error

	// CreateSession : Stores a new session.
	CreateSession(ctx context.Context, c *Session) error

	// GetSession : Returns a session. It reports ErrNotFound if
	// there is none.
	GetSession(ctx context.Context, id string) (*Session, error)

	// ListSessions : Returns a user's sessions, most recently used
	// first. They belong to the person, so every one of their clients sees
	// all of them.
	ListSessions(ctx context.Context, userID string, limit int) ([]Session, error)

	// Unfinished : Returns the identifiers of a session's chats that
	// have not reached a terminal status, oldest first.
	Unfinished(ctx context.Context, sessionID string) ([]string, error)

	// FailRunning : Marks every chat still recorded as running as failed,
	// with the given explanation, and reports how many were changed.
	//
	// It is called at startup. A process that stopped mid-chat leaves rows
	// reading running that nothing will ever move, because whatever was
	// working on them is gone.
	FailRunning(ctx context.Context, reason string) (int64, error)
}
