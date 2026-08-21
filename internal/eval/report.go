package eval

import (
	"fmt"
	"io"
	"sort"
	"strings"
)

// Render writes one configuration's scores.
func Render(w io.Writer, r Report) {
	fmt.Fprintf(w, "%s — %s level, %d judged queries, %d negative\n",
		r.Cfg.Name, r.Level, r.Judged, r.Negative)
	for _, k := range r.Cutoffs {
		fmt.Fprintf(w, "  recall@%-2d %.3f    ndcg@%-2d %.3f\n", k, r.Recall[k], k, r.NDCG[k])
	}
	fmt.Fprintf(w, "  MRR       %.3f\n", r.MRR)
	if r.Negative > 0 {
		fmt.Fprintf(w, "  negative  %d/%d returned nothing, %d results leaked, worst cosine %.3f\n",
			r.Clean, r.Negative, r.Leaked, r.TopNoise)
	}
}

// Compare writes two configurations side by side with the delta between them,
// which is the only form these numbers are worth reading in.
func Compare(w io.Writer, a, b Report) {
	fmt.Fprintf(w, "%-14s  %10s  %10s  %9s\n", "metric", short(a.Cfg.Name), short(b.Cfg.Name), "delta")
	fmt.Fprintln(w, strings.Repeat("-", 50))
	for _, k := range a.Cutoffs {
		row(w, fmt.Sprintf("recall@%d", k), a.Recall[k], b.Recall[k])
	}
	row(w, "MRR", a.MRR, b.MRR)
	for _, k := range a.Cutoffs {
		row(w, fmt.Sprintf("ndcg@%d", k), a.NDCG[k], b.NDCG[k])
	}
	if a.Negative > 0 || b.Negative > 0 {
		row(w, "negative clean", float64(a.Clean), float64(b.Clean))
		row(w, "negative hits", float64(a.Leaked), float64(b.Leaked))
		row(w, "worst noise", a.TopNoise, b.TopNoise)
	}
}

// RenderCases writes the per-query detail: where the first true answer landed,
// and how much came back. This is what says *which* queries a change helped,
// which an aggregate cannot.
func RenderCases(w io.Writer, a, b Report) {
	fmt.Fprintf(w, "\n%-46s  %-13s  %-13s\n", "query", short(a.Cfg.Name), short(b.Cfg.Name))
	for i := range a.Cases {
		ac := a.Cases[i]
		bc := b.Cases[i]
		fmt.Fprintf(w, "%-46s  %-13s  %-13s\n",
			trunc(ac.Case.Query, 46), caseCell(ac), caseCell(bc))
		if ac.Case.Why != "" {
			fmt.Fprintf(w, "  %s\n", ac.Case.Why)
		}
	}
}

// caseCell states a case's outcome in the terms the case was written in: a
// negative case is judged by how much leaked, everything else by where its first
// true answer landed.
func caseCell(c CaseResult) string {
	if c.Case.ExpectEmpty {
		if c.Returned == 0 {
			return "clean"
		}
		return fmt.Sprintf("%d leaked", c.Returned)
	}
	if c.FirstRank == 0 {
		return fmt.Sprintf("missed (%d back)", c.Returned)
	}
	return fmt.Sprintf("first @%d  r@10 %.2f", c.FirstRank, c.Recall[10])
}

func row(w io.Writer, name string, a, b float64) {
	d := b - a
	sign := ""
	if d > 0 {
		sign = "+"
	}
	fmt.Fprintf(w, "%-14s  %10.3f  %10.3f  %9s\n", name, a, b, sign+fmt.Sprintf("%.3f", d))
}

func short(s string) string { return trunc(s, 10) }

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return s[:n]
	}
	return s[:n-1] + "…"
}

// FloorSweep is the measurement a similarity floor is chosen from: for a series
// of candidate cutoffs, what each one would have thrown away.
//
// The two columns pull in opposite directions by construction — every floor high
// enough to silence an absurd query is high enough to lose something true
// eventually — so the number to pick is where the noise column reaches zero and
// the signal column has not yet started to move.
func FloorSweep(w io.Writer, r Report, floors []float64) {
	if len(r.HitSims) == 0 && len(r.NoiseSims) == 0 {
		fmt.Fprintf(w, "%s: no cosines to calibrate (lexical)\n", r.Cfg.Name)
		return
	}
	fmt.Fprintf(w, "\n%s: %d true results found, %d results returned for queries with no answer\n",
		r.Cfg.Name, len(r.HitSims), len(r.NoiseSims))
	fmt.Fprintf(w, "  true results   min %.3f  p10 %.3f  median %.3f  max %.3f\n",
		quantile(r.HitSims, 0), quantile(r.HitSims, 0.10),
		quantile(r.HitSims, 0.50), quantile(r.HitSims, 1))
	fmt.Fprintf(w, "  no-answer      median %.3f  p90 %.3f  max %.3f\n",
		quantile(r.NoiseSims, 0.50), quantile(r.NoiseSims, 0.90), quantile(r.NoiseSims, 1))
	fmt.Fprintf(w, "\n  %-7s  %-22s  %s\n", "floor", "true results lost", "no-answer results left")
	for _, f := range floors {
		lost := countBelow(r.HitSims, f)
		left := len(r.NoiseSims) - countBelow(r.NoiseSims, f)
		fmt.Fprintf(w, "  %-7.2f  %3d of %-3d (%5.1f%%)      %3d of %-3d\n",
			f, lost, len(r.HitSims), 100*float64(lost)/float64(max(len(r.HitSims), 1)),
			left, len(r.NoiseSims))
	}
}

func countBelow(xs []float64, f float64) int {
	n := 0
	for _, x := range xs {
		if x < f {
			n++
		}
	}
	return n
}

// quantile is the p-th quantile of xs by nearest rank, which needs no
// interpolation and cannot invent a value that was never measured.
func quantile(xs []float64, p float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	sorted := append([]float64(nil), xs...)
	sort.Float64s(sorted)
	i := int(p * float64(len(sorted)-1))
	return sorted[i]
}
