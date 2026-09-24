package session

import "context"

// Repository : Where a session's messages are kept.
type Repository interface {
	// Append : Stores a message at the end of its session and returns it
	// with Seq filled in.
	//
	// The position is assigned here rather than by the caller, because two
	// callers appending at once must not be given the same one.
	Append(ctx context.Context, m Message) (Message, error)

	// Before : Returns a session's messages up to but not including the
	// given position, oldest first.
	//
	// A caller reads the history it is about to answer by appending the
	// question first and asking for everything before it. That is what keeps
	// the question from arriving twice — once as the last thing said and
	// again as the prompt.
	Before(ctx context.Context, sessionID string, seq int) ([]Message, error)

	// All : Returns everything said in a session, oldest first.
	All(ctx context.Context, sessionID string) ([]Message, error)
}
