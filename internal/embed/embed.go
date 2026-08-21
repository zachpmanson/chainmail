// Package embed turns text into vectors, and does the vector arithmetic the
// corpus needs to rank by them.
//
// The model runs in a separate process (ollama, over HTTP) rather than in this
// one. Every Go binding for a local model needs CGO, and CGO fights the pure-Go
// modernc.org/sqlite driver the corpus is built on; a hosted API is out because
// the input is personal mail. The cost of the split is an extra daemon a user
// has to have running, which is why Embedder is an interface and ErrDaemonDown
// exists: "the model is not there" must be reportable as itself, never as an
// empty result set.
package embed

import (
	"context"
	"errors"
	"fmt"
	"math"
)

// Sentinel conditions a caller has to be able to tell apart. A search that
// returns nothing because the daemon is down is a different answer from a
// search that returns nothing because the corpus holds nothing matching, and
// collapsing the two makes a broken setup look like a working one.
var (
	// ErrDaemonDown means nothing is listening: ollama is not running.
	ErrDaemonDown = errors.New("no embedding daemon reachable")
	// ErrModelMissing means the daemon is up but has never pulled the model.
	ErrModelMissing = errors.New("embedding model not pulled")
	// ErrDimMismatch means two vectors, or a vector and its column, disagree
	// on length. Always an error: a shorter loop over a longer vector returns a
	// plausible number computed from the wrong data.
	ErrDimMismatch = errors.New("vector dimension mismatch")
	// ErrZeroVector means a vector has no direction, so it has no cosine with
	// anything. Reached by embedding text with no content.
	ErrZeroVector = errors.New("vector has zero magnitude")
)

// Embedder produces vectors for text.
//
// Model and Dim are part of the interface because they are stored beside every
// vector: a corpus holding vectors from two models, or from two dimensions of
// one model, returns confident nonsense, and the only way to notice is to have
// written down what produced each row.
type Embedder interface {
	// Model names the model, exactly as it will be recorded in the corpus.
	Model() string
	// Dim is the vector length the model is expected to return. An
	// implementation that discovers a different length must fail rather than
	// adopt it — a model swapped behind an unchanged name is precisely the
	// silent corruption Dim is here to catch.
	Dim() int
	// Embed returns one vector per input text, in order, each L2-normalised.
	// A partial result is an error: a caller cannot tell which input a short
	// slice belongs to.
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

// Normalise scales v to unit length in place, so that a cosine between two
// stored vectors is a plain dot product. Doing it once at write time rather
// than twice per comparison is the difference between three passes over every
// stored vector per query and one.
func Normalise(v []float32) error {
	var sum float64
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	if sum == 0 {
		return ErrZeroVector
	}
	inv := float32(1 / math.Sqrt(sum))
	for i := range v {
		v[i] *= inv
	}
	return nil
}

// Cosine is the cosine similarity of a and b, in [-1, 1]. It normalises as it
// goes, so it is correct for unnormalised input and merely redundant for
// normalised input; Dot is the fast path for vectors already known to be unit
// length.
func Cosine(a, b []float32) (float64, error) {
	if len(a) != len(b) {
		return 0, fmt.Errorf("%w: %d vs %d", ErrDimMismatch, len(a), len(b))
	}
	var dot, na, nb float64
	for i := range a {
		x, y := float64(a[i]), float64(b[i])
		dot += x * y
		na += x * x
		nb += y * y
	}
	if na == 0 || nb == 0 {
		return 0, ErrZeroVector
	}
	c := dot / (math.Sqrt(na) * math.Sqrt(nb))
	// Accumulated rounding can push an identical pair a hair past 1, which then
	// escapes as a similarity no caller's range check expects.
	return math.Max(-1, math.Min(1, c)), nil
}

// Dot is the cosine of two vectors that are already unit length. The dimension
// check is not optional: it is the only thing standing between a stored vector
// of the wrong width and a similarity computed over the prefix it shares.
func Dot(a, b []float32) (float64, error) {
	if len(a) != len(b) {
		return 0, fmt.Errorf("%w: %d vs %d", ErrDimMismatch, len(a), len(b))
	}
	var dot float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
	}
	return dot, nil
}
