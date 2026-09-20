package runner

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/DhanushRamesh/friday/internal/provider"
	"github.com/DhanushRamesh/friday/internal/task"
)

// execute : Runs one task from start to a terminal status.
//
// ctx carries the task's logging attributes and outlives the run, so that a
// stopped task can still record why. lifeCtx is cancelled to stop the task and
// covers the wait for a slot as well as the run itself.
func (r *Runner) execute(ctx, lifeCtx context.Context, t *task.Task) {
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
	runCtx, stopRun := context.WithTimeout(lifeCtx, r.taskTimeout)
	defer stopRun()

	if err := t.Start(); err != nil {
		r.logger.ErrorContext(ctx, "cannot start task", slog.Any("error", err))
		return
	}
	if err := r.save(ctx, t); err != nil {
		return
	}
	r.logger.InfoContext(ctx, "task started")

	r.consume(runCtx, ctx, t)
}

// consume : Reads the provider's stream and records what it produces.
//
// runCtx bounds the provider's work and is cancelled to stop it. ctx outlives
// it and is used for the final write, because a cancelled context cannot be
// used to record that the task was cancelled.
func (r *Runner) consume(runCtx, ctx context.Context, t *task.Task) {
	stream, err := r.provider.Run(runCtx, provider.Request{Prompt: t.Prompt})
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
			r.record(ctx, t.ID, msg)
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
		r.finishWith(ctx, t, func() error { return t.Fail(final.Text) })
	case final != nil:
		r.complete(ctx, t, final.Text)
	default:
		// The stream closed with no terminal message, which the provider
		// contract says means the run was stopped rather than finished.
		r.finishStopped(ctx, t, runCtx.Err())
	}
}

// complete : Records a task's result, failing it instead if the result cannot
// be stored.
func (r *Runner) complete(ctx context.Context, t *task.Task, text string) {
	err := t.Complete(text)
	if errors.Is(err, task.ErrResponseTooLarge) {
		r.logger.ErrorContext(ctx, "response too large to store",
			slog.Int("bytes", len(text)))
		r.finishWith(ctx, t, func() error {
			return t.Fail("The answer was too long for me to keep.")
		})
		return
	}
	if err != nil {
		r.logger.ErrorContext(ctx, "cannot complete task", slog.Any("error", err))
		return
	}
	if err := r.save(ctx, t); err != nil {
		return
	}
	r.logger.InfoContext(ctx, "task completed",
		slog.Duration("took", t.Duration()),
		slog.Int("response_bytes", len(t.Response)))
}

// finishStopped : Records a task whose stream ended without a result, which
// happens when it was cancelled or outlived its deadline.
func (r *Runner) finishStopped(ctx context.Context, t *task.Task, runErr error) {
	if errors.Is(runErr, context.DeadlineExceeded) {
		r.logger.WarnContext(ctx, "task exceeded its deadline",
			slog.Duration("timeout", r.taskTimeout))
		r.finishWith(ctx, t, func() error { return t.Fail(timeoutReason) })
		return
	}

	// A reason set before cancelling distinguishes a shutdown from a user
	// stopping the task themselves.
	if reason, ok := r.cancelReason(t.ID); ok && reason != "" {
		r.logger.InfoContext(ctx, "task stopped", slog.String("reason", reason))
		r.finishWith(ctx, t, func() error { return t.Fail(reason) })
		return
	}

	r.logger.InfoContext(ctx, "task cancelled")
	r.finishWith(ctx, t, func() error { return t.Cancel() })
}

// finishWith : Applies a terminal transition and stores the result.
func (r *Runner) finishWith(ctx context.Context, t *task.Task, transition func() error) {
	if err := transition(); err != nil {
		r.logger.ErrorContext(ctx, "cannot finish task", slog.Any("error", err))
		return
	}
	_ = r.save(ctx, t)
}

// record : Stores one transient message.
//
// A message that cannot be stored does not fail the task: the answer still
// matters, and losing a line of progress is not worth discarding it for.
func (r *Runner) record(ctx context.Context, taskID string, msg provider.Message) {
	if _, err := r.repo.AppendMessage(ctx, taskID, string(msg.Kind), msg.Text); err != nil {
		r.logger.ErrorContext(ctx, "cannot store message", slog.Any("error", err))
	}
}

// save : Writes a task's current state, using a context that outlives the
// run so that a cancelled task can still record having been cancelled.
func (r *Runner) save(ctx context.Context, t *task.Task) error {
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()

	if err := r.repo.Update(writeCtx, t); err != nil {
		r.logger.ErrorContext(ctx, "cannot store task",
			slog.String("status", string(t.Status)),
			slog.Any("error", err))
		return err
	}
	return nil
}
