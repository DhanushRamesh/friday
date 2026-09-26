// Package inmemory : Holds memories in memory rather than a database.
//
// It exists so that everything above the store can be exercised without
// MySQL. It is a real implementation of memory.Store, not a stub, and
// behaves as the MySQL one does wherever the difference would let a bug
// through: ownership is enforced between users, listings come back oldest
// first, and updating a memory clears its vector.
//
// It is not durable and is not meant to be.
package inmemory

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/embed"
	"github.com/DhanushRamesh/personal-assistant/internal/memory"
)

// Store : An in-memory memory.Store.
type Store struct {
	mu   sync.RWMutex
	kept map[string]memory.Memory
}

// New : An empty Store.
func New() *Store { return &Store{kept: map[string]memory.Memory{}} }

// Create : Stores a new memory.
func (s *Store) Create(_ context.Context, m *memory.Memory) error {
	if err := m.Valid(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.kept[m.ID] = *m
	return nil
}

// Get : Returns one memory, or memory.ErrNotFound.
func (s *Store) Get(_ context.Context, userID, id string) (*memory.Memory, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	m, ok := s.kept[id]
	if !ok || m.UserID != userID {
		return nil, memory.ErrNotFound
	}
	return &m, nil
}

// Update : Replaces a memory's subject, body and tier, clearing its vector.
func (s *Store) Update(_ context.Context, m *memory.Memory) error {
	if err := m.Valid(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	kept, ok := s.kept[m.ID]
	if !ok || kept.UserID != m.UserID {
		return memory.ErrNotFound
	}

	kept.Tier, kept.Subject, kept.Body = m.Tier, m.Subject, m.Body
	kept.Embedding, kept.EmbedModel = nil, ""
	kept.UpdatedAt = time.Now().UTC()
	s.kept[m.ID] = kept

	m.Embedding, m.EmbedModel = nil, ""
	m.UpdatedAt = kept.UpdatedAt
	return nil
}

// Forget : Removes a memory. Removing one already gone is not an error.
func (s *Store) Forget(_ context.Context, userID, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if m, ok := s.kept[id]; ok && m.UserID == userID {
		delete(s.kept, id)
	}
	return nil
}

// All : Every memory in a tier, oldest first.
func (s *Store) All(_ context.Context, userID string, tier memory.Tier) ([]memory.Memory, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]memory.Memory, 0, len(s.kept))
	for _, m := range s.kept {
		if m.UserID == userID && m.Tier == tier {
			out = append(out, m)
		}
	}
	oldestFirst(out)
	return out, nil
}

// NearestTo : The user's memories closest to a vector, nearest first.
func (s *Store) NearestTo(_ context.Context, userID string, q embed.Vector, model string, limit int) ([]memory.Match, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]memory.Match, 0, len(s.kept))
	for _, m := range s.kept {
		if m.UserID != userID || !m.Embedded(model) {
			continue
		}
		out = append(out, memory.Match{Memory: m, Score: embed.Similarity(q, m.Embedding)})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })

	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// Matching : The user's memories whose words match a question, best first.
func (s *Store) Matching(_ context.Context, userID, question string, limit int) ([]memory.Match, error) {
	wanted := words(question)
	if len(wanted) == 0 {
		return nil, nil
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]memory.Match, 0, len(s.kept))
	for _, m := range s.kept {
		if m.UserID != userID {
			continue
		}
		have := words(m.Text())
		var shared int
		for w := range wanted {
			if have[w] {
				shared++
			}
		}
		if shared > 0 {
			out = append(out, memory.Match{
				Memory:  m,
				Score:   float64(shared) / float64(len(wanted)),
				ByWords: true,
			})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })

	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// SetEmbedding : Records the vector for a memory, and what produced it.
func (s *Store) SetEmbedding(_ context.Context, id, model string, vector []float32) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	m, ok := s.kept[id]
	if !ok {
		return memory.ErrNotFound
	}
	m.EmbedModel, m.Embedding = model, vector
	s.kept[id] = m
	return nil
}

// Unembedded : Memories with no vector from the named model, oldest first.
func (s *Store) Unembedded(_ context.Context, model string, limit int) ([]memory.Memory, error) {
	if limit <= 0 {
		return nil, nil
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]memory.Memory, 0, len(s.kept))
	for _, m := range s.kept {
		if !m.Embedded(model) {
			out = append(out, m)
		}
	}
	oldestFirst(out)

	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// Used : Records that memories were given to the model.
func (s *Store) Used(_ context.Context, ids []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	at := time.Now().UTC()
	for _, id := range ids {
		if m, ok := s.kept[id]; ok {
			m.Uses++
			m.LastUsedAt = &at
			s.kept[id] = m
		}
	}
	return nil
}

// oldestFirst : Orders memories as the MySQL store does, so a test cannot
// pass here and fail there.
func oldestFirst(m []memory.Memory) {
	sort.SliceStable(m, func(i, j int) bool {
		if !m[i].CreatedAt.Equal(m[j].CreatedAt) {
			return m[i].CreatedAt.Before(m[j].CreatedAt)
		}
		return m[i].ID < m[j].ID
	})
}

// words : The distinct words of text, lowercased.
func words(text string) map[string]bool {
	out := map[string]bool{}
	for _, w := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !(r == '_' || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'))
	}) {
		if len(w) > 2 {
			out[w] = true
		}
	}
	return out
}

// Ensure the in-memory store satisfies the interface it stands in for.
var _ memory.Store = (*Store)(nil)
