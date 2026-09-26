package embed_test

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/DhanushRamesh/personal-assistant/internal/embed"
)

// Identical text scores 1, and nothing scores higher than identical text.
func TestTheSameTextIsPerfectlyAlike(t *testing.T) {
	f := embed.Fake{}
	a, _ := f.Query(context.Background(), "the roofer quoted forty thousand")
	b, _ := f.Query(context.Background(), "the roofer quoted forty thousand")

	if got := embed.Similarity(a, b); math.Abs(got-1) > 1e-6 {
		t.Errorf("similarity = %v, want 1", got)
	}
}

// Sharing words scores higher than sharing none. The Fake understands no
// meaning, so this is all it can promise.
func TestSharedWordsScoreHigher(t *testing.T) {
	f := embed.Fake{}
	ctx := context.Background()

	roof, _ := f.Query(ctx, "the roofer quoted forty thousand rupees")
	same, _ := f.Query(ctx, "the roofer quoted a price")
	other, _ := f.Query(ctx, "who captained India in the world cup")

	if embed.Similarity(roof, same) <= embed.Similarity(roof, other) {
		t.Error("unrelated text scored at least as high as related text")
	}
}

// Vectors from two models are different lengths, and comparing them would
// otherwise produce a number that looks like a score.
func TestDifferentLengthsAreNotComparable(t *testing.T) {
	if got := embed.Similarity(embed.Vector{1, 0}, embed.Vector{1, 0, 0}); got != 0 {
		t.Errorf("similarity = %v, want 0", got)
	}
	if got := embed.Similarity(nil, embed.Vector{1}); got != 0 {
		t.Errorf("similarity = %v, want 0", got)
	}
}

// A zero vector has no direction, so it is alike to nothing.
func TestAnEmptyVectorScoresZero(t *testing.T) {
	if got := embed.Similarity(embed.Vector{0, 0}, embed.Vector{1, 0}); got != 0 {
		t.Errorf("similarity = %v, want 0", got)
	}
}

// Normalize makes a vector unit length, so similarity is a dot product.
func TestNormalizeGivesUnitLength(t *testing.T) {
	v := embed.Normalize(embed.Vector{3, 4})

	var n float64
	for _, x := range v {
		n += float64(x) * float64(x)
	}
	if math.Abs(math.Sqrt(n)-1) > 1e-6 {
		t.Errorf("length = %v, want 1", math.Sqrt(n))
	}
}

// Documents returns one vector per input, in order.
func TestDocumentsAnswersInOrder(t *testing.T) {
	f := embed.Fake{}
	ctx := context.Background()

	texts := []string{"roof quotes", "cricket scores", "shopping list"}
	got, err := f.Documents(ctx, texts)
	if err != nil {
		t.Fatalf("Documents: %v", err)
	}
	if len(got) != len(texts) {
		t.Fatalf("got %d vectors for %d texts", len(got), len(texts))
	}

	for i, text := range texts {
		want, _ := f.Documents(ctx, []string{text})
		if embed.Similarity(got[i], want[0]) < 1-1e-6 {
			t.Errorf("vector %d is not the one for %q", i, text)
		}
	}
}

// The same text always gives the same vector, or a stored vector stops
// matching the text it was made from.
func TestTheFakeIsDeterministic(t *testing.T) {
	f := embed.Fake{}
	ctx := context.Background()

	first, _ := f.Documents(ctx, []string{"remember the roof quote"})
	second, _ := f.Documents(ctx, []string{"remember the roof quote"})

	if embed.Similarity(first[0], second[0]) < 1-1e-6 {
		t.Error("the same text gave two different vectors")
	}
}

// Off refuses rather than returning a vector that would compare as unrelated
// to everything.
func TestOffRefuses(t *testing.T) {
	var e embed.Embedder = embed.Off{}

	if e.Available() {
		t.Error("Off reports itself available")
	}
	if _, err := e.Query(context.Background(), "anything"); !errors.Is(err, embed.ErrUnavailable) {
		t.Errorf("error = %v, want ErrUnavailable", err)
	}
	if _, err := e.Documents(context.Background(), []string{"anything"}); !errors.Is(err, embed.ErrUnavailable) {
		t.Errorf("error = %v, want ErrUnavailable", err)
	}
}
