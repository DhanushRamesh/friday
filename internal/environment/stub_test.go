package environment_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/environment"
)

// Stub must satisfy the interface it exists to stand in for.
var _ environment.Environment = (*environment.Stub)(nil)

// collect : Drains a stream, returning every message it produced.
func collect(t *testing.T, ch <-chan environment.Message) []environment.Message {
	t.Helper()
	var got []environment.Message
	for msg := range ch {
		got = append(got, msg)
	}
	return got
}

// run : Starts a stub run, failing the test if it could not be started.
func run(t *testing.T, ctx context.Context, s *environment.Stub, prompt string) <-chan environment.Message {
	t.Helper()
	ch, err := s.Run(ctx, environment.Request{Prompt: prompt})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return ch
}

// A run yields updates, then exactly one terminal message, then closes.
func TestStubStreamsUpdatesThenOneFinal(t *testing.T) {
	s := &environment.Stub{Updates: []string{"first", "second"}}

	got := collect(t, run(t, context.Background(), s, "what is the time"))

	if len(got) != 3 {
		t.Fatalf("got %d messages, want 2 updates and 1 final: %v", len(got), got)
	}
	for i, msg := range got[:2] {
		if msg.Kind != environment.KindUpdate {
			t.Errorf("message %d kind = %q, want update", i, msg.Kind)
		}
	}
	last := got[len(got)-1]
	if last.Kind != environment.KindFinal {
		t.Errorf("last message kind = %q, want final", last.Kind)
	}
	if !strings.Contains(last.Text, "what is the time") {
		t.Errorf("final text %q does not reflect the prompt", last.Text)
	}
}

// Exactly one terminal message, and nothing after it.
func TestStubSendsOneTerminalMessageLast(t *testing.T) {
	s := &environment.Stub{}

	got := collect(t, run(t, context.Background(), s, "hello"))

	if len(got) == 0 {
		t.Fatal("no messages produced")
	}
	terminals := 0
	for i, msg := range got {
		if msg.Kind.Terminal() {
			terminals++
			if i != len(got)-1 {
				t.Errorf("terminal message at position %d of %d, want it last", i, len(got))
			}
		}
	}
	if terminals != 1 {
		t.Errorf("got %d terminal messages, want exactly 1", terminals)
	}
}

func TestStubFailureEndsWithKindError(t *testing.T) {
	const reason = "The model did not respond in time."
	s := &environment.Stub{Updates: []string{"working"}, FailWith: reason}

	got := collect(t, run(t, context.Background(), s, "do something"))

	last := got[len(got)-1]
	if last.Kind != environment.KindError {
		t.Fatalf("last message kind = %q, want error", last.Kind)
	}
	if last.Text != reason {
		t.Errorf("error text = %q, want %q", last.Text, reason)
	}
}

// A run that cannot start reports it rather than returning a stream that
// fails immediately.
func TestStubRejectsEmptyPrompt(t *testing.T) {
	s := &environment.Stub{}

	for _, prompt := range []string{"", "   "} {
		ch, err := s.Run(context.Background(), environment.Request{Prompt: prompt})
		if !errors.Is(err, environment.ErrEmptyPrompt) {
			t.Errorf("Run(%q) error = %v, want ErrEmptyPrompt", prompt, err)
		}
		if ch != nil {
			t.Errorf("Run(%q) returned a channel alongside an error", prompt)
		}
	}
}

// Cancelling ends the run and closes the stream, which is what will stop a
// chat mid-flight.
func TestStubStopsWhenCancelled(t *testing.T) {
	s := &environment.Stub{
		Updates: []string{"one", "two", "three", "four"},
		Delay:   20 * time.Millisecond,
	}

	ctx, cancel := context.WithCancel(context.Background())
	ch := run(t, ctx, s, "long job")

	// Take one message, then stop the run.
	first, ok := <-ch
	if !ok {
		t.Fatal("stream closed before producing anything")
	}
	if first.Kind != environment.KindUpdate {
		t.Fatalf("first message kind = %q, want update", first.Kind)
	}
	cancel()

	// The stream must close, and must not have run to completion.
	rest := collect(t, ch)
	for _, msg := range rest {
		if msg.Kind == environment.KindFinal {
			t.Error("a cancelled run produced a final message")
		}
	}
	if len(rest) > 1 {
		t.Errorf("cancelled run produced %d further messages, want at most 1 in flight", len(rest))
	}
}

// A context already cancelled must produce nothing at all.
func TestStubProducesNothingWhenAlreadyCancelled(t *testing.T) {
	s := &environment.Stub{Updates: []string{"one", "two"}}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if got := collect(t, run(t, ctx, s, "too late")); len(got) != 0 {
		t.Errorf("got %d messages from an already-cancelled run, want 0: %v", len(got), got)
	}
}

// Cancelling must not leave the provider's goroutine blocked on a send.
func TestStubGoroutineEndsWhenCallerStopsReading(t *testing.T) {
	s := &environment.Stub{Updates: []string{"one", "two", "three"}}

	ctx, cancel := context.WithCancel(context.Background())
	ch := run(t, ctx, s, "abandoned")

	<-ch // read one, then walk away
	cancel()

	// The goroutine closes the channel on its way out; if it were blocked on
	// a send this would time out.
	deadline := time.After(2 * time.Second)
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return // closed, so the goroutine finished
			}
		case <-deadline:
			t.Fatal("provider goroutine did not exit after cancellation")
		}
	}
}

// An empty non-nil slice means no updates, distinct from nil meaning defaults.
func TestStubUpdatesNilMeansDefaultsEmptyMeansNone(t *testing.T) {
	withDefaults := collect(t, run(t, context.Background(), &environment.Stub{}, "hello"))
	if len(withDefaults) < 2 {
		t.Errorf("nil Updates produced %d messages, want defaults plus a final", len(withDefaults))
	}

	withNone := collect(t, run(t, context.Background(), &environment.Stub{Updates: []string{}}, "hello"))
	if len(withNone) != 1 || withNone[0].Kind != environment.KindFinal {
		t.Errorf("empty Updates produced %v, want only a final message", withNone)
	}
}

func TestStubDelayIsHonoured(t *testing.T) {
	const delay = 15 * time.Millisecond
	s := &environment.Stub{Updates: []string{"one", "two"}, Delay: delay}

	start := time.Now()
	collect(t, run(t, context.Background(), s, "slow"))
	elapsed := time.Since(start)

	// Two updates and a final, so three pauses.
	if want := 3 * delay; elapsed < want {
		t.Errorf("run took %v, want at least %v", elapsed, want)
	}
}
