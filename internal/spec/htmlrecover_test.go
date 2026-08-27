package spec

import (
	"strings"
	"testing"
)

// The hosts here are invented, in the two shapes real clients quote in: Gmail
// nests the quoted message inside a blockquote, Outlook states a header block
// and writes the message as the siblings after it.

// gmailHost quotes one message, with a table in it, under a reply.
const gmailHost = `<div dir="ltr">Numbers look right, thanks.</div>` +
	`<div class="gmail_quote">` +
	`<div class="gmail_attr">On Thu, 7 May 2026 at 04:38, Ada Byron &lt;ada@loomworks.example&gt; wrote:</div>` +
	`<blockquote class="gmail_quote">` +
	`<div>Here is the column mapping we agreed:</div>` +
	`<table><tr><th>Original column</th><th>New column</th></tr>` +
	`<tr><td>Site ref</td><td>NMI</td></tr>` +
	`<tr><td>Read date</td><td>Meter read</td></tr></table>` +
	`<div>Shout if the second column is wrong for the Fjordvik sites.</div>` +
	`</blockquote></div>`

const gmailQuotedText = "Here is the column mapping we agreed:\n" +
	"Original column\tNew column\n" +
	"Site ref\tNMI\n" +
	"Read date\tMeter read\n" +
	"Shout if the second column is wrong for the Fjordvik sites.\n"

func TestAMailtoMentionOpeningStillRecoversTheTable(t *testing.T) {
	// The #20 shape: the quoted message opens on a pasted mention — the needle
	// holds "@Siobhan Murphy <mailto:siobhan@termina.io>" while the host renders
	// the same mention as a link whose visible text is only the name. The
	// mailto: address adds needle tokens that never appear in the block, which
	// used to fail the head alignment and drop the whole body to plain text.
	host := `<div dir="ltr"><a href="mailto:siobhan@termina.io">@Siobhan Murphy</a>, pls help</div>` +
		`<div class="gmail_quote">` +
		`<div class="gmail_attr">On Tue, 4 Aug 2026 at 13:26, Tosh Chak wrote:</div>` +
		`<div dir="ltr">Hi <a href="mailto:siobhan@termina.io">@Siobhan Murphy</a>,</div>` +
		`<div>This ICP has been added to the database under Multiplex Cinemas Ltd. Since this is an ` +
		`unbundled ICP, no online review was completed.</div>` +
		`<table><tr><th>ICP</th><th>Remarks</th></tr>` +
		`<tr><td>0030020136PCDA3</td><td>Unbundle</td></tr></table>` +
		`<div>Thanks!</div>` +
		`</div>`
	r := &entryRow{
		Source: "mail",
		BodyText: "Hi @Siobhan Murphy <mailto:siobhan@termina.io>,\n" +
			"This ICP has been added to the database under Multiplex Cinemas Ltd. Since this is an " +
			"unbundled ICP, no online review was completed.\n" +
			"ICP\tRemarks\n003002002122PCDA3\tUnbundle\nThanks!",
		HostHTML: []string{host},
	}
	got := bodyHTML(r)
	if !strings.Contains(got, "<table>") {
		t.Fatalf("body = %q, want the table recovered despite the mailto mention opening", got)
	}
}

func TestAQuotedEntryRecoversItsMarkupFromAHost(t *testing.T) {
	// The point of the whole file: this entry has no markup of its own, and its
	// table exists only inside the reply that quoted it.
	r := &entryRow{Source: "mail", BodyText: gmailQuotedText, HostHTML: []string{gmailHost}}
	got := bodyHTML(r)
	if !strings.Contains(got, "<table>") {
		t.Fatalf("body = %q, want the table recovered from the host", got)
	}
	if !strings.Contains(got, "Original column") || !strings.Contains(got, "Meter read") {
		t.Errorf("body = %q, want the table's cells", got)
	}
	if strings.Contains(got, "Numbers look right") || strings.Contains(got, "wrote:") {
		t.Errorf("body = %q, want only the quoted message, not its host", got)
	}
}

func TestAnOutlookHostWithNoContainerStillYieldsTheBlock(t *testing.T) {
	// Outlook's history is a run of siblings after a header block, so the block
	// is delimited by the next header block and by nothing else.
	host := `<div><p>Approved.</p>` +
		`<p><b>From:</b> Ada Byron</p><p><b>Sent:</b> Thursday, 7 May 2026 04:38</p>` +
		`<p><b>Subject:</b> Re: column mapping</p>` +
		`<p>Here is the column mapping we agreed on the call, with the second column renamed so that the ` +
		`import will match what the retailer sends us each month:</p>` +
		`<table><tr><td>Site ref</td><td>NMI</td></tr></table>` +
		`<p><b>From:</b> Grace Hopper</p><p><b>Sent:</b> Wednesday, 6 May 2026 09:00</p>` +
		`<p>Opened the ticket for the cutover this morning.</p></div>`
	r := &entryRow{
		Source: "mail",
		BodyText: "Here is the column mapping we agreed on the call, with the second column renamed so that\n" +
			"the import will match what the retailer sends us each month:\nSite ref\tNMI\n",
		HostHTML: []string{host},
	}
	got := bodyHTML(r)
	if !strings.Contains(got, "<table>") {
		t.Fatalf("body = %q, want the block between the two header blocks", got)
	}
	if strings.Contains(got, "Opened the ticket") || strings.Contains(got, "Approved.") {
		t.Errorf("body = %q, want neither the host's own text nor the message below", got)
	}
}

func TestADeeperQuoteInsideARecoveredBlockIsPeeled(t *testing.T) {
	// The block the entry quoted is on the page as its own entry, exactly as for
	// an entry with markup of its own.
	host := `<div class="gmail_quote"><div class="gmail_attr">On Thu, 7 May 2026 at 04:38, Ada wrote:</div>` +
		`<blockquote class="gmail_quote"><div>Here is the column mapping we agreed, with the second column renamed.</div>` +
		`<div class="gmail_quote"><div class="gmail_attr">On Wed, 6 May 2026 at 09:00, Grace wrote:</div>` +
		`<blockquote class="gmail_quote"><div>Which column did you want renamed for the Fjordvik sites?</div></blockquote>` +
		`</div></blockquote></div>`
	r := &entryRow{
		Source:   "mail",
		BodyText: "Here is the column mapping we agreed, with the second column renamed.\n",
		HostHTML: []string{host},
	}
	got := bodyHTML(r)
	if !strings.Contains(got, "second column renamed") {
		t.Fatalf("body = %q, want the entry's own words", got)
	}
	if strings.Contains(got, "Which column did you want") {
		t.Errorf("body = %q, want the deeper quote peeled", got)
	}
}

func TestAShortBodyIsNeverCorrelated(t *testing.T) {
	// "Thanks, will do" matches every second message in a mailbox, and there is
	// no test here that would catch having matched the wrong one.
	r := &entryRow{
		Source:   "mail",
		BodyText: "Thanks Ada, will do.",
		HostHTML: []string{`<div class="gmail_quote"><blockquote class="gmail_quote"><div>Thanks Ada, will do.</div></blockquote></div>`},
	}
	if got := bodyHTML(r); got != "<p>Thanks Ada, will do.</p>" {
		t.Errorf("body = %q, want the text path", got)
	}
}

func TestBoilerplateAloneDoesNotIdentifyAMessage(t *testing.T) {
	// Two short messages from one person share a signature and a disclaimer,
	// which is most of both. The host holds only the first one; the second must
	// not take it. Matching on a bag of words does exactly that, which is why
	// the opening is compared in order.
	const sig = "\nKind regards\nAda Byron | Loomworks Pty Ltd\nThis email and any attachments are confidential. " +
		"If you are not the intended recipient please delete it and tell us. The views expressed are the " +
		"sender's own and not necessarily those of Loomworks Pty Ltd, which accepts no liability for anything " +
		"arising from this message or its attachments.\n"
	host := `<div class="gmail_quote"><div class="gmail_attr">On Thu, 7 May 2026 at 04:38, Ada wrote:</div>` +
		`<blockquote class="gmail_quote"><div>Can you confirm the meter read landed inside the billing period?</div>` +
		`<div>` + strings.ReplaceAll(sig, "\n", "<br>") + `</div></blockquote></div>`

	mine := &entryRow{Source: "mail",
		BodyText: "Can you confirm the meter read landed inside the billing period?" + sig,
		HostHTML: []string{host}}
	if got := bodyHTML(mine); !strings.Contains(got, "meter read landed") || strings.Contains(got, "<p>") {
		t.Errorf("body = %q, want the recovered markup for the message that is in the host", got)
	}

	other := &entryRow{Source: "mail",
		BodyText: "Did the second invoice for Fjordvik ever go out?" + sig,
		HostHTML: []string{host}}
	got := bodyHTML(other)
	if strings.Contains(got, "meter read landed") {
		t.Fatalf("body = %q, want no recovery: this message is not in that host", got)
	}
	if !strings.Contains(got, "second invoice for Fjordvik") {
		t.Errorf("body = %q, want the text path", got)
	}
}

func TestTwoBlocksThatBothFitAndDisagreeAreDeclined(t *testing.T) {
	// Someone sent nearly the same request twice, and both are in this host's
	// trail. Each fits the entry about equally and they end differently, so which
	// one this entry is cannot be told from here — and guessing is the one
	// outcome worse than plain text.
	const shared = "Could you reissue the offersheet for the sites we went through on the call, with the demand " +
		"class corrected and the discount applied before GST rather than after, and copy the account manager " +
		"when it goes out so that she can send it on to the client the same day. "
	host := `<div class="gmail_quote"><div class="gmail_attr">On Thu, 7 May 2026 at 04:38, Ada wrote:</div>` +
		`<blockquote class="gmail_quote"><div>` + shared + `The Fjordvik sites are the urgent ones this week.</div>` +
		`<div class="gmail_quote"><div class="gmail_attr">On Wed, 6 May 2026 at 09:00, Ada wrote:</div>` +
		`<blockquote class="gmail_quote"><div>` + shared + `Anvil Lane can wait until the meter reads land.</div>` +
		`</blockquote></div></blockquote></div>`
	r := &entryRow{
		Source:   "mail",
		BodyText: shared + "Loomworks Two is the one I need first.",
		HostHTML: []string{host},
	}
	got := bodyHTML(r)
	if strings.Contains(got, "Fjordvik") || strings.Contains(got, "Anvil Lane") {
		t.Errorf("body = %q, want no recovery: two blocks fit and they disagree", got)
	}
	if !strings.Contains(got, "Loomworks Two is the one I need first.") {
		t.Errorf("body = %q, want the entry's own text", got)
	}
}

func TestOneMessageQuotedBySeveralPeopleIsNotAmbiguous(t *testing.T) {
	// Three hosts offering the same block is agreement, not ambiguity.
	r := &entryRow{Source: "mail", BodyText: gmailQuotedText,
		HostHTML: []string{gmailHost, gmailHost, gmailHost}}
	if got := bodyHTML(r); !strings.Contains(got, "<table>") {
		t.Errorf("body = %q, want the block three hosts agree on", got)
	}
}

func TestNoHostMarkupLeavesTheTextPathAlone(t *testing.T) {
	r := &entryRow{Source: "mail", BodyText: gmailQuotedText}
	if got := bodyHTML(r); !strings.Contains(got, "<p>Here is the column mapping we agreed:") {
		t.Errorf("body = %q, want the text path", got)
	}
}

func TestAMalformedHostIsJustAHostWithNothingToOffer(t *testing.T) {
	for _, host := range []string{
		"", "<<<>>>", `<div class="gmail_quote"><blockquote class="gmail_quote"><table><tr><td>Here is the col`,
	} {
		r := &entryRow{Source: "mail", BodyText: gmailQuotedText, HostHTML: []string{host}}
		if got := bodyHTML(r); got == "" {
			t.Errorf("host %q: body is empty, want at least the text path", host)
		}
	}
}

func TestARecoveredBlockIsFoldedByTheSameMappingAsAMailboxMessage(t *testing.T) {
	// One rule for both provenances. The signature is laid out inside a single
	// paragraph here, so the fold is placed by dividing that paragraph — the same
	// thing that happens to a mailbox message's own part (see
	// TestAParagraphHoldingBothTheMessageAndTheBlockIsDividedAtTheBreakBetweenThem),
	// and the reason the mapping is not written twice.
	host := `<div dir="ltr">Numbers look right, thanks.</div>` +
		`<div class="gmail_quote">` +
		`<div class="gmail_attr">On Thu, 7 May 2026 at 04:38, Ada Byron ` +
		`&lt;ada@loomworks.example&gt; wrote:</div>` +
		`<blockquote class="gmail_quote">` +
		`<p>Here is the column mapping we agreed, with the second column renamed so that the ` +
		`import matches what the retailer sends us each month.<br>` +
		`Ada Byron<br>Head of Metering, Loomworks<br>+61 400 000 000</p>` +
		`</blockquote></div>`
	r := &entryRow{
		Source: "mail",
		BodyText: "Here is the column mapping we agreed, with the second column renamed so that the\n" +
			"import matches what the retailer sends us each month.\n" +
			"Ada Byron\nHead of Metering, Loomworks\n+61 400 000 000\n",
		HostHTML: []string{host},
		Fold:     sigFold,
	}
	got := bodyHTML(r)
	if !r.Folded {
		t.Fatalf("body = %q, want the recovered block folded", got)
	}
	at := strings.Index(got, "<details")
	if at < 0 || strings.Index(got, "column mapping") > at {
		t.Errorf("body = %q, want the sender's own sentence on screen", got)
	}
	if !strings.Contains(got, "+61 400 000 000") {
		t.Errorf("body = %q, want the folded lines still in the document", got)
	}
}

func TestInlineImagesFindsTheCidImageInTheMatchedBlock(t *testing.T) {
	// A host whose quoted message pastes an image in with a cid: reference — the
	// #51 shape. The image's alt is its filename, which is how it is matched back
	// to the host's MIME attachment.
	host := `<div dir="ltr">Thanks for the screenshot.</div>` +
		`<div class="gmail_quote">` +
		`<div class="gmail_attr">On Mon, 17 Aug 2026 at 11:11, Chris &lt;chris@x.example&gt; wrote:</div>` +
		`<blockquote class="gmail_quote">` +
		`<div>I retendered these three sites last week but I am not sure why they ` +
		`still show up on this report every morning, despite the fact that we ` +
		`completed the whole review process for each of them before the end of ` +
		`the previous month.</div>` +
		`<img src="cid:ii_abc123" alt="image.png" width="562" height="154">` +
		`</blockquote></div>`
	text := "I retendered these three sites last week but I am not sure why they " +
		"still show up on this report every morning, despite the fact that we " +
		"completed the whole review process for each of them before the end of " +
		"the previous month."
	got := inlineImages(text, []string{host})
	if len(got) != 1 || got[0] != "image.png" {
		t.Fatalf("inlineImages = %v, want [image.png]", got)
	}
}

func TestInlineImagesNoneWhenBlockHasNoCidImage(t *testing.T) {
	host := `<div dir="ltr">ok</div>` +
		`<div class="gmail_quote">` +
		`<div class="gmail_attr">On Mon, 17 Aug 2026 at 11:11, Chris wrote:</div>` +
		`<blockquote class="gmail_quote"><div>No image lives in this quoted message body.</div></blockquote></div>`
	text := "No image lives in this quoted message body."
	got := inlineImages(text, []string{host})
	if len(got) != 0 {
		t.Fatalf("inlineImages = %v, want none", got)
	}
}

func TestAttributeInlineImagesDuplicatesTheCidImageOntoTheQuotedEntry(t *testing.T) {
	host := &entryRow{
		ID: 1, Source: "mail", Direct: true, GmailID: "gmail-host",
		BodyHTML: `<div dir="ltr">Thanks.</div>` +
			`<div class="gmail_quote">` +
			`<div class="gmail_attr">On Mon, 17 Aug 2026 at 11:11, Chris &lt;chris@x.example&gt; wrote:</div>` +
			`<blockquote class="gmail_quote">` +
			`<div>I retendered these three sites last week but I am not sure why they ` +
			`still show up on this report every morning, despite the fact that we ` +
			`completed the whole review process for each of them before the end of ` +
			`the previous month.</div>` +
			`<img src="cid:ii_abc123" alt="image.png" width="562" height="154">` +
			`</blockquote></div>`,
		Atts: []attRow{
			{Name: "image.png", Mime: "image/png", Size: 64632, SourceRef: "1"},
			{Name: "quote.pdf", Mime: "application/pdf", Size: 2048, SourceRef: "2"},
		},
	}
	child := &entryRow{
		ID: 2, Source: "mail", Direct: false, SeenIn: []int64{1},
		BodyText: "I retendered these three sites last week but I am not sure why they " +
			"still show up on this report every morning, despite the fact that we " +
			"completed the whole review process for each of them before the end of " +
			"the previous month.",
	}
	rows := []*entryRow{host, child}
	attributeAttachments(rows)

	if len(child.Atts) != 1 || child.Atts[0].Name != "image.png" {
		t.Fatalf("child.Atts = %+v, want the image.png duplicated onto it", child.Atts)
	}
	if child.Atts[0].GmailID != "gmail-host" {
		t.Errorf("copied att GmailID = %q, want the host's so the chip still opens the image", child.Atts[0].GmailID)
	}
	// The host keeps its own row: the file is genuinely in its mailbox too.
	if len(host.Atts) != 2 {
		t.Fatalf("host.Atts = %+v, want both carried attachments kept", host.Atts)
	}
}

func TestAttributeFilenamesMentionedInChildText(t *testing.T) {
	// A quoted child whose body is a single short line naming the file it
	// pasted — below the token floor for block matching, so only the filename
	// evidence works. This is the Nepal-shaped case.
	host := &entryRow{
		ID: 3, Source: "mail", Direct: true, GmailID: "gmail-host2",
		Atts: []attRow{
			{Name: "image.png", Mime: "image/png", Size: 116876, SourceRef: "1"},
			{Name: "Screenshot 2026-08-26 at 8.52.12 am.png", Mime: "image/png", Size: 95716, SourceRef: "2"},
		},
	}
	child := &entryRow{
		ID: 4, Source: "mail", Direct: false, SeenIn: []int64{3},
		BodyText: "+fyi @Siobhan Murphy <mailto:siobhan@termina.io>\n<image.png>",
	}
	rows := []*entryRow{host, child}
	attributeAttachments(rows)

	if len(child.Atts) != 1 || child.Atts[0].Name != "image.png" {
		t.Fatalf("child.Atts = %+v, want image.png duplicated onto it", child.Atts)
	}
	if child.Atts[0].Size != 116876 {
		t.Errorf("copied size = %d, want 116876 (the image.png row, not the screenshot)", child.Atts[0].Size)
	}
	// The screenshot, not mentioned by name, stays on the host only.
	if len(host.Atts) != 2 {
		t.Fatalf("host.Atts = %+v, want both rows kept on the host", host.Atts)
	}
}

func TestAttributeFilenamesMentionedNormalisesWhitespace(t *testing.T) {
	// Gmail archives a macOS screenshot with a U+202F narrow no-break space in
	// the clock ("8.52.12\u202fam"), while the quoted body renders the same
	// name with a plain space. The filename has to match across that before it
	// can be attributed to the message that showed it.
	host := &entryRow{
		ID: 5, Source: "mail", Direct: true, GmailID: "gmail-host3",
		Atts: []attRow{
			{Name: "Screenshot 2026-08-26 at 8.52.12\u202fam.png", Mime: "image/png", Size: 95716, SourceRef: "1"},
		},
	}
	child := &entryRow{
		ID: 6, Source: "mail", Direct: false, SeenIn: []int64{5},
		BodyText: "Please update ASAP @review\n<Screenshot 2026-08-26 at 8.52.12 am.png>",
	}
	rows := []*entryRow{host, child}
	attributeAttachments(rows)

	if len(child.Atts) != 1 || child.Atts[0].Name != "Screenshot 2026-08-26 at 8.52.12\u202fam.png" {
		t.Fatalf("child.Atts = %+v, want the screenshot attributed despite the nbsp in the clock", child.Atts)
	}
	if child.Atts[0].Size != 95716 {
		t.Errorf("copied size = %d, want 95716", child.Atts[0].Size)
	}
}
