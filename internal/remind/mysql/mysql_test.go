package mysql_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/chat"
	chatmysql "github.com/DhanushRamesh/personal-assistant/internal/chat/mysql"
	"github.com/DhanushRamesh/personal-assistant/internal/config"
	"github.com/DhanushRamesh/personal-assistant/internal/remind"
	remindmysql "github.com/DhanushRamesh/personal-assistant/internal/remind/mysql"
	"github.com/DhanushRamesh/personal-assistant/internal/storage"
)

// discard : A logger that writes nowhere.
func discard() *slog.Logger { return slog.New(slog.NewJSONHandler(io.Discard, nil)) }

// testEnv : The settings the database tests run against. A dedicated
// database, never the one the server uses.
func testEnv(key string) (string, bool) {
	switch key {
	case "ASSISTANT_DATABASE_NAME":
		if name := os.Getenv("ASSISTANT_TEST_DATABASE"); name != "" {
			return name, true
		}
		return "assistant_test", true
	case "ASSISTANT_DATABASE_PASSWORD":
		if pw := os.Getenv("ASSISTANT_DATABASE_PASSWORD"); pw != "" {
			return pw, true
		}
		return "friday_dev", true
	}
	return "", false
}

// newStore : Opens the test database, migrates it, and returns a store with
// a user to own the reminders.
func newStore(t *testing.T) (*remindmysql.Store, string) {
	t.Helper()

	cfg, err := config.Load("", testEnv)
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	if !strings.HasSuffix(cfg.Database.Name, "_test") {
		t.Skipf("refusing to run against %q", cfg.Database.Name)
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

	id := chat.NewUserID()
	owner, err := chat.NewUser("tester"+id[len(id)-12:], "hash")
	if err != nil {
		t.Fatalf("chat.NewUser: %v", err)
	}
	if err := chatmysql.NewRepository(db).CreateUser(context.Background(), owner); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	return remindmysql.New(db), owner.ID
}

// stored : Creates a reminder due at the given time.
func stored(t *testing.T, s *remindmysql.Store, user, title string, due time.Time, repeats remind.Repeat) *remind.Reminder {
	t.Helper()
	r, err := remind.New(user, "", remind.ScopeUser, title, "time to "+title, due, repeats)
	if err != nil {
		t.Fatalf("remind.New: %v", err)
	}
	if err := s.Create(context.Background(), r); err != nil {
		t.Fatalf("Create: %v", err)
	}
	return r
}

// A reminder survives the round trip.
func TestAReminderComesBackAsItWentIn(t *testing.T) {
	s, user := newStore(t)
	due := time.Now().UTC().Add(time.Hour).Truncate(time.Millisecond)
	want := stored(t, s, user, "Wake", due, remind.Daily)

	got, err := s.Get(context.Background(), user, want.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Title != want.Title || got.Body != want.Body {
		t.Errorf("got %+v, want %+v", got, want)
	}
	if !got.DueAt.Equal(due) {
		t.Errorf("due = %v, want %v", got.DueAt, due)
	}
	if got.Repeats != remind.Daily || got.Status != remind.Pending {
		t.Errorf("repeats = %q, status = %q", got.Repeats, got.Status)
	}
}

// One person's reminder is not readable by another.
func TestAReminderIsNotReadableByAnotherPerson(t *testing.T) {
	s, user := newStore(t)
	r := stored(t, s, user, "Wake", time.Now().Add(time.Hour), remind.Once)

	if _, err := s.Get(context.Background(), "usr_someone_else", r.ID); !errors.Is(err, remind.ErrNotFound) {
		t.Errorf("error = %v, want ErrNotFound", err)
	}
}

// Only what is due comes back, and only while it is pending.
func TestDueFindsWhatIsReadyAndNothingElse(t *testing.T) {
	s, user := newStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	ready := stored(t, s, user, "Ready", now.Add(-time.Minute), remind.Once)
	later := stored(t, s, user, "Later", now.Add(time.Hour), remind.Once)
	cancelled := stored(t, s, user, "Cancelled", now.Add(-time.Minute), remind.Once)
	if err := s.Cancel(ctx, user, cancelled.ID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	// Due is deliberately not scoped to a person: the firing loop wants
	// everything that is due. So it also returns rows earlier runs left in
	// this shared database, and only this test's own rows can be asserted
	// on.
	got, err := s.Due(ctx, now, 100000)
	if err != nil {
		t.Fatalf("Due: %v", err)
	}

	mine := map[string]bool{ready.ID: true, later.ID: true, cancelled.ID: true}

	var sawReady, sawOther bool
	for _, r := range got {
		if !mine[r.ID] {
			continue
		}
		if r.ID == ready.ID {
			sawReady = true
		} else {
			sawOther = true
		}
	}
	if !sawReady {
		t.Error("something due was not returned")
	}
	if sawOther {
		t.Error("something not due, or cancelled, was returned")
	}
}

// A one-shot that fired is finished.
func TestAOneShotThatFiredIsDone(t *testing.T) {
	s, user := newStore(t)
	ctx := context.Background()
	r := stored(t, s, user, "Once", time.Now().Add(-time.Minute), remind.Once)

	if err := s.Fired(ctx, r.ID, time.Now().UTC(), time.Time{}); err != nil {
		t.Fatalf("Fired: %v", err)
	}

	got, err := s.Get(ctx, user, r.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != remind.Done {
		t.Errorf("status = %q, want done", got.Status)
	}
	if got.Fires != 1 || got.LastFiredAt == nil {
		t.Errorf("fires = %d, last = %v", got.Fires, got.LastFiredAt)
	}
}

// A repeating one comes back, still pending, due at its next time.
func TestARepeatingOneComesBack(t *testing.T) {
	s, user := newStore(t)
	ctx := context.Background()
	r := stored(t, s, user, "Daily", time.Now().Add(-time.Minute), remind.Daily)

	next := time.Now().UTC().Add(24 * time.Hour).Truncate(time.Millisecond)
	if err := s.Fired(ctx, r.ID, time.Now().UTC(), next); err != nil {
		t.Fatalf("Fired: %v", err)
	}

	got, err := s.Get(ctx, user, r.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != remind.Pending {
		t.Errorf("status = %q, want it still pending", got.Status)
	}
	if !got.DueAt.Equal(next) {
		t.Errorf("due = %v, want %v", got.DueAt, next)
	}
	if got.Fires != 1 {
		t.Errorf("fires = %d, want 1", got.Fires)
	}
}

// Firing one twice is refused, so two passes of the loop cannot both say
// the same thing.
func TestFiringTwiceIsRefused(t *testing.T) {
	s, user := newStore(t)
	ctx := context.Background()
	r := stored(t, s, user, "Once", time.Now().Add(-time.Minute), remind.Once)

	if err := s.Fired(ctx, r.ID, time.Now().UTC(), time.Time{}); err != nil {
		t.Fatalf("Fired: %v", err)
	}
	if err := s.Fired(ctx, r.ID, time.Now().UTC(), time.Time{}); !errors.Is(err, remind.ErrNotFound) {
		t.Errorf("error = %v, want the second firing refused", err)
	}

	got, _ := s.Get(ctx, user, r.ID)
	if got.Fires != 1 {
		t.Errorf("fires = %d, want it counted once", got.Fires)
	}
}

// A missed one is kept rather than removed, so it can be mentioned.
func TestAMissedOneIsKept(t *testing.T) {
	s, user := newStore(t)
	ctx := context.Background()
	r := stored(t, s, user, "Missed", time.Now().Add(-48*time.Hour), remind.Once)

	if err := s.Missed(ctx, r.ID, time.Now().UTC()); err != nil {
		t.Fatalf("Missed: %v", err)
	}

	got, err := s.Get(ctx, user, r.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != remind.Missed {
		t.Errorf("status = %q, want missed", got.Status)
	}
}

// Cancelling twice is not an error: the caller wanted it not to happen.
func TestCancellingTwiceIsFine(t *testing.T) {
	s, user := newStore(t)
	ctx := context.Background()
	r := stored(t, s, user, "Cancel", time.Now().Add(time.Hour), remind.Once)

	for i := 0; i < 2; i++ {
		if err := s.Cancel(ctx, user, r.ID); err != nil {
			t.Fatalf("Cancel %d: %v", i+1, err)
		}
	}
	if got, _ := s.Get(ctx, user, r.ID); got.Status != remind.Cancelled {
		t.Errorf("status = %q, want cancelled", got.Status)
	}
}

// A listing is one person's, in the states asked for, soonest first.
func TestListIsOnePersonsAndOrdered(t *testing.T) {
	s, user := newStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	stored(t, s, user, "Third", now.Add(3*time.Hour), remind.Once)
	stored(t, s, user, "First", now.Add(time.Hour), remind.Once)
	stored(t, s, user, "Second", now.Add(2*time.Hour), remind.Once)

	got, err := s.List(ctx, user, remind.Pending)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d, want 3", len(got))
	}
	for i, want := range []string{"First", "Second", "Third"} {
		if got[i].Title != want {
			t.Errorf("position %d is %q, want %q", i, got[i].Title, want)
		}
		if got[i].UserID != user {
			t.Errorf("found %s's reminder", got[i].UserID)
		}
	}
}
