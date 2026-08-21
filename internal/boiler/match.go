package boiler

import (
	"regexp"
	"strings"

	"github.com/zachpmanson/chainmail/internal/unnest"
)

// The evidence is compared after the sending client's rendering is taken off it,
// because the repetition being counted is a fact about what the sender appended
// and not about which client flattened it.
//
// One signature arrives in as many spellings as there are clients that quoted
// it: an inline link is "label <url>" here and bare "label" there, an inline
// image is "[image: name.png]" here and "<name.png>" there, an address grows a
// "<mailto:...>" beside itself, bold flattens to asterisks, and the same
// sentence is hard-wrapped at one width in the mailbox copy and another width in
// somebody's quote of it. Counted verbatim, those spellings are different
// blocks: one sender's nine messages split three ways, no group clears three,
// and the block that does clear is the coarser one shared with their colleagues
// — which starts below their name and title and leaves the sign-off on screen.
//
// Nothing here reaches the page. The fold is applied to Visible, the sender's
// own text, and only ever by a count of lines; these strings exist to be hashed
// and are discarded. Normalising and then folding the normalised form would
// silently rewrite bodies, which is the one thing this repository must not do to
// a body it is trying to preserve — see the byte-equality test in match_test.go.

var (
	// A bracketed alt text with the href it labelled right behind it: one client's
	// rendering of an image inside a link, where the next renders the href alone
	// and a third renders nothing. Both halves come off, so all three agree.
	//
	// Only when the two are adjacent. A bare "[...]" is prose — a sender writing
	// "[see attached]" means it — and dropping every bracketed run would let two
	// unrelated tails agree on what is left.
	reAltLink = regexp.MustCompile(`\[[^\]\n]*\]\s*<[^>\s]*>`)
	// An angle-bracketed run with no whitespace in it: a url, a mailto, the name
	// of an inline image. Which of these a client emits beside a link, and whether
	// it emits one at all, is the client's decision and not the sender's.
	//
	// The no-whitespace requirement is what keeps this off prose. A sender writing
	// "<see below>" keeps it; what goes is the shape a machine wrote.
	reAngleToken = regexp.MustCompile(`<[^>\s]*>`)
	// unnest.Normalise already folds U+00A0 and U+202F to a space and right-trims,
	// and it runs before this on every path (boiler.Lines). What is left is runs of
	// ordinary spaces around a flattened table cell, which differ between two
	// renditions of one signature line.
	reSpaceRun = regexp.MustCompile(`\s+`)
)

// Match is the lines a tail is tallied over: Visible, with the sending client's
// rendering normalised away.
//
// One entry per visible line and in the same order, so a tail of n entries here
// is the same n lines Visible would hand the fold. An entry may normalise to the
// empty string — a line that held nothing but a placeholder — and it stays in
// place rather than being dropped, because dropping it would shift every index
// above it and fold a different block than the one that was counted.
func Match(lines []unnest.Line) []string {
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		if strings.TrimSpace(l.Text) == "" {
			continue
		}
		out = append(out, matchable(l.Text))
	}
	return out
}

func matchable(s string) string {
	s = reAltLink.ReplaceAllString(s, "")
	s = reAngleToken.ReplaceAllString(s, "")
	// Asterisks are how the clients in this corpus flatten bold, and a client that
	// drops the formatting instead emits the same words without them. Removing
	// every asterisk rather than only balanced pairs also covers the half-open
	// "*Name  *" a flattener leaves when the bold run ended inside trailing space.
	s = strings.ReplaceAll(s, "*", "")
	return strings.TrimSpace(reSpaceRun.ReplaceAllString(s, " "))
}
