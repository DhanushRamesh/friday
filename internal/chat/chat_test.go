package chat_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/chat"
)

// mustNew : Creates a chat, failing the test if the prompt is rejected.
func mustNew(t *testing.T, prompt string) *chat.Chat {
	t.Helper()
	tk, err := chat.New("", prompt)
	if err != nil {
		t.Fatalf("New(%q): %v", prompt, err)
	}
	return tk
}

func TestNewChatStartsPending(t *testing.T) {
	tk := mustNew(t, "  check my open merge requests  ")

	if tk.Status != chat.StatusPending {
		t.Errorf("Status = %q, want pending", tk.Status)
	}
	if tk.Prompt != "check my open merge requests" {
		t.Errorf("Prompt = %q, want it trimmed", tk.Prompt)
	}
	if !chat.ValidID(tk.ID) {
		t.Errorf("ID = %q, want a valid chat identifier", tk.ID)
	}
	if tk.CreatedAt.IsZero() || tk.UpdatedAt.IsZero() {
		t.Error("timestamps not set")
	}
	if tk.CreatedAt.Location() != time.UTC {
		t.Errorf("CreatedAt location = %v, want UTC", tk.CreatedAt.Location())
	}
	if tk.StartedAt != nil {
		t.Error("StartedAt set on a chat that has not run")
	}
	if tk.FinishedAt != nil {
		t.Error("FinishedAt set on a chat that has not finished")
	}
}

func TestNewRejectsBadPrompts(t *testing.T) {
	for _, prompt := range []string{"", "   ", "\n\t "} {
		if _, err := chat.New("", prompt); !errors.Is(err, chat.ErrEmptyPrompt) {
			t.Errorf("New(%q) error = %v, want ErrEmptyPrompt", prompt, err)
		}
	}

	if _, err := chat.New("", strings.Repeat("a", chat.MaxPromptRunes+1)); !errors.Is(err, chat.ErrPromptTooLong) {
		t.Errorf("oversized prompt error = %v, want ErrPromptTooLong", err)
	}
	// The limit counts runes, so a multi-byte prompt at the limit is accepted.
	if _, err := chat.New("", strings.Repeat("こ", chat.MaxPromptRunes)); err != nil {
		t.Errorf("prompt of exactly MaxPromptRunes runes rejected: %v", err)
	}
}

func TestIDsAreUniqueAndOrderByCreation(t *testing.T) {
	const n = 1000
	seen := make(map[string]bool, n)
	ids := make([]string, n)

	for i := range ids {
		ids[i] = chat.NewID()
		if seen[ids[i]] {
			t.Fatalf("duplicate id generated: %s", ids[i])
		}
		seen[ids[i]] = true
	}

	// ULIDs are monotonic, so lexical order matches creation order. Listing
	// chats relies on this to come back ordered without an explicit sort.
	for i := 1; i < len(ids); i++ {
		if ids[i-1] >= ids[i] {
			t.Fatalf("ids not increasing: %s came before %s", ids[i-1], ids[i])
		}
	}
}

func TestValidID(t *testing.T) {
	valid := chat.NewID()
	cases := map[string]bool{
		valid:                              true,
		"":                                 false,
		"chat_":                            false,
		strings.TrimPrefix(valid, "chat_"): false,
		"chat_not-a-ulid-at-all-xxxx":      false,
		valid + "x":                        false,
		"tusk_01J000000000000000000000":    false,
	}
	for id, want := range cases {
		if got := chat.ValidID(id); got != want {
			t.Errorf("ValidID(%q) = %v, want %v", id, got, want)
		}
	}
}

func TestLifecycleToCompleted(t *testing.T) {
	tk := mustNew(t, "check my merge requests")

	if err := tk.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if tk.Status != chat.StatusRunning {
		t.Fatalf("Status = %q, want running", tk.Status)
	}
	if tk.StartedAt == nil {
		t.Fatal("StartedAt not stamped by Start")
	}
	if tk.FinishedAt != nil {
		t.Error("FinishedAt stamped on a chat that is only running")
	}

	const answer = "You have 4 open merge requests."
	if err := tk.Complete(answer); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if tk.Status != chat.StatusCompleted {
		t.Errorf("Status = %q, want completed", tk.Status)
	}
	if tk.Response != answer {
		t.Errorf("Response = %q, want the final answer", tk.Response)
	}
	if tk.FinishedAt == nil {
		t.Error("FinishedAt not stamped on a finished chat")
	}
	if tk.Duration() < 0 {
		t.Errorf("Duration = %v, want a non-negative span", tk.Duration())
	}
}

// Timestamps are truncated to StoredPrecision, so a chat finishing within the
// same millisecond it started reports no duration at all. A chat that takes
// real time must report it.
func TestDurationMeasuresElapsedTime(t *testing.T) {
	tk := mustNew(t, "something slow")

	if err := tk.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	time.Sleep(5 * time.Millisecond)
	if err := tk.Complete("done"); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	if got := tk.Duration(); got < 3*time.Millisecond {
		t.Errorf("Duration = %v, want at least 3ms after sleeping 5ms", got)
	}
}

// Every timestamp the domain sets must already be at the precision it will be
// stored at, so a chat in memory matches the row written from it.
func TestTimestampsAreAtStoredPrecision(t *testing.T) {
	tk := mustNew(t, "check my precision")
	if err := tk.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := tk.Complete("done"); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	stamps := map[string]time.Time{
		"CreatedAt":  tk.CreatedAt,
		"UpdatedAt":  tk.UpdatedAt,
		"StartedAt":  *tk.StartedAt,
		"FinishedAt": *tk.FinishedAt,
	}
	for name, ts := range stamps {
		if ts.Truncate(chat.StoredPrecision) != ts {
			t.Errorf("%s = %v, which carries more precision than can be stored", name, ts)
		}
	}
}

func TestTerminalStatesAreFinal(t *testing.T) {
	endings := map[string]func(*chat.Chat) error{
		"completed": func(tk *chat.Chat) error { return tk.Complete("done") },
		"failed":    func(tk *chat.Chat) error { return tk.Fail("GitLab did not respond in time.") },
		"cancelled": func(tk *chat.Chat) error { return tk.Cancel() },
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
				var te *chat.TransitionError
				if err := fn(); !errors.As(err, &te) {
					t.Errorf("%s on a %s chat: error = %v, want TransitionError", op, name, err)
				}
			}
		})
	}
}

// A queued chat can be cancelled or failed before it ever runs. Failing a
// pending chat is how a request rejected at dequeue is recorded.
func TestPendingCanEndWithoutRunning(t *testing.T) {
	for name, end := range map[string]func(*chat.Chat) error{
		"cancel": func(tk *chat.Chat) error { return tk.Cancel() },
		"fail":   func(tk *chat.Chat) error { return tk.Fail("no capacity") },
	} {
		t.Run(name, func(t *testing.T) {
			tk := mustNew(t, "never mind")
			if err := end(tk); err != nil {
				t.Fatalf("%s on a pending chat: %v", name, err)
			}
			if !tk.Status.IsTerminal() {
				t.Errorf("Status = %q, want a terminal status", tk.Status)
			}
			// It never ran, so there is no duration to report.
			if tk.StartedAt != nil {
				t.Error("StartedAt stamped on a chat that never ran")
			}
			if tk.Duration() != 0 {
				t.Errorf("Duration = %v, want 0", tk.Duration())
			}
		})
	}
}

// A chat cannot skip straight from pending to completed; that would mean a
// result appearing for work that never ran.
func TestCannotCompleteWithoutRunning(t *testing.T) {
	tk := mustNew(t, "do something")

	var te *chat.TransitionError
	if err := tk.Complete("result"); !errors.As(err, &te) {
		t.Fatalf("Complete on a pending chat: error = %v, want TransitionError", err)
	}
}

// A rejected transition must leave the chat exactly as it was.
func TestRejectedTransitionDoesNotMutate(t *testing.T) {
	tk := mustNew(t, "do something")
	before := *tk

	if err := tk.Complete("skipping straight to done"); err == nil {
		t.Fatal("Complete on a pending chat: want error, got nil")
	}

	if tk.Status != before.Status || tk.Response != before.Response || !tk.UpdatedAt.Equal(before.UpdatedAt) {
		t.Errorf("chat mutated by a rejected transition:\n before %+v\n after  %+v", before, *tk)
	}
}

// A response beyond what the column can hold must be refused before the chat
// is marked complete, so the caller can fail it rather than have the write
// rejected by the database.
func TestOversizedResponseRefusedAndChatUnchanged(t *testing.T) {
	tk := mustNew(t, "summarise everything")
	if err := tk.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	err := tk.Complete(strings.Repeat("a", chat.MaxResponseBytes+1))
	if !errors.Is(err, chat.ErrResponseTooLarge) {
		t.Fatalf("error = %v, want ErrResponseTooLarge", err)
	}
	if tk.Status != chat.StatusRunning {
		t.Errorf("Status = %q, want the chat left running", tk.Status)
	}
	if tk.Response != "" {
		t.Error("Response set despite the transition being refused")
	}

	// The chat can still be failed with an explanation.
	if err := tk.Fail("The answer was too large to store."); err != nil {
		t.Fatalf("Fail after an oversized response: %v", err)
	}
}

func TestStatusPredicates(t *testing.T) {
	for _, s := range []chat.Status{chat.StatusCompleted, chat.StatusFailed, chat.StatusCancelled} {
		if !s.IsTerminal() || !s.Valid() {
			t.Errorf("%s: terminal=%v valid=%v, want both true", s, s.IsTerminal(), s.Valid())
		}
	}
	for _, s := range []chat.Status{chat.StatusPending, chat.StatusRunning} {
		if s.IsTerminal() || !s.Valid() {
			t.Errorf("%s: terminal=%v valid=%v, want false and true", s, s.IsTerminal(), s.Valid())
		}
	}
	if chat.Status("banana").Valid() {
		t.Error(`Status("banana").Valid() = true, want false`)
	}
	if chat.Status("banana").CanTransitionTo(chat.StatusRunning) {
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
		t.Fatal("Start on a cancelled chat: want error")
	}
	if !strings.Contains(err.Error(), "final state") {
		t.Errorf("error %q should say the state is final", err)
	}
}

func TestRenameTrimsAndAllowsClearing(t *testing.T) {
	s := chat.NewSession("usr_1", "first")

	if err := s.Rename("  the grocery list  "); err != nil {
		t.Fatalf("rename: %v", err)
	}
	if s.Title != "the grocery list" {
		t.Errorf("title = %q, want it trimmed", s.Title)
	}

	// Clearing is allowed: a name given by mistake should be removable
	// without deleting the conversation under it.
	if err := s.Rename("   "); err != nil {
		t.Fatalf("clearing: %v", err)
	}
	if s.Title != "" {
		t.Errorf("title = %q, want it cleared", s.Title)
	}
}

func TestRenameCountsCharactersNotBytes(t *testing.T) {
	s := chat.NewSession("usr_1", "")

	// The column is VARCHAR(200), which MySQL counts in characters. Counting
	// bytes here would give a name in Tamil a third of the length of one in
	// English for no reason the person could see.
	tamil := strings.Repeat("அ", chat.MaxTitleLen)
	if err := s.Rename(tamil); err != nil {
		t.Errorf("a title of exactly the limit was refused: %v", err)
	}

	if err := s.Rename(strings.Repeat("a", chat.MaxTitleLen+1)); !errors.Is(err, chat.ErrTitleTooLong) {
		t.Errorf("err = %v, want ErrTitleTooLong", err)
	}
}
