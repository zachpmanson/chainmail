package spec

import (
	"html"
	"regexp"
	"strings"

	"github.com/zachpmanson/chainmail/internal/boiler"
	"github.com/zachpmanson/chainmail/internal/unnest"
)

// Bodies are the one field where the corpus holds a message in one form and the
// schema wants another, so the conversion lives here rather than being pushed
// onto the renderer.
//
// The shaping applied to a text body differs by where the text came from,
// because the same newline means different things in mail and in Slack. See
// bodyStyle. Text is escaped before any markup is added and the only tags
// emitted are the ones written in this file; markup taken from a sender's own
// html part is a separate matter, and htmlbody.go says what is done with it.

// bodyHTML renders one entry's stored body as the HTML the schema expects, or
// "" when the entry genuinely has no text — a Slack file-only post, a forward
// whose sender wrote nothing above the quote. An empty body is a real fact about
// the message and is emitted as an empty string rather than an empty <p>, which
// would render as a bubble claiming content that does not exist.
//
// Three sources, in order of how close each is to what the sender actually sent:
// their own text/html part; the same message quoted inside somebody else's part,
// where the markup survives even though this entry has none of its own; and last
// the text rendition, shaped by textToHTML. Every markup path can decline —
// see htmlbody.go and htmlrecover.go for what makes it decline — and declining
// lands here, on the text, which is plain but never wrong.
//
// The markup is emitted unsanitised. That is stated where the choice is made,
// at the top of htmlbody.go, and tracked as issue #14.
//
// It records on r whether it folded a signature or a disclaimer. Only this pass
// knows — the fold can be detected and then declined, at the seam of a markup
// tree or on a body that is nothing but the block — and the source note has to
// report entries that were folded rather than entries that could have been.
func bodyHTML(r *entryRow) string {
	st := styleFor(r)
	bf := blockFor(r, st)
	if r.BodyHTML != "" {
		if s, folded := htmlBody(r.BodyHTML, st, bf); s != "" {
			r.Folded = folded
			return s
		}
	} else if !r.Direct {
		// Only for an entry that was never in the mailbox. A message that was
		// there and has no html part was sent as plain text, and the markup a
		// quoting client wrapped it in later is that client's, not the sender's.
		if s, folded := recoverHTML(r.BodyText, r.HostHTML, bf); s != "" {
			r.Folded = folded
			return s
		}
	}
	s, folded := textToHTML(r.BodyText, st, r.Fold)
	r.Folded = folded
	return s
}

// bodyStyle says how far a body's text may be reshaped. Both switches are off
// unless the provenance of the text justifies them, because each one is a claim
// about what a newline in that text means.
type bodyStyle struct {
	// peel drops everything from the first quoted-history boundary onward.
	//
	// Only safe where unnest has already mined that history into entries of its
	// own: the text is then not lost, it is the rest of the page. On any other
	// source this would delete content that appears nowhere else.
	peel bool
	// reflow joins the lines of a hard-wrapped paragraph back into one.
	//
	// Only true for mail, where a newline inside a paragraph is the sending
	// client's 72-column wrap and means nothing. In Slack a newline is a key the
	// author pressed, so joining lines there would rewrite what they wrote.
	reflow bool
}

func styleFor(r *entryRow) bodyStyle {
	if r.Source != "mail" {
		return bodyStyle{}
	}
	// A quoted entry's stored text is already one peeled block (internal/
	// mailingest/quoted.go), so peeling again could only misfire — a hand-typed
	// "From:"/"To:" pair in someone's prose would read as a boundary and blank
	// the entry.
	return bodyStyle{peel: r.Direct, reflow: true}
}

// flowWidth is the shortest line that may be treated as a wrap rather than as a
// break the author chose.
//
// Measured on this corpus: across mail lines followed by a non-blank line, the
// count per length is flat below 66 (400–1200 a length) and then climbs to a
// plateau of 1200–2100 between 70 and 76 before collapsing past 80. That plateau
// is the population of wrapped lines and 66 is where it starts. Setting the
// bound lower would join lines that were short because the author stopped
// there; not reflowing at all leaves every mail paragraph as a 72-column ladder
// inside a fluid-width bubble, which is what the ragged pages showed.
const flowWidth = 66

// textToHTML renders a body's text, and reports whether it folded a trailing
// block of boilerplate. Peel reports line ranges precisely, and boiler.Lines
// keeps them, so the quote depths this path renders as blockquotes survive — the
// block texts Peel also returns have had their markers stripped.
func textToHTML(text string, st bodyStyle, f boiler.Fold) (string, bool) {
	if strings.TrimSpace(text) == "" {
		return "", false
	}
	lines, ok := boiler.Lines(text, st.peel)
	if !ok {
		// The body opens on a boundary: the sender forwarded or replied without
		// writing anything of their own above it.
		return "", false
	}
	lines, tail, note := splitBoilerplate(lines, f)

	var b strings.Builder
	render(&b, paragraphs(lines), 0, st)
	if b.Len() == 0 {
		return "", false
	}
	if len(tail) == 0 {
		return b.String(), false
	}
	var t strings.Builder
	render(&t, paragraphs(tail), 0, st)
	if t.Len() == 0 {
		return b.String(), false
	}
	b.WriteString(foldHTML(note, t.String()))
	return b.String(), true
}

// para is a run of lines that belong together: one paragraph, at one quote
// depth. A blank line ends it, and so does a change of depth — quoted text
// beginning mid-paragraph is a different voice, not a continuation.
type para struct {
	depth int
	lines []unnest.Line
}

func paragraphs(lines []unnest.Line) []para {
	var out []para
	var cur *para
	for _, l := range lines {
		if strings.TrimSpace(l.Text) == "" {
			cur = nil
			continue
		}
		if cur == nil || cur.depth != l.Depth {
			out = append(out, para{depth: l.Depth})
			cur = &out[len(out)-1]
		}
		cur.lines = append(cur.lines, l)
	}
	return out
}

// render emits paragraphs at quote depth `base`, wrapping each deeper run in a
// blockquote and recursing into it, so that ">>" nests instead of flattening to
// the same indent as ">".
//
// Quoted lines that survive to here are intra-message quotes: for mail, the
// trailing history was already peeled, and what remains is material the sender
// quoted inside their own text — a pull-quote from a pull request, a line from a
// ticket. Rendering it as prose would strip the "> " markers and silently merge
// someone else's words into the sender's paragraph.
func render(b *strings.Builder, ps []para, base int, st bodyStyle) {
	for i := 0; i < len(ps); {
		if ps[i].depth <= base {
			if s := paraHTML(ps[i].lines, st); s != "" {
				b.WriteString("<p>" + s + "</p>")
			}
			i++
			continue
		}
		j := i
		for j < len(ps) && ps[j].depth > base {
			j++
		}
		b.WriteString("<blockquote>")
		render(b, ps[i:j], base+1, st)
		b.WriteString("</blockquote>")
		i = j
	}
}

func paraHTML(lines []unnest.Line, st bodyStyle) string {
	var b strings.Builder
	for i, l := range lines {
		if i > 0 {
			if st.reflow && wrapped(lines[i-1].Text, l.Text) {
				b.WriteString(" ")
			} else {
				b.WriteString("<br>")
			}
		}
		b.WriteString(escapeAndLink(strings.TrimRight(l.Text, " \t")))
	}
	return b.String()
}

var reBullet = regexp.MustCompile(`^([-*•·+o]\s|\(?\d+[.)]\s|[a-z][.)]\s)`)

// wrapped reports whether `next` continues `cur` as the tail of one wrapped
// line, rather than starting a line of its own.
//
// The length of `cur` is the primary signal (see flowWidth); the rest are vetoes
// for the layout that reflowing destroys. Indentation and runs of spaces are
// what hold an address block, a figures column and a pasted code snippet
// together, and every one of those has short lines only by coincidence — a
// 70-column table row would otherwise be joined to the row beneath it and the
// column would be gone.
func wrapped(cur, next string) bool {
	if len([]rune(strings.TrimRight(cur, " \t"))) < flowWidth {
		return false
	}
	if indented(cur) || indented(next) {
		return false
	}
	if columnar(cur) || columnar(next) {
		return false
	}
	return !reBullet.MatchString(next)
}

func indented(s string) bool { return strings.HasPrefix(s, " ") || strings.HasPrefix(s, "\t") }

// columnar reports text aligned with whitespace rather than written as prose.
// Two consecutive spaces mid-line is a column gap; prose has one.
func columnar(s string) bool {
	return strings.Contains(s, "\t") || strings.Contains(strings.TrimSpace(s), "  ")
}

// reURL matches a bare http(s) URL. The scheme list is the whole security
// boundary for links: matching "javascript:" or "data:" here would put a script
// behind a link the reader has every reason to trust, and no other scheme in
// this corpus is worth a click. Brackets, quotes and angle brackets end a URL
// because mail clients wrap URLs in them ("<https://…>", "(https://…)").
var reURL = regexp.MustCompile("(?i)https?://[^\\s<>\"'`{}\\[\\]|\\\\^]+")

// escapeAndLink escapes text and links the URLs in it.
//
// The order is what makes it safe: the input is split on URL matches and *every*
// piece is escaped, so nothing from a message reaches the page as markup —
// neither the prose around a URL nor the URL in the href it becomes. Escaping
// the whole string first and linkifying the result would be no safer and would
// have to tell "&amp;" inside a query string apart from "&lt;" ending one, which
// is exactly the distinction the raw text still makes for free.
func escapeAndLink(s string) string {
	var b strings.Builder
	for s != "" {
		m := reURL.FindStringIndex(s)
		if m == nil {
			b.WriteString(html.EscapeString(s))
			break
		}
		b.WriteString(html.EscapeString(s[:m[0]]))
		u := trimURLTail(s[m[0]:m[1]])
		e := html.EscapeString(u)
		b.WriteString(`<a href="` + e + `" target="_blank" rel="noopener">` + e + `</a>`)
		s = s[m[0]+len(u):]
	}
	return b.String()
}

// trimURLTail gives back the sentence punctuation a URL at the end of a sentence
// swallows. A trailing ")" is kept only when the URL opened one, since a URL may
// legitimately contain balanced parentheses.
func trimURLTail(u string) string {
	for u != "" {
		last := u[len(u)-1]
		if strings.IndexByte(".,;:!?'\"", last) >= 0 {
			u = u[:len(u)-1]
			continue
		}
		if last == ')' && strings.Count(u, ")") > strings.Count(u, "(") {
			u = u[:len(u)-1]
			continue
		}
		break
	}
	return u
}
