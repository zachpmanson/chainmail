// Package unnest recovers the messages hidden inside quoted history.
//
// Most of a mail trail is not in the mailbox as messages: on a real 58-entry
// trail, 41 entries existed only inside quoted text, mined from 20 forwards. This
// package turns one body into the ordered blocks it actually contains.
package unnest

import (
	"regexp"
	"strings"
)

// Line is one physical line with its quote depth measured and stripped.
//
// Separating depth from content is the central design decision, and it is where
// every surveyed library goes wrong: quotequail joins raw lines then strips a
// single leading ">", and talon marks a line as quoted before any sentinel
// pattern sees it. Both then fail on a wrapped attribution whose continuation
// line sits at a different depth — 251 of 708 quoting messages in this mailbox.
type Line struct {
	Text  string // marker-stripped
	Depth int    // number of quote levels
	Raw   string
}

// RFC 3676 §4.5: quote depth counts ">" characters, and a space immediately after
// a ">" is space-stuffing rather than content. So ">>" is depth 2 while "> >" is
// also depth 2 — the space belongs to the first marker.
func splitDepth(raw string) (text string, depth int) {
	s := strings.TrimLeft(raw, " \t")
	for strings.HasPrefix(s, ">") {
		depth++
		s = s[1:]
		s = strings.TrimPrefix(s, " ")
	}
	return s, depth
}

var (
	// Gmail emits U+202F before am/pm and Outlook emits U+00A0; Go's \s matches
	// neither, so they are folded to a plain space before anything else runs.
	reOddSpace = regexp.MustCompile("[    ]")
	// HTML-to-text flattening leaves these where a boundary marker would be, and
	// they interfere with "previous non-empty line" logic.
	reImagePlaceholder = regexp.MustCompile(`(?i)\[(?:image|cid):[^\]]*\]`)
	// Outlook flattens bold header keys to *From:* / *From: *
	reBoldKey = regexp.MustCompile(`^\*([A-Za-z-]+)\*?\s*:\s*\*?\s*`)
)

// Normalise splits a body into lines with depth measured, folding the
// whitespace and placeholder noise that would otherwise defeat matching.
//
// CRLF is handled here rather than in every pattern: bodies arrive CRLF, and Go's
// `$` does not consume the \r, which silently produced zero matches for several
// sentinel families during the survey of this mailbox.
func Normalise(body string) []Line {
	body = strings.ReplaceAll(body, "\r\n", "\n")
	body = strings.ReplaceAll(body, "\r", "\n")
	body = reOddSpace.ReplaceAllString(body, " ")

	raw := strings.Split(body, "\n")
	out := make([]Line, 0, len(raw))
	for _, r := range raw {
		text, depth := splitDepth(r)
		text = reImagePlaceholder.ReplaceAllString(text, "")
		text = strings.TrimRight(text, " \t")
		out = append(out, Line{Text: text, Depth: depth, Raw: r})
	}
	return out
}

// unbold turns "*From:*" and "*From: *" into "From:" so one pattern covers all
// four dialects Outlook emits.
func unbold(s string) string {
	if m := reBoldKey.FindStringSubmatch(s); m != nil {
		return m[1] + ": " + strings.TrimSpace(s[len(m[0]):])
	}
	return s
}
