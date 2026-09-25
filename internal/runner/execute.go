package runner

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/chat"
	"github.com/DhanushRamesh/personal-assistant/internal/events"
	"github.com/DhanushRamesh/personal-assistant/internal/failure"
	"github.com/DhanushRamesh/personal-assistant/internal/provider"
	"github.com/DhanushRamesh/personal-assistant/internal/session"
)

// execute : Runs one chat from start to a terminal status.
//
// ctx carries the chat's logging attributes and outlives the run, so that a
// stopped chat can still record why. lifeCtx is cancelled to stop the chat and
// covers the wait for a slot as well as the run itself.
func (r *Runner) execute(ctx, lifeCtx context.Context, t *chat.Chat) {
	select {
	case r.slots <- struct{}{}:
		defer func() { <-r.slots }()
	case <-lifeCtx.Done():
		// Stopped before it ever started. It never ran, so no deadline can
		// have passed.
		r.finishStopped(ctx, t, nil)
		return
	}

	// The deadline covers the run itself, not the wait for a slot.
	runCtx, stopRun := context.WithTimeout(lifeCtx, r.chatTimeout)
	defer stopRun()

	if err := t.Start(); err != nil {
		r.logger.ErrorContext(ctx, "cannot start chat", slog.Any("error", err))
		return
	}
	if err := r.save(ctx, t); err != nil {
		return
	}
	r.logger.InfoContext(ctx, "chat started")

	r.consume(runCtx, ctx, t)

	// After the answer is recorded and announced, so that maintaining the
	// session's memory is never in front of the person waiting for it. The
	// slot is still held, which keeps this from competing with the next
	// chat for the same provider.
	r.condense(ctx, t)
}

// consume : Reads the provider's stream and records what it produces.
//
// runCtx bounds the provider's work and is cancelled to stop it. ctx outlives
// it and is used for the final write, because a cancelled context cannot be
// used to record that the chat was cancelled.
func (r *Runner) consume(runCtx, ctx context.Context, t *chat.Chat) {
	window := r.history(ctx, t)
	stream, err := r.provider.Run(runCtx, provider.Request{
		Prompt:  t.Prompt,
		History: toProviderTurns(window.Messages),
		Summary: window.Summary,
		Vendor:  t.Model.Vendor,
		Model:   t.Model.ID,
	})
	if err != nil {
		r.logger.ErrorContext(ctx, "provider would not start", slog.Any("error", err))
		r.finishWith(ctx, t, func() error {
			return t.Fail("I could not reach the service that answers this.")
		})
		return
	}

	var final *provider.Message
	for msg := range stream {
		switch msg.Kind {
		case provider.KindUpdate:
			r.announce(t.ID, msg)
		case provider.KindFinal, provider.KindError:
			// Keep a copy: the loop must run to completion so the provider's
			// goroutine is not left blocked on a send.
			m := msg
			final = &m
		default:
			r.logger.WarnContext(ctx, "provider sent an unknown message kind",
				slog.String("kind", string(msg.Kind)))
		}
	}

	switch {
	case final != nil && final.Kind == provider.KindError:
		if final.Code != "" && !failure.Known(failure.Code(final.Code)) {
			r.logger.WarnContext(ctx, "provider sent an unknown failure code",
				slog.String("code", final.Code))
		}
		r.finishWith(ctx, t, func() error {
			return t.FailWith(final.Text, final.Code, final.Detail)
		})
	case final != nil:
		r.complete(ctx, t, final.Text)
	default:
		// The stream closed with no terminal message, which the provider
		// contract says means the run was stopped rather than finished.
		r.finishStopped(ctx, t, runCtx.Err())
	}
}

// history : Records the question and returns what was said before it.
//
// The question is written first and the history read up to it, which is what
// keeps it from reaching the model twice — once as the last thing said and
// again as the prompt. Neither failure is worth abandoning the chat for: an
// unrecorded question costs the next turn its context, and an unread history
// leaves the prompt to make sense on its own, which it usually does.
func (r *Runner) history(ctx context.Context, t *chat.Chat) session.Window {
	if t.SessionID == "" {
		return session.Window{}
	}

	asked, err := r.messages.Append(ctx, session.Said(t.SessionID, t.Prompt, t.CreatedAt))
	if err != nil {
		r.logger.ErrorContext(ctx, "cannot record the question", slog.Any("error", err))
	}

	said, err := r.messages.Before(ctx, t.SessionID, asked.Seq)
	if err != nil {
		r.logger.ErrorContext(ctx, "cannot read session history", slog.Any("error", err))
		return session.Window{}
	}

	// A session with no summary yet reads as the zero one, which Plan treats
	// as nothing condensed. Failing to read it costs the turn its oldest
	// context, not the turn itself.
	summary, err := r.messages.Summary(ctx, t.SessionID)
	if err != nil {
		r.logger.ErrorContext(ctx, "cannot read session summary", slog.Any("error", err))
	}

	return session.Plan(said, summary, r.limitsFor(t.Model))
}

// toProviderTurns : Converts a session's messages into the form a provider
// takes.
func toProviderTurns(messages []session.Message) []provider.Turn {
	out := make([]provider.Turn, len(messages))
	for i, m := range messages {
		out[i] = provider.Turn{
			Role: provider.Role(m.Role),
			Text: m.Content,
		}
	}
	return out
}

// recordOutcome : Stores what the chat ended up saying, so the next turn in
// the session can refer to it.
//
// A failure is recorded too, and shown to the person, but is never given back
// to a model: see session.Failure. A turn the person stopped is marked as
// stopped, and that mark is given to a model: see session.Interruption.
func (r *Runner) recordOutcome(ctx context.Context, t *chat.Chat) {
	if t.SessionID == "" {
		return
	}

	said := t.FinishedAt
	if said == nil {
		now := time.Now().UTC()
		said = &now
	}

	var written []session.Message
	switch {
	case t.Response != "":
		written = append(written, session.Answered(t.SessionID, t.Response, *said))
	case t.Error != "":
		written = append(written, session.Failed(t.SessionID, t.Error, t.ErrorDetail, *said))
	}

	// Whatever it managed to say, a turn the person stopped is marked as
	// stopped. Partial output is kept rather than replaced: what ran, ran,
	// and once a turn can call tools some of it will have left effects
	// behind that the next turn has to reason about.
	if t.Status == chat.StatusCancelled {
		written = append(written, session.Interrupted(t.SessionID, *said))
	}

	for _, m := range written {
		if _, err := r.messages.Append(ctx, m); err != nil {
			r.logger.ErrorContext(ctx, "cannot record the answer", slog.Any("error", err))
		}
	}
}

// complete : Records a chat's result, failing it instead if the result cannot
// be stored.
func (r *Runner) complete(ctx context.Context, t *chat.Chat, text string) {
	err := t.Complete(text)
	if errors.Is(err, chat.ErrResponseTooLarge) {
		r.logger.ErrorContext(ctx, "response too large to store",
			slog.Int("bytes", len(text)))
		r.finishWith(ctx, t, func() error {
			return t.Fail("The answer was too long for me to keep.")
		})
		return
	}
	if err != nil {
		r.logger.ErrorContext(ctx, "cannot complete chat", slog.Any("error", err))
		return
	}
	if err := r.save(ctx, t); err != nil {
		return
	}
	r.recordOutcome(ctx, t)
	r.announceOutcome(t)
	r.logger.InfoContext(ctx, "chat completed",
		slog.Duration("took", t.Duration()),
		slog.Int("response_bytes", len(t.Response)))
}

// finishStopped : Records a chat whose stream ended without a result, which
// happens when it was cancelled or outlived its deadline.
func (r *Runner) finishStopped(ctx context.Context, t *chat.Chat, runErr error) {
	if errors.Is(runErr, context.DeadlineExceeded) {
		r.logger.WarnContext(ctx, "chat exceeded its deadline",
			slog.Duration("timeout", r.chatTimeout))
		r.finishWith(ctx, t, func() error { return t.Fail(timeoutReason) })
		return
	}

	// A reason set before cancelling distinguishes a shutdown from a user
	// stopping the chat themselves.
	if reason, ok := r.cancelReason(t.ID); ok && reason != "" {
		r.logger.InfoContext(ctx, "chat stopped", slog.String("reason", reason))
		r.finishWith(ctx, t, func() error { return t.Fail(reason) })
		return
	}

	r.logger.InfoContext(ctx, "chat cancelled")
	r.finishWith(ctx, t, func() error { return t.Cancel() })
}

// finishWith : Applies a terminal transition and stores the result.
func (r *Runner) finishWith(ctx context.Context, t *chat.Chat, transition func() error) {
	if err := transition(); err != nil {
		r.logger.ErrorContext(ctx, "cannot finish chat", slog.Any("error", err))
		return
	}
	_ = r.save(ctx, t)
	r.recordOutcome(ctx, t)
	r.announceOutcome(t)
}

// announce : Reports where a chat has got to, without storing it.
//
// Progress is transient by nature: it is worth hearing while the answer is
// being produced and worth nothing afterwards. It used to be written to a
// table so a dropped stream could replay it, and that table went with the
// stream.
func (r *Runner) announce(chatID string, msg provider.Message) {
	r.publish(events.Event{
		ChatID: chatID,
		Kind:   events.KindUpdate,
		Text:   msg.Text,
		At:     msg.At,
	})
}

// publish : Announces an event, if there is anywhere to announce it.
func (r *Runner) publish(ev events.Event) {
	if r.publisher == nil {
		return
	}
	if ev.At.IsZero() {
		ev.At = time.Now().UTC()
	}
	r.publisher.Publish(ev)
}

// announceOutcome : Tells listeners how a chat ended, so a stream can close
// rather than waiting for a message that will never come.
func (r *Runner) announceOutcome(t *chat.Chat) {
	ev := events.Event{ChatID: t.ID, At: t.UpdatedAt}
	switch t.Status {
	case chat.StatusCompleted:
		ev.Kind, ev.Text = events.KindFinal, t.Response
	case chat.StatusFailed:
		ev.Kind, ev.Text = events.KindError, t.Error
	case chat.StatusCancelled:
		ev.Kind, ev.Text = events.KindCancelled, ""
	default:
		return
	}
	r.publish(ev)
}

// save : Writes a chat's current state, using a context that outlives the
// run so that a cancelled chat can still record having been cancelled.
func (r *Runner) save(ctx context.Context, t *chat.Chat) error {
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()

	if err := r.repo.Update(writeCtx, t); err != nil {
		r.logger.ErrorContext(ctx, "cannot store chat",
			slog.String("status", string(t.Status)),
			slog.Any("error", err))
		return err
	}
	return nil
}
