// Package runner : Executes chats.
//
// It joins the three pieces that otherwise know nothing of each other: a chat,
// which records where it is in its lifecycle; a provider, which produces the
// stream of messages answering it; and a repository, which stores both.
package runner

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/chat"
	"github.com/DhanushRamesh/personal-assistant/internal/events"
	"github.com/DhanushRamesh/personal-assistant/internal/logging"
	"github.com/DhanushRamesh/personal-assistant/internal/provider"
	"github.com/DhanushRamesh/personal-assistant/internal/session"
)

const (
	// DefaultChatTimeout : How long a chat may run before it is abandoned.
	// Without a deadline a wedged provider call holds its slot until the
	// process restarts.
	DefaultChatTimeout = 5 * time.Minute

	// DefaultMaxConcurrent : How many chats may run at once. The rest wait,
	// staying pending until a slot frees.
	DefaultMaxConcurrent = 4

	// interruptedReason : Recorded against chats found still running at
	// startup, which no process is working on any more.
	interruptedReason = "The server restarted while this was running, so it did not finish."

	// timeoutReason : Recorded when a chat outlives its deadline.
	timeoutReason = "This took too long, so I stopped it."

	// shutdownReason : Recorded when a chat is stopped because FRIDAY is
	// shutting down.
	shutdownReason = "The server shut down before this finished."
)

// Publisher : Somewhere to announce what a chat is doing.
type Publisher interface {
	// Publish : Delivers an event to whoever is listening. It must not block.
	Publish(ev events.Event)
}

// Options : The dependencies and settings a Runner is built from.
type Options struct {
	// Repository : Stores chats and their messages. Required.
	Repository chat.Repository
	// Provider : Answers prompts. Required.
	Provider provider.Provider
	// Logger : Receives execution records. Required.
	Logger *slog.Logger
	// Publisher : Receives a chat's messages as they happen, for clients
	// listening to it. Optional; without one a chat still runs and is still
	// recorded, but nothing hears it until it is read back.
	Publisher Publisher
	// ChatTimeout : How long a chat may run. Zero selects
	// DefaultChatTimeout.
	ChatTimeout time.Duration
	// MaxConcurrent : How many chats may run at once. Zero selects
	// DefaultMaxConcurrent.
	MaxConcurrent int
	// Messages : Where the conversation is read and written. Required.
	Messages session.Repository
	// HistoryBudget : How much of a session, in bytes of text, is sent to
	// the provider. Zero selects session.DefaultBudget.
	HistoryBudget int
}

// Runner : Executes chats in the background.
//
// It is safe for concurrent use.
type Runner struct {
	repo          chat.Repository
	messages      session.Repository
	provider      provider.Provider
	publisher     Publisher
	logger        *slog.Logger
	chatTimeout   time.Duration
	historyBudget int

	// slots : Limits how many chats run at once. A chat holds one for the
	// whole of its run.
	slots chan struct{}

	// base : The lifetime of every chat. Deliberately not derived from the
	// request that submitted one: that context ends when its response is
	// sent, which would cancel the chat the moment the caller was told it had
	// started.
	base         context.Context
	stopBase     context.CancelFunc
	wg           sync.WaitGroup
	mu           sync.Mutex
	active       map[string]*activeChat
	shuttingDown bool
}

// activeChat : A chat currently running, and the means to stop it.
type activeChat struct {
	cancel context.CancelFunc
	// reason : Why the chat was stopped, recorded before cancelling so the
	// goroutine can tell a user's cancellation from a shutdown.
	reason string
}

// New : Builds a Runner. It reports an error if a required dependency is
// missing.
func New(opts Options) (*Runner, error) {
	switch {
	case opts.Repository == nil:
		return nil, errors.New("runner: a repository is required")
	case opts.Provider == nil:
		return nil, errors.New("runner: a provider is required")
	case opts.Logger == nil:
		return nil, errors.New("runner: a logger is required")
	case opts.Messages == nil:
		return nil, errors.New("runner: a message store is required")
	}

	if opts.ChatTimeout <= 0 {
		opts.ChatTimeout = DefaultChatTimeout
	}
	if opts.MaxConcurrent <= 0 {
		opts.MaxConcurrent = DefaultMaxConcurrent
	}
	if opts.HistoryBudget <= 0 {
		opts.HistoryBudget = session.DefaultBudget
	}

	base, stop := context.WithCancel(context.Background())
	return &Runner{
		repo:          opts.Repository,
		messages:      opts.Messages,
		provider:      opts.Provider,
		publisher:     opts.Publisher,
		logger:        opts.Logger,
		chatTimeout:   opts.ChatTimeout,
		historyBudget: opts.HistoryBudget,
		slots:         make(chan struct{}, opts.MaxConcurrent),
		base:          base,
		stopBase:      stop,
		active:        map[string]*activeChat{},
	}, nil
}

// Recover : Fails every chat the database still records as running.
//
// It is called once at startup. A process that stopped mid-chat leaves rows
// reading running that nothing is working on and nothing will ever move.
func (r *Runner) Recover(ctx context.Context) error {
	changed, err := r.repo.FailRunning(ctx, interruptedReason)
	if err != nil {
		return err
	}
	if changed > 0 {
		r.logger.WarnContext(ctx, "failed chats interrupted by a restart",
			slog.Int64("chats", changed))
	}
	return nil
}

// Submit : Starts running a chat in the background and returns at once.
//
// The chat must already be stored. It reports an error only if the Runner is
// shutting down.
//
// The Runner works on its own copy. The caller keeps the chat it passed and is
// free to go on reading it, which a handler does when rendering its response;
// sharing one would have two goroutines reading and writing the same struct.
func (r *Runner) Submit(t *chat.Chat) error {
	own := *t

	ctx := logging.WithAttrs(r.base,
		slog.String("chat_id", t.ID),
		slog.String("provider", r.provider.Name()))
	lifeCtx, stopLife := context.WithCancel(ctx)

	r.mu.Lock()
	if r.shuttingDown {
		r.mu.Unlock()
		stopLife()
		return errors.New("runner: shutting down")
	}
	// Registered here rather than inside the goroutine, so that a chat is
	// cancellable the moment Submit returns. Registering later leaves a
	// window in which a cancellation silently does nothing.
	r.active[t.ID] = &activeChat{cancel: stopLife}
	r.wg.Add(1)
	r.mu.Unlock()

	go func() {
		defer r.wg.Done()
		defer stopLife()
		defer func() {
			r.mu.Lock()
			delete(r.active, t.ID)
			r.mu.Unlock()
		}()
		r.execute(ctx, lifeCtx, &own)
	}()
	return nil
}

// Cancel : Stops a running chat. It reports whether one was running.
func (r *Runner) Cancel(id string) bool {
	return r.stop(id, "")
}

// Shutdown : Stops every running chat and waits for them to record their
// state, or until ctx ends.
func (r *Runner) Shutdown(ctx context.Context) error {
	r.mu.Lock()
	r.shuttingDown = true
	for id := range r.active {
		r.active[id].reason = shutdownReason
		r.active[id].cancel()
	}
	r.mu.Unlock()

	done := make(chan struct{})
	go func() {
		r.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		r.stopBase()
		return nil
	case <-ctx.Done():
		// Give up waiting and cut the chats off where they are.
		r.stopBase()
		return fmt.Errorf("runner: chats did not finish before shutdown: %w", ctx.Err())
	}
}

// stop : Cancels a chat, recording why. It reports whether one was running.
func (r *Runner) stop(id, reason string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	at, ok := r.active[id]
	if !ok {
		return false
	}
	at.reason = reason
	at.cancel()
	return true
}

// cancelReason : Returns why a chat was stopped, if it was stopped
// deliberately.
func (r *Runner) cancelReason(id string) (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	at, ok := r.active[id]
	if !ok {
		return "", false
	}
	return at.reason, true
}
