package spec

import (
	"regexp"
	"strings"
	"testing"
)

var reAnyTag = regexp.MustCompile(`(?i)<\s*/?\s*([a-z0-9]+)([^>]*)>`)

// body reaches dangerouslySetInnerHTML, so the ONLY tags in a rendered body may
// be ones this package writes. Asserting on substrings is not enough: escaped
// text legitimately contains the word "onerror", and the question is whether it
// is an attribute or a character.
func TestOnlyOurOwnTagsSurviveHostileInput(t *testing.T) {
	allowed := map[string]bool{"p": true, "br": true, "blockquote": true, "a": true}
	attacks := []string{
		`<script>alert(1)</script>`,
		`<img src=x onerror=alert(1)>`,
		`<a href="javascript:alert(1)">x</a>`,
		`javascript:alert(1)`,
		`<iframe src="https://evil.example"></iframe>`,
		`</p><script>alert(1)</script><p>`,
		`<svg/onload=alert(1)>`,
		`data:text/html;base64,PHNjcmlwdD4=`,
		`<a href="http://ok.example" onmouseover="alert(1)">y</a>`,
		`vbscript:msgbox(1)`,
		`<style>*{display:none}</style>`,
		"<!--<script>alert(1)</script>-->",
		`http://ok.example/"><script>alert(1)</script>`,
		`<base href="https://evil.example/">`,
		`<form action=x><input name=p></form>`,
	}
	for _, a := range attacks {
		got := renderText(a, bodyStyle{reflow: true})
		for _, m := range reAnyTag.FindAllStringSubmatch(got, -1) {
			tag, attrs := strings.ToLower(m[1]), strings.ToLower(m[2])
			if !allowed[tag] {
				t.Errorf("input %q produced disallowed tag <%s>\n  in %q", a, tag, got)
				continue
			}
			// No event handlers, and an anchor may only point at http(s).
			if strings.Contains(attrs, " on") {
				t.Errorf("input %q produced <%s> with an event handler: %q", a, tag, m[0])
			}
			if tag == "a" && strings.Contains(attrs, "href=") {
				if !strings.Contains(attrs, `href="http://`) && !strings.Contains(attrs, `href="https://`) {
					t.Errorf("input %q produced a non-http anchor: %q", a, m[0])
				}
			}
		}
	}
}
