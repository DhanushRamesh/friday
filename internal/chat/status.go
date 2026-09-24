package task

import "fmt"

// Status : The lifecycle state of a task.
type Status string

const (
	// StatusPending : Accepted and queued. No work has begun.
	StatusPending Status = "pending"
	// StatusRunning : Being worked on.
	StatusRunning Status = "running"
	// StatusCompleted : Finished successfully. Response is set.
	StatusCompleted Status = "completed"
	// StatusFailed : Stopped by an error. Error is set.
	StatusFailed Status = "failed"
	// StatusCancelled : Stopped at the user's request.
	StatusCancelled Status = "cancelled"
)

// allowedTransitions : The statuses each status may move to. A status absent
// from this map permits no further change.
var allowedTransitions = map[Status][]Status{
	StatusPending: {StatusRunning, StatusFailed, StatusCancelled},
	StatusRunning: {StatusCompleted, StatusFailed, StatusCancelled},
}

// Valid : Reports whether s is a known status.
func (s Status) Valid() bool {
	switch s {
	case StatusPending, StatusRunning, StatusCompleted, StatusFailed, StatusCancelled:
		return true
	default:
		return false
	}
}

// IsTerminal : Reports whether s is an end state, from which no transition is
// possible.
func (s Status) IsTerminal() bool {
	switch s {
	case StatusCompleted, StatusFailed, StatusCancelled:
		return true
	default:
		return false
	}
}

// CanTransitionTo : Reports whether a task may move from s to next.
func (s Status) CanTransitionTo(next Status) bool {
	for _, allowed := range allowedTransitions[s] {
		if allowed == next {
			return true
		}
	}
	return false
}

// String : Returns the status as written in the database and the API.
func (s Status) String() string { return string(s) }

// TransitionError : Reports an attempt to make an illegal status change.
type TransitionError struct {
	From Status
	To   Status
}

// Error : Describes the rejected transition, noting when the cause is that the
// task had already finished.
func (e *TransitionError) Error() string {
	if e.From.IsTerminal() {
		return fmt.Sprintf("task: cannot move from %s to %s: %s is a final state", e.From, e.To, e.From)
	}
	return fmt.Sprintf("task: cannot move from %s to %s", e.From, e.To)
}
