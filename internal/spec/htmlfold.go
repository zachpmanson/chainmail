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
// The disclosure always begins at a line boundary — a <br>, or the edge of a
// block element — and never partway through a line. A line the sender wrote is
// the smallest thing this may not divide: half of a closing sentence behind the
// disclosure is the one outcome worse than leaving a signature on screen, and
// the words the block opens on are how the seam is placed at all.
//
// Where that boundary is not already the edge of one of the host's children, the
// elements between it and the host are divided at it (see splitTail). A signature
// that a client laid out as <br>-separated lines inside one paragraph has no
// sibling boundary anywhere in it, and on this corpus that is the common shape:
// of the mailbox messages whose block the corpus finds, 123 open it on a child of
// the host and 395 open it inside one. Dividing reaches 36 of those; the rest are
// declined by the rules below, most of them automated mail laid out in nested
// tables, whose flattened text lines correspond to nothing in the markup.
func foldRepeatedTail(body *html.Node, bf bodyFold) bool {
	if len(bf.lines) == 0 {
		return false
	}
	host := foldHost(body)
	if run := tailRun(host, bf.lines); len(run) > 0 && foldNodes(body, run, bf.note) {
		return true
	}
	return foldSplitTail(body, host, bf)
}

// tailRun is the run of the host's trailing children that the block covers, or
// nil when no child of the host opens it.
func tailRun(host *html.Node, block []string) []*html.Node {
	var kids []*html.Node
	for c := host.FirstChild; c != nil; c = c.NextSibling {
		kids = append(kids, c)
	}
	want, at, best := len(block), -1, 0
	tail := 0
	for i := len(kids) - 1; i >= 0; i-- {
		tail += visibleLines(kids[i])
		if !startsBlock(kids[i], block) {
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
		return nil
	}
	return kids[at:]
}

// foldSplitTail divides the elements that hold the block's opening line, so that
// the line begins a child of the host, and folds from there.
//
// The block is a suffix of the message, so what the disclosure has to hold is
// everything from one point in document order onward — which is a run of
// siblings only when the client happened to end an element there. Dividing an
// element at a line boundary it already contains produces two elements that read
// as the one did: the sender's <br> between two lines becomes the seam between
// two paragraphs. Nothing is reordered and no text is dropped, which is what
// makes this a different act from folding mid-line.
func foldSplitTail(body, host *html.Node, bf bodyFold) bool {
	n := splitPoint(host, bf.lines)
	if n == nil {
		return false
	}
	// Asked before the split, because a fold that will be declined must not leave
	// the tree divided: see foldNodes for why a body is never folded entirely.
	if !hasContentBefore(host, n) {
		return false
	}
	top := splitTail(host, n)
	if p := top.PrevSibling; p != nil {
		// The break the block used to follow is now the last thing in what stays on
		// screen, separating nothing. It is the case trimTrailingChrome exists for.
		trimTrailingChrome(p)
	}
	var run []*html.Node
	for c := top; c != nil; c = c.NextSibling {
		run = append(run, c)
	}
	return foldNodes(body, run, bf.note)
}

// splitPoint is the node inside the host that the block opens on, or nil when
// the block opens partway along a line.
//
// A candidate has to open on the block's words *and* follow a line boundary.
// Both, because either alone places a seam the sender did not write: the words
// alone would divide a line in the middle of itself, and a boundary alone would
// divide at whichever break came last.
func splitPoint(host *html.Node, block []string) *html.Node {
	want := len(block)
	var best *html.Node
	bestTail := 0
	var walk func(*html.Node)
	walk = func(p *html.Node) {
		for c := p.FirstChild; c != nil; c = c.NextSibling {
			if opensAfterBreak(c) && startsBlock(c, block) && divisible(host, c) {
				// Closest to the block's length wins, as in tailRun and for the same
				// reason: an opening line that appears twice is disambiguated by how
				// much of the message follows it.
				if t := suffixLines(host, c); best == nil || abs(t-want) < abs(bestTail-want) {
					best, bestTail = c, t
				}
			}
			walk(c)
		}
	}
	walk(host)
	return best
}

// opensAfterBreak reports whether a node starts a line: something before it
// among its siblings ends one.
//
// The empty inline elements between are stepped over — Word writes an <o:p></o:p>
// after every line of a signature — because they put nothing on the page and so
// end nothing. A node that is the first of its parent's children reports false:
// the question is then about the parent, which is a candidate in its own right
// and answers it there.
func opensAfterBreak(n *html.Node) bool {
	for p := n.PrevSibling; p != nil; p = p.PrevSibling {
		switch p.Type {
		case html.ElementNode:
			if breaksLine(p.DataAtom) {
				return true
			}
			if hasContent(p) {
				return false
			}
		case html.TextNode:
			if strings.TrimSpace(p.Data) != "" {
				return strings.HasSuffix(strings.TrimRight(p.Data, " \t"), "\n")
			}
			if strings.Contains(p.Data, "\n") {
				return true
			}
		}
	}
	return false
}

// divisible reports whether the elements between a node and the host may be
// divided at it.
//
// The listed elements are the ones a client wraps a line in, and two of any of
// them read as the one did. A table is the case that rules the others out: a
// signature laid out as rows and cells divides into two tables whose columns no
// longer line up, so a block inside one is left in view rather than restructured.
// An <a> is excluded for the same kind of reason — dividing it would make two
// links where the sender wrote one — and a <blockquote> because a second
// blockquote is a second claim about who is being quoted.
func divisible(host, n *html.Node) bool {
	for cur := n.Parent; cur != nil && cur != host; cur = cur.Parent {
		if !dividableTags[cur.DataAtom] {
			return false
		}
	}
	return true
}

var dividableTags = map[atom.Atom]bool{
	atom.Div: true, atom.P: true, atom.Span: true, atom.Font: true,
	atom.B: true, atom.Strong: true, atom.I: true, atom.Em: true, atom.U: true,
}

// splitTail divides every element between n and the host at n, and returns the
// host's child that the block now begins.
//
// Each ancestor is replaced by two of itself: the one already there keeps what
// came before n, and a copy carrying the same attributes takes n and everything
// after it. Working outward means each division moves a node that is already the
// first of its run, so no text changes order and none is copied.
func splitTail(host, n *html.Node) *html.Node {
	cur := n
	for cur.Parent != host {
		parent := cur.Parent
		clone := &html.Node{Type: parent.Type, DataAtom: parent.DataAtom, Data: parent.Data,
			Attr: append([]html.Attribute(nil), parent.Attr...)}
		for s := cur; s != nil; {
			next := s.NextSibling
			parent.RemoveChild(s)
			clone.AppendChild(s)
			s = next
		}
		if parent.NextSibling != nil {
			parent.Parent.InsertBefore(clone, parent.NextSibling)
		} else {
			parent.Parent.AppendChild(clone)
		}
		cur = clone
	}
	return cur
}

// suffixLines counts the visible lines from n to the end of the host: n's own,
// and every sibling after it at each level up to the host.
func suffixLines(host, n *html.Node) int {
	count := visibleLines(n)
	for cur := n; cur != nil && cur != host; cur = cur.Parent {
		for s := cur.NextSibling; s != nil; s = s.NextSibling {
			count += visibleLines(s)
		}
	}
	return count
}

// hasContentBefore reports whether anything before n in document order puts
// content on the page, reading an <img> as content exactly as hasContentOutside
// does.
func hasContentBefore(host, n *html.Node) bool {
	found, has := false, false
	var walk func(*html.Node)
	walk = func(c *html.Node) {
		if found || c == n {
			found = true
			return
		}
		switch c.Type {
		case html.TextNode:
			if strings.TrimSpace(c.Data) != "" {
				has = true
			}
		case html.ElementNode:
			if c.DataAtom == atom.Img {
				has = true
			}
		}
		for k := c.FirstChild; k != nil && !found; k = k.NextSibling {
			walk(k)
		}
	}
	for c := host.FirstChild; c != nil && !found; c = c.NextSibling {
		walk(c)
	}
	return has
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
