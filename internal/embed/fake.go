package embed

import (
	"context"
	"hash/fnv"
	"strings"
	"unicode"
)

// FakeDims : The length of a Fake's vectors.
const FakeDims = 64

// Fake : A deterministic Embedder for tests, needing no model and no network.
//
// Each word is hashed to a position, so the same text always gives the same
// vector and texts sharing words score higher than texts that do not. It
// understands no meaning at all: only tests should use it.
type Fake struct {
	// Dims : The length of the vectors it returns. Zero selects FakeDims.
	Dims int
	// Prefix : Added to a query before hashing, so a test can tell the query
	// path from the document path. Optional.
	Prefix string
}

// Query : Embeds text being searched with.
func (f Fake) Query(_ context.Context, text string) (Vector, error) {
	return f.vector(f.Prefix + text), nil
}

// Documents : Embeds text being stored.
func (f Fake) Documents(_ context.Context, texts []string) ([]Vector, error) {
	out := make([]Vector, 0, len(texts))
	for _, t := range texts {
		out = append(out, f.vector(t))
	}
	return out, nil
}

// Model : A fixed name, so rows written by it are recognisable.
func (Fake) Model() string { return "fake" }

// Available : Always true.
func (Fake) Available() bool { return true }

// vector : The bag of words of text, hashed into a fixed number of positions.
func (f Fake) vector(text string) Vector {
	dims := f.Dims
	if dims <= 0 {
		dims = FakeDims
	}

	v := make(Vector, dims)
	for _, word := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	}) {
		h := fnv.New32a()
		_, _ = h.Write([]byte(word))
		v[int(h.Sum32())%dims]++
	}
	return Normalize(v)
}
