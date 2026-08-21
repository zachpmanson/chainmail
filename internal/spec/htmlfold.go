package spec

import (
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// The markup side of the fold. body.go's text path splits a line sequence
// (splitBoilerplate); this moves nodes in a parsed tree, which is a different
// job with the same rules and the same notes.

// foldMarked moves the blocks a sending client marked as its signature into a
// disclosure, and reports whether it moved any.
//
// Only the marked ones: gmail_signature is a fact the client stated. A signature
// from a client that marks nothing is left to the repetition pass, which is what
// this file exists for.
//
// Detection has to run before stripChrome, which drops the class the detection
// reads; the wrapping has to run after it, so the disclosure's own class is not
// dropped with the sender's. Hence a list of nodes rather than one pass.
func markedSignatures(n *html.Node) []*html.Node {
	var out []*html.Node
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && hasClass(c, "gmail_signature") {
			out = append(out, c)
			continue
		}
		out = append(out, markedSignatures(c)...)
	}
	return out
}

// foldNodes wraps a run of consecutive siblings in a disclosure, and reports
// whether it did.
//
// It declines when nothing outside the run puts anything on the page: see
// splitBoilerplate for why a body is never folded away entirely.
func foldNodes(body *html.Node, run []*html.Node, note foldNote) bool {
	if len(run) == 0 || run[0].Parent == nil {
		return false
	}
	if !hasContentOutside(body, run) {
		return false
	}
	parent, before := run[0].Parent, run[len(run)-1].NextSibling
	det := &html.Node{Type: html.ElementNode, DataAtom: atom.Details, Data: "details",
		Attr: []html.Attribute{{Key: "class", Val: "sig"}}}
	sum := &html.Node{Type: html.ElementNode, DataAtom: atom.Summary, Data: "summary",
		Attr: []html.Attribute{{Key: "title", Val: note.title}}}
	sum.AppendChild(&html.Node{Type: html.TextNode, Data: note.label})
	det.AppendChild(sum)
	// A wrapper for the folded content so one CSS rule can indent it and one can
	// reveal it in print, whichever path built the fold.
	inner := &html.Node{Type: html.ElementNode, DataAtom: atom.Div, Data: "div",
		Attr: []html.Attribute{{Key: "class", Val: "sigbd"}}}
	det.AppendChild(inner)
	for _, n := range run {
		parent.RemoveChild(n)
		inner.AppendChild(n)
	}
	if before != nil {
		parent.InsertBefore(det, before)
	} else {
		parent.AppendChild(det)
	}
	return true
}

// foldRepeatedTail moves the trailing markup carrying a repeated block into a
// disclosure, and reports whether it did.
//
// The block was detected in the text and is applied to the markup, because the
// text is where the repetition is legible. The same signature arrives as
// different bytes from one sender in one month — a client rewrites its inline
// styles, an image URL gains a cache key, a table gains a row — so matching
// markup would split one block into several, each below the threshold. Only 24
// senders in this corpus have a byte-identical html tail three times over,
// against 69 with a repeated text tail.
//
// Where the block starts is found by its own first line rather than by counting
// lines back from the end, because the two renditions of one message do not
// agree on how many lines it has: an anchor in a Word signature carries a
// newline inside its text node that the text/plain part never had, so counting
// lands a line early and the seam moves. The line the block opens on is the same
// text in both.
//
// The fold has to start at a sibling boundary, so a block that begins partway
// inside one element is not folded at all. Erring the other way would put the
// sender's closing sentence behind the disclosure, which is the one outcome
// worse than leaving a signature on screen.
func foldRepeatedTail(body *html.Node, bf bodyFold) bool {
	if len(bf.lines) == 0 {
		return false
	}
	host := foldHost(body)
	var kids []*html.Node
	for c := host.FirstChild; c != nil; c = c.NextSibling {
		kids = append(kids, c)
	}
	want, at, best := len(bf.lines), -1, 0
	tail := 0
	for i := len(kids) - 1; i >= 0; i-- {
		tail += visibleLines(kids[i])
		if !startsBlock(kids[i], bf.lines) {
			continue
		}
		// The same opening line can appear twice — a sender whose sign-off names
		// them and whose signature block names them again. The block is a suffix of
		// the message, so the candidate holding about as many lines as the block
		// does is the block.
		if at < 0 || abs(tail-want) < abs(best-want) {
			at, best = i, tail
		}
	}
	if at < 0 {
		return false
	}
	return foldNodes(body, kids[at:], bf.note)
}

// startsBlock reports whether a node opens on the words the block opens on.
//
// Words rather than lines, and a prefix rather than an equality, because the two
// renditions of one message disagree about where a line ends: Word writes a
// signature's name and job title as two spans that the text/plain part flattened
// onto one line and the markup reader splits at neither, and an anchor in the
// same block carries a newline inside its text node that the text never had.
// Comparing whole lines fails on every one of those; comparing what the block
// starts with survives them.
//
// Whitespace is collapsed for the same reason — the html carries a run of spaces
// or a non-breaking space where the flattened text carries one ordinary space.
func startsBlock(n *html.Node, block []string) bool {
	head := collapse(firstVisibleLine(n))
	if head == "" {
		return false
	}
	// One direction each way: the markup line stops short of the block's first
	// line, or it runs past it because the markup put two of the block's lines in
	// one element.
	return strings.HasPrefix(collapse(strings.Join(block, " ")), head) ||
		strings.HasPrefix(head, collapse(block[0]))
}

func collapse(s string) string { return strings.Join(strings.Fields(s), " ") }

func firstVisibleLine(n *html.Node) string {
	for _, l := range strings.Split(textHead(n, boundaryTextHead), "\n") {
		if strings.TrimSpace(l) != "" {
			return l
		}
	}
	return ""
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// foldHost descends past the wrappers a client puts around a whole message, to
// the node whose children are the message's own blocks.
//
// Without it there is nothing to fold at: most clients emit one <div> holding
// everything, so the body has a single child and a trailing run of siblings does
// not exist at that level.
//
// Only through a <div>, which is the element clients wrap with. Descending into
// anything else means descending into content: a body that is one <p> would have
// its trailing <br>-separated lines folded out of the middle of the paragraph,
// which both splits a sentence the sender wrote and puts a block element inside
// a <p>, where the parser will move it back out again.
func foldHost(body *html.Node) *html.Node {
	for {
		var only *html.Node
		for c := body.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.TextNode && strings.TrimSpace(c.Data) == "" {
				continue
			}
			if only != nil {
				return body
			}
			only = c
		}
		if only == nil || only.Type != html.ElementNode || only.DataAtom != atom.Div ||
			only.FirstChild == nil {
			return body
		}
		body = only
	}
}

// visibleLines counts the non-blank lines a subtree puts on the page, reading it
// the way sentinel detection does — block elements end a line, so a signature
// laid out with <br> counts as its several lines rather than as one.
func visibleLines(n *html.Node) int {
	count := 0
	for _, l := range strings.Split(textHead(n, 1<<20), "\n") {
		if strings.TrimSpace(l) != "" {
			count++
		}
	}
	return count
}

// hasContentOutside reports whether anything outside the given subtrees puts
// content on the page.
func hasContentOutside(n *html.Node, skip []*html.Node) bool {
	for _, s := range skip {
		if n == s {
			return false
		}
	}
	switch n.Type {
	case html.TextNode:
		return strings.TrimSpace(n.Data) != ""
	case html.ElementNode:
		if n.DataAtom == atom.Img {
			return true
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if hasContentOutside(c, skip) {
			return true
		}
	}
	return false
}
