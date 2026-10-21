package spec

import (
	"encoding/json"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

const designedOriginal = `<div style="max-width:600px;padding:24px;background:#fff"><h1>Project update for the whole team</h1><p>Here is the work we completed this week and the plan for next week.</p><a href="https://example.test/action" style="display:inline-block;background:#123;padding:12px">Open project</a></div>`

func TestOriginalAnalysisStructures(t *testing.T) {
	data := `<table><tr><th>Item</th><th>Total</th></tr><tr><td>A</td><td>12</td></tr></table>`
	contact := `<table width="600"><tr><td><table><tr><td><span style="font-size:24px">Alex Example</span><br>Company and contact information</td><td><img width="100" height="80" src="https://example.test/logo"></td></tr></table><a href="mailto:person@example.test">Email me</a><a href="https://example.test">Website</a></td></tr></table>`
	for _, tc := range []struct {
		name, raw string
		want      bool
	}{
		{"empty", "", false},
		{"designed page", designedOriginal, true},
		{"short designed main", `<table width="600" align="center"><tr><td style="padding:24px;background:#eef;border:1px solid #ccd"><h2>Review packet</h2><p>The agenda is ready.</p><a href="https://example.test/open" style="display:inline-block;background:#147;padding:12px">Open packet</a></td></tr></table>`, true},
		{"ordinary references inside a painted page cell", `<table width="600"><tr><td style="padding:24px;background:#eef"><p>This is ordinary correspondence with enough words and two inline reference links to follow.</p><a href="https://example.test/one">First reference</a><a href="https://example.test/two">Second reference</a></td></tr></table>`, false},
		{"correlated padded wrappers", `<div style="width:600px;padding:24px;background:#eef"><div style="padding:24px;background:#eef"><div style="padding:24px;background:#eef">This is ordinary correspondence with enough words and no independent designed elements at all.</div></div></div>`, false},
		{"uniform large body", `<div style="width:600px;font-size:24px"><p>This ordinary message uses uniformly large text for the whole body.</p><p>It does not have a separate visual heading or any other independent design evidence.</p></div>`, false},
		{"uniform large type inherited outside candidate", `<div style="font-size:24px"><table width="600"><tr><td><div style="font-size:24px">Short opening line</div><p>Ordinary correspondence uses the same size everywhere with no independent design evidence.</p></td></tr></table></div>`, false},
		{"prominent banner geometry", `<div style="width:600px;padding:24px;background:#eef"><p>This designed main content has a prominent banner and ordinary inline reference links.</p><img width="400" height="90" src="https://example.test/banner"><a href="https://example.test/one">One</a><a href="https://example.test/two">Two</a></div>`, true},
		{"positive after closed quote", `<blockquote type="cite"><p>Earlier question</p></blockquote>` + designedOriginal, true},
		{"positive after closed quote containing real headers", `<div><blockquote type="cite"><p>From: earlier author</p><p>Sent: yesterday</p><p>Old message</p></blockquote>` + designedOriginal + `</div>`, true},
		{"positive before between and after quotes", designedOriginal + `<div class="gmail_quote">Earlier question</div>` + designedOriginal + `<blockquote type="cite">Old message</blockquote>` + designedOriginal, true},
		{"signature before positive", `<div class="gmail_signature">Contact details</div>` + designedOriginal, true},
		{"literal field not header", `<p>The literal field From: is required.</p>` + designedOriginal, true},
		{"unused CSS not evidence", `<style>.unused{width:600px;padding:24px;background:#eef}</style><p>Hello</p>`, false},
		{"painted data cells", `<table width="600"><tr><th>Data</th><th>Total</th></tr><tr><td style="padding:24px;background:#eef">Data table text that should not be treated as an independent page module.</td><td style="padding:24px;background:#eee">More data table text that remains ordinary data rather than designed content.</td></tr></table>`, false},
		{"positive after contact suffix", `<p>Yes, that works.</p>` + contact + designedOriginal, true},
		{"contact sentence inside main design", strings.Replace(designedOriginal, "Here is", `<span>Reach us at <a href="mailto:team@example.test">our team address</a>.</span> Here is`, 1), true},
		{"substantial prose inside design", strings.Replace(designedOriginal, "Here is", strings.Repeat("More substantive authored prose. ", 200)+"Here is", 1), true},
		{"short reply and unmarked nested contact suffix", "<div>Yes, that works.</div>" + contact, false},
		{"sibling data tables", data + data, false},
		{"nested data and inline colour", `<div style="color:red">` + data + `<table><tr><td>` + data + `</td></tr></table></div>`, false},
		{"hidden design", `<div style="display: none !important">` + designedOriginal + `</div><p>Hello</p>`, false},
		{"zero font with visible children", `<div style="font-size:0"><div style="font-size:16px">` + designedOriginal + `</div></div>`, true},
		{"zero font invisible text", `<div style="font-size:0">` + designedOriginal + `</div>`, false},
		{"zero height image no graphic", `<div style="width:600px"><p>This is ordinary text with enough words to be considered the main content.</p><img width="400" height="0" src="https://example.test/pixel"></div>`, false},
		{"tracker no graphic", `<div style="width:600px"><p>Ordinary main text</p><p>This is ordinary text with enough words to be considered the main content.</p><img width="1" height="1" src="https://example.test/pixel"></div>`, false},
		{"flat div modules", `<div style="max-width:600px"><div style="padding:20px;background:#eee">First module with enough words to explain the work we have done.</div><div style="padding:20px;background:#ddd">Second module with more words explaining the work remaining.</div><a href="https://example.test/go" style="display:inline-block;background:#123;padding:12px">Open project</a></div>`, true},
		{"disconnected design", `<div style="width:600px"><p>Ordinary message with enough words to read but not a designed page.</p></div><h1>Separate heading</h1><a href="https://example.test/go" style="display:inline-block;background:#123;padding:12px">Open project</a>`, false},
		{"unwrapped history uncertain", `<p>From: Alex</p><p>Sent: yesterday</p>` + designedOriginal, false},
		{"unmarked pull quote retained", strings.Replace(designedOriginal, "<p>", "<blockquote>", 1), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := AnalyseOriginal(tc.raw)
			if a.DefaultsToOriginal != tc.want {
				t.Fatalf("got %+v, want original=%v", a, tc.want)
			}
			sum := 0
			for _, m := range a.Matches {
				sum += m.Points
			}
			if sum != a.Score {
				t.Fatalf("receipt total %d != %d", sum, a.Score)
			}
		})
	}
}

func TestOriginalAnalysisInvariants(t *testing.T) {
	want := AnalyseOriginal(designedOriginal)
	for _, wrap := range []string{
		`<div class="renamed" id="other">%s</div>`,
		`<article><div><section>%s</section></div></article>`,
		`%s<div class="extra x_GMAIL_SIGNATURE">` + designedOriginal + `</div>`,
		`%s<details class="sig"><summary>Signature</summary>` + designedOriginal + `</details>`,
		`<div class="x_gmail_quote">` + designedOriginal + `</div>%s`,
		`%s<blockquote type="cite">` + designedOriginal + `</blockquote>`,
		`%s<div style="display:none">` + designedOriginal + `</div>`,
		`%s<div style="display:none"><p>From: earlier author</p><p>Sent: yesterday</p>` + designedOriginal + `</div>`,
		`<div style="display:none"><p>From: earlier author</p><p>Sent: yesterday</p></div>%s`,
	} {
		raw := strings.Replace(wrap, "%s", designedOriginal, 1)
		got := AnalyseOriginal(raw)
		a, _ := json.Marshal(got)
		b, _ := json.Marshal(want)
		if string(a) != string(b) {
			t.Errorf("mutation %s: %s != %s", wrap, a, b)
		}
	}
	for _, raw := range []string{
		strings.ReplaceAll(designedOriginal, "example.test/action", "renamed.invalid/something"),
		strings.ReplaceAll(designedOriginal, "<p>", `<div class="renamed">`),
	} {
		if got := AnalyseOriginal(raw); got.Score != want.Score || got.DefaultsToOriginal != want.DefaultsToOriginal {
			t.Errorf("renamed: %+v", got)
		}
	}
	prose := strings.Repeat("A substantive paragraph with inline <span style=\"color:red\">answers</span> and details. ", 30)
	for _, raw := range []string{"<p>" + prose + "</p><p>" + prose + "</p>", "<div>" + prose + "</div><div>" + prose + "</div>", prose + "<br><br>" + prose} {
		if a := AnalyseOriginal(raw); a.Score != 0 || a.DefaultsToOriginal {
			t.Errorf("equivalent correspondence: %+v", a)
		}
	}
}

func TestAnalysisQuoteExtractionKeepsAuthoredSiblings(t *testing.T) {
	raw := `<div>before<div class="gmail_quote">history</div>between<blockquote type="cite">history</blockquote>after<blockquote>pull quote</blockquote>end</div>`
	doc, _ := html.Parse(strings.NewReader(raw))
	_, body := documentParts(doc)
	if analysisBoundaries(body) {
		t.Fatal("explicit quotes should be separable")
	}
	text := textHead(body, 2000)
	for _, s := range []string{"before", "between", "after", "pull quote", "end"} {
		if !strings.Contains(text, s) {
			t.Errorf("lost %s: %s", s, text)
		}
	}
	if strings.Contains(text, "history") {
		t.Errorf("kept history: %s", text)
	}
	for _, marker := range []string{`class="gmail_quote"`, `class="x_GMAIL_QUOTE"`, `type="cite"`} {
		if got := AnalyseOriginal(`<blockquote ` + marker + `>` + designedOriginal + `</blockquote>`); got.DefaultsToOriginal || got.Score != 0 {
			t.Errorf("quote only: %+v", got)
		}
	}
	for _, raw := range []string{
		`<div class="gmail_signature">` + designedOriginal + `</div>`,
		`<div class="extra x_GMAIL_SIGNATURE">` + designedOriginal + `</div>`,
		`<details class="sig">` + designedOriginal + `</details>`,
	} {
		if got := AnalyseOriginal(raw); got.Score != 0 {
			t.Errorf("signature only: %+v", got)
		}
	}
	original, _ := OriginalBody(raw)
	AnalyseOriginal(raw)
	after, _ := OriginalBody(raw)
	if original != after {
		t.Fatal("analysis mutated original")
	}
}
