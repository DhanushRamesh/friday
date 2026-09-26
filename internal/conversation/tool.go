package conversation

import (
	"strings"
	"time"
)

// ToolCall : The assistant asking for a tool to be run.
type ToolCall struct {
	// ID : What the answer will be matched back to. A turn may ask for
	// several tools at once and the answers do not arrive in order.
	ID string
	// Name : Which tool.
	Name string
	// Arguments : What it was called with, as the JSON the model produced.
	//
	// Kept as it was written rather than parsed into a map, because what the
	// model actually said is the thing worth having when an argument turns
	// out to be wrong.
	Arguments string
}

// Outcome : How a tool call turned out.
//
// Three rather than two, because the middle one is where an assistant starts
// bluffing: four lights asked for, three turned off, one unreachable. Told
// only "ok" or "failed" a model reports either "done" or "nothing happened",
// and both are untrue.
type Outcome string

const (
	// OutcomeOK : It did what was asked.
	OutcomeOK Outcome = "ok"
	// OutcomeFailed : It did nothing.
	OutcomeFailed Outcome = "failed"
	// OutcomePartial : It did some of it. What was and was not done belongs
	// in the result, in words.
	OutcomePartial Outcome = "partial"
)

// known : Whether this is an outcome the store will accept.
func (o Outcome) known() bool {
	return o == OutcomeOK || o == OutcomeFailed || o == OutcomePartial
}

// ToolResult : What a tool gave back.
type ToolResult struct {
	// ID : The call this answers.
	ID string
	// Name : Which tool, repeated here so a result reads on its own.
	Name string
	// Outcome : Whether it worked.
	Outcome Outcome
	// Content : What it produced, or exactly what went wrong.
	//
	// A failure carries the real error, verbatim. A tool that reports "could
	// not do that" without saying why leaves the model to invent a reason,
	// which is the whole of how an assistant comes to bluff.
	Content string
	// TookMS : How long the tool ran, in milliseconds.
	//
	// Not shown to the model, which has no use for it. It is what makes a
	// timeline of an answer readable afterwards, and a tool that has become
	// slow visible at all.
	TookMS int64 `json:",omitempty"`
}

// CalledTools : A message recording that the assistant asked for tools.
//
// It carries no words. A message is either prose or tool calls, never both:
// a model that explains itself and acts in the same breath gives a person
// something to read that may not describe what actually happened.
func CalledTools(conversationID string, calls []ToolCall, at time.Time) Message {
	return Message{
		ID:             NewMessageID(),
		ConversationID: conversationID,
		Kind:           Chat,
		Role:           Assistant,
		ToolCalls:      calls,
		At:             at,
	}
}

// ToolsReturned : A message recording what the tools gave back.
func ToolsReturned(conversationID string, results []ToolResult, at time.Time) Message {
	return Message{
		ID:             NewMessageID(),
		ConversationID: conversationID,
		Kind:           Chat,
		Role:           Tool,
		ToolResults:    results,
		At:             at,
	}
}

// validTools : Whether a message's tool calls and results are usable, and why
// not if they are not.
func (m Message) validTools() error {
	for _, c := range m.ToolCalls {
		switch {
		case strings.TrimSpace(c.ID) == "":
			return errToolCallNeedsID
		case strings.TrimSpace(c.Name) == "":
			return errToolCallNeedsName
		}
	}
	for _, r := range m.ToolResults {
		switch {
		case strings.TrimSpace(r.ID) == "":
			return errToolResultNeedsID
		case !r.Outcome.known():
			return errToolResultNeedsOutcome
		}
	}
	return nil
}

// answers : The tool calls a result message replies to, by identifier.
func (m Message) answers() map[string]bool {
	ids := make(map[string]bool, len(m.ToolResults))
	for _, r := range m.ToolResults {
		ids[r.ID] = true
	}
	return ids
}

// asks : The identifiers this message's tool calls will be answered by.
func (m Message) asks() map[string]bool {
	ids := make(map[string]bool, len(m.ToolCalls))
	for _, c := range m.ToolCalls {
		ids[c.ID] = true
	}
	return ids
}
