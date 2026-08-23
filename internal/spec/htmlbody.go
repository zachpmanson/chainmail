package spec

import (
	"regexp"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/zachpmanson/chainmail/internal/unnest"
)

// The stored text/html part is the sender's own markup: the links they wrote,
// the table they laid out, the list they numbered. None of that survives the
// text rendition, so a body derived from text is a transcript of a transcript.
// This file renders the markup instead, and falls back to the text path only
// where there is no markup to render.
//
// Stated plainly, once: nothing in this file sanitises. What it emits is the
// sender's markup with the document chrome taken off, and it reaches the page
// through dangerouslySetInnerHTML (src/components/Timeline.tsx). An on* handler,
// a javascript: href, an <svg onload> all pass through untouched. That is a
// deliberate decision for what this tool currently is: one person's own mailbox,
// rendered to a local file, opened in their own browser, holding markup their
// mail client already rendered once. Issue #14 tracks the sanitiser.
//
// The decision rests entirely on the reader being the person whose mail it is.
// Serving a generated page, or sharing one (issue #10), turns every entry into
// stored XSS delivered by whoever sent the mail: the trust is the mailbox
// owner's to give and a visitor never gave it. #14 has to land before #10.

// htmlBody renders a stored text/html part, or "" when it holds nothing to show.
//
// "" is also the signal to fall back to the text path, which is why every empty
// outcome here is the same value: a part that peels down to a bare quote, one
// that survives truncation as markup with no words in it, and one the parser
// makes nothing of are all cases where the text rendition is the better source
// and is still available.
//
// The second return says whether a signature or a disclaimer was folded, for the
// source note; see bodyHTML for why it is reported from here.
func htmlBody(raw string, st bodyStyle, bf bodyFold) (string, bool) {
	doc, err := html.Parse(strings.NewReader(raw))
	if err != nil {
		// Only a read error, which a strings.Reader does not produce. Parsing
		// itself never fails: malformed and truncated markup is recovered per the
		// HTML5 tree construction rules.
		return "", false
	}
	body := findBody(doc)
	if body == nil {
		return "", false
	}
	if st.peel {
		peelQuoted(body)
	}
	return renderBody(body, bf)
}

// renderBody strips, tidies and serialises a body subtree.
//
// Everything on the page is re-serialised from the parse tree rather than
// sliced out of the source. That is what makes a truncated part safe to render:
// the parser closes what the truncation left open, so the output cannot leak an
// unclosed <div> or <table> that would swallow the rest of the timeline.
//
// The fold is applied between the two passes on purpose. The blocks a client
// marked are found before stripChrome, which drops the class that marks them,
// and moved after it, so the disclosure's own class is not dropped with the
// sender's.
func renderBody(body *html.Node, bf bodyFold) (string, bool) {
	marked := markedSignatures(body)
	stripChrome(body)
	trimTrailingChrome(body)
	if !hasContent(body) {
		return "", false
	}
	folded := false
	for _, n := range marked {
		if foldNodes(body, []*html.Node{n}, markedNote) {
			folded = true
		}
	}
	if !folded {
		// A client that marked its signature has already said where the block is,
		// and folding a second time would fold the first disclosure inside another.
		folded = foldRepeatedTail(body, bf)
	}
	trimEdgeWhitespace(body)
	var b strings.Builder
	for c := body.FirstChild; c != nil; c = c.NextSibling {
		if err := html.Render(&b, c); err != nil {
			return "", false
		}
	}
	return b.String(), folded
}

func findBody(n *html.Node) *html.Node {
	if n.Type == html.ElementNode && n.DataAtom == atom.Body {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if b := findBody(c); b != nil {
			return b
		}
	}
	return nil
}

// peelQuoted drops the quoted history, as the text path does and for the same
// reason: unnest has already mined those blocks into entries of their own, which
// sit elsewhere on the same page.
//
// Markup states the boundary instead of implying it, which is the one place the
// HTML path has it easier than the text path: nesting is a container inside a
// container rather than a count of ">" markers.
func peelQuoted(body *html.Node) {
	if n := firstBoundary(body); n != nil {
		cutFrom(body, n)
	}
}

// firstBoundary returns the first quoted-history boundary in document order, or
// nil when the subtree holds none.
//
// Document order, not level order: a container nested inside an earlier sibling
// starts the trail before a header block written later at this level does, and
// cutting at the later one would leave the earlier trail on the page.
func firstBoundary(n *html.Node) *html.Node {
	starts := map[*html.Node]bool{}
	for _, sp := range boundarySpans(n) {
		starts[sp.first] = true
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if starts[c] {
			return c
		}
		if f := firstBoundary(c); f != nil {
			return f
		}
	}
	return nil
}

// span is a boundary as it appears among a node's children: where the quoted
// history starts, and the child the message it introduces starts at.
//
// A span rather than a node because Outlook writes a header block as one
// paragraph per key — "From:" in its own <p>, "Sent:" in the next — so the
// boundary is several siblings wide and nothing wraps it.
type span struct {
	first *html.Node // inclusive
	end   *html.Node // exclusive; nil when the boundary runs to the last child
}

// boundarySpans finds the boundaries among one node's children, in document
// order.
//
// Two rules, because a boundary is stated at two different scales. A child that
// is a boundary in its own right is one span. The other rule reads the children
// as lines (see childLines) and hands them to unnest.FindHeaderBlock: that is
// what finds the header block Outlook spreads across siblings, where no single
// child holds two header keys and so no single child looks like a boundary at
// all.
func boundarySpans(n *html.Node) []span {
	var kids []*html.Node
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		kids = append(kids, c)
	}
	lines, at := childLines(kids)

	ends := map[int]int{} // first child index -> end child index, exclusive
	for i, c := range kids {
		if isBoundary(c) {
			ends[i] = i + 1
		}
	}
	for li := 0; li < len(lines); {
		if _, ok := unnest.FindHeaderBlock(lines, li); !ok {
			li++
			continue
		}
		if li > 0 && at[li-1] == at[li] {
			// The block starts partway into a child, so the child is not the
			// boundary — something inside it is, and the walk reaches it there.
			// Cutting at the child would take the sender's own text with it.
			li++
			continue
		}
		stop := headerBlockEnd(lines, li)
		end := len(kids)
		if stop < len(at) {
			end = at[stop]
		}
		if end <= at[li] {
			// The block began and ended inside one child, which is the inline form:
			// that child is the whole boundary.
			end = at[li] + 1
		}
		if e, seen := ends[at[li]]; !seen || e < end {
			ends[at[li]] = end
		}
		li = stop
	}

	var out []span
	for i := 0; i < len(kids); {
		end, ok := ends[i]
		if !ok {
			i++
			continue
		}
		// A span swallows any boundary starting inside it: the header keys of one
		// block are not each a boundary of their own.
		for j := i + 1; j < end; j++ {
			if e, ok := ends[j]; ok && e > end {
				end = e
			}
		}
		sp := span{first: kids[i]}
		if end < len(kids) {
			sp.end = kids[end]
		}
		out = append(out, sp)
		i = end
	}
	return out
}

// childLines reads a node's children as the lines unnest expects: one line per
// header key, blank lines left out.
//
// Blank children are dropped because Outlook puts an empty paragraph between the
// keys and a header block never contains a blank line. Lines are split at each
// key because clients disagree about how much markup a header block gets: one
// paragraph per key in Outlook, one <br> per key in Gmail's forward, and
// sometimes the whole block inline in a single line. unnest counts keys per
// line, so the inline form is one key line and no boundary at all unless it is
// broken up first.
func childLines(kids []*html.Node) (lines []unnest.Line, at []int) {
	for i, c := range kids {
		for _, raw := range strings.Split(textHead(c, boundaryTextHead), "\n") {
			for _, seg := range splitKeys(raw) {
				t := strings.Join(strings.Fields(seg), " ")
				if t == "" {
					continue
				}
				lines = append(lines, unnest.Line{Text: t})
				at = append(at, i)
			}
		}
	}
	return lines, at
}

// reKeyish matches where a header key could start: key-shaped, not judged. Which
// keys are real is unnest's vocabulary to know — it holds the localisations and
// the Outlook bolding — so this only says where to cut.
var reKeyish = regexp.MustCompile(`(?:^|\s)\*?[A-Za-z][A-Za-z-]{1,11}\*?:(?:\s|$)`)

// splitKeys breaks a line before every key after the first.
func splitKeys(s string) []string {
	m := reKeyish.FindAllStringIndex(s, -1)
	if len(m) < 2 {
		return []string{s}
	}
	out := make([]string, 0, len(m))
	prev := 0
	for _, loc := range m[1:] {
		out = append(out, s[prev:loc[0]])
		prev = loc[0]
	}
	return append(out, s[prev:])
}

// headerBlockEnd returns the line after the last one belonging to the header
// block that starts at li.
//
// unnest reports its own end and that end is not usable here. Its rule treats a
// non-key line as a folded recipient list whenever a key resumes within six
// lines, which is right for a body read as text and wrong for one read as
// markup: reading each element as a line compresses a whole message to a handful
// of them, so the next quoted message's "From:" falls inside the window and the
// message between the two header blocks is swallowed into the boundary.
//
// The rule here judges the line itself instead. A wrapped recipient list breaks
// into fragments — an address, a ">", a ";" — and markup is where it breaks
// worst, since Gmail puts each fragment of a folded To: in its own element. A
// line that is neither a key nor one of those fragments is the message starting.
//
// The cost of being wrong is asymmetric, which is what settles the trade: too
// short leaves a stray address at the top of a block, too long deletes the
// message.
func headerBlockEnd(lines []unnest.Line, li int) int {
	last := li // the last line that belongs to the block
	for j := li; j < len(lines); j++ {
		if isHeaderKeyLine(lines[j].Text) || continuesHeaderValue(lines[j].Text) {
			last = j
			continue
		}
		break
	}
	return last + 1
}

// continuesHeaderValue reports a line that is part of a header value rather than
// the start of the message.
//
// What a folded recipient list breaks into, in the order the fragments appear:
// an address, a bare ">", and a ">, Name <" that carries the tail of one
// recipient and the head of the next. A message does not open on any of those.
func continuesHeaderValue(text string) bool {
	t := strings.TrimSpace(text)
	if t == "" {
		return false
	}
	if strings.Contains(t, "@") || strings.HasSuffix(t, "<") {
		return true
	}
	if strings.IndexByte(">;,", t[0]) >= 0 {
		return true
	}
	// Punctuation alone: the brackets and separators the list breaks on.
	return strings.Trim(t, "<>;,. ") == ""
}

// isHeaderKeyLine reports whether one line is "Key: value" for a header key
// unnest recognises.
//
// It asks by handing unnest the line twice: the rule there is two or more
// consecutive recognised keys, so a pair of identical lines matches exactly when
// the line is one. Keeping the question there rather than restating the key
// vocabulary here is the point — that vocabulary includes the localisations and
// the Outlook bolding, and a second copy of it would drift.
func isHeaderKeyLine(text string) bool {
	l := unnest.Line{Text: text}
	_, ok := unnest.FindHeaderBlock([]unnest.Line{l, l}, 0)
	return ok
}

// Container names every quoting client in this corpus writes, "x_" stripped
// first because Outlook on the web prefixes the classes of markup it quotes.
// Recognising the name rather than the styling is what survives the six
// different border-left declarations Gmail alone has emitted for one blockquote.
var boundaryClass = map[string]bool{
	"gmail_quote":           true,
	"gmail_quote_container": true,
	"gmail_attr":            true,
	"moz-cite-prefix":       true,
	"yahoo_quoted":          true,
	"zendesk_quote":         true,
	"quoted-text":           true,
}

var boundaryID = map[string]bool{"divrplyfwdmsg": true}

// boundaryTextHead is how much of a node's text is read to ask whether it opens
// with an attribution or a header block. Two of either fit in far less; reading
// the whole subtree would walk the entire trail at every node.
const boundaryTextHead = 1024

// isBoundary reports whether a node begins quoted history.
//
// Three kinds of statement, because clients disagree about which fact they
// make. A named container — by class, or by the one id Outlook writes — is the
// reliable one. type="cite" is Apple Mail, which names nothing else. The last
// reads the node's own opening text and asks unnest, the same detectors the text
// path uses, which is what catches Outlook's border-top separator and the
// sixty-odd blockquotes carrying styling and no name at all.
//
// That last rule is also what keeps an inline pull-quote on the page. A sender
// quoting a paragraph into their own message writes a blockquote with no
// attribution above it and no client class on it, so none of the three rules
// fire and the quote, and everything the sender wrote after it, survives.
// Treating every blockquote as history instead would delete the tail of 15
// entries in this corpus to spare 5 a repeated trail, which is the wrong trade:
// a repeated trail is noise, a deleted tail is a lie.
func isBoundary(n *html.Node) bool {
	if n.Type != html.ElementNode {
		return false
	}
	for _, c := range classes(n) {
		if boundaryClass[c] {
			return true
		}
	}
	if boundaryID[strings.ToLower(strings.TrimPrefix(attr(n, "id"), "x_"))] {
		return true
	}
	if n.DataAtom == atom.Blockquote && strings.EqualFold(attr(n, "type"), "cite") {
		return true
	}
	return opensWithSentinel(textHead(n, boundaryTextHead))
}

func opensWithSentinel(text string) bool {
	lines := unnest.Normalise(text)
	for i, l := range lines {
		if strings.TrimSpace(l.Text) == "" {
			continue
		}
		if _, ok := unnest.FindAttribution(lines, i); ok {
			return true
		}
		_, ok := unnest.FindHeaderBlock(lines, i)
		return ok
	}
	return false
}

// cutFrom removes n and everything after it in document order, stopping at
// stop. Following siblings are removed at every level up the ancestor chain
// because that is where the rest of the trail lives: Outlook's history is a run
// of siblings after the separator, not children of it.
func cutFrom(stop, n *html.Node) {
	for cur := n; cur != nil && cur != stop && cur.Parent != nil; cur = cur.Parent {
		for s := cur.NextSibling; s != nil; {
			next := s.NextSibling
			cur.Parent.RemoveChild(s)
			s = next
		}
	}
	if n.Parent != nil {
		n.Parent.RemoveChild(n)
	}
}

// Tags that act on the document rather than say anything in it. A <style> or a
// <link> is the sharpest case: CSS has no scope, so one sender's stylesheet
// restyles the whole timeline and not just their bubble — 26% of the parts here
// carry one. <base> rewrites every relative URL on the page, <meta> can carry a
// refresh, and the rest have no rendition in a transcript at all.
//
// Dropping <script> is not sanitisation and must not be read as any: it is
// removed on the same ground as <style>, and the executable surface that stays
// is stated at the top of this file.
var droppedTags = map[atom.Atom]bool{
	atom.Style:    true,
	atom.Link:     true,
	atom.Meta:     true,
	atom.Base:     true,
	atom.Title:    true,
	atom.Script:   true,
	atom.Noscript: true,
	atom.Iframe:   true,
	atom.Object:   true,
	atom.Embed:    true,
}

// Attributes that carry a light-background, fixed-width document's presentation
// into a themed fluid one. A hardcoded color:#000 is invisible on the dark
// theme (11% of parts declare one) and a width:600px table pushes the timeline
// sideways (88% declare a pixel width), so what is dropped here is exactly what
// asserts a colour or a size.
//
// class and id go with them. The sender's stylesheet was just dropped, so their
// class names now select nothing of theirs — while a name like "sub" or "tm"
// would select this page's own rules, and a duplicated id would break the
// fragment links the timeline navigates by.
//
// What that costs: a sender's red "URGENT" reads as ordinary text, and a table
// they sized to their content is sized to ours. Keeping the declarations
// instead costs legibility on a whole theme, which is worse — a body that cannot
// be read is not a body.
var droppedAttrs = map[string]bool{
	"class": true, "id": true, "style": true,
	"bgcolor": true, "background": true, "color": true, "face": true,
	"width": true, "height": true,
	"border": true, "cellpadding": true, "cellspacing": true,
}

// Declarations kept out of a style attribute: emphasis the sender chose, which
// means the same thing against any background. Dropping these with the rest
// would flatten a deliberately bold line into prose.
var keptStyleProps = map[string]bool{
	"font-weight":     true,
	"font-style":      true,
	"text-decoration": true,
	"text-align":      true,
}

// stripChrome removes the document-level nodes and the presentational
// attributes, in place.
func stripChrome(n *html.Node) {
	var next *html.Node
	for c := n.FirstChild; c != nil; c = next {
		next = c.NextSibling
		switch c.Type {
		case html.CommentNode, html.DoctypeNode:
			// Comments are Office's conditional-comment chrome in 21% of parts, and
			// carry nothing a reader sees in the rest.
			n.RemoveChild(c)
			continue
		case html.ElementNode:
			if droppedTags[c.DataAtom] {
				n.RemoveChild(c)
				continue
			}
			if c.DataAtom == atom.Img && isDeadImage(c) {
				n.RemoveChild(c)
				continue
			}
			c.Attr = keptAttrs(c)
		}
		stripChrome(c)
	}
}

// isDeadImage reports an <img> that can never load. A cid: URL names a MIME part
// of the original message, and this corpus stores attachment metadata and no
// bytes, so the reference resolves to nothing and renders as a broken-image box
// — 21% of parts carry at least one. The attachment itself is still on the page,
// in the row the entry already lists it in.
//
// Remote images are deliberately kept. They are the inline screenshots and the
// logos, which is real content, and the cost is real too: every tracking pixel
// in the trail fires again when the page is opened, telling the sender the mail
// was read at that moment from that address. The mail client already fetched
// them once, and the alternative loses the screenshots, so they stay.
func isDeadImage(n *html.Node) bool {
	return strings.HasPrefix(strings.ToLower(attr(n, "src")), "cid:")
}

func keptAttrs(n *html.Node) []html.Attribute {
	out := make([]html.Attribute, 0, len(n.Attr))
	for _, a := range n.Attr {
		key := strings.ToLower(a.Key)
		if key == "style" {
			if v := keptStyle(a.Val); v != "" {
				out = append(out, html.Attribute{Key: "style", Val: v})
			}
			continue
		}
		// An image's own dimensions are the aspect ratio the sender laid it out
		// at, not a claim about the page width, and the stylesheet caps the
		// rendered size anyway.
		if droppedAttrs[key] && !(n.DataAtom == atom.Img && (key == "width" || key == "height")) {
			continue
		}
		out = append(out, a)
	}
	return out
}

func keptStyle(v string) string {
	var keep []string
	for _, decl := range strings.Split(v, ";") {
		prop, _, ok := strings.Cut(decl, ":")
		if !ok {
			continue
		}
		if keptStyleProps[strings.ToLower(strings.TrimSpace(prop))] {
			keep = append(keep, strings.TrimSpace(decl))
		}
	}
	return strings.Join(keep, "; ")
}

// trimEdgeWhitespace strips the whitespace-only nodes at the very start and very
// end of a body, so the first and last visible lines of a message are its own
// content rather than the formatting margin email clients pad their markup
// with. A leading blank paragraph or a trailing run of blank lines reads as a
// sloppy edge on a page that presents email as a clean transcript; the sender's
// markup is preserved whole, only the emptiness at the two edges goes.
//
// Trailing whitespace is treated as running to the first disclosed (folded)
// signature: the fold is signed-content, not trailer, and the body ends where
// the signature summary is reached. So a build that folded a five-line
// signature has its exposed remainder trimmed right up to the disclosure,
// leaving exactly one clean boundary before the fold starts.
func trimEdgeWhitespace(body *html.Node) {
	trimLeadingWhitespace(body)
	trimTrailingWhitespace(body)
}

// trimLeadingWhitespace removes whitespace-only nodes from the start of a body:
// a run of blank text, <br>, or an empty container (like the empty <div> some
// clients open a message with). It stops at a node that has content — an image,
// text, or a folded signature summary — so nothing real is touched.
func trimLeadingWhitespace(n *html.Node) {
	for c := n.FirstChild; c != nil; c = n.FirstChild {
		if isSignatureFold(c) {
			return
		}
		if hasContent(c) {
			if c.Type == html.ElementNode && c.DataAtom != atom.Pre {
				trimLeadingWhitespace(c)
			}
			return
		}
		n.RemoveChild(c)
	}
}

// trimTrailingWhitespace removes whitespace-only nodes from the end of a body,
// stopping before a folded signature: the fold summary is content and stays.
// A <br> or blank paragraph trailing the last real line is dropped, so the final
// line of the exposed body sits edge-on to the signature disclosure rather than
// a run of empty lines.
func trimTrailingWhitespace(n *html.Node) {
	for {
		c := n.LastChild
		if c == nil {
			return
		}
		if isSignatureFold(c) {
			// The exposed body ends right before the fold. Trim the blanks sitting
			// immediately above it — a blank paragraph or two before a disclosure
			// reads as a sloppy edge — but stop at the nearest real node. The
			// fold itself and its own internal padding are left untouched.
			for p := c.PrevSibling; p != nil && !hasContent(p); p = p.PrevSibling {
				c.Parent.RemoveChild(p)
			}
			return
		}
		if hasContent(c) {
			if c.Type == html.ElementNode && c.DataAtom != atom.Pre {
				trimTrailingWhitespace(c)
			}
			return
		}
		n.RemoveChild(c)
	}
}

// isSignatureFold reports whether a node begins a disclosed signature block —
// the <details class="sig"> both fold paths build. Used by the two edge trims
// to keep the signature (and its own padding) intact while trimming the exposed
// body around it.
func isSignatureFold(n *html.Node) bool {
	return n.Type == html.ElementNode && n.DataAtom == atom.Details && hasClass(n, "sig")
}

// trimTrailingChrome drops the whitespace a peel leaves dangling. Outlook writes
func trimTrailingChrome(n *html.Node) {
	for c := n.LastChild; c != nil; c = n.LastChild {
		if hasContent(c) {
			// The rule is usually wrapped: clients put the whole message in one
			// <div>, so the dangling separator is the last child of that, not of
			// the body.
			trimTrailingChrome(c)
			return
		}
		n.RemoveChild(c)
	}
}

// hasContent reports whether a subtree puts anything on the page. An <img> does
// even with no text under it; whitespace, a <br> and an emptied <div> do not.
func hasContent(n *html.Node) bool {
	switch n.Type {
	case html.TextNode:
		return strings.TrimSpace(n.Data) != ""
	case html.ElementNode:
		if n.DataAtom == atom.Img {
			return true
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if hasContent(c) {
			return true
		}
	}
	return false
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if strings.EqualFold(a.Key, key) {
			return a.Val
		}
	}
	return ""
}

func classes(n *html.Node) []string {
	fields := strings.Fields(strings.ToLower(attr(n, "class")))
	for i, f := range fields {
		fields[i] = strings.TrimPrefix(f, "x_")
	}
	return fields
}

func hasClass(n *html.Node, want string) bool {
	for _, c := range classes(n) {
		if c == want {
			return true
		}
	}
	return false
}

// textHead returns up to max bytes of a subtree's text, in document order.
func textHead(n *html.Node, max int) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if b.Len() >= max {
			return
		}
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
			return
		}
		if n.Type == html.ElementNode && breaksLine(n.DataAtom) {
			b.WriteString("\n")
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return b.String()
}

// breaksLine names the elements that end a line when markup is read as text.
// It matters for sentinel detection and for correlation alike: without it an
// Outlook header block arrives as one run of "From: … Sent: … To: …", where
// unnest is looking for one key per line.
func breaksLine(a atom.Atom) bool {
	switch a {
	case atom.Br, atom.P, atom.Div, atom.Tr, atom.Td, atom.Th, atom.Li,
		atom.Table, atom.Blockquote, atom.H1, atom.H2, atom.H3, atom.H4,
		atom.H5, atom.H6, atom.Pre:
		return true
	}
	return false
}
