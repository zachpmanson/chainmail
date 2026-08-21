package embed

import (
	"context"
	"fmt"
	"hash/fnv"
	"math"
	"strings"
)

// Fake is an Embedder that needs no daemon and no network.
//
// It is not a test double bolted on afterwards: nothing in this repository can
// assume ollama is installed, so every test of storage, staleness, backfill and
// fusion has to run against a model that is pure arithmetic. It lives in the
// non-test file so the CLI can use it too, which is the only way to exercise
// the wiring end to end on a machine with no model at all.
//
// Vectors are a hashed bag of words: each token is hashed to a coordinate and
// summed, then normalised. That makes similarity track word overlap, which is
// not what a real model does, but it is deterministic and it is enough to
// assert that a ranking is ordered by similarity at all. Where a test needs two
// texts to be alike without sharing words — the vocabulary gap that semantic
// search exists for — it pins them in Texts.
type Fake struct {
	Name      string
	Dimension int
	// Texts pins exact texts to exact vectors. A pinned vector is used as
	// given, after normalisation, so a test can state the geometry it means.
	Texts map[string][]float32
	// Fail, when non-nil, is returned instead of any vector. Used to interrupt
	// a backfill at a chosen point.
	Fail error
	// Calls counts texts vectorised, so a test can assert that a resumed
	// backfill did not redo finished work.
	Calls int
	// Seen records every text handed to the model, in order.
	Seen []string
}

// NewFake returns a fake with a stated dimension. Small dimensions are fine and
// preferable in tests: nothing here depends on the width.
func NewFake(dim int) *Fake {
	return &Fake{Name: "fake-" + fmt.Sprint(dim), Dimension: dim}
}

func (f *Fake) Model() string {
	if f.Name == "" {
		return "fake"
	}
	return f.Name
}

func (f *Fake) Dim() int { return f.Dimension }

func (f *Fake) Embed(_ context.Context, texts []string) ([][]float32, error) {
	if f.Fail != nil {
		return nil, f.Fail
	}
	out := make([][]float32, 0, len(texts))
	for _, t := range texts {
		f.Calls++
		f.Seen = append(f.Seen, t)
		v, err := f.vector(t)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

func (f *Fake) vector(text string) ([]float32, error) {
	if pinned, ok := f.Texts[text]; ok {
		v := make([]float32, len(pinned))
		copy(v, pinned)
		if len(v) != f.Dimension {
			return nil, fmt.Errorf("%w: pinned vector for %q is %d wide, want %d",
				ErrDimMismatch, text, len(v), f.Dimension)
		}
		if err := Normalise(v); err != nil {
			return nil, err
		}
		return v, nil
	}
	v := make([]float32, f.Dimension)
	for _, w := range strings.Fields(strings.ToLower(text)) {
		h := fnv.New32a()
		h.Write([]byte(w))
		sum := h.Sum32()
		i := int(sum % uint32(f.Dimension))
		// The sign comes from a different bit than the coordinate so two words
		// landing on one coordinate do not always reinforce each other.
		if sum&0x80000000 != 0 {
			v[i] -= 1
		} else {
			v[i] += 1
		}
	}
	if err := Normalise(v); err != nil {
		// Text with no words has no direction. Callers gate on content before
		// reaching a model, so this is a signal that the gate was skipped.
		return nil, fmt.Errorf("fake embedding %q: %w", text, err)
	}
	return v, nil
}

// Unit returns the i-th basis vector of width dim, for tests that want two
// texts to be exactly orthogonal or exactly opposite.
func Unit(dim, i int, sign float32) []float32 {
	v := make([]float32, dim)
	v[((i%dim)+dim)%dim] = sign
	return v
}

// Mix returns a unit vector at angle theta between basis vectors i and j, so a
// test can state "these two texts are 0.8 similar" rather than discovering it.
func Mix(dim, i, j int, theta float64) []float32 {
	v := make([]float32, dim)
	v[i%dim] = float32(math.Cos(theta))
	v[j%dim] = float32(math.Sin(theta))
	return v
}
