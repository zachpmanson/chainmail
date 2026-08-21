package main

import (
	"fmt"
	"io"

	"github.com/zachpmanson/chainmail/internal/corpus"
)

// Expansion prints a ranked chain as the whole conversation rather than as the
// entries that matched.
//
// Measured on this corpus, one query recovers well under half of a real trail at
// entry level, while the chains holding that trail are found. The misses are the
// messages of a found conversation that do not happen to contain the query's
// words, so the recall worth optimising is chain recall, and the move that pays
// is to locate the conversation and then stop searching inside it.
//
// What that costs is volume, which is why expansion is opt-in and gated. The
// gate has three parts because there are three different ways expanding is the
// wrong answer:
//
//   - Top is the rank cutoff, and the load-bearing one. The chain score already
//     ranks by aggregate relevance with harmonic damping, so "the best few" is a
//     relevance judgement that has been made by the time this runs; a cutoff is
//     also the only gate that bounds output regardless of how the corpus is
//     shaped. Without it a broad query unfolds twenty conversations.
//   - MinMatched is a floor on corroboration. One incidental hit is the
//     signature of a passing mention — this corpus holds a 134-message channel
//     that a query hits twice — and unfolding a channel on that evidence buries
//     the answer it was supposed to surface.
//   - MinRatio is available and defaults to off. The ratio reads as the obvious
//     gate and is the wrong default: a thread genuinely about the topic has a low
//     ratio *because it is long*, which is precisely the chain expansion exists
//     to recover. It discriminates usefully only on a corpus of uniformly short
//     threads, so it is a knob rather than a default.
type expandOpts struct {
	// Top is how many qualifying chains to expand, best first. Zero disables
	// expansion entirely and leaves the summary output untouched.
	Top int
	// MinMatched is the number of matched entries a chain needs to qualify.
	MinMatched int
	// MinRatio is the matched/total ratio a chain needs to qualify. Zero means
	// no ratio gate.
	MinRatio float64
	// Cap stops expansion once this many entries have been printed. Zero is
	// unbounded.
	//
	// It is checked before a chain starts, never part-way through, so the bound
	// is Cap plus one chain rather than Cap exactly. Truncating a conversation
	// mid-way is worse than not expanding it: the reader cannot tell that what
	// they are reading is a fragment, which is the one thing expansion is
	// supposed to fix.
	Cap int
}

// chainSource is the one store method printChains needs, so the printing can be
// exercised without standing up a corpus.
type chainSource func(extID string) ([]corpus.Shown, error)

// printChains renders search's chain results, expanding the ones that qualify.
//
// The summary form is what runs when opts.Top is zero, and the two paths share
// this function rather than branching in the caller so the un-expanded output
// cannot drift as expansion changes.
func printChains(w io.Writer, chain chainSource, chains []corpus.ChainHit, opts expandOpts) error {
	printed := 0 // entries emitted by expansion so far, for opts.Cap
	expanded := 0
	for _, c := range chains {
		fmt.Fprintf(w, "%-46s  %2d/%-3d matched  %s -> %s\n",
			trunc(orElse(c.Subject, c.RootExtID), 46), c.Matched, c.Entries,
			c.First.Format("2006-01-02"), c.Last.Format("2006-01-02"))
		fmt.Fprintf(w, "    root %s\n", c.RootExtID)
		if opts.Top <= 0 {
			continue
		}
		if why := declined(c, opts, expanded, printed); why != "" {
			fmt.Fprintf(w, "    not expanded: %s\n", why)
			continue
		}
		items, err := chain(c.RootExtID)
		if err != nil {
			return fmt.Errorf("expanding %s: %w", c.RootExtID, err)
		}
		printExpanded(w, c, items)
		expanded++
		printed += len(items)
	}
	fmt.Fprintf(w, "\n%d chains\n", len(chains))
	if opts.Top > 0 {
		fmt.Fprintf(w, "%d expanded, %d %s shown in full\n",
			expanded, printed, plural(printed, "entry", "entries"))
	}
	return nil
}

// declined names the reason a chain is not expanded, or "" when it qualifies.
// It returns the reason rather than a bool because a gate whose verdict a reader
// cannot account for is indistinguishable from a bug in the search.
func declined(c corpus.ChainHit, opts expandOpts, expanded, printed int) string {
	if c.Matched < opts.MinMatched {
		return fmt.Sprintf("%d matched, -expand-min is %d", c.Matched, opts.MinMatched)
	}
	if opts.MinRatio > 0 && c.Entries > 0 {
		if r := float64(c.Matched) / float64(c.Entries); r < opts.MinRatio {
			return fmt.Sprintf("matched %.2f of it, -expand-ratio is %.2f", r, opts.MinRatio)
		}
	}
	if expanded >= opts.Top {
		return fmt.Sprintf("-expand is %d and that many already were", opts.Top)
	}
	// Checked after the relevance gates so the message names the binding
	// constraint: a chain that would not have qualified anyway should not be
	// reported as a victim of the cap.
	if opts.Cap > 0 && printed >= opts.Cap {
		return fmt.Sprintf("-expand-cap of %d entries reached", opts.Cap)
	}
	return ""
}

// printExpanded lists a chain's whole membership in time order, marking each
// entry "match" or "chain".
//
// The marking is not decoration. An expanded chain mixes entries the query
// found with entries that came in because of who they are replying to, and a
// reader who cannot separate those two cannot tell a thread that is about the
// topic from a thread that mentions it once.
func printExpanded(w io.Writer, c corpus.ChainHit, items []corpus.Shown) {
	matched := map[string]corpus.EntryHit{}
	for _, h := range c.Best {
		matched[h.ExtID] = h
	}
	for _, it := range items {
		h, hit := matched[it.ExtID]
		label := "chain"
		excerpt := it.Body
		if hit {
			label = "match"
			// The snippet is empty for a structural-only query, which has no
			// keyword to highlight; the body then says more than nothing does.
			if h.Snippet != "" {
				excerpt = h.Snippet
			}
		}
		fmt.Fprintf(w, "    %s  %s  %-22s %s%s\n", label,
			it.TS.Format("2006-01-02 15:04"), trunc(orElse(it.Author, "unknown"), 22),
			it.ExtID, whyHit(h, hit))
		if line := oneLine(excerpt, 96); line != "" {
			fmt.Fprintf(w, "           %s\n", line)
		}
	}
}

// whyHit is why() over an optional hit, so an expanded entry that also matched
// still says which ranking found it.
func whyHit(h corpus.EntryHit, hit bool) string {
	if !hit {
		return ""
	}
	return why(h)
}

// expandPerChain is the PerChain a search runs with when expanding.
//
// Marking matched-vs-expanded is only truthful if ChainHit.Best holds *every*
// matched entry rather than the best few, and a PerChain above the candidate
// pool is how you say "all of them" to a query that caps per-chain hits, and
// this is comfortably above the 500 that pool defaults to.
const expandPerChain = 1000
