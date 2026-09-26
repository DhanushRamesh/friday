// Package inmemory : Holds reminders in memory rather than a database.
//
// It exists so the firing loop can be exercised without MySQL, and it
// behaves as the MySQL store does wherever the difference would let a bug
// through: ownership is enforced, listings come back soonest first, and
// firing something that is no longer pending is refused.
//
// It is not durable and is not meant to be.
package inmemory

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/remind"
)

// Store : An in-memory remind.Store.
type Store struct {
	mu   sync.Mutex
	kept map[string]remind.Reminder
}

// New : An empty Store.
func New() *Store { return &Store{kept: map[string]remind.Reminder{}} }

// Create : Stores a new reminder.
func (s *Store) Create(_ context.Context, r *remind.Reminder) error {
	if err := r.Valid(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.kept[r.ID] = *r
	return nil
}

// Get : Returns one reminder, or remind.ErrNotFound.
func (s *Store) Get(_ context.Context, userID, id string) (*remind.Reminder, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	r, ok := s.kept[id]
	if !ok || r.UserID != userID {
		return nil, remind.ErrNotFound
	}
	return &r, nil
}

// List : A person's reminders in the given states, soonest first.
func (s *Store) List(_ context.Context, userID string, states ...remind.Status) ([]remind.Reminder, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	wanted := map[remind.Status]bool{}
	for _, state := range states {
		wanted[state] = true
	}

	out := make([]remind.Reminder, 0, len(s.kept))
	for _, r := range s.kept {
		if r.UserID != userID || (len(wanted) > 0 && !wanted[r.Status]) {
			continue
		}
		out = append(out, r)
	}
	soonestFirst(out)
	return out, nil
}

// Cancel : Calls one off.
func (s *Store) Cancel(_ context.Context, userID, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	r, ok := s.kept[id]
	if !ok || r.UserID != userID {
		return remind.ErrNotFound
	}
	r.Status = remind.Cancelled
	r.UpdatedAt = time.Now().UTC()
	s.kept[id] = r
	return nil
}

// Due : Pending reminders whose time has come, soonest first.
func (s *Store) Due(_ context.Context, at time.Time, limit int) ([]remind.Reminder, error) {
	if limit <= 0 {
		limit = remind.DefaultDueLimit
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]remind.Reminder, 0, len(s.kept))
	for _, r := range s.kept {
		if r.Status == remind.Pending && !r.DueAt.After(at) {
			out = append(out, r)
		}
	}
	soonestFirst(out)

	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// Fired : Records that a reminder was said. A zero next finishes it.
func (s *Store) Fired(_ context.Context, id string, at, next time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	r, ok := s.kept[id]
	if !ok || r.Status != remind.Pending {
		return remind.ErrNotFound
	}

	fired := at.UTC()
	r.LastFiredAt, r.Fires, r.UpdatedAt = &fired, r.Fires+1, fired
	if next.IsZero() {
		r.Status = remind.Done
	} else {
		r.DueAt = next.UTC()
	}
	s.kept[id] = r
	return nil
}

// Missed : Records that a reminder's time passed with nothing listening.
func (s *Store) Missed(_ context.Context, id string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	r, ok := s.kept[id]
	if !ok || r.Status != remind.Pending {
		return remind.ErrNotFound
	}
	r.Status, r.UpdatedAt = remind.Missed, at.UTC()
	s.kept[id] = r
	return nil
}

// Reschedule : Moves a reminder to its next time without saying it.
func (s *Store) Reschedule(_ context.Context, id string, next time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	r, ok := s.kept[id]
	if !ok || r.Status != remind.Pending {
		return remind.ErrNotFound
	}
	r.DueAt, r.UpdatedAt = next.UTC(), time.Now().UTC()
	s.kept[id] = r
	return nil
}

// soonestFirst : Orders reminders as the MySQL store does, so a test
// cannot pass here and fail there.
func soonestFirst(r []remind.Reminder) {
	sort.SliceStable(r, func(i, j int) bool {
		if !r[i].DueAt.Equal(r[j].DueAt) {
			return r[i].DueAt.Before(r[j].DueAt)
		}
		return r[i].ID < r[j].ID
	})
}

// Ensure the in-memory store satisfies the interface it stands in for.
var _ remind.Store = (*Store)(nil)
