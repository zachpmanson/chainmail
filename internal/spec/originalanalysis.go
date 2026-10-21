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

// AnalyseOriginal uses a separate tree, never the displayed OriginalBody output
// (which includes app CSS). No corpus, sender, URL identity or label is consulted.
func AnalyseOriginal(raw string) OriginalAnalysis {
	result := OriginalAnalysis{Matches: []OriginalMatch{}}
	doc, err := html.Parse(strings.NewReader(raw))
	if err != nil {
		result.Uncertain = true
		return result
	}
	_, body := documentParts(doc)
	if body == nil {
		return result
	}
	pruneOriginal(body)
	analysisVisible(body, 16)
	result.Uncertain = analysisBoundaries(body)
	analysisContacts(body)
	total := analysisFacts(body)
	var best, weak []OriginalMatch
	var visit func(*html.Node) int
	visit = func(n *html.Node) int {
		if n.Type == html.ElementNode && analysisContainer(n) {
			facts := analysisFacts(n)
			// A design around a minor footer must not choose the reading mode
			// for the authored message. Neutral wrappers have no evidence.
			if facts.words > 0 && analysisPage(n) {
				matches := []OriginalMatch{{Rule: "bounded or padded main layout", Points: 1}}
				if facts.actions > 0 {
					matches = append(matches, OriginalMatch{Rule: "designed action", Points: 1})
				}
				if facts.headings > 0 || facts.modules >= 2 || facts.columns {
					matches = append(matches, OriginalMatch{Rule: "visual hierarchy or modules", Points: 1})
				}
				if facts.graphics > 0 {
					matches = append(matches, OriginalMatch{Rule: "prominent graphic", Points: 1})
				}
				if len(matches) >= 2 {
					if len(matches) > len(best) {
						best = matches
					}
					// Count a fully evidenced region once, never pool partial
					// signals across unrelated sections or nested wrappers.
					return facts.words
				}
				if facts.words*2 >= total.words {
					weak = matches
				}
			}
		}
		covered := 0
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			covered += visit(c)
		}
		return covered
	}
	covered := visit(body)
	if covered*2 < total.words {
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
	uncertain := false
	for _, sig := range markedSignatures(n) {
		sig.Parent.RemoveChild(sig)
	}
	for c := n.FirstChild; c != nil; {
		next := c.NextSibling
		if c.Type == html.ElementNode && c.DataAtom == atom.Details && hasClass(c, "sig") {
			n.RemoveChild(c)
		} else if analysisQuote(c) {
			n.RemoveChild(c)
		} else {
			if analysisBoundaries(c) {
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

// Inline geometry only. Stylesheets/external CSS are deliberately not guessed.
func analysisStyle(n *html.Node) map[string]string {
	out := map[string]string{}
	for _, declaration := range strings.Split(attr(n, "style"), ";") {
		key, value, ok := strings.Cut(declaration, ":")
		if ok {
			out[strings.ToLower(strings.TrimSpace(key))] = strings.TrimSpace(strings.TrimSuffix(strings.ToLower(strings.TrimSpace(value)), "!important"))
		}
	}
	return out
}

func analysisNumber(value string) float64 {
	value = strings.TrimSpace(value)
	end := 0
	for end < len(value) && (value[end] >= '0' && value[end] <= '9' || value[end] == '.') {
		end++
	}
	n, _ := strconv.ParseFloat(value[:end], 64)
	if strings.HasSuffix(value, "pt") {
		n *= 4.0 / 3
	}
	if strings.HasSuffix(value, "em") {
		n *= 16
	}
	return n
}

func analysisDimension(n *html.Node, key string) float64 {
	v := analysisStyle(n)[key]
	if v == "" {
		v = attr(n, key)
	}
	if strings.Contains(v, "%") {
		return 0
	}
	return analysisNumber(v)
}

func analysisVisible(n *html.Node, font float64) {
	s := analysisStyle(n)
	if v, ok := s["font-size"]; ok {
		font = analysisFontSize(v, font)
	}
	for c := n.FirstChild; c != nil; {
		next := c.NextSibling
		cs := analysisStyle(c)
		_, hidden := cAttr(c, "hidden")
		drop := hidden || cs["display"] == "none" || cs["visibility"] == "hidden" || cs["visibility"] == "collapse" || cs["opacity"] == "0"
		drop = drop || c.DataAtom == atom.Style || c.DataAtom == atom.Link
		if c.Type == html.TextNode && font == 0 {
			drop = true
		}
		if c.DataAtom == atom.Img {
			for _, key := range []string{"width", "height"} {
				if v := analysisDimension(c, key); v > 0 && v <= 2 {
					drop = true
				}
				v := cs[key]
				if v == "" {
					v = attr(c, key)
				}
				if v == "0" || v == "0px" || v == "0pt" || v == "0%" {
					drop = true
				}
			}
		}
		if drop {
			n.RemoveChild(c)
		} else {
			analysisVisible(c, font)
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
	words, links, contacts, images, graphics, actions, headings, modules int
	columns                                                              bool
}

func analysisFacts(n *html.Node) originalFacts {
	var f originalFacts
	sizedHeading := false
	var walk func(*html.Node)
	walk = func(c *html.Node) {
		if c.Type == html.TextNode {
			f.words += len(strings.Fields(c.Data))
		}
		if c.Type == html.ElementNode {
			s := analysisStyle(c)
			if c.DataAtom == atom.A {
				f.links++
				href := strings.ToLower(strings.TrimSpace(attr(c, "href")))
				if strings.HasPrefix(href, "mailto:") || strings.HasPrefix(href, "tel:") {
					f.contacts++
				} else if analysisAction(c) {
					f.actions++
				}
			}
			if c.DataAtom == atom.Img {
				f.images++
				w, h := analysisDimension(c, "width"), analysisDimension(c, "height")
				if w >= 320 && h >= 60 || w >= 250 && (h == 0 || h >= 100) || w >= 180 && h >= 150 {
					f.graphics++
				}
			}
			if c.DataAtom == atom.H1 || c.DataAtom == atom.H2 || c.DataAtom == atom.H3 {
				if strings.TrimSpace(textHead(c, 200)) != "" {
					f.headings++
				}
			}
			if (c.DataAtom == atom.Div || c.DataAtom == atom.P) && analysisNumber(s["font-size"]) >= 22 && analysisMaxFlow(c) <= 20 {
				sizedHeading = true
			}

			if s["display"] == "grid" && strings.Contains(s["grid-template-columns"], " ") || s["display"] == "flex" && s["flex-direction"] != "column" {
				f.columns = true
			}
		}
		for ch := c.FirstChild; ch != nil; ch = ch.NextSibling {
			walk(ch)
		}
	}
	walk(n)
	f.modules = analysisModules(n)
	if sizedHeading {
		low, high := analysisFonts(n, analysisInheritedFont(n.Parent))
		if low > 0 && high >= low*1.25 {
			f.headings++
		}
	}
	return f
}

func analysisContainer(n *html.Node) bool {
	switch n.DataAtom {
	case atom.Div, atom.Table, atom.Td, atom.Section, atom.Article, atom.Main, atom.Body:
		return true
	}
	return false
}

func analysisPadding(n *html.Node) float64 {
	s := analysisStyle(n)
	maxPad := analysisNumber(attr(n, "cellpadding"))
	for _, key := range []string{"padding", "padding-left", "padding-right", "padding-top", "padding-bottom"} {
		for _, v := range strings.Fields(s[key]) {
			maxPad = max(maxPad, analysisNumber(v))
		}
	}
	return maxPad
}

func analysisPage(n *html.Node) bool {
	s := analysisStyle(n)
	bounded := false
	for _, key := range []string{"width", "max-width"} {
		w := analysisDimension(n, key)
		bounded = bounded || w >= 320 && w <= 1200
	}
	// A page can be fluid, but ordinary paragraph indents are not a canvas.
	surface := s["background"] != "" || s["background-color"] != "" || attr(n, "bgcolor") != "" || s["border"] != ""
	return bounded || analysisPadding(n) >= 16 && surface || n.DataAtom == atom.Table && strings.EqualFold(attr(n, "align"), "center") && surface && analysisContainsBound(n)
}

func analysisAction(n *html.Node) bool {
	if strings.TrimSpace(attr(n, "href")) == "" {
		return false
	}
	s := analysisStyle(n)
	if s["display"] != "inline-block" && s["display"] != "block" && s["display"] != "inline-flex" {
		return false
	}
	painted := func(c *html.Node) bool {
		cs := analysisStyle(c)
		return (cs["background"] != "" || cs["background-color"] != "" || cs["border"] != "" || attr(c, "bgcolor") != "") && (analysisPadding(c) >= 6 || cs["border-radius"] != "")
	}
	if painted(n) {
		return true
	}
	// Some clients put the button's paint on a cell INSIDE the link. Never
	// inherit a page cell's background/padding for an ordinary inline reference.
	var inside func(*html.Node) bool
	inside = func(c *html.Node) bool {
		if c != n && painted(c) && analysisMaxFlow(c) <= 12 {
			return true
		}
		for ch := c.FirstChild; ch != nil; ch = ch.NextSibling {
			if inside(ch) {
				return true
			}
		}
		return false
	}
	return inside(n)
}

func analysisBackground(n *html.Node) string {
	for c := n; c != nil; c = c.Parent {
		s := analysisStyle(c)
		v := s["background-color"]
		if v == "" {
			v = s["background"]
		}
		if v == "" {
			v = attr(c, "bgcolor")
		}
		v = strings.ReplaceAll(strings.ToLower(v), " ", "")
		if v == "#fff" || v == "#ffffff" || v == "rgb(255,255,255)" {
			v = "white"
		}
		if v != "" {
			return v
		}
	}
	return "white"
}

func analysisContainsBound(n *html.Node) bool {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		for _, key := range []string{"width", "max-width"} {
			w := analysisDimension(c, key)
			if w >= 320 && w <= 1200 {
				return true
			}
		}
		if analysisContainsBound(c) {
			return true
		}
	}
	return false
}

// Nested wrappers are one module, not independent votes. Modules are painted,
// padded sibling regions; arbitrary repetition of a wrapper cannot add votes.
func analysisModules(n *html.Node) int {
	children := 0
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		children += analysisModules(c)
	}
	s := analysisStyle(n)
	panel := n.DataAtom == atom.Div || n.DataAtom == atom.Section || n.DataAtom == atom.Article || n.DataAtom == atom.Table
	if panel && analysisPadding(n) >= 12 && (analysisBackground(n) != analysisBackground(n.Parent) || s["border"] != "" && s["border"] != "none" || s["border-top"] != "") && strings.TrimSpace(textHead(n, 200)) != "" {
		return max(1, children)
	}
	return children
}

// Compact contact/logo suffixes are not main design. Shape and link schemes,
// never names, sign-off prose, generic footer IDs or destination identities.
func analysisContacts(n *html.Node) {
	for c := n.FirstChild; c != nil; {
		next := c.NextSibling
		f := analysisFacts(c)
		if analysisContainer(c) && f.contacts > 0 && f.images > 0 && f.graphics == 0 && f.actions == 0 && f.words <= 120 && analysisMaxFlow(c) <= 12 && analysisHasEarlierContent(c) {
			n.RemoveChild(c)
		} else {
			analysisContacts(c)
		}
		c = next
	}
}

func analysisHasEarlierContent(n *html.Node) bool {
	for c := n; c != nil && c.DataAtom != atom.Body; c = c.Parent {
		for p := c.PrevSibling; p != nil; p = p.PrevSibling {
			if analysisFacts(p).words > 0 {
				return true
			}
		}
	}
	return false
}

func analysisInheritedFont(n *html.Node) float64 {
	if n == nil {
		return 16
	}
	inherited := analysisInheritedFont(n.Parent)
	if v, ok := analysisStyle(n)["font-size"]; ok {
		return analysisFontSize(v, inherited)
	}
	return inherited
}

func analysisFontSize(value string, inherited float64) float64 {
	if value == "0" || value == "0px" || value == "0pt" || value == "0em" || value == "0%" {
		return 0
	}
	if strings.HasSuffix(value, "%") {
		return analysisNumber(value) / 100 * inherited
	}
	if strings.HasSuffix(value, "em") && !strings.HasSuffix(value, "rem") {
		return analysisNumber(value) / 16 * inherited
	}
	if n := analysisNumber(value); n > 0 {
		return n
	}
	// Unknown/keyword sizes are not hidden text and supply no size evidence.
	return inherited
}

// Typography needs contrast within a region, not a count of large-font nodes.
func analysisFonts(n *html.Node, inherited float64) (float64, float64) {
	if v, ok := analysisStyle(n)["font-size"]; ok {
		inherited = analysisFontSize(v, inherited)
	}
	low, high := math.Inf(1), 0.0
	if n.Type == html.TextNode && strings.TrimSpace(n.Data) != "" && inherited > 0 {
		return inherited, inherited
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		l, h := analysisFonts(c, inherited)
		low, high = min(low, l), max(high, h)
	}
	return low, high
}

// Equivalent p/div/br flows produce the same contact-region shape. Inline
// colours, neutral wrappers and table depth do not create design evidence.
func analysisMaxFlow(n *html.Node) int {
	longest, words := 0, 0
	flush := func() { longest = max(longest, words); words = 0 }
	var walk func(*html.Node)
	walk = func(c *html.Node) {
		block := false
		switch c.DataAtom {
		case atom.P, atom.Div, atom.Br, atom.Td, atom.Th, atom.Tr, atom.Li, atom.H1, atom.H2, atom.H3, atom.Section, atom.Article, atom.Blockquote:
			block = true
		}
		if block {
			flush()
		}
		if c.Type == html.TextNode {
			words += len(strings.Fields(c.Data))
		}
		for ch := c.FirstChild; ch != nil; ch = ch.NextSibling {
			walk(ch)
		}
		if block {
			flush()
		}
	}
	walk(n)
	flush()
	return longest
}
