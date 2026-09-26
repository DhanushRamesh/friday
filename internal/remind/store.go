package remind

import (
	"context"
	"time"
)

// Store : Where reminders are kept.
type Store interface {
	// Create : Stores a new reminder.
	Create(ctx context.Context, r *Reminder) error

	// Get : Returns one reminder, or ErrNotFound.
	Get(ctx context.Context, userID, id string) (*Reminder, error)

	// List : A person's reminders in the given states, soonest first.
	// No states means every state.
	List(ctx context.Context, userID string, states ...Status) ([]Reminder, error)

	// Cancel : Calls one off. Cancelling one already cancelled, or already
	// fired, is not an error: the caller wanted it not to happen and it
	// will not happen.
	Cancel(ctx context.Context, userID, id string) error

	// Due : Pending reminders whose time has come, soonest first.
	//
	// This is what the firing loop asks, several times a minute, for ever.
	Due(ctx context.Context, at time.Time, limit int) ([]Reminder, error)

	// Fired : Records that a reminder was said.
	//
	// A zero next finishes it. Otherwise it is due again then, which is how
	// a repeating one comes back.
	Fired(ctx context.Context, id string, at time.Time, next time.Time) error

	// Missed : Records that a reminder's time passed with nothing listening,
	// too long ago to say now.
	Missed(ctx context.Context, id string, at time.Time) error
}

// DefaultDueLimit : The most reminders one pass of the firing loop takes.
//
// A bound rather than a guess at a maximum. If a hundred are somehow due at
// once, they are said over several passes rather than all in one breath.
const DefaultDueLimit = 20
