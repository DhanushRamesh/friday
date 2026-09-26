package reminders_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/api/apitest"
	"github.com/DhanushRamesh/personal-assistant/internal/api/reminders"
	"github.com/DhanushRamesh/personal-assistant/internal/chat"
	"github.com/DhanushRamesh/personal-assistant/internal/remind"
	"github.com/DhanushRamesh/personal-assistant/internal/remind/inmemory"
)

// env : A server with a reminder store behind it.
func env(t *testing.T) (*apitest.Env, *inmemory.Store) {
	t.Helper()
	store := inmemory.New()
	return apitest.NewWith(t, apitest.Options{Reminders: store}), store
}

// put : Stores a reminder for the logged-in person.
func put(t *testing.T, e *apitest.Env, s *inmemory.Store, title string, due time.Time) *remind.Reminder {
	t.Helper()
	r, err := remind.New(e.User.ID, "", remind.ScopeUser, title, "time to "+title, due, remind.Once)
	if err != nil {
		t.Fatalf("remind.New: %v", err)
	}
	if err := s.Create(t.Context(), r); err != nil {
		t.Fatalf("Create: %v", err)
	}
	return r
}

// listed : The listing, decoded.
func listed(t *testing.T, e *apitest.Env, path string) reminders.ListResponse {
	t.Helper()
	rec := e.Get(t, path)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var got reminders.ListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	return got
}

// What is coming is listed, soonest first.
func TestListShowsWhatIsComing(t *testing.T) {
	e, store := env(t)
	now := time.Now().UTC()
	put(t, e, store, "Later", now.Add(2*time.Hour))
	put(t, e, store, "Sooner", now.Add(time.Hour))

	got := listed(t, e, "/v1/reminders")
	if len(got.Reminders) != 2 {
		t.Fatalf("got %d, want 2", len(got.Reminders))
	}
	if got.Reminders[0].Title != "Sooner" {
		t.Errorf("first is %q, want the sooner one", got.Reminders[0].Title)
	}
	if got.Reminders[0].Say == "" || got.Reminders[0].Status != string(remind.Pending) {
		t.Errorf("reminder = %+v", got.Reminders[0])
	}
}

// Only what is still coming, unless asked otherwise. A list of everything
// that ever fired is a log, and nobody opens a settings screen for one.
func TestFinishedOnesAreLeftOutUnlessAsked(t *testing.T) {
	e, store := env(t)
	now := time.Now().UTC()
	coming := put(t, e, store, "Coming", now.Add(time.Hour))
	gone := put(t, e, store, "Gone", now.Add(-time.Hour))
	if err := store.Cancel(t.Context(), e.User.ID, gone.ID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	got := listed(t, e, "/v1/reminders")
	if len(got.Reminders) != 1 || got.Reminders[0].ID != coming.ID {
		t.Errorf("got %d reminders, want only the one still coming", len(got.Reminders))
	}

	all := listed(t, e, "/v1/reminders?all=true")
	if len(all.Reminders) != 2 {
		t.Errorf("got %d with all=true, want 2", len(all.Reminders))
	}
}

// Nothing waiting is an empty list, not a failure.
func TestNothingWaitingIsAnEmptyList(t *testing.T) {
	e, _ := env(t)

	if got := listed(t, e, "/v1/reminders"); len(got.Reminders) != 0 {
		t.Errorf("got %d, want none", len(got.Reminders))
	}
}

// Cancelling stops it, and says so.
func TestCancellingStopsIt(t *testing.T) {
	e, store := env(t)
	r := put(t, e, store, "Timer", time.Now().UTC().Add(time.Hour))

	rec := e.Do(t, http.MethodDelete, "/v1/reminders/"+r.ID, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	after, err := store.Get(t.Context(), e.User.ID, r.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if after.Status != remind.Cancelled {
		t.Errorf("status = %q, want cancelled", after.Status)
	}
}

// An identifier that is not one, or names nothing, is missing rather than
// an error.
func TestAnUnknownReminderIsNotFound(t *testing.T) {
	e, _ := env(t)

	for _, id := range []string{remind.NewID(), "not-an-id", "rem_garbage"} {
		if rec := e.Do(t, http.MethodDelete, "/v1/reminders/"+id, ""); rec.Code != http.StatusNotFound {
			t.Errorf("DELETE %s: status = %d, want 404", id, rec.Code)
		}
	}
}

// One person must never see or call off another's, and must not learn it
// exists either.
func TestAnotherPersonsReminderIsHidden(t *testing.T) {
	e, store := env(t)

	stranger, err := chat.NewUser("stranger", "hash")
	if err != nil {
		t.Fatalf("chat.NewUser: %v", err)
	}
	if err := e.Repo.CreateUser(t.Context(), stranger); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	theirs, err := remind.New(stranger.ID, "", remind.ScopeUser, "Private", "their business", time.Now().Add(time.Hour), remind.Once)
	if err != nil {
		t.Fatalf("remind.New: %v", err)
	}
	if err := store.Create(t.Context(), theirs); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if got := listed(t, e, "/v1/reminders?all=true"); len(got.Reminders) != 0 {
		t.Errorf("another person's reminder was listed: %+v", got.Reminders)
	}
	if rec := e.Do(t, http.MethodDelete, "/v1/reminders/"+theirs.ID, ""); rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}

	still, err := store.Get(t.Context(), stranger.ID, theirs.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if still.Status != remind.Pending {
		t.Error("another person's reminder was called off")
	}
}

// A server with nowhere to keep reminders serves an empty list rather
// than failing.
func TestNoStoreIsAnEmptyList(t *testing.T) {
	e := apitest.NewWith(t, apitest.Options{})

	if got := listed(t, e, "/v1/reminders"); len(got.Reminders) != 0 {
		t.Errorf("got %d, want none", len(got.Reminders))
	}
}
