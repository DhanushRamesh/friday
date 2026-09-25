// Package provider : Defines the engines that carry out a chat.
//
// A provider is whatever can turn a prompt into an answer: Claude, GPT, a
// local model, or the stub in this package. Which one runs a given chat is a
// routing decision made elsewhere; a chat does not know or care which answered
// it.
//
// A run is a stream rather than a single reply, because an answer can take
// long enough that the user needs to hear something before it arrives.
package environment

import (
	"context"
	"time"
)

// Kind : Whether a message is progress, the result, or a failure.
type Kind string

const (
	// KindUpdate : Transient progress. More messages will follow.
	KindUpdate Kind = "update"
	// KindFinal : The result. The stream ends after it.
	KindFinal Kind = "final"
	// KindError : The run failed. The stream ends after it.
	KindError Kind = "error"
)

// Valid : Reports whether k is a known kind.
func (k Kind) Valid() bool {
	switch k {
	case KindUpdate, KindFinal, KindError:
		return true
	default:
		return false
	}
}

// Terminal : Reports whether a message of this kind ends the stream.
func (k Kind) Terminal() bool { return k == KindFinal || k == KindError }

// String : Returns the kind as written in the database and the API.
func (k Kind) String() string { return string(k) }

// Message : One thing a provider has to say during a run.
type Message struct {
	// Kind : Whether this is progress, the result, or a failure.
	Kind Kind
	// Text : What to show the user. For KindError this is the explanation
	// they see, so it is written in plain language.
	Text string
	// Code : For KindError, which kind of failure it was, as a failure.Code.
	// Empty otherwise.
	Code string
	// Detail : For KindError, what the service actually said, kept exactly.
	// Empty otherwise, and never the thing shown without being asked for.
	Detail string
	// At : When the provider produced the message.
	At time.Time
}

// Update : Returns a transient progress message.
func Update(text string) Message {
	return Message{Kind: KindUpdate, Text: text, At: time.Now().UTC()}
}

// Final : Returns the message carrying a run's result.
func Final(text string) Message {
	return Message{Kind: KindFinal, Text: text, At: time.Now().UTC()}
}

// Failure : Returns the message ending a run that could not produce a result.
//
// [code] and [detail] carry what kind of failure it was and what the service
// actually said. Both may be empty when a caller knows no more than the
// sentence.
func Failure(text, code, detail string) Message {
	return Message{
		Kind:   KindError,
		Text:   text,
		Code:   code,
		Detail: detail,
		At:     time.Now().UTC(),
	}
}

// Role : Who said something in a conversation.
type Role string

const (
	// RoleUser : The person asking.
	RoleUser Role = "user"
	// RoleAssistant : The assistant answering.
	RoleAssistant Role = "assistant"
)

// Turn : One thing said earlier in the same conversation.
type Turn struct {
	Role Role
	Text string
}

// Purpose : Why a request is being made.
//
// Most are the person's question. Some are the assistant's own housekeeping --
// naming a conversation, condensing an old one -- which nobody is waiting on
// and which could one day be sent to a cheaper model. Recorded so anything
// downstream can tell them apart instead of guessing from the shape of the
// prompt.
type Purpose string

const (
	// PurposeChat : Answering the person. The default, and the zero value.
	PurposeChat Purpose = ""
	// PurposeTitle : Naming a conversation.
	PurposeTitle Purpose = "title"
	// PurposeCondense : Condensing the earlier part of a conversation.
	PurposeCondense Purpose = "condense"
)

// Request : What an environment is asked to do.
type Request struct {
	// Prompt : What the user asked for.
	Prompt string
	// History : What was said earlier in the same conversation, oldest first,
	// excluding this prompt. Without it a correction such as "no, make it
	// four" reaches the model with nothing to make four.
	History []Turn
	// Purpose : Why this is being asked. The zero value is the person's own
	// question.
	Purpose Purpose
	// SystemPrompt : How the assistant is told to answer. Empty leaves it to
	// whatever the environment is configured with.
	//
	// Carried per request rather than fixed when the environment is built,
	// because the manner is chosen in the settings and has to take effect
	// without a restart.
	SystemPrompt string
	// Vendor, Model : Which model to ask. Empty leaves it to whatever the
	// provider is configured with, which is what a caller with no preference
	// sends.
	Vendor string
	Model  string
	// Summary : The part of the conversation too old to send in full, condensed.
	// Empty when the whole conversation fits.
	//
	// Not a Turn, because nobody said it. Where it belongs in a request is
	// the provider's to decide: alongside the system prompt for one that
	// takes a separate field, as a leading system message for one that does
	// not.
	Summary string
}

// Environment : An engine that answers a prompt as a stream of messages.
//
// One configured place to send a prompt: an endpoint, the credentials for it,
// the wire format it speaks and the model behind it. Two of these may be the
// same service reached with different credentials, or the same credentials
// pointed at different models.
type Environment interface {
	// Name : Identifies the provider in configuration, routing and logs.
	Name() string

	// Run : Starts answering req and returns the stream of messages it
	// produces.
	//
	// The stream yields zero or more KindUpdate messages, then exactly one
	// KindFinal or KindError message, and is then closed by the environment. A
	// returned error means the run could not be started at all, in which case
	// no channel is returned; a failure during the run arrives as KindError
	// instead.
	//
	// Cancelling ctx ends the run. The stream is then closed without a
	// terminal message, because the caller has stopped listening and there is
	// nowhere to deliver one. A caller that sees the stream close without a
	// terminal message should consult ctx.Err().
	//
	// The channel is unbuffered, so a provider blocks until the caller
	// receives each message or ctx is cancelled. A caller must therefore
	// drain the stream or cancel ctx, or the provider's goroutine is left
	// blocked forever.
	Run(ctx context.Context, req Request) (<-chan Message, error)
}
