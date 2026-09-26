package runner

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/chat"
	"github.com/DhanushRamesh/personal-assistant/internal/conversation"
	"github.com/DhanushRamesh/personal-assistant/internal/environment"
	"github.com/DhanushRamesh/personal-assistant/internal/events"
	"github.com/DhanushRamesh/personal-assistant/internal/failure"
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

	// Composed once. It reads the database and searches memory, and every
	// hop of the tool loop must be given the same one.
	systemPrompt := r.promptFor(ctx, t)

	r.consume(runCtx, ctx, t, systemPrompt)

	// After the answer is recorded and announced, so that maintaining the
	// conversation's memory is never in front of the person waiting for it. The
	// slot is still held, which keeps this from competing with the next
	// chat for the same environment.
	// Both run after the answer has been delivered, and both hold the slot
	// so they cannot pile up. Naming first, since it is the shorter of the
	// two and is the one somebody is waiting to hear.
	r.title(ctx, t)
	r.condense(ctx, t, systemPrompt)
	r.index(ctx)
}

// consume : Reads the provider's stream and records what it produces.
//
// runCtx bounds the provider's work and is cancelled to stop it. ctx outlives
// it and is used for the final write, because a cancelled context cannot be
// used to record that the chat was cancelled.
func (r *Runner) consume(runCtx, ctx context.Context, t *chat.Chat, systemPrompt string) {
	window := r.history(ctx, t, systemPrompt)
	turns := toProviderTurns(window.Messages)
	prompt := t.Prompt

	for hop := 0; ; hop++ {
		// The last round is offered nothing. A model that has run out of
		// rounds must answer from what it gathered, and saying what it
		// managed is better than being cut off mid-chain with nothing to
		// show for the work that already ran.
		tools := r.offered(t)
		if hop >= MaxToolHops-1 {
			if len(tools) > 0 {
				r.logger.WarnContext(ctx, "the tool chain ran long, so the last round is asked without tools",
					slog.Int("hops", hop))
			}
			tools = nil
		}

		stream, err := r.environment.Run(runCtx, environment.Request{
			Prompt:  prompt,
			History: turns,
			Summary: window.Summary,
			Vendor:  t.Model.Vendor,
			Model:   t.Model.ID,
			Tools:   tools,

			SystemPrompt: systemPrompt,
		})
		if err != nil {
			r.logger.ErrorContext(ctx, "environment would not start", slog.Any("error", err))
			r.finishWith(ctx, t, func() error {
				return t.Fail("I could not reach the service that answers this.")
			})
			return
		}

		final := r.drain(ctx, t, stream)

		switch {
		case final == nil:
			// The stream closed with no terminal message, which the contract
			// says means the run was stopped rather than finished.
			r.finishStopped(ctx, t, runCtx.Err())
			return

		case final.Kind == environment.KindError:
			if final.Code != "" && !failure.Known(failure.Code(final.Code)) {
				r.logger.WarnContext(ctx, "environment sent an unknown failure code",
					slog.String("code", final.Code))
			}
			r.finishWith(ctx, t, func() error {
				return t.FailWith(spoken(t.Channel, final.Text, final.Detail),
					final.Code, final.Detail)
			})
			return

		case final.Kind != environment.KindToolCalls:
			r.complete(ctx, t, final.Text)
			return
		}

		// Tools were asked for. Run them, remember both halves, and go round
		// again with what they returned. The question is not repeated: it is
		// in the history now, and asking it twice would have the model answer
		// it twice.
		turns = append(turns, asUserTurn(prompt)...)
		turns = append(turns, r.runTools(ctx, t, final.ToolCalls)...)
		prompt = ""

		if runCtx.Err() != nil {
			// Stopped while the tools were running. What ran, ran, and the
			// transcript already says so.
			r.finishStopped(ctx, t, runCtx.Err())
			return
		}
	}
}

// spoken : What a failure says, for the channel it has to be said on.
//
// Typed, the sentence alone: the exact error is a click away under "more
// info", and a wall of service jargon in the transcript buries the part
// anybody reads.
//
// Spoken, both. There is no "more info" on a speaker, so a sentence on its own
// leaves the person with a failure and no way to reach what caused it. Saying
// it aloud is ugly and is still better than withholding it.
func spoken(channel chat.Channel, sentence, detail string) string {
	detail = strings.TrimSpace(detail)
	if channel != chat.ChannelVoice || detail == "" {
		return sentence
	}
	return sentence + " The exact error was: " + detail

}

// asUserTurn : The question as a turn, or nothing when there is none.
//
// Added to the history the first time round, because from then on the request
// carries no prompt and the question would otherwise be missing from what the
// model reads.
func asUserTurn(prompt string) []environment.Turn {
	if strings.TrimSpace(prompt) == "" {
		return nil
	}
	return []environment.Turn{{Role: environment.RoleUser, Text: prompt}}
}

// drain : Reads a stream to its end, returning its terminal message.
//
// Always read to completion, whatever arrives: the environment blocks on an
// unread send, so abandoning a stream early leaves its goroutine stuck.
func (r *Runner) drain(ctx context.Context, t *chat.Chat, stream <-chan environment.Message) *environment.Message {
	var final *environment.Message
	for msg := range stream {
		switch msg.Kind {
		case environment.KindUpdate:
			r.announce(t.ID, msg)
		case environment.KindFinal, environment.KindError, environment.KindToolCalls:
			m := msg
			final = &m
		default:
			r.logger.WarnContext(ctx, "environment sent an unknown message kind",
				slog.String("kind", string(msg.Kind)))
		}
	}
	return final
}

// history : Records the question and returns what was said before it.
//
// The question is written first and the history read up to it, which is what
// keeps it from reaching the model twice — once as the last thing said and
// again as the prompt. Neither failure is worth abandoning the chat for: an
// unrecorded question costs the next turn its context, and an unread history
// leaves the prompt to make sense on its own, which it usually does.
func (r *Runner) history(ctx context.Context, t *chat.Chat, systemPrompt string) conversation.Window {
	if t.ConversationID == "" {
		return conversation.Window{}
	}

	asked, err := r.messages.Append(ctx, conversation.Said(t.ConversationID, t.Prompt, t.CreatedAt))
	if err != nil {
		r.logger.ErrorContext(ctx, "cannot record the question", slog.Any("error", err))
	}

	said, err := r.messages.Before(ctx, t.ConversationID, asked.Seq)
	if err != nil {
		r.logger.ErrorContext(ctx, "cannot read conversation history", slog.Any("error", err))
		return conversation.Window{}
	}

	// A conversation with no summary yet reads as the zero one, which Plan treats
	// as nothing condensed. Failing to read it costs the turn its oldest
	// context, not the turn itself.
	summary, err := r.messages.Summary(ctx, t.ConversationID)
	if err != nil {
		r.logger.ErrorContext(ctx, "cannot read conversation summary", slog.Any("error", err))
	}

	return conversation.Plan(said, summary, r.limitsFor(t.Model, r.alongside(t, systemPrompt)))
}

// toProviderTurns : Converts a conversation's messages into the form a provider
// takes.
func toProviderTurns(messages []conversation.Message) []environment.Turn {
	out := make([]environment.Turn, len(messages))
	for i, m := range messages {
		turn := environment.Turn{
			Role: environment.Role(m.Role),
			Text: m.Content,
		}
		for _, c := range m.ToolCalls {
			turn.ToolCalls = append(turn.ToolCalls, environment.ToolCall{
				ID:        c.ID,
				Name:      c.Name,
				Arguments: c.Arguments,
			})
		}
		for _, r := range m.ToolResults {
			// The outcome travels with the content rather than in a field of
			// its own, because the wire has nowhere else to put it and the
			// model has to be able to tell a success from a failure. A
			// result that reads as plain text is one the model will report
			// as having worked.
			turn.ToolResults = append(turn.ToolResults, environment.ToolResult{
				ID:      r.ID,
				Content: string(r.Outcome) + ": " + r.Content,
			})
		}
		out[i] = turn
	}
	return out
}

// recordOutcome : Stores what the chat ended up saying, so the next turn in
// the conversation can refer to it.
//
// A failure is recorded too, and shown to the person, but is never given back
// to a model: see conversation.Failure. A turn the person stopped is marked as
// stopped, and that mark is given to a model: see conversation.Interruption.
func (r *Runner) recordOutcome(ctx context.Context, t *chat.Chat) {
	if t.ConversationID == "" {
		return
	}

	said := t.FinishedAt
	if said == nil {
		now := time.Now().UTC()
		said = &now
	}

	var written []conversation.Message
	switch {
	case t.Response != "":
		written = append(written, conversation.Answered(t.ConversationID, t.Response, *said))
	case t.Error != "":
		written = append(written, conversation.Failed(t.ConversationID, t.Error, t.ErrorDetail, *said))
	}

	// Whatever it managed to say, a turn the person stopped is marked as
	// stopped. Partial output is kept rather than replaced: what ran, ran,
	// and once a turn can call tools some of it will have left effects
	// behind that the next turn has to reason about.
	if t.Status == chat.StatusCancelled {
		written = append(written, conversation.Interrupted(t.ConversationID, *said))
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
func (r *Runner) announce(chatID string, msg environment.Message) {
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
