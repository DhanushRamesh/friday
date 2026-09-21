package runner_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/DhanushRamesh/friday/internal/provider"
	"github.com/DhanushRamesh/friday/internal/runner"
	"github.com/DhanushRamesh/friday/internal/task"
)

// discard : A logger that writes nowhere.
func discard() *slog.Logger { return slog.New(slog.NewJSONHandler(io.Discard, nil)) }

// harness : A runner with an in-memory repository and a stub provider.
type harness struct {
	runner *runner.Runner
	repo   *memRepo
	convID string
}

// conversation : Returns the harness's conversation, creating it on first use.
func (h *harness) conversation(t *testing.T) string {
	t.Helper()
	if h.convID == "" {
		c := task.NewConversation("", "")
		if err := h.repo.CreateConversation(context.Background(), c); err != nil {
			t.Fatalf("CreateConversation: %v", err)
		}
		h.convID = c.ID
	}
	return h.convID
}

// newHarness : Builds a runner around the given provider.
func newHarness(t *testing.T, p provider.Provider, opts runner.Options) *harness {
	t.Helper()

	repo := newMemRepo()
	opts.Repository = repo
	opts.Provider = p
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
	return &harness{runner: r, repo: repo}
}

// submit : Creates and stores a task, then starts it running.
func (h *harness) submit(t *testing.T, prompt string) *task.Task {
	t.Helper()
	tk, err := task.New(h.conversation(t), prompt)
	if err != nil {
		t.Fatalf("task.New: %v", err)
	}
	if err := h.repo.Create(context.Background(), tk); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := h.runner.Submit(tk); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	return tk
}

// await : Waits for a task to reach one of the given statuses.
func (h *harness) await(t *testing.T, id string, want ...task.Status) *task.Task {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var last task.Status

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
	t.Fatalf("task %s stayed %q, waiting for one of %v", id, last, want)
	return nil
}

func TestRunToCompletionStoresMessagesAndResult(t *testing.T) {
	updates := []string{"Let me take a look.", "Still working on it."}
	h := newHarness(t, &provider.Stub{Updates: updates}, runner.Options{})

	tk := h.submit(t, "check my merge requests")
	done := h.await(t, tk.ID, task.StatusCompleted, task.StatusFailed)

	if done.Status != task.StatusCompleted {
		t.Fatalf("Status = %q (%s), want completed", done.Status, done.Error)
	}
	if !strings.Contains(done.Response, "check my merge requests") {
		t.Errorf("Response = %q, want it to reflect the prompt", done.Response)
	}
	if done.StartedAt == nil || done.FinishedAt == nil {
		t.Error("start or finish time not recorded")
	}

	// The transient messages are stored; the final one is not, since it lives
	// in the task's response.
	got := h.repo.texts(tk.ID)
	if len(got) != len(updates) {
		t.Fatalf("stored %d messages %v, want %d", len(got), got, len(updates))
	}
	for i, want := range updates {
		if got[i] != want {
			t.Errorf("message %d = %q, want %q", i, got[i], want)
		}
	}
	for _, text := range got {
		if text == done.Response {
			t.Error("the final message was stored as a transient one as well")
		}
	}
}

func TestProviderFailureFailsTheTask(t *testing.T) {
	const reason = "The service did not respond in time."
	h := newHarness(t, &provider.Stub{Updates: []string{"working"}, FailWith: reason}, runner.Options{})

	tk := h.submit(t, "do something")
	done := h.await(t, tk.ID, task.StatusFailed, task.StatusCompleted)

	if done.Status != task.StatusFailed {
		t.Fatalf("Status = %q, want failed", done.Status)
	}
	if done.Error != reason {
		t.Errorf("Error = %q, want %q", done.Error, reason)
	}
	// Progress before the failure is still worth keeping.
	if len(h.repo.texts(tk.ID)) != 1 {
		t.Errorf("stored %v, want the update sent before the failure", h.repo.texts(tk.ID))
	}
}

// A provider that cannot start at all must fail the task with something a
// user can hear, not a raw error.
func TestProviderThatWillNotStartFailsTheTask(t *testing.T) {
	h := newHarness(t, &refusingProvider{}, runner.Options{})

	tk := h.submit(t, "do something")
	done := h.await(t, tk.ID, task.StatusFailed, task.StatusCompleted)

	if done.Status != task.StatusFailed {
		t.Fatalf("Status = %q, want failed", done.Status)
	}
	if done.Error == "" {
		t.Error("no explanation recorded")
	}
	if strings.Contains(done.Error, "refusing") {
		t.Errorf("Error = %q, which exposes the internal error", done.Error)
	}
}

// Cancelling mid-run must stop the task and record it, which is what saying
// "stop" while FRIDAY is speaking will do.
func TestCancelStopsARunningTask(t *testing.T) {
	h := newHarness(t, &provider.Stub{
		Updates: []string{"one", "two", "three", "four", "five"},
		Delay:   30 * time.Millisecond,
	}, runner.Options{})

	tk := h.submit(t, "a long job")
	h.await(t, tk.ID, task.StatusRunning)

	if !h.runner.Cancel(tk.ID) {
		t.Fatal("Cancel reported no running task")
	}

	done := h.await(t, tk.ID, task.StatusCancelled, task.StatusCompleted, task.StatusFailed)
	if done.Status != task.StatusCancelled {
		t.Errorf("Status = %q, want cancelled", done.Status)
	}
	if done.Response != "" {
		t.Errorf("a cancelled task has a response: %q", done.Response)
	}
	if done.FinishedAt == nil {
		t.Error("finish time not recorded for a cancelled task")
	}
}

func TestCancelReportsWhenNothingIsRunning(t *testing.T) {
	h := newHarness(t, &provider.Stub{}, runner.Options{})

	if h.runner.Cancel(task.NewID()) {
		t.Error("Cancel reported stopping a task that was never running")
	}
}

// A task that outlives its deadline must be stopped and explained, rather
// than holding its slot until the process restarts.
func TestTaskExceedingItsDeadlineFails(t *testing.T) {
	h := newHarness(t, &provider.Stub{
		Updates: []string{"one", "two", "three", "four", "five", "six"},
		Delay:   50 * time.Millisecond,
	}, runner.Options{TaskTimeout: 40 * time.Millisecond})

	tk := h.submit(t, "a job that runs too long")
	done := h.await(t, tk.ID, task.StatusFailed, task.StatusCompleted, task.StatusCancelled)

	if done.Status != task.StatusFailed {
		t.Fatalf("Status = %q, want failed", done.Status)
	}
	if !strings.Contains(strings.ToLower(done.Error), "too long") {
		t.Errorf("Error = %q, want it to explain the task took too long", done.Error)
	}
}

// Shutdown must leave no task stuck running, and must say what happened
// rather than looking like the user cancelled it.
func TestShutdownStopsAndRecordsRunningTasks(t *testing.T) {
	repo := newMemRepo()
	r, err := runner.New(runner.Options{
		Repository: repo,
		Provider:   &provider.Stub{Updates: []string{"a", "b", "c", "d"}, Delay: 40 * time.Millisecond},
		Logger:     discard(),
	})
	if err != nil {
		t.Fatalf("runner.New: %v", err)
	}

	tk, err := task.New("", "interrupted by shutdown")
	if err != nil {
		t.Fatalf("task.New: %v", err)
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
		if got.Status == task.StatusRunning {
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
		t.Errorf("Error = %q, want it to say FRIDAY shut down", got.Error)
	}

	// Nothing may be submitted afterwards.
	if err := r.Submit(tk); err == nil {
		t.Error("Submit after Shutdown succeeded, want an error")
	}
}

func TestRecoverFailsTasksLeftRunning(t *testing.T) {
	h := newHarness(t, &provider.Stub{}, runner.Options{})
	ctx := context.Background()

	stranded, err := task.New("", "was running when the process died")
	if err != nil {
		t.Fatalf("task.New: %v", err)
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
	if got.Status != task.StatusFailed {
		t.Errorf("Status = %q, want failed", got.Status)
	}
	if !strings.Contains(strings.ToLower(got.Error), "restarted") {
		t.Errorf("Error = %q, want it to mention the restart", got.Error)
	}
}

// Only MaxConcurrent tasks run at once; the rest stay pending until a slot
// frees, rather than appearing to run while they wait.
func TestConcurrencyIsLimited(t *testing.T) {
	const limit = 2
	h := newHarness(t, &provider.Stub{Updates: []string{"a", "b"}, Delay: 60 * time.Millisecond},
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
			case task.StatusRunning:
				running++
			case task.StatusPending:
				pending++
			}
		}
		if running > limit {
			t.Fatalf("%d tasks running at once, want no more than %d", running, limit)
		}
		if pending > 0 && running == limit {
			sawPending = true
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !sawPending {
		t.Error("never observed a task waiting while the limit was in use")
	}
}

// A message that cannot be stored must not lose the answer with it.
func TestUnstorableMessageDoesNotFailTheTask(t *testing.T) {
	h := newHarness(t, &provider.Stub{Updates: []string{"progress"}}, runner.Options{})
	h.repo.appendErr = errors.New("messages table is full")

	tk := h.submit(t, "answer me anyway")
	done := h.await(t, tk.ID, task.StatusCompleted, task.StatusFailed)

	if done.Status != task.StatusCompleted {
		t.Errorf("Status = %q, want the task to complete despite the message failing", done.Status)
	}
}

func TestNewRequiresItsDependencies(t *testing.T) {
	cases := map[string]runner.Options{
		"no repository": {Provider: &provider.Stub{}, Logger: discard()},
		"no provider":   {Repository: newMemRepo(), Logger: discard()},
		"no logger":     {Repository: newMemRepo(), Provider: &provider.Stub{}},
	}
	for name, opts := range cases {
		if _, err := runner.New(opts); err == nil {
			t.Errorf("New with %s: want an error, got nil", name)
		}
	}
}

func TestDefaultsApplied(t *testing.T) {
	h := newHarness(t, &provider.Stub{}, runner.Options{})
	tk := h.submit(t, "use the defaults")
	if got := h.await(t, tk.ID, task.StatusCompleted, task.StatusFailed); got.Status != task.StatusCompleted {
		t.Errorf("Status = %q, want completed with default settings", got.Status)
	}
}

// refusingProvider : A provider that cannot start a run at all.
type refusingProvider struct{}

// Name : Returns the provider's name.
func (refusingProvider) Name() string { return "refusing" }

// Run : Always reports that it cannot start.
func (refusingProvider) Run(context.Context, provider.Request) (<-chan provider.Message, error) {
	return nil, errors.New("refusing: no credentials configured")
}

// A task waiting for a slot must be cancellable, not only one already
// running. Saying "stop" should work whichever it is.
func TestCancelStopsAQueuedTask(t *testing.T) {
	h := newHarness(t, &provider.Stub{
		Updates: []string{"a", "b", "c"},
		Delay:   80 * time.Millisecond,
	}, runner.Options{MaxConcurrent: 1})

	// The first task takes the only slot.
	blocker := h.submit(t, "holds the slot")
	h.await(t, blocker.ID, task.StatusRunning)

	queued := h.submit(t, "waits behind it")

	// It must still be pending, and cancelling it must work.
	got, err := h.repo.Get(context.Background(), queued.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != task.StatusPending {
		t.Fatalf("Status = %q, want pending while waiting for a slot", got.Status)
	}

	if !h.runner.Cancel(queued.ID) {
		t.Fatal("Cancel reported nothing to stop for a queued task")
	}

	done := h.await(t, queued.ID, task.StatusCancelled, task.StatusRunning, task.StatusCompleted)
	if done.Status != task.StatusCancelled {
		t.Errorf("Status = %q, want cancelled", done.Status)
	}
	if done.StartedAt != nil {
		t.Error("a task cancelled before running has a start time")
	}
}
