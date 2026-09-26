package memory

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/DhanushRamesh/personal-assistant/internal/embed"
)

// DefaultCandidates : How many memories are put in front of the model.
//
// The model decides which of them, if any, belongs. Three is enough for the
// right one to be among them and few enough that a prompt is not filled with
// things that are not.
const DefaultCandidates = 3

// DefaultAlwaysCap : The most memories that may go into every prompt.
const DefaultAlwaysCap = 25

// DefaultAlwaysBytes : The most room those memories may take.
//
// A ceiling in bytes as well as in count, because one long memory costs the
// conversation as much as several short ones.
const DefaultAlwaysBytes = 2000

// Recall : Finds the memories worth showing the model.
type Recall struct {
	// Store : Where memories are kept.
	Store Store
	// Embedder : What turns a question into something comparable. Off or nil
	// falls back to matching words.
	Embedder embed.Embedder
	// Candidates : How many to offer. Zero selects DefaultCandidates.
	Candidates int
	// AlwaysCap : The most always-memories to use. Zero selects
	// DefaultAlwaysCap.
	AlwaysCap int
	// AlwaysBytes : The most room they may take. Zero selects
	// DefaultAlwaysBytes.
	AlwaysBytes int
	// Logger : Where a failure goes. Optional.
	Logger *slog.Logger
}

// For : The memories that might bear on a question, nearest first.
//
// Nearest is not the same as relevant: a question with nothing stored about
// it still has a nearest memory, and it scores in the same range as a real
// match. Whoever calls this shows them to the model and lets it decide.
func (r *Recall) For(ctx context.Context, userID, question string) ([]Match, error) {
	if r == nil || r.Store == nil || strings.TrimSpace(question) == "" || userID == "" {
		return nil, nil
	}

	limit := r.Candidates
	if limit <= 0 {
		limit = DefaultCandidates
	}

	if r.Embedder != nil && r.Embedder.Available() {
		q, err := r.Embedder.Query(ctx, question)
		if err == nil {
			return r.Store.NearestTo(ctx, userID, q, r.Embedder.Model(), limit)
		}
		// The embedding server is the part most likely to be absent, and a
		// memory system that goes blind when it is would be worse than one
		// that matches words for a while.
		r.log(ctx, "cannot embed the question, falling back to words", err)
	}

	return r.Store.Matching(ctx, userID, question, limit)
}

// Always : The memories that go into every prompt, within their ceilings.
//
// Oldest first, stopping at whichever ceiling is reached first. Silently
// dropping the rest is the point: this is composed into every prompt, so it
// has to have a size that cannot run away.
func (r *Recall) Always(ctx context.Context, userID string) ([]Memory, error) {
	if r == nil || r.Store == nil || userID == "" {
		return nil, nil
	}

	all, err := r.Store.All(ctx, userID, TierAlways)
	if err != nil {
		return nil, err
	}

	maxCount, maxBytes := r.AlwaysCap, r.AlwaysBytes
	if maxCount <= 0 {
		maxCount = DefaultAlwaysCap
	}
	if maxBytes <= 0 {
		maxBytes = DefaultAlwaysBytes
	}

	out := make([]Memory, 0, len(all))
	used := 0
	for _, m := range all {
		size := len(m.Text()) + 1
		if len(out) >= maxCount || used+size > maxBytes {
			break
		}
		out = append(out, m)
		used += size
	}
	return out, nil
}

// Embed : Gives a vector to memories that have none.
//
// Called after writing one, and at startup for anything written while the
// embedding server was away. It reports how many it managed.
func (r *Recall) Embed(ctx context.Context, limit int) (int, error) {
	if r == nil || r.Store == nil || r.Embedder == nil || !r.Embedder.Available() {
		return 0, nil
	}

	model := r.Embedder.Model()
	pending, err := r.Store.Unembedded(ctx, model, limit)
	if err != nil {
		return 0, err
	}
	if len(pending) == 0 {
		return 0, nil
	}

	texts := make([]string, 0, len(pending))
	for i := range pending {
		texts = append(texts, pending[i].Text())
	}

	vectors, err := r.Embedder.Documents(ctx, texts)
	if err != nil {
		return 0, fmt.Errorf("memory: embedding %d memories: %w", len(texts), err)
	}
	if len(vectors) != len(pending) {
		return 0, fmt.Errorf("memory: asked for %d vectors, got %d", len(pending), len(vectors))
	}

	var done int
	for i := range pending {
		if err := r.Store.SetEmbedding(ctx, pending[i].ID, model, vectors[i]); err != nil {
			r.log(ctx, "cannot store a vector", err)
			continue
		}
		done++
	}
	return done, nil
}

// log : Records a failure, when there is anywhere to record it.
func (r *Recall) log(ctx context.Context, msg string, err error) {
	if r.Logger != nil {
		r.Logger.WarnContext(ctx, msg, slog.Any("error", err))
	}
}
