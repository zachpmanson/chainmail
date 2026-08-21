package slackingest

import (
	"regexp"
	"strings"
)

// Slack sends message text as mrkdwn: prose with entity references wrapped in
// angle brackets. What is stored is the plain-text rendering of it, matching
// what mailingest stores — entries.body_text is plain text everywhere, and
// body_html is left to the spec generator, which owns presentation.
//
// The one substantive rewrite is mentions. Left raw, a message reads "<@U07QE0>
// can you check" and, worse, is unfindable: searching a person's name would
// never match the messages that named them, which is most of what a mention is
// for. Resolving to a display name costs the uid from the text — recoverable
// from the archive at any time, since re-ingest is free — and buys a body that
// says what a human said.

var reEntity = regexp.MustCompile(`<([^<>]*)>`)

// Names resolves the ids inside a message to something readable. Both lookups
// return empty for an id the archive never saw, which leaves the id in place
// rather than dropping the reference.
type Names struct {
	User    func(id string) string
	Channel func(id string) string
}

// PlainText renders mrkdwn as plain text.
func PlainText(text string, names Names) string {
	out := reEntity.ReplaceAllStringFunc(text, func(m string) string {
		return renderEntity(m[1:len(m)-1], names)
	})
	// Unescaping comes last, on purpose: doing it first would turn a literal
	// "&lt;@U123&gt;" — someone quoting a mention rather than making one — into a
	// real entity reference and resolve it as if they had.
	return unescape(out)
}

func renderEntity(body string, names Names) string {
	ref, label, hasLabel := strings.Cut(body, "|")
	switch {
	case strings.HasPrefix(ref, "@"):
		id := strings.TrimPrefix(ref, "@")
		if n := lookup(names.User, id); n != "" {
			return "@" + n
		}
		if hasLabel && label != "" {
			return "@" + label
		}
		return "@" + id

	case strings.HasPrefix(ref, "#"):
		id := strings.TrimPrefix(ref, "#")
		if hasLabel && label != "" {
			return "#" + label
		}
		if n := lookup(names.Channel, id); n != "" {
			return "#" + n
		}
		return "#" + id

	case strings.HasPrefix(ref, "!"):
		// Broadcasts (<!here>) and special renders (<!date^...>, <!subteam^S1|@x>).
		// The label, where Slack supplied one, is the text it would have shown.
		if hasLabel && label != "" {
			return label
		}
		return "@" + strings.TrimPrefix(strings.SplitN(ref, "^", 2)[0], "!")

	case ref == "":
		return body

	default:
		// A link. mailto: links are addresses to a reader, not URLs.
		if addr := strings.TrimPrefix(ref, "mailto:"); addr != ref {
			if hasLabel && label != "" {
				return label
			}
			return addr
		}
		if hasLabel && label != "" && label != ref {
			// Both halves are kept: the label is what was read, the URL is what was
			// clicked, and a search for either should find the message.
			return label + " (" + ref + ")"
		}
		return ref
	}
}

func lookup(f func(string) string, id string) string {
	if f == nil || id == "" {
		return ""
	}
	return strings.TrimSpace(f(id))
}

// unescape reverses the three entities Slack escapes in message text. There are
// no others: mrkdwn is not HTML, so a general entity decoder would corrupt text
// that merely happens to contain "&copy;".
func unescape(s string) string {
	s = strings.ReplaceAll(s, "&lt;", "<")
	s = strings.ReplaceAll(s, "&gt;", ">")
	return strings.ReplaceAll(s, "&amp;", "&")
}
