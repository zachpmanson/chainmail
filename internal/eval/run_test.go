package eval

import (
	"context"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/zachpmanson/chainmail/internal/corpus"
	"github.com/zachpmanson/chainmail/internal/embed"
)

// Everything here is invented. The corpus this harness measures holds real
// personal correspondence, so the committed set and the corpus it is scored
// against are both written for the test: example.com addresses, invented topics.

const dim = 8

// syntheticCorpus builds the corpus fixtures/eval.synthetic.json judges.
//
// The vectors are pinned rather than derived, so the geometry is stated: the
// answers sit close to their query, the distractors are orthogonal to
// everything, and the absurd query is orthogonal to the whole corpus. A hashed
// bag of words could not express "these two texts mean the same thing in
// different words", which is the case the set exists to test.
func syntheticCorpus(t *testing.T) (*corpus.Store, *embed.Fake) {
	t.Helper()
	s, err := corpus.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	f := &embed.Fake{Name: "fake-" + string(rune('0'+dim)), Dimension: dim,
		Texts: map[string][]float32{}}

	// Direction 0 is "loading the same data twice", direction 2 is "what we
	// charge", direction 4 is an unrelated topic. Nothing occupies direction 6,
	// which is where the absurd query points.
	entries := []struct {
		id, subject, body string
		vec               []float32
	}{
		{"gap-on@example.com", "Nightly csv",
			"The billing file from Tuesday landed twice and both copies were loaded.",
			embed.Mix(dim, 0, 1, 0.15)},
		{"fee-a@example.com", "Membership billing",
			"We add five percent to every bill that goes out under the buying group.",
			embed.Mix(dim, 2, 3, 0.10)},
		{"fee-b@example.com", "Savings split",
			"That total is before our five percent comes off, so the client keeps the rest.",
			embed.Mix(dim, 2, 3, 0.20)},
		{"lunch@example.com", "Team lunch",
			"Booking the back room at the usual place for Friday, twelve of us.",
			embed.Unit(dim, 4, 1)},
	}
	for _, e := range entries {
		if _, err := s.Put(corpus.Entry{Source: corpus.SourceMail, ExtID: "mail:" + e.id,
			TS:      time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC),
			Subject: e.subject, BodyText: e.body},
			&corpus.Mail{MessageID: e.id, From: "someone@example.com"}, nil); err != nil {
			t.Fatal(err)
		}
		text, reason := corpus.EmbedTextFor(corpus.SourceMail, e.subject, e.body)
		if reason != "" {
			t.Fatalf("fixture %q was skipped as %s", e.subject, reason)
		}
		f.Texts[text] = e.vec
	}
	// The queries the judged set asks, placed by hand.
	f.Texts["duplicate imports"] = embed.Unit(dim, 0, 1)
	f.Texts["the fee we add to their bills"] = embed.Unit(dim, 2, 1)
	f.Texts["medieval falconry techniques"] = embed.Unit(dim, 6, 1)

	rep, err := s.BackfillEmbeddings(context.Background(), f, corpus.BackfillOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Embedded != len(entries) {
		t.Fatalf("backfill embedded %d of %d entries", rep.Embedded, len(entries))
	}
	return s, f
}

func TestTheHarnessScoresTheCommittedSet(t *testing.T) {
	set, err := LoadSet("../../fixtures/eval.synthetic.json")
	if err != nil {
		t.Fatal(err)
	}
	s, f := syntheticCorpus(t)
	floor := 0.5
	tgt := &Target{Cfg: Config{Name: "semantic", Mode: "semantic", MinSim: &floor},
		Store: s, Model: f}

	rep, err := tgt.Run(context.Background(), set, nil)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Judged != 2 || rep.Negative != 1 {
		t.Fatalf("scored %d judged and %d negative cases, want 2 and 1", rep.Judged, rep.Negative)
	}
	// Both judged queries have every answer at the top, so the ordering metrics
	// are at their ceiling and recall is limited only by the cutoff.
	if math.Abs(rep.MRR-1) > 1e-12 {
		t.Errorf("MRR = %v, want 1: both answers rank first", rep.MRR)
	}
	if math.Abs(rep.NDCG[5]-1) > 1e-12 {
		t.Errorf("ndcg@5 = %v, want 1", rep.NDCG[5])
	}
	if math.Abs(rep.Recall[10]-1) > 1e-12 {
		t.Errorf("recall@10 = %v, want 1", rep.Recall[10])
	}
	// The floor is what makes the absurd query come back empty; without it the
	// orthogonal query would still return four results at a cosine of zero.
	if rep.Clean != 1 || rep.Leaked != 0 {
		t.Errorf("the absurd query returned %d results; want none", rep.Leaked)
	}
}

func TestAFloorlessConfigurationLetsTheAbsurdQueryThrough(t *testing.T) {
	// The negative case has to be able to fail, or its passing says nothing.
	set, err := LoadSet("../../fixtures/eval.synthetic.json")
	if err != nil {
		t.Fatal(err)
	}
	s, f := syntheticCorpus(t)
	none := 0.0
	tgt := &Target{Cfg: Config{Name: "nofloor", Mode: "semantic", MinSim: &none},
		Store: s, Model: f}
	rep, err := tgt.Run(context.Background(), set, nil)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Leaked == 0 {
		t.Error("with no floor the absurd query returned nothing, so the floor is not what is being measured")
	}
}

func TestOneConfigurationCanBeScoredAgainstAnother(t *testing.T) {
	// The point of the harness: a delta. Lexical retrieval cannot reach the
	// vocabulary-gap answer at all, which is the gap the vector ranking exists
	// to close, so the two configurations must not score the same.
	set, err := LoadSet("../../fixtures/eval.synthetic.json")
	if err != nil {
		t.Fatal(err)
	}
	s, f := syntheticCorpus(t)
	floor := 0.5
	lexical, err := (&Target{Cfg: Config{Name: "lexical", Mode: "lexical"}, Store: s}).
		Run(context.Background(), set, nil)
	if err != nil {
		t.Fatal(err)
	}
	semantic, err := (&Target{Cfg: Config{Name: "semantic", Mode: "semantic", MinSim: &floor},
		Store: s, Model: f}).Run(context.Background(), set, nil)
	if err != nil {
		t.Fatal(err)
	}
	if semantic.MRR <= lexical.MRR {
		t.Errorf("semantic MRR %v did not beat lexical %v on a set built around the vocabulary gap",
			semantic.MRR, lexical.MRR)
	}
	var out strings.Builder
	Compare(&out, lexical, semantic)
	if !strings.Contains(out.String(), "MRR") {
		t.Errorf("the comparison table does not mention MRR:\n%s", out.String())
	}
}

func TestASemanticConfigurationWithoutAModelIsAnErrorNotAnEmptyResult(t *testing.T) {
	set, err := LoadSet("../../fixtures/eval.synthetic.json")
	if err != nil {
		t.Fatal(err)
	}
	s, _ := syntheticCorpus(t)
	_, err = (&Target{Cfg: Config{Name: "broken", Mode: "semantic"}, Store: s}).
		Run(context.Background(), set, nil)
	if err == nil {
		t.Fatal("scoring a semantic configuration with no model succeeded")
	}
}
