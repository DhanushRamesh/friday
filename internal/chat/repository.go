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
	// Distinguished from ErrNotFound inside the server so that a mistake is
	// diagnosable; at the edge both are answered the same way, because
	// telling one user that another's session exists reveals more than
	// it should.
	ErrNotOwned = errors.New("chat: belongs to another user")
)

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
	Channel    Channel
	Status     Status
	Error      string
	ErrorCode  string
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
	// [revoked] chooses which listing: the clients still usable, or the ones
	// that are not. Never both, because a revoked client is not a lesser
	// version of a live one — it cannot authenticate and cannot be brought
	// back, so it is a record rather than something to act on.
	ListClients(ctx context.Context, userID string, revoked bool) ([]Client, error)

	// ReissueClientToken : Replaces a client's token with a new one, so
	// signing in again on the same install keeps one client rather than
	// leaving a trail of them.
	//
	// The previous token stops working, which is the point: one client is one
	// credential. It reports ErrNotFound if there is no such client or it has
	// been revoked, and ErrNotOwned if it belongs to somebody else.
	ReissueClientToken(ctx context.Context, userID, clientID, tokenHash string) (*Client, error)

	// SetClientChannel : Changes how a client's prompts are treated. It
	// reports ErrNotFound if there is no such client, ErrNotOwned if it
	// belongs to somebody else, and ErrUnknownChannel for a channel that is
	// not one of the two.
	SetClientChannel(ctx context.Context, userID, clientID string, channel Channel) error

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

	// RenameSession : Changes a session's title. It reports ErrNotFound if
	// there is no such session, and ErrNotOwned if it belongs to somebody
	// else.
	RenameSession(ctx context.Context, userID, sessionID, title string) error

	// SetSessionArchived : Puts a session away or brings it back. It reports
	// ErrNotFound if there is no such session, and ErrNotOwned if it belongs
	// to somebody else.
	//
	// Archiving clears it from any client that was pointed at it, so that a
	// prompt does not land in a session the user has put away.
	SetSessionArchived(ctx context.Context, userID, sessionID string, archived bool) error

	// DeleteSession : Removes a session and everything said in it. It reports
	// ErrNotFound if there is no such session, and ErrNotOwned if it belongs
	// to somebody else.
	//
	// The chats and the transcript go with it, by the cascade on their
	// foreign keys. Nothing here can be undone.
	DeleteSession(ctx context.Context, userID, sessionID string) error

	// GetSession : Returns a session. It reports ErrNotFound if
	// there is none.
	GetSession(ctx context.Context, id string) (*Session, error)

	// ListSessions : Returns a user's sessions, most recently used
	// first. They belong to the person, so every one of their clients sees
	// all of them.
	ListSessions(ctx context.Context, userID string, limit int) ([]Session, error)

	// ListArchivedSessions : Returns a user's archived sessions, most
	// recently used first.
	ListArchivedSessions(ctx context.Context, userID string, limit int) ([]Session, error)

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
