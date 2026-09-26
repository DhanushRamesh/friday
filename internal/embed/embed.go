// Package embed turns text into vectors so that meaning can be compared.
//
// Word matching finds a memory only when its wording is reused. Comparing
// vectors finds it when the same thing is asked a different way.
package embed

import (
	"context"
	"errors"
	"math"
)

// Vector : One embedding. Unit length, so similarity is a dot product.
type Vector []float32

// Embedder : A source of embeddings.
//
// Query and Documents are separate because a retrieval model embeds a
// question and the text that should answer it differently.
type Embedder interface {
	// Query : Embeds text that is being searched with.
	Query(ctx context.Context, text string) (Vector, error)

	// Documents : Embeds text that is being stored, in one call. The result
	// has one vector per input, in the same order.
	Documents(ctx context.Context, texts []string) ([]Vector, error)

	// Model : Which model produced the vectors. Stored beside them, so
	// vectors from different models are never compared.
	Model() string

	// Available : Whether embeddings can actually be produced.
	Available() bool
}

// ErrUnavailable : Returned when there is no embedder to produce vectors.
var ErrUnavailable = errors.New("embed: nothing configured to embed with")

// Similarity : How alike two vectors are, from -1 to 1.
//
// Zero when either is empty or they are different lengths, which is what
// vectors from two different models look like.
func Similarity(a, b Vector) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}

	var dot, na, nb float64
	for i := range a {
		x, y := float64(a[i]), float64(b[i])
		dot += x * y
		na += x * x
		nb += y * y
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

// Normalize : Scales a vector to unit length, in place.
func Normalize(v Vector) Vector {
	var n float64
	for _, x := range v {
		n += float64(x) * float64(x)
	}
	if n == 0 {
		return v
	}
	n = math.Sqrt(n)
	for i := range v {
		v[i] = float32(float64(v[i]) / n)
	}
	return v
}

// Off : An Embedder with no model behind it.
//
// Reports itself unavailable and refuses rather than returning vectors that
// would silently compare as unrelated.
type Off struct{}

// Query : Always fails with ErrUnavailable.
func (Off) Query(context.Context, string) (Vector, error) { return nil, ErrUnavailable }

// Documents : Always fails with ErrUnavailable.
func (Off) Documents(context.Context, []string) ([]Vector, error) { return nil, ErrUnavailable }

// Model : The empty string. Nothing produced anything.
func (Off) Model() string { return "" }

// Available : Always false.
func (Off) Available() bool { return false }
