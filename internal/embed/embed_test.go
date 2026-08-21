package embed

import (
	"context"
	"errors"
	"math"
	"testing"
)

func TestCosineAtTheThreeAnglesThatMatter(t *testing.T) {
	// Identical, orthogonal and opposite are the only three values a reader can
	// verify by hand, so they are what a similarity is pinned against.
	cases := []struct {
		name string
		a, b []float32
		want float64
	}{
		{"identical", []float32{0.6, 0.8}, []float32{0.6, 0.8}, 1},
		{"identical unnormalised", []float32{3, 4}, []float32{30, 40}, 1},
		{"orthogonal", []float32{1, 0}, []float32{0, 1}, 0},
		{"opposite", []float32{0.6, 0.8}, []float32{-0.6, -0.8}, -1},
		{"half turn", []float32{1, 0}, []float32{1, 1}, math.Sqrt2 / 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := Cosine(c.a, c.b)
			if err != nil {
				t.Fatal(err)
			}
			if math.Abs(got-c.want) > 1e-6 {
				t.Errorf("Cosine = %v, want %v", got, c.want)
			}
		})
	}
}

// An identical pair must not report a similarity above 1: a caller that range
// checks, or renders a percentage, has no reading of 1.0000001 available to it.
func TestCosineNeverExceedsOne(t *testing.T) {
	v := make([]float32, 768)
	for i := range v {
		v[i] = float32(i%17) + 0.3
	}
	got, err := Cosine(v, v)
	if err != nil {
		t.Fatal(err)
	}
	if got > 1 {
		t.Errorf("Cosine of a vector with itself = %v, want <= 1", got)
	}
}

func TestDimensionMismatchIsAnErrorNotAPrefix(t *testing.T) {
	// The dangerous alternative is looping over the shorter of the two, which
	// returns a number that looks like a similarity and is computed from a
	// prefix. Both entry points must refuse.
	short, long := []float32{1, 0}, []float32{1, 0, 0}
	if _, err := Cosine(short, long); !errors.Is(err, ErrDimMismatch) {
		t.Errorf("Cosine on mismatched widths: err = %v, want ErrDimMismatch", err)
	}
	if _, err := Dot(short, long); !errors.Is(err, ErrDimMismatch) {
		t.Errorf("Dot on mismatched widths: err = %v, want ErrDimMismatch", err)
	}
}

func TestZeroVectorHasNoDirection(t *testing.T) {
	zero := []float32{0, 0, 0}
	if err := Normalise(zero); !errors.Is(err, ErrZeroVector) {
		t.Errorf("Normalise(zero): err = %v, want ErrZeroVector", err)
	}
	if _, err := Cosine(zero, []float32{1, 0, 0}); !errors.Is(err, ErrZeroVector) {
		t.Errorf("Cosine with a zero vector: err = %v, want ErrZeroVector", err)
	}
}

func TestNormaliseGivesUnitLength(t *testing.T) {
	v := []float32{3, 4, 12}
	if err := Normalise(v); err != nil {
		t.Fatal(err)
	}
	var sum float64
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	if math.Abs(sum-1) > 1e-6 {
		t.Errorf("squared length after Normalise = %v, want 1", sum)
	}
}

// Dot is only correct on unit vectors, and it is the path every stored
// comparison takes, so it has to agree with Cosine on normalised input.
func TestDotAgreesWithCosineOnUnitVectors(t *testing.T) {
	a, b := Mix(8, 0, 1, 0.3), Mix(8, 0, 1, 1.1)
	if err := Normalise(a); err != nil {
		t.Fatal(err)
	}
	if err := Normalise(b); err != nil {
		t.Fatal(err)
	}
	d, err := Dot(a, b)
	if err != nil {
		t.Fatal(err)
	}
	c, err := Cosine(a, b)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(d-c) > 1e-6 {
		t.Errorf("Dot = %v, Cosine = %v", d, c)
	}
}

func TestFakeReturnsUnitVectorsOfTheStatedWidth(t *testing.T) {
	f := NewFake(16)
	vs, err := f.Embed(context.Background(), []string{"billing csv ingestion", "unrelated words"})
	if err != nil {
		t.Fatal(err)
	}
	if len(vs) != 2 {
		t.Fatalf("got %d vectors, want 2", len(vs))
	}
	for i, v := range vs {
		if len(v) != f.Dim() {
			t.Errorf("vector %d is %d wide, want %d", i, len(v), f.Dim())
		}
		if c, err := Cosine(v, v); err != nil || math.Abs(c-1) > 1e-6 {
			t.Errorf("vector %d is not unit length: %v %v", i, c, err)
		}
	}
}

// A test that needs two texts to be alike without sharing a word — the
// vocabulary gap the whole feature exists for — pins their geometry.
func TestFakeHonoursPinnedVectors(t *testing.T) {
	f := &Fake{Name: "pinned", Dimension: 4, Texts: map[string][]float32{
		"ingestion": Unit(4, 0, 1),
		"csv":       Mix(4, 0, 1, 0.2),
		"lunch":     Unit(4, 1, 1),
	}}
	vs, err := f.Embed(context.Background(), []string{"ingestion", "csv", "lunch"})
	if err != nil {
		t.Fatal(err)
	}
	near, err := Cosine(vs[0], vs[1])
	if err != nil {
		t.Fatal(err)
	}
	far, err := Cosine(vs[0], vs[2])
	if err != nil {
		t.Fatal(err)
	}
	if !(near > far) {
		t.Errorf("pinned geometry ignored: ingestion~csv = %v, ingestion~lunch = %v", near, far)
	}
}
