package task

import (
	"context"
	"errors"
	"time"
)

var (
	// ErrNotFound : Nothing exists with the given identifier.
	ErrNotFound = errors.New("task: not found")
	// ErrRevoked : The client exists but may no longer authenticate.
	ErrRevoked = errors.New("task: client is revoked")
	// ErrNotOwned : It exists but belongs to a different user.
	//
	// Distinguished from ErrNotFound inside FRIDAY so that a mistake is
	// diagnosable; at the edge both are answered the same way, because
	// telling one user that another's session exists reveals more than
	// it should.
	ErrNotOwned = errors.New("task: belongs to another user")
)

// Message : One thing recorded during a task's run.
//
// Only transient messages are stored here. A task's result lives in its
// Response and a failure in its Error, so a large answer is held once rather
// than twice.
type Message struct {
	// TaskID : The task the message belongs to.
	TaskID string
	// Seq : Position within the task's stream, starting at 1.
	Seq int
	// Kind : What sort of message this is, holding a provider.Kind value.
	Kind string
	// Text : What was said, written to be spoken aloud.
	Text string
	// CreatedAt : When it was recorded.
	CreatedAt time.Time
}

// SessionSummary : A session with the tasks belonging to it.
type SessionSummary struct {
	Session Session
	Tasks   []Summary
}

// Summary : A task without its response body.
//
// Listing tasks and checking on one both read far more often than they need
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

// Filter : Narrows a listing of tasks.
type Filter struct {
	// Status : Restricts the listing to one status. Empty means any.
	Status Status
	// SessionID : Restricts the listing to one session. Empty means
	// any.
	SessionID string
	// UserID : Restricts the listing to one user's tasks. Empty means any.
	UserID string
	// Limit : The greatest number of tasks to return. Zero selects
	// DefaultListLimit.
	Limit int
}

const (
	// DefaultListLimit : How many tasks a listing returns when no limit is
	// given.
	DefaultListLimit = 50
	// MaxListLimit : The largest listing that will be returned, whatever is
	// asked for.
	MaxListLimit = 200
)

// Repository : Stores and retrieves tasks and the messages produced while
// running them.
//
// Implementations are safe for concurrent use.
type Repository interface {
	// Create : Stores a new task. It reports an error if one already exists
	// with the same identifier.
	Create(ctx context.Context, t *Task) error

	// Get : Returns the task with the given identifier, including its
	// response. It reports ErrNotFound if there is none.
	Get(ctx context.Context, id string) (*Task, error)

	// Update : Writes a task's current state over the stored one. It reports
	// ErrNotFound if the task has since been removed.
	Update(ctx context.Context, t *Task) error

	// List : Returns tasks in reverse order of creation, newest first,
	// without their responses.
	List(ctx context.Context, f Filter) ([]Summary, error)

	// AppendMessage : Records a message against a task, assigning it the next
	// position in that task's stream. It reports ErrNotFound if the task does
	// not exist.
	AppendMessage(ctx context.Context, taskID, kind, text string) (Message, error)

	// Messages : Returns a task's messages in the order they were produced.
	Messages(ctx context.Context, taskID string) ([]Message, error)

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

	// History : Returns a session's turns, oldest first, limited to the
	// most recent turns. A task that was cancelled or failed contributes its
	// prompt but no answer, which is what lets a correction be understood.
	History(ctx context.Context, sessionID string, turns int) ([]Turn, error)

	// Unfinished : Returns the identifiers of a session's tasks that
	// have not reached a terminal status, oldest first.
	Unfinished(ctx context.Context, sessionID string) ([]string, error)

	// FailRunning : Marks every task still recorded as running as failed,
	// with the given explanation, and reports how many were changed.
	//
	// It is called at startup. A process that stopped mid-task leaves rows
	// reading running that nothing will ever move, because whatever was
	// working on them is gone.
	FailRunning(ctx context.Context, reason string) (int64, error)
}
