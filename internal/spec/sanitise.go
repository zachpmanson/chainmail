package spec

import (
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// The schema calls body "trusted HTML", and this file is why that is now a
// property rather than a promise. Every body that leaves bodyHTML passes
// through sanitiseBody, whether it was shaped from text in body.go, taken
// from the sender's own html part in htmlbody.go, or recovered from a quoting
// host in htmlrecover.go. The allowlist here is the boundary the renderer
// trusts: a <script>, an on* handler, a javascript: href, an <svg onload>
// can reach the page only by first surviving this pass, and none of them can.
//
// It is an allowlist, not a blocklist — a tag keeps its place only when the
// map below names it, and even then only the attributes it is allowed, and
// even then only the URL schemes that are allowed. Everything unlisted is
// flattened to its own text, so unknown or hostile elements still read as
// the words a sender wrote, just without the chrome.
//
// What a hand-written spec (one that never ran through this generator) gets
// is the contract in schema/timeline.schema.json: the same allowlist, stated
// there, and the plain requirement that a spec from an untrusted source must
// be sanitised before it is rendered.

// allowedTags is the whole of what a page may render.
//
// It covers what the generator itself writes — p, br, blockquote, a, the
// <details class="sig"> fold, the <p class="ed"> gloss — and the sender
// markup the html part paths exist to preserve: its lists, tables, images,
// links and emphasis. The list is deliberately generous about the prose
// shapes mail clients actually emit, and deliberately short of anything that
// can act rather than say: no form controls, no media, no embedding, no
// document-level elements. A tag that is not here is flattened (its
// attributes dropped, its text kept) or dropped whole (see droppedSubtree).
var allowedTags = map[atom.Atom]bool{
	// Blocks.
	atom.P:          true,
	atom.Div:        true,
	atom.Br:         true,
	atom.Blockquote: true,
	atom.Hr:         true,
	atom.Pre:        true,
	atom.Ul:         true,
	atom.Ol:         true,
	atom.Li:         true,
	atom.Dl:         true,
	atom.Dt:         true,
	atom.Dd:         true,
	atom.H1:         true,
	atom.H2:         true,
	atom.H3:         true,
	atom.H4:         true,
	atom.H5:         true,
	atom.H6:         true,
	atom.Table:      true,
	atom.Tbody:      true,
	atom.Thead:      true,
	atom.Tfoot:      true,
	atom.Tr:         true,
	atom.Td:         true,
	atom.Th:         true,
	// Inline.
	atom.A:      true,
	atom.Img:    true,
	atom.Span:   true,
	atom.B:      true,
	atom.Strong: true,
	atom.I:      true,
	atom.Em:     true,
	atom.U:      true,
	atom.S:      true,
	atom.Del:    true,
	atom.Ins:    true,
	atom.Font:   true,
	atom.Code:   true,
	atom.Kbd:    true,
	atom.Samp:   true,
	atom.Sub:    true,
	atom.Sup:    true,
	atom.Small:  true,
	atom.Big:    true,
	atom.Mark:   true,
	atom.Abbr:   true,
	atom.Cite:   true,
	atom.Q:      true,
	atom.Var:    true,
	atom.Wbr:    true,
	atom.Center: true,
	// The signature fold and the editorial gloss.
	atom.Details: true,
	atom.Summary: true,
}

// droppedSubtree names elements whose contents are dropped whole rather than
// flattened into the surrounding text. Scripts and stylesheets are the sharp
// case — their content is raw markup, not prose, and re-emitted as text it
// would still be the exact thing that executes — and the embedding elements
// are dropped whole too, because an <iframe> or an <object> is a place where
// another document runs and there is no flattened form of that. Form
// controls and the media elements go with them: an input, a select, a
// <video> has no rendition in a transcript.
//
// Everything else outside the allowlist keeps its text: an unknown tag
// carries the sender's words, and those words stay on the page even though
// the tag does not.
var droppedSubtree = map[atom.Atom]bool{
	atom.Script:   true,
	atom.Noscript: true,
	atom.Style:    true,
	atom.Link:     true,
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
	atom.Picture:  true,
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
	atom.Map:      true,
	atom.Area:     true,
	atom.Dialog:   true,
	atom.Menu:     true,
	atom.Bdi:      true,
	atom.Bdo:      true,
	atom.Nav:      true,
}

// allowedAttrs is the per-element attribute allowlist. Everything not named
// here is dropped: id, event handlers, and any attribute some future spec
// thought an element could carry. The two attributes that apply more widely
// — style and title — are handled separately, style because it is filtered
// to keptStyleProps rather than allowed or dropped wholesale, title because
// it is harmless wherever a browser shows a tooltip. class is filtered to
// allowedClasses, the generator's own values.
var allowedAttrs = map[atom.Atom]map[string]bool{
	atom.A:    {"href": true, "target": true, "rel": true},
	atom.Img:  {"src": true, "alt": true, "width": true, "height": true},
	atom.Td:   {"colspan": true, "rowspan": true},
	atom.Th:   {"colspan": true, "rowspan": true},
	atom.Font: {"color": true},
}

// allowedClasses are the only class names that may survive to the page. The
// sender's classes were already stripped before this pass (see stripChrome);
// the three that remain are the generator's own — the editorial gloss, the
// signature disclosure, and the fold's content wrapper.
var allowedClasses = map[string]bool{
	"ed":    true,
	"sig":   true,
	"sigbd": true,
}

// sanitiseBody applies the allowlist to one rendered body and re-serialises
// it. It is the single exit from bodyHTML, so a page never renders a body
// that did not come through here.
//
// The pass is a parse, a prune, and a re-render: the markup is re-serialised
// from the parse tree, so the output is balanced even where the input was
// not, and every byte of it is written by html.Render's own escaping on the
// way out. A value that survives the pass cannot act — the only survivors
// are the tags above, with the attributes above, pointing at http(s) or a
// data: image.
func sanitiseBody(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return raw
	}
	doc, err := html.Parse(strings.NewReader(raw))
	if err != nil {
		// Only a read error, which a strings.Reader does not produce.
		return raw
	}
	body := findBody(doc)
	if body == nil {
		// Unreachable for real input: HTML5 tree construction always makes
		// a body. Return the input unchanged rather than guess — a body the
		// parser refuses is not a body this pass can judge.
		return raw
	}
	sanitiseChildren(body)
	var b strings.Builder
	for c := body.FirstChild; c != nil; c = c.NextSibling {
		if err := html.Render(&b, c); err != nil {
			return raw
		}
	}
	return b.String()
}

// sanitiseChildren prunes one element's children in place: comments go,
// dangerous elements go whole, and an element outside the allowlist is
// flattened to its own sanitised children. Children are visited first, so
// the decision about an element is made on an already-sanitised subtree and
// flattening one never re-exposes anything dangerous it held.
func sanitiseChildren(parent *html.Node) {
	var next *html.Node
	for c := parent.FirstChild; c != nil; c = next {
		next = c.NextSibling
		switch c.Type {
		case html.TextNode:
			continue
		case html.CommentNode, html.DoctypeNode:
			parent.RemoveChild(c)
			continue
		case html.ElementNode:
			sanitiseChildren(c)
			if droppedSubtree[c.DataAtom] {
				parent.RemoveChild(c)
				continue
			}
			if !allowedTags[c.DataAtom] {
				flatten(parent, c)
				continue
			}
			c.Attr = allowAttrs(c)
		}
	}
}

// flatten replaces an element with its own children, dropping the element
// and its attributes while keeping what it contained. The children were
// already sanitised, so nothing dangerous rides along. Each child is detached
// from the element before it is inserted in the parent — the html module
// refuses to move an attached node.
func flatten(parent, n *html.Node) {
	for ch := n.FirstChild; ch != nil; {
		next := ch.NextSibling
		n.RemoveChild(ch)
		parent.InsertBefore(ch, n)
		ch = next
	}
	parent.RemoveChild(n)
}

// keptAttrs filters one element's attributes to the allowlist. style is
// filtered to keptStyleProps (the same five properties stripChrome keeps), a
// href and an img src are subject to their URL scheme, and class survives
// only in the generator's own values.
func allowAttrs(n *html.Node) []html.Attribute {
	out := make([]html.Attribute, 0, len(n.Attr))
	for _, a := range n.Attr {
		key := strings.ToLower(a.Key)
		switch key {
		case "style":
			if v := keptStyle(a.Val); v != "" {
				out = append(out, html.Attribute{Key: "style", Val: v})
			}
			continue
		case "class":
			if allowedClasses[strings.ToLower(a.Val)] {
				out = append(out, a)
			}
			continue
		case "title":
			out = append(out, a)
			continue
		}
		if !allowedAttrs[n.DataAtom][key] {
			continue
		}
		if urlAttr(n.DataAtom, key) && !goodURL(key, a.Val) {
			continue
		}
		out = append(out, a)
	}
	return out
}

// urlAttr reports whether an attribute carries a URL, so its scheme gets
// judged before it is kept. Only two do: an anchor's href and an image's src.
func urlAttr(tag atom.Atom, key string) bool {
	return tag == atom.A && key == "href" || tag == atom.Img && key == "src"
}

// goodURL is the whole URL boundary: an anchor may point at http(s) and an
// image may load from http(s) or a data: image. Nothing else — javascript:,
// data:text/html, a relative href that a hostile base could redirect, all of
// it drops the attribute and leaves the element's text behind. Whitespace in
// a URL is a parse accident rather than a URL, so it is refused too.
func goodURL(attr string, v string) bool {
	low := strings.ToLower(strings.TrimSpace(v))
	if strings.IndexAny(low, " \t\r\n\f") >= 0 {
		return false
	}
	if strings.HasPrefix(low, "http://") || strings.HasPrefix(low, "https://") {
		return true
	}
	return attr == "src" && strings.HasPrefix(low, "data:image/")
}
