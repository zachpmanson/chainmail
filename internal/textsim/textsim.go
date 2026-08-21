// Package textsim compares two renditions of the same words.
//
// Every client rewrites the text it quotes — rewrapping to its own column width,
// eliding with "...", dropping [image: ] placeholders — so two copies of one
// message are never byte-equal and a hash cannot recognise them. What survives
// is the words and the order they open in, which is what this measures.
//
// It is a package rather than a file because two callers now ask the same
// question of different data: internal/spec correlates a recovered entry against
// the markup of the message that quoted it, and internal/corpus decides whether a
// quoted entry and a mailbox entry are one message. A second copy of the
// measure would let the two drift, and the head test below is the part that took
// a wrong answer to find.
package textsim

import "strings"

// Tokens reduces text to the words in it, lower-cased, in order. Order is kept
// because a token list is cheap to compare as a multiset and useful to eyeball;
// case and punctuation go because two renditions of one message disagree about
// both.
func Tokens(s string) []string {
	var out []string
	var cur strings.Builder
	flush := func() {
		if cur.Len() > 0 {
			out = append(out, cur.String())
			cur.Reset()
		}
	}
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			cur.WriteRune(r)
		default:
			flush()
		}
	}
	flush()
	return out
}

// Overlap is the size of the multiset intersection: how many of a's tokens are
// present in b, counting repeats once each.
func Overlap(a, b []string) int {
	have := make(map[string]int, len(b))
	for _, t := range b {
		have[t]++
	}
	n := 0
	for _, t := range a {
		if have[t] > 0 {
			have[t]--
			n++
		}
	}
	return n
}

// Similarity is the symmetric overlap of two token multisets: 0 for disjoint, 1
// for identical.
func Similarity(a, b []string) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	return 2 * float64(Overlap(a, b)) / float64(len(a)+len(b))
}

// HeadSimilarity reports how much of a's opening survives, in order, at the
// start of b: the longest common subsequence of a's first run tokens and b's
// first window tokens, as a fraction of that opening.
//
// In order, and only the opening, because that is the part a signature cannot
// forge. A message's opening words are what identify it; its closing words are a
// signature and a legal disclaimer, identical across everything that person ever
// sent. On a short message the boilerplate is most of the tokens, so an
// unordered measure alone matched one person's two-line question to a different
// two-line question of theirs a fortnight earlier — the same text scored well
// for both, on the strength of the signature they shared.
//
// The window is wider than the run because two renditions do not agree token for
// token — a link arrives as "text <url>", an image as a placeholder — so the
// same sentence starts at a different offset in each.
func HeadSimilarity(a, b []string, run, window int) float64 {
	head := a[:min(run, len(a))]
	if len(head) == 0 {
		return 0
	}
	return float64(LCS(head, b[:min(window, len(b))])) / float64(len(head))
}

// LCS is the length of the longest common subsequence of two short token runs.
func LCS(a, b []string) int {
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for i := 1; i <= len(a); i++ {
		for j := 1; j <= len(b); j++ {
			if a[i-1] == b[j-1] {
				cur[j] = prev[j-1] + 1
				continue
			}
			cur[j] = max(prev[j], cur[j-1])
		}
		prev, cur = cur, prev
		for j := range cur {
			cur[j] = 0
		}
	}
	return prev[len(b)]
}
