// Package task : Defines FRIDAY's unit of work.
//
// A task is created when a request arrives, runs in the background, and ends
// in exactly one terminal state. Callers observe a task only through its
// status, so the legal transitions between statuses are enforced here rather
// than left to whichever code happens to be writing a row.
package task

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/oklog/ulid/v2"
)

const (
	// IDPrefix : Marks an identifier as belonging to a task, so that one is
	// recognisable wherever it appears.
	IDPrefix = "task_"
	// idLen : The length of a prefixed identifier, being the prefix plus a
	// 26 character ULID.
	idLen = len(IDPrefix) + ulid.EncodedSize
	// MaxPromptRunes : The longest prompt accepted, bounding what a client
	// can push into the database and later into a model prompt.
	MaxPromptRunes = 16384
	// MaxResponseBytes : The largest response that can be stored. The column
	// holding it is a MEDIUMTEXT, which tops out near 16 MiB.
	MaxResponseBytes = 15 << 20
)

// StoredPrecision : The precision timestamps are kept at.
//
// Go clocks to the nanosecond and MySQL's DATETIME(3) columns to the
// millisecond, rounding what it is given. Left alone, a task in memory stops
// matching the row it was just written to, and every later comparison between
// the two is subtly wrong. Truncating here rather than rounding means the
// value the domain holds is exactly the value that will be stored.
const StoredPrecision = time.Millisecond

// now : Returns the current time at the precision timestamps are stored at.
func now() time.Time { return time.Now().UTC().Truncate(StoredPrecision) }

// Errors reported when a task cannot be created or completed.
var (
	// ErrEmptyPrompt : The prompt was empty or only whitespace.
	ErrEmptyPrompt = errors.New("task: prompt must not be empty")
	// ErrPromptTooLong : The prompt exceeded MaxPromptRunes.
	ErrPromptTooLong = errors.New("task: prompt is too long")
	// ErrResponseTooLarge : The response exceeded MaxResponseBytes and would
	// not survive being stored.
	ErrResponseTooLarge = errors.New("task: response is too large to store")
)

// Task : One unit of work submitted to FRIDAY.
type Task struct {
	// ID : The identifier, an IDPrefix followed by a ULID.
	ID string
	// ConversationID : The exchange this task belongs to, empty for a task
	// created before conversations existed.
	ConversationID string
	// Prompt : What the user asked for.
	Prompt string

	// Status : Where the task is in its lifecycle.
	Status Status
	// Response : The final answer. Set when the task completes.
	Response string
	// Error : Why the task failed, written for a user to read. Set when the
	// task fails.
	Error string

	// CreatedAt : When the task was accepted.
	CreatedAt time.Time
	// UpdatedAt : When the task last changed.
	UpdatedAt time.Time
	// StartedAt : When work began, or nil if it has not.
	StartedAt *time.Time
	// FinishedAt : When the task reached a terminal status, or nil if it has
	// not.
	FinishedAt *time.Time
}

// New : Creates a pending task from a user's prompt, belonging to the given
// conversation. Surrounding whitespace is removed.
func New(conversationID, prompt string) (*Task, error) {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return nil, ErrEmptyPrompt
	}
	if n := utf8.RuneCountInString(prompt); n > MaxPromptRunes {
		return nil, fmt.Errorf("%w: %d characters, limit is %d", ErrPromptTooLong, n, MaxPromptRunes)
	}

	created := now()
	return &Task{
		ID:             NewID(),
		ConversationID: conversationID,
		Prompt:         prompt,
		Status:         StatusPending,
		CreatedAt:      created,
		UpdatedAt:      created,
	}, nil
}

// NewID : Returns a fresh task identifier.
//
// ULIDs are used rather than random UUIDs because they sort by creation time,
// so task history comes back in order from an index scan without a sort.
func NewID() string { return IDPrefix + ulid.Make().String() }

// ValidID : Reports whether id is shaped like a task identifier. It checks the
// form only; no such task need exist.
func ValidID(id string) bool {
	if len(id) != idLen || !strings.HasPrefix(id, IDPrefix) {
		return false
	}
	_, err := ulid.ParseStrict(strings.TrimPrefix(id, IDPrefix))
	return err == nil
}

// Start : Moves a pending task into execution and records when work began.
func (t *Task) Start() error {
	if err := t.transitionTo(StatusRunning); err != nil {
		return err
	}
	started := t.UpdatedAt
	t.StartedAt = &started
	return nil
}

// Complete : Finishes the task successfully with the agent's final answer.
//
// A response too large to store is refused and the task is left unchanged, so
// that the caller can fail it with an explanation rather than have the write
// rejected by the database.
func (t *Task) Complete(response string) error {
	if len(response) > MaxResponseBytes {
		return fmt.Errorf("%w: %d bytes, limit is %d", ErrResponseTooLarge, len(response), MaxResponseBytes)
	}
	if err := t.transitionTo(StatusCompleted); err != nil {
		return err
	}
	t.Response = response
	return nil
}

// Fail : Finishes the task with an explanation.
//
// The reason is shown to the user, so it should describe what went wrong in
// plain language rather than carry a raw internal error.
func (t *Task) Fail(reason string) error {
	if err := t.transitionTo(StatusFailed); err != nil {
		return err
	}
	t.Error = reason
	return nil
}

// Cancel : Stops the task at the user's request.
func (t *Task) Cancel() error { return t.transitionTo(StatusCancelled) }

// Duration : Returns how long the task ran, or zero if it has not started.
// A task still running is measured to the present moment.
func (t *Task) Duration() time.Duration {
	if t.StartedAt == nil {
		return 0
	}
	if t.FinishedAt == nil {
		return time.Since(*t.StartedAt)
	}
	return t.FinishedAt.Sub(*t.StartedAt)
}

// transitionTo : Moves the task to next, rejecting an illegal change and
// leaving the task untouched when it does. It stamps UpdatedAt, and FinishedAt
// when the task reaches a terminal status.
func (t *Task) transitionTo(next Status) error {
	if !next.Valid() {
		return fmt.Errorf("task: %q is not a known status", next)
	}
	if !t.Status.CanTransitionTo(next) {
		return &TransitionError{From: t.Status, To: next}
	}

	changed := now()
	t.Status = next
	t.UpdatedAt = changed
	if next.IsTerminal() {
		t.FinishedAt = &changed
	}
	return nil
}
