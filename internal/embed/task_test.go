package embed

import (
	"context"
	"strings"
	"testing"
)

// recorder is an Embedder that implements no Prefixer, which is the case every
// unmeasured model falls into.
type recorder struct{ seen []string }

func (r *recorder) Model() string { return "unmeasured" }
func (r *recorder) Dim() int      { return 4 }
func (r *recorder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	r.seen = append(r.seen, texts...)
	out := make([][]float32, len(texts))
	for i := range out {
		out[i] = []float32{1, 0, 0, 0}
	}
	return out, nil
}

func TestBothSidesAreFramedForAModelThatWantsIt(t *testing.T) {
	// The failure this guards against is asymmetric and silent: prefixing only
	// the query, or only the documents, ranks worse than prefixing neither, and
	// nothing errors.
	f := &Fake{Name: DefaultModel, Dimension: 8}
	if _, err := Vectors(context.Background(), f, Document, []string{"the billing csv landed twice"}); err != nil {
		t.Fatal(err)
	}
	if _, err := Vectors(context.Background(), f, Query, []string{"duplicate imports"}); err != nil {
		t.Fatal(err)
	}
	if len(f.Seen) != 2 {
		t.Fatalf("model saw %d texts, want 2", len(f.Seen))
	}
	if !strings.HasPrefix(f.Seen[0], "search_document: ") {
		t.Errorf("document reached the model as %q", f.Seen[0])
	}
	if !strings.HasPrefix(f.Seen[1], "search_query: ") {
		t.Errorf("query reached the model as %q", f.Seen[1])
	}
}

func TestAModelWithNoStatedPrefixesGetsNone(t *testing.T) {
	// Two ways to be unmeasured: a named model with no entry in the traits
	// table, and an Embedder that does not implement Prefixer at all. Neither
	// may have two tokens of somebody else's convention bolted on.
	f := &Fake{Name: "bge-small-en-v1.5", Dimension: 8}
	if _, err := Vectors(context.Background(), f, Query, []string{"duplicate imports"}); err != nil {
		t.Fatal(err)
	}
	if f.Seen[0] != "duplicate imports" {
		t.Errorf("unmeasured model saw %q, want the text unchanged", f.Seen[0])
	}

	r := &recorder{}
	if _, err := Vectors(context.Background(), r, Document, []string{"the billing csv landed twice"}); err != nil {
		t.Fatal(err)
	}
	if r.seen[0] != "the billing csv landed twice" {
		t.Errorf("a non-Prefixer saw %q, want the text unchanged", r.seen[0])
	}
}

func TestTraitsIgnoreAnOllamaTag(t *testing.T) {
	// A user who pinned a tag has the same weights and must not silently lose
	// the framing the model was trained with.
	tagged := TraitsFor(DefaultModel + ":latest")
	if tagged != TraitsFor(DefaultModel) {
		t.Errorf("%s:latest resolved to %+v, want the same traits as the untagged name",
			DefaultModel, tagged)
	}
	if got := TraitsFor("something-nobody-has-measured"); got != (Traits{}) {
		t.Errorf("unmeasured model got %+v, want no prefixes and no floor", got)
	}
}

func TestTheCalibratedFloorSitsBetweenNoiseAndSignal(t *testing.T) {
	// Measured on the real corpus with this model: 30 results for queries the
	// corpus cannot answer topped out at 0.570, and the lowest cosine of a
	// judged-correct result was 0.573. A floor outside that band is either
	// letting absurd queries through or cutting real answers, and both are
	// regressions no other test would catch.
	got := TraitsFor(DefaultModel).MinSimilarity
	if got <= 0.57 || got >= 0.65 {
		t.Errorf("floor for %s is %.2f, outside the measured band (0.57, 0.65)", DefaultModel, got)
	}
}
