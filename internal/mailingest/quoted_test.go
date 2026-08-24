package mailingest

import (
	"strings"
	"testing"
	"time"

	"github.com/zachpmanson/chainmail/internal/corpus"
)

func openTest(t *testing.T) *corpus.Store {
	t.Helper()
	s, err := corpus.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// The same original quoted inside two different host messages must become ONE
// entry with TWO sightings. This is the case the anonymised fixture corpus
// cannot express — each fixture was anonymised independently, so one original
// carries different fake names in different fixtures — so it is constructed.
func TestSameOriginalInTwoHostsIsOneEntryTwoSightings(t *testing.T) {
	s := openTest(t)
	quoted := "On Wed, 19 Aug 2026 at 07:52, Ro Laren <ro.laren@daystrom.fed> wrote:\n" +
		"the levy column needs a decision"

	for i, id := range []string{"host-a", "host-b"} {
		msg := Message{
			Envelope: Envelope{
				ID: id, MessageID: "<" + id + "@x>", ThreadID: "t1",
				From: "Zora Miller <zora@x.fed>", Subject: "Re: the export",
				Date: time.Date(2026, 8, 20, 9, 0, 0, 0, time.UTC).Format(time.RFC1123Z),
			},
			// The second host rewraps the quote, as a different client would.
			Body: map[bool]string{false: quoted, true: quoted + "\n"}[i == 1],
		}
		if _, err := Put(s, msg); err != nil {
			t.Fatal(err)
		}
	}

	real, q, err := s.QuotedCount()
	if err != nil {
		t.Fatal(err)
	}
	if real != 2 {
		t.Errorf("real entries = %d, want 2 (the two hosts)", real)
	}
	if q != 1 {
		t.Fatalf("quoted entries = %d, want 1 — the same original quoted twice "+
			"must not become two entries", q)
	}
}

// A recovered block that carries a Message-ID matching a real mailbox message
// must merge into it, and must NOT overwrite the mailbox copy: the quoted text
// has been rewrapped and elided by every client that forwarded it.
func TestQuotedBlockNeverDegradesTheMailboxCopy(t *testing.T) {
	s := openTest(t)
	full := "the complete original text, every word of it"
	real := corpus.Entry{
		Source: corpus.SourceMail, ExtID: "mail:<orig@x>", Kind: "message",
		TS:      time.Date(2026, 8, 19, 7, 52, 0, 0, time.UTC),
		Subject: "the export", BodyText: full,
	}
	res, err := s.Put(real, &corpus.Mail{MessageID: "<orig@x>"}, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Same ext_id, elided body.
	id, created, err := s.PutQuoted(corpus.Entry{
		Source: corpus.SourceMail, ExtID: "mail:<orig@x>", Kind: "message",
		TS: real.TS, BodyText: "the complete original ...",
	})
	if err != nil {
		t.Fatal(err)
	}
	if created {
		t.Error("created a second entry for a message already present")
	}
	if id != res.ID {
		t.Errorf("id = %d, want the existing %d", id, res.ID)
	}
	var got string
	if err := s.DB().QueryRow(`select body_text from entries where id=?`, id).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != full {
		t.Errorf("mailbox copy was degraded by the quoted one:\n got %q\nwant %q", got, full)
	}
}

// Positional nesting is the reply graph. The outermost quoted block replies to
// the host; each deeper block is what the one above it replied to.
func TestPositionalNestingBecomesReplyEdges(t *testing.T) {
	s := openTest(t)
	body := "my reply\n\n" +
		"On Wed, 19 Aug 2026 at 10:00, Bea <bea@x.fed> wrote:\n" +
		"> second\n" +
		">\n" +
		"> On Wed, 19 Aug 2026 at 09:00, Cyd <cyd@x.fed> wrote:\n" +
		">> first\n"
	msg := Message{
		Envelope: Envelope{
			ID: "h", MessageID: "<h@x>", ThreadID: "t", From: "Ana <ana@x.fed>",
			Subject: "Re: x",
			Date:    time.Date(2026, 8, 19, 11, 0, 0, 0, time.UTC).Format(time.RFC1123Z),
		},
		Body: body,
	}
	if _, err := Put(s, msg); err != nil {
		t.Fatal(err)
	}
	rows, err := s.DB().Query(`
		select e.body_text, coalesce(p.body_text,'(host)')
		from entries e left join entries p on p.id = e.parent_id
		where e.quoted = 1 order by e.ts desc`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var edges [][2]string
	for rows.Next() {
		var child, parent string
		if err := rows.Scan(&child, &parent); err != nil {
			t.Fatal(err)
		}
		edges = append(edges, [2]string{child, parent})
	}
	if len(edges) != 2 {
		t.Fatalf("got %d quoted entries, want 2: %v", len(edges), edges)
	}
	// Direction: you quote what you are replying TO. So Bea's "second" replies to
	// Cyd's "first", and the deepest block has no parent in this body — whatever
	// it replied to was never quoted here.
	if edges[0][0] != "second" || edges[0][1] != "first" {
		t.Errorf("outer edge = %q -> %q, want \"second\" -> \"first\"", edges[0][0], edges[0][1])
	}
	if edges[1][0] != "first" || edges[1][1] != "(host)" {
		t.Errorf("deepest edge = %q -> %q, want \"first\" -> \"(host)\" (no parent)",
			edges[1][0], edges[1][1])
	}
}

// A message already in the mailbox must not be stored a second time because
// somebody quoted it. The sentinel states the clock the QUOTER rendered — ten
// hours ahead of the true instant here — so the two never meet on a key, and
// extraction has to reconcile them by that offset or create the duplicate.
func TestQuotingAMailboxMessageCreatesNoSecondEntry(t *testing.T) {
	s := openTest(t)
	sent := time.Date(2026, 5, 20, 1, 38, 14, 0, time.UTC)
	original := "Can you confirm the meter number for the Rothwell depot before the " +
		"tender pack goes out this afternoon? The retailer wants it on the cover " +
		"sheet and I do not want to guess at it."
	if _, err := Put(s, Message{
		Envelope: Envelope{
			ID: "orig", MessageID: "<orig@quarry.fed>", ThreadID: "t1",
			From: "Deniz Aslan <deniz.aslan@quarry.fed>", Subject: "Rothwell tender pack",
			Date: sent.Format(time.RFC1123Z),
		},
		Body: original,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := Put(s, Message{
		Envelope: Envelope{
			ID: "reply", MessageID: "<reply@moana.fed>", ThreadID: "t1",
			From: "Tui Walker <tui.walker@moana.fed>", Subject: "Re: Rothwell tender pack",
			Date: sent.Add(3 * time.Hour).Format(time.RFC1123Z),
		},
		// The reply's client rewrapped the quote and rendered the clock at +1200.
		Body: "Meter number is on the way.\n\n" +
			"On Wed, 20 May 2026 at 11:38, Deniz Aslan <deniz.aslan@quarry.fed> wrote:\n" +
			"Can you confirm the meter number for the Rothwell depot before the tender\n" +
			"pack goes out this afternoon? The retailer wants it on the cover sheet and\n" +
			"I do not want to guess at it.",
	}); err != nil {
		t.Fatal(err)
	}

	real, q, err := s.QuotedCount()
	if err != nil {
		t.Fatal(err)
	}
	if real != 2 || q != 0 {
		t.Fatalf("entries = %d real and %d recovered, want the two mailbox messages "+
			"and no recovered copy of one of them", real, q)
	}
	// The quote is still recorded as a sighting of the original, which is the
	// evidence that would otherwise have become an entry of its own.
	var seenIn, host int64
	if err := s.DB().QueryRow(`
		select g.seen_in, (select id from entries where ext_id='mail:<reply@moana.fed>')
		from sightings g join entries e on e.id = g.entry_id
		where e.ext_id = 'mail:<orig@quarry.fed>' and g.kind = 'quoted'`).
		Scan(&seenIn, &host); err != nil {
		t.Fatalf("the quote left no sighting on the original: %v", err)
	}
	if seenIn != host {
		t.Fatalf("sighting names host %d, want the reply %d", seenIn, host)
	}
}

// An answer typed INSIDE the text being quoted is not a twin of the original
// (the words do not hold) and not an unrelated message (it answers the very
// quote it sits in): extraction must recognise it as a MODIFIED copy, count it
// as Derived, and hang it off the base it edited so the renderer can show it
// as a quote-in-progress rather than a floating duplicate.
func TestAnAnswerInsideAQuotedMessageIsADerivedCopy(t *testing.T) {
	s := openTest(t)
	sent := time.Date(2026, 5, 20, 1, 38, 14, 0, time.UTC)
	original := "Can you confirm the meter number for the Rothwell depot before the " +
		"tender pack goes out this afternoon? The retailer wants it on the cover " +
		"sheet and I do not want to guess at it."
	if _, err := Put(s, Message{
		Envelope: Envelope{
			ID: "orig", MessageID: "<orig@quarry.fed>", ThreadID: "t1",
			From: "Deniz Aslan <deniz.aslan@quarry.fed>", Subject: "Rothwell tender pack",
			Date: sent.Format(time.RFC1123Z),
		},
		Body: original,
	}); err != nil {
		t.Fatal(err)
	}
	// Another participant answers INSIDE the quoted text: a sentence is woven
	// into the middle of the original, the way a red inline reply lands.
	annotated := "Can you confirm the meter number for the Rothwell depot before the " +
		"tender pack goes out before this afternoon? On the last invoice, I will " +
		"send it over. The retailer wants it on the cover sheet and I do not want " +
		"to guess at it."
	if _, err := Put(s, Message{
		Envelope: Envelope{
			ID: "reply", MessageID: "<reply@moana.fed>", ThreadID: "t1",
			From: "Tui Walker <tui.walker@moana.fed>", Subject: "Re: Rothwell tender pack",
			Date: sent.Add(3 * time.Hour).Format(time.RFC1123Z),
		},
		Body: "The meter number is on the way too.\n\n" +
			"On Wed, 20 May 2026 at 11:38, Deniz Aslan <deniz.aslan@quarry.fed> wrote:\n" +
			annotated,
	}); err != nil {
		t.Fatal(err)
	}

	// The original is not duplicated: the inline answer stays its own entry.
	// Its parent is the message it was edited inside, not the reply that quoted
	// it — that is the visible "modified quote lives where it was edited" edge.
	var derivedEntries, parentOf int64
	if err := s.DB().QueryRow(`
		select count(*) from entries where ext_id like 'quote:%'
		  and source = 'mail'`).Scan(&derivedEntries); err != nil {
		t.Fatal(err)
	}
	if derivedEntries != 1 {
		t.Fatalf("quoted entries = %d, want 1 (the edited copy, no duplicate of the original)",
			derivedEntries)
	}
	if err := s.DB().QueryRow(`
		select coalesce(parent_id,0) from entries where ext_id like 'quote:%'`,
	).Scan(&parentOf); err != nil {
		t.Fatal(err)
	}
	if parentOf == 0 {
		t.Fatalf("the modified copy was not linked to its base")
	}
}

// Issue #29: an answer someone types INSIDE the message they quote, marked in
// red in the text/html part, is a fresh reply from the quoter — not a rewrite
// of the quoted original. The colour exists only in the HTML, so detection reads
// it from the stored part; the flat text would hand the words to the quoted
// author. Extraction must lift the colour-marked run out as its own entry,
// attributed to the host who typed it, with a child edge to the message it
// answers.
func TestRedAnswerInsideAQuoteBecomesItsOwnEntry(t *testing.T) {
	s := openTest(t)
	sent := time.Date(2026, 5, 20, 1, 38, 14, 0, time.UTC)
	original := "Can you confirm the meter number for the Rothwell depot before the " +
		"tender pack goes out this afternoon? The retailer wants it on the cover " +
		"sheet and I do not want to guess at it."
	if _, err := Put(s, Message{
		Envelope: Envelope{
			ID: "orig", MessageID: "<orig@quarry.fed>", ThreadID: "t1",
			From: "Deniz Aslan <deniz.aslan@quarry.fed>", Subject: "Rothwell tender pack",
			Date: sent.Format(time.RFC1123Z),
		},
		Body: original,
	}); err != nil {
		t.Fatal(err)
	}

	// The quoter's red answer is interleaved into the text being quoted. It sits
	// in the flattened Body (so the quote reads as edited) AND is colour-marked
	// in the BodyHTML — the colour is the only sign it is the quoter's reply.
	answer := "On the last invoice, I will send it over."
	annotated := "Can you confirm the meter number for the Rothwell depot before the " +
		"tender pack goes out before this afternoon? " + answer + " The retailer " +
		"wants it on the cover sheet and I do not want to guess at it."
	html := "<p>The meter number is on the way too.</p>" +
		"<div>On Wed, 20 May 2026 at 11:38, Deniz Aslan " +
		"&lt;deniz.aslan@quarry.fed&gt; wrote:</div><blockquote>" + original +
		"<span style=\"color:red\"> " + answer + "</span> The retailer wants " +
		"it on the cover sheet and I do not want to guess at it.</blockquote>"
	if _, err := Put(s, Message{
		Envelope: Envelope{
			ID: "reply", MessageID: "<reply@moana.fed>", ThreadID: "t1",
			From: "Tui Walker <tui.walker@moana.fed>", Subject: "Re: Rothwell tender pack",
			Date: sent.Add(3 * time.Hour).Format(time.RFC1123Z),
		},
		Body: "The meter number is on the way too.\n\n" +
			"On Wed, 20 May 2026 at 11:38, Deniz Aslan <deniz.aslan@quarry.fed> wrote:\n" +
			annotated,
		BodyHTML: html,
	}); err != nil {
		t.Fatal(err)
	}

	// The answer is its own entry: edge to the original, authored by the quoter.
	var inlineID, inlinePerson, inlineParent int64
	var inlineBody string
	if err := s.DB().QueryRow(`
		select id, person_id, coalesce(parent_id,0), body_text
		  from entries where ext_id like 'inline:%'`).Scan(
		&inlineID, &inlinePerson, &inlineParent, &inlineBody); err != nil {
		t.Fatal(err)
	}
	if strings.Join(strings.Fields(inlineBody), " ") != answer {
		t.Fatalf("inline body = %q, want the red answer %q", inlineBody, answer)
	}

	var origID, origPerson int64
	if err := s.DB().QueryRow(`
		select id, person_id from entries where ext_id = 'mail:<orig@quarry.fed>'`,
	).Scan(&origID, &origPerson); err != nil {
		t.Fatal(err)
	}
	if inlineParent != origID {
		t.Fatalf("inline parent = %d, want the quoted original %d (the child edge)",
			inlineParent, origID)
	}

	var replyPerson int64
	if err := s.DB().QueryRow(`
		select person_id from entries where ext_id = 'mail:<reply@moana.fed>'`,
	).Scan(&replyPerson); err != nil {
		t.Fatal(err)
	}
	if inlinePerson == 0 || inlinePerson != replyPerson {
		t.Fatalf("inline author = %d, want the quoter's person %d (not the quoted author %d)",
			inlinePerson, replyPerson, origPerson)
	}
	if inlinePerson == origPerson {
		t.Fatalf("inline answer attributed to the quoted original's author — that is the bug #29 removes")
	}

	// With the answer lifted out, the quoted block is the original again and
	// collapses to it: no separate quote entry survives.
	var quotes int64
	if err := s.DB().QueryRow(`
		select count(*) from entries where ext_id like 'quote:%'
		  and source = 'mail'`).Scan(&quotes); err != nil {
		t.Fatal(err)
	}
	if quotes != 0 {
		t.Fatalf("quoted copies = %d, want 0 (the quote, denuded of the answer, folded into the original)", quotes)
	}
}
