package runner

import (
	"context"
	"log/slog"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/chat"
	"github.com/DhanushRamesh/personal-assistant/internal/conversation"
	"github.com/DhanushRamesh/personal-assistant/internal/environment"
	"github.com/DhanushRamesh/personal-assistant/internal/tool"
)

// MaxToolHops : How many rounds of tool calls one question may take.
//
// A model that keeps calling tools has to be stopped by something. The last
// round is asked again with no tools offered, so the answer is composed from
// what was actually gathered rather than cut off: the person gets a reply
// either way, and the model has to say what it managed rather than what it
// intended.
const MaxToolHops = 5

// offered : The tools this chat may reach, in the shape an environment takes.
//
// The first of the two gates. A tool missing from here is never described to
// the model, so a model cannot call what it has not been told exists. The
// registry checks again when one is actually called, because this is a
// prompt and a prompt is not a boundary.
func (r *Runner) offered(t *chat.Chat) []environment.ToolSpec {
	if r.tools == nil {
		return nil
	}

	reachable := r.tools.For(t.Channel)
	out := make([]environment.ToolSpec, 0, len(reachable))
	for _, x := range reachable {
		schema, err := x.Params.MarshalJSON()
		if err != nil {
			// Unreachable: the schema is a typed structure of strings. A tool
			// whose schema will not render is left out rather than offered
			// without one, since a tool with no arguments described is a tool
			// called with guessed arguments.
			r.logger.Error("a tool's schema will not render, so it is not offered",
				slog.String("tool", x.Name), slog.Any("error", err))
			continue
		}
		out = append(out, environment.ToolSpec{
			Name:        x.Name,
			Description: x.Description(),
			Parameters:  schema,
		})
	}
	return out
}

// runTools : Runs what the model asked for and records both halves.
//
// The call and the answer are both written to the conversation before the
// model is asked again. A chain cut in the middle -- by a restart, a
// deadline, or the person saying stop -- then still reads as what was done
// rather than as a question nobody answered.
func (r *Runner) runTools(
	ctx context.Context,
	t *chat.Chat,
	calls []environment.ToolCall,
) []environment.Turn {
	asked := make([]conversation.ToolCall, 0, len(calls))
	for _, c := range calls {
		asked = append(asked, conversation.ToolCall{ID: c.ID, Name: c.Name, Arguments: c.Arguments})
	}
	call := conversation.CalledTools(t.ConversationID, asked, time.Now().UTC())
	r.remember(ctx, t, call)

	caller := r.callerFor(ctx, t)

	got := make([]conversation.ToolResult, 0, len(calls))
	for _, c := range calls {
		started := time.Now()
		result := r.tools.Call(ctx, c.Name, tool.Invocation{
			Caller: caller,
			Args:   []byte(c.Arguments),
		})

		r.logger.InfoContext(ctx, "tool ran",
			slog.String("tool", c.Name),
			slog.String("outcome", string(result.Outcome)),
			slog.Duration("took", time.Since(started)))

		got = append(got, conversation.ToolResult{
			ID:      c.ID,
			Name:    c.Name,
			Outcome: result.Outcome,
			Content: result.Content,
		})
	}

	results := conversation.ToolsReturned(t.ConversationID, got, time.Now().UTC())
	r.remember(ctx, t, results)

	return toProviderTurns([]conversation.Message{call, results})
}

// callerFor : Who a tool is acting for.
//
// The user comes from the conversation rather than from the chat, which does
// not carry one. A tool given no user acts for nobody and every ownership
// check refuses it, which is the safe direction: a tool that cannot tell
// whose data it is looking at should not be looking at it.
func (r *Runner) callerFor(ctx context.Context, t *chat.Chat) tool.Caller {
	caller := tool.Caller{
		ClientID:       t.ClientID,
		ConversationID: t.ConversationID,
		Channel:        t.Channel,
	}

	if t.ConversationID == "" || r.repo == nil {
		return caller
	}
	c, err := r.repo.GetConversation(ctx, t.ConversationID)
	if err != nil {
		r.logger.ErrorContext(ctx, "cannot tell whose conversation this is",
			slog.String("conversation_id", t.ConversationID), slog.Any("error", err))
		return caller
	}
	caller.UserID = c.UserID
	return caller
}

// remember : Writes a message to the conversation, logging a failure rather
// than abandoning the turn.
//
// A tool that ran and was not recorded is worse than one that did not run:
// the effects are in the world and the transcript denies them. It is still
// not a reason to fail the answer, since failing it would leave the person
// with nothing and the effects still there.
func (r *Runner) remember(ctx context.Context, t *chat.Chat, m conversation.Message) {
	if t.ConversationID == "" {
		return
	}
	if _, err := r.messages.Append(ctx, m); err != nil {
		r.logger.ErrorContext(ctx, "cannot record what the tools did",
			slog.String("role", string(m.Role)), slog.Any("error", err))
	}
}
