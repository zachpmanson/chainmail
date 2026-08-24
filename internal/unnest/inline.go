package unnest

import (
	"regexp"
	"strings"

	"golang.org/x/net/html"
)

// InlineRun is one colour-marked run inside a host's text/html body. In the
// mail this corpus reads, that is an inline reply: a participant answers a
// quoted question in red (or another distinct colour) inside the text they are
// quoting, rather than starting a fresh message. The colour is the whole
// signal — the flat text rendition keeps the words but drops that the run is
// the quoter's own answer, which is why it lands attributed to the *quoted*
// author instead.
type InlineRun struct {
	// Text is the run's text, whitespace-normalised for matching against the
	// block text it was typed into.
	Text      string
	WordCount int
}

// InlineRuns returns the colour-marked runs in a host's text/html part.
//
// The signal is an explicit foreground colour, however a given client states
// it: a style attribute carrying "color: red", or a <font color=...>. The
// default quote colour is not in the source (it is the client's rendering
// default), so a run that states a colour explicitly is a deliberate one — most
// commonly the red or blue a reply client uses for an inline answer.
//
// Only the outer run of nested colour spans is collected, so one red answer
// opening a chain of inner red formatting does not fan out into several runs of
// the same text. And a run matters only if it looks like an *answer* rather than
// a piece of the quoted text that happened to be coloured. Three further guards
// keep the detector from shredding a quoted message into fake replies:
//
//   - it must be a real *foreground* colour. A style carrying border-color: or
//     background-color: is layout, not a reply, and reading the bare "color:"
//     out of it would make every table cell and header block its own message.
//   - it must not sit inside a table cell or a hyperlink. Those are structured
//     content whose colour is a label or link styling, not the quoter's prose.
//   - it must be propositional — a sentence, a clause the quoter's dash opens,
//     or a reply word ("No.", "Thanks."). A sender's grey signature line
//     or a job title is styled text, not an interjection, and storing it would
//     attribute a reply the author never wrote.
func InlineRuns(raw string) []InlineRun {
	doc, err := html.Parse(strings.NewReader(raw))
	if err != nil {
		return nil
	}
	var out []InlineRun
	collectColourRuns(doc, &out, false)
	return out
}

const (
	minInlineWords = 2  // below this a colour run is a single red word, not an answer
	maxInlineWords = 42 // above this it is a coloured passage, not an interjection
)

// reInlineColour matches a foreground colour declaration in a style attribute.
//
// The negative class is the whole point: "color:" inside "border-color:",
// "background-color:" or "currentcolor:" is layout or a resettable value, not
// a text run somebody deliberately coloured, and counting it shreds a styled
// email into dozens of fake messages. Requiring the character before "color"
// to be neither a word nor a hyphen keeps the match honest.
var reInlineColour = regexp.MustCompile(`(?i)(^|[^\w-])color\s*:\s*(?:#(?:[0-9a-f]{3}){1,2}|rgba?\(|hsla?\(|[a-z]+)`)

// reSentenceEnd is a full stop, question mark or exclamation at the tail of the run.
var reSentenceEnd = regexp.MustCompile(`[.!?][”’"')]*$`)

// reDashLead is the quoter's dash that leads an interjection, e.g. "– Yes, Friday".
var reDashLead = regexp.MustCompile(`^[-–—]\s`)

// reParticle covers the one-or-two-word replies that carry no sentence of their
// own: "No", "Thanks", "OK", "You're welcome".
var reParticle = regexp.MustCompile(`(?i)\b(yes|no|ok|okay|sure|yep|nope|right|fine|thanks|thank you|cool|great)\b`)

// collectColourRuns walks the tree and appends each colour-marked run. When a
// node is itself a colour run, the whole subtree beneath it is that one run and
// is not re-scanned (a nested colour span carries the same words); siblings and
// everything else are scanned normally.
func collectColourRuns(n *html.Node, out *[]InlineRun, inStruct bool) {
	if n.Type == html.ElementNode && colourAttributed(n) {
		text := nodeText(n)
		w := len(strings.Fields(normaliseRun(text)))
		// inStruct is inherited: a coloured node inside a table cell or a link
		// (or a colour node that is itself the table/link) is never the reply
		// the colour detector exists to find, and neither is a run that does not
		// read like one (a grey signature line, a job title, plain quoted text).
		if w >= minInlineWords && w <= maxInlineWords && !inStruct && !structured(n) && answerLike(text) {
			*out = append(*out, InlineRun{Text: normaliseRun(text), WordCount: w})
			return
		}
	}
	if n.Type == html.ElementNode && structured(n) {
		inStruct = true
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		collectColourRuns(c, out, inStruct)
	}
}

// structured reports an element that switches its whole subtree into "not a
// reply" territory: a table cell holds labels and data, and a hyperlink's
// colour is the link's styling — neither can be the quoter's interjection.
func structured(n *html.Node) bool {
	switch strings.ToLower(n.Data) {
	case "a", "table", "td", "th", "tr":
		return true
	}
	return false
}

// answerLike reports whether a colour run reads like the quoter's typed reply
// rather than a styled fragment of the text being quoted. The quoter's answer
// is realised prose: a sentence, a short clause the dash leads into, or a
// reply particle. A signature line ("Merchant Account Manager"), a contact
// block ("DDI 03 ... | M ... | E ...") and a job title carry none of those
// and so stay part of the quoted block instead of becoming their own message.
func answerLike(s string) bool {
	t := strings.TrimSpace(s)
	if t == "" {
		return false
	}
	return reSentenceEnd.MatchString(t) || reDashLead.MatchString(t) || reParticle.MatchString(t)
}

// colourAttributed reports whether an element states a foreground colour.
func colourAttributed(n *html.Node) bool {
	for _, a := range n.Attr {
		// Attributes are string keys; there is no atom constant for "style" or
		// "color" (the atom package names elements, not attributes).
		if a.Key == "style" && reInlineColour.MatchString(a.Val) {
			return true
		}
		if a.Key == "color" && a.Val != "" {
			return true
		}
	}
	return false
}

// nodeText is the run's own text: the full text of the node, since a colour
// element may wrap several lines or a nested list.
func nodeText(n *html.Node) string {
	var b strings.Builder
	appendNodeText(n, &b)
	return b.String()
}

func appendNodeText(n *html.Node, b *strings.Builder) {
	switch n.Type {
	case html.ElementNode:
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			appendNodeText(c, b)
		}
	case html.TextNode:
		b.WriteString(n.Data)
	default:
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			appendNodeText(c, b)
		}
	}
}

// normaliseRun folds the whitespace that renders between inline nodes, so a
// run can be matched against a block's already-flattened text.
func normaliseRun(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
