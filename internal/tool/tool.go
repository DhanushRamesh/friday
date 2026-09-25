// Package tool holds the things the assistant can do, as against say.
//
// A tool is described in a fixed shape rather than in a paragraph, because
// prose quality cannot be enforced and structure can. The descriptions that
// go wrong are the ones that say what a tool is without saying when to reach
// for it, and the repair is always the same: the description grows louder
// until it is shouting IMPORTANT at the model. A shape with a place for "use
// when" and a place for "do not" is what makes that unnecessary.
package tool

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/DhanushRamesh/personal-assistant/internal/chat"
	"github.com/DhanushRamesh/personal-assistant/internal/conversation"
)

// Tool : One thing the assistant can do.
type Tool struct {
	// Name : What the model calls it. Lowercase with underscores, and named
	// for the action rather than the subject, so that a listing of names
	// reads as a list of verbs.
	Name string
	// Purpose : One line saying what it does.
	Purpose string
	// UseWhen : When to reach for it. The part a model gets wrong when it is
	// missing, since a description of what something is does not say when it
	// applies.
	UseWhen string
	// Avoid : When not to, and what to use instead. Optional, but the way to
	// separate two tools a model would otherwise confuse.
	Avoid string
	// Params : What it takes.
	Params Schema
	// Channels : Which channels may reach it.
	//
	// The gate. A prompt that arrived as sound had no confirmation step: a
	// misheard sentence is the whole authorisation, so anything that cannot
	// be undone is left off the voice list.
	Channels []chat.Channel
	// Examples : What a call looks like. Written for the model, and checked
	// against the schema by a test, so an example that lies about its own
	// arguments is caught.
	Examples []Example
	// Run : What it does. Required.
	Run Run
}

// Example : A call worth showing the model.
type Example struct {
	// Ask : What the person said.
	Ask string
	// Args : What the tool should be called with, as JSON.
	Args string
}

// Run : What a tool does when called.
//
// It returns a Result rather than an error, because how a tool failed is
// something the model has to read: an error swallowed here becomes an
// invented explanation later.
type Run func(ctx context.Context, in Invocation) Result

// Invocation : One call to a tool.
type Invocation struct {
	// Caller : Who is asking, and from where.
	Caller Caller
	// Args : The arguments the model produced, as it wrote them.
	Args json.RawMessage
}

// Caller : Who a tool is acting for.
//
// A tool never takes a user or a conversation as an argument. The model would
// have to supply them, which means it could supply the wrong one, and no
// description prevents that. They come from the request instead.
type Caller struct {
	// UserID : Whose assistant this is.
	UserID string
	// ClientID : Which client asked.
	ClientID string
	// ConversationID : The conversation the prompt landed in.
	ConversationID string
	// Channel : How the prompt arrived.
	Channel chat.Channel
}

// Result : What a tool gives back.
type Result struct {
	// Outcome : Whether it worked, did not, or did part of it.
	Outcome conversation.Outcome
	// Content : What it produced, or exactly what went wrong.
	//
	// A failure says why, in the words of whatever actually refused. A tool
	// reporting "could not do that" leaves the model to invent a reason, and
	// it will.
	Content string
}

// OK : A result that worked.
func OK(content string) Result {
	return Result{Outcome: conversation.OutcomeOK, Content: content}
}

// Failed : A result that did nothing, saying exactly why.
func Failed(why string) Result {
	return Result{Outcome: conversation.OutcomeFailed, Content: why}
}

// Partial : A result that did some of what was asked. The content says which
// parts, since "partly done" on its own tells a model nothing it can report.
func Partial(what string) Result {
	return Result{Outcome: conversation.OutcomePartial, Content: what}
}

// Reaches : Whether this tool may be called from the given channel.
func (t Tool) Reaches(c chat.Channel) bool {
	for _, allowed := range t.Channels {
		if allowed == c {
			return true
		}
	}
	return false
}

// Description : What the model is told about the tool.
//
// The parts are joined into the single string a service's schema allows, in a
// fixed order, so that every tool reads the same way and none of them has to
// shout to be noticed.
func (t Tool) Description() string {
	parts := []string{strings.TrimSpace(t.Purpose)}
	if use := strings.TrimSpace(t.UseWhen); use != "" {
		parts = append(parts, "Use when: "+use)
	}
	if avoid := strings.TrimSpace(t.Avoid); avoid != "" {
		parts = append(parts, "Do not use: "+avoid)
	}
	for _, e := range t.Examples {
		parts = append(parts, `Example — "`+e.Ask+`": `+e.Args)
	}
	return strings.Join(parts, " ")
}
