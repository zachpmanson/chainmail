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

// The quoted message that triggered issue #33 is a styled table: column labels,
// comments, a signature and an old header block, most of them coloured by the
// sender's own formatting (grey signature lines, border-coloured cells, blue
// links). The colour detector must not read those as inline replies, or a
// single email shreds into dozens of fake messages attributed to the person
// who never wrote them.

// "color:" inside "border-color:" is layout, not a reply. This is the bug that
// turned every table cell of a whitespace table into its own message.
func TestInlineColourIsNotABorderOrBackgroundColour(t *testing.T) {
	raw := `<table><tr><td style="width: 197.95pt; border-color: rgb(204, 204, 204);">` +
		`Property Name</td></tr></table>`
	if runs := InlineRuns(raw); len(runs) != 0 {
		t.Fatalf("colour runs = %d, want 0 (a border-colour is not a text run): %v", len(runs), runs)
	}
}

// A table cell label is data, not prose; even a genuinely foreground-coloured
// cell must not become a reply.
func TestInlineRunIgnoresATableCell(t *testing.T) {
	raw := `<table><tr>` +
		`<td style="color: red; width: 100pt;">ATS Member</td>` +
		`</tr></table>`
	if runs := InlineRuns(raw); len(runs) != 0 {
		t.Fatalf("colour runs = %d, want 0 (a table cell is not an inline reply): %v", len(runs), runs)
	}
}

// A grey signature contact line is styled quoted text, not an answer.
func TestInlineRunIgnoresASignatureContactLine(t *testing.T) {
	raw := `<span style="color: gray;">DDI 03 307 5178 | M 027 722 7070 | E Al@example.com</span>`
	if runs := InlineRuns(raw); len(runs) != 0 {
		t.Fatalf("colour runs = %d, want 0 (a contact line is not an inline reply): %v", len(runs), runs)
	}
	raw = `<span style="color: gray;">Cloud IT Operations Manager</span>`
	if runs := InlineRuns(raw); len(runs) != 0 {
		t.Fatalf("colour runs = %d, want 0 (a job title is not an inline reply): %v", len(runs), runs)
	}
}

// A blue link's colour is link styling, not the quoter's answer.
func TestInlineRunIgnoresALinkChrome(t *testing.T) {
	raw := `<a href="https://example.com/spreadsheet" style="color: blue; text-decoration: underline;">in this sheet</a>`
	if runs := InlineRuns(raw); len(runs) != 0 {
		t.Fatalf("colour runs = %d, want 0 (a hyperlink is not an inline reply): %v", len(runs), runs)
	}
}

// The quoter's red answer survives even when the same quoted body carries the
// styled table and signature that made issue #33 — the reply is a sentence, the
// rest is formatting.
func TestInlineRunsStillFindsARealAnswerNextToStyledChrome(t *testing.T) {
	raw := `<blockquote>` +
		`<table><tr><td style="border-color: rgb(204,204,204);">ATS Number</td></tr></table>` +
		`<p>Can you send the meter number? ` +
		`<span style="color: red">Yes, I will send it over.</span></p>` +
		`</blockquote>`
	runs := InlineRuns(raw)
	if len(runs) != 1 {
		t.Fatalf("colour runs = %d, want the one real answer: %v", len(runs), runs)
	}
	if runs[0].Text != "Yes, I will send it over." {
		t.Errorf("run text = %q, want %q", runs[0].Text, "Yes, I will send it over.")
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
