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
		{"minimum panel width overrides geometry", wrap(strings.Replace(sections, `<table width=`, `<table style="min-width:700px" width=`, 1)), false},
		{"logical panel sizing overrides geometry", wrap(strings.Replace(sections, `<table width=`, `<table style="inline-size:700px" width=`, 1)), false},
		{"percentage width with minimum sizing conflict", wrap(strings.Replace(sections, `<table width=`, `<table style="width:100%;min-width:700px" width=`, 1)), false},
		{"unresolved width with logical sizing conflict", wrap(strings.Replace(sections, `<table width=`, `<table style="width:calc(100% - 10px);inline-size:700px" width=`, 1)), false},
		{"explicit panel left alignment", wrap(strings.ReplaceAll(sections, `<table width=`, `<table align="left" width=`)), false},
		{"explicit panel right alignment", wrap(strings.ReplaceAll(sections, `<table width=`, `<table align="right" width=`)), false},
		{"panel floating", wrap(strings.ReplaceAll(sections, `<table width=`, `<table style="float:left" width=`)), false},
		{"panel margins override alignment", wrap(strings.ReplaceAll(sections, `<table width=`, `<table style="margin:0" width=`)), false},
		{"panel display override", wrap(strings.ReplaceAll(sections, `<table width=`, `<table style="display:block" width=`)), false},
		{"cell display override", strings.Replace(wrap(sections), `<td align="center">`, `<td align="center" style="display:flex">`, 1), false},
		{"cell text alignment override", strings.Replace(wrap(sections), `<td align="center">`, `<td align="center" style="text-align:left">`, 1), false},
		{"horizontal cells", `<table width="100%"><tr><td align="center">` + intro + banner + `</td><td align="center">` + terms + `</td></tr></table>`, false},
		{"nested horizontal table cells", `<table><tr><td>` + wrap(intro+banner) + `</td><td>` + wrap(terms) + `</td></tr></table>`, false},
		{"nested flex items", `<div style="display:flex"><div>` + wrap(intro+banner) + `</div><div>` + wrap(terms) + `</div></div>`, false},
		{"nested grid items", `<div style="display:grid;grid-template-columns:1fr 1fr"><div>` + wrap(intro+banner) + `</div><div>` + wrap(terms) + `</div></div>`, false},
		{"inline block scaffolds", `<div style="display:inline-block">` + wrap(intro+banner) + `</div><div style="display:inline-block">` + wrap(terms) + `</div>`, false},
		{"row display override", strings.Replace(wrap(sections), `<tr><td align="center">`, `<tr style="display:flex"><td align="center">`, 1), false},
		{"row group display override", strings.Replace(wrap(sections), `<tr><td align="center">`, `<tbody style="display:flex"><tr><td align="center">`, 1), false},
		{"table display override", strings.Replace(wrap(sections), `<table width="100%">`, `<table width="100%" style="display:flex">`, 1), false},
		{"spanning cell", strings.Replace(wrap(sections), `<td align="center">`, `<td align="center" colspan="2">`, 1), false},
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
