package spec

import (
	"os"
	"strings"
	"testing"
)

func TestOriginalAnalysisComplexNestedTable(t *testing.T) {
	fixture, err := os.ReadFile("../../testdata/original-nested-layout.html")
	if err != nil {
		t.Fatal(err)
	}
	layout := string(fixture)
	wrap := func(s string) string { return `<table><tr><td>` + s + `</td></tr></table>` }
	prose := strings.Repeat("An ordinary paragraph discusses the work and asks for a written response. ", 30)
	data := `<table><tr><th>Item</th><th>Quantity</th></tr><tr><td>Garden tools</td><td>Twelve pairs</td></tr></table>`
	contact := `<table><tr><td>Robin Example<br>Community office<br><img width="100" height="30" src="https://example.test/logo"><br><a href="mailto:person@example.test">Write to the office</a></td></tr></table>`
	control := `<a href="https://example.test/action" style="display:block;background:blue;padding:12px">` + layout + `</a>`
	inlineControl := `<table style="display:inline-block"><tr><td><a href="https://example.test/action" style="display:block;background:blue;padding:12px">Choose this option</a></td></tr></table>`
	for _, tc := range []struct {
		name, raw string
		complex   bool
		uncertain bool
	}{
		{"composed authored layout", layout, true, false},
		{"neutral wrappers cannot change point", strings.Repeat(`<table><tr><td>`, 12) + layout + strings.Repeat(`</td></tr></table>`, 12), true, false},
		{"deeper composed levels still one point", wrap(layout + layout), true, false},
		{"tiny invalid control alongside genuine layout", wrap(layout + inlineControl), true, false},
		{"class and domain renaming", strings.ReplaceAll(layout, "<table>", `<table class="opaque" id="opaque">`), true, false},
		{"plain unary depth", strings.Repeat(`<table><tr><td>`, 30) + prose + strings.Repeat(`</td></tr></table>`, 30), false, false},
		{"minimal unary depth", strings.Repeat(`<table><tr><td>`, 30) + `A short message.` + strings.Repeat(`</td></tr></table>`, 30), false, false},
		{"shallow ordinary data", wrap(data), false, false},
		{"repeated nested data", wrap(data + wrap(data+data)), false, false},
		{"empty tables and spacer cells", wrap(strings.Repeat(wrap(`<table><tr><td>&nbsp;</td><td></td></tr></table>`), 30)), false, false},
		{"hidden complex layout", wrap(prose + `<div hidden>` + strings.Repeat(layout, 3) + `</div>`), false, false},
		{"invisible complex layout", wrap(prose + `<div style="opacity:0">` + strings.Repeat(layout, 3) + `</div>`), false, false},
		{"marked signature layouts", wrap(prose + strings.Repeat(`<div class="gmail_signature">`+layout+`</div>`, 3)), false, false},
		{"marked quote layouts", wrap(prose + strings.Repeat(`<blockquote type="cite">`+layout+`</blockquote>`, 3)), false, false},
		{"contact suffix layouts", wrap(prose + strings.Repeat(contact, 8)), false, false},
		{"standalone contact wrappers", wrap(wrap(contact + wrap(contact+contact))), false, false},
		{"action text is not authored branching", control, false, false},
		{"tracking images do not make branches", wrap(prose + strings.Repeat(wrap(`<img width="1" height="1" src="https://example.test/pixel">`), 30)), false, false},
		{"tiny complex fragment outside prose", prose + layout + prose, false, false},
		{"wrapped plain body with trailing complex fragment", wrap(prose + layout), false, false},
		{"substantial but late complex branch", wrap(strings.Repeat("An ordinary paragraph discusses the work and asks for a written response. ", 12) + layout), false, false},
		{"small early fragment inside plain body", wrap(layout + prose), false, false},
		{"complex footer cannot borrow table coverage", wrap(prose + `<footer>` + layout + `</footer>`), false, false},
		{"non-native critical tables", strings.ReplaceAll(layout, `<table>`, `<table style="display:block">`), false, false},
		{"non-native critical primary branch", strings.Replace(layout, "<table><tr><td>\n<table>\n", "<table style=\"display:inline-block\"><tr><td>\n<table>\n", 1), false, false},
		{"non-native owned rows", strings.ReplaceAll(layout, `<tr>`, `<tr style="display:flex">`), false, false},
		{"non-native owned cells", strings.ReplaceAll(layout, `<td>`, `<td style="display:block">`), false, false},
		{"uncertain boundary still vetoes default", strings.Replace(layout, `</td>`, `<p>From: Earlier author</p><p>Sent: yesterday</p></td>`, 1), true, true},
		{"resource cap still vetoes analysis", strings.Repeat(" ", analysisMaxBytes) + layout, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := AnalyseOriginal(tc.raw)
			if hasOriginalRule(a, "complex nested table layout") != tc.complex || a.Uncertain != tc.uncertain {
				t.Fatalf("want complex=%v uncertain=%v; got %+v", tc.complex, tc.uncertain, a)
			}
			counts, points := map[string]int{}, 0
			for _, match := range a.Matches {
				counts[match.Rule]++
				points += match.Points
				if (match.Rule == "whole message in a table" || match.Rule == "complex nested table layout") && (match.Points != 1 || counts[match.Rule] != 1) {
					t.Fatalf("global votes must be exactly one independent point each: %+v", a)
				}
			}
			if a.Score != points || a.DefaultsToOriginal != (a.Score >= 2 && !a.Uncertain) {
				t.Fatalf("score or uncertainty contract changed: %+v", a)
			}
			if tc.name == "composed authored layout" && (a.Score != 2 || !a.DefaultsToOriginal || !hasOriginalRule(a, "whole message in a table")) {
				t.Fatalf("invented layout must combine containment and complexity only: %+v", a)
			}
		})
	}
}
