package spec

import (
	"strings"
	"testing"
)

func TestOriginalAnalysisReviewRegressions(t *testing.T) {
	ordinary := strings.Repeat("Ordinary correspondence with substantive details and no main design. ", 100)
	for _, tc := range []struct{ name, raw string }{
		{"pooled footer heading", `<div style="width:600px"><p>` + ordinary + `</p><footer><h3>Contact details</h3></footer></div>`},
		{"pooled footer action", `<div style="width:600px"><p>` + ordinary + `</p><footer><a href="https://example.test/profile" style="display:block;padding:12px;background:#ddd">View profile</a></footer></div>`},
		{"unmarked late footer control", `<div style="width:600px;margin:0 auto"><p>` + ordinary + `</p><div><a href="https://example.test/profile" style="display:block;padding:12px;background:#ddd">View profile</a></div></div>`},
		{"transparent decoration", `<div style="padding:24px;background:transparent"><p>Ordinary correspondence.</p><a href="https://example.test/profile" style="display:block;padding:12px;background:rgba(0,0,0,0)">View profile</a></div>`},
		{"zero border decoration", `<div style="padding:24px;border:0px solid #ddd"><p>Ordinary correspondence.</p><a href="https://example.test/profile" style="display:block;padding:12px;border:0px solid #ddd">View profile</a></div>`},
		{"empty flex", `<div style="width:600px"><p>` + ordinary + `</p><div style="display:flex"></div></div>`},
		{"single item flex", `<div style="width:600px;display:flex"><p>` + ordinary + `</p></div>`},
		{"empty grid", `<div style="width:600px"><p>` + ordinary + `</p><div style="display:grid;grid-template-columns:1fr 1fr"></div></div>`},
		{"single item grid", `<div style="width:600px;display:grid;grid-template-columns:1fr 1fr"><p>` + ordinary + `</p></div>`},
		{"hidden body", `<body style="display:none">` + designedOriginal + `</body>`},
		{"hidden html", `<html style="display:none"><body>` + designedOriginal + `</body></html>`},
		{"opacity decimal", `<div style="opacity:0.0">` + designedOriginal + `</div><p>Hello</p>`},
		{"opacity percent", `<div style="opacity:0%">` + designedOriginal + `</div><p>Hello</p>`},
		{"important hidden", `<div style="display:none!important;display:block">` + designedOriginal + `</div><p>Hello</p>`},
		{"zero font decimal", `<div style="font-size:0.0px">` + designedOriginal + `</div><p>Hello</p>`},
		{"zero image height decimal", `<div style="width:600px"><p>Ordinary authored message without design</p><img width="400" style="height:0.0px" src="https://example.test/image"></div>`},
		{"empty heading", `<div style="width:600px"><h1></h1><p>Ordinary prose</p></div>`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if a := AnalyseOriginal(tc.raw); a.DefaultsToOriginal {
				t.Fatalf("expected simplified, got %+v", a)
			}
		})
	}
}

func TestOriginalAnalysisContactFlowEquivalence(t *testing.T) {
	for _, tag := range []string{"p", "div"} {
		for _, href := range []string{"mailto:person@example.test", "tel:+1234567"} {
			raw := `<div style="width:600px"><p>Yes, that works.</p><` + tag + ` style="font-size:24px">Alex Example<br>Company and contact information<br><img width="100" height="80" src="https://example.test/logo"><br><a href="` + href + `">Contact me</a></` + tag + `></div>`
			if a := AnalyseOriginal(raw); a.DefaultsToOriginal {
				t.Errorf("%s/%s contact suffix influenced default: %+v", tag, href, a)
			}
		}
	}
}

func TestOriginalAnalysisPositiveFlowEquivalence(t *testing.T) {
	var score int
	for i, content := range []string{
		`<p style="font-size:24px">Project update</p><p>Here is the work we completed this week and the plan for next week.</p>`,
		`<div style="font-size:24px">Project update</div><div>Here is the work we completed this week and the plan for next week.</div>`,
		`<span style="font-size:24px">Project update</span><br><br>Here is the work we completed this week and the plan for next week.`,
	} {
		a := AnalyseOriginal(`<div style="width:600px">` + content + `</div>`)
		if i == 0 {
			score = a.Score
		}
		if !a.DefaultsToOriginal || a.Score != score {
			t.Errorf("equivalent flow %d differs: %+v", i, a)
		}
	}
}

func TestOriginalAnalysisImageColumnGeometry(t *testing.T) {
	for _, tc := range []struct {
		name, raw string
		original  bool
	}{
		{"known column", `<div style="max-width:520px;margin:0 auto;padding:24px;background:#eee"><p>This primary message has a full-width banner in its explicit column.</p><img style="width:100%;max-width:100%;height:auto" src="https://example.test/banner"></div>`, true},
		{"percentage scaffold", `<div style="max-width:520px;margin:0 auto;padding:24px;background:#eee"><table width="100%"><tr><td><p>This primary message has a banner below the ordinary prose.</p><img style="width:100%;height:auto" src="https://example.test/banner"></td></tr></table></div>`, true},
		{"unknown parent", `<div style="padding:24px;background:#eee"><p>The containing width is not known from this markup.</p><img style="width:100%;height:auto" src="https://example.test/banner"></div>`, false},
		{"unknown intermediate", `<div style="width:520px;padding:24px;background:#eee"><div style="width:calc(100% - 300px)"><p>The intermediate geometry is not resolved.</p><img style="width:100%;height:auto" src="https://example.test/banner"></div></div>`, false},
		{"unknown table cells", `<div style="width:520px;padding:24px;background:#eee"><table width="100%"><tr><td>First column</td><td><p>The second column has no explicit width.</p><img style="width:100%;height:auto" src="https://example.test/banner"></td></tr></table></div>`, false},
		{"maximum image width", `<div style="width:520px;padding:24px;background:#eee"><p>The image is an explicitly small logo, not a banner.</p><img style="width:100%;max-width:120px;height:90px" src="https://example.test/logo"></div>`, false},
		{"zero width", `<div style="width:520px;padding:24px;background:#eee"><p>The zero-width image must not count.</p><img style="width:0%;height:90px" src="https://example.test/pixel"></div>`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if a := AnalyseOriginal(tc.raw); a.DefaultsToOriginal != tc.original {
				t.Fatalf("want original=%v; got %+v", tc.original, a)
			}
		})
	}
}

func TestOriginalAnalysisSecondaryTextAndAlignedPanels(t *testing.T) {
	main := `<div style="max-width:480px;margin:0 auto;padding:24px;background:#eee;font-size:14px"><h2>Project overview</h2><p>` + strings.Repeat("Substantive content. ", 15) + `</p></div>`
	footer := `<div style="font-size:10px">` + strings.Repeat("Footer information. ", 32) + `</div>`
	if a := AnalyseOriginal(main + footer); !a.DefaultsToOriginal {
		t.Fatalf("secondary fine print outweighed the designed primary content: %+v", a)
	}
	page := `<div style="max-width:480px;margin:0 auto"><p>First substantive section of the primary authored content.</p></div>` +
		`<div style="max-width:480px;margin:0 auto"><a href="https://example.test/open" style="display:inline-block;padding:12px;background:#123">Open project</a></div>` +
		`<div style="max-width:480px;margin:0 auto"><p>` + strings.Repeat("Further substantive content. ", 20) + `</p></div>`
	if a := AnalyseOriginal(page); !a.DefaultsToOriginal {
		t.Fatalf("same-column content and action should form one primary page: %+v", a)
	}
	unaligned := strings.ReplaceAll(page, "margin:0 auto", "margin:0")
	if a := AnalyseOriginal(unaligned); a.DefaultsToOriginal {
		t.Fatalf("disconnected partial regions pooled their evidence: %+v", a)
	}
}

func TestOriginalAnalysisResourceLimits(t *testing.T) {
	unit := strings.Repeat(`<div style="padding:12px;background:#eee">`, 400) + `Plain text` + strings.Repeat(`</div>`, 400)
	for _, raw := range []string{
		strings.Repeat(unit, 20),
		strings.Repeat(`<i>word</i>`, analysisMaxNodes),
		strings.Repeat("x", analysisMaxBytes+1),
	} {
		a := AnalyseOriginal(raw)
		if !a.Uncertain || a.DefaultsToOriginal || a.Score != 0 {
			t.Fatalf("complexity limit must decline conservatively: %+v", a)
		}
	}
}

func BenchmarkOriginalAnalysisNestedWrappers(b *testing.B) {
	unit := strings.Repeat(`<div style="padding:12px;background:#eee">`, 400) + `Plain text` + strings.Repeat(`</div>`, 400)
	raw := strings.Repeat(unit, 20)
	b.SetBytes(int64(len(raw)))
	for b.Loop() {
		AnalyseOriginal(raw)
	}
}
