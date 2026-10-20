package spec

import (
	"strings"
	"testing"
)

func TestOriginalStyleAttributesCannotCreateElements(t *testing.T) {
	for _, attribute := range []string{
		`style="color:red}</style><span data-extra='yes'>not markup</span><style>"`,
		`background="https://example.test/</style><span>not-markup</span><style>"`,
	} {
		got, ok := OriginalBody(`<body ` + attribute + `><p>visible</p></body>`)
		if !ok || !strings.Contains(got, "visible") {
			t.Fatalf("content disappeared: %s", got)
		}
		if strings.Contains(got, "<span") {
			t.Fatalf("attribute escaped its stylesheet: %s", got)
		}
		if strings.Count(got, "<style>") != strings.Count(got, "</style>") {
			t.Fatalf("attribute changed stylesheet boundaries: %s", got)
		}
	}
}

func TestOriginalKeepsLegacyStylesheetCommentWrappers(t *testing.T) {
	got, ok := OriginalBody(`<style><!-- body{color:red} --></style><p>visible</p>`)
	if !ok || !strings.Contains(got, `<!-- :host{color:red}`) || strings.Contains(got, `\3c !--`) {
		t.Fatalf("legacy CSS wrapper stopped being a comment delimiter: %s", got)
	}
}

func TestOriginalHandlesEscapedAndTruncatedSelectors(t *testing.T) {
	for _, css := range []string{
		`.foo\{color:red}`, `.foo\`, `.foo\{ {color:red}`, `.a\,b body {color:red}`,
		`p[x="truncated\`, `p{content:"truncated\`,
		`p:not( {color:red}`, `p:not(() {color:red}`, `[data-x="a\\"] {color:red}`,
	} {
		got, ok := OriginalBody(`<style>` + css + `</style><p>visible</p>`)
		if !ok || !strings.Contains(got, "visible") {
			t.Fatalf("selector %q lost content: %s", css, got)
		}
	}
}

func FuzzOriginalBodyDoesNotPanic(f *testing.F) {
	for _, raw := range []string{
		`<style>.foo\{color:red}</style><p>visible</p>`,
		`<style>:not((){}</style><p>visible</p>`,
		`<body style="color:red">visible</body>`,
	} {
		f.Add(raw)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		if len(raw) > 16<<10 {
			t.Skip()
		}
		OriginalBody(raw)
	})
}
