package provider

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrEmptyPrompt : The request carried no prompt for the provider to answer.
var ErrEmptyPrompt = errors.New("provider: request has no prompt")

// defaultStubUpdates : The progress messages a Stub sends when none are
// configured. They are phrased as the assistant would speak them, so that what a
// client renders during development resembles what it will render later.
var defaultStubUpdates = []string{
	"Let me take a look at that.",
	"Still working on it.",
}

// Stub : A provider that answers from a script rather than a model.
//
// It exists so that the chat lifecycle, streaming and cancellation can be
// built and debugged without an API key, a network, or the latency and
// variability of a real model. A real provider replaces it behind the same
// interface.
//
// The zero value is usable.
type Stub struct {
	// Updates : The progress messages to send before the result. Nil selects
	// defaultStubUpdates; an empty non-nil slice sends none.
	Updates []string
	// Delay : How long to pause before each message, imitating a model that
	// takes time to answer. Zero sends them as fast as the caller reads.
	Delay time.Duration
	// FailWith : When set, the run ends with this text as a KindError message
	// instead of a result, so that failure handling can be exercised.
	FailWith string
	// FailCode : The failure.Code a stubbed failure reports. Optional.
	FailCode string
	// FailDetail : The exact error a stubbed failure carries. Optional.
	FailDetail string
}

// Name : Returns the provider's name.
func (s *Stub) Name() string { return "stub" }

// Run : Sends the configured updates, then either the result or the
// configured failure. See Provider.Run for the contract it follows.
func (s *Stub) Run(ctx context.Context, req Request) (<-chan Message, error) {
	if strings.TrimSpace(req.Prompt) == "" {
		return nil, ErrEmptyPrompt
	}

	updates := s.Updates
	if updates == nil {
		updates = defaultStubUpdates
	}

	ch := make(chan Message)
	go func() {
		defer close(ch)

		for _, text := range updates {
			if !s.pause(ctx) || !send(ctx, ch, Update(text)) {
				return
			}
		}
		if !s.pause(ctx) {
			return
		}

		if s.FailWith != "" {
			send(ctx, ch, Failure(s.FailWith, s.FailCode, s.FailDetail))
			return
		}
		send(ctx, ch, Final(fmt.Sprintf("You asked: %q. This is a stub response.", req.Prompt)))
	}()

	return ch, nil
}

// pause : Waits for the configured delay, reporting false if ctx ends first.
func (s *Stub) pause(ctx context.Context) bool {
	if s.Delay <= 0 {
		// Still honour cancellation, so that a cancelled run with no delay
		// stops rather than sending its whole script.
		return ctx.Err() == nil
	}

	timer := time.NewTimer(s.Delay)
	defer timer.Stop()

	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}

// send : Delivers msg, reporting false if ctx ends before the caller receives
// it.
func send(ctx context.Context, ch chan<- Message, msg Message) bool {
	select {
	case ch <- msg:
		return true
	case <-ctx.Done():
		return false
	}
}
