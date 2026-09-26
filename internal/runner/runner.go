// Package runner : Executes chats.
//
// It joins the three pieces that otherwise know nothing of each other: a chat,
// which records where it is in its lifecycle; an environment, which produces the
// stream of messages answering it; and a repository, which stores both.
package runner

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/announce"
	"github.com/DhanushRamesh/personal-assistant/internal/chat"
	"github.com/DhanushRamesh/personal-assistant/internal/conversation"
	"github.com/DhanushRamesh/personal-assistant/internal/environment"
	"github.com/DhanushRamesh/personal-assistant/internal/events"
	"github.com/DhanushRamesh/personal-assistant/internal/llm"
	"github.com/DhanushRamesh/personal-assistant/internal/logging"
	"github.com/DhanushRamesh/personal-assistant/internal/memory"
	"github.com/DhanushRamesh/personal-assistant/internal/persona"
	"github.com/DhanushRamesh/personal-assistant/internal/tool"
)

const (
	// DefaultChatTimeout : How long a chat may run before it is abandoned.
	// Without a deadline a wedged provider call holds its slot until the
	// process restarts.
	DefaultChatTimeout = 5 * time.Minute

	// DefaultMaxConcurrent : How many chats may run at once. The rest wait,
	// staying pending until a slot frees.
	DefaultMaxConcurrent = 4

	// DefaultCondenseTimeout : How long condensing an old conversation may
	// take. Shorter than a chat, because nobody is waiting for it and a slow
	// one delays the next chat for no benefit.
	DefaultCondenseTimeout = 2 * time.Minute

	// interruptedReason : Recorded against chats found still running at
	// startup, which no process is working on any more.
	interruptedReason = "The server restarted while this was running, so it did not finish."

	// timeoutReason : Recorded when a chat outlives its deadline.
	timeoutReason = "This took too long, so I stopped it."

	// shutdownReason : Recorded when a chat is stopped because the server is
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
	// Environment : Where prompts are sent to be answered. Required.
	Environment environment.Environment
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
	Messages conversation.Repository
	// HistoryLimits : The ceilings the conversation sent to the provider is
	// held under. A zero size selects conversation.DefaultBudget, and a zero
	// count means the provider accepts any number of messages.
	HistoryLimits conversation.Limits
	// CondenseTimeout : How long condensing an old conversation may take.
	// Zero selects DefaultCondenseTimeout.
	CondenseTimeout time.Duration
	// AssistantName : What the assistant calls itself when it introduces
	// itself. Empty leaves it nameless.
	AssistantName string
	// Persona : The manner it answers in, read afresh on every prompt so a
	// change takes effect without a restart. Nil leaves it answering
	// plainly.
	Persona *persona.Setting
	// Announcer : Where something is said that nobody asked for, such as the
	// name a conversation has just been given. Nil says nothing.
	Announcer announce.Announcer
	// Tools : What the assistant can do as well as say. Nil offers none, and
	// a model offered none answers from what it knows.
	Tools *tool.Registry
	// Memory : What the assistant has been asked to remember. Nil remembers
	// nothing, which is how it behaved before there was anywhere to keep
	// anything.
	Memory *memory.Recall

	// Now : The time where the person is. Nil uses UTC, which is right
	// nowhere anybody lives but is at least a real time.
	Now func() time.Time
}

// Runner : Executes chats in the background.
//
// It is safe for concurrent use.
type Runner struct {
	repo            chat.Repository
	messages        conversation.Repository
	environment     environment.Environment
	publisher       Publisher
	logger          *slog.Logger
	chatTimeout     time.Duration
	historyLimits   conversation.Limits
	condenseTimeout time.Duration
	assistantName   string
	persona         *persona.Setting
	announcer       announce.Announcer
	tools           *tool.Registry
	memory          *memory.Recall
	now             func() time.Time

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
	case opts.Environment == nil:
		return nil, errors.New("runner: an environment is required")
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
	if opts.CondenseTimeout <= 0 {
		opts.CondenseTimeout = DefaultCondenseTimeout
	}
	if opts.Now == nil {
		opts.Now = func() time.Time { return time.Now().UTC() }
	}

	base, stop := context.WithCancel(context.Background())
	return &Runner{
		repo:            opts.Repository,
		messages:        opts.Messages,
		environment:     opts.Environment,
		publisher:       opts.Publisher,
		logger:          opts.Logger,
		chatTimeout:     opts.ChatTimeout,
		historyLimits:   opts.HistoryLimits,
		condenseTimeout: opts.CondenseTimeout,
		assistantName:   opts.AssistantName,
		persona:         opts.Persona,
		announcer:       opts.Announcer,
		tools:           opts.Tools,
		memory:          opts.Memory,
		now:             opts.Now,
		slots:           make(chan struct{}, opts.MaxConcurrent),
		base:            base,
		stopBase:        stop,
		active:          map[string]*activeChat{},
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
		slog.String("environment", r.environment.Name()))
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

// promptFor : How the assistant is told to answer this chat.
//
// Read afresh rather than stamped onto the chat when it was accepted, so a
// manner chosen in the settings takes effect on the next prompt.
func (r *Runner) prompt() string {
	id := persona.Default
	if r.persona != nil {
		id = r.persona.Current()
	}
	return persona.Prompt(id, r.assistantName)
}

// promptFor : The system prompt, with where this chat is being answered.
//
// The manner does not change per chat and the whereabouts do, so they are
// joined here rather than in the persona. Asked which conversation this is,
// an assistant that has not been told answers from whatever it remembers
// doing, and remembering having switched somewhere is not the same as being
// there.
func (r *Runner) promptFor(ctx context.Context, t *chat.Chat) string {
	standing := r.prompt() + " " + conversation.Now(r.now()) + heard(t)

	// The conversation is read once: it carries both where the assistant is
	// and whose memories these are.
	var userID string
	if t.ConversationID != "" && r.repo != nil {
		c, err := r.repo.GetConversation(ctx, t.ConversationID)
		if err != nil {
			r.logger.ErrorContext(ctx, "cannot tell the assistant where it is",
				slog.Any("error", err))
		} else {
			standing += " " + conversation.Whereabouts(c.ID, c.Title)
			userID = c.UserID
		}
	}

	// Gathered while the blocks are built, so what is recorded is what the
	// model was actually shown rather than a second search that might not
	// agree with it.
	var note chat.Recalled
	started := time.Now()

	prompt := join(standing,
		r.known(ctx, userID, &note),
		r.recalled(ctx, userID, t.Prompt, &note),
		r.quoted(ctx, userID, t.Prompt, t.ConversationID, &note))

	note.TookMS = time.Since(started).Milliseconds()
	r.recordRecalled(ctx, t, note)

	return prompt
}

// join : The parts of a system prompt that are not empty, separated so the
// model reads them as separate things rather than one run-on instruction.
func join(parts ...string) string {
	kept := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			kept = append(kept, part)
		}
	}
	return strings.Join(kept, "\n\n")
}

// known : The memories that go into every prompt, if there are any.
func (r *Runner) known(ctx context.Context, userID string, note *chat.Recalled) string {
	if r.memory == nil || userID == "" {
		return ""
	}

	all, err := r.memory.Always(ctx, userID)
	if err != nil {
		r.logger.ErrorContext(ctx, "cannot read what the assistant always knows",
			slog.Any("error", err))
		return ""
	}
	for i := range all {
		note.Always = append(note.Always, chat.RecalledNote{ID: all[i].ID, Text: all[i].Text()})
	}
	return memory.Standing(all)
}

// recalled : The memories that resemble what was asked, if any do.
//
// Records that they were offered, since a memory that is never found and one
// that is found every time and never helps are different problems. Nothing
// here may fail the turn: an answer with no memory is the answer that was
// given before there was any.
func (r *Runner) recalled(ctx context.Context, userID, question string, note *chat.Recalled) string {
	if r.memory == nil || userID == "" {
		return ""
	}

	found, err := r.memory.For(ctx, userID, question)
	if err != nil {
		r.logger.WarnContext(ctx, "cannot search what the assistant remembers",
			slog.Any("error", err))
		return ""
	}
	if len(found) == 0 {
		return ""
	}

	if err := r.memory.Store.Used(ctx, memory.IDs(found)); err != nil {
		r.logger.WarnContext(ctx, "cannot record that memories were offered",
			slog.Any("error", err))
	}

	for i := range found {
		note.Notes = append(note.Notes, chat.RecalledNote{
			ID:    found[i].Memory.ID,
			Text:  found[i].Memory.Text(),
			Score: found[i].Score,
		})
		note.ByWords = note.ByWords || found[i].ByWords
	}
	return memory.Offered(found)
}

// quoted : Past exchanges that resemble what was asked.
//
// Nothing from the conversation in progress, which is already in front of
// the model. Like recall, this may not fail the turn.
func (r *Runner) quoted(ctx context.Context, userID, question, conversationID string, note *chat.Recalled) string {
	if r.memory == nil || userID == "" {
		return ""
	}

	heard, err := r.memory.Said(ctx, userID, question, conversationID)
	if err != nil {
		r.logger.WarnContext(ctx, "cannot search the transcript",
			slog.Any("error", err))
		return ""
	}
	for i := range heard {
		note.Exchanges = append(note.Exchanges, chat.RecalledExchange{
			MessageID:      heard[i].Exchange.MessageID,
			ConversationID: heard[i].Exchange.ConversationID,
			Text:           heard[i].Exchange.Text,
			Score:          heard[i].Score,
			At:             heard[i].Exchange.At,
		})
	}
	// The person's own zone, which is the one r.now already works in.
	return memory.Quoted(heard, r.now().Location())
}

// recordRecalled : Stores what was put in front of the model.
//
// Observability, so a failure is logged and dropped: an answer whose
// timeline is missing is the answer that was given before there were
// timelines.
func (r *Runner) recordRecalled(ctx context.Context, t *chat.Chat, note chat.Recalled) {
	if r.repo == nil || note.Empty() {
		return
	}
	if err := r.repo.SetRecalled(ctx, t.ID, &note); err != nil {
		r.logger.WarnContext(ctx, "cannot record what was recalled",
			slog.Any("error", err))
	}
}

// heard : The warning that the words were spoken, for a chat that was.
//
// Not added to a typed turn. Typing means what it says, and reinterpreting a
// word somebody chose deliberately is worse than taking it literally.
func heard(t *chat.Chat) string {
	if t.Channel != chat.ChannelVoice {
		return ""
	}
	return " " + conversation.Heard()
}

// limitsFor : The ceilings a chat's history is held under.
//
// The count and the byte budget are the same for every chat. The context
// window is the chosen model's, so a client answered by a smaller model is
// sent less. A chosen model nobody has catalogued declares no window, leaving
// the byte budget to bound it alone.
func (r *Runner) limitsFor(m chat.Model, alongside int) conversation.Limits {
	limits := r.historyLimits
	if m.Chosen() {
		limits.ContextTokens = llm.ContextTokens(m.Vendor, m.ID)
	}
	limits.ReserveTokens = conversation.ReserveFor(alongside)
	return limits
}

// alongside : How much is sent with the history but is not part of it.
//
// The system prompt as it will actually be sent, and every tool offered
// whether the turn uses them or not. The composed prompt is passed in rather
// than rebuilt: composing it reads the database and searches memory, and it
// must be the same string the provider is given or the reserve is wrong.
func (r *Runner) alongside(t *chat.Chat, systemPrompt string) int {
	n := len(systemPrompt)
	for _, spec := range r.offered(t) {
		n += len(spec.Name) + len(spec.Description) + len(spec.Parameters)
	}
	return n
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
