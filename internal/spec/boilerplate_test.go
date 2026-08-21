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

func TestAFoldStraddlingOneElementHidesLessRatherThanMore(t *testing.T) {
	// The whole signature is inside the same paragraph as the sender's closing
	// sentence. Folding that paragraph would put the sentence behind the
	// disclosure, so nothing is folded at all.
	part := `<div><p>Numbers attached.<br>Ada Byron<br>Head of Metering, Loomworks<br>` +
		`+61 400 000 000</p></div>`
	got, folded := htmlBody(part, mailBody, sigBlock)
	if folded || strings.Contains(got, "<details") {
		t.Errorf("body = %q, want no fold: the block starts inside the sender's own paragraph", got)
	}
	if !strings.Contains(got, "Numbers attached.") {
		t.Errorf("body = %q, want the sender's text kept", got)
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
