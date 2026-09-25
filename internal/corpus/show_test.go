package corpus

import (
	"errors"
	"slices"
	"testing"
	"time"
)

// Search reports the entry that MATCHED, not the chain root, so naming any
// message must return the whole conversation. Resolving only downwards from the
// named entry would return a fragment and look like the whole thing.
func TestChainIsReachableFromAnyMemberNotJustTheRoot(t *testing.T) {
	s := open(t)
	mk := func(ext string, min int, parent int64, body string) int64 {
		e := Entry{
			Source: SourceMail, ExtID: ext, Kind: "message",
			TS: time.Date(2026, 8, 1, 9, min, 0, 0, time.UTC), BodyText: body,
		}
		res, err := s.Put(e, &Mail{MessageID: ext}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if parent != 0 {
			if err := s.SetParent(res.ID, parent); err != nil {
				t.Fatal(err)
			}
		}
		return res.ID
	}
	root := mk("a", 0, 0, "the first")
	mid := mk("b", 1, root, "the second")
	mk("c", 2, mid, "the third")

	for _, from := range []string{"a", "b", "c"} {
		got, err := s.Chain(from)
		if err != nil {
			t.Fatalf("from %s: %v", from, err)
		}
		if len(got) != 3 {
			t.Errorf("from %s: got %d entries, want the whole chain of 3", from, len(got))
		}
		// Time order, not insertion or traversal order.
		for i := 1; i < len(got); i++ {
			if got[i].TS.Before(got[i-1].TS) {
				t.Errorf("from %s: entry %d is out of time order", from, i)
			}
		}
	}
}

// The provenance is the reason this view exists: a message quoted in three
// forwards has three sightings, and that is the evidence it mattered.
func TestShowCarriesEverySighting(t *testing.T) {
	s := open(t)
	host, err := s.Put(Entry{
		Source: SourceMail, ExtID: "host", Kind: "message",
		TS: time.Unix(1_700_000_100, 0), BodyText: "the forward",
	}, &Mail{MessageID: "host"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	id, created, err := s.PutQuoted(Entry{
		Source: SourceMail, ExtID: "quote:abc", Kind: "message",
		TS: time.Unix(1_700_000_000, 0), BodyText: "the original",
	})
	if err != nil || !created {
		t.Fatalf("PutQuoted: created=%v err=%v", created, err)
	}
	if err := s.Sight(id, host.ID, "quoted", "depth 2"); err != nil {
		t.Fatal(err)
	}

	got, err := s.Show("quote:abc")
	if err != nil {
		t.Fatal(err)
	}
	if !got.Quoted {
		t.Error("an entry recovered from quoted text must report itself as such")
	}
	if len(got.Sightings) != 1 {
		t.Fatalf("sightings = %d, want 1", len(got.Sightings))
	}
	if g := got.Sightings[0]; g.Kind != "quoted" || g.SeenIn != "host" || g.Detail != "depth 2" {
		t.Errorf("sighting = %+v", g)
	}
}

// A pane that lets the reader arrange reply recipients needs the exact address
// from each message header, not the identity graph's broader set of aliases.
func TestShowCarriesExactHeaderRecipients(t *testing.T) {
	s := open(t)
	_, err := s.Put(Entry{
		Source: SourceMail, ExtID: "mail:<recipient-test@x>", Kind: "message",
		TS: time.Unix(1_700_000_000, 0), BodyText: "the message",
	}, &Mail{
		MessageID: "recipient-test",
		To:        `Cy Okafor <cy@loomworks.example>, "Nkemdirim, Carl" <carl@example.net>`,
		Cc:        `Marit Solheim <marit@loomworks.example>`,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	got, err := s.Show("mail:<recipient-test@x>")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.ToRecipients) != 2 || got.ToRecipients[0].Addr != "cy@loomworks.example" ||
		got.ToRecipients[1].Addr != "carl@example.net" || got.ToRecipients[1].Name != "Nkemdirim, Carl" {
		t.Errorf("To recipients = %+v, want the two exact header addresses and names", got.ToRecipients)
	}
	if len(got.CcRecipients) != 1 || got.CcRecipients[0].Addr != "marit@loomworks.example" {
		t.Errorf("Cc recipients = %+v, want the exact header address", got.CcRecipients)
	}
}

// An unknown id must be distinguishable from an empty result, so a caller can
// tell "no such thing" from "nothing to say about it".
func TestShowReportsAMissingIDAsSuch(t *testing.T) {
	s := open(t)
	if _, err := s.Show("mail:<nope@x>"); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
	if _, err := s.Chain("mail:<nope@x>"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Chain err = %v, want ErrNotFound", err)
	}
}

// The files a message carries come back with it, and in the order the source
// stated. A read that could not answer this is why an attachment showed nowhere in
// the pane: the rows were there, and nothing on the read path asked for them.
func TestShowCarriesAnEntrysAttachments(t *testing.T) {
	s := open(t)
	e := Entry{Source: SourceMail, ExtID: "with-files", Kind: "message",
		TS: time.Date(2026, 9, 14, 2, 12, 0, 0, time.UTC), BodyText: "the file is attached"}
	_, err := s.Put(e, &Mail{MessageID: "with-files"}, []Attachment{
		{Name: "billing.csv", Mime: "text/csv", Size: 4096, SourceRef: "part-1",
			Permalink: "https://mail.google.com/mail/u/0/#all/abc"},
		{Name: "notes.txt", Mime: "text/plain", Size: 12, SourceRef: "part-2"},
	})
	if err != nil {
		t.Fatal(err)
	}
	// The bytes themselves, so the digest below is a real link rather than a
	// string the test just agreed with itself about.
	if err := s.PutBlob(Blob{Bytes: []byte("csv,bytes"), Mime: "text/csv", Source: SourceMail}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.LinkBlob("with-files", "part-1", BlobSHA([]byte("csv,bytes"))); err != nil {
		t.Fatal(err)
	}

	got, err := s.Show("with-files")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Attachments) != 2 {
		t.Fatalf("got %d attachments, want 2", len(got.Attachments))
	}
	one := got.Attachments[0]
	if one.Name != "billing.csv" || one.Mime != "text/csv" || one.Size != 4096 ||
		one.SourceRef != "part-1" || one.Permalink != "https://mail.google.com/mail/u/0/#all/abc" {
		t.Errorf("first attachment: %+v", one)
	}
	// The digest is what lets a client ask for the bytes rather than send the
	// reader back to the mailbox, so it has to survive the read.
	if one.BlobSHA != BlobSHA([]byte("csv,bytes")) {
		t.Errorf("first attachment digest: %q, want the digest of the bytes", one.BlobSHA)
	}
	if got.Attachments[1].Name != "notes.txt" {
		t.Errorf("second attachment: %+v (order is the source's)", got.Attachments[1])
	}
	// And the chain read, which is what the pane goes through, carries them too.
	chain, err := s.Chain("with-files")
	if err != nil {
		t.Fatal(err)
	}
	if len(chain) != 1 || len(chain[0].Attachments) != 2 {
		t.Fatalf("chain: %d entries, %d attachments on the first", len(chain), len(chain[0].Attachments))
	}
}

// putAt puts one message at a stated moment, with the headers a reply carries, and
// reports its row id.
//
// The clock is the parameter and not a constant because the cases below are about the
// clock disagreeing with the conversation: an entry recovered from someone's
// quotation carries the wall clock the quoter's client wrote for it, read as UTC (see
// unnest.Attribution.Sent), so it can sort after the replies that came before it.
func putAt(t *testing.T, s *Store, ext, messageID, inReplyTo string, ts time.Time) int64 {
	t.Helper()
	res, err := s.Put(Entry{
		Source: SourceMail, ExtID: ext, Kind: "message",
		TS: ts, BodyText: ext, ParentRef: inReplyTo,
	}, &Mail{MessageID: messageID, InReplyTo: inReplyTo}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return res.ID
}

// extIDs is a chain read as the list a client draws.
func extIDs(chain []Shown) []string {
	out := make([]string, 0, len(chain))
	for _, e := range chain {
		out = append(out, e.ExtID)
	}
	return out
}

// parentsFirst fails if any entry is drawn before a parent that is also in the set.
func parentsFirst(t *testing.T, chain []Shown) {
	t.Helper()
	at := map[string]int{}
	for i, e := range chain {
		at[e.ExtID] = i
	}
	for i, e := range chain {
		if e.Parent == "" {
			continue
		}
		j, in := at[e.Parent]
		if !in {
			continue
		}
		if j >= i {
			t.Errorf("%s is drawn at %d, before its parent %s at %d", e.ExtID, i, e.Parent, j)
		}
	}
}

// A chain whose root is a message recovered from a quotation is drawn in the
// conversation's order, not the clock's.
//
// The recovered root has no Date header of its own, so its ts is the wall clock the
// quoter's client wrote for it, read as UTC — later than the replies that came before
// it. `order by e.ts` therefore drew a reply above the message it answers. Every other
// reader places this edge already (chronological.ts on the page, tree.ts in the pane,
// tzinfer treating "a quoted message was sent before the message quoting it" as a hard
// constraint); the chain read was the one that believed the clock.
func TestAChainIsOrderedByTheReplyGraphWhereTheClockDisagrees(t *testing.T) {
	s := open(t)
	// 16:17 UTC is the quoter's wall clock read as UTC, and the only entry here with no
	// zone at all. The three below it are real mailbox messages, in the order they were
	// sent: a reply, a second reply to the same message, and an answer to the first.
	at := func(h, m int) time.Time { return time.Date(2026, 8, 30, h, m, 0, 0, time.UTC) }
	putAt(t, s, "mail:<ledger-2026-08@billing.example>", "<ledger-2026-08@billing.example>", "", at(16, 17))
	putAt(t, s, "mail:<re-checking-this@mailbox.example>", "<re-checking-this@mailbox.example>", "<ledger-2026-08@billing.example>", at(6, 31))
	putAt(t, s, "mail:<and-one-more-thing@mailbox.example>", "<and-one-more-thing@mailbox.example>", "<ledger-2026-08@billing.example>", at(6, 45))
	putAt(t, s, "mail:<thanks@mailbox.example>", "<thanks@mailbox.example>", "<re-checking-this@mailbox.example>", at(7, 0))
	if _, err := s.ResolveParents(); err != nil {
		t.Fatal(err)
	}

	// Named from the middle, which is how search reports a hit.
	chain, err := s.Chain("mail:<re-checking-this@mailbox.example>")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"mail:<ledger-2026-08@billing.example>",
		"mail:<re-checking-this@mailbox.example>",
		// The two replies to the recovered root are not ancestor and descendant, so the
		// clock still decides between them: 6:31 before 6:45.
		"mail:<and-one-more-thing@mailbox.example>",
		"mail:<thanks@mailbox.example>",
	}
	if got := extIDs(chain); !slices.Equal(got, want) {
		t.Errorf("chain order:\n got %v\nwant %v (the root first, then time order)", got, want)
	}
	parentsFirst(t, chain)
}

// An entry whose parent is not in the set is placed where it belongs rather than
// dropped or drawn last: a chain's head is usually a message whose own parent we
// never received.
func TestAnEntryWhoseParentIsNotInTheSetIsPlacedByItsOwnClock(t *testing.T) {
	s := open(t)
	at := func(h, m int) time.Time { return time.Date(2026, 8, 30, h, m, 0, 0, time.UTC) }
	// Nothing in the corpus holds this id: the chain starts here, and the row carries a
	// ParentRef that resolves to nothing.
	putAt(t, s, "mail:<first@mailbox.example>", "<first@mailbox.example>", "<never-received@elsewhere.example>", at(7, 0))
	putAt(t, s, "mail:<later@mailbox.example>", "<later@mailbox.example>", "<first@mailbox.example>", at(7, 30))
	putAt(t, s, "mail:<earlier@mailbox.example>", "<earlier@mailbox.example>", "<first@mailbox.example>", at(6, 0))
	if _, err := s.ResolveParents(); err != nil {
		t.Fatal(err)
	}

	chain, err := s.Chain("mail:<first@mailbox.example>")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"mail:<first@mailbox.example>",
		"mail:<earlier@mailbox.example>",
		"mail:<later@mailbox.example>",
	}
	if got := extIDs(chain); !slices.Equal(got, want) {
		t.Errorf("chain order:\n got %v\nwant %v", got, want)
	}
	// The head is drawn first even though nothing resolved its parent: a missing parent
	// is not a reason to move an entry, and it is not an orphan to the chain either.
	parentsFirst(t, chain)
}

// A parent cycle still returns every row.
//
// The schema does not forbid one — parent_id is a foreign key to the same table and
// two rows naming each other satisfy it — and nothing an ingest does should produce
// one. But a read that answered with an empty list, or with one of the two, would be a
// worse failure than an order that means nothing: the rows are what the reader asked
// for, and Kahn's algorithm has to be given a way out.
func TestAParentCycleStillReturnsEveryRow(t *testing.T) {
	s := open(t)
	at := func(h, m int) time.Time { return time.Date(2026, 8, 30, h, m, 0, 0, time.UTC) }
	first := putAt(t, s, "mail:<one@loop.example>", "<one@loop.example>", "", at(6, 0))
	second := putAt(t, s, "mail:<two@loop.example>", "<two@loop.example>", "", at(7, 0))
	for _, e := range []struct{ id, parent int64 }{{first, second}, {second, first}} {
		if _, err := s.db.Exec(`update entries set parent_id = ? where id = ?`, e.parent, e.id); err != nil {
			t.Fatal(err)
		}
	}

	chain, err := s.Chain("mail:<one@loop.example>")
	if err != nil {
		t.Fatal(err)
	}
	// Both rows, and the earlier clock first: neither is eligible, so the earliest is
	// taken out of order and the other follows it.
	want := []string{"mail:<one@loop.example>", "mail:<two@loop.example>"}
	if got := extIDs(chain); !slices.Equal(got, want) {
		t.Errorf("chain order:\n got %v\nwant %v (every row, earliest first)", got, want)
	}
	// And nothing was dropped on the way in: the walk itself has to terminate inside the
	// cycle, which is what `union` at each step of the CTE buys.
	if len(chain) != 2 {
		t.Errorf("chain returned %d rows, want both", len(chain))
	}
}
