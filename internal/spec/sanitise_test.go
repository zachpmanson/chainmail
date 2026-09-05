package spec

import (
	"regexp"
	"strings"
	"testing"
)

// renderBodyHTML is bodyHTML for a direct mail entry with a stored html part.
func renderBodyHTML(raw string) string {
	return bodyHTML(&entryRow{Source: "mail", Direct: true,
		BodyText: "the text rendition", BodyHTML: raw})
}

// reAnyTagSan is any attribute, for the "no event handlers" check.
var reAnyTagSan = regexp.MustCompile(`(?i)<\s*/?\s*([a-z0-9]+)([^>]*)>`)

// The allowlist in sanitise.go is the boundary the renderer trusts, and it is
// tested at that boundary: a body the generator could produce from any
// source — its own html part, markup recovered from a host, or the text
// path — goes in, and only the allowlist comes out. Every part below is
// invented.

func TestHostileMarkupInTheHtmlPartIsStripped(t *testing.T) {
	hostile := []string{
		`<script>alert(1)</script>`,
		`<img src=x onerror=alert(1)>`,
		`<a href="javascript:alert(1)">click</a>`,
		`<svg/onload=alert(1)>`,
		`<style>*{display:none}</style>`,
		`<iframe src="https://evil.example"></iframe>`,
		`<base href="https://evil.example/">`,
		`<meta http-equiv=refresh content="0;url=https://evil.example">`,
		`<form action=https://evil.example><input name=p></form>`,
		`<a href="http://ok.example" onmouseover="alert(1)">y</a>`,
		`<video src="https://evil.example/x.mp4"></video>`,
		`<object data="https://evil.example/x.swf"></object>`,
		`<p style="position:fixed;top:0">text</p>`,
		`<a href="vbscript:msgbox(1)">z</a>`,
		`<a href="data:text/html;base64,PHNjcmlwdD4=">w</a>`,
		`<!--<script>alert(1)</script>-->`,
		`<body onload="alert(1)"><p>real text</p></body>`,
	}
	for _, h := range hostile {
		got := renderBodyHTML(`<div>` + h + `</div>`)
		for _, m := range reAnyTagSan.FindAllStringSubmatch(got, -1) {
			attrs := strings.ToLower(m[2])
			if strings.Contains(attrs, " on") {
				t.Errorf("input %q produced an event handler: %q", h, m[0])
			}
			if strings.Contains(strings.ToLower(got), "javascript:") ||
				strings.Contains(strings.ToLower(got), "vbscript:") {
				t.Errorf("input %q left an executable URL in %q", h, got)
			}
		}
		low := strings.ToLower(got)
		for _, gone := range []string{"<script", "<style", "<svg", "<iframe",
			"<base ", "<meta ", "<form", "<input", "<video", "<object", "<!--"} {
			if strings.Contains(low, gone) {
				t.Errorf("input %q left %q in %q", h, gone, got)
			}
		}
		// The words survive even when the chrome does not.
		if strings.Contains(h, ">click</a>") && !strings.Contains(got, "click") {
			t.Errorf("input %q lost its link text in %q", h, got)
		}
	}
}

// TestHostileMarkupInTheTextPathStaysText is the html-part sibling of
// inj_test.go: hostile input on the TEXT path must stay exactly as escaped as
// before, and the sanitiser must not widen what the escaping let through.
func TestHostileMarkupInTheTextPathStaysText(t *testing.T) {
	allowed := map[string]bool{
		"p": true, "br": true, "blockquote": true, "a": true,
		"details": true, "summary": true, "div": true,
	}
	attacks := []string{
		`<script>alert(1)</script>`,
		`<img src=x onerror=alert(1)>`,
		`<a href="javascript:alert(1)">x</a>`,
		`<svg/onload=alert(1)>`,
		`<style>*{display:none}</style>`,
		`<iframe src="https://evil.example"></iframe>`,
		`<details class="sig"><summary title="folded">sig</summary><div class="sigbd">body</div></details>`,
	}
	for _, a := range attacks {
		got := bodyHTML(&entryRow{Source: "mail", Direct: true, BodyText: a})
		for _, m := range reAnyTagSan.FindAllStringSubmatch(got, -1) {
			tag, attrs := strings.ToLower(m[1]), strings.ToLower(m[2])
			if !allowed[tag] {
				t.Errorf("input %q produced disallowed tag <%s> in %q", a, tag, got)
			}
			if strings.Contains(attrs, " on") {
				t.Errorf("input %q produced <%s> with an event handler: %q", a, tag, m[0])
			}
		}
	}
}

// TestCleanSenderMarkupSurvives is the regression guard that matters most: the
// html part path exists to preserve the sender's tables, lists, links and
// images, and the sanitiser must not drain a real message of the formatting
// this tool was built to keep.
func TestCleanSenderMarkupSurvives(t *testing.T) {
	part := `<div dir="ltr">Signed off on ` +
		`<a href="https://loomworks.example/rate-review?id=2547678">this review</a>.</div>` +
		`<table><tbody><tr><td colspan="2">meter reads</td></tr></tbody></table>` +
		`<ul><li>confirm the demand class</li></ul>` +
		`<img src="https://loomworks.example/inline.png" width="300" alt="chart" style="color:red">` +
		`<blockquote>quoted</blockquote><p style="text-align:center;width:1000px"><b>done</b></p>`
	got := renderBodyHTML(part)
	for _, want := range []string{
		`href="https://loomworks.example/rate-review?id=2547678"`,
		"<table>", "<tbody>", "<tr>", `<td colspan="2">`,
		"<ul>", "<li>", "confirm the demand class",
		`src="https://loomworks.example/inline.png"`, `width="300"`, `alt="chart"`,
		"<blockquote>quoted</blockquote>",
		"text-align:center",
		"<b>done</b>",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("body = %q, want %q to survive", got, want)
		}
	}
	for _, gone := range []string{"width:1000px", `dir="ltr"`, "onmouseover", "onerror",
		"background:"} {
		if strings.Contains(got, gone) {
			t.Errorf("body = %q, want %q gone", got, gone)
		}
	}
}

// TestTheFoldAndTheGlossSurvive: <details class="sig">, <summary>, the
// fold's <div class="sigbd"> and the editorial <p class="ed"> are the
// generator's own markup, produced after stripChrome as the only classes a
// body carries. The allowlist must let exactly those through.
func TestTheFoldAndTheGlossSurvive(t *testing.T) {
	raw := `<details class="sig"><summary title="Folded, not removed">signature</summary>` +
		`<div class="sigbd">Ada Byron<br/>Head of Metering</div></details>` +
		`<p class="ed">Blank — only forwarded messages.</p>`
	got := sanitiseBody(raw)
	if !strings.Contains(got, `<details class="sig">`) ||
		!strings.Contains(got, `<summary title=`) ||
		!strings.Contains(got, `<div class="sigbd">`) ||
		!strings.Contains(got, `<p class="ed">`) {
		t.Errorf("sanitised = %q, want the generator's own fold and gloss kept", got)
	}
	if strings.Contains(got, `class="gmail_signature"`) {
		t.Errorf("sanitised = %q, want sender classes gone", got)
	}
}

// TestStyleIsFilteredBeforeItSurvives: style is the one attribute that could
// smuggle a URL or a layout attack past a tag allowlist, so it is filtered
// to the same five properties stripChrome keeps.
func TestStyleIsFilteredBeforeItSurvives(t *testing.T) {
	got := sanitiseBody(`<span style="color:#ff0000;width:1000px;background:url(javascript:alert(1));text-align:right">hi</span>`)
	if !strings.Contains(got, "color:#ff0000") || !strings.Contains(got, "text-align:right") {
		t.Errorf("sanitised = %q, want the kept props", got)
	}
	if strings.Contains(got, "width:1000px") || strings.Contains(got, "background") ||
		strings.Contains(got, "javascript") {
		t.Errorf("sanitised = %q, want hostile style gone", got)
	}
}

// TestURLSchemesAreTheWholeBoundary: an anchor may point at http(s) only and
// an image may load from http(s) or a data: image. The link text stays even
// when the href goes.
func TestURLSchemesAreTheWholeBoundary(t *testing.T) {
	got := sanitiseBody(`<a href="javascript:alert(1)">x</a>` +
		`<a href="mailto:ada@loomworks.example">mail</a>` +
		`<a href="/relative">rel</a>` +
		`<a href="https://ok.example">ok</a>` +
		`<img src="data:image/png;base64,iVBORw0KGgo=">` +
		`<img src="data:text/html;base64,PHNjdw==">` +
		`<img src="https://ok.example/pic.png">`)
	if !strings.Contains(got, `href="https://ok.example"`) {
		t.Errorf("sanitised = %q, want a clean https anchor kept", got)
	}
	if !strings.Contains(got, `src="data:image/png;base64,iVBORw0KGgo="`) {
		t.Errorf("sanitised = %q, want a data: image kept", got)
	}
	if !strings.Contains(got, `src="https://ok.example/pic.png"`) {
		t.Errorf("sanitised = %q, want an https image kept", got)
	}
	for _, gone := range []string{"javascript:", "mailto:", `href="/relative"`,
		"data:text/html"} {
		if strings.Contains(strings.ToLower(got), gone) {
			t.Errorf("sanitised = %q, want %q gone", got, gone)
		}
	}
	if strings.Count(got, ">x</a>") != 1 || !strings.Contains(got, ">mail</a>") ||
		!strings.Contains(got, ">rel</a>") {
		t.Errorf("sanitised = %q, want dropped-href links to keep their text", got)
	}
}

// TestUnknownTagsAreFlattenedNotDeleted: a tag the allowlist does not name
// keeps its text — an unknown element in a sender's email still says what the
// sender said, just without the tag.
func TestUnknownTagsAreFlattenedNotDeleted(t *testing.T) {
	got := sanitiseBody(`<o:p>an office artifact</o:p><custom-tag data-x="1">custom</custom-tag>`)
	if !strings.Contains(got, "an office artifact") || !strings.Contains(got, "custom") {
		t.Errorf("sanitised = %q, want the words kept", got)
	}
	if strings.Contains(got, "<o:p") || strings.Contains(got, "<custom") ||
		strings.Contains(got, "data-x") {
		t.Errorf("sanitised = %q, want the tags and their attributes gone", got)
	}
}

// TestCommentsAndDoctypeDoNotSurvive: the only rendition a comment or a
// doctype has in a transcript is none.
func TestCommentsAndDoctypeDoNotSurvive(t *testing.T) {
	got := sanitiseBody(`<p>real</p><!--[if gte mso 9]><i>office</i><![endif]--><!DOCTYPE html>`)
	if got != "<p>real</p>" {
		t.Errorf("sanitised = %q, want the markup reduced to the paragraph", got)
	}
}

// TestMalformedMarkupComesOutBalanced: the html-part path re-serialises from
// a parse tree so truncation cannot leak an unclosed element; the sanitiser
// must keep that property, not just filter on it.
func TestMalformedMarkupComesOutBalanced(t *testing.T) {
	got := sanitiseBody(`<div><p>unclosed <b>bold`)
	open, close := strings.Count(got, "<b>"), strings.Count(got, "</b>")
	if open != close && open > 0 {
		t.Errorf("sanitised = %q, want balanced markup", got)
	}
	// The parser closes the truncation, so the paragraph and its emphasis
	// both survive as closed elements.
	if !strings.Contains(got, "</b>") || !strings.Contains(got, "</p>") || !strings.Contains(got, "</div>") {
		t.Errorf("sanitised = %q, want the truncation closed by the re-serialisation", got)
	}
}
