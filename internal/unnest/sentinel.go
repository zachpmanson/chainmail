package unnest

import (
	"regexp"
	"strings"
)

// maxWrapLines is how many continuation lines an attribution may span.
//
// quotequail caps this at 2 and consequently misses a three-line wrap outright.
// Deep quoting wraps attributions hard — a 55-deep prefix plus a name plus an
// address does not fit in 78 columns — so this is deliberately higher.
const maxWrapLines = 5

// Kind classifies a boundary.
type Kind int

const (
	KindNone Kind = iota
	// KindAttribution is "On <date>, <who> wrote:" and its localised forms. It
	// carries the sender and date of the block that follows.
	KindAttribution
	// KindHeaderBlock is two or more consecutive From:/Sent:/To:/Subject: lines.
	KindHeaderBlock
	// KindForwardRule is "---------- Forwarded message ---------" and friends,
	// which in this mailbox is *always* followed within five lines by a header
	// block — 222 of 222 cases. The two must be consumed as one boundary or an
	// empty entry appears between them.
	KindForwardRule
	// KindSignature is "-- ", RFC 3676's delimiter. Not a boundary: it ends the
	// current author's text, so it trims a block rather than splitting one.
	KindSignature
)

// Openers and closers are kept as separate lists so an attribution can be found
// by scanning for the rarer closer and walking back to the nearest opener. That
// is the RE2-expressible equivalent of the "tempered dot" the PHP library uses:
// refusing to cross a second opener is what stops one match spanning two
// attributions, which produces a wrong sender rather than a missing one.
var (
	openers = []string{
		"on", "le", "el", "il", "em", "am", "op", "den", "på", "pe",
		"w dniu", "dnia", "vào", "在",
	}
	closers = []string{
		"wrote:", "sent:", "a écrit :", "a écrit:", "écrit:", "escribió:",
		"ha scritto:", "scritto:", "escreveu:", "schrieb:", "schreef:",
		"napisał:", "napisała:", "pisze:", "skrev:", "kirjoitti:",
		"đã viết:", "写道：", "작성:",
	}

	reHeaderKey = regexp.MustCompile(`(?i)^\*?(from|sent|to|subject|cc|bcc|date|reply-to|` +
		`von|gesendet|an|betreff|de|enviado|para|asunto|objet|envoyé|` +
		`van|verzonden|aan|da|inviato|oggetto|fra|sendt|skickat|till|ämne|assunto)` +
		`\*?\s*:\s`)
	reForwardRule = regexp.MustCompile(`(?i)^-{2,}\s?forwarded message\s?-{2,}$|` +
		`^-{3,}\s?original message\s?-{3,}$|^begin forwarded message\s*:$`)
	// A bare rule of dashes or underscores is Outlook chrome. It is only a
	// boundary when a header block follows within a few lines: a 60-underscore run
	// is a Teams meeting-invite decoration sitting mid-message, and splitting there
	// fabricates an entry.
	reBareRule = regexp.MustCompile(`^([-_]{10,}|\*{8,})$`)
	// Normalise right-trims lines, so the RFC 3676 delimiter arrives as "--".
	reSigDelim = regexp.MustCompile(`^--$`)
)

func hasClosum(s string) bool {
	l := strings.ToLower(s)
	for _, c := range closers {
		if strings.Contains(l, c) {
			return true
		}
	}
	return false
}

func startsWithOpener(s string) bool {
	l := strings.ToLower(strings.TrimSpace(s))
	for _, o := range openers {
		if l == o || strings.HasPrefix(l, o+" ") || strings.HasPrefix(l, o+" ") {
			return true
		}
	}
	return false
}

// Boundary is a detected split point.
type Boundary struct {
	Kind Kind
	// Start and End are line indexes, End exclusive: the whole sentinel,
	// including any continuation lines and a following header block.
	Start, End int
	// Depth is the quote depth of the sentinel's FIRST line. A wrapped
	// attribution may continue at a different depth, and the first line is the
	// one that says where this boundary sits in the trail.
	Depth int
	// Text is the joined, marker-free sentinel, ready for attribution parsing.
	Text string
}

// FindAttribution looks for an attribution ending at or before line i, joining
// continuation lines. It returns the boundary and true if one starts at i.
//
// The join deliberately ignores quote depth: a continuation line's markers may be
// deeper OR shallower than the first line's, which is the case that makes
// quotequail corrupt the captured sender and makes talon miss the boundary
// entirely.
func FindAttribution(lines []Line, i int) (Boundary, bool) {
	if i >= len(lines) || !startsWithOpener(lines[i].Text) {
		return Boundary{}, false
	}
	var parts []string
	for j := i; j < len(lines) && j <= i+maxWrapLines; j++ {
		t := strings.TrimSpace(lines[j].Text)
		// A blank line ends the attempt; an attribution does not straddle one.
		if t == "" && j > i {
			break
		}
		// Never run past a second opener: that would span two attributions and
		// yield a sender belonging to neither.
		if j > i && startsWithOpener(t) {
			break
		}
		parts = append(parts, t)
		joined := strings.Join(parts, " ")
		if hasClosum(joined) {
			return Boundary{
				Kind:  KindAttribution,
				Start: i,
				End:   j + 1,
				Depth: lines[i].Depth,
				Text:  strings.Join(strings.Fields(joined), " "),
			}, true
		}
	}
	return Boundary{}, false
}

// FindHeaderBlock requires two or more consecutive recognised header keys.
//
// One key is not enough: 32 messages in this mailbox carry a From: line with no
// address at all ("From: Alice | Acme"), so requiring an address would reject
// real blocks — while "Passcode:" and "Meeting ID:" lines are Key: value shaped
// and would be accepted by anything looser.
func FindHeaderBlock(lines []Line, i int) (Boundary, bool) {
	n := 0
	j := i
	for ; j < len(lines); j++ {
		t := unbold(strings.TrimSpace(lines[j].Text))
		if t == "" {
			break
		}
		if !reHeaderKey.MatchString(t) {
			// A long recipient list wraps, and the quoted rendering usually loses
			// the leading whitespace RFC 5322 folding would have left. Treat the
			// line as a continuation only when a header key resumes right after it:
			// that is what separates a folded Cc: from the start of the body.
			//
			// Stopping here instead cost real data — the remaining keys were
			// orphaned outside the block, so Subject: landed in the body text and
			// every recipient past the wrap was lost. 10 of 28 recovered entries
			// on one real trail.
			if n > 0 && continuesHeader(lines, j) {
				continue
			}
			break
		}
		n++
	}
	if n < 2 {
		return Boundary{}, false
	}
	var parts []string
	for k := i; k < j; k++ {
		parts = append(parts, unbold(strings.TrimSpace(lines[k].Text)))
	}
	return Boundary{
		Kind: KindHeaderBlock, Start: i, End: j,
		Depth: lines[i].Depth, Text: strings.Join(parts, "\n"),
	}, true
}

// maxFoldLines is how far a folded value may run before the next key.
//
// A long recipient list wraps repeatedly — one real Cc: here spans three lines —
// so tolerating a single continuation is not enough. The bound is what keeps the
// rule safe: prose is only mistaken for a folded value if a header key appears
// within this many lines of it, which body text does not do.
const maxFoldLines = 6

// continuesHeader reports whether line j is a folded continuation rather than
// the start of the body, judged by whether a header key resumes within
// maxFoldLines. A blank line ends the search: a header block never contains one.
func continuesHeader(lines []Line, j int) bool {
	for k := j + 1; k < len(lines) && k <= j+maxFoldLines; k++ {
		t := unbold(strings.TrimSpace(lines[k].Text))
		if t == "" {
			return false
		}
		if reHeaderKey.MatchString(t) {
			return true
		}
	}
	return false
}
