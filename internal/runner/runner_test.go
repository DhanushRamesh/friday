package runner_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/chat"
	"github.com/DhanushRamesh/personal-assistant/internal/chat/memory"
	"github.com/DhanushRamesh/personal-assistant/internal/environment"
	"github.com/DhanushRamesh/personal-assistant/internal/events"
	"github.com/DhanushRamesh/personal-assistant/internal/runner"
)

// discard : A logger that writes nowhere.
func discard() *slog.Logger { return slog.New(slog.NewJSONHandler(io.Discard, nil)) }

// harness : A runner with an in-memory repository and a stub environment.
type harness struct {
	runner *runner.Runner
	repo   *memory.Repository
	bus    *events.Bus
	convID string
}

// listen : Collects the progress a chat announces, as a client would hear it.
//
// Returns a function giving what has arrived so far, since the updates come
// on another goroutine and the test reads them once the chat has finished.
func (h *harness) listen(t *testing.T, chatID string) func() []string {
	t.Helper()
	ch, stop := h.bus.Subscribe(chatID)
	t.Cleanup(stop)

	var mu sync.Mutex
	var got []string
	go func() {
		for ev := range ch {
			if ev.Kind != events.KindUpdate {
				continue
			}
			mu.Lock()
			got = append(got, ev.Text)
			mu.Unlock()
		}
	}()
	return func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), got...)
	}
}

// conversation : Returns the harness's conversation, creating it on first use.
func (h *harness) conversation(t *testing.T) string {
	t.Helper()
	if h.convID == "" {
		c := chat.NewConversation("", "")
		if err := h.repo.CreateConversation(context.Background(), c); err != nil {
			t.Fatalf("CreateConversation: %v", err)
		}
		h.convID = c.ID
	}
	return h.convID
}

// newHarness : Builds a runner around the given environment.
func newHarness(t *testing.T, p environment.Environment, opts runner.Options) *harness {
	t.Helper()

	repo := memory.New()
	bus := events.NewBus(discard())
	opts.Repository = repo
	opts.Messages = repo
	opts.Publisher = bus
	opts.Environment = p
	opts.Logger = discard()

	r, err := runner.New(opts)
	if err != nil {
		t.Fatalf("runner.New: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = r.Shutdown(ctx)
	})
	return &harness{runner: r, repo: repo, bus: bus}
}

// submit : Creates and stores a chat, then starts it running.
// submitOn : Creates and starts a chat that arrived by a given channel.
func (h *harness) submitOn(t *testing.T, channel chat.Channel, prompt string) *chat.Chat {
	t.Helper()
	tk, err := chat.New(h.conversation(t), channel, prompt)
	if err != nil {
		t.Fatalf("chat.New: %v", err)
	}
	if err := h.repo.Create(context.Background(), tk); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := h.runner.Submit(tk); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	return tk
}

func (h *harness) submit(t *testing.T, prompt string) *chat.Chat {
	t.Helper()
	tk, err := chat.New(h.conversation(t), chat.ChannelDirect, prompt)
	if err != nil {
		t.Fatalf("chat.New: %v", err)
	}
	if err := h.repo.Create(context.Background(), tk); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := h.runner.Submit(tk); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	return tk
}

// await : Waits for a chat to reach one of the given statuses.
func (h *harness) await(t *testing.T, id string, want ...chat.Status) *chat.Chat {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var last chat.Status

	for time.Now().Before(deadline) {
		got, err := h.repo.Get(context.Background(), id)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		last = got.Status
		for _, w := range want {
			if got.Status == w {
				return got
			}
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("chat %s stayed %q, waiting for one of %v", id, last, want)
	return nil
}

func TestRunToCompletionAnnouncesProgressAndStoresTheResult(t *testing.T) {
	updates := []string{"Let me take a look.", "Still working on it."}
	h := newHarness(t, &environment.Stub{Updates: updates}, runner.Options{})

	tk := h.submit(t, "check my merge requests")
	heard := h.listen(t, tk.ID)
	done := h.await(t, tk.ID, chat.StatusCompleted, chat.StatusFailed)

	if done.Status != chat.StatusCompleted {
		t.Fatalf("Status = %q (%s), want completed", done.Status, done.Error)
	}
	if !strings.Contains(done.Response, "check my merge requests") {
		t.Errorf("Response = %q, want it to reflect the prompt", done.Response)
	}
	if done.StartedAt == nil || done.FinishedAt == nil {
		t.Error("start or finish time not recorded")
	}

	// Progress is announced and not stored. It was stored once, so a dropped
	// stream could replay it; there is no such stream now, and where a chat
	// had got to an hour ago is worth nothing.
	got := heard()
	if len(got) != len(updates) {
		t.Fatalf("heard %d updates %v, want %d", len(got), got, len(updates))
	}
	for i, want := range updates {
		if got[i] != want {
			t.Errorf("update %d = %q, want %q", i, got[i], want)
		}
	}
}

func TestProviderFailureFailsTheChat(t *testing.T) {
	const reason = "The service did not respond in time."
	h := newHarness(t, &environment.Stub{Updates: []string{"working"}, FailWith: reason}, runner.Options{})

	tk := h.submit(t, "do something")
	done := h.await(t, tk.ID, chat.StatusFailed, chat.StatusCompleted)

	if done.Status != chat.StatusFailed {
		t.Fatalf("Status = %q, want failed", done.Status)
	}
	if done.Error != reason {
		t.Errorf("Error = %q, want %q", done.Error, reason)
	}
}

// A provider that cannot start at all must fail the chat with something a
// user can hear, not a raw error.
func TestProviderThatWillNotStartFailsTheChat(t *testing.T) {
	h := newHarness(t, &refusingProvider{}, runner.Options{})

	tk := h.submit(t, "do something")
	done := h.await(t, tk.ID, chat.StatusFailed, chat.StatusCompleted)

	if done.Status != chat.StatusFailed {
		t.Fatalf("Status = %q, want failed", done.Status)
	}
	if done.Error == "" {
		t.Error("no explanation recorded")
	}
	if strings.Contains(done.Error, "refusing") {
		t.Errorf("Error = %q, which exposes the internal error", done.Error)
	}
}

// Cancelling mid-run must stop the chat and record it, which is what saying
// "stop" while the assistant is speaking will do.
func TestCancelStopsARunningChat(t *testing.T) {
	h := newHarness(t, &environment.Stub{
		Updates: []string{"one", "two", "three", "four", "five"},
		Delay:   30 * time.Millisecond,
	}, runner.Options{})

	tk := h.submit(t, "a long job")
	h.await(t, tk.ID, chat.StatusRunning)

	if !h.runner.Cancel(tk.ID) {
		t.Fatal("Cancel reported no running chat")
	}

	done := h.await(t, tk.ID, chat.StatusCancelled, chat.StatusCompleted, chat.StatusFailed)
	if done.Status != chat.StatusCancelled {
		t.Errorf("Status = %q, want cancelled", done.Status)
	}
	if done.Response != "" {
		t.Errorf("a cancelled chat has a response: %q", done.Response)
	}
	if done.FinishedAt == nil {
		t.Error("finish time not recorded for a cancelled chat")
	}
}

func TestCancelReportsWhenNothingIsRunning(t *testing.T) {
	h := newHarness(t, &environment.Stub{}, runner.Options{})

	if h.runner.Cancel(chat.NewID()) {
		t.Error("Cancel reported stopping a chat that was never running")
	}
}

// A chat that outlives its deadline must be stopped and explained, rather
// than holding its slot until the process restarts.
func TestChatExceedingItsDeadlineFails(t *testing.T) {
	h := newHarness(t, &environment.Stub{
		Updates: []string{"one", "two", "three", "four", "five", "six"},
		Delay:   50 * time.Millisecond,
	}, runner.Options{ChatTimeout: 40 * time.Millisecond})

	tk := h.submit(t, "a job that runs too long")
	done := h.await(t, tk.ID, chat.StatusFailed, chat.StatusCompleted, chat.StatusCancelled)

	if done.Status != chat.StatusFailed {
		t.Fatalf("Status = %q, want failed", done.Status)
	}
	if !strings.Contains(strings.ToLower(done.Error), "too long") {
		t.Errorf("Error = %q, want it to explain the chat took too long", done.Error)
	}
}

// Shutdown must leave no chat stuck running, and must say what happened
// rather than looking like the user cancelled it.
func TestShutdownStopsAndRecordsRunningChats(t *testing.T) {
	repo := memory.New()
	r, err := runner.New(runner.Options{
		Repository:  repo,
		Messages:    repo,
		Environment: &environment.Stub{Updates: []string{"a", "b", "c", "d"}, Delay: 40 * time.Millisecond},
		Logger:      discard(),
	})
	if err != nil {
		t.Fatalf("runner.New: %v", err)
	}

	tk, err := chat.New("", chat.ChannelDirect, "interrupted by shutdown")
	if err != nil {
		t.Fatalf("chat.New: %v", err)
	}
	if err := repo.Create(context.Background(), tk); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := r.Submit(tk); err != nil {
		t.Fatalf("Submit: %v", err)
	}

	// Wait until it is actually running before shutting down.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		got, _ := repo.Get(context.Background(), tk.ID)
		if got.Status == chat.StatusRunning {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := r.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	got, err := repo.Get(context.Background(), tk.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !got.Status.IsTerminal() {
		t.Errorf("Status = %q, want a terminal status after shutdown", got.Status)
	}
	if !strings.Contains(strings.ToLower(got.Error), "shut down") {
		t.Errorf("Error = %q, want it to say the server shut down", got.Error)
	}

	// Nothing may be submitted afterwards.
	if err := r.Submit(tk); err == nil {
		t.Error("Submit after Shutdown succeeded, want an error")
	}
}

func TestRecoverFailsChatsLeftRunning(t *testing.T) {
	h := newHarness(t, &environment.Stub{}, runner.Options{})
	ctx := context.Background()

	stranded, err := chat.New("", chat.ChannelDirect, "was running when the process died")
	if err != nil {
		t.Fatalf("chat.New: %v", err)
	}
	if err := stranded.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := h.repo.Create(ctx, stranded); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := h.runner.Recover(ctx); err != nil {
		t.Fatalf("Recover: %v", err)
	}

	got, err := h.repo.Get(ctx, stranded.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != chat.StatusFailed {
		t.Errorf("Status = %q, want failed", got.Status)
	}
	if !strings.Contains(strings.ToLower(got.Error), "restarted") {
		t.Errorf("Error = %q, want it to mention the restart", got.Error)
	}
}

// Only MaxConcurrent chats run at once; the rest stay pending until a slot
// frees, rather than appearing to run while they wait.
func TestConcurrencyIsLimited(t *testing.T) {
	const limit = 2
	h := newHarness(t, &environment.Stub{Updates: []string{"a", "b"}, Delay: 60 * time.Millisecond},
		runner.Options{MaxConcurrent: limit})

	var ids []string
	for i := 0; i < 5; i++ {
		ids = append(ids, h.submit(t, "concurrent job").ID)
	}

	// Sample repeatedly; at no point may more than the limit be running.
	deadline := time.Now().Add(400 * time.Millisecond)
	sawPending := false
	for time.Now().Before(deadline) {
		running := 0
		pending := 0
		for _, id := range ids {
			got, err := h.repo.Get(context.Background(), id)
			if err != nil {
				t.Fatalf("Get: %v", err)
			}
			switch got.Status {
			case chat.StatusRunning:
				running++
			case chat.StatusPending:
				pending++
			}
		}
		if running > limit {
			t.Fatalf("%d chats running at once, want no more than %d", running, limit)
		}
		if pending > 0 && running == limit {
			sawPending = true
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !sawPending {
		t.Error("never observed a chat waiting while the limit was in use")
	}
}

// A message that cannot be stored must not lose the answer with it.
func TestUnstorableMessageDoesNotFailTheChat(t *testing.T) {
	h := newHarness(t, &environment.Stub{Updates: []string{"progress"}}, runner.Options{})
	h.repo.FailAppends(errors.New("messages table is full"))

	tk := h.submit(t, "answer me anyway")
	done := h.await(t, tk.ID, chat.StatusCompleted, chat.StatusFailed)

	if done.Status != chat.StatusCompleted {
		t.Errorf("Status = %q, want the chat to complete despite the message failing", done.Status)
	}
}

func TestNewRequiresItsDependencies(t *testing.T) {
	full := func() runner.Options {
		repo := memory.New()
		return runner.Options{
			Repository:  repo,
			Messages:    repo,
			Environment: &environment.Stub{},
			Logger:      discard(),
		}
	}

	cases := map[string]func(*runner.Options){
		"no repository": func(o *runner.Options) { o.Repository = nil },
		"no provider":   func(o *runner.Options) { o.Environment = nil },
		"no logger":     func(o *runner.Options) { o.Logger = nil },
		"no messages":   func(o *runner.Options) { o.Messages = nil },
	}
	for name, remove := range cases {
		opts := full()
		remove(&opts)
		if _, err := runner.New(opts); err == nil {
			t.Errorf("New with %s: want an error, got nil", name)
		}
	}
}

func TestDefaultsApplied(t *testing.T) {
	h := newHarness(t, &environment.Stub{}, runner.Options{})
	tk := h.submit(t, "use the defaults")
	if got := h.await(t, tk.ID, chat.StatusCompleted, chat.StatusFailed); got.Status != chat.StatusCompleted {
		t.Errorf("Status = %q, want completed with default settings", got.Status)
	}
}

// refusingProvider : A provider that cannot start a run at all.
type refusingProvider struct{}

// Name : Returns the provider's name.
func (refusingProvider) Name() string { return "refusing" }

// Run : Always reports that it cannot start.
func (refusingProvider) Run(context.Context, environment.Request) (<-chan environment.Message, error) {
	return nil, errors.New("refusing: no credentials configured")
}

// A chat waiting for a slot must be cancellable, not only one already
// running. Saying "stop" should work whichever it is.
func TestCancelStopsAQueuedChat(t *testing.T) {
	h := newHarness(t, &environment.Stub{
		Updates: []string{"a", "b", "c"},
		Delay:   80 * time.Millisecond,
	}, runner.Options{MaxConcurrent: 1})

	// The first chat takes the only slot.
	blocker := h.submit(t, "holds the slot")
	h.await(t, blocker.ID, chat.StatusRunning)

	queued := h.submit(t, "waits behind it")

	// It must still be pending, and cancelling it must work.
	got, err := h.repo.Get(context.Background(), queued.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != chat.StatusPending {
		t.Fatalf("Status = %q, want pending while waiting for a slot", got.Status)
	}

	if !h.runner.Cancel(queued.ID) {
		t.Fatal("Cancel reported nothing to stop for a queued chat")
	}

	done := h.await(t, queued.ID, chat.StatusCancelled, chat.StatusRunning, chat.StatusCompleted)
	if done.Status != chat.StatusCancelled {
		t.Errorf("Status = %q, want cancelled", done.Status)
	}
	if done.StartedAt != nil {
		t.Error("a chat cancelled before running has a start time")
	}
}
