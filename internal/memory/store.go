package memory

import (
	"context"

	"github.com/DhanushRamesh/personal-assistant/internal/embed"
)

// Store : Where memories are kept.
type Store interface {
	// Create : Stores a new memory.
	Create(ctx context.Context, m *Memory) error

	// Get : Returns one memory, or ErrNotFound.
	Get(ctx context.Context, userID, id string) (*Memory, error)

	// Update : Replaces a memory's subject, body and tier, and clears its
	// embedding, since the text it was made from has changed.
	Update(ctx context.Context, m *Memory) error

	// Forget : Removes a memory. Removing one that is already gone is not an
	// error.
	Forget(ctx context.Context, userID, id string) error

	// All : Every memory in a tier, oldest first.
	All(ctx context.Context, userID string, tier Tier) ([]Memory, error)

	// NearestTo : The user's memories closest to a vector, nearest first.
	// Only those embedded by the named model, since vectors from two models
	// cannot be compared.
	NearestTo(ctx context.Context, userID string, q embed.Vector, model string, limit int) ([]Match, error)

	// Matching : The user's memories whose words match a question, best
	// first. The fallback for when there is no embedding server.
	Matching(ctx context.Context, userID, question string, limit int) ([]Match, error)

	// SetEmbedding : Records the vector for a memory, and what produced it.
	SetEmbedding(ctx context.Context, id, model string, vector []float32) error

	// Unembedded : Memories with no vector from the named model, oldest
	// first, so they can be caught up after the embedding server has been
	// away.
	Unembedded(ctx context.Context, model string, limit int) ([]Memory, error)

	// Used : Records that memories were given to the model.
	Used(ctx context.Context, ids []string) error
}

// Match : A memory that might answer a question.
type Match struct {
	// Memory : What was found.
	Memory Memory
	// Score : How near it was, from 0 to 1 for a vector comparison.
	//
	// Comparable only against other scores from the same search. It says
	// which candidate is nearest, never whether any of them is relevant.
	Score float64
	// ByWords : Whether words were compared rather than vectors, which
	// happens when there is no embedding server.
	ByWords bool
}
