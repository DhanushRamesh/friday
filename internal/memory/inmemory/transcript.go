package inmemory

import (
	"context"
	"sort"
	"sync"

	"github.com/DhanushRamesh/personal-assistant/internal/embed"
	"github.com/DhanushRamesh/personal-assistant/internal/memory"
)

// Transcript : An in-memory memory.Transcript.
//
// It behaves as the MySQL one does wherever the difference would let a bug
// through: ownership is enforced, the conversation in progress is left out,
// and re-indexing replaces rather than duplicating.
type Transcript struct {
	mu      sync.RWMutex
	order   []string
	said    map[string]memory.Exchange
	vectors map[string][]float32
	model   map[string]string
}

// NewTranscript : An empty Transcript.
func NewTranscript() *Transcript {
	return &Transcript{
		said:    map[string]memory.Exchange{},
		vectors: map[string][]float32{},
		model:   map[string]string{},
	}
}

// Add : Records something said, as the message store would have.
func (t *Transcript) Add(e memory.Exchange) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if _, seen := t.said[e.MessageID]; !seen {
		t.order = append(t.order, e.MessageID)
	}
	t.said[e.MessageID] = e
}

// Unindexed : Exchanges with no vector from the named model, oldest first.
func (t *Transcript) Unindexed(_ context.Context, model string, limit int) ([]memory.Exchange, error) {
	if limit <= 0 {
		return nil, nil
	}

	t.mu.RLock()
	defer t.mu.RUnlock()

	out := make([]memory.Exchange, 0, limit)
	for _, id := range t.order {
		if t.model[id] == model && t.vectors[id] != nil {
			continue
		}
		if out = append(out, t.said[id]); len(out) == limit {
			break
		}
	}
	return out, nil
}

// Index : Stores the vector for one exchange, replacing any it had.
func (t *Transcript) Index(_ context.Context, e memory.Exchange, model string, vector []float32) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if _, seen := t.said[e.MessageID]; !seen {
		t.order = append(t.order, e.MessageID)
	}
	t.said[e.MessageID] = e
	t.vectors[e.MessageID] = vector
	t.model[e.MessageID] = model
	return nil
}

// NearestTo : The user's past exchanges closest to a vector, nearest first.
func (t *Transcript) NearestTo(_ context.Context, userID string, q []float32,
	model, skipConversation string, limit int) ([]memory.Heard, error) {

	if limit <= 0 || len(q) == 0 {
		return nil, nil
	}

	t.mu.RLock()
	defer t.mu.RUnlock()

	out := make([]memory.Heard, 0, len(t.order))
	for _, id := range t.order {
		e := t.said[id]
		switch {
		case e.UserID != userID, t.model[id] != model, t.vectors[id] == nil:
			continue
		case skipConversation != "" && e.ConversationID == skipConversation:
			continue
		}
		out = append(out, memory.Heard{Exchange: e, Score: embed.Similarity(q, t.vectors[id])})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })

	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// Ensure the in-memory transcript satisfies the interface it stands in for.
var _ memory.Transcript = (*Transcript)(nil)
