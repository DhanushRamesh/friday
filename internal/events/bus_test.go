package events_test

import (
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/DhanushRamesh/friday/internal/events"
)

// newBus : Returns a Bus that logs nowhere.
func newBus() *events.Bus {
	return events.NewBus(slog.New(slog.NewJSONHandler(io.Discard, nil)))
}

// receive : Takes one event, failing the test if none arrives.
func receive(t *testing.T, ch <-chan events.Event) events.Event {
	t.Helper()
	select {
	case ev, ok := <-ch:
		if !ok {
			t.Fatal("channel closed while waiting for an event")
		}
		return ev
	case <-time.After(2 * time.Second):
		t.Fatal("no event arrived")
		return events.Event{}
	}
}

func TestSubscriberReceivesEventsForItsTask(t *testing.T) {
	b := newBus()
	ch, stop := b.Subscribe("task_a")
	defer stop()

	b.Publish(events.Event{TaskID: "task_a", Kind: events.KindUpdate, Seq: 1, Text: "working"})

	got := receive(t, ch)
	if got.Seq != 1 || got.Text != "working" || got.Kind != events.KindUpdate {
		t.Errorf("got %+v, want the published event", got)
	}
}

// An event for one task must not reach a listener for another.
func TestEventsAreScopedToTheirTask(t *testing.T) {
	b := newBus()
	chA, stopA := b.Subscribe("task_a")
	defer stopA()
	chB, stopB := b.Subscribe("task_b")
	defer stopB()

	b.Publish(events.Event{TaskID: "task_a", Kind: events.KindUpdate, Seq: 1, Text: "for a"})

	if got := receive(t, chA); got.Text != "for a" {
		t.Errorf("subscriber a got %q", got.Text)
	}
	select {
	case ev := <-chB:
		t.Errorf("subscriber b received %+v, which belongs to another task", ev)
	case <-time.After(50 * time.Millisecond):
	}
}

// Several clients may listen to one task, as a phone and a desktop might.
func TestEveryListenerReceivesEveryEvent(t *testing.T) {
	b := newBus()

	const listeners = 3
	chans := make([]<-chan events.Event, listeners)
	for i := range chans {
		ch, stop := b.Subscribe("task_a")
		defer stop()
		chans[i] = ch
	}

	b.Publish(events.Event{TaskID: "task_a", Kind: events.KindFinal, Text: "the answer"})

	for i, ch := range chans {
		if got := receive(t, ch); got.Text != "the answer" {
			t.Errorf("listener %d got %q, want the answer", i, got.Text)
		}
	}
}

// Publishing must never block, or one stalled client holds up the task that
// is producing the messages.
func TestPublishDoesNotBlockOnAStalledListener(t *testing.T) {
	b := newBus()
	_, stop := b.Subscribe("task_a")
	defer stop()

	done := make(chan struct{})
	go func() {
		defer close(done)
		// Far more than the buffer holds, with nothing reading.
		for i := 0; i < 5000; i++ {
			b.Publish(events.Event{TaskID: "task_a", Kind: events.KindUpdate, Seq: i})
		}
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Publish blocked on a listener that was not reading")
	}
}

func TestUnsubscribeStopsDeliveryAndClosesTheChannel(t *testing.T) {
	b := newBus()
	ch, stop := b.Subscribe("task_a")

	stop()

	if _, ok := <-ch; ok {
		t.Error("channel still open after unsubscribing")
	}
	if n := b.Listeners("task_a"); n != 0 {
		t.Errorf("Listeners = %d, want 0 after unsubscribing", n)
	}
	// Publishing to nobody must not panic on a closed channel.
	b.Publish(events.Event{TaskID: "task_a", Kind: events.KindUpdate})
}

// A handler may unsubscribe more than once through deferred cleanup.
func TestUnsubscribeIsIdempotent(t *testing.T) {
	b := newBus()
	_, stop := b.Subscribe("task_a")

	stop()
	stop()
	stop()
}

func TestCloseEndsEverySubscription(t *testing.T) {
	b := newBus()
	chA, stopA := b.Subscribe("task_a")
	defer stopA()
	chB, stopB := b.Subscribe("task_b")
	defer stopB()

	b.Close()

	for name, ch := range map[string]<-chan events.Event{"a": chA, "b": chB} {
		if _, ok := <-ch; ok {
			t.Errorf("subscriber %s still open after Close", name)
		}
	}
	// Subscribing afterwards yields a channel that is already closed.
	ch, stop := b.Subscribe("task_c")
	defer stop()
	if _, ok := <-ch; ok {
		t.Error("subscribing to a closed bus yielded an open channel")
	}
	b.Close() // idempotent
}

// Subscribing and unsubscribing while events are published must be safe, which
// is what many clients connecting and disconnecting amounts to.
func TestConcurrentUseIsSafe(t *testing.T) {
	b := newBus()
	var wg sync.WaitGroup

	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				ch, stop := b.Subscribe("task_a")
				go func() {
					for range ch {
					}
				}()
				b.Publish(events.Event{TaskID: "task_a", Kind: events.KindUpdate, Seq: j})
				stop()
			}
		}()
	}
	wg.Wait()
}

func TestKindTerminal(t *testing.T) {
	for _, k := range []events.Kind{events.KindFinal, events.KindError, events.KindCancelled} {
		if !k.Terminal() {
			t.Errorf("%s.Terminal() = false, want true", k)
		}
	}
	if events.KindUpdate.Terminal() {
		t.Error("an update must not end the stream")
	}
}
