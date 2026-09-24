package mysql_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DhanushRamesh/friday/internal/session"
)

func TestAppendNumbersMessagesInOrder(t *testing.T) {
	r := newRepository(t)
	ctx := context.Background()
	id := storedSession(t, r)

	at := time.Now().UTC().Truncate(time.Millisecond)
	for i, content := range []string{"what is the time", "half past two", "and the date"} {
		m, err := r.Append(ctx, session.Message{
			SessionID: id,
			Kind:      session.Chat,
			Role:      session.User,
			Content:   content,
			At:        at.Add(time.Duration(i) * time.Second),
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
	id := storedSession(t, r)

	for _, content := range []string{"first", "second", "third"} {
		if _, err := r.Append(ctx, session.Said(id, content, time.Now().UTC())); err != nil {
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
	id := storedSession(t, r)

	const writers = 5
	var wg sync.WaitGroup
	errs := make([]error, writers)

	wg.Add(writers)
	for i := 0; i < writers; i++ {
		go func(i int) {
			defer wg.Done()
			_, errs[i] = r.Append(ctx, session.Said(id, "message", time.Now().UTC()))
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

func TestAppendRefusesAMessageWithNoSession(t *testing.T) {
	r := newRepository(t)

	_, err := r.Append(context.Background(),
		session.Said("sess_00000000000000000000000000", "hello", time.Now().UTC()))
	if !errors.Is(err, session.ErrNoSession) {
		t.Errorf("err = %v, want ErrNoSession", err)
	}
}

func TestAppendRefusesAnEmptyMessage(t *testing.T) {
	r := newRepository(t)
	id := storedSession(t, r)

	if _, err := r.Append(context.Background(), session.Said(id, "   ", time.Now().UTC())); err == nil {
		t.Error("an empty message was stored")
	}
}

func TestAppendRefusesAMessageTooLarge(t *testing.T) {
	r := newRepository(t)
	id := storedSession(t, r)

	huge := strings.Repeat("a", (1<<20)+1)
	_, err := r.Append(context.Background(), session.Said(id, huge, time.Now().UTC()))
	if !errors.Is(err, session.ErrTooLarge) {
		t.Errorf("err = %v, want ErrTooLarge", err)
	}
}
