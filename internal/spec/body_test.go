package spec

import (
	"strings"
	"testing"
)

// Every body in this file is invented. The shapes are real — a hard-wrapped
// paragraph, an address in angle brackets, an entity docket failed to decode —
// but no line of real correspondence is committed.

var mailBody = bodyStyle{peel: true, reflow: true}
var quotedMail = bodyStyle{reflow: true}
var chatBody = bodyStyle{}

func TestAngleBracketAddressSurvivesAsVisibleText(t *testing.T) {
	// The case that proves escaping runs before any markup is added: injected
	// raw, "<ada@loomworks.example>" is an unknown tag and the address vanishes
	// from the page entirely.
	got := textToHTML("Ping <ada@loomworks.example> about the cutover.", mailBody)
	want := "<p>Ping &lt;ada@loomworks.example&gt; about the cutover.</p>"
	if got != want {
		t.Errorf("body = %q, want %q", got, want)
	}
}

func TestMarkupInTheTextCannotBecomeMarkupOnThePage(t *testing.T) {
	got := textToHTML(`<script>alert(1)</script> and <b onmouseover="x">bold</b>`, mailBody)
	want := `<p>&lt;script&gt;alert(1)&lt;/script&gt; and ` +
		`&lt;b onmouseover=&#34;x&#34;&gt;bold&lt;/b&gt;</p>`
	if got != want {
		t.Errorf("body = %q,\nwant %q", got, want)
	}
}

func TestUndecodedEntityIsShownAsTheCharactersItIs(t *testing.T) {
	// docket leaves some entities undecoded. Decoding them here would be a guess
	// about someone else's bug; reproducing them is at least true, and the page
	// shows what the corpus holds.
	got := textToHTML("Loom &amp; Fjord, 10&nbsp;kW", mailBody)
	want := "<p>Loom &amp;amp; Fjord, 10&amp;nbsp;kW</p>"
	if got != want {
		t.Errorf("body = %q, want %q", got, want)
	}
}

func TestBlankLinesSeparateParagraphs(t *testing.T) {
	got := textToHTML("First point.\n\nSecond point.\n\n\nThird.", mailBody)
	want := "<p>First point.</p><p>Second point.</p><p>Third.</p>"
	if got != want {
		t.Errorf("body = %q, want %q", got, want)
	}
}

func TestWrappedLinesReflowButDeliberateBreaksDoNot(t *testing.T) {
	// The first two lines are a paragraph the sending client broke at 72
	// columns; the address block below it is layout the sender chose.
	body := "We have moved the cutover to the following Monday so that the meter\n" +
		"reads land inside the billing period.\n" +
		"\n" +
		"Loomworks Pty Ltd\n" +
		"14 Anvil Lane\n" +
		"Fjordvik"
	got := textToHTML(body, mailBody)
	want := "<p>We have moved the cutover to the following Monday so that the meter " +
		"reads land inside the billing period.</p>" +
		"<p>Loomworks Pty Ltd<br>14 Anvil Lane<br>Fjordvik</p>"
	if got != want {
		t.Errorf("body = %q,\nwant %q", got, want)
	}
}

func TestReflowLeavesColumnsAndIndentationAlone(t *testing.T) {
	// Both rows are past flowWidth, so length alone would join them and destroy
	// the column. Aligned whitespace is the veto.
	body := "Site 0000034582WE5C1   peak 12.40 c/kWh   shoulder 9.10 c/kWh   off 6.20\n" +
		"Site 0589236722LCF6D   peak 13.90 c/kWh   shoulder 9.80 c/kWh   off 6.55"
	got := textToHTML(body, mailBody)
	if !strings.Contains(got, "off 6.20<br>Site 0589236722LCF6D") {
		t.Errorf("body = %q, want the two rows kept on separate lines", got)
	}

	indented := "    if unit == \"$/kW\" {\n        return v / days\n    }"
	got = textToHTML(indented, mailBody)
	if strings.Count(got, "<br>") != 2 {
		t.Errorf("body = %q, want indented lines kept as written", got)
	}
}

func TestReflowNeverJoinsIntoABullet(t *testing.T) {
	body := "The remaining items before we can sign the offer off are as follows:\n" +
		"- confirm the demand class\n" +
		"- reissue the offersheet"
	got := textToHTML(body, mailBody)
	if strings.Count(got, "<br>") != 2 {
		t.Errorf("body = %q, want the bullets on their own lines", got)
	}
}

func TestChatNewlinesAreNeverReflowed(t *testing.T) {
	// A newline in a Slack message is a key someone pressed. The first line is
	// past flowWidth, so this is exactly where a mail body would have joined.
	body := "I have pushed the fix for the demand guesser to the branch just now\nwill deploy after lunch"
	got := textToHTML(body, chatBody)
	if !strings.Contains(got, "just now<br>will deploy") {
		t.Errorf("body = %q, want the author's own break kept", got)
	}
}

func TestQuotedHistoryIsPeeledFromAMailBodyButAnInlineQuoteIsKept(t *testing.T) {
	// The trailing history is not lost: unnest mined it into entries of its own,
	// which appear elsewhere on the same page. Repeating it under every reply is
	// what the transcript exists to undo.
	trail := "Looking into it now.\n" +
		"\n" +
		"> On 7 May 2026, at 04:38, Ada Byron <ada@loomworks.example> wrote:\n" +
		">\n" +
		"> Has the review finished?\n"
	got := textToHTML(trail, mailBody)
	if got != "<p>Looking into it now.</p>" {
		t.Errorf("body = %q, want only the sender's own text", got)
	}

	// An inline quote has no attribution above it, so it is the sender quoting
	// something into their own message and belongs on the page.
	// The quoted lines are wrapped too, so reflow has to reach inside a quote.
	inline := "Root cause from the pull request:\n" +
		"\n" +
		"> the unit is converted twice, once in the demand guesser and then once\n" +
		"> again in the price itself\n" +
		"\n" +
		"Fixed on the branch."
	got = textToHTML(inline, mailBody)
	want := "<p>Root cause from the pull request:</p>" +
		"<blockquote><p>the unit is converted twice, once in the demand guesser and then once " +
		"again in the price itself</p></blockquote>" +
		"<p>Fixed on the branch.</p>"
	if got != want {
		t.Errorf("body = %q,\nwant %q", got, want)
	}
}

func TestDeeperQuotesNest(t *testing.T) {
	got := textToHTML("Quoting a quote:\n\n> she said\n>> he said\n", mailBody)
	want := "<p>Quoting a quote:</p>" +
		"<blockquote><p>she said</p><blockquote><p>he said</p></blockquote></blockquote>"
	if got != want {
		t.Errorf("body = %q,\nwant %q", got, want)
	}
}

func TestAQuotedEntryKeepsItsOwnTextEvenWhenItLooksLikeAHeaderBlock(t *testing.T) {
	// A quoted entry's stored text is already one peeled block, so peeling is
	// off for it. Were it on, these two lines would read as a boundary and the
	// entry would render blank.
	body := "From: the vendor portal\nTo: whoever owns the account\n\nplease action."
	if got := textToHTML(body, quotedMail); !strings.Contains(got, "please action.") {
		t.Errorf("body = %q, want the text kept", got)
	}
}

func TestOnlyHTTPSchemesAreLinkified(t *testing.T) {
	got := textToHTML("See https://loomworks.example/plan and javascript:alert(1) now", mailBody)
	if !strings.Contains(got,
		`<a href="https://loomworks.example/plan" target="_blank" rel="noopener">https://loomworks.example/plan</a>`) {
		t.Errorf("body = %q, want the https url linked", got)
	}
	if strings.Contains(got, "javascript:alert(1)<") || strings.Contains(got, `href="javascript`) {
		t.Errorf("body = %q, must not link a javascript: url", got)
	}
	if !strings.Contains(got, "javascript:alert(1)") {
		t.Errorf("body = %q, want the javascript: text still shown", got)
	}
}

func TestLinkifyingCannotBreakOutOfTheHref(t *testing.T) {
	// A URL is matched from raw text, so anything that would end an attribute
	// has to be neutralised by escaping rather than by the match alone.
	got := textToHTML(`https://loomworks.example/a"onmouseover="alert(1)`, mailBody)
	if strings.Contains(got, `onmouseover="`) {
		t.Fatalf("body = %q, attribute escaped the href", got)
	}
	if strings.Count(got, `"`) != 6 { // href="…" target="_blank" rel="noopener"
		t.Errorf("body = %q, want exactly the three generated attributes", got)
	}
}

func TestURLPunctuationAndWrappersAreNotSwallowed(t *testing.T) {
	cases := map[string]string{
		"see https://loomworks.example/plan.":      "https://loomworks.example/plan",
		"see (https://loomworks.example/plan)":     "https://loomworks.example/plan",
		"see <https://loomworks.example/plan>":     "https://loomworks.example/plan",
		"see https://loomworks.example/a_(b) here": "https://loomworks.example/a_(b)",
	}
	for in, want := range cases {
		got := textToHTML(in, mailBody)
		if !strings.Contains(got, `href="`+want+`"`) {
			t.Errorf("%q -> %q, want href %q", in, got, want)
		}
	}
}

func TestSignatureIsTrimmedButSaidSo(t *testing.T) {
	body := "Numbers attached.\n--\nAda Byron\nLoomworks | +61 400 000 000\nhttps://loomworks.example"
	got := textToHTML(body, mailBody)
	if strings.Contains(got, "Ada Byron") {
		t.Errorf("body = %q, want the signature trimmed", got)
	}
	if !strings.Contains(got, `<p class="ed">[signature trimmed]</p>`) {
		t.Errorf("body = %q, want the trim declared rather than silent", got)
	}
}

func TestEmptyTextProducesAnEmptyBodyNotEmptyMarkup(t *testing.T) {
	for _, in := range []string{"", "   ", "\n\n \n"} {
		if got := textToHTML(in, mailBody); got != "" {
			t.Errorf("textToHTML(%q) = %q, want the empty string", in, got)
		}
	}
	// A reply that quotes and says nothing of its own has no text either: what
	// it quoted is on the page as its own entry.
	quoteOnly := "> On 7 May 2026, at 04:38, Ada Byron <ada@loomworks.example> wrote:\n> Has the review finished?\n"
	if got := textToHTML(quoteOnly, mailBody); got != "" {
		t.Errorf("body = %q, want empty: the sender wrote nothing above the quote", got)
	}
}

func TestStyleFollowsProvenance(t *testing.T) {
	cases := []struct {
		name string
		row  entryRow
		want bodyStyle
	}{
		{"mailbox mail reshapes fully", entryRow{Source: "mail", Direct: true}, bodyStyle{peel: true, reflow: true}},
		{"unspooled mail is already peeled", entryRow{Source: "mail"}, bodyStyle{reflow: true}},
		{"chat is left as written", entryRow{Source: "slack", Direct: true}, bodyStyle{}},
	}
	for _, c := range cases {
		if got := styleFor(&c.row); got != c.want {
			t.Errorf("%s: styleFor = %+v, want %+v", c.name, got, c.want)
		}
	}
}
