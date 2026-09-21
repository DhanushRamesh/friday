package mysql_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/DhanushRamesh/friday/internal/config"
	"github.com/DhanushRamesh/friday/internal/storage"
	"github.com/DhanushRamesh/friday/internal/task"
	taskmysql "github.com/DhanushRamesh/friday/internal/task/mysql"
)

// discard : A logger that writes nowhere.
func discard() *slog.Logger { return slog.New(slog.NewJSONHandler(io.Discard, nil)) }

// newRepository : Opens the development database, migrates it, and returns a
// repository. It skips the test when MySQL is not reachable, so the suite
// still runs on a bare checkout.
func newRepository(t *testing.T) *taskmysql.Repository {
	t.Helper()

	cfg, err := config.Load("", func(key string) (string, bool) {
		if key == "FRIDAY_DATABASE_PASSWORD" {
			return "friday_dev", true
		}
		return "", false
	})
	if err != nil {
		t.Fatalf("config: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	db, err := storage.Open(ctx, cfg.Database, discard(), storage.Options{})
	if err != nil {
		t.Skipf("MySQL not reachable at %s, skipping: %v", cfg.Database.SafeAddr(), err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := storage.Migrate(ctx, db, discard()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return taskmysql.NewRepository(db)
}

// storedConversation : Creates a conversation for a test to attach tasks to.
func storedConversation(t *testing.T, r *taskmysql.Repository) string {
	t.Helper()
	owner, err := task.NewUser("tester"+task.NewUserID()[4:14], "hash")
	if err != nil {
		t.Fatalf("task.NewUser: %v", err)
	}
	if err := r.CreateUser(context.Background(), owner); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	c := task.NewConversation(owner.ID, "")
	if err := r.CreateConversation(context.Background(), c); err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}
	return c.ID
}

// storedTask : Creates a task, stores it, and removes it when the test ends.
func storedTask(t *testing.T, r *taskmysql.Repository, prompt string) *task.Task {
	t.Helper()
	tk, err := task.New(storedConversation(t, r), prompt)
	if err != nil {
		t.Fatalf("task.New: %v", err)
	}
	if err := r.Create(context.Background(), tk); err != nil {
		t.Fatalf("Create: %v", err)
	}
	return tk
}

func TestCreateAndGetRoundTrip(t *testing.T) {
	r := newRepository(t)
	ctx := context.Background()

	tk := storedTask(t, r, "check my merge requests")

	got, err := r.Get(ctx, tk.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if got.ID != tk.ID || got.Prompt != tk.Prompt || got.Status != tk.Status {
		t.Errorf("round trip changed the task:\n stored %+v\n loaded %+v", tk, got)
	}
	if got.Response != "" || got.Error != "" {
		t.Errorf("a new task came back with a response or error: %+v", got)
	}
	if got.StartedAt != nil || got.FinishedAt != nil {
		t.Error("a pending task came back with start or finish times")
	}
}

// GORM sets CreatedAt and UpdatedAt automatically unless told not to. The
// domain owns those times, and a task's own history must match its row.
func TestTimestampsAreNotOverwrittenByGORM(t *testing.T) {
	r := newRepository(t)
	ctx := context.Background()

	tk := storedTask(t, r, "keep my timestamps")

	got, err := r.Get(ctx, tk.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if !got.CreatedAt.Equal(tk.CreatedAt) {
		t.Errorf("CreatedAt = %v, want the value the domain set, %v", got.CreatedAt, tk.CreatedAt)
	}
	if !got.UpdatedAt.Equal(tk.UpdatedAt) {
		t.Errorf("UpdatedAt = %v, want the value the domain set, %v", got.UpdatedAt, tk.UpdatedAt)
	}
	if got.CreatedAt.Location() != time.UTC {
		t.Errorf("CreatedAt location = %v, want UTC", got.CreatedAt.Location())
	}
}

// Milliseconds must survive. Without them a task's duration is unmeasurable.
func TestSubSecondPrecisionSurvives(t *testing.T) {
	r := newRepository(t)
	ctx := context.Background()

	tk := storedTask(t, r, "measure me")

	got, err := r.Get(ctx, tk.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if diff := got.CreatedAt.Sub(tk.CreatedAt); diff != 0 {
		t.Errorf("CreatedAt drifted by %v; sub-second precision was lost", diff)
	}
	if tk.CreatedAt.Nanosecond() != 0 && got.CreatedAt.Nanosecond() == 0 {
		t.Error("nanoseconds truncated to zero on the way through the database")
	}
}

func TestUpdatePersistsTheWholeLifecycle(t *testing.T) {
	r := newRepository(t)
	ctx := context.Background()

	tk := storedTask(t, r, "run to completion")

	if err := tk.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := r.Update(ctx, tk); err != nil {
		t.Fatalf("Update after Start: %v", err)
	}

	running, err := r.Get(ctx, tk.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if running.Status != task.StatusRunning {
		t.Errorf("Status = %q, want running", running.Status)
	}
	if running.StartedAt == nil {
		t.Fatal("StartedAt not stored")
	}

	const answer = "You have four open merge requests."
	if err := tk.Complete(answer); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if err := r.Update(ctx, tk); err != nil {
		t.Fatalf("Update after Complete: %v", err)
	}

	done, err := r.Get(ctx, tk.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if done.Status != task.StatusCompleted {
		t.Errorf("Status = %q, want completed", done.Status)
	}
	if done.Response != answer {
		t.Errorf("Response = %q, want %q", done.Response, answer)
	}
	if done.FinishedAt == nil {
		t.Error("FinishedAt not stored")
	}
}

func TestGetAndUpdateReportMissingTasks(t *testing.T) {
	r := newRepository(t)
	ctx := context.Background()

	missing := task.NewID()

	if _, err := r.Get(ctx, missing); !errors.Is(err, task.ErrNotFound) {
		t.Errorf("Get on a missing task: error = %v, want ErrNotFound", err)
	}

	ghost, err := task.New("", "never stored")
	if err != nil {
		t.Fatalf("task.New: %v", err)
	}
	if err := r.Update(ctx, ghost); !errors.Is(err, task.ErrNotFound) {
		t.Errorf("Update on a missing task: error = %v, want ErrNotFound", err)
	}
}

// Listing must not carry response bodies, which is the whole reason the
// summary path exists.
func TestListOmitsResponses(t *testing.T) {
	r := newRepository(t)
	ctx := context.Background()

	tk := storedTask(t, r, "list me")
	if err := tk.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := tk.Complete(strings.Repeat("x", 4096)); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if err := r.Update(ctx, tk); err != nil {
		t.Fatalf("Update: %v", err)
	}

	got, err := r.List(ctx, task.Filter{Limit: 10})
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	var found bool
	for _, s := range got {
		if s.ID == tk.ID {
			found = true
			if s.Status != task.StatusCompleted {
				t.Errorf("Status = %q, want completed", s.Status)
			}
		}
	}
	if !found {
		t.Fatalf("task %s missing from the listing", tk.ID)
	}
	// Summary has no Response field at all, so this is a compile-time
	// guarantee as much as a runtime one. Confirm the full read still has it.
	full, err := r.Get(ctx, tk.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(full.Response) != 4096 {
		t.Errorf("full read returned %d bytes of response, want 4096", len(full.Response))
	}
}

func TestListOrdersNewestFirstAndHonoursLimits(t *testing.T) {
	r := newRepository(t)
	ctx := context.Background()

	var created []string
	for i := 0; i < 3; i++ {
		created = append(created, storedTask(t, r, "ordering probe").ID)
	}

	got, err := r.List(ctx, task.Filter{Limit: 3})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d tasks, want 3", len(got))
	}
	// The three just created are the newest, most recent first.
	for i, want := range []string{created[2], created[1], created[0]} {
		if got[i].ID != want {
			t.Errorf("position %d = %s, want %s", i, got[i].ID, want)
		}
	}

	// A limit beyond the maximum is capped rather than obeyed.
	capped, err := r.List(ctx, task.Filter{Limit: task.MaxListLimit + 500})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(capped) > task.MaxListLimit {
		t.Errorf("got %d tasks, want no more than %d", len(capped), task.MaxListLimit)
	}
}

func TestListFiltersByStatus(t *testing.T) {
	r := newRepository(t)
	ctx := context.Background()

	pending := storedTask(t, r, "stays pending")

	cancelled := storedTask(t, r, "gets cancelled")
	if err := cancelled.Cancel(); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if err := r.Update(ctx, cancelled); err != nil {
		t.Fatalf("Update: %v", err)
	}

	got, err := r.List(ctx, task.Filter{Status: task.StatusCancelled, Limit: 100})
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	var sawCancelled, sawPending bool
	for _, s := range got {
		if s.Status != task.StatusCancelled {
			t.Errorf("listing by cancelled returned a %s task", s.Status)
		}
		switch s.ID {
		case cancelled.ID:
			sawCancelled = true
		case pending.ID:
			sawPending = true
		}
	}
	if !sawCancelled {
		t.Error("the cancelled task is missing from its own listing")
	}
	if sawPending {
		t.Error("a pending task appeared in a listing filtered to cancelled")
	}
}

func TestAppendMessageNumbersInOrder(t *testing.T) {
	r := newRepository(t)
	ctx := context.Background()

	tk := storedTask(t, r, "talk to me")

	texts := []string{"Let me take a look.", "Still working on it.", "Nearly there."}
	for i, text := range texts {
		msg, err := r.AppendMessage(ctx, tk.ID, "update", text)
		if err != nil {
			t.Fatalf("AppendMessage %d: %v", i, err)
		}
		if msg.Seq != i+1 {
			t.Errorf("message %d has seq %d, want %d", i, msg.Seq, i+1)
		}
		if msg.Text != text {
			t.Errorf("message %d text = %q, want %q", i, msg.Text, text)
		}
		if msg.CreatedAt.Location() != time.UTC {
			t.Errorf("message %d timestamp location = %v, want UTC", i, msg.CreatedAt.Location())
		}
	}

	got, err := r.Messages(ctx, tk.ID)
	if err != nil {
		t.Fatalf("Messages: %v", err)
	}
	if len(got) != len(texts) {
		t.Fatalf("got %d messages, want %d", len(got), len(texts))
	}
	for i, want := range texts {
		if got[i].Text != want || got[i].Seq != i+1 {
			t.Errorf("message %d = seq %d %q, want seq %d %q", i, got[i].Seq, got[i].Text, i+1, want)
		}
	}
}

// Messages belong to a task; one cannot be recorded against a task that does
// not exist.
func TestAppendMessageRejectsMissingTask(t *testing.T) {
	r := newRepository(t)

	_, err := r.AppendMessage(context.Background(), task.NewID(), "update", "orphan")
	if !errors.Is(err, task.ErrNotFound) {
		t.Errorf("error = %v, want ErrNotFound", err)
	}
}

func TestMessagesForATaskWithNoneIsEmpty(t *testing.T) {
	r := newRepository(t)

	tk := storedTask(t, r, "silent task")

	got, err := r.Messages(context.Background(), tk.ID)
	if err != nil {
		t.Fatalf("Messages: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d messages, want none", len(got))
	}
}

// A process that stops mid-task leaves rows reading running that nothing will
// ever move.
func TestFailRunningRecoversInterruptedTasks(t *testing.T) {
	r := newRepository(t)
	ctx := context.Background()

	interrupted := storedTask(t, r, "was running when the server died")
	if err := interrupted.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := r.Update(ctx, interrupted); err != nil {
		t.Fatalf("Update: %v", err)
	}

	untouched := storedTask(t, r, "still queued")

	const reason = "FRIDAY restarted while this was running."
	changed, err := r.FailRunning(ctx, reason)
	if err != nil {
		t.Fatalf("FailRunning: %v", err)
	}
	if changed < 1 {
		t.Errorf("FailRunning changed %d tasks, want at least 1", changed)
	}

	recovered, err := r.Get(ctx, interrupted.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if recovered.Status != task.StatusFailed {
		t.Errorf("Status = %q, want failed", recovered.Status)
	}
	if recovered.Error != reason {
		t.Errorf("Error = %q, want %q", recovered.Error, reason)
	}
	if recovered.FinishedAt == nil {
		t.Error("FinishedAt not stamped on a recovered task")
	}

	// A queued task was not running, so it must be left alone.
	stillPending, err := r.Get(ctx, untouched.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if stillPending.Status != task.StatusPending {
		t.Errorf("a pending task became %q; only running tasks should be failed", stillPending.Status)
	}
}

// Unicode must survive the round trip, since prompts arrive from speech in
// whatever language was spoken.
func TestUnicodeSurvivesTheRoundTrip(t *testing.T) {
	r := newRepository(t)
	ctx := context.Background()

	const prompt = "எனது merge requests சரிபார்க்கவும் 🎧"
	tk := storedTask(t, r, prompt)

	got, err := r.Get(ctx, tk.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Prompt != prompt {
		t.Errorf("Prompt = %q, want %q", got.Prompt, prompt)
	}
}
