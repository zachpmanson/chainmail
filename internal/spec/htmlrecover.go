package spec

import (
	"crypto/sha256"
	"regexp"
	"strings"
	"sync"

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
	best := bestCandidate(text, hosts)
	if best == nil {
		return "", false
	}
	body := parseBlock(best.html)
	if body == nil {
		return "", false
	}
	return renderBody(body, bf)
}

// bestCandidate finds the host block that most confidently holds text, or nil
// when no block clears every confidence bound.
//
// Two blocks of the trail fitting an entry while saying different things is an
// ambiguity that cannot be told apart, and guessing is the one outcome worse
// than plain text.
func bestCandidate(text string, hosts []string) *block {
	needle := textsim.Tokens(stripMailtoMentions(text))
	if len(needle) < minNeedleTokens {
		return nil
	}
	var best, rival *fit
	for _, h := range hosts {
		blocks := hostBlocks(h)
		for i := range blocks {
			c := fit{block: blocks[i]}
			c.score(needle)
			if c.recall < minRecall || c.precision < minPrecision || c.headMatch < minHeadRun {
				continue
			}
			switch {
			case best == nil || c.f1 > best.f1:
				rival, best = best, &c
			case rival == nil || c.f1 > rival.f1:
				rival = &c
			}
		}
	}
	if best == nil {
		return nil
	}
	if rival != nil && best.f1-rival.f1 < ambiguityMargin &&
		textsim.Similarity(best.block.tokens, rival.block.tokens) < sameContent {
		return nil
	}
	return &best.block
}

// blockCacheLimit bounds how many hosts' blocks are kept.
//
// Bounded by host rather than by byte because the two move together: a host's
// blocks hold its text and its markup, so a cache of 64 hosts is a cache of about
// 64 messages' worth of markup — tens of megabytes for a mailbox read in one
// sitting, which is the whole working set of the request that fills it.
const blockCacheLimit = 64

var (
	blocksMu   sync.Mutex
	blockCache = map[[sha256.Size]byte][]block{}
)

// hostBlocks is the haystack: the blocks of one host's markup, parsed once.
//
// Recovery is a correlation, and every entry mined out of the same host is
// matched against the same blocks — but the work was per *entry*, not per host.
// Measured on a 134 KB host, `candidates` cost 36.9 ms of the 39.8 ms it took to
// recover one entry, and a chain of 29 recovered messages found inside four hosts
// re-parsed those documents 116 times: 15 s of one request, against 1.3 s for a
// chain whose messages were never quoted. Parsing pays for the document; scoring
// is what actually differs per entry.
//
// The key is the markup itself, so nothing has to be invalidated and a hit cannot
// return another host's words. What is kept is the extracted markup as text and
// the tokens, not the parsed tree: an extraction cuts the trail that follows the
// entry out of the run, and renderBody mutates what it is handed, so a tree cannot
// be shared between entries — the string can, and the winner is re-parsed from it
// (a block's worth of markup, not a message's).
func hostBlocks(raw string) []block {
	key := sha256.Sum256([]byte(raw))
	blocksMu.Lock()
	cached, ok := blockCache[key]
	blocksMu.Unlock()
	if ok {
		return cached
	}
	parsed := collectBlocks(raw)
	blocksMu.Lock()
	if len(blockCache) >= blockCacheLimit {
		// A blunt bound and deliberately not an eviction policy: what the cache buys
		// is one parse per host *per request*, and a full cache means this process has
		// been asked about more hosts than a sitting reads.
		blockCache = map[[sha256.Size]byte][]block{}
	}
	blockCache[key] = parsed
	blocksMu.Unlock()
	return parsed
}

func collectBlocks(raw string) []block {
	cands := candidates(raw)
	out := make([]block, 0, len(cands))
	for _, c := range cands {
		out = append(out, block{tokens: c.tokens, html: nodeText(c.extract())})
	}
	return out
}

// nodeText writes a node's children as markup, which is what a block is once the
// tree it was cut from has been put down.
func nodeText(n *html.Node) string {
	var b strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if err := html.Render(&b, c); err != nil {
			return ""
		}
	}
	return b.String()
}

// parseBlock turns a block's markup back into the body node renderBody expects.
func parseBlock(markup string) *html.Node {
	doc, err := html.Parse(strings.NewReader(markup))
	if err != nil {
		return nil
	}
	return findBody(doc)
}

// mailtoMention matches the address Gmail appends to a pasted mention: the
// quoted recovery's needle text holds "@Nella Forge <mailto:siobhan@...>"
// while the same mention in the host's markup is rendered as a link whose
// visible text is only the name. The mailto: address therefore contributes tokens
// that exist in the needle but nowhere in the block, so a body opening on a
// mention failed the head alignment: three of the first eight tokens were
// mailto: address fragments that could never match. Remove the address and the
// name counts the way it does in markup: "@Nella Forge", then the content.
var mailtoMention = regexp.MustCompile(`<mailto:[^>]+>`)

// stripMailtoMentions drops an @mention's trailing mailto address from text
// before it is tokenised as the recovery needle. The name itself stays: it is
// real content that appears in both renditions. Only the address fragment, an
// artifact of how the plain rendition writes a mention, is removed.
func stripMailtoMentions(text string) string {
	return mailtoMention.ReplaceAllString(text, "")
}

// inlineImages returns the filenames of the images a quoted message placed in
// its block: the alt text of every cid-referenced <img> in the host block that
// best matches text. Gmail writes the pasted file's name as the image's alt, so
// alt text is how the part is matched back to the MIME attachment that the
// host's row carries. See attributeInlineImages.
func inlineImages(text string, hosts []string) []string {
	b := bestCandidate(text, hosts)
	if b == nil {
		return nil
	}
	body := parseBlock(b.html)
	if body == nil {
		return nil
	}
	var out []string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.DataAtom == atom.Img && isDeadImage(n) {
			if a := strings.TrimSpace(attr(n, "alt")); a != "" {
				out = append(out, a)
			}
		}
		for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
			walk(ch)
		}
	}
	// The block's own markup, not the host's document. This is the run that was
	// matched, with the deeper quoted trail already cut out of it, so an image a
	// host placed in its own chrome — or in a message it went on to quote — is not
	// attributed to this entry.
	for n := body.FirstChild; n != nil; n = n.NextSibling {
		walk(n)
	}
	return out
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
}

// block is a candidate as it can be kept: the words it says, and the markup it is,
// with no tree behind either. See hostBlocks for why the tree has to go.
type block struct {
	tokens []string
	html   string
}

// fit is how well one block matches one entry. It is computed per entry and never
// kept: the block is what is shared.
type fit struct {
	block     block
	recall    float64
	headMatch float64
	precision float64
	f1        float64
}

func (c *fit) score(needle []string) {
	c.headMatch = textsim.HeadSimilarity(needle, c.block.tokens, headRun, headWindow)

	inter := float64(textsim.Overlap(needle, c.block.tokens))
	c.recall = inter / float64(len(needle))
	if len(c.block.tokens) > 0 {
		c.precision = inter / float64(len(c.block.tokens))
	}
	if c.recall+c.precision > 0 {
		c.f1 = 2 * c.recall * c.precision / (c.recall + c.precision)
	}
}

// extract builds the run as a node of its own, cutting the trail that follows it
// inside the run — the deeper history the entry quoted, which is on the page as its
// own entries.
//
// The run is copied, not moved. It used to be moved, which is right while a parsed
// host belongs to one recovery and wrong now that one parsed host answers for every
// entry found inside it: moving a node out of the document that a sibling candidate
// still points into leaves that candidate pointing at a parent it no longer has, and
// RemoveChild panics on exactly that.
func (c *candidate) extract() *html.Node {
	holder := &html.Node{Type: html.ElementNode, DataAtom: atom.Div, Data: "div"}
	for n := c.first; n != nil && n != c.end; n = n.NextSibling {
		holder.AppendChild(cloneNode(n))
	}
	if b := firstBoundary(holder); b != nil {
		cutFrom(holder, b)
	}
	return holder
}

// cloneNode copies a subtree. Attributes are copied including their namespace,
// because a sanitiser that later reads an attribute by name must see the one the
// host wrote.
func cloneNode(n *html.Node) *html.Node {
	c := &html.Node{Type: n.Type, DataAtom: n.DataAtom, Data: n.Data, Namespace: n.Namespace}
	if len(n.Attr) > 0 {
		c.Attr = append([]html.Attribute(nil), n.Attr...)
	}
	for k := n.FirstChild; k != nil; k = k.NextSibling {
		c.AppendChild(cloneNode(k))
	}
	return c
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
	return candidatesIn(body)
}

// candidatesIn enumerates the blocks of an already-parsed host, which is what the
// cache shares between entries.
func candidatesIn(body *html.Node) []*candidate {
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
