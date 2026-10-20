package spec

import (
	"strings"
	"testing"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
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
		doc, err := html.Parse(strings.NewReader(got))
		if err != nil {
			t.Fatal(err)
		}
		styles := 0
		var walk func(*html.Node)
		walk = func(n *html.Node) {
			if n.Type == html.ElementNode {
				if n.DataAtom == atom.Span {
					t.Fatalf("attribute escaped its stylesheet: %s", got)
				}
				if n.DataAtom == atom.Style {
					styles++
				}
			}
			for child := n.FirstChild; child != nil; child = child.NextSibling {
				walk(child)
			}
		}
		walk(doc)
		if styles != 2 {
			t.Fatalf("attribute changed stylesheet boundaries: styles=%d, %s", styles, got)
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
