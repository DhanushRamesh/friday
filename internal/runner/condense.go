package runner

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"github.com/DhanushRamesh/personal-assistant/internal/chat"
	"github.com/DhanushRamesh/personal-assistant/internal/conversation"
	"github.com/DhanushRamesh/personal-assistant/internal/environment"
)

// errNoAnswer : Returned when a provider's stream ends without a result.
var errNoAnswer = errors.New("runner: the provider said nothing")

// condense : Folds the earliest part of a conversation into its running notes when
// the conversation has grown close to a ceiling.
//
// Every failure here is logged and dropped. The conversation is left as it was, so
// the next turn sends what it can and tries again; nothing the person asked
// for depends on this succeeding.
func (r *Runner) condense(ctx context.Context, t *chat.Chat, systemPrompt string) {
	conversationID := t.ConversationID
	if conversationID == "" {
		return
	}

	limits := r.limitsFor(t.Model, r.alongside(t, systemPrompt))

	current, err := r.messages.Summary(ctx, conversationID)
	if err != nil {
		r.logger.ErrorContext(ctx, "cannot read conversation summary", slog.Any("error", err))
		return
	}

	said, err := r.messages.All(ctx, conversationID)
	if err != nil {
		r.logger.ErrorContext(ctx, "cannot read conversation history", slog.Any("error", err))
		return
	}

	through, due := conversation.Due(said, current, limits)
	if !due {
		return
	}

	fold := between(said, current.ThroughSeq, through)
	if len(fold) == 0 {
		return
	}

	askCtx, cancel := context.WithTimeout(ctx, r.condenseTimeout)
	defer cancel()

	notes, err := r.ask(askCtx, t.Model, environment.PurposeCondense,
		conversation.CondensePrompt(current.Text, fold))
	if err != nil {
		r.logger.WarnContext(ctx, "cannot condense the conversation",
			slog.String("conversation_id", conversationID), slog.Any("error", err))
		return
	}

	next := conversation.Summary{Text: notes, ThroughSeq: through}
	if err := r.messages.SetSummary(ctx, conversationID, next); err != nil {
		r.logger.ErrorContext(ctx, "cannot store the conversation summary",
			slog.String("conversation_id", conversationID), slog.Any("error", err))
		return
	}

	r.logger.InfoContext(ctx, "conversation condensed",
		slog.String("conversation_id", conversationID),
		slog.Int("through_seq", through),
		slog.Int("messages", len(fold)),
		slog.Int("summary_bytes", len(notes)))
}

// between : The messages after seq and up to and including through.
func between(messages []conversation.Message, after, through int) []conversation.Message {
	out := make([]conversation.Message, 0, len(messages))
	for _, m := range messages {
		if m.Seq > after && m.Seq <= through {
			out = append(out, m)
		}
	}
	return out
}

// ask : Puts a prompt to the provider and returns what it answers.
//
// Used for the assistant's own housekeeping rather than for a chat, so the
// result is not recorded anywhere and no failure is shown to anyone.
func (r *Runner) ask(ctx context.Context, model chat.Model, why environment.Purpose, prompt string) (string, error) {
	stream, err := r.environment.Run(ctx, environment.Request{
		Prompt:  prompt,
		Purpose: why,
		Vendor:  model.Vendor,
		Model:   model.ID,
	})
	if err != nil {
		return "", err
	}

	// The stream is drained to the end whatever it says, so the provider's
	// goroutine is never left blocked on a send.
	var final *environment.Message
	for msg := range stream {
		if msg.Kind == environment.KindFinal || msg.Kind == environment.KindError {
			m := msg
			final = &m
		}
	}

	switch {
	case final == nil && ctx.Err() != nil:
		return "", ctx.Err()
	case final == nil:
		return "", errNoAnswer
	case final.Kind == environment.KindError:
		return "", errors.New(final.Text)
	}

	text := strings.TrimSpace(final.Text)
	if text == "" {
		return "", errNoAnswer
	}
	return text, nil
}

// indexBudget : How many exchanges one turn indexes.
//
// The turn just finished is one; the rest is a backlog left by an embedding
// server that was away. Small, because this runs while the slot is still
// held.
const indexBudget = 20

// index : Makes what was just said searchable later.
//
// After the answer, like condensing, and its failure is logged and dropped:
// an exchange that is not indexed is found by nothing, which is what every
// exchange was before this existed.
func (r *Runner) index(ctx context.Context) {
	if r.memory == nil {
		return
	}

	ctx, cancel := context.WithTimeout(ctx, r.condenseTimeout)
	defer cancel()

	done, err := r.memory.IndexTranscript(ctx, indexBudget)
	if err != nil {
		r.logger.WarnContext(ctx, "cannot index what was said", slog.Any("error", err))
		return
	}
	if done > 0 {
		r.logger.DebugContext(ctx, "exchanges indexed", slog.Int("exchanges", done))
	}
}
