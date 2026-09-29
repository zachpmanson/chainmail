package spec

import (
	stdhtml "html"
	"regexp"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// A sender's own markup, rendered as the sender wrote it.
//
// This file is the second policy. sanitise.go is the first: a body destined for
// the transcript, where one page holds many senders at once, so a stylesheet has
// to go (CSS has no scope — see droppedTags) and a fixed-width light-background
// document has to be trimmed to the page's own theme (see droppedAttrs).
//
// The reader can ask for the other thing: show me the mail as it was written.
// That request is answered here, and it is a real request — some senders' mail is
// legible only with its stylesheet, and a Google Calendar invitation is the
// standing example, whose whole design is a <style> block plus classes.
//
// The difference is not "less safe". It is a different containment strategy:
// the transcript relies on stripping, this relies on the shadow root the caller
// mounts the result into. Inside a shadow tree a stylesheet cannot reach the
// page and the page cannot reach in, so <style>, class and id can all stay —
// which is exactly what the first policy cannot allow.
//
// What stays true either way is the executable surface. Both policies refuse a
// <script>, an on* handler and a javascript: URL, and here that is load-bearing
// rather than belt-and-braces: there is no sandbox attribute and no per-message
// CSP on a shadow root, so a handler that survives this pass is a handler that
// runs. Everything below that removes or rewrites something is a refusal on the
// same ground as its counterpart in sanitise.go.

// OriginalBody renders the sender's stored text/html part for the shadow root,
// or "" when there is nothing to show.
//
// The input is the part exactly as stored — the whole document, head included,
// because that is where a mail's stylesheet usually lives. The output is a
// fragment: the stylesheets first, then the body's own content. No <html>,
// <head> or <body> appears, since the caller mounts this inside a shadow tree
// rather than a document, and none of the three has a meaning there.
//
// The one place a rewrite is unavoidable is the page-level selector. There is no
// html or body element inside a shadow tree, so a rule written for either
// matches nothing — and for these senders the font stack and, often, the canvas
// background are written on one of them. html, body and :root therefore become
// :host, which is the element the caller mounts this into. Everything else is
// copied as written; see rewriteCSS for the shape of that pass and what it
// deliberately does not do.
func OriginalBody(raw string) (string, bool) {
	if strings.TrimSpace(raw) == "" {
		return "", false
	}
	doc, err := html.Parse(strings.NewReader(raw))
	if err != nil {
		// Only a read error, which a strings.Reader does not produce.
		return "", false
	}
	head, body := documentParts(doc)
	if body == nil {
		// Unreachable for real input: HTML5 tree construction always makes a
		// body. Declining beats guessing at a document the parser refused.
		return "", false
	}
	var sheets []*html.Node
	if head != nil {
		sheets = collectSheets(head)
	}
	pruneOriginal(body)
	if !hasContent(body) && len(sheets) == 0 {
		// A part that survives as tags with no words and no words' worth of
		// images is not a thing to show. The caller falls back to the
		// transcript rendering it already has.
		return "", false
	}
	var b strings.Builder
	// The app's own canvas first, then the <body> element's presentation, then the
	// stylesheets in document order, then the body's content. That is the order a
	// browser would have applied them in, and the order is load-bearing: all of
	// these reach the host element at equal specificity, so the last one to state a
	// property is the one that holds — which is what lets a sender's own canvas
	// override ours and ours override nothing but the app's theme.
	b.WriteString(appCanvas)
	if canvas := hostCanvas(body); canvas != "" {
		b.WriteString(canvas)
	}
	for _, s := range sheets {
		writeNode(&b, s)
	}
	for c := body.FirstChild; c != nil; c = c.NextSibling {
		writeNode(&b, c)
	}
	return b.String(), true
}

// appCanvas is the canvas the app writes for a message that says nothing about
// its own: white paper, black ink, and a light colour scheme, because that is
// what mail is designed for and the app around it is a dark theme. It also
// states the app's defaults for the one structure mail leans on hardest and HTML
// leaves naked — a table, which browsers otherwise draw as a grid of nothing:
// collapsed borders and the padding that keeps a word off the next one.
//
// What is deliberately NOT here is an outline. A border around every cell is a
// hairline the sender did not ask for, and this whole mode exists to show the
// mail as it was written — so a bare <table> comes back with no border at all
// rather than with the app's. The line is between normalising and painting:
// border-collapse and the cell padding stand in for defaults a browser would
// otherwise apply silently (separated borders, no padding), so either choice is
// the app's, and collapsed-and-padded is the ordinary reading of a plain table
// in a mail. A border is not in that position — an outlined table is a design
// decision, and the mail did not make it. The transcript's own table styling is
// a separate policy and is untouched: a reader there is looking at the page's
// design, not at the sender's.
//
// The selectors are plain (table / th / td), not :host-qualified, because that is
// what the fragment contains: the shadow tree has no html or body, and these
// elements are below the host rather than being it. They are still emitted from
// inside the shadow root, so — like everything else here — the sender's own
// stylesheet comes after them at equal specificity and wins.
//
// It is emitted FIRST and not into the app's own stylesheet, and both halves of
// that matter. Inside the shadow root because a rule in the outer document wins
// over a `:host` rule written from within — the sender's stylesheet is in here,
// so a canvas out there is a canvas the mail has no way to correct. First
// because equal specificity means the last declaration holds: anything the mail
// does say about its own canvas (a `body {}` rule rewritten to `:host`, a
// `bgcolor`) or its own table (`table { border-collapse: separate }`) comes
// after this and wins, which is the whole point of preferring the sender's own
// design over a better-looking default.
const appCanvas = "<style>:host{background:#fff;color:#000;color-scheme:light}" +
	"table{border-collapse:collapse}" +
	"th,td{padding:.2rem .5rem}</style>"

// hostCanvas turns the <body> element's own presentation into a :host rule.
//
// The body element does not survive into the fragment — there is no such
// element inside a shadow tree, and one written into the markup would be
// dropped by the fragment parser anyway — but its attributes are where a mail's
// canvas is most often written, and losing them is losing the background the
// whole design sits on. So the two attributes that carry a canvas are restated
// as the rule the element stood for:
//
//   - style, copied as a declaration list and neutralised like any other CSS.
//   - bgcolor, which HTML defines as a presentational hint for
//     background-color.
//
// background, the old URL-valued attribute, becomes a background-image — and
// only when the URL passes the same scheme test every other URL does, with the
// characters that would let a URL close the url() it sits in refused.
//
// A class or an id on the body is not carried. There is nothing to carry it to:
// it would have to become a class on the element the caller mounts this into,
// and :host(.x) is a selector the caller would have to be told about. A sender
// that qualifies a body selector with a class is rare, and the cost of missing
// it is a rule that does not apply rather than one that applies to the wrong
// element.
func hostCanvas(body *html.Node) string {
	var decls []string
	if v := strings.TrimSpace(attr(body, "style")); v != "" {
		decls = append(decls, strings.Trim(strings.TrimSpace(neutraliseCSSURLs(v)), ";"))
	}
	if v := strings.TrimSpace(attr(body, "bgcolor")); v != "" && isColourValue(v) {
		decls = append(decls, "background-color:"+v)
	}
	if v := strings.TrimSpace(attr(body, "background")); v != "" && goodDocumentURL("background", v) && isSafeCSSURL(v) {
		decls = append(decls, "background-image:url("+v+")")
	}
	if len(decls) == 0 {
		return ""
	}
	return "<style>:host{" + strings.Join(decls, "; ") + "}</style>"
}

// isColourValue accepts a value that can only be a colour: a hex literal or a
// bare name. A presentational attribute is not a place to accept arbitrary CSS
// text — anything that could carry a semicolon or a brace could add a
// declaration this pass never saw.
var reColourValue = regexp.MustCompile(`^(?:#[0-9a-fA-F]{3,8}|[a-zA-Z]{1,20})$`)

func isColourValue(v string) bool { return reColourValue.MatchString(v) }

// isSafeCSSURL refuses a URL that could end the url() it is written into. A
// parenthesis or a quote in a URL is not a URL.
func isSafeCSSURL(v string) bool {
	return !strings.ContainsAny(v, "()'\"\\")
}

// documentParts finds the document's head and body.
func documentParts(doc *html.Node) (head, body *html.Node) {
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.DataAtom {
			case atom.Head:
				if head == nil {
					head = n
				}
			case atom.Body:
				if body == nil {
					body = n
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return head, body
}

// documentDrops are the elements that go whole, with everything they contained.
//
// This is the same judgement as droppedSubtree in sanitise.go — an element that
// acts, embeds, or is a place another document runs has no form that is worth
// showing — with three deliberate differences, all of them towards fidelity,
// because a shadow tree is its own document and the reason those elements were
// dropped whole in the transcript was the page around them:
//
//   - <picture> is not here: an HTML part that carries a <picture> carries an
//     <img> fallback inside it, and dropping the whole element to remove the
//     <source> children would drop the picture that does render.
//   - nav, menu, map, area, bdi, bdo and dialog are not here either. They are
//     content — a newsletter's <nav> holds its links — and a transcript that
//     dropped them would lose the text with them. sanitise.go drops them whole;
//     this pass keeps them, because there is no page of ours for them to
//     interfere with.
//   - <link> is kept, filtered to stylesheets (see collectSheets). A <link> in a
//     shadow tree is scoped to that tree, which is the one property that makes
//     it allowable here and not in the transcript.
//
// <style> is kept, and rewritten rather than dropped: rewriteCSS is that pass.
var documentDrops = map[atom.Atom]bool{
	atom.Script:   true,
	atom.Noscript: true,
	atom.Meta:     true,
	atom.Base:     true,
	atom.Title:    true,
	atom.Iframe:   true,
	atom.Object:   true,
	atom.Embed:    true,
	atom.Applet:   true,
	atom.Param:    true,
	atom.Video:    true,
	atom.Audio:    true,
	atom.Canvas:   true,
	atom.Svg:      true,
	atom.Math:     true,
	atom.Template: true,
	atom.Frame:    true,
	atom.Frameset: true,
	atom.Noframes: true,
	atom.Source:   true,
	atom.Track:    true,
	atom.Marquee:  true,
	atom.Form:     true,
	atom.Input:    true,
	atom.Button:   true,
	atom.Select:   true,
	atom.Option:   true,
	atom.Textarea: true,
	atom.Label:    true,
	atom.Fieldset: true,
	atom.Datalist: true,
	atom.Output:   true,
	atom.Progress: true,
	atom.Meter:    true,
}

// documentURLAttrs are the attributes that name a URL, and so are the ones whose
// scheme has to be judged before the attribute is kept.
//
// The list is longer than sanitise.go's two, because the transcript's allowlist
// decides which attributes exist at all and this pass keeps whatever the sender
// wrote. srcset is deliberately absent: its entries are candidate image URLs and
// an engine ignores an entry it cannot fetch, so the img src — which is judged —
// is the one that decides whether anything loads.
var documentURLAttrs = map[string]bool{
	"href": true, "src": true, "background": true, "poster": true,
	"data": true, "action": true, "formaction": true, "cite": true,
	"longdesc": true, "usemap": true, "xlink:href": true,
}

// documentDroppedAttrs are the attributes dropped whatever element carries them.
//
// on* is the executable surface and the one that matters: `<img onerror>` is a
// script that runs when the image fails, and with no sandbox attribute on the
// shadow root nothing downstream would stop it. The rest are inert here but add
// nothing a reader sees: srcdoc can only appear on a <frame>/<iframe> and both
// are dropped, ping is a tracking endpoint a click would fire, and integrity/
// nonce/crossorigin are only meaningful on a <link> we already filter to
// stylesheets in a tree that cannot fetch a font's CORS state.
var documentDroppedAttrs = map[string]bool{
	"srcdoc": true, "ping": true,
	"integrity": true, "nonce": true, "crossorigin": true,
}

// pruneOriginal prunes one subtree in place: comments and doctypes go, and an
// element that acts is removed with its contents.
//
// Unlike sanitiseChildren there is no allowlist of tags. An element this pass
// does not recognise is left exactly as the sender wrote it — that is the point
// of the mode, and an unknown tag is inert in a shadow tree in a way it would
// not be if it were being flattened into a page whose stylesheet it might
// match.
func pruneOriginal(n *html.Node) {
	var next *html.Node
	for c := n.FirstChild; c != nil; c = next {
		next = c.NextSibling
		switch c.Type {
		case html.TextNode:
			continue
		case html.CommentNode, html.DoctypeNode:
			n.RemoveChild(c)
			continue
		case html.ElementNode:
			if documentDrops[c.DataAtom] {
				n.RemoveChild(c)
				continue
			}
			if c.DataAtom == atom.Img && isDeadImage(c) {
				// A cid: reference names a MIME part of the original message, and
				// the corpus stores attachment metadata and no bytes — so it can
				// only ever render as a broken-image box. The attachment is
				// already on the page, in the row the entry lists it in.
				n.RemoveChild(c)
				continue
			}
			if c.DataAtom == atom.Link && !isStylesheetLink(c) {
				n.RemoveChild(c)
				continue
			}
			pruneOriginal(c)
			if c.DataAtom == atom.Style {
				rewriteStyleElement(c)
			}
			c.Attr = documentAttrs(c)
		}
	}
}

// documentAttrs filters one element's attributes: the dropped few go, a URL is
// judged by its scheme, and a style attribute has its own dangerous URLs
// neutralised rather than dropping the declaration's element.
func documentAttrs(n *html.Node) []html.Attribute {
	out := make([]html.Attribute, 0, len(n.Attr)+2)
	for _, a := range n.Attr {
		key := strings.ToLower(a.Key)
		if documentDroppedAttrs[key] || isEventHandler(key) {
			continue
		}
		if documentURLAttrs[key] && !goodDocumentURL(key, a.Val) {
			continue
		}
		if key == "style" {
			a.Val = neutraliseCSSURLs(a.Val)
		}
		out = append(out, a)
	}
	if n.DataAtom == atom.A {
		// A link with no target navigates this page away — inside a shadow tree
		// there is no sandbox to stop it, and the app is the document. Sending it
		// to a new tab is the one attribute rewrite this pass makes, and it is
		// forced rather than added when absent: a sender that wrote target="_self"
		// meant it in their own document, not in ours.
		out = setAttr(out, "target", "_blank")
		out = setAttr(out, "rel", "noopener noreferrer")
	}
	return out
}

// isEventHandler reports whether an attribute name is an event handler. The
// prefix rule is the whole of it: on* is a closed namespace in HTML, and an
// attribute that begins with "on" on a mail element is a handler or a typo.
func isEventHandler(key string) bool {
	return len(key) > 2 && strings.HasPrefix(key, "on")
}

// goodDocumentURL is the scheme boundary for an attribute that carries a URL.
//
// http(s) for anything that fetches, plus mailto: on a link, because a mail is
// the one document where "reply to this person" is a link a reader expects to
// work. A relative URL is refused rather than resolved: inside the app it would
// resolve against our origin, and the sender wrote it against theirs. Whitespace
// is a parse accident rather than a URL, so it refuses that too — the tab in
// "java\tscript:" is the case this catches.
func goodDocumentURL(attr, v string) bool {
	low := strings.ToLower(strings.TrimSpace(v))
	if strings.IndexAny(low, " \t\r\n\f") >= 0 {
		return false
	}
	if strings.HasPrefix(low, "http://") || strings.HasPrefix(low, "https://") {
		return true
	}
	switch attr {
	case "href", "cite", "longdesc":
		return strings.HasPrefix(low, "mailto:")
	case "src", "poster", "background":
		return strings.HasPrefix(low, "data:image/")
	}
	return false
}

// setAttr replaces an attribute's value, or appends it when the element does not
// carry it.
func setAttr(attrs []html.Attribute, key, val string) []html.Attribute {
	for i, a := range attrs {
		if strings.EqualFold(a.Key, key) {
			attrs[i].Val = val
			return attrs
		}
	}
	return append(attrs, html.Attribute{Key: key, Val: val})
}

// isStylesheetLink reports whether a <link> is a stylesheet, which is the only
// kind this pass has any use for. A preload, an icon or a prefetch in a shadow
// tree is a fetch with no reader on the other end of it.
func isStylesheetLink(n *html.Node) bool {
	for _, rel := range strings.Fields(strings.ToLower(attr(n, "rel"))) {
		if rel == "stylesheet" {
			return true
		}
	}
	return false
}

// collectSheets removes the head's stylesheets and returns them in document
// order. They are rendered before the body rather than in place because that is
// the order a browser applies them, and the caller is assembling a fragment out
// of what used to be a document.
func collectSheets(head *html.Node) []*html.Node {
	var sheets []*html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		var next *html.Node
		for c := n.FirstChild; c != nil; c = next {
			next = c.NextSibling
			if c.Type != html.ElementNode {
				continue
			}
			switch c.DataAtom {
			case atom.Style:
				n.RemoveChild(c)
				sheets = append(sheets, c)
				continue
			case atom.Link:
				if isStylesheetLink(c) {
					n.RemoveChild(c)
					c.Attr = documentAttrs(c)
					sheets = append(sheets, c)
					continue
				}
			}
			walk(c)
		}
	}
	walk(head)
	for _, s := range sheets {
		if s.DataAtom == atom.Style {
			rewriteStyleElement(s)
		}
	}
	return sheets
}

// rewriteStyleElement rewrites one <style> element's text in place: the
// dangerous URL neutralisation every CSS value gets, then the selector rewrite.
func rewriteStyleElement(n *html.Node) {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.TextNode {
			c.Data = rewriteCSS(neutraliseCSSURLs(c.Data))
		}
	}
}

// reCSSScriptURL matches a url() whose target is a script. Modern engines do not
// execute a CSS URL, so this is a refusal on principle rather than a live hole —
// but "no CSS URL is a script" is a property worth holding as a property, and
// the alternative is a rule that says this one place in the pipeline trusts the
// browser's parsing of a scheme it never checks.
//
// data:text/html is included for the same reason: it is not an image, and a
// stylesheet has no business pointing at a document. url() for anything else —
// an image, a font, a background — is kept, and is the fidelity this mode exists
// for. (The cost is real and named in the docs: every url() in an expanded
// message is a fetch to the sender's server.)
var reCSSScriptURL = []*regexp.Regexp{
	regexp.MustCompile(`(?i)url\(\s*["']?\s*(?:javascript|vbscript)\s*:[^)]*\)`),
	regexp.MustCompile(`(?i)url\(\s*["']?\s*data\s*:\s*text/html[^)]*\)`),
}

// neutraliseCSSURLs replaces a script URL with a target that does nothing. The
// declaration is kept rather than dropped so the value is a value the browser
// still parses: a dropped declaration and an empty one are the same to
// everything downstream of it.
func neutraliseCSSURLs(css string) string {
	for _, re := range reCSSScriptURL {
		css = re.ReplaceAllString(css, "url(about:blank)")
	}
	return css
}

// @import is dropped, and it is the one at-rule this pass refuses.
//
// It is not a styling decision. A <style> element's text is the only stylesheet
// this pass can read, and an @import names one it cannot: the rules would be
// applied without ever passing a check. A <link> is the same fetch, but it is an
// element — collectSheets can filter it to http(s) stylesheets and the pass can
// see it — so the rule here is about what can be inspected, not about what is
// remote.
var reImportStatement = regexp.MustCompile(`(?i)^\s*@import\b`)

// rewriteCSS rewrites a stylesheet's selector preludes and copies everything
// else verbatim.
//
// The pass is deliberately shallow. It understands one thing — where a rule's
// selector ends and its declarations begin — because the rewrite it has to make
// concerns selectors alone:
//
//   - html, body and :root become :host, so a rule written for a document's
//     outermost elements applies to the element the caller mounts this into.
//     There is no html or body inside a shadow tree, and for these senders those
//     rules carry the font stack and, often, the canvas background.
//   - Nothing else is touched. Declarations, at-rules, comments and media
//     queries are copied byte for byte, so a stylesheet that survives this pass
//     is a stylesheet the sender would recognise.
//
// What it does not do: CSS nesting (& .x { } inside a rule) is copied with the
// rule it is nested in, so a nested selector is not rewritten; an @scope
// prelude's selectors are not rewritten either. Both are unheard of in mail
// markup, and the failure mode of missing them is a rule that does not apply —
// never a rule that applies to the wrong thing.
func rewriteCSS(css string) string {
	var out strings.Builder
	for i := 0; i < len(css); {
		next := rewriteRules(css, i, &out)
		if next <= i {
			break
		}
		i = next
	}
	return out.String()
}

// groupingAtRules hold rules rather than declarations, so their bodies are
// walked as rule lists. Everything else that starts with @ — @font-face, @page,
// @keyframes, @property — holds declarations, and is copied through with its
// braces balanced.
var groupingAtRules = []string{
	"@media", "@supports", "@container", "@layer", "@scope",
	"@document", "@-moz-document", "@starting-style",
}

// rewriteRules walks one rule list, from i to the end of the input or to the
// closing brace of the block it is in, and returns where it stopped. Whitespace
// and comments are preserved; an at-statement is dropped if it is @import; a
// declaration block is copied verbatim; a style rule has its selector rewritten
// and its declarations copied verbatim.
func rewriteRules(css string, i int, out *strings.Builder) int {
	end := len(css)
	for i < end {
		// Leading whitespace and comments are copied: they cost nothing and a
		// stylesheet that comes back byte-identical outside its selectors is a
		// stylesheet a reader can diff against the original.
		for i < end {
			if isCSSSpace(css[i]) {
				out.WriteByte(css[i])
				i++
				continue
			}
			if j, ok := cssCommentEnd(css, i, end); ok {
				out.WriteString(css[i:j])
				i = j
				continue
			}
			break
		}
		if i >= end {
			break
		}
		if css[i] == '}' {
			out.WriteByte('}')
			return i + 1
		}
		start := i
		depth := 0
		for i < end {
			c := css[i]
			if c == '"' || c == '\'' {
				i = cssStringEnd(css, i, end)
				continue
			}
			if j, ok := cssCommentEnd(css, i, end); ok {
				i = j
				continue
			}
			switch c {
			case '(':
				depth++
			case ')':
				if depth > 0 {
					depth--
				}
			case '{', ';':
				if depth == 0 {
					goto found
				}
			}
			i++
		}
	found:
		prelude := css[start:i]
		if i >= end {
			// Unterminated rule: the part is truncated mid-stylesheet, which is
			// a real shape here (docket truncates from the end of a body). What
			// was written is kept as written and the pass stops.
			out.WriteString(prelude)
			return end
		}
		if css[i] == ';' {
			if !reImportStatement.MatchString(prelude) {
				out.WriteString(prelude)
				out.WriteByte(';')
			}
			i++
			continue
		}
		// css[i] == '{'
		i++
		switch {
		case isGroupingAtRule(prelude):
			out.WriteString(prelude)
			out.WriteByte('{')
			i = rewriteRules(css, i, out)
		case strings.HasPrefix(strings.TrimSpace(prelude), "@"):
			out.WriteString(prelude)
			out.WriteByte('{')
			i = copyBlock(css, i, out)
		default:
			out.WriteString(rewriteSelectorList(prelude))
			out.WriteByte('{')
			i = copyBlock(css, i, out)
		}
	}
	return i
}

// copyBlock copies a declaration block to and including its closing brace,
// verbatim, with nested braces balanced and strings and comments respected.
func copyBlock(css string, i int, out *strings.Builder) int {
	end := len(css)
	depth := 1
	start := i
	for i < end {
		c := css[i]
		if c == '"' || c == '\'' {
			i = cssStringEnd(css, i, end)
			continue
		}
		if j, ok := cssCommentEnd(css, i, end); ok {
			i = j
			continue
		}
		if c == '{' {
			depth++
		}
		if c == '}' {
			depth--
			if depth == 0 {
				out.WriteString(css[start : i+1])
				return i + 1
			}
		}
		i++
	}
	out.WriteString(css[start:end])
	return end
}

func isGroupingAtRule(prelude string) bool {
	low := strings.ToLower(strings.TrimSpace(prelude))
	for _, at := range groupingAtRules {
		if strings.HasPrefix(low, at) {
			return true
		}
	}
	return false
}

func isCSSSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f'
}

// cssCommentEnd returns the offset just past a comment starting at i, and
// whether there was one.
func cssCommentEnd(css string, i, end int) (int, bool) {
	if i+1 >= end || css[i] != '/' || css[i+1] != '*' {
		return i, false
	}
	j := strings.Index(css[i+2:end], "*/")
	if j < 0 {
		return end, true
	}
	return i + 2 + j + 2, true
}

// cssStringEnd returns the offset just past a quoted string starting at i. An
// unterminated string runs to the end of the input, which is what a browser
// does with one too.
func cssStringEnd(css string, i, end int) int {
	quote := css[i]
	i++
	for i < end {
		if css[i] == '\\' {
			i += 2
			continue
		}
		if css[i] == quote {
			return i + 1
		}
		i++
	}
	return end
}

// rewriteSelectorList rewrites one selector list, which is the prelude of a
// style rule: everything before its opening brace.
//
// The whitespace around the prelude is preserved rather than trimmed, so the
// only thing a rewritten stylesheet differs from the sender's in is the
// selectors that had to change.
func rewriteSelectorList(sel string) string {
	lead := sel[:len(sel)-len(strings.TrimLeft(sel, " \t\n\r\f"))]
	trail := sel[len(strings.TrimRight(sel, " \t\n\r\f")):]
	parts := splitSelectors(sel)
	seen := map[string]bool{}
	kept := parts[:0]
	for _, p := range parts {
		r := rewriteSelector(p)
		// html, body and :root all become :host, so "html, body { }" would
		// otherwise arrive as the same selector twice. A duplicated selector is
		// not wrong, but it is noise in a stylesheet a reader may well be
		// diffing against the sender's.
		if seen[r] {
			continue
		}
		seen[r] = true
		kept = append(kept, r)
	}
	return lead + strings.Join(kept, ", ") + trail
}

// splitSelectors splits a selector list on its top-level commas, keeping the
// whitespace around each part out of the split.
func splitSelectors(sel string) []string {
	var parts []string
	depth := 0
	start := 0
	for i := 0; i < len(sel); {
		switch sel[i] {
		case '"', '\'':
			i = cssStringEnd(sel, i, len(sel))
			continue
		case '(', '[':
			depth++
		case ')', ']':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				parts = append(parts, sel[start:i])
				start = i + 1
			}
		}
		i++
	}
	parts = append(parts, sel[start:])
	for i, p := range parts {
		parts[i] = strings.TrimSpace(p)
	}
	return parts
}

// rewriteSelector rewrites one complex selector: every identifier in a type
// position that names html or body, and every :root, becomes :host.
//
// Type position is the whole of the discrimination, and it is what keeps
// ".body" and "#body" — which are a class and an id a sender chose, and must
// stay — apart from "body". It is tracked by walking the selector left to right:
// an identifier is a type selector when the last significant thing before it is
// the start of the selector, a combinator, a comma, or an open parenthesis.
// Attribute selectors, classes, ids and pseudos all consume their own names, so
// an identifier inside one is never read as a type.
//
// A functional pseudo's argument is a selector list too (":is(html)", ":not(body)"),
// so it is recursed into rather than copied.
func rewriteSelector(sel string) string {
	var b strings.Builder
	atType := true
	for i := 0; i < len(sel); {
		c := sel[i]
		switch {
		case c == '[':
			j := cssBracketEnd(sel, i)
			b.WriteString(sel[i:j])
			i = j
			atType = false
		case c == '"' || c == '\'':
			j := cssStringEnd(sel, i, len(sel))
			b.WriteString(sel[i:j])
			i = j
			atType = false
		case c == '(':
			j := cssParenEnd(sel, i)
			b.WriteString("(" + rewriteSelectorList(sel[i+1:j-1]) + ")")
			i = j
			atType = false
		case c == '.' || c == '#':
			j := i + 1 + cssIdentLen(sel[i+1:])
			b.WriteString(sel[i:j])
			i = j
			atType = false
		case c == ':':
			j := i + 1 + cssIdentLen(sel[i+1:])
			name := sel[i+1 : j]
			if strings.EqualFold(name, "root") {
				b.WriteString(":host")
			} else {
				b.WriteString(sel[i:j])
			}
			i = j
			atType = false
		case c == '*' || c == '>' || c == '+' || c == '~' || c == ',':
			b.WriteByte(c)
			i++
			atType = true
		case isCSSSpace(c):
			b.WriteByte(c)
			i++
			atType = true
		case isCSSIdentStart(c):
			j := i + cssIdentLen(sel[i:])
			name := sel[i:j]
			if atType && (strings.EqualFold(name, "html") || strings.EqualFold(name, "body")) {
				b.WriteString(":host")
			} else {
				b.WriteString(name)
			}
			i = j
			atType = false
		default:
			b.WriteByte(c)
			i++
			atType = false
		}
	}
	return collapseHostChain(b.String())
}

// collapseHostChain turns ":host :host" and ":host > :host" into ":host".
//
// "html body" is a selector for a body element, reached through its html parent;
// rewriting both halves gives a :host inside a :host, which matches nothing —
// there is exactly one host element and it cannot be its own descendant. The two
// halves are the same element by construction, so collapsing them is the rewrite
// that keeps the rule's meaning.
var reHostChain = regexp.MustCompile(`:host\s*(?:[>+~]\s*)?:host`)

func collapseHostChain(sel string) string {
	for {
		next := reHostChain.ReplaceAllString(sel, ":host")
		if next == sel {
			return sel
		}
		sel = next
	}
}

// cssBracketEnd returns the offset just past an attribute selector starting at
// i, respecting a quoted value.
func cssBracketEnd(sel string, i int) int {
	for j := i + 1; j < len(sel); {
		if sel[j] == '"' || sel[j] == '\'' {
			j = cssStringEnd(sel, j, len(sel))
			continue
		}
		if sel[j] == ']' {
			return j + 1
		}
		j++
	}
	return len(sel)
}

// cssParenEnd returns the offset just past a balanced parenthesised run
// starting at i.
func cssParenEnd(sel string, i int) int {
	depth := 0
	for j := i; j < len(sel); {
		switch sel[j] {
		case '"', '\'':
			j = cssStringEnd(sel, j, len(sel))
			continue
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return j + 1
			}
		}
		j++
	}
	return len(sel)
}

// cssIdentLen measures an identifier's run: letters, digits, hyphens, escapes
// and anything non-ASCII.
func cssIdentLen(s string) int {
	i := 0
	for i < len(s) {
		c := s[i]
		if c == '\\' {
			i += 2
			continue
		}
		if isCSSIdentChar(c) {
			i++
			continue
		}
		break
	}
	return i
}

func isCSSIdentStart(c byte) bool {
	return c == '_' || c == '-' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= 0x80
}

func isCSSIdentChar(c byte) bool {
	return isCSSIdentStart(c) || c >= '0' && c <= '9'
}

// writeNode serialises one node, and it exists because html.Render cannot be
// used here: it escapes every text node, and a <style> element's text is CSS, not
// HTML. A ">" combinator would come back as "&gt;" and the stylesheet the whole
// mode exists to preserve would be broken at the first child selector.
//
// So the escaping is decided per node instead: raw inside <style>, escaped
// everywhere else. Attribute values are escaped on the way out; the parser has
// already normalised entities on the way in.
func writeNode(out *strings.Builder, n *html.Node) {
	switch n.Type {
	case html.TextNode:
		if n.Parent != nil && n.Parent.DataAtom == atom.Style {
			out.WriteString(n.Data)
			return
		}
		out.WriteString(stdhtml.EscapeString(n.Data))
	case html.CommentNode:
		// Comments do not survive pruneOriginal; a comment reached here is one
		// the caller added, and is written as-is.
		out.WriteString("<!--" + n.Data + "-->")
	case html.ElementNode:
		out.WriteString("<" + n.Data)
		for _, a := range n.Attr {
			out.WriteString(" " + a.Key + `="` + stdhtml.EscapeString(a.Val) + `"`)
		}
		out.WriteString(">")
		if voidElements[n.DataAtom] {
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			writeNode(out, c)
		}
		out.WriteString("</" + n.Data + ">")
	}
}

// voidElements are the elements that take no closing tag. Only the ones this
// pass keeps are listed.
var voidElements = map[atom.Atom]bool{
	atom.Br: true, atom.Img: true, atom.Hr: true, atom.Link: true,
	atom.Wbr: true, atom.Area: true, atom.Col: true,
}
