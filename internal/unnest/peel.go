package unnest

import "strings"

// Block is one message recovered from a body.
//
// The first block of a body is what the sender actually wrote; the rest were
// quoted beneath it and exist nowhere else in the mailbox.
type Block struct {
	// Depth is the quote depth this block was found at. 0 is the visible message.
	Depth int
	// Sentinel is the boundary that introduced this block, empty for the first.
	Sentinel string
	// Kind of that boundary.
	Kind Kind
	// Text is the block's own content, markers stripped, trailing signature and
	// blank lines trimmed.
	Text string
	// Lines is the half-open range in the normalised body this block covers,
	// including its sentinel. Kept so callers can attribute a block back to the
	// exact source region rather than by matching text.
	Start, End int
}

// maxDepth bounds recursion.
//
// Deliberately far above talon's MAX_LINES_COUNT = 1000 and quotequail's
// limit=1000, which exist because those projects hit catastrophic regex
// backtracking — a problem Go's RE2-based engine does not have. Inheriting their
// caps would truncate exactly the deep history this package exists to recover:
// real trails here reach depth 18, and References chains reach 15.
const maxDepth = 128

// Peel splits a body into the blocks it contains, in the order they appear.
//
// The algorithm is a single forward pass. At each line it asks whether a boundary
// starts here; if so the current block ends and a new one begins. Depth is
// carried on each line rather than consumed by the scan, so a sentinel nested
// inside quoted text is just as visible as one at the top — which is the majority
// case in real mail, and the thing talon gets wrong by marking a line as quoted
// before any splitter sees it.
func Peel(body string) []Block {
	lines := Normalise(body)
	if len(lines) == 0 {
		return nil
	}

	var out []Block
	cur := Block{Depth: lines[0].Depth, Start: 0}
	var buf []string

	flush := func(end int) {
		cur.Text = trimBlock(buf)
		cur.End = end
		if cur.Text != "" || cur.Sentinel != "" {
			out = append(out, cur)
		}
		buf = nil
	}

	for i := 0; i < len(lines); {
		b, ok := boundaryAt(lines, i)
		if !ok {
			buf = append(buf, lines[i].Text)
			i++
			continue
		}
		flush(b.Start)
		cur = Block{
			Depth:    b.Depth,
			Sentinel: b.Text,
			Kind:     b.Kind,
			Start:    b.Start,
		}
		i = b.End
	}
	flush(len(lines))
	return out
}

// boundaryAt reports the boundary starting at line i, if any.
//
// Order matters. A forward rule is checked first because it is followed within a
// few lines by a header block in every observed case — 222 of 222 in this
// mailbox — and the two must be consumed as one boundary or a spurious empty
// block appears between them.
func boundaryAt(lines []Line, i int) (Boundary, bool) {
	if b, ok := findForwardRule(lines, i); ok {
		return b, true
	}
	if b, ok := FindAttribution(lines, i); ok {
		return b, true
	}
	if b, ok := FindHeaderBlock(lines, i); ok {
		return b, true
	}
	return Boundary{}, false
}

// findForwardRule matches a forward separator, or a bare rule of dashes or
// underscores, and swallows any header block that follows.
//
// A bare rule alone is NOT a boundary: a 32-underscore run is Outlook's quote
// separator, but a 60-underscore run is Teams meeting-invite chrome sitting in the
// middle of a live message, and splitting there fabricates an entry. Length does
// not separate the two, so the rule only counts when a header block or attribution
// follows within a few lines.
func findForwardRule(lines []Line, i int) (Boundary, bool) {
	t := strings.TrimSpace(lines[i].Text)
	named := reForwardRule.MatchString(t)
	bare := reBareRule.MatchString(t)
	if !named && !bare {
		return Boundary{}, false
	}

	// Look ahead past blank lines for the header block or attribution that makes
	// this a real boundary.
	j := i + 1
	for lookahead := 0; j < len(lines) && lookahead < 5; j++ {
		if strings.TrimSpace(lines[j].Text) == "" {
			continue
		}
		lookahead++
		if hb, ok := FindHeaderBlock(lines, j); ok {
			return Boundary{
				Kind: KindForwardRule, Start: i, End: hb.End,
				Depth: lines[i].Depth,
				Text:  t + "\n" + hb.Text,
			}, true
		}
		if at, ok := FindAttribution(lines, j); ok {
			return Boundary{
				Kind: KindForwardRule, Start: i, End: at.End,
				Depth: lines[i].Depth,
				Text:  t + "\n" + at.Text,
			}, true
		}
		break
	}
	if named {
		// A named rule is unambiguous even with nothing recognisable after it.
		return Boundary{
			Kind: KindForwardRule, Start: i, End: i + 1,
			Depth: lines[i].Depth, Text: t,
		}, true
	}
	return Boundary{}, false
}

// trimBlock removes leading and trailing blank lines, and everything from an
// RFC 3676 "-- " signature delimiter onward.
//
// The delimiter is not a boundary: it ends the current author's text rather than
// starting someone else's, so it trims a block instead of splitting one.
func trimBlock(lines []string) string {
	for i, l := range lines {
		if reSigDelim.MatchString(l) {
			lines = lines[:i]
			break
		}
	}
	start, end := 0, len(lines)
	for start < end && strings.TrimSpace(lines[start]) == "" {
		start++
	}
	for end > start && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	return strings.Join(lines[start:end], "\n")
}
