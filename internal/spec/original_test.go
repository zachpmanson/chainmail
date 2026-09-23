package spec

import (
	"regexp"
	"strings"
	"testing"
)

// Every part in this file is invented. The shapes are the ones real senders
// emit — a stylesheet in the head, classes on the elements below it, a fixed
// width, a coloured band with white text on it — but no line of real
// correspondence is committed.
//
// What is being held here is the difference between this policy and the other
// one: sanitise.go's tests assert that the sender's styling is *gone*, and these
// assert that it is *kept*, while both assert that the executable surface is
// closed. The two files are meant to be read as a pair.

const inviteDocument = `<!doctype html><html><head><title>invite</title>
<style>
  body, html { font-family: Roboto, sans-serif; background-color: #f6f8fc; }
  .band { background-color: #1a73e8; width: 600px; }
  .band > span { color: #fff; font-weight: 700; }
  @media (max-width: 600px) { .band { width: 100%; } }
  a { text-decoration: none; }
  #button { padding: 8px; }
</style>
</head><body>
<div class="band" id="button"><span>Join with Google Meet</span></div>
<a href="https://meet.example/kds-rxjy-quk">meet.example/kds-rxjy-quk</a>
</body></html>`

func originalBody(t *testing.T, raw string) string {
	t.Helper()
	got, ok := OriginalBody(raw)
	if !ok {
		t.Fatalf("OriginalBody(%q) declined, want a body", raw)
	}
	return got
}

func TestOriginalKeepsTheStylesheetTheTranscriptDrops(t *testing.T) {
	got := originalBody(t, inviteDocument)
	for _, want := range []string{
		"<style>",
		"background-color: #1a73e8",
		"width: 600px",
		"font-family: Roboto, sans-serif",
		`class="band"`,
		`id="button"`,
		"font-weight: 700",
		"text-decoration: none",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("original body is missing %q\n  in %q", want, got)
		}
	}
}

func TestOriginalKeepsEveryDeclaration(t *testing.T) {
	// The transcript keeps five properties (see keptStyleProps). This policy
	// keeps the declaration as written, because the shadow root is what makes it
	// safe to: nothing outside the tree can be restyled by it.
	raw := `<div style="background-color:#fff; width:600px; font-size:13px; padding:8px !important; margin:0 auto">x</div>`
	got := originalBody(t, raw)
	for _, want := range []string{"background-color:#fff", "width:600px", "font-size:13px", "padding:8px !important", "margin:0 auto"} {
		if !strings.Contains(got, want) {
			t.Errorf("original body is missing %q\n  in %q", want, got)
		}
	}
}

func TestOriginalRewritesPageLevelSelectorsOnly(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"body", "body { color: red }", ":host { color: red }"},
		{"html", "html { color: red }", ":host { color: red }"},
		{"root", ":root { --x: 1 }", ":host { --x: 1 }"},
		{"list", "html, body { color: red }", ":host { color: red }"},
		{"descendant chain", "html body { color: red }", ":host { color: red }"},
		{"child chain", "html > body { color: red }", ":host { color: red }"},
		{"descendant of body", "body .band { color: red }", ":host .band { color: red }"},
		{"inside media", "@media (max-width: 600px) { body { width: 100% } }", "@media (max-width: 600px) { :host { width: 100% } }"},
		{"inside functional pseudo", ":is(html, body) { color: red }", ":is(:host) { color: red }"},
		{"a class named body is untouched", ".body { color: red }", ".body { color: red }"},
		{"an id named body is untouched", "#body { color: red }", "#body { color: red }"},
		{"an attribute named body is untouched", "[data-body] { color: red }", "[data-body] { color: red }"},
		{"a type named bodyguard is untouched", "bodyguard { color: red }", "bodyguard { color: red }"},
		{"a selector mentioning html in a value is untouched", ".x::before { content: 'html' }", ".x::before { content: 'html' }"},
		{"keyframes are untouched", "@keyframes f { 0% { opacity: 0 } 100% { opacity: 1 } }", "@keyframes f { 0% { opacity: 0 } 100% { opacity: 1 } }"},
		{"font-face is untouched", "@font-face { font-family: X; src: url(https://x.example/f.woff2) }", "@font-face { font-family: X; src: url(https://x.example/f.woff2) }"},
		{"declarations are untouched", ":host { content: 'body { }' }", ":host { content: 'body { }' }"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := rewriteCSS(c.in); got != c.want {
				t.Errorf("rewriteCSS(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestOriginalDoesNotEscapeStylesheetText(t *testing.T) {
	// html.Render escapes every text node, which would turn a child combinator
	// into "&gt;" and break the stylesheet the mode exists to preserve. The
	// serialiser writes a <style> element's text raw for that reason.
	got := originalBody(t, `<html><head><style>.a > .b, .c + .d { color: red }</style></head><body><p>x</p></body></html>`)
	if !strings.Contains(got, ".a > .b, .c + .d { color: red }") {
		t.Errorf("stylesheet text was escaped\n  got %q", got)
	}
	if strings.Contains(got, "&gt;") {
		t.Errorf("stylesheet contains an escaped combinator\n  got %q", got)
	}
}

func TestOriginalHoistsHeadStylesheetsBeforeTheBody(t *testing.T) {
	got := originalBody(t, `<html><head><style>.a{color:red}</style></head><body><p>words</p></body></html>`)
	if strings.Index(got, "<style>") > strings.Index(got, "words") {
		t.Errorf("stylesheet came after the body: %q", got)
	}
	if strings.Contains(got, "<head") || strings.Contains(got, "<body") || strings.Contains(got, "<html") {
		t.Errorf("a document element survived into the fragment: %q", got)
	}
}

func TestOriginalForcesLinksIntoANewTab(t *testing.T) {
	got := originalBody(t, `<p><a href="https://x.example/">x</a></p>`)
	if !strings.Contains(got, `target="_blank"`) || !strings.Contains(got, `rel="noopener noreferrer"`) {
		t.Errorf("link is not forced into a new tab: %q", got)
	}
	// A sender's own target is replaced rather than respected: they wrote it for
	// their document, where navigating away was not navigating away from ours.
	got = originalBody(t, `<p><a href="https://x.example/" target="_self" rel="">x</a></p>`)
	if strings.Contains(got, `_self"`) {
		t.Errorf("sender target survived: %q", got)
	}
}

func TestOriginalDropsTheExecutableSurface(t *testing.T) {
	cases := []struct {
		name string
		in   string
		gone []string
	}{
		{"script", `<p>a</p><script>alert(1)</script>`, []string{"<script", "alert(1)"}},
		{"style-less onclick", `<p onclick="alert(1)">a</p>`, []string{"onclick", "alert(1)"}},
		{"img onerror", `<img src="https://x.example/i.png" onerror="alert(1)">`, []string{"onerror"}},
		{"svg onload", `<svg onload="alert(1)"><circle/></svg>`, []string{"<svg", "onload"}},
		{"javascript href", `<a href="javascript:alert(1)">a</a>`, []string{"javascript:"}},
		{"javascript href with a tab", "<a href=\"java\tscript:alert(1)\">a</a>", []string{"script:alert"}},
		{"relative href", `<a href="/mail/1">a</a>`, []string{`href="/mail/1"`}},
		{"iframe", `<iframe src="https://evil.example/"></iframe>`, []string{"<iframe"}},
		{"object", `<object data="https://evil.example/"></object>`, []string{"<object"}},
		{"embed", `<embed src="https://evil.example/">`, []string{"<embed"}},
		{"form", `<form action="https://evil.example/"><input name="p"></form>`, []string{"<form", "<input"}},
		{"base", `<base href="https://evil.example/">`, []string{"<base"}},
		{"meta refresh", `<meta http-equiv="refresh" content="0;url=https://evil.example/">`, []string{"<meta"}},
		{"import", `<style>@import url(https://evil.example/x.css); .a{color:red}</style>`, []string{"@import", "evil.example"}},
		{"script url in a style attribute", `<p style="background:url(javascript:alert(1))">a</p>`, []string{"javascript:"}},
		{"script url in a stylesheet", `<style>.a{background:url('javascript:alert(1)')}</style>`, []string{"javascript:"}},
		{"srcdoc", `<div srcdoc="<script>alert(1)</script>">a</div>`, []string{"srcdoc", "<script"}},
		{"ping", `<a href="https://x.example/" ping="https://track.example/">a</a>`, []string{"ping="}},
		{"cid image", `<img src="cid:ii_abc123">`, []string{"<img"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, _ := OriginalBody(c.in)
			for _, want := range c.gone {
				if strings.Contains(got, want) {
					t.Errorf("input %q left %q\n  in %q", c.in, want, got)
				}
			}
		})
	}
}

func TestOriginalKeepsWhatTheTranscriptStrips(t *testing.T) {
	// The pair of the test above: everything that exists for presentation is
	// kept, because the shadow root is what contains it.
	raw := `<html><head><link rel="stylesheet" href="https://x.example/mail.css">` +
		`<style>body{background-color:#eee}</style></head>` +
		`<body bgcolor="#eeeeee" style="margin:0"><table width="600" cellpadding="0" border="0">` +
		`<tr><td align="center"><img src="https://x.example/logo.png" width="120" height="40"></td></tr>` +
		`</table></body></html>`
	got := originalBody(t, raw)
	for _, want := range []string{
		`<link rel="stylesheet" href="https://x.example/mail.css">`,
		`:host{margin:0; background-color:#eeeeee}`,
		`width="600"`,
		`cellpadding="0"`,
		`border="0"`,
		`align="center"`,
		`width="120"`,
		`<img src="https://x.example/logo.png"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("original body is missing %q\n  in %q", want, got)
		}
	}
}

func TestOriginalWritesTheAppsCanvasFirstSoTheMailCanOverrideIt(t *testing.T) {
	// A mail designed for paper, drawn inside a dark app, is dark-on-dark unless
	// something says otherwise — and the something cannot be the app's stylesheet,
	// because a rule in the outer document beats a :host rule written from inside.
	// So the app writes a canvas into the fragment, first, at the same specificity
	// as everything the sender states: whatever they say about their own canvas
	// comes later and wins.
	got := originalBody(t, `<html><head><style>body{background:#eef}</style></head><body><p>x</p></body></html>`)
	canvas := strings.Index(got, "background:#fff")
	sender := strings.Index(got, "background:#eef")
	if canvas < 0 {
		t.Fatalf("the app's canvas is not in the fragment: %q", got)
	}
	if sender < 0 {
		t.Fatalf("the sender's own canvas is missing: %q", got)
	}
	if canvas > sender {
		t.Errorf("the app's canvas must come first, so the sender's overrides it: %q", got)
	}
	// And a mail that says nothing about its canvas still gets one: the fragment is
	// mounted into the app's page, not into a document of its own.
	if bare := originalBody(t, `<body><p>nothing stated</p></body>`); !strings.Contains(bare, "color-scheme:light") {
		t.Errorf("a fragment with no canvas of its own has no canvas at all: %q", bare)
	}
}

func TestOriginalCarriesTheBodyCanvasAsAHostRule(t *testing.T) {
	// The body element does not survive into a shadow tree, so the canvas written
	// on it has to become a rule about the element the caller mounts this into.
	// It is emitted first, so a stylesheet rule of equal specificity overrides it
	// — the same order a browser would have applied them in.
	got := originalBody(t, `<html><head><style>body{background-color:#fff}</style></head><body bgcolor="#eeeeee"><p>x</p></body></html>`)
	canvas := strings.Index(got, ":host{background-color:#eeeeee}")
	rule := strings.Index(got, ":host{background-color:#fff}")
	if canvas < 0 || rule < 0 {
		t.Fatalf("want both the canvas and the stylesheet rule, got %q", got)
	}
	if canvas > rule {
		t.Errorf("the canvas rule must come before the stylesheet's: %q", got)
	}
}

func TestOriginalGivesABareTableNormalisationButNoOutline(t *testing.T) {
	// HTML leaves a table naked and a browser draws the cells with defaults of its
	// own, which is the gap the app's canvas fills: a bare <table> in the sender's
	// markup comes back with the canvas's collapsed borders and cell padding, and
	// the rules are plain table / th / td rather than :host-qualified because that
	// is what the shadow tree holds. What it must NOT come back with is the app's
	// outline — a border the sender did not write is the app's design, and this
	// mode shows the mail, not the app.
	got := originalBody(t, `<body><table><tr><td>a</td><th>b</th></tr></table></body>`)
	for _, want := range []string{
		"table{border-collapse:collapse}",
		"th,td{padding:.2rem .5rem}",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the canvas is missing %q:\n%s", want, got)
		}
	}
	for _, unwanted := range []string{
		"table,th,td{border",
		"border:1px solid black",
	} {
		if strings.Contains(got, unwanted) {
			t.Errorf("the canvas paints a table outline %q the sender did not ask for:\n%s", unwanted, got)
		}
	}
}

func TestOriginalLetsASenderTableRuleOverrideTheCanvas(t *testing.T) {
	// The canvas is emitted first at the same specificity as the sender's own
	// stylesheet, so a sender who states their own table rules keeps their own
	// look: the last declaration holds. border-collapse is the property to test
	// that with now that the canvas draws no outline — the invariant is about
	// order, not about which property the sender overrides.
	got := originalBody(t, `<html><head><style>table { border-collapse: separate }</style></head><body><table><tr><td>a</td></tr></table></body></html>`)
	canvas := strings.Index(got, "table{border-collapse:collapse}")
	sender := strings.Index(got, "table { border-collapse: separate }")
	if canvas < 0 {
		t.Fatalf("the canvas's table rules are not in the fragment: %q", got)
	}
	if sender < 0 {
		t.Fatalf("the sender's own table rule is missing: %q", got)
	}
	if canvas > sender {
		t.Errorf("the canvas's table rules must come before the sender's, so the sender's override them: %q", got)
	}
}

func TestOriginalRefusesACanvasThatIsNotAColour(t *testing.T) {
	// A presentational attribute is not a place to accept arbitrary CSS: a
	// semicolon in a bgcolor could add a declaration this pass never saw.
	got := originalBody(t, `<body bgcolor="#fff;background:url(https://evil.example/x)"><p>x</p></body>`)
	if strings.Contains(got, "evil.example") || strings.Contains(got, ";background") {
		t.Errorf("bgcolor injected a declaration: %q", got)
	}
}

func TestOriginalDropsNonStylesheetLinks(t *testing.T) {
	got := originalBody(t, `<html><head><link rel="preload" href="https://x.example/f.woff2"><link rel="icon" href="https://x.example/i.png"></head><body><p>words</p></body></html>`)
	if strings.Contains(got, "<link") {
		t.Errorf("a non-stylesheet link survived: %q", got)
	}
}

func TestOriginalDeclinesAPartWithNothingToShow(t *testing.T) {
	for _, raw := range []string{
		"",
		"   ",
		`<html><head><title>t</title></head><body></body></html>`,
		`<html><body><div><br></div></body></html>`,
		`<html><body><script>alert(1)</script></body></html>`,
	} {
		if got, ok := OriginalBody(raw); ok {
			t.Errorf("OriginalBody(%q) = %q, true; want declined", raw, got)
		}
	}
}

func TestOriginalKeepsAStylesheetWithNoBody(t *testing.T) {
	// A part whose body is empty but which carries a stylesheet is still a thing
	// a reader can be shown, and the caller decides what to do with it; declining
	// here would hide the stylesheet of a part whose text was stored separately.
	if _, ok := OriginalBody(`<html><head><style>.a{color:red}</style></head><body></body></html>`); !ok {
		t.Error("a stylesheet with no body declined; want a body")
	}
}

// reOriginalHandlerAttr matches any attribute whose name begins with "on", in
// any element, whatever the quoting.
var reOriginalHandlerAttr = regexp.MustCompile(`(?i)[\s"'/]on[a-z]+\s*=`)

// reOriginalURLPosition matches a script scheme in a position where it would be
// a URL: an attribute that fetches or navigates, or a CSS url(). Bare text is
// not a URL — the transcript linkifies plain text, this policy renders the
// sender's markup and nothing else — so "javascript:alert(1)" arriving as a
// sentence is a sentence.
var reOriginalURLPosition = regexp.MustCompile(`(?i)(?:href|src|action|formaction|background|poster|data|ping)\s*=\s*["']?\s*(?:javascript|vbscript|data\s*:\s*text/html)|url\(\s*["']?\s*(?:javascript|vbscript|data\s*:\s*text/html)`)

// TestOriginalClosesTheExecutableSurface is the counterpart of
// TestOnlyOurOwnTagsSurviveHostileInput: the same attacks, held to the same bar,
// against the other policy. Each is a string a sender could put in a part, and
// the assertion is that nothing executable and nothing that names a script in a
// URL position survives the pass — not by substring alone, but by re-parsing
// what came out and walking it.
func TestOriginalClosesTheExecutableSurface(t *testing.T) {
	attacks := []string{
		`<script>alert(1)</script>`,
		`<img src=x onerror=alert(1)>`,
		`<a href="javascript:alert(1)">x</a>`,
		`javascript:alert(1)`,
		`<style>body{background:url(javascript:alert(1))}</style>`,
		`<iframe src="https://evil.example"></iframe>`,
		`</p><script>alert(1)</script><p>`,
		`<svg/onload=alert(1)>`,
		`data:text/html;base64,PHNjcmlwdD4=`,
		`<a href="http://ok.example" onmouseover="alert(1)">y</a>`,
		`vbscript:msgbox(1)`,
		`<div style="width:expression(alert(1))">z</div>`,
		"<!--<script>alert(1)</script>-->",
		`http://ok.example/"><script>alert(1)</script>`,
		`<base href="https://evil.example/">`,
		`<form action=x><input name=p></form>`,
		`<meta http-equiv="refresh" content="0;url=https://evil.example/">`,
		`<template><img src=x onerror=alert(1)></template>`,
		`<math><mtext><script>alert(1)</script></mtext></math>`,
		`<object data="https://evil.example/"><param name="x" value="y"></object>`,
		`<a href="jAvAsCrIpT&amp;#58;alert(1)">x</a>`,
		`<img src=x onerror=alert(1) onerror=alert(2)>`,
	}
	for _, a := range attacks {
		got, _ := OriginalBody(`<html><body>` + a + `<p>words</p></body></html>`)
		if strings.Contains(strings.ToLower(got), "<script") {
			t.Errorf("input %q left a script element: %q", a, got)
			continue
		}
		if m := reOriginalHandlerAttr.FindString(got); m != "" {
			t.Errorf("input %q left an event handler (%s): %q", a, m, got)
		}
		if m := reOriginalURLPosition.FindString(got); m != "" {
			t.Errorf("input %q left a script URL (%s): %q", a, m, got)
		}
		for _, tag := range reAnyTag.FindAllStringSubmatch(got, -1) {
			if name := strings.ToLower(tag[1]); originalDroppedTags[name] {
				t.Errorf("input %q produced dropped tag <%s>: %q", a, name, got)
			}
		}
	}
}

// originalDroppedTags mirrors documentDrops by name, so the test above can read
// it from the serialised output.
var originalDroppedTags = map[string]bool{
	"script": true, "noscript": true, "meta": true, "base": true, "title": true,
	"iframe": true, "object": true, "embed": true, "applet": true, "param": true,
	"video": true, "audio": true, "canvas": true, "svg": true, "math": true,
	"template": true, "frame": true, "frameset": true, "noframes": true,
	"source": true, "track": true, "marquee": true, "form": true, "input": true,
	"button": true, "select": true, "option": true, "textarea": true,
	"label": true, "fieldset": true, "datalist": true, "output": true,
	"progress": true, "meter": true,
}

func TestOriginalKeepsTruncatedMarkupBalanced(t *testing.T) {
	// docket truncates from the end of a body, so a part can arrive mid-element.
	// Nothing here may leak an unclosed element the caller would then mount: the
	// parser closes it, and the pass re-serialises from the parse tree.
	got, ok := OriginalBody(`<div class="band"><table><tr><td style="background:#fff">half a mes`)
	if !ok {
		t.Fatal("a truncated part declined; want what there was")
	}
	if strings.Count(got, "<div") != strings.Count(got, "</div>") {
		t.Errorf("unbalanced divs in %q", got)
	}
	if !strings.Contains(got, "half a mes") {
		t.Errorf("truncated text was lost: %q", got)
	}
}
