package memory_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/DhanushRamesh/personal-assistant/internal/embed"
	"github.com/DhanushRamesh/personal-assistant/internal/memory"
	"github.com/DhanushRamesh/personal-assistant/internal/memory/inmemory"
)

// store : A store holding the given memories, all embedded.
func store(t *testing.T, e embed.Embedder, facts ...[2]string) (*inmemory.Store, *memory.Recall) {
	t.Helper()

	s := inmemory.New()
	r := &memory.Recall{Store: s, Embedder: e}

	for _, f := range facts {
		m, err := memory.New("usr_1", memory.TierRecall, f[0], f[1])
		if err != nil {
			t.Fatalf("New(%q): %v", f[0], err)
		}
		if err := s.Create(context.Background(), m); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}
	if _, err := r.Embed(context.Background(), 100); err != nil {
		t.Fatalf("Embed: %v", err)
	}
	return s, r
}

// The nearest memories come back, nearest first.
func TestTheNearestMemoriesComeBack(t *testing.T) {
	_, r := store(t, embed.Fake{},
		[2]string{"Roof quote", "the roofer quoted forty thousand rupees"},
		[2]string{"Birthday", "22 October 1999"},
		[2]string{"Cricket", "follows Kapil Dev and MS Dhoni"},
	)

	got, err := r.For(context.Background(), "usr_1", "what did the roofer quote")
	if err != nil {
		t.Fatalf("For: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("nothing came back")
	}
	if !strings.Contains(got[0].Memory.Body, "roofer") {
		t.Errorf("nearest was %q, want the roof memory", got[0].Memory.Subject)
	}
	for i := 1; i < len(got); i++ {
		if got[i].Score > got[i-1].Score {
			t.Error("results are not nearest first")
		}
	}
}

// Only the asker's memories are searched.
func TestAnotherPersonsMemoriesAreNotFound(t *testing.T) {
	s, r := store(t, embed.Fake{}, [2]string{"Roof quote", "forty thousand rupees"})

	mine, _ := memory.New("usr_2", memory.TierRecall, "Roof quote", "forty thousand rupees")
	if err := s.Create(context.Background(), mine); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := r.For(context.Background(), "usr_1", "what did the roofer quote")
	if err != nil {
		t.Fatalf("For: %v", err)
	}
	for _, m := range got {
		if m.Memory.UserID != "usr_1" {
			t.Errorf("found %s's memory", m.Memory.UserID)
		}
	}
}

// Nothing stored still returns nothing, rather than failing.
func TestAnEmptyStoreFindsNothing(t *testing.T) {
	_, r := store(t, embed.Fake{})

	got, err := r.For(context.Background(), "usr_1", "anything at all")
	if err != nil || len(got) != 0 {
		t.Errorf("For = %v, %v, want nothing", got, err)
	}
}

// broken : An Embedder that is available and always fails, which is what an
// embedding server being up but unwell looks like.
type broken struct{}

func (broken) Query(context.Context, string) (embed.Vector, error) {
	return nil, errors.New("no")
}
func (broken) Documents(context.Context, []string) ([]embed.Vector, error) {
	return nil, errors.New("no")
}
func (broken) Model() string   { return "broken" }
func (broken) Available() bool { return true }

// A failing embedding server falls back to words rather than going blind.
func TestAFailedEmbeddingFallsBackToWords(t *testing.T) {
	s := inmemory.New()
	m, _ := memory.New("usr_1", memory.TierRecall, "Roof quote", "the roofer quoted forty thousand")
	if err := s.Create(context.Background(), m); err != nil {
		t.Fatalf("Create: %v", err)
	}

	r := &memory.Recall{Store: s, Embedder: broken{}}
	got, err := r.For(context.Background(), "usr_1", "what did the roofer quote")
	if err != nil {
		t.Fatalf("For: %v", err)
	}
	if len(got) != 1 || !got[0].ByWords {
		t.Errorf("got %v, want one match found by words", got)
	}
}

// No embedder at all is the same story: words, not silence.
func TestNoEmbedderStillSearches(t *testing.T) {
	s := inmemory.New()
	m, _ := memory.New("usr_1", memory.TierRecall, "Roof quote", "the roofer quoted forty thousand")
	if err := s.Create(context.Background(), m); err != nil {
		t.Fatalf("Create: %v", err)
	}

	r := &memory.Recall{Store: s, Embedder: embed.Off{}}
	got, err := r.For(context.Background(), "usr_1", "what did the roofer quote")
	if err != nil {
		t.Fatalf("For: %v", err)
	}
	if len(got) != 1 || !got[0].ByWords {
		t.Errorf("got %v, want one match found by words", got)
	}
}

// No more candidates than asked for, since every one of them costs room in
// the prompt.
func TestOnlyAsManyCandidatesAsAsked(t *testing.T) {
	facts := make([][2]string, 0, 10)
	for _, w := range []string{"one", "two", "three", "four", "five", "six", "seven", "eight", "nine", "ten"} {
		facts = append(facts, [2]string{"Number " + w, "a note about " + w})
	}
	_, r := store(t, embed.Fake{}, facts...)
	r.Candidates = 2

	got, err := r.For(context.Background(), "usr_1", "a note about three")
	if err != nil {
		t.Fatalf("For: %v", err)
	}
	if len(got) > 2 {
		t.Errorf("got %d candidates, want at most 2", len(got))
	}
}

// The always tier stops at its count, since it is in every prompt.
func TestTheAlwaysTierIsCappedByCount(t *testing.T) {
	s := inmemory.New()
	for i := 0; i < 10; i++ {
		m, _ := memory.New("usr_1", memory.TierAlways, "Fact", "something short")
		if err := s.Create(context.Background(), m); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}

	r := &memory.Recall{Store: s, AlwaysCap: 3}
	got, err := r.Always(context.Background(), "usr_1")
	if err != nil {
		t.Fatalf("Always: %v", err)
	}
	if len(got) != 3 {
		t.Errorf("got %d, want the cap of 3", len(got))
	}
}

// And at its size, because one long memory costs as much as several short
// ones.
func TestTheAlwaysTierIsCappedByBytes(t *testing.T) {
	s := inmemory.New()
	for i := 0; i < 5; i++ {
		m, _ := memory.New("usr_1", memory.TierAlways, "Fact", strings.Repeat("x", 100))
		if err := s.Create(context.Background(), m); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}

	r := &memory.Recall{Store: s, AlwaysBytes: 250}
	got, err := r.Always(context.Background(), "usr_1")
	if err != nil {
		t.Fatalf("Always: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("got %d memories, want the 2 that fit in 250 bytes", len(got))
	}
}

// Recall reaches only the always tier for the prompt.
func TestAlwaysDoesNotReachTheRecallTier(t *testing.T) {
	s := inmemory.New()
	kept, _ := memory.New("usr_1", memory.TierAlways, "Name", "Dhanush")
	other, _ := memory.New("usr_1", memory.TierRecall, "Roof", "forty thousand")
	for _, m := range []*memory.Memory{kept, other} {
		if err := s.Create(context.Background(), m); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}

	r := &memory.Recall{Store: s}
	got, err := r.Always(context.Background(), "usr_1")
	if err != nil {
		t.Fatalf("Always: %v", err)
	}
	if len(got) != 1 || got[0].Tier != memory.TierAlways {
		t.Errorf("got %v, want only the always memory", got)
	}
}

// Embedding catches up whatever has no vector, so a memory written while the
// embedding server was away is not invisible for ever.
func TestEmbeddingCatchesUp(t *testing.T) {
	s := inmemory.New()
	m, _ := memory.New("usr_1", memory.TierRecall, "Roof quote", "forty thousand")
	if err := s.Create(context.Background(), m); err != nil {
		t.Fatalf("Create: %v", err)
	}

	r := &memory.Recall{Store: s, Embedder: embed.Fake{}}
	done, err := r.Embed(context.Background(), 10)
	if err != nil || done != 1 {
		t.Fatalf("Embed = %d, %v, want 1", done, err)
	}

	again, err := r.Embed(context.Background(), 10)
	if err != nil || again != 0 {
		t.Errorf("Embed = %d, %v, want nothing left to do", again, err)
	}
}

// Changing a memory clears its vector, or it keeps matching what it used to
// say.
func TestUpdatingClearsTheVector(t *testing.T) {
	s, r := store(t, embed.Fake{}, [2]string{"Roof quote", "forty thousand"})

	all, _ := s.All(context.Background(), "usr_1", memory.TierRecall)
	m := all[0]
	if !m.Embedded("fake") {
		t.Fatal("the memory was never embedded")
	}

	m.Body = "fifty thousand"
	if err := s.Update(context.Background(), &m); err != nil {
		t.Fatalf("Update: %v", err)
	}

	after, err := s.Get(context.Background(), "usr_1", m.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if after.Embedded("fake") {
		t.Error("the old vector survived a change to the text it described")
	}

	if done, err := r.Embed(context.Background(), 10); err != nil || done != 1 {
		t.Errorf("Embed = %d, %v, want it re-embedded", done, err)
	}
}

// Nothing is searched for an empty question.
func TestAnEmptyQuestionSearchesNothing(t *testing.T) {
	_, r := store(t, embed.Fake{}, [2]string{"Roof quote", "forty thousand"})

	if got, err := r.For(context.Background(), "usr_1", "   "); err != nil || len(got) != 0 {
		t.Errorf("For = %v, %v, want nothing", got, err)
	}
}
