package spec

import (
	"strings"
	"testing"
)

func TestOriginalAnalysisSharedCellColumn(t *testing.T) {
	panel := func(body string) string { return `<table width="552"><tr><td>` + body + `</td></tr></table>` }
	intro := panel(`<h1>Seasonal project overview</h1><p>Explore the latest work and the plans for the coming season.</p>`)
	banner := panel(`<img width="552" height="180" src="https://example.test/banner">`)
	terms := panel(`<p>` + strings.Repeat("Additional details about this project and its requirements. ", 100) + `</p>`)
	sections := intro + banner + terms
	wrap := func(body string) string {
		return `<table width="100%"><tr><td align="center">` + body + `</td></tr></table>`
	}
	for _, tc := range []struct {
		name, raw string
		want      bool
	}{
		{"shared centered cell", wrap(sections), true},
		{"neutral outer wrappers", `<div><section>` + wrap(sections) + `</section></div>`, true},
		{"separate single-cell scaffolds", wrap(intro+banner) + wrap(terms), true},
		{"no centering", strings.Replace(wrap(sections), ` align="center"`, "", 1), false},
		{"left aligned cell", strings.Replace(wrap(sections), `align="center"`, `align="left"`, 1), false},
		{"unequal panel widths", wrap(strings.Replace(sections, `width="552"`, `width="480"`, 1)), false},
		{"explicit panel left alignment", wrap(strings.ReplaceAll(sections, `<table width=`, `<table align="left" width=`)), false},
		{"explicit panel right alignment", wrap(strings.ReplaceAll(sections, `<table width=`, `<table align="right" width=`)), false},
		{"panel floating", wrap(strings.ReplaceAll(sections, `<table width=`, `<table style="float:left" width=`)), false},
		{"panel margins override alignment", wrap(strings.ReplaceAll(sections, `<table width=`, `<table style="margin:0" width=`)), false},
		{"panel display override", wrap(strings.ReplaceAll(sections, `<table width=`, `<table style="display:block" width=`)), false},
		{"cell display override", strings.Replace(wrap(sections), `<td align="center">`, `<td align="center" style="display:flex">`, 1), false},
		{"cell text alignment override", strings.Replace(wrap(sections), `<td align="center">`, `<td align="center" style="text-align:left">`, 1), false},
		{"horizontal cells", `<table width="100%"><tr><td align="center">` + intro + banner + `</td><td align="center">` + terms + `</td></tr></table>`, false},
		{"unrelated centered ancestor", `<div align="center"><table><tr><td>` + sections + `</td></tr></table></div>`, false},
		{"ordinary centered prose", wrap(panel(`<p>Here is the first ordinary paragraph of our correspondence.</p>`) + terms), false},
		{"hidden banner and no heading", wrap(panel(`<p>Here is the first ordinary paragraph of our correspondence.</p>`) + strings.Replace(banner, `<img `, `<img style="display:none" `, 1) + terms), false},
		{"late action only", wrap(terms + panel(`<a href="https://example.test/open" style="display:inline-block;padding:12px;background:#123">Open project</a>`)), false},
		{"dominant prose outside column", `<p>` + strings.Repeat("Separate ordinary correspondence with substantive details. ", 400) + `</p>` + wrap(intro+banner+terms), false},
		{"marked history", `<blockquote type="cite">` + wrap(sections) + `</blockquote>`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := AnalyseOriginal(tc.raw); got.DefaultsToOriginal != tc.want {
				t.Fatalf("want original=%v; got %+v", tc.want, got)
			}
		})
	}
}
