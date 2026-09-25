// Package announce speaks to a person without having been asked.
//
// Everything else the assistant says is a reply: something arrives, something
// goes back. This is the other direction — a timer that has finished, a
// conversation that has just been given a name, a window left open. The
// assistant has to be able to start a sentence, not only finish one.
package announce

import (
	"context"
	"log/slog"
)

// Announcer : Somewhere a sentence can be said aloud.
type Announcer interface {
	// Say : Speaks the message, or reports why it could not.
	//
	// A caller announcing something incidental should log a failure and carry
	// on: not being heard is not a reason to fail the thing being announced.
	Say(ctx context.Context, message string) error

	// Available : Whether anything is actually wired up. A caller can use
	// this to avoid composing a message nobody will hear.
	Available() bool
}

// Silent : An Announcer with nowhere to speak.
//
// The default, so that an unconfigured server behaves exactly as it did
// before there was anything to announce with, rather than failing.
type Silent struct {
	// Logger : Where the unspoken message goes instead, so that what would
	// have been said is still visible while this is being set up. Optional.
	Logger *slog.Logger
}

// Say : Records the message and reports success. Nothing was spoken.
func (s Silent) Say(ctx context.Context, message string) error {
	if s.Logger != nil {
		s.Logger.DebugContext(ctx, "nothing is configured to speak this",
			slog.String("message", message))
	}
	return nil
}

// Available : Always false. There is nowhere to say anything.
func (Silent) Available() bool { return false }
