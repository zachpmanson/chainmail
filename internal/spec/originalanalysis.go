package spec

import (
	"math"
	"strconv"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// OriginalAnalysis is a receipt for a conservative, HTML-only display default.
// It is not a fidelity judgement: unknown designs stay in the simplified view.
type OriginalAnalysis struct {
	Score              int             `json:"score"`
	Matches            []OriginalMatch `json:"matches"`
	DefaultsToOriginal bool            `json:"defaultsToOriginal"`
	Uncertain          bool            `json:"uncertain"`
}

type OriginalMatch struct {
	Rule   string `json:"rule"`
	Points int    `json:"points"`
}

// Bound both parsing and the shared boundary recognizers' subtree work. Display
// uses its independent sanitizer and remains available when analysis declines.
const analysisMaxBytes = 2 << 20
const analysisMaxNodes = 20000
const analysisMaxWork = 100000

type analysisNode struct {
	style        map[string]string
	telephone    bool
	font, column float64
	background   string
	padding      float64
	facts        originalFacts
	start, end   int
	startArea    float64
}

type analysisFlow struct {
	words      int
	area, font float64
	heading    bool
	footer     bool
}

type analysisTree struct {
	nodes       map[*html.Node]*analysisNode
	flows       []analysisFlow
	pending     analysisFlow
	area        float64
	nextHeading []int
	flowArea    []float64
}

// AnalyseOriginal scores a private tree, never the displayed OriginalBody output.
func AnalyseOriginal(raw string) OriginalAnalysis {
	result := OriginalAnalysis{Matches: []OriginalMatch{}}
	if len(raw) > analysisMaxBytes {
		result.Uncertain = true
		return result
	}
	doc, err := html.Parse(strings.NewReader(raw))
	if err != nil {
		result.Uncertain = true
		return result
	}
	tree := &analysisTree{nodes: map[*html.Node]*analysisNode{}}
	nodes, work := 0, 0
	var prepare func(*html.Node, int) bool
	prepare = func(n *html.Node, depth int) bool {
		nodes++
		work += depth + 1
		if nodes > analysisMaxNodes || work > analysisMaxWork {
			return false
		}
		tree.nodes[n] = &analysisNode{style: analysisStyle(n), telephone: n.DataAtom == atom.A && strings.HasPrefix(strings.ToLower(strings.TrimSpace(attr(n, "href"))), "tel:")}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if !prepare(c, depth+1) {
				return false
			}
		}
		return true
	}
	if !prepare(doc, 0) {
		result.Uncertain = true
		return result
	}
	_, body := documentParts(doc)
	if body == nil {
		return result
	}
	// Remember only the contact scheme before the rendering policy strips tel:.
	// No executable attributes or new URL permissions reach OriginalBody.
	pruneOriginal(doc)
	tree.visible(doc, 16)
	result.Uncertain = analysisBoundaries(body)
	tree.snapshot(doc)
	tree.contacts(body, false)
	tree.snapshot(doc)
	total := tree.nodes[body].facts.area
	var best, weak []OriginalMatch
	var visit func(*html.Node) float64
	visit = func(n *html.Node) float64 {
		d := tree.nodes[n]
		f := d.facts
		if n.Type == html.ElementNode && analysisContainer(n) && f.words > 0 && tree.page(n) {
			matches := []OriginalMatch{{Rule: "bounded or padded main layout", Points: 1}}
			if tree.action(n) {
				matches = append(matches, OriginalMatch{Rule: "designed action", Points: 1})
			}
			if tree.heading(n) || f.modules >= 2 || f.columns {
				matches = append(matches, OriginalMatch{Rule: "visual hierarchy or modules", Points: 1})
			}
			if f.graphics > 0 {
				matches = append(matches, OriginalMatch{Rule: "prominent graphic", Points: 1})
			}
			if len(matches) >= 2 {
				if len(matches) > len(best) {
					best = matches
				}
				return f.area
			}
			if f.area*2 >= total {
				weak = matches
			}
		}
		// Same-width centered panels form one column, even through neutral table
		// scaffolding. Unaligned/disconnected partial regions never pool votes.
		if matches, area := tree.alignedColumn(n); area > 0 {
			if len(matches) > len(best) {
				best = matches
			}
			return area
		}
		covered := 0.0
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			covered += visit(c)
		}
		return covered
	}
	covered := visit(body)
	if covered*2 < total {
		best = weak
	}
	if best != nil {
		result.Matches = best
		result.Score = len(best)
	}
	result.DefaultsToOriginal = result.Score >= 2 && !result.Uncertain
	return result
}

// Remove explicit quote subtrees only, not cutFrom's suffix: authored siblings
// before, between and after quotes survive. Header-only/unwrapped history cannot
// be separated safely from inline answers, so retain it and withhold the default.
func analysisBoundaries(n *html.Node) bool {
	for _, sig := range markedSignatures(n) {
		sig.Parent.RemoveChild(sig)
	}
	return analysisBoundaryWalk(n)
}

func analysisBoundaryWalk(n *html.Node) bool {
	uncertain := false
	for c := n.FirstChild; c != nil; {
		next := c.NextSibling
		if c.Type == html.ElementNode && c.DataAtom == atom.Details && hasClass(c, "sig") {
			n.RemoveChild(c)
		} else if analysisQuote(c) {
			n.RemoveChild(c)
		} else {
			if analysisBoundaryWalk(c) {
				uncertain = true
			}
			if isBoundary(c) {
				uncertain = true
			}
		}
		c = next
	}
	// Shared recognition includes Outlook headers split across p/div/br flows.
	if len(boundarySpans(n)) > 0 {
		uncertain = true
	}
	return uncertain
}

func analysisQuote(n *html.Node) bool {
	if !isBoundary(n) {
		return false
	}
	if n.DataAtom == atom.Blockquote && strings.EqualFold(attr(n, "type"), "cite") {
		return true
	}
	for _, c := range classes(n) {
		if boundaryClass[c] && c != "gmail_attr" && c != "moz-cite-prefix" {
			return true
		}
	}
	return false
}

// Inline declarations only; importance wins before source order.
func analysisStyle(n *html.Node) map[string]string {
	out := map[string]string{}
	important := map[string]bool{}
	for _, declaration := range strings.Split(attr(n, "style"), ";") {
		key, value, ok := strings.Cut(declaration, ":")
		if !ok {
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.ToLower(strings.TrimSpace(value))
		imp := false
		if at := strings.LastIndex(value, "!"); at >= 0 && strings.TrimSpace(value[at+1:]) == "important" {
			imp = true
			value = strings.TrimSpace(value[:at])
		}
		if !important[key] || imp {
			out[key] = value
			important[key] = imp
		}
	}
	return out
}

func analysisNumeric(value string) (float64, string, bool) {
	value = strings.TrimSpace(value)
	end := 0
	for end < len(value) && (value[end] >= '0' && value[end] <= '9' || value[end] == '.' || end == 0 && (value[end] == '+' || value[end] == '-')) {
		end++
	}
	if end == 0 {
		return 0, "", false
	}
	n, err := strconv.ParseFloat(value[:end], 64)
	return n, strings.TrimSpace(value[end:]), err == nil
}

func analysisNumber(value string) float64 {
	n, unit, ok := analysisNumeric(value)
	if !ok {
		return 0
	}
	switch unit {
	case "pt":
		n *= 4.0 / 3
	case "em", "rem":
		n *= 16
	case "", "px":
	default:
		return 0
	}
	return n
}

func analysisZero(value string) bool {
	n, unit, ok := analysisNumeric(value)
	return ok && n == 0 && (unit == "" || unit == "px" || unit == "pt" || unit == "em" || unit == "rem" || unit == "%")
}

func analysisFontSize(value string, inherited float64) float64 {
	n, unit, ok := analysisNumeric(value)
	if !ok || n < 0 {
		return inherited
	}
	switch unit {
	case "%":
		return n / 100 * inherited
	case "em":
		return n * inherited
	case "rem":
		return n * 16
	case "pt":
		return n * 4 / 3
	case "", "px":
		return n
	}
	return inherited
}

func (t *analysisTree) dimension(n *html.Node, key string) float64 {
	v := t.nodes[n].style[key]
	if v == "" {
		v = attr(n, key)
	}
	return analysisNumber(v)
}

// Resolve intended column geometry, not viewport pixels or intrinsic image sizes.
// Unknown shrink-to-fit/multi-column constraints break percentage propagation.
func (t *analysisTree) columnWidth(n *html.Node) float64 {
	s := t.nodes[n].style
	parent := 0.0
	if n.Parent != nil {
		p := t.nodes[n.Parent]
		parent = p.column
		if p.style["display"] == "grid" || p.style["display"] == "flex" && p.style["flex-direction"] != "column" {
			parent = 0
		}
	}
	width := s["width"]
	if width == "" {
		width = attr(n, "width")
	}
	if (n.DataAtom == atom.Td || n.DataAtom == atom.Th) && width == "" && n.Parent != nil {
		cells := 0
		for c := n.Parent.FirstChild; c != nil; c = c.NextSibling {
			if c.DataAtom == atom.Td || c.DataAtom == atom.Th {
				cells++
			}
		}
		if cells > 1 {
			parent = 0
		}
	}
	resolve := func(value string) float64 {
		if size, unit, ok := analysisNumeric(value); ok && unit == "%" {
			return max(0, parent*size/100)
		}
		return max(0, analysisNumber(value))
	}
	column := parent
	if width != "" && width != "auto" {
		column = resolve(width)
	} else if n.DataAtom == atom.Img || n.DataAtom == atom.Table || s["display"] == "inline-block" || s["display"] == "inline-flex" {
		column = 0
	}
	if maximum := resolve(s["max-width"]); maximum > 0 {
		if column > 0 {
			column = min(column, maximum)
		} else if _, unit, ok := analysisNumeric(s["max-width"]); n.DataAtom != atom.Img && ok && unit != "%" && (width == "" || width == "auto" || strings.HasSuffix(width, "%")) {
			column = maximum
		}
	}
	return column
}

func (t *analysisTree) visible(n *html.Node, font float64) {
	d := t.nodes[n]
	s := d.style
	if v, ok := s["font-size"]; ok {
		font = analysisFontSize(v, font)
	}
	for c := n.FirstChild; c != nil; {
		next := c.NextSibling
		cs := t.nodes[c].style
		_, hidden := cAttr(c, "hidden")
		drop := hidden || cs["display"] == "none" || cs["visibility"] == "hidden" || cs["visibility"] == "collapse" || analysisZero(cs["opacity"])
		drop = drop || c.DataAtom == atom.Style || c.DataAtom == atom.Link || c.DataAtom == atom.Head
		if c.Type == html.TextNode && font == 0 {
			drop = true
		}
		if c.DataAtom == atom.Img {
			for _, key := range []string{"width", "height"} {
				v := cs[key]
				if v == "" {
					v = attr(c, key)
				}
				size := t.dimension(c, key)
				if analysisZero(v) || size > 0 && size <= 2 {
					drop = true
				}
			}
		}
		if drop {
			n.RemoveChild(c)
		} else {
			t.visible(c, font)
		}
		c = next
	}
}

func cAttr(n *html.Node, key string) (string, bool) {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val, true
		}
	}
	return "", false
}

type originalFacts struct {
	words, contacts, images, graphics, actions, modules int
	area, actionAt                                      float64
	columns, bound, painted                             bool
	prefix, suffix, maxFlow                             int
	breaks                                              bool
}

func (f *originalFacts) add(c originalFacts, evidence bool) {
	f.words += c.words
	f.area += c.area
	f.contacts += c.contacts
	f.images += c.images
	f.maxFlow = max(f.maxFlow, c.maxFlow, f.suffix+c.prefix)
	if !f.breaks {
		f.prefix += c.prefix
	}
	if c.breaks {
		f.suffix = c.suffix
	} else {
		f.suffix += c.suffix
	}
	f.breaks = f.breaks || c.breaks
	f.bound = f.bound || c.bound
	f.painted = f.painted || c.painted
	if evidence {
		f.graphics += c.graphics
		if c.actions > 0 && (f.actions == 0 || c.actionAt < f.actionAt) {
			f.actionAt = c.actionAt
		}
		f.actions += c.actions
		f.modules += c.modules
		f.columns = f.columns || c.columns
	}
}

func analysisBlock(n *html.Node) bool {
	switch n.DataAtom {
	case atom.P, atom.Div, atom.Br, atom.Td, atom.Th, atom.Tr, atom.Li, atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6, atom.Section, atom.Article, atom.Main, atom.Footer, atom.Blockquote:
		return true
	}
	return false
}

func analysisContainer(n *html.Node) bool {
	switch n.DataAtom {
	case atom.Div, atom.Table, atom.Td, atom.Section, atom.Article, atom.Main, atom.Body:
		return true
	}
	return false
}

func (t *analysisTree) flush() {
	if t.pending.words > 0 {
		t.flows = append(t.flows, t.pending)
	}
	t.pending = analysisFlow{}
}

func (t *analysisTree) snapshot(root *html.Node) {
	t.flows = nil
	t.pending = analysisFlow{}
	t.area = 0
	var walk func(*html.Node, float64, string, bool, bool)
	walk = func(n *html.Node, font float64, bg string, heading, footer bool) {
		d := t.nodes[n]
		s := d.style
		f := originalFacts{}
		switch n.DataAtom {
		case atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6:
			ratio := map[atom.Atom]float64{atom.H1: 2, atom.H2: 1.5, atom.H3: 1.17, atom.H4: 1, atom.H5: .83, atom.H6: .67}
			font *= ratio[n.DataAtom]
			heading = true
		}
		if v, ok := s["font-size"]; ok {
			font = analysisFontSize(v, font)
		}
		d.font = font
		d.column = t.columnWidth(n)
		ownBG := s["background-color"]
		if ownBG == "" {
			ownBG = s["background"]
		}
		if ownBG == "" {
			ownBG = attr(n, "bgcolor")
		}
		if color := analysisBackgroundValue(ownBG); color != "" {
			bg = color
		}
		d.background = bg
		d.padding = analysisNumber(attr(n, "cellpadding"))
		for _, key := range []string{"padding", "padding-left", "padding-right", "padding-top", "padding-bottom"} {
			for _, v := range strings.Fields(s[key]) {
				d.padding = max(d.padding, analysisNumber(v))
			}
		}
		footer = footer || n.DataAtom == atom.Footer
		block := analysisBlock(n)
		if block {
			t.flush()
		}
		d.start = len(t.flows)
		d.startArea = t.area
		if n.Type == html.TextNode {
			f.words = len(strings.Fields(n.Data))
			f.area = float64(f.words) * math.Pow(min(32, max(8, font)), 2)
			f.prefix = f.words
			f.suffix = f.words
			f.maxFlow = f.words
			t.pending.words += f.words
			t.pending.area += f.area
			t.pending.font = max(t.pending.font, font)
			t.pending.heading = t.pending.heading || heading
			t.pending.footer = t.pending.footer || footer
			t.area += f.area
		}
		items := 0
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c, font, bg, heading, footer)
			cf := t.nodes[c].facts
			f.add(cf, c.DataAtom != atom.Footer)
			if cf.words > 0 || cf.images > 0 {
				items++
			}
		}
		if block {
			t.flush()
			f.prefix = 0
			f.suffix = 0
			f.breaks = true
		}
		d.end = len(t.flows)
		if t.pending.words > 0 {
			d.end++
		}
		if n.Type == html.ElementNode {
			w := max(t.dimension(n, "width"), t.dimension(n, "max-width"))
			f.bound = f.bound || w >= 320 && w <= 1200
			if n.DataAtom == atom.Img {
				f.images++
				w, h := d.column, t.dimension(n, "height")
				if w >= 320 && h >= 60 || w >= 250 && (h == 0 || h >= 100) || w >= 180 && h >= 150 {
					f.graphics++
				}
			}
			if n.DataAtom == atom.A {
				href := strings.ToLower(strings.TrimSpace(attr(n, "href")))
				if d.telephone || strings.HasPrefix(href, "mailto:") {
					f.contacts++
				} else if href != "" && (s["display"] == "inline-block" || s["display"] == "block" || s["display"] == "inline-flex") && (t.painted(n) || f.painted) {
					f.actions++
					f.actionAt = d.startArea
				}
			}
			f.painted = f.painted || t.painted(n) && f.maxFlow <= 12
			horizontal := s["display"] == "flex" && s["flex-direction"] != "column" && s["flex-direction"] != "column-reverse" || s["display"] == "grid" && strings.Contains(s["grid-template-columns"], " ")
			f.columns = f.columns || horizontal && items >= 2
			parentBG := "white"
			if n.Parent != nil {
				parentBG = t.nodes[n.Parent].background
			}
			panel := n.DataAtom == atom.Div || n.DataAtom == atom.Section || n.DataAtom == atom.Article || n.DataAtom == atom.Table
			if panel && d.padding >= 12 && (bg != parentBG || analysisBorder(s["border"]) || analysisBorder(s["border-top"])) && f.words > 0 {
				f.modules = max(1, f.modules)
			}
		}
		d.facts = f
	}
	walk(root, 16, "white", false, false)
	t.flush()
	t.flowArea = make([]float64, len(t.flows)+1)
	for i, flow := range t.flows {
		t.flowArea[i+1] = t.flowArea[i] + flow.area
	}
	t.nextHeading = make([]int, len(t.flows)+1)
	next := len(t.flows)
	t.nextHeading[len(t.flows)] = next
	for i := len(t.flows) - 1; i >= 0; i-- {
		f := t.flows[i]
		if i+1 < len(t.flows) && f.words <= 20 && !f.footer && !t.flows[i+1].footer && !t.flows[i+1].heading && (f.heading || t.flows[i+1].font > 0 && f.font >= t.flows[i+1].font*1.25) {
			next = i
		}
		t.nextHeading[i] = next
	}
}

func analysisBackgroundValue(v string) string {
	v = strings.ReplaceAll(strings.ToLower(v), " ", "")
	if v == "" || v == "none" || v == "transparent" {
		return ""
	}
	if strings.HasPrefix(v, "rgba(") || strings.Contains(v, "/") {
		if at := strings.LastIndexAny(v, ",/"); at >= 0 && strings.HasSuffix(v, ")") && analysisZero(v[at+1:len(v)-1]) {
			return ""
		}
	}
	if v == "#fff" || v == "#ffffff" || v == "rgb(255,255,255)" {
		return "white"
	}
	return v
}

func analysisBorder(v string) bool {
	if v == "" || strings.Contains(v, "none") || strings.Contains(v, "hidden") {
		return false
	}
	for _, part := range strings.Fields(v) {
		if analysisZero(part) {
			return false
		}
	}
	return true
}

func (t *analysisTree) surface(n *html.Node) bool {
	s := t.nodes[n].style
	bg := s["background-color"]
	if bg == "" {
		bg = s["background"]
	}
	if bg == "" {
		bg = attr(n, "bgcolor")
	}
	return analysisBackgroundValue(bg) != "" || analysisBorder(s["border"])
}

func (t *analysisTree) painted(n *html.Node) bool {
	d := t.nodes[n]
	s := d.style
	return t.surface(n) && (d.padding >= 6 || s["border-radius"] != "")
}

// A tiny late control in a bare column is not evidence about the main prose.
// A genuinely painted/padded enclosing card may put its primary action last.
func (t *analysisTree) action(n *html.Node) bool {
	d := t.nodes[n]
	return d.facts.actions > 0 && ((d.facts.actionAt-d.startArea)*2 <= d.facts.area || d.padding >= 16 && t.surface(n))
}

func (t *analysisTree) heading(n *html.Node) bool {
	d := t.nodes[n]
	if d.start >= len(t.nextHeading) {
		return false
	}
	i := t.nextHeading[d.start]
	if i+1 >= d.end {
		return false
	}
	// A heading must lead primary content, not label a tiny trailing footer.
	before := t.flowArea[i] - t.flowArea[d.start]
	return before*2 <= d.facts.area
}

func (t *analysisTree) centered(n *html.Node) bool {
	s := t.nodes[n].style
	if strings.EqualFold(attr(n, "align"), "center") {
		return true
	}
	if s["margin-left"] == "auto" && s["margin-right"] == "auto" {
		return true
	}
	m := strings.Fields(s["margin"])
	return len(m) == 2 && m[1] == "auto" || len(m) == 4 && m[1] == "auto" && m[3] == "auto"
}

func (t *analysisTree) page(n *html.Node) bool {
	d := t.nodes[n]
	w := max(t.dimension(n, "width"), t.dimension(n, "max-width"))
	bounded := w >= 320 && w <= 1200
	surface := t.surface(n)
	// A width declaration alone is not a main design. A leading typographic
	// hierarchy can establish the structure of a minimally styled bounded page.
	return bounded && (d.padding >= 12 || t.centered(n) || t.heading(n) || d.facts.columns || d.facts.modules >= 2) || d.padding >= 16 && surface || n.DataAtom == atom.Table && t.centered(n) && surface && d.facts.bound
}

func contactShape(f originalFacts) bool {
	return f.contacts > 0 && f.images > 0 && f.graphics == 0 && f.actions == 0 && f.words <= 120 && f.maxFlow <= 12
}

// Contact recognition uses flow shape, independently of page-candidate tags.
func (t *analysisTree) contacts(n *html.Node, earlier bool) {
	// A br-delimited contact suffix may have no paragraph wrapper. Examine each
	// inline run once, backwards, preserving text before its separating break.
	var run []*html.Node
	trim := func() {
		suffix := originalFacts{}
		var first *html.Node
		for i := len(run) - 1; i >= 0; i-- {
			c := run[i]
			if c.DataAtom == atom.Br && contactShape(suffix) && (earlier || t.nodes[n].facts.words > suffix.words) {
				first = c.NextSibling
			}
			f := t.nodes[c].facts
			f.add(suffix, true)
			suffix = f
		}
		if first != nil {
			end := run[len(run)-1].NextSibling
			for c := first; c != end; {
				next := c.NextSibling
				n.RemoveChild(c)
				c = next
			}
		}
		run = nil
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if analysisBlock(c) && c.DataAtom != atom.Br {
			trim()
		} else {
			run = append(run, c)
		}
	}
	trim()
	for c := n.FirstChild; c != nil; {
		next := c.NextSibling
		f := t.nodes[c].facts
		if c.Type == html.ElementNode && contactShape(f) && earlier {
			n.RemoveChild(c)
		} else {
			t.contacts(c, earlier)
		}
		earlier = earlier || f.words > 0
		c = next
	}
}

func (t *analysisTree) alignedColumn(n *html.Node) ([]OriginalMatch, float64) {
	// Only maximal bounded panels participate: no nested wrapper vote inflation.
	var panels []*html.Node
	var collect func(*html.Node)
	collect = func(c *html.Node) {
		if c.DataAtom == atom.Footer {
			return
		}
		d := t.nodes[c]
		w := max(t.dimension(c, "width"), t.dimension(c, "max-width"))
		if c != n && w >= 320 && w <= 1200 && t.centered(c) && (d.facts.words > 0 || d.facts.images > 0) {
			panels = append(panels, c)
			return
		}
		for ch := c.FirstChild; ch != nil; ch = ch.NextSibling {
			collect(ch)
		}
	}
	// Restrict inference to the common neutral scaffolding, not each ancestor.
	if n.FirstChild == nil || n.Type != html.ElementNode {
		return nil, 0
	}
	meaningful := 0
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if t.nodes[c].facts.words > 0 || t.nodes[c].facts.images > 0 {
			meaningful++
		}
	}
	if meaningful < 2 {
		return nil, 0
	}
	collect(n)
	if len(panels) < 2 {
		return nil, 0
	}
	width := max(t.dimension(panels[0], "width"), t.dimension(panels[0], "max-width"))
	area := 0.0
	actions, graphics := 0, 0
	actionAt := math.Inf(1)
	hierarchy := false
	for _, p := range panels {
		w := max(t.dimension(p, "width"), t.dimension(p, "max-width"))
		if math.Abs(w-width) > 1 {
			return nil, 0
		}
		d := t.nodes[p]
		area += d.facts.area
		actions += d.facts.actions
		if d.facts.actions > 0 {
			actionAt = min(actionAt, d.facts.actionAt)
		}
		graphics += d.facts.graphics
		hierarchy = hierarchy || t.heading(p) || d.facts.modules >= 2 || d.facts.columns
	}
	if area*2 < t.nodes[n].facts.area {
		return nil, 0
	}
	matches := []OriginalMatch{{Rule: "bounded or padded main layout", Points: 1}}
	if actions > 0 && (actionAt-t.nodes[panels[0]].startArea)*2 <= area {
		matches = append(matches, OriginalMatch{Rule: "designed action", Points: 1})
	}
	if hierarchy {
		matches = append(matches, OriginalMatch{Rule: "visual hierarchy or modules", Points: 1})
	}
	if graphics > 0 {
		matches = append(matches, OriginalMatch{Rule: "prominent graphic", Points: 1})
	}
	if len(matches) < 2 {
		return nil, 0
	}
	return matches, area
}
