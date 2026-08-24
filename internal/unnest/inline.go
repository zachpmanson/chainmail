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
// The signal is an explicit colour, however a given client states it: a style
// attribute carrying "color:", or a <font color=...>. The default quote colour
// is not in the source (it is the client's rendering default), so any run that
// says a colour explicitly is a deliberate one — most commonly the red or blue
// reply clients use for an inline answer.
//
// Only the outer run of nested colour spans is collected, so one red answer
// opening a chain of inner red formatting does not fan out into several runs of
// the same text. And a run matters only if it looks like an answer rather than
// a piece of the quoted text that happened to be coloured: it must be short
// prose, long enough to carry a real word and short enough not to be a full
// quoted paragraph.
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

var reInlineColour = regexp.MustCompile(`(?i)color\s*:`)

// collectColourRuns walks the tree and appends each colour-marked run. inside
// tells a node it is beneath an already-collected colour span, so a nested
// colour does not become a second run of the same answer. It returns whether
// this subtree held a colour run, so the caller can mark everything beneath one.
func collectColourRuns(n *html.Node, out *[]InlineRun, inside bool) bool {
	if inside {
		return true
	}
	if n.Type == html.ElementNode && colourAttributed(n) {
		text := nodeText(n)
		w := len(strings.Fields(normaliseRun(text)))
		if w >= minInlineWords && w <= maxInlineWords {
			*out = append(*out, InlineRun{Text: normaliseRun(text), WordCount: w})
			// The whole subtree is the same run; do not scan inside it again.
			return true
		}
	}
	held := false
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if collectColourRuns(c, out, inside || held) {
			held = true
		}
	}
	return held
}

// colourAttributed reports whether an element states a colour itself.
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
