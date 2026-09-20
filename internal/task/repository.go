package task

import (
	"context"
	"errors"
	"time"
)

// ErrNotFound : No task exists with the given identifier.
var ErrNotFound = errors.New("task: not found")

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

// Summary : A task without its response body.
//
// Listing tasks and checking on one both read far more often than they need
// the answer itself, and a response can run to megabytes.
type Summary struct {
	ID         string
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

	// FailRunning : Marks every task still recorded as running as failed,
	// with the given explanation, and reports how many were changed.
	//
	// It is called at startup. A process that stopped mid-task leaves rows
	// reading running that nothing will ever move, because whatever was
	// working on them is gone.
	FailRunning(ctx context.Context, reason string) (int64, error)
}
