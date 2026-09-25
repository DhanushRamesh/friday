package runner

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"github.com/DhanushRamesh/personal-assistant/internal/provider"
	"github.com/DhanushRamesh/personal-assistant/internal/session"
)

// errNoAnswer : Returned when a provider's stream ends without a result.
var errNoAnswer = errors.New("runner: the provider said nothing")

// condense : Folds the earliest part of a session into its running notes when
// the conversation has grown close to a ceiling.
//
// Every failure here is logged and dropped. The session is left as it was, so
// the next turn sends what it can and tries again; nothing the person asked
// for depends on this succeeding.
func (r *Runner) condense(ctx context.Context, sessionID string) {
	if sessionID == "" {
		return
	}

	current, err := r.messages.Summary(ctx, sessionID)
	if err != nil {
		r.logger.ErrorContext(ctx, "cannot read session summary", slog.Any("error", err))
		return
	}

	said, err := r.messages.All(ctx, sessionID)
	if err != nil {
		r.logger.ErrorContext(ctx, "cannot read session history", slog.Any("error", err))
		return
	}

	through, due := session.Due(said, current, r.historyLimits)
	if !due {
		return
	}

	fold := between(said, current.ThroughSeq, through)
	if len(fold) == 0 {
		return
	}

	askCtx, cancel := context.WithTimeout(ctx, r.condenseTimeout)
	defer cancel()

	notes, err := r.ask(askCtx, session.CondensePrompt(current.Text, fold))
	if err != nil {
		r.logger.WarnContext(ctx, "cannot condense the session",
			slog.String("session_id", sessionID), slog.Any("error", err))
		return
	}

	next := session.Summary{Text: notes, ThroughSeq: through}
	if err := r.messages.SetSummary(ctx, sessionID, next); err != nil {
		r.logger.ErrorContext(ctx, "cannot store the session summary",
			slog.String("session_id", sessionID), slog.Any("error", err))
		return
	}

	r.logger.InfoContext(ctx, "session condensed",
		slog.String("session_id", sessionID),
		slog.Int("through_seq", through),
		slog.Int("messages", len(fold)),
		slog.Int("summary_bytes", len(notes)))
}

// between : The messages after seq and up to and including through.
func between(messages []session.Message, after, through int) []session.Message {
	out := make([]session.Message, 0, len(messages))
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
func (r *Runner) ask(ctx context.Context, prompt string) (string, error) {
	stream, err := r.provider.Run(ctx, provider.Request{Prompt: prompt})
	if err != nil {
		return "", err
	}

	// The stream is drained to the end whatever it says, so the provider's
	// goroutine is never left blocked on a send.
	var final *provider.Message
	for msg := range stream {
		if msg.Kind == provider.KindFinal || msg.Kind == provider.KindError {
			m := msg
			final = &m
		}
	}

	switch {
	case final == nil && ctx.Err() != nil:
		return "", ctx.Err()
	case final == nil:
		return "", errNoAnswer
	case final.Kind == provider.KindError:
		return "", errors.New(final.Text)
	}

	text := strings.TrimSpace(final.Text)
	if text == "" {
		return "", errNoAnswer
	}
	return text, nil
}
