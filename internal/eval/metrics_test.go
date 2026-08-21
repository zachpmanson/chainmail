package eval

import (
	"math"
	"testing"
)

// The arithmetic is worked by hand here rather than compared against a golden
// value produced by this same code. A metric that is quietly wrong makes every
// comparison built on it wrong in the same direction, which is worse than having
// no metric at all.
func TestMetricsMatchHandWorkedArithmetic(t *testing.T) {
	ranked := []string{"a", "b", "c", "d"}
	rel := map[string]bool{"b": true, "d": true}

	for _, c := range []struct {
		k    int
		want float64
	}{
		{1, 0},   // a only, no hit
		{2, 0.5}, // b found, one of two
		{3, 0.5},
		{4, 1},
	} {
		if got := RecallAt(ranked, rel, c.k); got != c.want {
			t.Errorf("recall@%d = %v, want %v", c.k, got, c.want)
		}
	}

	if got := FirstRelevantRank(ranked, rel); got != 2 {
		t.Errorf("first relevant rank = %d, want 2", got)
	}
	if got := ReciprocalRank(ranked, rel); got != 0.5 {
		t.Errorf("reciprocal rank = %v, want 0.5", got)
	}

	// nDCG@4: gains at ranks 2 and 4, so
	//   DCG   = 1/log2(3) + 1/log2(5) = 0.630930 + 0.430677 = 1.061607
	//   ideal = 1/log2(2) + 1/log2(3) = 1        + 0.630930 = 1.630930
	//   nDCG  = 0.650921
	if got := NDCGAt(ranked, rel, 4); math.Abs(got-0.650921) > 1e-6 {
		t.Errorf("ndcg@4 = %v, want 0.650921", got)
	}
	// nDCG@2: one gain at rank 2 against an ideal that still assumes two
	// reachable positions, so 0.630930 / 1.630930 = 0.386853.
	if got := NDCGAt(ranked, rel, 2); math.Abs(got-0.386853) > 1e-6 {
		t.Errorf("ndcg@2 = %v, want 0.386853", got)
	}
	// A perfect ranking is exactly 1, which is the property that makes the
	// normalisation worth doing at all.
	if got := NDCGAt([]string{"b", "d", "a", "c"}, rel, 4); math.Abs(got-1) > 1e-12 {
		t.Errorf("ndcg@4 of a perfect ranking = %v, want 1", got)
	}
}

func TestIdealGainIsCappedAtTheCutoff(t *testing.T) {
	// Three relevant results and room for two: the ideal has to be the best
	// reachable at depth 2, or a query with many answers could never score 1
	// and would drag every average down for having been answered well.
	ranked := []string{"a", "b", "c"}
	rel := map[string]bool{"a": true, "b": true, "c": true}
	if got := NDCGAt(ranked, rel, 2); math.Abs(got-1) > 1e-12 {
		t.Errorf("ndcg@2 = %v, want 1", got)
	}
	if got := RecallAt(ranked, rel, 2); math.Abs(got-2.0/3) > 1e-12 {
		t.Errorf("recall@2 = %v, want 2/3", got)
	}
}

func TestAMissedQueryScoresZeroRatherThanBeingSkipped(t *testing.T) {
	ranked := []string{"x", "y"}
	rel := map[string]bool{"b": true}
	if got := ReciprocalRank(ranked, rel); got != 0 {
		t.Errorf("reciprocal rank of a miss = %v, want 0", got)
	}
	if got := NDCGAt(ranked, rel, 2); got != 0 {
		t.Errorf("ndcg of a miss = %v, want 0", got)
	}
	if got := FirstRelevantRank(ranked, rel); got != 0 {
		t.Errorf("first rank of a miss = %d, want 0", got)
	}
}

func TestAQueryWithNoJudgementsScoresNothing(t *testing.T) {
	// A negative case must not be averaged in as a recall of zero: it is a
	// different question, counted separately, and folding it in here would make
	// every aggregate a function of how many absurd queries the set holds.
	if got := RecallAt([]string{"a"}, nil, 5); got != 0 {
		t.Errorf("recall with no judgements = %v, want 0", got)
	}
	if got := NDCGAt([]string{"a"}, nil, 5); got != 0 {
		t.Errorf("ndcg with no judgements = %v, want 0", got)
	}
}

func TestAggregatesAreMacroAveraged(t *testing.T) {
	if got := mean([]float64{1, 0, 0.5}); math.Abs(got-0.5) > 1e-12 {
		t.Errorf("mean = %v, want 0.5", got)
	}
	if got := mean(nil); got != 0 {
		t.Errorf("mean of nothing = %v, want 0", got)
	}
}
