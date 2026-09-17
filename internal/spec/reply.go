package spec

import (
	"strings"

	"github.com/zachpmanson/chainmail/internal/corpus"
)

// ReplyBody is what a reply says: the reader's own words, then the message they
// are answering, quoted and attributed the way a mail client quotes.
//
// It is composed here rather than in the browser for the reason the recipient
// line and the rendered body are (see render.go): the words an entry's clock is
// written in are this package's, and a quote's heading names the same sender and
// the same instant its own bubble prints. Composing it anywhere else would be a
// second renderer of one message, which is exactly what the pane and a page build
// were made to share. The send handler is the only caller, and the body it hands
// to the mailbox is this — so what the reader is shown as the plan (POST /v1/send
// without `confirm`) is the message itself rather than a draft of one.
//
// The shape is the transcript's own:
//
//	The reader's words.
//
//	On Mon 2 Jan 2026 15:04 AEDT, Ada Okoye <ada@loomworks.example> wrote:
//	> the message being answered, a line at a time
//
// Nothing about the quote is trimmed, folded or excluded. The body is the one the
// corpus holds — already unspooled from the quotes around it and stored as its own
// text — so what is quoted is the message the reader just read, with whatever
// trailing signature it carried. A mail client quotes the message; it does not
// quote a tidied version of it, and the pane's fold decisions are the reader's
// screen rather than the message's content.
//
// An entry with no body is quoted under a heading that still names it, with no
// quoted lines beneath: an answer to a message with nothing in it is still an
// answer to that message, and a heading with no quote under it says exactly that.
func ReplyBody(own string, t corpus.ReplyTarget) string {
	var b strings.Builder
	b.WriteString(strings.TrimRight(own, " \t\r\n"))
	b.WriteString("\n\n")

	// The heading names the instant with the words the entry's own bubble prints:
	// stamp is the page's formatter, and the label is the one it publishes
	// alongside the clock — the source's own where it agrees with the offset being
	// shown, and a numeric offset where it does not (see zones.go).
	//
	// The one case that is spelled rather than passed on is an instant nothing
	// placed: stamp then shows a UTC clock, and a page build records that as a
	// caveat of its own. A quote has no room for a source note, and naming the
	// sender's own label beside a UTC clock would be the lie zones.go exists to
	// avoid — so the caveat is the word for it.
	date, clock, label, resolved := stamp(t.TS, t.TZ, t.TZOffset)
	if !resolved {
		label = "UTC"
	}
	b.WriteString("On " + strings.TrimSpace(date+" "+clock+" "+label) + ", ")
	if who := t.Who(); who != "" {
		b.WriteString(who)
	} else {
		b.WriteString("somebody")
	}
	b.WriteString(" wrote:\n")

	if strings.TrimSpace(t.Body) == "" {
		return b.String()
	}
	for _, line := range strings.Split(strings.TrimRight(t.Body, "\r\n"), "\n") {
		line = strings.TrimSuffix(line, "\r")
		if strings.TrimSpace(line) == "" {
			// A mark rather than "> " with a trailing space: every client writes
			// the bare mark for a blank line, and trailing whitespace in a body
			// that people read as text is noise.
			b.WriteString(">\n")
			continue
		}
		// One more level in, as every client nests a quote of a quote: the message
		// being answered may itself carry quoted lines from further back, and a
		// quote that dropped them would be an edit of the message it claims to be.
		b.WriteString("> " + line + "\n")
	}
	return b.String()
}
