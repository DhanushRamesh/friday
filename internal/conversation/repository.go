package conversation

import "context"

// Repository : Where a conversation's messages are kept.
type Repository interface {
	// Append : Stores a message at the end of its conversation and returns it
	// with Seq filled in.
	//
	// The position is assigned here rather than by the caller, because two
	// callers appending at once must not be given the same one.
	Append(ctx context.Context, m Message) (Message, error)

	// Before : Returns a conversation's messages up to but not including the
	// given position, oldest first.
	//
	// A caller reads the history it is about to answer by appending the
	// question first and asking for everything before it. That is what keeps
	// the question from arriving twice — once as the last thing said and
	// again as the prompt.
	Before(ctx context.Context, conversationID string, seq int) ([]Message, error)

	// All : Returns everything said in a conversation, oldest first.
	All(ctx context.Context, conversationID string) ([]Message, error)

	// ByChat : Returns everything one turn wrote, oldest first. Empty for a
	// turn from before messages recorded which one wrote them.
	ByChat(ctx context.Context, chatID string) ([]Message, error)

	// Summary : Returns the conversation's condensed earlier conversation. A
	// conversation with none yields the zero Summary and no error.
	Summary(ctx context.Context, conversationID string) (Summary, error)

	// SetSummary : Replaces the conversation's condensed earlier conversation.
	SetSummary(ctx context.Context, conversationID string, s Summary) error
}
