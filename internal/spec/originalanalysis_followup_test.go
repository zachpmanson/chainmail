package spec

import (
	"strings"
	"testing"
)

func TestOriginalAnalysisInlineControls(t *testing.T) {
	for _, tc := range []struct {
		name, style, label, href string
		original                 bool
	}{
		{"implicit inline", "background:#176;padding:10px 28px;border-radius:6px", "Open project", "https://example.test/open", true},
		{"explicit inline", "display:inline;background:#176;padding:8px 16px", "Open project", "https://example.test/open", true},
		{"four sides", "background:#176;padding:10px 20px 12px 24px", "Open project", "https://example.test/open", true},
		{"longhands", "background:#176;padding-top:10px;padding-bottom:10px;padding-left:28px;padding-right:28px", "Open project", "https://example.test/open", true},
		{"border control", "border:1px solid #176;padding:10px 28px", "Open project", "https://example.test/open", true},
		{"colored reference", "color:#176;text-decoration:underline", "Project reference", "https://example.test/open", false},
		{"highlighted reference", "background:#ff0;padding:2px 4px", "Project reference", "https://example.test/open", false},
		{"rounded highlight", "background:#ff0;border-radius:6px", "Project reference", "https://example.test/open", false},
		{"only horizontal padding", "background:#176;padding:0 28px", "Project reference", "https://example.test/open", false},
		{"only vertical padding", "background:#176;padding:28px 0", "Project reference", "https://example.test/open", false},
		{"inherited cell paint", "padding:10px 28px;border-radius:6px", "Project reference", "https://example.test/open", false},
		{"inherited border", "border:inherit;padding:10px 28px", "Project reference", "https://example.test/open", false},
		{"overridden zero border", "border:1px solid #176;border-width:0;padding:10px 28px", "Project reference", "https://example.test/open", false},
		{"overridden border style", "border:1px solid #176;border-style:none;padding:10px 28px", "Project reference", "https://example.test/open", false},
		{"color only border", "border:#176;padding:10px 28px", "Project reference", "https://example.test/open", false},
		{"transparent paint", "background:rgba(0,0,0,0);padding:10px 28px", "Project reference", "https://example.test/open", false},
		{"reset color by shorthand", "background-color:#176;background:transparent;padding:10px 28px", "Project reference", "https://example.test/open", false},
		{"mixed background reverse order", "background:transparent;background-color:#176;padding:10px 28px", "Project reference", "https://example.test/open", false},
		{"important background reset", "background-color:#176!important;background:none!important;padding:10px 28px", "Project reference", "https://example.test/open", false},
		{"background longhand override", "background:#176;background-image:none;padding:10px 28px", "Project reference", "https://example.test/open", false},
		{"nonpainting shorthand", "background:none no-repeat;padding:10px 28px", "Project reference", "https://example.test/open", false},
		{"unsupported shorthand", "background:url(https://example.test/unknown) no-repeat;padding:10px 28px", "Project reference", "https://example.test/open", false},
		{"unresolved shorthand color", "background:#176 no-repeat;padding:10px 28px", "Project reference", "https://example.test/open", false},
		{"invalid color", "background:#ggg;padding:10px 28px", "Project reference", "https://example.test/open", false},
		{"named color", "background:teal;padding:10px 28px", "Open project", "https://example.test/open", true},
		{"border independent of background reset", "background:#176;background-color:transparent;border:1px solid #176;padding:10px 28px", "Open project", "https://example.test/open", true},
		{"transparent border", "border:1px solid transparent;padding:10px 28px", "Project reference", "https://example.test/open", false},
		{"zero border", "border:0 solid #176;padding:10px 28px", "Project reference", "https://example.test/open", false},
		{"descendant paint", "", `<span style="background:#176;padding:10px 28px">Open project</span>`, "https://example.test/open", false},
		{"unknown image not compact", "background:#176;padding:10px 28px", `<img src="https://example.test/image">Open project`, "https://example.test/open", false},
		{"padded long reference", "background:#ff0;padding:10px 28px", strings.Repeat("reference ", 13), "https://example.test/open", false},
		{"mixed padding cascade", "background:#176;padding:0!important;padding-top:10px;padding-bottom:10px;padding-left:28px;padding-right:28px", "Project reference", "https://example.test/open", false},
		{"empty destination", "background:#176;padding:10px 28px", "Open project", "", false},
		{"mail destination", "background:#176;padding:10px 28px", "Contact project", "mailto:person@example.test", false},
		{"telephone destination", "background:#176;padding:10px 28px", "Contact project", "tel:+1234567", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := `<table width="600"><tr><td style="padding:24px;background:#eef"><p>Here is the primary authored project message.</p><a href="` + tc.href + `" style="` + tc.style + `">` + tc.label + `</a></td></tr></table>`
			a := AnalyseOriginal(raw)
			wantScore := 1
			if tc.original {
				wantScore = 2
			}
			if a.DefaultsToOriginal != tc.original || a.Score != wantScore {
				t.Fatalf("want original=%v score=%d; got %+v", tc.original, wantScore, a)
			}
		})
	}
	late := `<div style="width:600px;margin:0 auto"><p>` + strings.Repeat("Ordinary substantive correspondence. ", 100) + `</p><a href="https://example.test/profile" style="background:#176;padding:10px 28px">View profile</a></div>`
	if a := AnalyseOriginal(late); a.DefaultsToOriginal {
		t.Fatalf("late inline profile control described the main prose: %+v", a)
	}
}

func TestOriginalAnalysisInlinePaddingUnits(t *testing.T) {
	for _, tc := range []struct {
		name, font, padding string
		original            bool
	}{
		{"inherited small em", "8px", "padding:.5em 1em", false},
		{"inherited small em longhands", "8px", "padding-top:.5em;padding-bottom:.5em;padding-left:1em;padding-right:1em", false},
		{"resolved small em", "8px", "padding:1em 2em", true},
		{"resolved large em", "32px", "padding:.25em .5em", true},
		{"px independent of font", "8px", "padding:8px 16px", true},
		{"pt lengths", "8px", "padding:6pt 12pt", true},
		{"positive unitless", "16px", "padding:8 16", false},
		{"positive unitless longhands", "16px", "padding-top:8;padding-bottom:8;padding-left:16;padding-right:16", false},
		{"unit separated by whitespace", "16px", "padding-top:8 px;padding-bottom:8px;padding-left:16px;padding-right:16px", false},
		{"trailing decimal point", "16px", "padding:8.px 16.px", false},
		{"negative side", "16px", "padding:8px 16px -8px 16px", false},
		{"extra shorthand values", "16px", "padding:8px 16px 8px 16px 24px", false},
		{"unknown percentage", "16px", "padding:8% 16%", false},
		{"unresolved root font", "8px", "padding:.5rem 1rem", false},
		{"unresolved logical padding", "16px", "padding:8px 16px;padding-inline:0", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := `<table width="600"><tr><td style="padding:24px;background:#eef;font-size:` + tc.font + `"><p>Here is the primary authored project message.</p><a href="https://example.test/open" style="background:#176;` + tc.padding + `">Open project</a></td></tr></table>`
			a := AnalyseOriginal(raw)
			wantScore := 1
			if tc.original {
				wantScore = 2
			}
			if a.DefaultsToOriginal != tc.original || a.Score != wantScore {
				t.Fatalf("want original=%v score=%d; got %+v", tc.original, wantScore, a)
			}
		})
	}
}

func TestOriginalAnalysisFluidGutters(t *testing.T) {
	prose := `<p>` + strings.Repeat("Substantive primary authored content. ", 12) + `</p>`
	heading := `<p style="font-size:24px">Project overview</p>`
	row := func(content string) string {
		return `<tr><td width="16">&nbsp;</td><td>` + content + `</td><td width="16">&nbsp;</td></tr>`
	}
	page := func(rows string) string {
		return `<table align="center"><tr><td><table width="100%">` + rows + `</table></td></tr></table>`
	}
	rows := row(heading+prose) + row(prose)
	fluid := page(rows)
	data := `<table><tr><th>Item</th><th>Total</th></tr><tr><td>One</td><td>12</td></tr></table>`
	for _, tc := range []struct {
		name, raw string
		score     int
		original  bool
	}{
		{"heading independent of gutters", fluid, 2, true},
		{"neutral wrappers", `<div><section>` + fluid + `</section></div>`, 2, true},
		{"own centering", `<table width="100%" style="margin:0 auto">` + rows + `</table>`, 2, true},
		{"CSS gutter widths", strings.ReplaceAll(fluid, `width="16"`, `style="width:16px"`), 2, true},
		{"anonymous blank gutter cells", strings.ReplaceAll(fluid, `width="16"`, `width="16" style="display:block;width:16px"`), 2, true},
		{"compatible gutter limits", strings.ReplaceAll(fluid, `width="16"`, `width="16" style="min-width:12px;max-width:48px;padding:0"`), 2, true},
		{"full width empty spacers", page(`<tr><td colspan="3" height="20">&nbsp;</td></tr>` + rows + `<tr><td colspan="3"></td></tr>`), 2, true},
		{"explicit table displays", strings.ReplaceAll(strings.ReplaceAll(fluid, `<table width="100%">`, `<table width="100%" style="display:table">`), `<tr>`, `<tr style="display:table-row">`), 2, true},
		{"independent action", page(row(`<a href="https://example.test/open" style="background:#176;padding:10px 28px">Open project</a>`+prose) + row(prose)), 2, true},
		{"independent modules", page(row(`<div style="padding:16px;border:1px solid #ddd">`+prose+`</div>`) + row(`<div style="padding:16px;border:1px solid #ddd">`+prose+`</div>`)), 2, true},
		{"no independent family", page(row(prose) + row(prose)), 1, false},
		{"unknown percentage image", page(row(`<img width="100%" height="90" src="https://example.test/banner">`) + row(prose)), 1, false},
		{"full width tracker", page(row(`<img width="100%" height="1" src="https://example.test/pixel">`+prose) + row(prose)), 1, false},
		{"heading cannot replace gutters", `<table width="100%" align="center"><tr><td>` + heading + prose + `</td></tr><tr><td>` + prose + `</td></tr></table>`, 0, false},
		{"one guttered row", page(row(heading + prose)), 0, false},
		{"empty repetitions", page(row(heading+prose) + row("&nbsp;") + row("")), 0, false},
		{"not centered", strings.ReplaceAll(fluid, `align="center"`, ""), 0, false},
		{"auto width alone", strings.ReplaceAll(fluid, `width="100%"`, `width="auto"`), 0, false},
		{"oversize ceiling unchanged", `<table width="100%" align="center" style="max-width:1400px"><tr><td>` + heading + prose + `</td></tr></table>`, 0, false},
		{"unequal gutters", strings.Replace(fluid, `width="16"`, `width="24"`, 1), 0, false},
		{"nonrepeating gutters", page(row(heading+prose) + strings.ReplaceAll(row(prose), `width="16"`, `width="24"`)), 0, false},
		{"percentage gutters", strings.ReplaceAll(fluid, `width="16"`, `width="16%"`), 0, false},
		{"empty row spanning fourth column", page(`<tr><td rowspan="3" width="300"></td><td colspan="3"></td></tr>` + rows), 0, false},
		{"empty row span to end", page(`<tr><td rowspan="0" width="300"></td><td colspan="3"></td></tr>` + rows), 0, false},
		{"empty fourth column", page(`<tr><td colspan="4"></td></tr>` + rows), 0, false},
		{"empty extra cell", page(`<tr><td></td><td></td><td></td><td width="300"></td></tr>` + rows), 0, false},
		{"empty gutter size contradiction", page(strings.Replace(row(""), `width="16"`, `width="300"`, 1) + rows), 0, false},
		{"gutter minimum contradicts width", strings.ReplaceAll(fluid, `width="16"`, `width="16" style="min-width:300px"`), 0, false},
		{"anonymous gutter minimum contradicts width", strings.ReplaceAll(fluid, `width="16"`, `width="16" style="display:block;min-width:300px"`), 0, false},
		{"gutter maximum contradicts width", strings.ReplaceAll(fluid, `width="16"`, `width="16" style="max-width:8px"`), 0, false},
		{"unknown gutter minimum", strings.ReplaceAll(fluid, `width="16"`, `width="16" style="min-width:50%"`), 0, false},
		{"CSS width overrides attribute", strings.ReplaceAll(fluid, `width="16"`, `width="16" style="width:300px"`), 0, false},
		{"invalid CSS gutter width", strings.ReplaceAll(fluid, `width="16"`, `width="16" style="width:16"`), 0, false},
		{"gutter shorthand padding", strings.ReplaceAll(fluid, `width="16"`, `width="16" style="padding:0 300px"`), 0, false},
		{"gutter longhand padding", strings.ReplaceAll(fluid, `width="16"`, `width="16" style="padding-left:300px"`), 0, false},
		{"anonymous gutter padding", strings.ReplaceAll(fluid, `width="16"`, `width="16" style="display:block;padding:0 300px"`), 0, false},
		{"table cell padding", strings.Replace(fluid, `<table width="100%">`, `<table width="100%" cellpadding="300">`, 1), 0, false},
		{"block table", strings.Replace(fluid, `<table width="100%">`, `<table width="100%" style="display:block">`, 1), 0, false},
		{"grid table", strings.Replace(fluid, `<table width="100%">`, `<table width="100%" style="display:grid">`, 1), 0, false},
		{"block rows", strings.ReplaceAll(fluid, `<tr>`, `<tr style="display:block">`), 0, false},
		{"block empty spacer row", page(`<tr style="display:block"><td colspan="3"></td></tr>` + rows), 0, false},
		{"block spanning spacer cell", page(`<tr><td colspan="3" style="display:block;width:300px"></td></tr>` + rows), 0, false},
		{"block row group", strings.Replace(fluid, `<table width="100%">`, `<table width="100%"><tbody style="display:block">`, 1), 0, false},
		{"block content cells", strings.ReplaceAll(fluid, `<td><p`, `<td style="display:block"><p`), 0, false},
		{"tiny gutters", strings.ReplaceAll(fluid, `width="16"`, `width="1"`), 0, false},
		{"populated side column", strings.Replace(fluid, "&nbsp;", "Sidebar content", 1), 0, false},
		{"side image column", strings.Replace(fluid, "&nbsp;", `<img width="20" height="20" src="https://example.test/icon">`, 1), 0, false},
		{"spanning content", strings.Replace(fluid, `<td><p`, `<td colspan="2"><p`, 1), 0, false},
		{"extra unaligned content row", page(rows + `<tr><td colspan="3">Additional content</td></tr>`), 0, false},
		{"unrelated centered ancestor", `<table align="center"><tr><td>Separate content</td><td><table width="100%">` + rows + `</table></td></tr></table>`, 0, false},
		{"nested rows cannot pool", page(row(heading + prose + `<table>` + row(prose) + `</table>`)), 0, false},
		{"centered correspondence with boxed aside", `<table align="center" width="100%"><tr><td>` + heading + prose + `<div style="padding:16px;border:1px solid #ddd">Short boxed aside.</div>` + prose + `</td></tr></table>`, 0, false},
		{"single boxed aside", page(row(prose+`<div style="padding:16px;border:1px solid #ddd">Short boxed aside.</div>`) + row(prose)), 1, false},
		{"ordinary data in gutters", page(row(data+prose) + row(data+prose)), 1, false},
		{"disconnected heading and action", page(row(prose)+row(prose)) + heading + `<a href="https://example.test/open" style="background:#176;padding:10px 28px">Open project</a>`, 1, false},
		{"small late fluid card", `<p>` + strings.Repeat("Ordinary correspondence. ", 300) + `</p>` + fluid, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := AnalyseOriginal(tc.raw)
			if a.DefaultsToOriginal != tc.original || a.Score != tc.score {
				t.Fatalf("want original=%v score=%d; got %+v", tc.original, tc.score, a)
			}
		})
	}
}
