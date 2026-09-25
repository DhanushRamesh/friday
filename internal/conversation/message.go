// Package conversation holds what was said in a conversation.
//
// It is deliberately separate from the work that produced it. A conversation is a
// log of messages; how any one of them came to be written — which request,
// which provider, how long it took — is somebody else's concern.
package conversation

import (
	"errors"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
)

// maxContentBytes : The longest a single message may be.
//
// The column is MEDIUMTEXT, which holds sixteen megabytes. The limit is well
// below that because a message this long is a fault rather than an answer,
// and it is better refused at the edge than truncated in the database.
const maxContentBytes = 1 << 20

var (
	// ErrTooLarge : Returned when a message's content will not fit.
	ErrTooLarge = errors.New("conversation: the message is too long to store")

	// ErrNoConversation : Returned when the conversation written to does not exist.
	ErrNoConversation = errors.New("conversation: no such conversation")
)

// Kind : Whether a message is part of the conversation or a report that
// something went wrong.
type Kind string

const (
	// Chat : Something said, by either side. These are the messages a model
	// is given.
	Chat Kind = "chat"

	// Failure : The assistant reporting that it could not answer.
	//
	// It is given to a model as well as shown, which it was not before. The
	// reason it was withheld was that a bare "something went wrong" read back
	// as conversation makes the model explain an outage it had no part in and
	// invent detail to fill the gap. What closes that gap is Detail: with the
	// exact error present there is nothing left to invent, and "what exactly
	// failed?" becomes answerable out loud.
	Failure Kind = "error"

	// Interruption : The turn was stopped part-way by the person.
	//
	// Unlike a Failure this is given to a model, because the model has to
	// know the turn did not finish. Dropping the question instead would be
	// simpler while a turn is only ever text — nothing happened, so nothing
	// is lost. It stops being true the moment a turn can act: half a chain
	// of tool calls may already have run and persisted its effects, and a
	// history that omits the request leaves the model contradicting a world
	// it changed.
	Interruption Kind = "stopped"
)

// known : Whether this is a kind the store will accept.
//
// Listed here rather than checked inline in Valid, so that adding a kind and
// forgetting to allow it is one edit rather than two. It was two, and an
// interruption was rejected by the store for a morning without anything
// louder than a line in the log.
func (k Kind) known() bool {
	return k == Chat || k == Failure || k == Interruption
}

// Role : Who said something.
type Role string

const (
	// User : The person asking.
	User Role = "user"
	// Assistant : The assistant answering.
	Assistant Role = "assistant"
)

// MessageIDPrefix : Marks an identifier as belonging to a message.
const MessageIDPrefix = "msg_"

// NewMessageID : Returns a fresh message identifier.
func NewMessageID() string { return MessageIDPrefix + ulid.Make().String() }

// Message : One thing said in a conversation.
type Message struct {
	// ID : The identifier, a MessageIDPrefix followed by a ULID.
	//
	// Separate from the position because a position is not an identity: seq
	// orders the conversation and moves if anything is ever removed from the
	// middle of one, while this names the same message afterwards.
	ID string
	// ConversationID : The conversation it belongs to.
	ConversationID string
	// Seq : Position within the conversation, starting at 1. Assigned when the
	// message is stored, so it is zero until then.
	Seq int
	// Kind : Whether this is conversation or a reported failure.
	Kind Kind
	// Role : Who said it.
	Role Role
	// Content : What was said.
	Content string
	// Detail : The exact error behind a Failure, kept out of Content so that
	// what is read aloud stays short. Empty for everything else.
	Detail string
	// At : When it was said.
	At time.Time
}

// Said : A message from the person.
func Said(conversationID, content string, at time.Time) Message {
	return Message{
		ID:             NewMessageID(),
		ConversationID: conversationID,
		Kind:           Chat,
		Role:           User,
		Content:        content,
		At:             at,
	}
}

// Answered : A message from the server.
func Answered(conversationID, content string, at time.Time) Message {
	return Message{
		ID:             NewMessageID(),
		ConversationID: conversationID,
		Kind:           Chat,
		Role:           Assistant,
		Content:        content,
		At:             at,
	}
}

// Interrupted : A note that the person stopped the turn before it finished.
//
// Written as the assistant's own turn so the roles still alternate, and
// worded as a statement of what happened rather than an apology: it is read
// back to a model, which should treat it as a fact about the conversation and
// not as something to make up for.
func Interrupted(conversationID string, at time.Time) Message {
	return Message{
		ID:             NewMessageID(),
		ConversationID: conversationID,
		Kind:           Interruption,
		Role:           Assistant,
		Content:        "[The person stopped this before it finished.]",
		At:             at,
	}
}

// Failed : The assistant reporting that it could not answer.
//
// [detail] is what the service actually said, and may be empty when nothing
// more is known than the sentence.
func Failed(conversationID, content, detail string, at time.Time) Message {
	return Message{
		ID:             NewMessageID(),
		ConversationID: conversationID,
		Kind:           Failure,
		Detail:         strings.TrimSpace(detail),
		Role:           Assistant,
		Content:        content,
		At:             at,
	}
}

// Valid : Reports whether a message can be stored, and why not if it cannot.
func (m Message) Valid() error {
	switch {
	case m.ID == "":
		return errors.New("conversation: a message needs an identifier")
	case m.ConversationID == "":
		return errors.New("conversation: a message needs a conversation")
	case strings.TrimSpace(m.Content) == "":
		return errors.New("conversation: a message needs something in it")
	case len(m.Content) > maxContentBytes:
		return ErrTooLarge
	case !m.Kind.known():
		return errors.New("conversation: a message needs a kind")
	case m.Role != User && m.Role != Assistant:
		return errors.New("conversation: a message needs a speaker")
	}
	return nil
}

// ForModel : The messages a provider is given, oldest first.
//
// Everything is given, failures included: see Failure for why they no longer
// are not. A failure is rendered with its detail appended, because the
// sentence alone is what made a model invent the rest. Consecutive messages by the same speaker
// are joined, because a question that was superseded contributes no answer
// and two questions would otherwise sit side by side — which providers that
// require the roles to alternate reject, and which reads correctly joined
// anyway, a question and its correction being one request.
func ForModel(messages []Message) []Message {
	kept := make([]Message, 0, len(messages))
	for _, m := range messages {
		if strings.TrimSpace(m.Content) == "" {
			continue
		}
		m.Content = forModelContent(m)
		if n := len(kept); n > 0 && kept[n-1].Role == m.Role {
			// The joined message keeps the earlier time and position: that
			// is when the speaker started saying all of it.
			kept[n-1].Content += "\n\n" + m.Content
			continue
		}
		kept = append(kept, m)
	}
	return kept
}

// forModelContent : What a message reads as when given to a model.
//
// A failure carries its exact error inline, marked as the detail it is. The
// model is being told what the service said, not being handed something to
// repeat: a spoken answer should still be the sentence, and the detail is
// there so that asking for it gets the truth.
func forModelContent(m Message) string {
	if m.Kind != Failure || m.Detail == "" {
		return m.Content
	}
	return m.Content + "\n\n[Exact error, for reference if asked: " + m.Detail + "]"
}

// ForPerson : The messages a person sees, oldest first.
//
// Everything is shown, failures included. The two views differ today only in
// that one hides failures, but they are separate functions because they
// answer separate questions, and only one of them is allowed to change when
// a provider demands something.
func ForPerson(messages []Message) []Message {
	shown := make([]Message, 0, len(messages))
	for _, m := range messages {
		if strings.TrimSpace(m.Content) == "" {
			continue
		}
		shown = append(shown, m)
	}
	return shown
}
