// Package events : Carries a chat's messages from the runner producing them
// to the clients listening for them.
//
// Delivery is in-process and best effort. It exists so that a client hears a
// message as it happens; the durable record is the database, and a client that
// missed something can read it there.
package events

import (
	"log/slog"
	"sync"
	"time"
)

const (
	// subscriberBuffer : How many events a subscriber may fall behind by
	// before events are dropped rather than held.
	//
	// A chat produces a handful of messages, so a subscriber this far behind
	// is not reading at all. Publishing must never block: the alternative is
	// one stalled client holding up the chat that is producing the messages.
	subscriberBuffer = 64
)

// Kind : What sort of event this is. The first four mirror a message's kind;
// the rest describe how a chat ended.
type Kind string

const (
	// KindUpdate : Transient progress. More will follow.
	KindUpdate Kind = "update"
	// KindFinal : The chat's answer. The stream ends after it.
	KindFinal Kind = "final"
	// KindError : The chat failed. The stream ends after it.
	KindError Kind = "error"
	// KindCancelled : The chat was stopped. The stream ends after it.
	KindCancelled Kind = "cancelled"
)

// Terminal : Reports whether an event of this kind ends a chat's stream.
func (k Kind) Terminal() bool {
	return k == KindFinal || k == KindError || k == KindCancelled
}

// String : Returns the kind as written on the wire.
func (k Kind) String() string { return string(k) }

// Event : One thing that happened to a chat.
type Event struct {
	// ChatID : The chat it happened to.
	ChatID string
	// Kind : What sort of event it is.
	Kind Kind
	// Seq : The message's position in the chat's stream, or zero for an
	// event that is not a stored message.
	Seq int
	// Text : What to show the user, written to be spoken aloud.
	Text string
	// At : When it happened.
	At time.Time
}

// Bus : Delivers events to whoever is listening for a given chat.
//
// It is safe for concurrent use.
type Bus struct {
	logger *slog.Logger

	mu sync.RWMutex
	// subscribers : Listeners for each chat, by chat identifier.
	subscribers map[string]map[int]chan Event
	// nextID : Distinguishes subscribers to the same chat.
	nextID int
	closed bool
}

// NewBus : Returns an empty Bus.
func NewBus(logger *slog.Logger) *Bus {
	return &Bus{
		logger:      logger,
		subscribers: map[string]map[int]chan Event{},
	}
}

// Subscribe : Returns a channel of events for one chat, and a function that
// stops the subscription.
//
// The caller must call the returned function, or the subscription is held for
// the life of the process. The channel is closed when it does, or when the Bus
// is closed.
func (b *Bus) Subscribe(chatID string) (<-chan Event, func()) {
	ch := make(chan Event, subscriberBuffer)

	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		close(ch)
		return ch, func() {}
	}
	b.nextID++
	id := b.nextID
	if b.subscribers[chatID] == nil {
		b.subscribers[chatID] = map[int]chan Event{}
	}
	b.subscribers[chatID][id] = ch
	b.mu.Unlock()

	var once sync.Once
	return ch, func() {
		once.Do(func() {
			b.mu.Lock()
			defer b.mu.Unlock()
			if subs, ok := b.subscribers[chatID]; ok {
				if _, ok := subs[id]; ok {
					delete(subs, id)
					close(ch)
				}
				if len(subs) == 0 {
					delete(b.subscribers, chatID)
				}
			}
		})
	}
}

// Publish : Delivers an event to every listener for its chat.
//
// It never blocks. A listener too far behind to accept the event loses it
// rather than delaying the chat that produced it.
func (b *Bus) Publish(ev Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	for _, ch := range b.subscribers[ev.ChatID] {
		select {
		case ch <- ev:
		default:
			b.logger.Warn("dropped event for a listener that is not keeping up",
				slog.String("chat_id", ev.ChatID),
				slog.String("kind", string(ev.Kind)),
				slog.Int("seq", ev.Seq))
		}
	}
}

// Listeners : Reports how many subscriptions a chat has.
func (b *Bus) Listeners(chatID string) int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.subscribers[chatID])
}

// Close : Ends every subscription. Publishing afterwards does nothing.
func (b *Bus) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	b.closed = true
	for chatID, subs := range b.subscribers {
		for id, ch := range subs {
			close(ch)
			delete(subs, id)
		}
		delete(b.subscribers, chatID)
	}
}
