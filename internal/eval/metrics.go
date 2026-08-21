// Package eval measures retrieval quality, so that a change to indexing,
// ranking or embeddings can be shown to have helped rather than asserted to.
//
// Without it, every retrieval change is unfalsifiable: results look plausible
// either way, and the only available evidence is whether the person who made
// the change likes the top five.
//
// # Which metrics, and why
//
// Judgements here are binary — an entry either answers the query or it does
// not — because graded relevance over one's own mail is a judgement call that
// drifts between sittings, and a metric fed by drifting labels measures the
// labeller.
//
// Three numbers, each answering a different question:
//
//   - recall@k: did the answer surface at all within the first k? This is the
//     one that guards against a change that improves ordering by dropping
//     things, and the one a similarity floor can only hurt.
//   - MRR: how far down was the first true answer? It is the sharpest possible
//     statement of "finds the right neighbourhood, orders badly within it",
//     because it moves the moment a better answer overtakes a worse one and
//     ignores everything after the first hit.
//   - nDCG@k: MRR sees only the first relevant result, and most queries here
//     have several. nDCG@k credits every one of them, discounted by depth, and
//     is the metric to read when two configurations have the same recall and
//     the same first hit but disagree about everything below it.
//
// Precision is not reported as such. A query with three true answers in a
// corpus of thirty thousand has a precision@10 ceiling of 0.3, so the number
// would mostly measure how many answers the query happens to have. The negative
// cases carry the precision argument instead: a query nothing in the corpus
// answers must return nothing, and that is counted directly.
package eval

import "math"

// RecallAt is the share of an entry's known-relevant results appearing in the
// first k. Zero relevant results is not a recall of zero but a question that
// does not apply — the caller filters those out (they are the negative cases),
// and 0 here would silently drag an average down.
func RecallAt(ranked []string, relevant map[string]bool, k int) float64 {
	if len(relevant) == 0 {
		return 0
	}
	found := 0
	for _, id := range firstN(ranked, k) {
		if relevant[id] {
			found++
		}
	}
	return float64(found) / float64(len(relevant))
}

// FirstRelevantRank is the 1-based position of the first relevant result, or 0
// when none appears. Reported alongside the reciprocal because "rank 7" is
// legible and "0.14" is not.
func FirstRelevantRank(ranked []string, relevant map[string]bool) int {
	for i, id := range ranked {
		if relevant[id] {
			return i + 1
		}
	}
	return 0
}

// ReciprocalRank is 1/rank of the first relevant result, and 0 when the ranking
// contains none. Averaged over queries this is MRR.
func ReciprocalRank(ranked []string, relevant map[string]bool) float64 {
	if r := FirstRelevantRank(ranked, relevant); r > 0 {
		return 1 / float64(r)
	}
	return 0
}

// NDCGAt is discounted cumulative gain over the first k, normalised by the best
// achievable arrangement of the same judgements.
//
// Binary gains, log2 discount, and the ideal ranking is every relevant result
// packed into the top positions — which is why a query with more relevant
// results than k can still score 1.0: it is scored against what is reachable at
// depth k, not against an unreachable perfect list.
func NDCGAt(ranked []string, relevant map[string]bool, k int) float64 {
	if len(relevant) == 0 || k <= 0 {
		return 0
	}
	var dcg float64
	for i, id := range firstN(ranked, k) {
		if relevant[id] {
			dcg += 1 / math.Log2(float64(i)+2)
		}
	}
	var ideal float64
	for i := 0; i < min(k, len(relevant)); i++ {
		ideal += 1 / math.Log2(float64(i)+2)
	}
	if ideal == 0 {
		return 0
	}
	return dcg / ideal
}

func firstN(ids []string, k int) []string {
	if k <= 0 || k >= len(ids) {
		return ids
	}
	return ids[:k]
}

// mean is the average of xs, and 0 for none. Every aggregate here is a
// macro-average — one vote per query, regardless of how many relevant results
// that query has — so that a single query with fifteen judgements cannot
// outweigh ten queries with one.
func mean(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	var sum float64
	for _, x := range xs {
		sum += x
	}
	return sum / float64(len(xs))
}
