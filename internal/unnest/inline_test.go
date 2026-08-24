package unnest

import (
	"strings"
	"testing"
)

// A colour-marked run inside a quoted body is the shape issue #29 describes:
// an inline answer typed in red into the text being quoted. It must survive as
// a run whose text is the answer, not be swallowed into the surrounding quote.
func TestInlineRunsFindsARedAnswerInsideAQuote(t *testing.T) {
	html := `<p>Hi,<br>` +
		`<blockquote>Can you send the meter number? ` +
		`<span style="color:red">Yes, I will send it over.</span>` +
		`</blockquote></p>`
	runs := InlineRuns(html)
	if len(runs) != 1 {
		t.Fatalf("colour runs = %d, want 1: %v", len(runs), runs)
	}
	if runs[0].Text != "Yes, I will send it over." {
		t.Errorf("run text = %q, want %q", runs[0].Text, "Yes, I will send it over.")
	}
}

// A nested colour span carries the same answer's text, so it must not become a
// second run — the outer span is the answer, the inner span is the same words.
func TestInlineRunDoesNotDoubleCountNestedColour(t *testing.T) {
	raw := `<span style="color:red">Yes <b style="color:rgb(255,0,0)">I will</b> send it.</span>`
	runs := InlineRuns(raw)
	if len(runs) != 1 {
		t.Fatalf("colour runs = %d, want 1 (nested span is the same answer): %v", len(runs), runs)
	}
}

// A single colour word is not an answer; the floor exists so a lone red "Done!"
// is not lifted out of the text it belongs to.
func TestInlineRunIgnoresASingleColouredWord(t *testing.T) {
	raw := `<p>Review the tariff sheet <span style="color:red">now</span> before close.</p>`
	if runs := InlineRuns(raw); len(runs) != 0 {
		t.Fatalf("colour runs = %d, want 0 (a one-word run is not an answer)", len(runs))
	}
}

// A whole quoted paragraph marked in colour is the sender's own formatting, not
// an interjection, and must not become a run.
func TestInlineRunIgnoresAFullColouredPassage(t *testing.T) {
	long := strings.Repeat("The tariff sheet was revised again on Monday and the fresh copy is "+
		"already with the retailer for comment before the tender pack goes out, so ", 6) +
		"and that is more than a paragraph of prose."
	raw := `<p style="color: blue">` + long + `</p>`
	if runs := InlineRuns(raw); len(runs) != 0 {
		t.Fatalf("colour runs = %d, want 0 (a long coloured paragraph is not an inline reply)", len(runs))
	}
}
