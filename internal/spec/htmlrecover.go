package spec

import (
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/zachpmanson/chainmail/internal/textsim"
)

// Most of this corpus is messages that exist only inside someone's quote: on a
// real page, 41 of 57 entries were mined out of quoted history and have no
// stored markup of their own. Their markup is not lost, though. A client that
// quotes a message quotes its *markup*, so the table someone laid out survives
// inside the reply that answered them — in that reply's text/html part, which is
// stored.
//
// So a quoted entry's body can be recovered from a message it was found inside.
// The correlation is by content and never by position: the entry's own text is
// the needle, and the blocks of the host's markup are the haystack. That choice
// is the whole safety argument. unnest finds seven blocks in one host's text
// where the DOM has four containers with nesting under them, so lining the two
// peels up by index would hand an entry somebody else's words while looking
// entirely correct. Matching on the words themselves cannot: it either finds the
// block that says what this entry says, or it finds nothing.
//
// Nothing is a good answer. Every failure here falls back to the text path,
// which is what the page showed before: plain, and never wrong. Measured over
// this corpus, 785 of 1,710 unspooled mail entries recover their markup: 411 are
// too short to identify at all, and the rest find no block that clears the
// bounds below. A conservative rate is the intended outcome — the failure this
// is tuned against is not a plain bubble, it is a confident wrong one.

// Confidence bounds. Deliberately strict — a miss costs a plain-text bubble,
// while a false match puts one person's words under another person's name on a
// page whose entire purpose is establishing who said what when.
const (
	// minNeedleTokens is the shortest body worth correlating. "Thanks, will do"
	// matches every second message in a mailbox, and picking the wrong one of two
	// identical replies is undetectable by any test here.
	minNeedleTokens = 25
	// minRecall: nearly every word of the entry must appear in the block. Not
	// every word, because the text rendition is not a lossless view of the
	// markup — a link arrives as "text <url>", an image as a placeholder, a table
	// cell boundary as whitespace.
	minRecall = 0.85
	// minPrecision: the block may not be much larger than the entry. This is what
	// rejects a block that holds this entry *and* the trail beneath it, which
	// would otherwise score a perfect recall.
	minPrecision = 0.7
	// The opening has to line up too, and in order, because a bag of words is
	// exactly what shared boilerplate defeats — see textsim.HeadSimilarity for
	// the case that establishes it. The run is the entry's first tokens and the
	// window is how far into the block they are looked for.
	headRun    = 8
	headWindow = 48
	minHeadRun = 0.75
	// ambiguityMargin is how much better the winner must be than any rival that
	// says something different.
	ambiguityMargin = 0.1
	// sameContent is the similarity above which two blocks are one message quoted
	// twice rather than two different messages. One message quoted by three
	// people yields three near-identical blocks, and that is agreement rather
	// than ambiguity.
	sameContent = 0.9
)

// recoverHTML returns the sender's markup for a quoted entry, or "" when no host
// offers a confident match. The second return says whether a signature or a
// disclaimer was folded out of it.
func recoverHTML(text string, hosts []string, bf bodyFold) (string, bool) {
	needle := textsim.Tokens(text)
	if len(needle) < minNeedleTokens {
		return "", false
	}
	var best, rival *candidate
	for _, h := range hosts {
		for _, c := range candidates(h) {
			c.score(needle)
			if c.recall < minRecall || c.precision < minPrecision || c.headMatch < minHeadRun {
				continue
			}
			switch {
			case best == nil || c.f1 > best.f1:
				best, rival = c, best
			case rival == nil || c.f1 > rival.f1:
				rival = c
			}
		}
	}
	if best == nil {
		return "", false
	}
	if rival != nil && best.f1-rival.f1 < ambiguityMargin &&
		textsim.Similarity(best.tokens, rival.tokens) < sameContent {
		// Two blocks of the trail fit this entry and they do not say the same
		// thing, so one of them is a different message. Which one cannot be told
		// from here, and guessing is the one outcome worse than plain text.
		return "", false
	}
	return renderBody(best.extract(), bf)
}

// candidate is one block of a host's markup: a run of sibling nodes bounded by
// quoted-history boundaries, holding the words of one message.
//
// A run rather than a single node because clients disagree about whether a
// quoted message is a container. Gmail nests one: the message is the content of
// a blockquote. Outlook states a header block and then writes the message as the
// siblings that follow it, so the block has no element of its own and the only
// thing that delimits it is the next header block.
type candidate struct {
	parent *html.Node
	first  *html.Node // inclusive
	end    *html.Node // exclusive; nil runs to the last sibling
	tokens []string

	recall    float64
	headMatch float64
	precision float64
	f1        float64
}

func (c *candidate) score(needle []string) {
	c.headMatch = textsim.HeadSimilarity(needle, c.tokens, headRun, headWindow)

	inter := float64(textsim.Overlap(needle, c.tokens))
	c.recall = inter / float64(len(needle))
	if len(c.tokens) > 0 {
		c.precision = inter / float64(len(c.tokens))
	}
	if c.recall+c.precision > 0 {
		c.f1 = 2 * c.recall * c.precision / (c.recall + c.precision)
	}
}

// extract moves a run out of its document and into a node of its own, cutting
// the trail that follows it inside the run — the deeper history the entry
// quoted, which is on the page as its own entries.
func (c *candidate) extract() *html.Node {
	holder := &html.Node{Type: html.ElementNode, DataAtom: atom.Div, Data: "div"}
	for n := c.first; n != nil && n != c.end; {
		next := n.NextSibling
		c.parent.RemoveChild(n)
		holder.AppendChild(n)
		n = next
	}
	if b := firstBoundary(holder); b != nil {
		cutFrom(holder, b)
	}
	return holder
}

// candidates parses a host part and returns the blocks of quoted history in it.
//
// Only runs that a boundary delimits are offered: a quoted message is always
// separated from its host's own text by one, so enumerating every element
// instead would offer a thousand nested <div>s whose words differ from their
// parent's by a whitespace node.
func candidates(raw string) []*candidate {
	doc, err := html.Parse(strings.NewReader(raw))
	if err != nil {
		return nil
	}
	body := findBody(doc)
	if body == nil {
		return nil
	}
	var out []*candidate
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		spans := boundarySpans(n)
		if len(spans) > 0 || isBoundary(n) {
			out = append(out, runs(n, spans)...)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.ElementNode {
				walk(c)
			}
		}
	}
	walk(body)
	return out
}

// runs splits a node's children at its boundaries, one candidate per gap.
func runs(n *html.Node, spans []span) []*candidate {
	var out []*candidate
	add := func(first, end *html.Node) {
		if first == nil || first == end {
			return
		}
		c := &candidate{parent: n, first: first, end: end}
		c.tokens = textsim.Tokens(textUntilBoundary(first, end))
		if len(c.tokens) > 0 {
			out = append(out, c)
		}
	}
	first := n.FirstChild
	for _, sp := range spans {
		add(first, sp.first)
		first = sp.end
	}
	add(first, nil)
	return out
}

// textUntilBoundary reads a run as text and stops at the first boundary inside
// it, wherever in the subtree that is. Stopping is what makes a block's words
// its own: a block runs until the next quote begins, exactly as unnest reads it
// in the text.
func textUntilBoundary(first, end *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node) bool
	walk = func(n *html.Node) bool {
		if isBoundary(n) {
			return false
		}
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
			return true
		}
		if n.Type == html.ElementNode && breaksLine(n.DataAtom) {
			b.WriteString("\n")
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if !walk(c) {
				return false
			}
		}
		return true
	}
	for n := first; n != nil && n != end; n = n.NextSibling {
		if !walk(n) {
			break
		}
	}
	return b.String()
}
