package spec

import (
	"fmt"
	"html"
	"strings"

	"github.com/zachpmanson/chainmail/internal/boiler"
	"github.com/zachpmanson/chainmail/internal/unnest"
)

// A signature and a confidentiality notice are folded behind a disclosure, not
// removed.
//
// Removing them reads better and is the wrong trade. The phone number in a
// signature is sometimes exactly the thing being looked for, and a notice is
// occasionally the fact under dispute — which retailer's disclaimer was on the
// mail that quoted the rate. Folding gets the same page back and keeps both: the
// text is in the document either way, so it is found by the browser's own search
// and by grep over the exported file, and one click puts it back on screen.
//
// The disclosure is deliberately the same weight as the "[signature trimmed]"
// line it replaces. Fifty-seven entries each carrying a prominent expander would
// be a second kind of noise, so the control is one quiet italic line — the
// weight of an editorial aside, which is what it is — and the evidence for the
// fold is in its title attribute, the way an inferred zone argues for itself in
// Stamp (src/components/Timeline.tsx).
//
// The blocks a client marked itself are folded the same way, rather than
// trimmed. There is one story about signatures on the page, and a reader who
// opens one fold and finds the text has no reason to think another fold works
// differently. Trimming the marked ones and folding the rest would mean the page
// silently keeps some signatures and silently drops others, with nothing on it to
// say which.

// foldNote is what a disclosure says about itself: the word in the summary, and
// the evidence behind it for the reader who wants to argue.
type foldNote struct {
	label string
	title string
}

// repeatNote describes a fold the repetition found. The counts are the whole
// argument, so they are stated rather than summarised: "appears on 43 of this
// sender's messages" is checkable, "looks like a signature" is not.
func repeatNote(f boiler.Fold) foldNote {
	switch f.Scope {
	case boiler.Domain:
		return foldNote{
			label: "disclaimer",
			title: fmt.Sprintf("Folded, not removed — this block of %d lines ends %d messages "+
				"from %d senders at this domain, so it is appended by the organisation rather "+
				"than written by the sender. The text is still on the page and still searchable.",
				f.Lines, f.Count, f.Senders),
		}
	default:
		return foldNote{
			label: "signature",
			title: fmt.Sprintf("Folded, not removed — these %d lines end %d of this sender's "+
				"messages verbatim, so they are appended rather than written. The text is still "+
				"on the page and still searchable.", f.Lines, f.Count),
		}
	}
}

// markedNote describes a fold the sending client asked for: the RFC 3676 "-- "
// delimiter, or a class the client put on its own signature block.
var markedNote = foldNote{
	label: "signature",
	title: "Folded, not removed — the sending client marked this block as a signature. " +
		"The text is still on the page and still searchable.",
}

// foldHTML wraps already-rendered markup in the disclosure.
//
// The class survives because this runs after stripChrome, which drops class
// attributes on everything a sender sent. Anything added before that pass would
// arrive on the page unstyled.
func foldHTML(n foldNote, inner string) string {
	return `<details class="sig"><summary title="` + html.EscapeString(n.title) + `">` +
		html.EscapeString(n.label) + `</summary><div class="sigbd">` + inner + `</div></details>`
}

// bodyFold is a detected block as the markup path needs it: the block's own
// lines, taken from the text rendition where the repetition was found, and what
// the disclosure will say about itself. Empty means nothing was detected.
type bodyFold struct {
	lines []string
	note  foldNote
}

// blockFor reduces an entry's detected fold to its lines.
//
// From the entry's own text and not from the corpus pass, so that the lines are
// this message's rather than a representative of the block — they are the same
// text by construction, and reading them here keeps the corpus pass free of
// having to hand back every body it read.
func blockFor(r *entryRow, st bodyStyle) bodyFold {
	if r.Fold.Lines <= 0 {
		return bodyFold{}
	}
	lines, ok := boiler.Lines(r.BodyText, st.peel)
	if !ok {
		return bodyFold{}
	}
	vis := boiler.Visible(lines)
	if r.Fold.Lines >= len(vis) {
		// Nothing would remain in view; see splitBoilerplate.
		return bodyFold{}
	}
	return bodyFold{lines: vis[len(vis)-r.Fold.Lines:], note: repeatNote(r.Fold)}
}

// splitBoilerplate divides a body's lines into what the sender wrote and the
// block appended below it.
//
// Two boundaries are considered and the earlier one wins, since the block that
// starts higher contains the other. The RFC 3676 delimiter is the client's own
// statement and needs no threshold; the repetition is corpus evidence and comes
// with counts. Where the delimiter falls inside a repeated block the note
// reports the repetition, which is the larger claim of the two.
//
// The split never takes every line: a body that is entirely boilerplate keeps
// its first line, because a bubble whose only content is a closed disclosure
// reads as a page that failed to render. boiler.Detect already guarantees this
// for a repeated tail; the delimiter has no such guarantee, and a body that
// opens on "--" is the case it is needed for.
func splitBoilerplate(lines []unnest.Line, f boiler.Fold) (body, tail []unnest.Line, note foldNote) {
	at := len(lines)
	if f.Lines > 0 {
		if i := boiler.TailStart(lines, f.Lines); i < at {
			at, note = i, repeatNote(f)
		}
	}
	if i := sigDelimiter(lines); i >= 0 && i < at {
		at, note = i, markedNote
	}
	if at >= len(lines) || !hasVisible(lines[:at]) {
		return lines, nil, foldNote{}
	}
	return lines[:at], lines[at:], note
}

// reSigDelim is the RFC 3676 signature delimiter. Normalise right-trims, so
// "-- " arrives as "--".
//
// It fires on nothing in this corpus — every client that marks a signature at
// all does it in markup — so it is kept for what it costs rather than for what
// it catches: a body from a client that does write it is folded on the client's
// own word, with no threshold to clear.
func sigDelimiter(lines []unnest.Line) int {
	for i, l := range lines {
		if strings.TrimSpace(l.Text) == "--" {
			return i
		}
	}
	return -1
}

func hasVisible(lines []unnest.Line) bool {
	for _, l := range lines {
		if strings.TrimSpace(l.Text) != "" {
			return true
		}
	}
	return false
}

// foldNotes reports what was folded, so the page says it rather than merely
// doing it. A reader who cannot see a signature has to be able to find out from
// the page that one is there and why it is not on screen.
func foldNotes(rows []*entryRow) []string {
	folded, byScope := 0, map[boiler.Scope]int{}
	for _, r := range rows {
		if !r.Folded {
			continue
		}
		folded++
		byScope[r.Fold.Scope]++
	}
	if folded == 0 {
		return nil
	}
	item := fmt.Sprintf(
		"%d of %d entries have a trailing block folded behind a disclosure in the body — "+
			"a signature or a legal notice, found by its appearing verbatim at the end of "+
			"many messages from the same sender or the same domain. Nothing is removed: the "+
			"text is in the page, searchable, one click from view.", folded, len(rows))
	if n := byScope[boiler.Domain]; n > 0 {
		item += fmt.Sprintf(" %d of them repeat across several senders at one domain rather "+
			"than one person's messages, which is what an organisation's notice looks like.", n)
	}
	return []string{item}
}
