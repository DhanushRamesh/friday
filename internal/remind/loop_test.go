package remind_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/remind"
	"github.com/DhanushRamesh/personal-assistant/internal/remind/inmemory"
)

// heard : A Speaker that records what it was given, and can refuse.
type heard struct {
	mu     sync.Mutex
	said   []remind.Reminder
	refuse error
}

func (h *heard) Say(_ context.Context, r remind.Reminder) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.refuse != nil {
		return h.refuse
	}
	h.said = append(h.said, r)
	return nil
}

func (h *heard) count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.said)
}

// loop : A loop over a fresh store, with the clock held still.
func loop(t *testing.T, at time.Time) (*remind.Loop, *inmemory.Store, *heard) {
	t.Helper()
	store, speaker := inmemory.New(), &heard{}
	return &remind.Loop{
		Store:    store,
		Speaker:  speaker,
		Location: india,
		Now:      func() time.Time { return at },
	}, store, speaker
}

// put : Stores a reminder due at the given time.
func put(t *testing.T, s *inmemory.Store, due time.Time, repeats remind.Repeat) *remind.Reminder {
	t.Helper()
	r, err := remind.New("usr_1", "", remind.ScopeUser, "Wake", "time to get up", due, repeats)
	if err != nil {
		t.Fatalf("remind.New: %v", err)
	}
	if err := s.Create(context.Background(), r); err != nil {
		t.Fatalf("Create: %v", err)
	}
	return r
}

// Something due is said, once, and finished.
func TestSomethingDueIsSaidAndFinished(t *testing.T) {
	now := at(2026, 9, 26, 7, 0)
	l, store, spoke := loop(t, now)
	r := put(t, store, now.Add(-time.Minute), remind.Once)

	if got := l.Once(context.Background()); got.Said != 1 {
		t.Fatalf("pass = %+v, want one said", got)
	}
	if spoke.count() != 1 {
		t.Errorf("said %d times, want once", spoke.count())
	}

	after, _ := store.Get(context.Background(), "usr_1", r.ID)
	if after.Status != remind.Done {
		t.Errorf("status = %q, want done", after.Status)
	}
}

// Nothing due is said, which is almost every pass for ever.
func TestNothingDueSaysNothing(t *testing.T) {
	now := at(2026, 9, 26, 7, 0)
	l, store, spoke := loop(t, now)
	put(t, store, now.Add(time.Hour), remind.Once)

	if got := l.Once(context.Background()); got != (remind.Pass{}) {
		t.Errorf("pass = %+v, want nothing done", got)
	}
	if spoke.count() != 0 {
		t.Error("something not yet due was said")
	}
}

// Two passes do not say the same thing twice.
func TestItIsNotSaidTwice(t *testing.T) {
	now := at(2026, 9, 26, 7, 0)
	l, store, spoke := loop(t, now)
	put(t, store, now.Add(-time.Minute), remind.Once)

	l.Once(context.Background())
	l.Once(context.Background())

	if spoke.count() != 1 {
		t.Errorf("said %d times, want once", spoke.count())
	}
}

// A repeating one is said and comes back, rather than finishing.
func TestARepeatingOneComesBack(t *testing.T) {
	now := at(2026, 9, 26, 7, 0)
	l, store, _ := loop(t, now)
	r := put(t, store, now.Add(-time.Minute), remind.Daily)

	l.Once(context.Background())

	after, _ := store.Get(context.Background(), "usr_1", r.ID)
	if after.Status != remind.Pending {
		t.Errorf("status = %q, want it still pending", after.Status)
	}
	if !after.DueAt.After(now) {
		t.Errorf("due = %v, want a time after now", after.DueAt.In(india))
	}
	if after.Fires != 1 {
		t.Errorf("fires = %d, want 1", after.Fires)
	}
}

// Something far too late is not said. A timer three hours late is wrong.
func TestSomethingTooLateIsNotSaid(t *testing.T) {
	now := at(2026, 9, 26, 7, 0)
	l, store, spoke := loop(t, now)
	r := put(t, store, now.Add(-3*time.Hour), remind.Once)

	if got := l.Once(context.Background()); got.Missed != 1 {
		t.Fatalf("pass = %+v, want one missed", got)
	}
	if spoke.count() != 0 {
		t.Error("something three hours late was said anyway")
	}

	after, _ := store.Get(context.Background(), "usr_1", r.ID)
	if after.Status != remind.Missed {
		t.Errorf("status = %q, want missed", after.Status)
	}
}

// Something a little late is still said, because it is still worth having.
func TestSomethingALittleLateIsStillSaid(t *testing.T) {
	now := at(2026, 9, 26, 7, 0)
	l, store, spoke := loop(t, now)
	put(t, store, now.Add(-20*time.Minute), remind.Once)

	if got := l.Once(context.Background()); got.Said != 1 {
		t.Errorf("pass = %+v, want it said", got)
	}
	if spoke.count() != 1 {
		t.Error("something twenty minutes late was dropped")
	}
}

// A repeating one that was missed is moved on, not ended. Marking it
// missed would stop it for good.
func TestAMissedRepeatIsMovedOnNotEnded(t *testing.T) {
	now := at(2026, 9, 26, 12, 0)
	l, store, spoke := loop(t, now)
	r := put(t, store, at(2026, 9, 20, 7, 0), remind.Daily)

	got := l.Once(context.Background())
	if got.Moved != 1 || got.Missed != 0 {
		t.Fatalf("pass = %+v, want it moved on", got)
	}
	if spoke.count() != 0 {
		t.Error("a reminder six days late was said")
	}

	after, _ := store.Get(context.Background(), "usr_1", r.ID)
	if after.Status != remind.Pending {
		t.Errorf("status = %q, want it still alive", after.Status)
	}
	if next := after.DueAt.In(india); next.Day() != 27 || next.Hour() != 7 {
		t.Errorf("next = %v, want 7am on the 27th", next)
	}
	if after.Fires != 0 {
		t.Errorf("fires = %d, want nothing counted: nobody heard it", after.Fires)
	}
}

// Nothing to say it means it stays pending and is tried again, rather
// than being recorded as delivered.
func TestSomethingNobodyCouldSayStaysPending(t *testing.T) {
	now := at(2026, 9, 26, 7, 0)
	l, store, spoke := loop(t, now)
	spoke.refuse = remind.ErrNowhereToSay
	r := put(t, store, now.Add(-time.Minute), remind.Once)

	if got := l.Once(context.Background()); got.Failed != 1 {
		t.Fatalf("pass = %+v, want one failure", got)
	}

	after, _ := store.Get(context.Background(), "usr_1", r.ID)
	if after.Status != remind.Pending {
		t.Errorf("status = %q, want it still pending to try again", after.Status)
	}
	if after.Fires != 0 {
		t.Errorf("fires = %d, want nothing counted", after.Fires)
	}
}

// Trying again cannot go on for ever: once it is older than the grace
// window it becomes missed.
func TestRetryingEndsAtTheGraceWindow(t *testing.T) {
	due := at(2026, 9, 26, 7, 0)
	store, spoke := inmemory.New(), &heard{refuse: remind.ErrNowhereToSay}

	now := due.Add(time.Minute)
	l := &remind.Loop{
		Store: store, Speaker: spoke, Location: india,
		Now: func() time.Time { return now },
	}
	r := put(t, store, due, remind.Once)

	if got := l.Once(context.Background()); got.Failed != 1 {
		t.Fatalf("pass = %+v, want it retried", got)
	}

	now = due.Add(2 * time.Hour)
	if got := l.Once(context.Background()); got.Missed != 1 {
		t.Fatalf("pass = %+v, want it given up on", got)
	}

	after, _ := store.Get(context.Background(), "usr_1", r.ID)
	if after.Status != remind.Missed {
		t.Errorf("status = %q, want missed", after.Status)
	}
}

// Running says what is due and stops when told.
func TestRunSaysAndStops(t *testing.T) {
	now := time.Now().UTC()
	store, spoke := inmemory.New(), &heard{}
	l := &remind.Loop{
		Store: store, Speaker: spoke, Location: india,
		Every: 5 * time.Millisecond,
	}
	put(t, store, now.Add(-time.Minute), remind.Once)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { l.Run(ctx); close(done) }()

	deadline := time.Now().Add(2 * time.Second)
	for spoke.count() == 0 && time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
	}
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the loop did not stop when its context ended")
	}
	if spoke.count() != 1 {
		t.Errorf("said %d times, want once", spoke.count())
	}
}

// Nothing configured to speak refuses rather than reporting success, so a
// reminder nobody could hear is not recorded as delivered.
func TestNowhereRefuses(t *testing.T) {
	err := remind.Nowhere{}.Say(context.Background(), remind.Reminder{})
	if !errors.Is(err, remind.ErrNowhereToSay) {
		t.Errorf("error = %v, want ErrNowhereToSay", err)
	}
}

// One delivery is enough: a browser that is closed does not stop the
// satellite saying it.
func TestEverywhereNeedsOnlyOneToTake(t *testing.T) {
	good := &heard{}
	e := remind.Everywhere{To: []remind.Speaker{remind.Nowhere{}, good}}

	if err := e.Say(context.Background(), remind.Reminder{Body: "up"}); err != nil {
		t.Errorf("Say: %v", err)
	}
	if good.count() != 1 {
		t.Error("the one place that would take it did not get it")
	}
}

// Nowhere at all is a failure, not a quiet success.
func TestEverywhereFailsWhenNobodyTakesIt(t *testing.T) {
	e := remind.Everywhere{To: []remind.Speaker{remind.Nowhere{}, remind.Nowhere{}}}

	if err := e.Say(context.Background(), remind.Reminder{Body: "up"}); err == nil {
		t.Error("it reported success with nowhere to deliver")
	}
}

// What is said is what the reminder says, not its filing name.
func TestSpokenIsTheBodyNotTheTitle(t *testing.T) {
	got := remind.Spoken(remind.Reminder{Title: "Wake", Body: "time to get up"})
	if got != "time to get up" {
		t.Errorf("Spoken = %q", got)
	}
}
