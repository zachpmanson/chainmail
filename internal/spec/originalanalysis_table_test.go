package spec

import (
	"strings"
	"testing"
)

func hasOriginalRule(a OriginalAnalysis, rule string) bool {
	for _, match := range a.Matches {
		if match.Rule == rule {
			return true
		}
	}
	return false
}

func TestOriginalAnalysisWholeMessageTable(t *testing.T) {
	bare := `<table><tr><td>Here is the complete authored message.</td></tr></table>`
	page := `<table width="560" align="center"><tr><td>Here is the complete authored message.</td></tr></table>`
	logo := `<img width="140" height="42" src="https://example.test/logo">`
	for _, tc := range []struct {
		name, raw string
		score     int
		contained bool
		uncertain bool
	}{
		{"bare table is only one point", bare, 1, true, false},
		{"bounded table combines with containment", page, 2, true, false},
		{"neutral wrappers", `<div><section>` + page + `</section></div>`, 2, true, false},
		{"nested tables count once", `<table><tr><td>` + page + `</td></tr></table>`, 2, true, false},
		{"ordinary data only one point", `<table><tr><th>Item</th><th>Total</th></tr><tr><td>Blocks</td><td>12</td></tr></table>`, 1, true, false},
		{"visible prose before", `<p>Outside introduction.</p>` + page, 1, false, false},
		{"visible prose after", page + `<p>Outside conclusion.</p>`, 1, false, false},
		{"one visible outside word", page + `Outside`, 1, false, false},
		{"visible logo outside", logo + page, 1, false, false},
		{"unknown size image outside", page + `<img src="https://example.test/image">`, 1, false, false},
		{"logo inside", strings.Replace(page, `</td>`, logo+`</td>`, 1), 2, true, false},
		{"separate sibling tables", page + bare, 1, false, false},
		{"whitespace outside", " \n\t&nbsp;" + page + " \n", 2, true, false},
		{"hidden preheader and tracker", `<div style="display:none">Hidden preview</div>` + page + `<img width="1" height="1" src="https://example.test/pixel">`, 2, true, false},
		{"zero font outside", `<div style="font-size:0">Invisible preview</div>` + page, 2, true, false},
		{"marked quote outside", `<blockquote type="cite">Earlier message</blockquote>` + page, 2, true, false},
		{"marked signature outside", page + `<div class="gmail_signature">Contact information</div>`, 2, true, false},
		{"contact suffix outside", page + `<div>Alex Example<br>Studio contact<br>` + logo + `<br><a href="mailto:person@example.test">Email</a></div>`, 2, true, false},
		{"unmarked quote outside", page + `<blockquote>Authored pull quote</blockquote>`, 1, false, false},
		{"unwrapped history veto", strings.Replace(page, `</td>`, `<p>From: Earlier author</p><p>Sent: yesterday</p></td>`, 1), 2, true, true},
		{"hidden table", `<div style="display:none">` + page + `</div>`, 0, false, false},
		{"empty table", `<table width="560" align="center"><tr><td>&nbsp;</td></tr></table>`, 0, false, false},
		{"image only table", `<table><tr><td>` + logo + `</td></tr></table>`, 0, false, false},
		{"quote only table", `<blockquote type="cite">` + page + `</blockquote>`, 0, false, false},
		{"signature only table", `<div class="gmail_signature">` + page + `</div>`, 0, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := AnalyseOriginal(tc.raw)
			if a.Score != tc.score || a.Uncertain != tc.uncertain || a.DefaultsToOriginal != (tc.score >= 2 && !tc.uncertain) {
				t.Fatalf("want score=%d uncertain=%v; got %+v", tc.score, tc.uncertain, a)
			}
			count := 0
			for _, match := range a.Matches {
				if match.Rule == "whole message in a table" {
					count++
					if match.Points != 1 {
						t.Errorf("containment must be exactly one point: %+v", match)
					}
				}
			}
			if (count == 1) != tc.contained || count > 1 {
				t.Errorf("want contained=%v with no duplicate votes; got %+v", tc.contained, a)
			}
		})
	}
}
