package mysql_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/chat"
	"github.com/DhanushRamesh/personal-assistant/internal/conversation"
)

func TestAppendNumbersMessagesInOrder(t *testing.T) {
	r := newRepository(t)
	ctx := context.Background()
	id := storedConversation(t, r)

	at := time.Now().UTC().Truncate(time.Millisecond)
	for i, content := range []string{"what is the time", "half past two", "and the date"} {
		m, err := r.Append(ctx, conversation.Message{
			ConversationID: id,
			Kind:           conversation.Chat,
			Role:           conversation.User,
			Content:        content,
			At:             at.Add(time.Duration(i) * time.Second),
		})
		if err != nil {
			t.Fatalf("Append(%q): %v", content, err)
		}
		if m.Seq != i+1 {
			t.Errorf("Seq = %d, want %d", m.Seq, i+1)
		}
	}

	said, err := r.All(ctx, id)
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if len(said) != 3 {
		t.Fatalf("read back %d messages, want 3", len(said))
	}
	if said[0].Content != "what is the time" || said[2].Content != "and the date" {
		t.Errorf("read back out of order: %q then %q", said[0].Content, said[2].Content)
	}
	if !said[0].At.Equal(at) {
		t.Errorf("At = %v, want %v", said[0].At, at)
	}
}

// Before is what keeps a question out of its own history, so the boundary
// matters: the message at the given position is excluded, the one before it
// is not.
func TestBeforeStopsShortOfThePosition(t *testing.T) {
	r := newRepository(t)
	ctx := context.Background()
	id := storedConversation(t, r)

	for _, content := range []string{"first", "second", "third"} {
		if _, err := r.Append(ctx, conversation.Said(id, content, time.Now().UTC())); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}

	said, err := r.Before(ctx, id, 3)
	if err != nil {
		t.Fatalf("Before: %v", err)
	}
	if len(said) != 2 {
		t.Fatalf("read %d messages, want the two before the third", len(said))
	}
	if said[1].Content != "second" {
		t.Errorf("last message = %q, want \"second\"", said[1].Content)
	}
}

// Two appends racing must not be given the same position — one of them would
// silently replace the other.
func TestConcurrentAppendsGetDistinctPositions(t *testing.T) {
	r := newRepository(t)
	ctx := context.Background()
	id := storedConversation(t, r)

	const writers = 5
	var wg sync.WaitGroup
	errs := make([]error, writers)

	wg.Add(writers)
	for i := 0; i < writers; i++ {
		go func(i int) {
			defer wg.Done()
			_, errs[i] = r.Append(ctx, conversation.Said(id, "message", time.Now().UTC()))
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("writer %d: %v", i, err)
		}
	}

	said, err := r.All(ctx, id)
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if len(said) != writers {
		t.Fatalf("stored %d messages, want %d — a position was reused", len(said), writers)
	}

	seen := map[int]bool{}
	for _, m := range said {
		if seen[m.Seq] {
			t.Errorf("position %d was used twice", m.Seq)
		}
		seen[m.Seq] = true
	}
}

func TestAppendRefusesAMessageWithNoConversation(t *testing.T) {
	r := newRepository(t)

	_, err := r.Append(context.Background(),
		conversation.Said("sess_00000000000000000000000000", "hello", time.Now().UTC()))
	if !errors.Is(err, conversation.ErrNoConversation) {
		t.Errorf("err = %v, want ErrNoConversation", err)
	}
}

func TestAppendRefusesAnEmptyMessage(t *testing.T) {
	r := newRepository(t)
	id := storedConversation(t, r)

	if _, err := r.Append(context.Background(), conversation.Said(id, "   ", time.Now().UTC())); err == nil {
		t.Error("an empty message was stored")
	}
}

func TestAppendRefusesAMessageTooLarge(t *testing.T) {
	r := newRepository(t)
	id := storedConversation(t, r)

	huge := strings.Repeat("a", (1<<20)+1)
	_, err := r.Append(context.Background(), conversation.Said(id, huge, time.Now().UTC()))
	if !errors.Is(err, conversation.ErrTooLarge) {
		t.Errorf("err = %v, want ErrTooLarge", err)
	}
}

// A conversation with nothing condensed reports the zero summary rather than an
// error: having no summary yet is the ordinary case.
func TestSummaryOfAnUncondensedConversation(t *testing.T) {
	r := newRepository(t)
	id := storedConversation(t, r)

	got, err := r.Summary(context.Background(), id)
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if got.Text != "" || got.ThroughSeq != 0 {
		t.Errorf("Summary = %+v, want the zero one", got)
	}
}

// Both columns survive the round trip. A field added to a row but left out of
// the write is stored nowhere, and only the database says so.
func TestSummarySurvivesStorage(t *testing.T) {
	r := newRepository(t)
	ctx := context.Background()
	id := storedConversation(t, r)

	want := conversation.Summary{Text: "they agreed on the roof", ThroughSeq: 42}
	if err := r.SetSummary(ctx, id, want); err != nil {
		t.Fatalf("SetSummary: %v", err)
	}

	got, err := r.Summary(ctx, id)
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if got != want {
		t.Errorf("Summary = %+v, want %+v", got, want)
	}
}

// Condensing again replaces what was there, rather than adding to it.
func TestSummaryIsReplaced(t *testing.T) {
	r := newRepository(t)
	ctx := context.Background()
	id := storedConversation(t, r)

	first := conversation.Summary{Text: "the first part", ThroughSeq: 20}
	if err := r.SetSummary(ctx, id, first); err != nil {
		t.Fatalf("SetSummary: %v", err)
	}
	second := conversation.Summary{Text: "the first and second parts", ThroughSeq: 60}
	if err := r.SetSummary(ctx, id, second); err != nil {
		t.Fatalf("SetSummary: %v", err)
	}

	got, err := r.Summary(ctx, id)
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if got != second {
		t.Errorf("Summary = %+v, want %+v", got, second)
	}
}

// Writing the same summary twice is not mistaken for a conversation that is not
// there. MySQL counts rows it changed, not rows it matched.
func TestSummaryWrittenTwiceIsNotAMissingConversation(t *testing.T) {
	r := newRepository(t)
	ctx := context.Background()
	id := storedConversation(t, r)

	same := conversation.Summary{Text: "unchanged", ThroughSeq: 7}
	if err := r.SetSummary(ctx, id, same); err != nil {
		t.Fatalf("first SetSummary: %v", err)
	}
	if err := r.SetSummary(ctx, id, same); err != nil {
		t.Errorf("second SetSummary: %v", err)
	}
}

// A conversation that does not exist is an error rather than a silent no-op.
func TestSummaryOfAMissingConversation(t *testing.T) {
	r := newRepository(t)
	ctx := context.Background()

	if _, err := r.Summary(ctx, chat.NewConversationID()); !errors.Is(err, conversation.ErrNoConversation) {
		t.Errorf("Summary error = %v, want ErrNoConversation", err)
	}
	err := r.SetSummary(ctx, chat.NewConversationID(), conversation.Summary{Text: "x", ThroughSeq: 1})
	if !errors.Is(err, conversation.ErrNoConversation) {
		t.Errorf("SetSummary error = %v, want ErrNoConversation", err)
	}
}
