package spec

import (
	"strings"
	"testing"
)

// renderMarkup is htmlBody for a part with no boilerplate detected. The fold
// tests call htmlBody directly with the block they are about.
func renderMarkup(raw string, st bodyStyle) string {
	s, _ := htmlBody(raw, st, bodyFold{})
	return s
}

// Every part in this file is invented. The shapes are the ones real clients
// emit — a Gmail blockquote, an Outlook header block written one paragraph per
// key, a Word stylesheet in the middle of the body — but no line of real
// correspondence is committed.

func TestOwnMarkupIsPreferredToTheTextRendition(t *testing.T) {
	// The whole point: a link's target exists in the markup and nowhere in the
	// text, so a body derived from text cannot be clicked.
	r := &entryRow{
		Source:   "mail",
		Direct:   true,
		BodyText: "Signed off on this review.",
		BodyHTML: `<div>Signed off on <a href="https://loomworks.example/rate-review?id=2547678">this review</a>.</div>`,
	}
	got := bodyHTML(r)
	if !strings.Contains(got, `href="https://loomworks.example/rate-review?id=2547678"`) {
		t.Errorf("body = %q, want the sender's own link target", got)
	}
}

func TestAnEntryWithNoMarkupFallsBackToTheTextPath(t *testing.T) {
	// 41 of 57 entries on a real page were mined out of quoted text and have no
	// part of their own. The fallback is required, not a nicety.
	r := &entryRow{Source: "mail", Direct: true, BodyText: "First point.\n\nSecond point."}
	if got := bodyHTML(r); got != "<p>First point.</p><p>Second point.</p>" {
		t.Errorf("body = %q, want the text path's paragraphs", got)
	}
}

func TestMarkupThatSaysNothingFallsBackToTheText(t *testing.T) {
	// A part that survives as tags with no words in it is not a reason to blank
	// an entry whose text is right there.
	r := &entryRow{
		Source:   "mail",
		Direct:   true,
		BodyText: "Numbers attached.",
		BodyHTML: `<html><head><style>p{color:#000}</style></head><body><div><br></div></body></html>`,
	}
	if got := bodyHTML(r); got != "<p>Numbers attached.</p>" {
		t.Errorf("body = %q, want the text path", got)
	}
}

func TestChatKeepsTheTextPath(t *testing.T) {
	// Slack entries carry no html part, and their newlines are keys someone
	// pressed. Nothing here changes that.
	r := &entryRow{Source: "slack", Direct: true,
		BodyText: "pushed the fix for the demand guesser to the branch just now\nwill deploy after lunch"}
	got := bodyHTML(r)
	if !strings.Contains(got, "just now<br>will deploy") {
		t.Errorf("body = %q, want the author's own break kept", got)
	}
}

func TestGmailQuoteIsPeeled(t *testing.T) {
	part := `<div dir="ltr">Looking into it now.</div>` +
		`<div class="gmail_quote">` +
		`<div class="gmail_attr">On Thu, 7 May 2026 at 04:38, Ada Byron &lt;ada@loomworks.example&gt; wrote:</div>` +
		`<blockquote class="gmail_quote"><div>Has the review finished?</div></blockquote>` +
		`</div>`
	got := renderMarkup(part, mailBody)
	if !strings.Contains(got, "Looking into it now.") {
		t.Errorf("body = %q, want the sender's own text", got)
	}
	if strings.Contains(got, "Has the review finished?") || strings.Contains(got, "wrote:") {
		t.Errorf("body = %q, want the quoted history peeled", got)
	}
}

func TestAppleCiteQuoteIsPeeled(t *testing.T) {
	// Apple Mail names nothing but type="cite", and puts the attribution in a
	// bare div above it.
	part := `<div>Agreed.</div><div><br></div>` +
		`<div>On 7 May 2026, at 04:38, Ada Byron &lt;ada@loomworks.example&gt; wrote:</div>` +
		`<blockquote type="cite"><div>Has the review finished?</div></blockquote>`
	got := renderMarkup(part, mailBody)
	if !strings.Contains(got, "Agreed.") {
		t.Errorf("body = %q, want the sender's own text", got)
	}
	if strings.Contains(got, "Has the review") || strings.Contains(got, "wrote:") {
		t.Errorf("body = %q, want the quote and its attribution peeled", got)
	}
}

func TestOutlookHeaderBlockIsPeeledEvenSpreadAcrossSiblings(t *testing.T) {
	// The flavour nothing wraps: Outlook writes the separator, then one
	// paragraph per header key, then the quoted message as further siblings. No
	// single element holds two keys, so only reading the siblings as lines finds
	// the boundary at all.
	part := `<div><p>Approved, thanks.</p>` +
		`<div id="divRplyFwdMsg"><p><b>From:</b> Ada Byron</p></div>` +
		`<p><b>Sent:</b> Thursday, 7 May 2026 04:38</p>` +
		`<p><b>To:</b> ops@loomworks.example</p>` +
		`<p><b>Subject:</b> Re: cutover</p>` +
		`<p>Has the review finished?</p></div>`
	got := renderMarkup(part, mailBody)
	if !strings.Contains(got, "Approved, thanks.") {
		t.Errorf("body = %q, want the sender's own text", got)
	}
	for _, gone := range []string{"Has the review", "Sent:", "Subject:"} {
		if strings.Contains(got, gone) {
			t.Errorf("body = %q, want %q peeled with the rest of the trail", got, gone)
		}
	}
}

func TestOutlookHeaderBlockWithoutAContainerIsPeeled(t *testing.T) {
	// Same flavour with no divRplyFwdMsg to anchor on: the header block is the
	// only boundary there is.
	part := `<div><p>Approved.</p><p>&nbsp;</p>` +
		`<p><b>From:</b> Ada Byron &lt;ada@loomworks.example&gt;</p>` +
		`<p><b>Sent:</b> Thursday, 7 May 2026 04:38</p>` +
		`<p>Has the review finished?</p></div>`
	got := renderMarkup(part, mailBody)
	if !strings.Contains(got, "Approved.") || strings.Contains(got, "Has the review") {
		t.Errorf("body = %q, want everything from the header block onward peeled", got)
	}
}

func TestNestedQuotesGoWithTheOuterOne(t *testing.T) {
	// Nesting is explicit in markup, so the outer container is the cut: peeling
	// the inner one and keeping the outer would leave one level of the trail.
	part := `<div>Chasing this.</div>` +
		`<div class="gmail_quote"><div class="gmail_attr">On Thu, 7 May 2026 at 04:38, Ada wrote:</div>` +
		`<blockquote class="gmail_quote"><div>Any update?</div>` +
		`<div class="gmail_quote"><div class="gmail_attr">On Wed, 6 May 2026 at 09:00, Grace wrote:</div>` +
		`<blockquote class="gmail_quote"><div>Opened the ticket.</div></blockquote></div>` +
		`</blockquote></div>`
	got := renderMarkup(part, mailBody)
	if got != "<div>Chasing this.</div>" {
		t.Errorf("body = %q, want only the sender's own text", got)
	}
}

func TestAnUnspooledEntryIsNotPeeledAgain(t *testing.T) {
	// A quoted entry's stored body is already one peeled block, so peeling is off
	// for it — and would blank it, since its own text opens on what reads as a
	// header block.
	r := &entryRow{
		Source:   "mail",
		BodyText: "From: the vendor portal\nTo: whoever owns the account\n\nplease action.",
		BodyHTML: `<div><p>From: the vendor portal</p><p>To: whoever owns the account</p><p>please action.</p></div>`,
	}
	if got := bodyHTML(r); !strings.Contains(got, "please action.") {
		t.Errorf("body = %q, want the entry's own text kept", got)
	}
}

func TestAnInlinePullQuoteStaysOnThePage(t *testing.T) {
	// A blockquote with no attribution above it and no client class on it is the
	// sender quoting something into their own message. Treating every blockquote
	// as history would delete this and everything after it.
	part := `<div>Root cause from the pull request:</div>` +
		`<blockquote style="margin:0 0 0 40px"><div>the unit is converted twice</div></blockquote>` +
		`<div>Fixed on the branch.</div>`
	got := renderMarkup(part, mailBody)
	for _, want := range []string{"Root cause", "converted twice", "Fixed on the branch."} {
		if !strings.Contains(got, want) {
			t.Errorf("body = %q, want %q kept", got, want)
		}
	}
}

func TestDocumentChromeIsDropped(t *testing.T) {
	// A <style> is the sharp case: CSS has no scope, so a sender's stylesheet
	// restyles the whole timeline rather than their own bubble.
	part := `<html><head><meta charset="utf-8"><title>Message</title>` +
		`<style>.msg{display:none}</style><link rel="stylesheet" href="https://loomworks.example/m.css">` +
		`</head><body><!--[if mso]><p>Word chrome</p><![endif]-->` +
		`<script>alert(1)</script><p>Rates attached.</p></body></html>`
	got := renderMarkup(part, mailBody)
	if got != "<p>Rates attached.</p>" {
		t.Errorf("body = %q, want only the sender's paragraph", got)
	}
}

func TestPresentationIsDroppedAndEmphasisIsKept(t *testing.T) {
	part := `<table width="600" bgcolor="#ffffff"><tr>` +
		`<td style="color:#000000;width:300px;font-weight:bold" class="MsoNormal" id="c1">Peak</td>` +
		`<td><font color="#ff0000" face="Calibri">12.40 c/kWh</font></td></tr></table>`
	got := renderMarkup(part, mailBody)
	for _, gone := range []string{"color:#000000", "width:300px", `width="600"`, "bgcolor", "MsoNormal", `id="c1"`, "Calibri"} {
		if strings.Contains(got, gone) {
			t.Errorf("body = %q, want %q dropped: it asserts a colour or a size", got, gone)
		}
	}
	if !strings.Contains(got, "font-weight:bold") {
		t.Errorf("body = %q, want the emphasis the sender chose kept", got)
	}
	if !strings.Contains(got, "<table>") || !strings.Contains(got, "12.40 c/kWh") {
		t.Errorf("body = %q, want the table and its cells kept", got)
	}
}

func TestRemoteImagesAreKeptAndUnfetchableOnesAreNot(t *testing.T) {
	part := `<div><img src="https://loomworks.example/screenshot.png" width="480" alt="the offersheet">` +
		`<img src="cid:image001.png@01D9" alt="logo">Attached above.</div>`
	got := renderMarkup(part, mailBody)
	if !strings.Contains(got, `src="https://loomworks.example/screenshot.png"`) {
		t.Errorf("body = %q, want the remote image: it is content", got)
	}
	if !strings.Contains(got, `width="480"`) {
		t.Errorf("body = %q, want an image's own dimensions kept", got)
	}
	if strings.Contains(got, "cid:") {
		t.Errorf("body = %q, want the cid image dropped: the bytes are not in the corpus", got)
	}
}

func TestAClientMarkedSignatureBlockIsFoldedAndStillPresent(t *testing.T) {
	part := `<div>Numbers attached.</div>` +
		`<div class="gmail_signature"><div>Ada Byron<br>Loomworks</div></div>`
	got := renderMarkup(part, mailBody)
	if !strings.Contains(got, `<details class="sig">`) {
		t.Errorf("body = %q, want the signature behind a disclosure", got)
	}
	if !strings.Contains(got, "Ada Byron") {
		t.Errorf("body = %q, want the signature folded, not removed", got)
	}
	if !strings.Contains(got, "Numbers attached.") {
		t.Errorf("body = %q, want the sender's own text outside the fold", got)
	}
}

func TestTrailingSeparatorGoesWithTheQuoteItSeparated(t *testing.T) {
	part := `<div><p>Approved.</p><hr><div id="divRplyFwdMsg"><p>From: Ada</p><p>Sent: Thursday</p></div><p>Original question.</p></div>`
	got := renderMarkup(part, mailBody)
	if strings.Contains(got, "<hr") {
		t.Errorf("body = %q, want the rule dropped: it now separates nothing", got)
	}
}

func TestMalformedMarkupNeitherPanicsNorLeaksAnOpenTag(t *testing.T) {
	// A body can be arbitrary, and a truncated one ends mid-element. Every case
	// here is re-serialised from the parse tree, so what comes out is balanced
	// whatever went in — an unclosed <div> would otherwise swallow the rest of
	// the timeline.
	for _, part := range []string{
		`<div><p>Rates attached.`,
		`<table><tr><td>Peak`,
		`<div><b>unclosed <i>and nested`,
		`<<<>>> <div/><p>Rates attached.</p>`,
		`<div>Rates attached.</div><style>.msg{display:none`,
		strings.Repeat("<div>", 200) + "Rates attached.",
	} {
		got := renderMarkup(part, mailBody)
		if !strings.Contains(got, "Rates attached.") && !strings.Contains(got, "Peak") &&
			!strings.Contains(got, "unclosed") {
			t.Errorf("part %q -> %q, want the words that were in it", part, got)
		}
		if opens, closes := strings.Count(got, "<div"), strings.Count(got, "</div>"); opens != closes {
			t.Errorf("part %q -> %q, want %d closing div tags, got %d", part, got, opens, closes)
		}
		if strings.Count(got, "<table") != strings.Count(got, "</table>") {
			t.Errorf("part %q -> %q, want balanced table tags", part, got)
		}
	}
}

func TestAnEmptyPartIsNotABubble(t *testing.T) {
	for _, part := range []string{"", "   ", `<html><body></body></html>`, `<div>&nbsp;</div>`, `<div><br><br></div>`} {
		if got := renderMarkup(part, mailBody); got != "" {
			t.Errorf("htmlBody(%q) = %q, want the empty string", part, got)
		}
	}
}

func TestAReplyThatOnlyQuotesHasNoBody(t *testing.T) {
	// The sender wrote nothing above the quote. What they quoted is on the page
	// as its own entry.
	part := `<div class="gmail_quote"><div class="gmail_attr">On Thu, 7 May 2026 at 04:38, Ada wrote:</div>` +
		`<blockquote class="gmail_quote"><div>Has the review finished?</div></blockquote></div>`
	if got := renderMarkup(part, mailBody); got != "" {
		t.Errorf("body = %q, want empty", got)
	}
}
