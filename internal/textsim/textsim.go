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

// maxAlignTokens caps what Align will attempt. The alignment is quadratic in
// both lengths, and above this a pair is a legal notice or a pasted report
// rather than two renditions of one message — the answer would be slow and
// would not be believed anyway.
const maxAlignTokens = 2000

// Divergence is where one rendition's extra words sit relative to the rendition
// it quotes: the same count of extra words means different things depending on
// whether they interrupt the quoted text or follow it.
//
// Inside is the count that a quoter typed into the middle of what they were
// quoting, and it is the only field that says somebody answered inline. Appended
// and Before are client chrome — a signature, a legal notice, a tracking blob, an
// attribution line — which every requote grows and which no reader would miss.
// Astride is extra words at a point where the quoted rendition also skips words
// of its own, so the two texts diverge there rather than one containing the
// other; that is what a wrongly paired footer looks like, and reading it as an
// annotation is how the measurement inflates.
type Divergence struct {
	Inside        int
	LongestInside int
	Appended      int
	Before        int
	Astride       int
	// Measured is false when the pair was too long to align, so a caller cannot
	// mistake a skipped alignment for a clean one.
	Measured bool
}

// Divergences locates the tokens of later that base does not have.
//
// Positional rather than a count, because the count alone cannot tell an inline
// answer from a footer: both are "words the other copy lacks". The alignment is
// the longest common subsequence, so the anchors it finds are in order, and a
// run of later's tokens counts as inside only where base's own tokens run
// straight through — anchor to adjacent anchor with nothing of base's skipped.
// Requiring that adjacency is what keeps a footer's stray matches on common
// words ("the", "and") from stretching the last anchor into the footer and
// reporting it as an insertion.
func Divergences(base, later []string) Divergence {
	if len(base) > maxAlignTokens || len(later) > maxAlignTokens {
		return Divergence{}
	}
	pairs := Align(base, later)
	d := Divergence{Measured: true}
	if len(pairs) == 0 {
		return d
	}
	d.Before = pairs[0][1]
	d.Appended = len(later) - 1 - pairs[len(pairs)-1][1]
	for k := 1; k < len(pairs); k++ {
		run := pairs[k][1] - pairs[k-1][1] - 1
		if run == 0 {
			continue
		}
		if pairs[k][0]-pairs[k-1][0] > 1 {
			d.Astride += run
			continue
		}
		d.Inside += run
		d.LongestInside = max(d.LongestInside, run)
	}
	return d
}

// Align is the longest common subsequence of two token runs as the index pairs
// it matches, in order.
//
// Separate from LCS, which answers only how long: the positional test needs the
// anchors themselves, and a length cannot say where a gap between two matches
// falls.
func Align(a, b []string) [][2]int {
	n, m := len(a), len(b)
	// One row per prefix of a, kept whole: the backtrace needs the table, and
	// int32 holds a length no token run this function accepts can exceed.
	table := make([][]int32, n+1)
	for i := range table {
		table[i] = make([]int32, m+1)
	}
	for i := 1; i <= n; i++ {
		for j := 1; j <= m; j++ {
			if a[i-1] == b[j-1] {
				table[i][j] = table[i-1][j-1] + 1
				continue
			}
			table[i][j] = max(table[i-1][j], table[i][j-1])
		}
	}
	out := make([][2]int, 0, table[n][m])
	for i, j := n, m; i > 0 && j > 0; {
		switch {
		case a[i-1] == b[j-1]:
			out = append(out, [2]int{i - 1, j - 1})
			i, j = i-1, j-1
		case table[i-1][j] >= table[i][j-1]:
			i--
		default:
			j--
		}
	}
	for l, r := 0, len(out)-1; l < r; l, r = l+1, r-1 {
		out[l], out[r] = out[r], out[l]
	}
	return out
}
