package spec

import (
	"strings"

	"github.com/zachpmanson/chainmail/internal/corpus"
)

// Reply is one answer in the two forms it is sent in: the plain text a transcript
// shows and the HTML a mail client renders.
//
// The reader's own words, the attribution line and the reader's half of the
// message are composed once and rendered twice, which is the point of the type:
// they cannot come to say different things. The quote is the exception, and
// deliberately so — it is the message being answered, and each form of the reply
// quotes that message from its own rendering of it: the HTML part carries the
// sender's own markup (see quoteHTML), the text part its text. Converting one
// into the other would be a second reading of the message, and mail already
// carries both readings; a quote that flattened markup would be the text
// rendition of a transcript.
type Reply struct {
	// Text is the reply as plain text: the part every client reads, and the one the
	// plan shows (POST /v1/send without `confirm`).
	Text string
	// HTML is the same reply marked up: paragraphs, the attribution as its own
	// block, and the message being answered inside a blockquote. It is not a
	// reformatting of the text — nothing is reflowed, trimmed or re-wrapped — and it
	// is always non-empty, because a reply is sent in both forms.
	HTML string
}

// ComposeReply is what a reply says: the reader's own words, then the message they
// are answering, quoted and attributed the way a mail client quotes — as text, and
// as HTML.
//
// It is composed here rather than in the browser for the reason the recipient
// line and the rendered body are (see render.go): the words an entry's clock is
// written in are this package's, and a quote's heading names the same sender and
// the same instant its own bubble prints. Composing it anywhere else would be a
// second renderer of one message, which is exactly what the pane and a page build
// were made to share. The send handler is the only caller, and the body it hands
// to the mailbox is this — so what the reader is shown as the plan is the message
// itself rather than a draft of one.
//
// The shape is the transcript's own:
//
//	The reader's words.
//
//	On Mon 2 Jan 2026 15:04 AEDT, Ada Okoye <ada@loomworks.example> wrote:
//	> the message being answered, a line at a time
//
// and the same shape in HTML: the words as paragraphs, the heading as a block of
// its own, and the quoted message inside <blockquote class="gmail_quote"> — the
// class a client (Gmail among them) keys off to fold a quote away, so the fold is
// the client's decision and the markup is not pretending to be formatted prose.
// What goes inside that blockquote is the answered message's own markup when it had
// any, and paragraphs of its text when it did not (see quoteHTML): the same
// message, quoted from the form that message was sent in rather than from a
// rendition of the other one.
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
func ComposeReply(own string, t corpus.ReplyTarget) Reply {
	words := strings.TrimRight(own, " \t\r\n")
	head := replyHeading(t)

	var text strings.Builder
	text.WriteString(words)
	text.WriteString("\n\n")
	text.WriteString(head)
	text.WriteString("\n")
	text.WriteString(quoteText(t.Body))

	var html strings.Builder
	html.WriteString(blocks(words))
	html.WriteString("\n<p>")
	html.WriteString(escapeHTML(head))
	html.WriteString("</p>\n")
	if quoted := quoteHTML(t.HTML, t.Body); quoted != "" {
		html.WriteString("<blockquote class=\"gmail_quote\">\n")
		html.WriteString(quoted)
		html.WriteString("\n</blockquote>\n")
	}

	return Reply{Text: text.String(), HTML: html.String()}
}

// replyHeading is the line a quote is introduced by: the instant in the page's own
// words and the sender the way a client writes a person.
//
// The instant is named with the words the entry's own bubble prints: stamp is the
// page's formatter, and the label is the one it publishes alongside the clock — the
// source's own where it agrees with the offset being shown, and a numeric offset
// where it does not (see zones.go).
//
// The one case that is spelled rather than passed on is an instant nothing placed:
// stamp then shows a UTC clock, and a page build records that as a caveat of its
// own. A quote has no room for a source note, and naming the sender's own label
// beside a UTC clock would be the lie zones.go exists to avoid — so the caveat is
// the word for it.
func replyHeading(t corpus.ReplyTarget) string {
	date, clock, label, resolved := stamp(t.TS, t.TZ, t.TZOffset)
	if !resolved {
		label = "UTC"
	}
	who := t.Who()
	if who == "" {
		who = "somebody"
	}
	return "On " + strings.TrimSpace(date+" "+clock+" "+label) + ", " + who + " wrote:"
}

// quoteHTML is the message being answered as the HTML part of a reply quotes it:
// the sender's own text/html part, allowlisted, in place of the paragraphs its
// text would become.
//
// This is the difference a reader notices first. A reply to a message that had
// markup used to carry that message as escaped text — a table became its cells'
// lines, a link its label, and the sender's own quoted message a run of "&gt;" —
// so the answer to an HTML mail read as a transcript of one. The markup is there
// in the corpus (it is the same column the pane renders, and the same part
// `/original` serves), so the quote is built from it.
//
// The pass is the pane's own allowlist (sanitise.go) and nothing else: no
// presentation is applied, because a quote is the message as it was sent rather
// than a reading of it — signature folds, quote trimming and the rest are the
// reader's screen, and the recipient is entitled to the same words the reader was
// shown. What the allowlist removes is what could act rather than say.
//
// It is an allowlist of attributes as well as of elements, so a message's own
// quoted message keeps its nesting and loses the class a client folds it by: the
// reply's outer blockquote (written here, not sanitised) is still what a client
// folds, and widening the pane's rule for a quote's benefit is not this file's to do.
//
// It falls back to the text rendition when the part is absent — the common case,
// since most mail is text — and when it is present but holds nothing a quote could
// show, like a client's <style> block with no body after it. Text is plain but never
// wrong, and a quote with no source at all is still not a quote: a message with
// neither form says nothing, and the caller emits no blockquote for it.
func quoteHTML(raw, text string) string {
	if safe := sanitiseBody(raw); strings.TrimSpace(safe) != "" {
		return safe
	}
	if strings.TrimSpace(text) == "" {
		return ""
	}
	return blocks(strings.TrimRight(text, "\r\n"))
}

// quoteText is the message being answered, one line at a time, one level in.
func quoteText(body string) string {
	if strings.TrimSpace(body) == "" {
		return ""
	}
	var b strings.Builder
	for _, line := range strings.Split(strings.TrimRight(body, "\r\n"), "\n") {
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

// blocks is text as HTML paragraphs.
//
// This is the whole of the markup a reply adds to the reader's words and the
// message being answered: a blank line starts a new paragraph and a single line
// break within one is a <br>, which is what the text already means. Nothing is
// interpreted — no URLs are linked, no emphasis is guessed at, and anything that
// looks like a tag was typed by a person and goes out as the characters they
// typed (see escapeHTML). A mail client that renders this must be able to trust
// that nothing in it came from anywhere but the two messages themselves.
func blocks(text string) string {
	var out []string
	for _, para := range splitParagraphs(text) {
		lines := strings.Split(para, "\n")
		for i, line := range lines {
			lines[i] = escapeHTML(strings.TrimSuffix(line, "\r"))
		}
		out = append(out, "<p>"+strings.Join(lines, "<br>")+"</p>")
	}
	return strings.Join(out, "\n")
}

// splitParagraphs cuts text at its blank lines, keeping the text itself whole:
// a paragraph is what sits between two of them.
func splitParagraphs(text string) []string {
	var paras []string
	var cur []string
	flush := func() {
		if len(cur) > 0 {
			paras = append(paras, strings.Join(cur, "\n"))
			cur = nil
		}
	}
	for _, line := range strings.Split(strings.TrimRight(text, "\r\n"), "\n") {
		if strings.TrimSpace(line) == "" {
			flush()
			continue
		}
		cur = append(cur, line)
	}
	flush()
	return paras
}

// escapeHTML makes text safe to put in element content, which is every place this
// package puts any: the quotes and the apostrophes are left as they are because
// nothing here is written into an attribute, and the three characters that would
// otherwise be read as markup are the three that are escaped.
func escapeHTML(text string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(text)
}
