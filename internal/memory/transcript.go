package memory

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// MaxQuoted : How many past exchanges are put in front of the model.
//
// Fewer than the curated memories. The transcript is larger and looser, so
// its near misses are nearer and more numerous.
const MaxQuoted = 3

// IndexBatch : How many exchanges are embedded in one call.
const IndexBatch = 32

// Exchange : Something said, with the reply it drew.
//
// A turn on its own is often unsearchable -- "yes", "try again" -- and means
// something only beside what it answered.
type Exchange struct {
	// MessageID : The message somebody sent. The exchange is keyed by it.
	MessageID string
	// UserID : Who said it.
	UserID string
	// ConversationID : Where it was said.
	ConversationID string
	// Text : The exchange as it is embedded and as it is shown.
	Text string
	// At : When it was said.
	At time.Time
}

// Heard : A past exchange that might bear on a question.
type Heard struct {
	// Exchange : What was said.
	Exchange Exchange
	// Score : How near it was. Comparable only against other scores from the
	// same search.
	Score float64
}

// Transcript : The searchable record of everything said.
type Transcript interface {
	// Unindexed : Exchanges with no vector from the named model, oldest
	// first, with their text already built.
	Unindexed(ctx context.Context, model string, limit int) ([]Exchange, error)

	// Index : Stores the vector for one exchange.
	Index(ctx context.Context, e Exchange, model string, vector []float32) error

	// NearestTo : The user's past exchanges closest to a vector, nearest
	// first. skipConversation is left out, since what was said there is
	// already in front of the model.
	NearestTo(ctx context.Context, userID string, q []float32, model string,
		skipConversation string, limit int) ([]Heard, error)
}

// ExchangeText : How a message and its reply are written down.
//
// Both halves are named, because a search hit is quoted back to the person
// and "you said" and "I said" are not the same claim.
func ExchangeText(said, reply string) string {
	said, reply = strings.TrimSpace(said), strings.TrimSpace(reply)
	if said == "" {
		return ""
	}
	if reply == "" {
		return "They said: " + said
	}
	return "They said: " + said + "\nYou answered: " + reply
}

// Quoted : What past exchanges look like in a system prompt.
//
// Kept apart from the curated memories, and dated. These are the record of
// what happened, not something the assistant concluded, and they include
// whatever speech-to-text got wrong, so they are quotable and never assertable.
func Quoted(heard []Heard) string {
	if len(heard) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("Earlier exchanges found by searching the record of what was actually said. ")
	b.WriteString("They are a transcript, not something you worked out: quote them as what was ")
	b.WriteString("said and when, and never state one as a fact of your own. Some arrived through ")
	b.WriteString("speech-to-text and may contain the wrong word. They were chosen for resembling ")
	b.WriteString("the question, which is not the same as bearing on it, so ignoring all of them ")
	b.WriteString("is often right.")
	for i := range heard {
		b.WriteString("\n\n[")
		b.WriteString(heard[i].Exchange.At.Format("2 January 2006"))
		b.WriteString("]\n")
		b.WriteString(heard[i].Exchange.Text)
	}
	return b.String()
}

// Said : The past exchanges that might bear on a question.
//
// Nothing from the conversation in progress: it is already in front of the
// model, and offering it back reads as the assistant quoting itself.
func (r *Recall) Said(ctx context.Context, userID, question, skipConversation string) ([]Heard, error) {
	if r == nil || r.Transcript == nil || userID == "" || strings.TrimSpace(question) == "" {
		return nil, nil
	}
	if r.Embedder == nil || !r.Embedder.Available() {
		// No fallback to words here. The curated memories have one, and
		// searching the whole transcript by wording finds whatever repeated
		// a common word rather than whatever bore on the question.
		return nil, nil
	}

	q, err := r.Embedder.Query(ctx, question)
	if err != nil {
		return nil, err
	}

	limit := r.Quoted
	if limit <= 0 {
		limit = MaxQuoted
	}
	return r.Transcript.NearestTo(ctx, userID, q, r.Embedder.Model(), skipConversation, limit)
}

// IndexTranscript : Gives a vector to exchanges that have none.
//
// At startup and after each turn. It reports how many it managed.
func (r *Recall) IndexTranscript(ctx context.Context, limit int) (int, error) {
	if r == nil || r.Transcript == nil || r.Embedder == nil || !r.Embedder.Available() || limit <= 0 {
		return 0, nil
	}

	model := r.Embedder.Model()
	var done int

	for done < limit {
		batch := limit - done
		if batch > IndexBatch {
			batch = IndexBatch
		}

		pending, err := r.Transcript.Unindexed(ctx, model, batch)
		if err != nil {
			return done, err
		}
		if len(pending) == 0 {
			return done, nil
		}

		texts := make([]string, 0, len(pending))
		for i := range pending {
			texts = append(texts, pending[i].Text)
		}

		vectors, err := r.Embedder.Documents(ctx, texts)
		if err != nil {
			return done, fmt.Errorf("memory: embedding %d exchanges: %w", len(texts), err)
		}
		if len(vectors) != len(pending) {
			return done, fmt.Errorf("memory: asked for %d vectors, got %d", len(pending), len(vectors))
		}

		for i := range pending {
			if err := r.Transcript.Index(ctx, pending[i], model, vectors[i]); err != nil {
				r.log(ctx, "cannot store a vector for an exchange", err)
				continue
			}
			done++
		}
	}
	return done, nil
}
