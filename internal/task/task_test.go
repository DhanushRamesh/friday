package task_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/DhanushRamesh/friday/internal/task"
)

// mustNew : Creates a task, failing the test if the prompt is rejected.
func mustNew(t *testing.T, prompt string) *task.Task {
	t.Helper()
	tk, err := task.New(prompt)
	if err != nil {
		t.Fatalf("New(%q): %v", prompt, err)
	}
	return tk
}

func TestNewTaskStartsPending(t *testing.T) {
	tk := mustNew(t, "  check my open merge requests  ")

	if tk.Status != task.StatusPending {
		t.Errorf("Status = %q, want pending", tk.Status)
	}
	if tk.Prompt != "check my open merge requests" {
		t.Errorf("Prompt = %q, want it trimmed", tk.Prompt)
	}
	if !task.ValidID(tk.ID) {
		t.Errorf("ID = %q, want a valid task identifier", tk.ID)
	}
	if tk.CreatedAt.IsZero() || tk.UpdatedAt.IsZero() {
		t.Error("timestamps not set")
	}
	if tk.CreatedAt.Location() != time.UTC {
		t.Errorf("CreatedAt location = %v, want UTC", tk.CreatedAt.Location())
	}
	if tk.StartedAt != nil {
		t.Error("StartedAt set on a task that has not run")
	}
	if tk.FinishedAt != nil {
		t.Error("FinishedAt set on a task that has not finished")
	}
}

func TestNewRejectsBadPrompts(t *testing.T) {
	for _, prompt := range []string{"", "   ", "\n\t "} {
		if _, err := task.New(prompt); !errors.Is(err, task.ErrEmptyPrompt) {
			t.Errorf("New(%q) error = %v, want ErrEmptyPrompt", prompt, err)
		}
	}

	if _, err := task.New(strings.Repeat("a", task.MaxPromptRunes+1)); !errors.Is(err, task.ErrPromptTooLong) {
		t.Errorf("oversized prompt error = %v, want ErrPromptTooLong", err)
	}
	// The limit counts runes, so a multi-byte prompt at the limit is accepted.
	if _, err := task.New(strings.Repeat("こ", task.MaxPromptRunes)); err != nil {
		t.Errorf("prompt of exactly MaxPromptRunes runes rejected: %v", err)
	}
}

func TestIDsAreUniqueAndOrderByCreation(t *testing.T) {
	const n = 1000
	seen := make(map[string]bool, n)
	ids := make([]string, n)

	for i := range ids {
		ids[i] = task.NewID()
		if seen[ids[i]] {
			t.Fatalf("duplicate id generated: %s", ids[i])
		}
		seen[ids[i]] = true
	}

	// ULIDs are monotonic, so lexical order matches creation order. Listing
	// tasks relies on this to come back ordered without an explicit sort.
	for i := 1; i < len(ids); i++ {
		if ids[i-1] >= ids[i] {
			t.Fatalf("ids not increasing: %s came before %s", ids[i-1], ids[i])
		}
	}
}

func TestValidID(t *testing.T) {
	valid := task.NewID()
	cases := map[string]bool{
		valid:                              true,
		"":                                 false,
		"task_":                            false,
		strings.TrimPrefix(valid, "task_"): false,
		"task_not-a-ulid-at-all-xxxx":      false,
		valid + "x":                        false,
		"tusk_01J000000000000000000000":    false,
	}
	for id, want := range cases {
		if got := task.ValidID(id); got != want {
			t.Errorf("ValidID(%q) = %v, want %v", id, got, want)
		}
	}
}

func TestLifecycleToCompleted(t *testing.T) {
	tk := mustNew(t, "check my merge requests")

	if err := tk.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if tk.Status != task.StatusRunning {
		t.Fatalf("Status = %q, want running", tk.Status)
	}
	if tk.StartedAt == nil {
		t.Fatal("StartedAt not stamped by Start")
	}
	if tk.FinishedAt != nil {
		t.Error("FinishedAt stamped on a task that is only running")
	}

	const answer = "You have 4 open merge requests."
	if err := tk.Complete(answer); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if tk.Status != task.StatusCompleted {
		t.Errorf("Status = %q, want completed", tk.Status)
	}
	if tk.Response != answer {
		t.Errorf("Response = %q, want the final answer", tk.Response)
	}
	if tk.FinishedAt == nil {
		t.Error("FinishedAt not stamped on a finished task")
	}
	if tk.Duration() <= 0 {
		t.Errorf("Duration = %v, want a positive span", tk.Duration())
	}
}

func TestTerminalStatesAreFinal(t *testing.T) {
	endings := map[string]func(*task.Task) error{
		"completed": func(tk *task.Task) error { return tk.Complete("done") },
		"failed":    func(tk *task.Task) error { return tk.Fail("GitLab did not respond in time.") },
		"cancelled": func(tk *task.Task) error { return tk.Cancel() },
	}

	for name, end := range endings {
		t.Run(name, func(t *testing.T) {
			tk := mustNew(t, "do something")
			if err := tk.Start(); err != nil {
				t.Fatalf("Start: %v", err)
			}
			if err := end(tk); err != nil {
				t.Fatalf("finish: %v", err)
			}

			for op, fn := range map[string]func() error{
				"Start":    tk.Start,
				"Cancel":   tk.Cancel,
				"Complete": func() error { return tk.Complete("again") },
				"Fail":     func() error { return tk.Fail("again") },
			} {
				var te *task.TransitionError
				if err := fn(); !errors.As(err, &te) {
					t.Errorf("%s on a %s task: error = %v, want TransitionError", op, name, err)
				}
			}
		})
	}
}

// A queued task can be cancelled or failed before it ever runs. Failing a
// pending task is how a request rejected at dequeue is recorded.
func TestPendingCanEndWithoutRunning(t *testing.T) {
	for name, end := range map[string]func(*task.Task) error{
		"cancel": func(tk *task.Task) error { return tk.Cancel() },
		"fail":   func(tk *task.Task) error { return tk.Fail("no capacity") },
	} {
		t.Run(name, func(t *testing.T) {
			tk := mustNew(t, "never mind")
			if err := end(tk); err != nil {
				t.Fatalf("%s on a pending task: %v", name, err)
			}
			if !tk.Status.IsTerminal() {
				t.Errorf("Status = %q, want a terminal status", tk.Status)
			}
			// It never ran, so there is no duration to report.
			if tk.StartedAt != nil {
				t.Error("StartedAt stamped on a task that never ran")
			}
			if tk.Duration() != 0 {
				t.Errorf("Duration = %v, want 0", tk.Duration())
			}
		})
	}
}

// A task cannot skip straight from pending to completed; that would mean a
// result appearing for work that never ran.
func TestCannotCompleteWithoutRunning(t *testing.T) {
	tk := mustNew(t, "do something")

	var te *task.TransitionError
	if err := tk.Complete("result"); !errors.As(err, &te) {
		t.Fatalf("Complete on a pending task: error = %v, want TransitionError", err)
	}
}

// A rejected transition must leave the task exactly as it was.
func TestRejectedTransitionDoesNotMutate(t *testing.T) {
	tk := mustNew(t, "do something")
	before := *tk

	if err := tk.Complete("skipping straight to done"); err == nil {
		t.Fatal("Complete on a pending task: want error, got nil")
	}

	if tk.Status != before.Status || tk.Response != before.Response || !tk.UpdatedAt.Equal(before.UpdatedAt) {
		t.Errorf("task mutated by a rejected transition:\n before %+v\n after  %+v", before, *tk)
	}
}

// A response beyond what the column can hold must be refused before the task
// is marked complete, so the caller can fail it rather than have the write
// rejected by the database.
func TestOversizedResponseRefusedAndTaskUnchanged(t *testing.T) {
	tk := mustNew(t, "summarise everything")
	if err := tk.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	err := tk.Complete(strings.Repeat("a", task.MaxResponseBytes+1))
	if !errors.Is(err, task.ErrResponseTooLarge) {
		t.Fatalf("error = %v, want ErrResponseTooLarge", err)
	}
	if tk.Status != task.StatusRunning {
		t.Errorf("Status = %q, want the task left running", tk.Status)
	}
	if tk.Response != "" {
		t.Error("Response set despite the transition being refused")
	}

	// The task can still be failed with an explanation.
	if err := tk.Fail("The answer was too large to store."); err != nil {
		t.Fatalf("Fail after an oversized response: %v", err)
	}
}

func TestStatusPredicates(t *testing.T) {
	for _, s := range []task.Status{task.StatusCompleted, task.StatusFailed, task.StatusCancelled} {
		if !s.IsTerminal() || !s.Valid() {
			t.Errorf("%s: terminal=%v valid=%v, want both true", s, s.IsTerminal(), s.Valid())
		}
	}
	for _, s := range []task.Status{task.StatusPending, task.StatusRunning} {
		if s.IsTerminal() || !s.Valid() {
			t.Errorf("%s: terminal=%v valid=%v, want false and true", s, s.IsTerminal(), s.Valid())
		}
	}
	if task.Status("banana").Valid() {
		t.Error(`Status("banana").Valid() = true, want false`)
	}
	if task.Status("banana").CanTransitionTo(task.StatusRunning) {
		t.Error("an unknown status should permit no transition")
	}
}

func TestTransitionErrorExplainsFinality(t *testing.T) {
	tk := mustNew(t, "x")
	if err := tk.Cancel(); err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	err := tk.Start()
	if err == nil {
		t.Fatal("Start on a cancelled task: want error")
	}
	if !strings.Contains(err.Error(), "final state") {
		t.Errorf("error %q should say the state is final", err)
	}
}
