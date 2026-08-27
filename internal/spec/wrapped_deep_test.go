package spec

import (
	"strings"
	"testing"
)

// The full Gmail shape for card 1: content block whose final div itself ends in
// a <br clear="all"/> before the wrapped signature fold. The trailing separator
// lives one level deeper than the direct-child fold case.
func TestTrailingDeepBlanksBeforeWrappedFoldAreTrimmed(t *testing.T) {
	part := `<div dir="ltr"><div>` +
		`<div>Hi team,</div>` +
		`<div><span>&lt;Screenshot 2026-08-26 at 8.52.12 am.png&gt;</span><br/><span style="text-align:center"><br/></span></div>` +
		`<br clear="all"/></div>` +
		`<div><div class="gmail_signature"><div>Ada<br>Loomworks</div></div></div></div>`
	got := renderMarkup(part, mailBody)
	if !strings.Contains(got, `<details class="sig">`) {
		t.Fatalf("body = %q, want the signature folded", got)
	}
	cut := strings.Index(got, "<details")
	exposed := got[:cut]
	if strings.Contains(exposed, `<br clear="all"/>`) {
		t.Errorf("body = %q, the <br clear=\"all\"/> deep in the content block must be trimmed", got)
	}
	if strings.Contains(exposed, `<span style="text-align:center"><br/></span>`) {
		t.Errorf("body = %q, want the nested blank after the screenshot trimmed", got)
	}
	if stripped := strings.TrimRight(exposed, " \t\n"); stripped != exposed {
		t.Errorf("body = %q, want no blank run before the wrapped fold", got)
	}
	if !strings.Contains(got, "Screenshot") {
		t.Errorf("body = %q, want the sender's own text kept", got)
	}
}
