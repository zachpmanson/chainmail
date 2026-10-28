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
	cells        int
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
			if c.DataAtom == atom.Td || c.DataAtom == atom.Th {
				tree.nodes[n].cells++
			}
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
			if tree.hierarchy(n) {
				matches = append(matches, OriginalMatch{Rule: "visual hierarchy or modules", Points: 1})
			}
			if f.graphics > 0 && tree.primary(n, f.graphicAt) {
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
		if t.nodes[n.Parent].cells > 1 {
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
	area, actionAt, structureAt, graphicAt              float64
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
		if c.graphics > 0 && (f.graphics == 0 || c.graphicAt < f.graphicAt) {
			f.graphicAt = c.graphicAt
		}
		if (c.modules > 0 || c.columns) && (f.modules == 0 && !f.columns || c.structureAt < f.structureAt) {
			f.structureAt = c.structureAt
		}
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
					f.graphicAt = d.startArea
				}
			}
			if n.DataAtom == atom.A {
				href := strings.ToLower(strings.TrimSpace(attr(n, "href")))
				blockLike := s["display"] == "inline-block" || s["display"] == "block" || s["display"] == "inline-flex"
				if d.telephone || strings.HasPrefix(href, "mailto:") {
					f.contacts++
				} else if href != "" && (blockLike && (t.painted(n) || f.painted) || t.inlineControl(n, f)) {
					f.actions++
					f.actionAt = d.startArea
				}
			}
			f.painted = f.painted || t.painted(n) && f.maxFlow <= 12
			horizontal := s["display"] == "flex" && s["flex-direction"] != "column" && s["flex-direction"] != "column-reverse" || s["display"] == "grid" && analysisGridTracks(s["grid-template-columns"]) >= 2
			if horizontal && items >= 2 {
				f.columns = true
				f.structureAt = d.startArea
			}
			parentBG := "white"
			if n.Parent != nil {
				parentBG = t.nodes[n.Parent].background
			}
			panel := n.DataAtom == atom.Div || n.DataAtom == atom.Section || n.DataAtom == atom.Article || n.DataAtom == atom.Table
			if panel && d.padding >= 12 && (bg != parentBG || analysisBorder(s["border"]) || analysisBorder(s["border-top"])) && f.words > 0 {
				if f.modules == 0 {
					if !f.columns {
						f.structureAt = d.startArea
					}
					f.modules = 1
				}
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

// Whitespace inside sizing functions separates arguments, not grid tracks.
func analysisGridTracks(value string) int {
	var count func(string, int) int
	count = func(value string, depth int) int {
		if depth > 8 || len(value) > 4096 {
			return 0
		}
		var tracks []string
		var token strings.Builder
		paren, bracket := 0, 0
		flush := func() {
			if token.Len() > 0 {
				tracks = append(tracks, token.String())
				token.Reset()
			}
		}
		for _, c := range value {
			if c == '[' && paren == 0 {
				bracket++
			}
			if bracket > 0 {
				if c == ']' {
					bracket--
				}
				continue
			}
			if (c == ' ' || c == '\t' || c == '\n') && paren == 0 {
				flush()
				continue
			}
			if c == '(' {
				paren++
			} else if c == ')' {
				paren--
				if paren < 0 {
					return 0
				}
			}
			token.WriteRune(c)
		}
		if paren != 0 || bracket != 0 {
			return 0
		}
		flush()
		total := 0
		for _, track := range tracks {
			if strings.HasPrefix(track, "repeat(") && strings.HasSuffix(track, ")") {
				n, inner, ok := strings.Cut(track[7:len(track)-1], ",")
				times, err := strconv.Atoi(strings.TrimSpace(n))
				if !ok || err != nil || times <= 0 {
					return 0
				}
				total += min(2, times) * count(inner, depth+1)
			} else if track == "none" || track == "subgrid" || strings.HasPrefix(track, "var(") {
				return 0
			} else {
				total++
			}
		}
		return min(2, total)
	}
	return count(value, 0)
}

func analysisBackgroundValue(v string) string {
	v = strings.ReplaceAll(strings.ToLower(v), " ", "")
	if v == "" || v == "none" || v == "transparent" || v == "inherit" || v == "initial" || v == "unset" {
		return ""
	}
	if strings.HasPrefix(v, "#") && (len(v) == 5 && v[4] == '0' || len(v) == 9 && v[7:] == "00") {
		return ""
	}
	if strings.HasPrefix(v, "rgba(") || strings.HasPrefix(v, "hsla(") || strings.Contains(v, "/") {
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
	if v == "" || strings.Contains(v, "none") || strings.Contains(v, "hidden") || strings.Contains(v, "transparent") {
		return false
	}
	for _, part := range strings.Fields(v) {
		if analysisZero(part) || strings.HasPrefix(part, "#") && analysisBackgroundValue(part) == "" {
			return false
		}
	}
	compact := strings.ReplaceAll(v, " ", "")
	for _, function := range []string{"rgba(", "hsla(", "rgb(", "hsl("} {
		if start := strings.Index(compact, function); start >= 0 {
			if end := strings.IndexByte(compact[start:], ')'); end >= 0 && analysisBackgroundValue(compact[start:start+end+1]) == "" {
				return false
			}
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

// Only simple colors supply new inline-control paint. Background shorthands
// with other components or mixed longhands need a cascade we do not model.
func analysisControlBackground(s map[string]string) bool {
	bg := s["background-color"]
	if shorthand, ok := s["background"]; ok {
		for key := range s {
			if strings.HasPrefix(key, "background-") {
				return false
			}
		}
		bg = shorthand
	}
	if strings.HasPrefix(bg, "#") {
		if len(bg) != 4 && len(bg) != 5 && len(bg) != 7 && len(bg) != 9 {
			return false
		}
		for _, c := range bg[1:] {
			if !strings.ContainsRune("0123456789abcdef", c) {
				return false
			}
		}
		return analysisBackgroundValue(bg) != ""
	}
	switch bg {
	case "black", "silver", "gray", "white", "maroon", "red", "purple", "fuchsia", "green", "lime", "olive", "yellow", "navy", "blue", "teal", "aqua", "orange", "rebeccapurple":
		return true
	}
	return false
}

// CSS lengths, unlike HTML dimensions, cannot be positive unitless numbers.
// em uses the resolved element font; root-relative and other units are declined.
func analysisCSSLength(value string, font float64) (float64, bool) {
	if strings.ContainsAny(value, " \t\n\r\f") {
		return 0, false
	}
	n, unit, ok := analysisNumeric(value)
	if !ok || n < 0 || strings.HasSuffix(strings.TrimSuffix(value, unit), ".") {
		return 0, false
	}
	switch unit {
	case "":
		return 0, n == 0
	case "px":
		return n, true
	case "pt":
		return n * 4 / 3, true
	case "em":
		return n * font, true
	}
	return 0, false
}

func analysisPadding(s map[string]string, font float64) ([4]float64, bool) {
	var padding [4]float64
	value, shorthand := s["padding"]
	values := strings.Fields(value)
	if shorthand {
		if len(values) < 1 || len(values) > 4 {
			return padding, false
		}
		for i := range padding {
			index := i
			if len(values) == 1 {
				index = 0
			} else if len(values) == 2 {
				index %= 2
			} else if len(values) == 3 && i == 3 {
				index = 1
			}
			var ok bool
			padding[i], ok = analysisCSSLength(values[index], font)
			if !ok {
				return padding, false
			}
		}
	}
	for i, key := range []string{"padding-top", "padding-right", "padding-bottom", "padding-left"} {
		if value, ok := s[key]; ok {
			// analysisStyle does not resolve cross-property cascade order.
			if shorthand {
				return padding, false
			}
			var valid bool
			padding[i], valid = analysisCSSLength(value, font)
			if !valid {
				return padding, false
			}
		}
	}
	for key := range s {
		if strings.HasPrefix(key, "padding-") && key != "padding-top" && key != "padding-right" && key != "padding-bottom" && key != "padding-left" {
			return padding, false
		}
	}
	return padding, true
}

// Inline paint alone can be highlighted prose. Require a compact label and
// generous padding on both axes, owned by the anchor rather than its cell.
func (t *analysisTree) inlineControl(n *html.Node, f originalFacts) bool {
	s := t.nodes[n].style
	if s["display"] != "" && s["display"] != "inline" || f.words == 0 || f.words > 12 || f.images > 0 {
		return false
	}
	paint := analysisControlBackground(s)
	// A border needs its own visible line style; inherited paint and unresolved
	// shorthand/longhand overrides cannot supply it.
	border := analysisBorder(s["border"])
	for key := range s {
		if strings.HasPrefix(key, "border-") && !strings.HasSuffix(key, "radius") {
			border = false
		}
	}
	if border {
		for _, value := range strings.Fields(s["border"]) {
			switch value {
			case "solid", "dashed", "dotted", "double", "groove", "ridge", "inset", "outset":
				paint = true
			}
		}
	}
	if !paint {
		return false
	}
	padding, ok := analysisPadding(s, t.nodes[n].font)
	return ok && padding[0] >= 8 && padding[2] >= 8 && padding[1] >= 16 && padding[3] >= 16
}

// A tiny late control in a bare column is not evidence about the main prose.
// A genuinely painted/padded enclosing card may put its primary action last.
func (t *analysisTree) primary(n *html.Node, at float64) bool {
	d := t.nodes[n]
	return (at-d.startArea)*2 <= d.facts.area || d.padding >= 16 && t.surface(n)
}

func (t *analysisTree) action(n *html.Node) bool {
	return t.nodes[n].facts.actions > 0 && t.primary(n, t.nodes[n].facts.actionAt)
}

func (t *analysisTree) hierarchy(n *html.Node) bool {
	d := t.nodes[n]
	return t.heading(n) || (d.facts.modules >= 2 || d.facts.columns) && (d.facts.structureAt-d.startArea)*2 <= d.facts.area
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
	return bounded && (d.padding >= 12 || t.centered(n) || t.heading(n) || d.facts.columns || d.facts.modules >= 2) || d.padding >= 16 && surface || n.DataAtom == atom.Table && t.centered(n) && surface && d.facts.bound || t.fluidPage(n)
}

// Blank display:block cells may sit inside anonymous table cells. Their own
// width is usable only without extra horizontal sizing we cannot reconcile.
func (t *analysisTree) fluidGutterWidth(n *html.Node) float64 {
	d := t.nodes[n]
	s := d.style
	if display := s["display"]; display != "" && display != "table-cell" && display != "block" {
		return 0
	}
	var w float64
	if value, ok := s["width"]; ok {
		var valid bool
		w, valid = analysisCSSLength(value, d.font)
		if !valid {
			return 0
		}
	} else {
		w = analysisNumber(attr(n, "width"))
	}
	if w < 12 || w > 48 {
		return 0
	}
	for _, key := range []string{"min-width", "max-width"} {
		if value, ok := s[key]; ok {
			limit, valid := analysisCSSLength(value, d.font)
			if !valid || key == "min-width" && limit > w || key == "max-width" && limit < w {
				return 0
			}
		}
	}
	padding, ok := analysisPadding(s, d.font)
	if !ok || padding[1] != 0 || padding[3] != 0 {
		return 0
	}
	for key, value := range s {
		if strings.Contains(key, "inline-size") || strings.HasPrefix(key, "border") && value != "none" && !analysisZero(value) {
			return 0
		}
		if strings.HasPrefix(key, "margin") && !analysisZero(value) {
			return 0
		}
	}
	return w
}

// Repeated blank side cells can define a fluid page gutter without defining
// any pixel width for its content or images. Only this table's own rows count.
func (t *analysisTree) fluidPage(n *html.Node) bool {
	if n.DataAtom != atom.Table {
		return false
	}
	s := t.nodes[n].style
	if display := s["display"]; display != "" && display != "table" && display != "inline-table" {
		return false
	}
	if value := attr(n, "cellpadding"); value != "" && !analysisZero(value) {
		return false
	}
	width := s["width"]
	if width == "" {
		width = attr(n, "width")
	}
	if size, unit, ok := analysisNumeric(width); !ok || unit != "%" || size != 100 {
		return false
	}
	// Centering may belong to a single-content outer scaffold, never to an
	// ancestor that also contains disconnected content beside this region.
	centered := false
	for p := n; p != nil; p = p.Parent {
		if t.nodes[p].facts.area != t.nodes[n].facts.area || t.nodes[p].facts.images != t.nodes[n].facts.images {
			break
		}
		if t.centered(p) {
			centered = true
			break
		}
	}
	if !centered {
		return false
	}
	rows, substantive := 0, false
	gutter := 0.0
	valid := true
	checkRow := func(row *html.Node) {
		if row.DataAtom != atom.Tr || !valid {
			return
		}
		if display := t.nodes[row].style["display"]; display != "" && display != "table-row" {
			valid = false
			return
		}
		f := t.nodes[row].facts
		empty := f.words == 0 && f.images == 0
		var cells []*html.Node
		columns := 0
		for c := row.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.ElementNode {
				span := 1
				if value := attr(c, "colspan"); value != "" {
					var err error
					span, err = strconv.Atoi(value)
					if err != nil || span < 1 || span > 3 || !empty && span != 1 {
						valid = false
						return
					}
				}
				// Empty cells still occupy physical columns in following rows.
				if c.DataAtom != atom.Td || attr(c, "rowspan") != "" && attr(c, "rowspan") != "1" {
					valid = false
					return
				}
				columns += span
				cells = append(cells, c)
			}
		}
		if empty && len(cells) == 0 {
			return
		}
		if empty && len(cells) == 1 && columns == 3 {
			display := t.nodes[cells[0]].style["display"]
			valid = display == "" || display == "table-cell"
			return // A full-width table-cell spacer preserves the grid.
		}
		if len(cells) != 3 || columns != 3 {
			valid = false
			return
		}
		if display := t.nodes[cells[1]].style["display"]; display != "" && display != "table-cell" {
			valid = false
			return
		}
		left, right := t.nodes[cells[0]].facts, t.nodes[cells[2]].facts
		w := t.fluidGutterWidth(cells[0])
		if left.words != 0 || right.words != 0 || left.images != 0 || right.images != 0 || w == 0 || w != t.fluidGutterWidth(cells[2]) || gutter != 0 && w != gutter {
			valid = false
			return
		}
		gutter = w
		if !empty {
			rows++
			substantive = substantive || t.nodes[cells[1]].facts.words > 20
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		switch c.DataAtom {
		case atom.Tr:
			checkRow(c)
		case atom.Tbody, atom.Thead, atom.Tfoot:
			expected := map[atom.Atom]string{atom.Tbody: "table-row-group", atom.Thead: "table-header-group", atom.Tfoot: "table-footer-group"}
			if display := t.nodes[c].style["display"]; display != "" && display != expected[c.DataAtom] {
				return false
			}
			for row := c.FirstChild; row != nil; row = row.NextSibling {
				checkRow(row)
			}
		case atom.Caption:
			if t.nodes[c].facts.words > 0 || t.nodes[c].facts.images > 0 {
				return false
			}
		}
	}
	return valid && rows >= 2 && substantive
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

// Legacy table alignment can belong to the immediate cell instead of each
// stacked table. Decline horizontal cells and overrides rather than inheriting
// arbitrary ancestor text alignment as block geometry.
func (t *analysisTree) panelCentered(n *html.Node) bool {
	if t.centered(n) {
		return true
	}
	if n.DataAtom != atom.Table || attr(n, "align") != "" || n.Parent == nil || n.Parent.DataAtom != atom.Td {
		return false
	}
	s := t.nodes[n].style
	if s["display"] != "" && s["display"] != "table" || s["float"] != "" && s["float"] != "none" || s["position"] != "" && s["position"] != "static" {
		return false
	}
	for key := range s {
		if strings.HasPrefix(key, "margin") {
			return false
		}
	}
	cell := n.Parent
	cs := t.nodes[cell].style
	if !strings.EqualFold(attr(cell, "align"), "center") || cs["text-align"] != "" || cs["display"] != "" && cs["display"] != "table-cell" {
		return false
	}
	for _, key := range []string{"rowspan", "colspan"} {
		if v := attr(cell, key); v != "" && v != "1" {
			return false
		}
	}
	return cell.Parent != nil && cell.Parent.DataAtom == atom.Tr && t.nodes[cell.Parent].cells == 1
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
		if c != n && w >= 320 && w <= 1200 && t.panelCentered(c) && (d.facts.words > 0 || d.facts.images > 0) {
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
	actionAt, hierarchyAt, graphicAt := math.Inf(1), math.Inf(1), math.Inf(1)
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
		if d.facts.graphics > 0 {
			graphicAt = min(graphicAt, d.facts.graphicAt)
		}
		if t.heading(p) {
			hierarchyAt = min(hierarchyAt, t.flowArea[t.nextHeading[d.start]])
		}
		if d.facts.modules >= 2 || d.facts.columns {
			hierarchyAt = min(hierarchyAt, d.facts.structureAt)
		}
	}
	if area*2 < t.nodes[n].facts.area {
		return nil, 0
	}
	matches := []OriginalMatch{{Rule: "bounded or padded main layout", Points: 1}}
	if actions > 0 && (actionAt-t.nodes[panels[0]].startArea)*2 <= area {
		matches = append(matches, OriginalMatch{Rule: "designed action", Points: 1})
	}
	if (hierarchyAt-t.nodes[panels[0]].startArea)*2 <= area {
		matches = append(matches, OriginalMatch{Rule: "visual hierarchy or modules", Points: 1})
	}
	if graphics > 0 && (graphicAt-t.nodes[panels[0]].startArea)*2 <= area {
		matches = append(matches, OriginalMatch{Rule: "prominent graphic", Points: 1})
	}
	if len(matches) < 2 {
		return nil, 0
	}
	return matches, area
}
