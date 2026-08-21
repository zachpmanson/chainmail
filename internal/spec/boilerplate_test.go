package spec

import (
	"strings"
	"testing"

	"github.com/zachpmanson/chainmail/internal/boiler"
	"github.com/zachpmanson/chainmail/internal/corpus"
)

// Every body here is invented. What is real is the shape — a sign-off with a
// title under it, a confidentiality notice appended below one — and no line of
// real correspondence is committed.

var sigFold = boiler.Fold{Lines: 3, Count: 43, Scope: boiler.Author}

// sigBlock is the same fold as the markup path receives it: the block's own
// lines, as the text rendition of these test parts writes them.
var sigBlock = bodyFold{
	lines: []string{"Ada Byron", "Head of Metering, Loomworks", "+61 400 000 000"},
	note:  repeatNote(sigFold),
}

func TestAFoldedSignatureIsOffScreenAndStillInTheDocument(t *testing.T) {
	// The difference between folding and deleting, and the reason for it: the
	// number is what somebody is searching for, and find-in-page reaches it
	// whether the disclosure is open or shut.
	body := "Numbers attached.\n\nAda Byron\nHead of Metering, Loomworks\n+61 400 000 000"
	got, folded := textToHTML(body, mailBody, sigFold)
	if !folded {
		t.Fatalf("body = %q, want a fold reported so the source note can count it", got)
	}
	if !strings.HasPrefix(got, "<p>Numbers attached.</p><details class=\"sig\">") {
		t.Errorf("body = %q, want the sender's own text then the disclosure", got)
	}
	if !strings.Contains(got, "+61 400 000 000") {
		t.Errorf("body = %q, want the folded lines present, not removed", got)
	}
	if !strings.Contains(got, "these 3 lines end 43 of this sender") {
		t.Errorf("body = %q, want the evidence stated on the control", got)
	}
}

func TestADomainFoldIsCalledADisclaimerRatherThanASignature(t *testing.T) {
	// The word matters: an organisation's notice is not the sender's sign-off, and
	// the summary is the only place the page says which was found.
	body := "Rates attached.\n\nThis email is confidential.\nIf it is not for you, delete it."
	got, _ := textToHTML(body, mailBody,
		boiler.Fold{Lines: 2, Count: 5, Senders: 3, Scope: boiler.Domain})
	if !strings.Contains(got, ">disclaimer</summary>") {
		t.Errorf("body = %q, want the control to say disclaimer", got)
	}
	if !strings.Contains(got, "from 3 senders at this domain") {
		t.Errorf("body = %q, want the sender count stated", got)
	}
}

func TestAnEntirelyBoilerplateBodyDoesNotRenderEmpty(t *testing.T) {
	// A fold that took every line would leave a bubble whose only content is a
	// closed disclosure. boiler.Detect will not produce one; this asserts the
	// render declines it anyway, since the fold arrives from a separate pass and
	// nothing else here checks.
	body := "Ada Byron\nLoomworks"
	got, folded := textToHTML(body, mailBody, boiler.Fold{Lines: 2, Count: 9, Scope: boiler.Author})
	if folded || strings.Contains(got, "<details") {
		t.Errorf("body = %q, want no fold when nothing would remain in view", got)
	}
	if !strings.Contains(got, "Ada Byron") {
		t.Errorf("body = %q, want the body rendered as it stands", got)
	}
}

func TestAFoldIsAppliedToTheSendersOwnMarkupAtASiblingBoundary(t *testing.T) {
	part := `<div><p>Numbers attached.</p>` +
		`<p>Ada Byron<br>Head of Metering, Loomworks<br>+61 400 000 000</p></div>`
	got, folded := htmlBody(part, mailBody, sigBlock)
	if !folded {
		t.Fatalf("body = %q, want the trailing block folded", got)
	}
	if !strings.Contains(got, "Numbers attached.") || !strings.Contains(got, "+61 400 000 000") {
		t.Errorf("body = %q, want both the message and the folded block present", got)
	}
	i, j := strings.Index(got, "<details"), strings.Index(got, "+61 400 000 000")
	if i < 0 || j < i {
		t.Errorf("body = %q, want the signature inside the disclosure", got)
	}
}

func TestAParagraphHoldingBothTheMessageAndTheBlockIsDividedAtTheBreakBetweenThem(t *testing.T) {
	// The shape most mailbox messages arrive in: a client lays the signature out
	// as <br>-separated lines inside the same paragraph as the closing sentence,
	// so there is no sibling boundary anywhere in it. The paragraph is divided at
	// the break the block opens after, which leaves the sentence on screen.
	part := `<div><p>Numbers attached.<br>Ada Byron<br>Head of Metering, Loomworks<br>` +
		`+61 400 000 000</p></div>`
	got, folded := htmlBody(part, mailBody, sigBlock)
	if !folded {
		t.Fatalf("body = %q, want the block folded", got)
	}
	i, j, k := strings.Index(got, "Numbers attached."), strings.Index(got, "<details"),
		strings.Index(got, "Ada Byron")
	if i < 0 || j < i || k < j {
		t.Errorf("body = %q, want the sentence, then the disclosure, then the block", got)
	}
	if !strings.Contains(got, "+61 400 000 000") {
		t.Errorf("body = %q, want the folded lines present, not removed", got)
	}
}

func TestABlockOpeningPartwayAlongALineIsNotFoldedAtAll(t *testing.T) {
	// Nothing in the markup ends a line where the block begins: the sender's
	// sign-off runs into it. Dividing there would take half a line the sender
	// wrote behind the disclosure, so the block is left in view instead.
	part := `<div><p>Numbers attached. Ada Byron<br>Head of Metering, Loomworks<br>` +
		`+61 400 000 000</p></div>`
	got, folded := htmlBody(part, mailBody, sigBlock)
	if folded || strings.Contains(got, "<details") {
		t.Errorf("body = %q, want no fold when the block opens mid-line", got)
	}
	if !strings.Contains(got, "Numbers attached. Ada Byron") {
		t.Errorf("body = %q, want the line intact, not divided", got)
	}
}

func TestABlockRunningPastItsWrapperTakesTheSiblingsAfterItToo(t *testing.T) {
	// The block opens inside the signature wrapper and ends in a notice the
	// client wrote after it, so what has to be folded is neither a run of
	// siblings nor one subtree. The wrapper is divided and the run continues into
	// what follows it.
	part := `<div><p>Numbers attached.</p>` +
		`<div><p>Ada Byron<br>Head of Metering, Loomworks<br>+61 400 000 000</p></div>` +
		`<p>This email is confidential.</p></div>`
	block := bodyFold{
		lines: []string{"+61 400 000 000", "This email is confidential."},
		note:  repeatNote(boiler.Fold{Lines: 2, Count: 5, Senders: 2, Scope: boiler.Domain}),
	}
	got, folded := htmlBody(part, mailBody, block)
	if !folded {
		t.Fatalf("body = %q, want the block folded", got)
	}
	at := strings.Index(got, "<details")
	if at < 0 || strings.Index(got, "Ada Byron") > at {
		t.Errorf("body = %q, want the name on screen: it is not part of the block", got)
	}
	for _, want := range []string{"+61 400 000 000", "This email is confidential."} {
		if i := strings.Index(got, want); i < at {
			t.Errorf("body = %q, want %q inside the disclosure", got, want)
		}
	}
}

func TestASignatureLaidOutAsATableIsLeftInViewRatherThanRestructured(t *testing.T) {
	// Dividing a table at a cell makes two tables whose columns no longer line
	// up, which is a worse page than the signature it would have folded.
	part := `<div><p>Numbers attached.</p><table><tr><td>Ada Byron<br>` +
		`Head of Metering, Loomworks<br>+61 400 000 000</td></tr></table></div>`
	block := bodyFold{
		lines: []string{"Head of Metering, Loomworks", "+61 400 000 000"},
		note:  repeatNote(sigFold),
	}
	got, folded := htmlBody(part, mailBody, block)
	if folded || strings.Contains(got, "<details") {
		t.Errorf("body = %q, want no fold inside a table", got)
	}
	if !strings.Contains(got, "<table>") {
		t.Errorf("body = %q, want the table as the sender laid it out", got)
	}
}

func TestAnImagePlaceholderInsideTheBlockDoesNotMoveWhereItEnds(t *testing.T) {
	// The text rendition writes an inline image as "[alt]<href>", so a signature
	// with a logo in it has a line the markup has no text for, and a cid: image
	// is dropped before the fold is placed. Neither changes the end of the block,
	// which is the end of the body.
	part := `<div><p>Numbers attached.</p>` +
		`<p>Ada Byron<br>Head of Metering, Loomworks<br>` +
		`<img src="cid:logo001.png" alt="Loomworks">` +
		`<img src="https://loomworks.example/sig.png" alt="Loomworks"><br>` +
		`+61 400 000 000</p></div>`
	got, folded := htmlBody(part, mailBody, sigBlock)
	if !folded {
		t.Fatalf("body = %q, want the block folded", got)
	}
	at := strings.Index(got, "<details")
	if i := strings.Index(got, "sig.png"); i < at {
		t.Errorf("body = %q, want the logo inside the disclosure with the rest of the block", got)
	}
	if strings.Contains(got, "cid:") {
		t.Errorf("body = %q, want the unresolvable image dropped", got)
	}
	if !strings.Contains(got, "+61 400 000 000") {
		t.Errorf("body = %q, want the line after the logo folded, not lost", got)
	}
}

func TestMarkupWithNothingOutsideTheBlockIsNotFoldedAway(t *testing.T) {
	part := `<div><p>Ada Byron</p><p>Head of Metering, Loomworks</p><p>+61 400 000 000</p></div>`
	got, folded := htmlBody(part, mailBody, sigBlock)
	if folded || strings.Contains(got, "<details") {
		t.Errorf("body = %q, want no fold when the whole part is the block", got)
	}
}

func TestNoDetectedBlockLeavesTheBodyExactlyAsItWas(t *testing.T) {
	body := "Numbers attached.\n\nAda Byron\n+61 400 000 000"
	with, folded := textToHTML(body, mailBody, boiler.Fold{})
	if folded || strings.Contains(with, "<details") {
		t.Errorf("body = %q, want nothing folded without evidence", with)
	}
}

func TestTheSourceNoteCountsEntriesFoldedRatherThanEntriesDetected(t *testing.T) {
	// Folded is set by the render, not by the detector, because the render is
	// what declines at a markup seam or on an all-boilerplate body. A note built
	// from the detection would over-report and could not be checked against the
	// page.
	rows := []*entryRow{
		{Fold: sigFold, Folded: true},
		{Fold: boiler.Fold{Lines: 4, Count: 5, Senders: 2, Scope: boiler.Domain}, Folded: true},
		{Fold: sigFold}, // detected, declined at render
		{},
	}
	items := foldNotes(rows)
	if len(items) != 1 {
		t.Fatalf("foldNotes = %v, want one item", items)
	}
	if !strings.Contains(items[0], "2 of 4 entries") {
		t.Errorf("note = %q, want the folded count, not the detected one", items[0])
	}
	if !strings.Contains(items[0], "1 of them repeat across several senders") {
		t.Errorf("note = %q, want the domain-scope share broken out", items[0])
	}
	if n := foldNotes([]*entryRow{{}}); n != nil {
		t.Errorf("foldNotes = %v, want nothing said when nothing was folded", n)
	}
}

// foldTrail is a small invented corpus: one person whose signature repeats across
// four messages, of which only one is selected for the page.
func foldTrail(t *testing.T) (*corpus.Store, string) {
	s := open(t)
	ida := person(t, s, "Ada Byron", "ada@loomworks.example")
	idb := person(t, s, "Bo Halvorsen", "bo@fjordline.example")
	sig := "\n\nAda Byron\nHead of Metering, Loomworks\n+61 400 000 000"
	for i, subject := range []string{"one", "two", "three"} {
		put(t, s, msg{
			ext: "mail:<off" + subject + "@loomworks>", ts: "2026-02-0" + string(rune('1'+i)) + "T09:00:00+11:00",
			tz: "AEDT", person: ida, container: "T" + subject, subject: subject,
			messageID: "<off" + subject + "@loomworks>",
			from:      "Ada Byron <ada@loomworks.example>",
			to:        "Bo Halvorsen <bo@fjordline.example>",
			text:      "Elsewhere in the corpus." + sig,
		})
	}
	id := put(t, s, msg{
		ext: "mail:<on@loomworks>", ts: "2026-03-02T09:15:00+11:00", tz: "AEDT",
		person: ida, container: "T1", subject: "Loom cutover",
		messageID: "<on@loomworks>", from: "Ada Byron <ada@loomworks.example>",
		to: "Bo Halvorsen <bo@fjordline.example>",
		// Bo's own message repeats nothing, so his identical closing lines stay.
		text: "The cutover is Monday." + sig,
	})
	other := put(t, s, msg{
		ext: "mail:<bo@fjordline>", ts: "2026-03-02T23:40:00+01:00", tz: "+0100",
		person: idb, container: "T1", subject: "Loom cutover",
		messageID: "<bo@fjordline>", inReplyTo: "<on@loomworks>",
		from: "Bo Halvorsen <bo@fjordline.example>",
		to:   "Ada Byron <ada@loomworks.example>",
		text: "Noted." + sig,
	})
	for _, e := range []int64{id, other} {
		if err := s.Sight(e, 0, "direct", ""); err != nil {
			t.Fatalf("Sight: %v", err)
		}
	}
	if _, err := s.ResolveParents(); err != nil {
		t.Fatalf("ResolveParents: %v", err)
	}
	return s, "T1"
}

func TestTheEvidenceIsTheCorpusAndNotThePage(t *testing.T) {
	// The reason detection cannot live in an entry: the page holds one message
	// from Ada, and her signature is only visible as boilerplate from the three
	// messages that are not on it.
	s, container := foldTrail(t)
	sp := generate(t, s, Options{Containers: []string{container}, Title: "Cutover"})
	var ada, bo Entry
	for _, e := range sp.Messages {
		switch e.Sender {
		case "Ada Byron":
			ada = e
		case "Bo Halvorsen":
			bo = e
		}
	}
	if !strings.Contains(ada.Body, `<details class="sig">`) {
		t.Errorf("Ada's body = %q, want her signature folded on evidence from off the page", ada.Body)
	}
	if !strings.Contains(ada.Body, "+61 400 000 000") {
		t.Errorf("Ada's body = %q, want the folded lines still in the document", ada.Body)
	}
	if strings.Contains(bo.Body, "<details") {
		t.Errorf("Bo's body = %q, want nothing folded: the same lines are his own prose here", bo.Body)
	}
	var note string
	for _, n := range sp.SourceNotes {
		for _, i := range n.Items {
			if strings.Contains(i, "folded behind a disclosure") {
				note = i
			}
		}
	}
	if !strings.Contains(note, "1 of 2 entries") {
		t.Errorf("source note = %q, want the fold declared and counted", note)
	}
}

func TestAMailboxMessagesOwnPartIsFoldedAfterItsHistoryIsPeeled(t *testing.T) {
	// The direct path end to end: the entry was in the mailbox, so its trail comes
	// off (it is on the page as its own entries) and the block is then the tail of
	// what is left. The block's lines are read from this entry's own text, peeled
	// the same way, which is what keeps the two ends of the fold measuring the
	// same thing.
	r := &entryRow{
		Source: "mail",
		Direct: true,
		Fold:   sigFold,
		BodyText: "The cutover is Monday.\nAda Byron\nHead of Metering, Loomworks\n" +
			"+61 400 000 000\n\nOn Thu, 7 May 2026 at 04:38, Bo Halvorsen <bo@fjordline.example> wrote:\n" +
			"> Which Monday?\n",
		BodyHTML: `<div><p>The cutover is Monday.<br>Ada Byron<br>Head of Metering, Loomworks<br>` +
			`+61 400 000 000</p>` +
			`<div class="gmail_quote"><div class="gmail_attr">On Thu, 7 May 2026 at 04:38, ` +
			`Bo Halvorsen &lt;bo@fjordline.example&gt; wrote:</div>` +
			`<blockquote class="gmail_quote"><p>Which Monday?</p></blockquote></div></div>`,
	}
	got := bodyHTML(r)
	if !r.Folded {
		t.Fatalf("body = %q, want the block folded on the mailbox message's own markup", got)
	}
	if strings.Contains(got, "Which Monday?") {
		t.Errorf("body = %q, want the quoted history peeled, not folded", got)
	}
	at := strings.Index(got, "<details")
	if at < 0 || strings.Index(got, "The cutover is Monday.") > at {
		t.Errorf("body = %q, want the sender's sentence on screen", got)
	}
	if !strings.Contains(got, "+61 400 000 000") {
		t.Errorf("body = %q, want the folded lines still in the document", got)
	}
}
