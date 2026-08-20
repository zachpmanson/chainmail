package corpus

import (
	"testing"
	"time"
)

func open(t *testing.T) *Store {
	t.Helper()
	s, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func entry(ext, body string) Entry {
	return Entry{
		Source: SourceMail, ExtID: ext, TS: time.Unix(1_700_000_000, 0),
		BodyText: body, BodyHTML: "<p>" + body + "</p>",
	}
}

func TestOpenIsIdempotent(t *testing.T) {
	s := open(t)
	// migrate() again on an already-migrated database must be a no-op
	if err := s.migrate(); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
}

func TestPutCreatesThenReportsUnchangedThenChanged(t *testing.T) {
	s := open(t)

	r, err := s.Put(entry("mail:<a@x>", "hello"), nil, nil)
	if err != nil {
		t.Fatalf("first put: %v", err)
	}
	if !r.Created || r.Changed {
		t.Fatalf("first put: got %+v, want created", r)
	}

	// re-ingesting identical content must be harmless and reported as such:
	// this is what makes a resumable backfill safe to restart
	r2, err := s.Put(entry("mail:<a@x>", "hello"), nil, nil)
	if err != nil {
		t.Fatalf("second put: %v", err)
	}
	if r2.Created || r2.Changed || r2.ID != r.ID {
		t.Fatalf("re-put: got %+v, want unchanged id %d", r2, r.ID)
	}

	r3, err := s.Put(entry("mail:<a@x>", "hello, edited"), nil, nil)
	if err != nil {
		t.Fatalf("third put: %v", err)
	}
	if r3.Changed != true || r3.Created {
		t.Fatalf("edited put: got %+v, want changed", r3)
	}
	if r3.ID != r.ID {
		t.Fatalf("edited put changed the id: %d -> %d", r.ID, r3.ID)
	}
}

func TestBodySHAIgnoresReflow(t *testing.T) {
	// a forwarded body is often rewrapped; that is not an edit
	if BodySHA("one two\nthree") != BodySHA("one  two   three") {
		t.Fatal("whitespace should not change the hash")
	}
	if BodySHA("one two") == BodySHA("one three") {
		t.Fatal("different words should change the hash")
	}
}

func TestDanglingParentIsKeptAndCounted(t *testing.T) {
	s := open(t)
	e := entry("mail:<child@x>", "reply")
	e.ParentRef = "<parent-we-never-received@outlook>"
	if _, err := s.Put(e, &Mail{MessageID: "<child@x>"}, nil); err != nil {
		t.Fatalf("put: %v", err)
	}

	// The parent is outside the mailbox. That is the normal case, so the row must
	// survive, and it must be reported as a known hole rather than silently lost.
	st, err := s.Stats()
	if err != nil {
		t.Fatal(err)
	}
	if st.Unresolved != 1 {
		t.Fatalf("unresolved: got %d, want 1", st.Unresolved)
	}

	n, err := s.ResolveParents()
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("resolved %d edges with no parent present", n)
	}
}

func TestResolveParentsLinksOnceTheParentArrives(t *testing.T) {
	s := open(t)
	child := entry("mail:<child@x>", "reply")
	child.ParentRef = "<parent@x>"
	if _, err := s.Put(child, &Mail{MessageID: "<child@x>"}, nil); err != nil {
		t.Fatal(err)
	}

	if n, _ := s.ResolveParents(); n != 0 {
		t.Fatalf("resolved %d before the parent existed", n)
	}

	// the parent turns up later, e.g. extracted from a forward
	if _, err := s.Put(entry("mail:<parent@x>", "original"), &Mail{MessageID: "<parent@x>"}, nil); err != nil {
		t.Fatal(err)
	}
	n, err := s.ResolveParents()
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("resolved %d edges, want 1", n)
	}

	var parent int64
	if err := s.DB().QueryRow(
		`select parent_id from entries where ext_id='mail:<child@x>'`).Scan(&parent); err != nil {
		t.Fatal(err)
	}
	if parent == 0 {
		t.Fatal("parent_id still null after resolution")
	}

	// parent_ref is kept after resolution, so the edge's provenance stays visible
	var ref string
	if err := s.DB().QueryRow(
		`select parent_ref from entries where ext_id='mail:<child@x>'`).Scan(&ref); err != nil {
		t.Fatal(err)
	}
	if ref != "<parent@x>" {
		t.Fatalf("parent_ref = %q, want it kept", ref)
	}
}

func TestFullTextAndIdentifierSearch(t *testing.T) {
	s := open(t)
	e := entry("mail:<a@x>", "Confirming the levy on invoice CINV-00066864 today")
	e.Subject = "June billing"
	if _, err := s.Put(e, nil, nil); err != nil {
		t.Fatal(err)
	}

	// porter stemming: the query word is not the word in the body
	var n int
	if err := s.DB().QueryRow(
		`select count(*) from entries_fts where entries_fts match 'confirm'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("stemmed match: got %d, want 1", n)
	}

	// trigram: a substring of an identifier the word tokenizers would split
	if err := s.DB().QueryRow(
		`select count(*) from entries_ident where entries_ident match '00066864'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("identifier match: got %d, want 1", n)
	}

	// An edit must be reflected in the indexes, not just the table. Note the
	// replacement deliberately shares no stem with the original: "levies" would
	// still match 'levy' under the porter tokenizer, which is correct behaviour
	// and would make this assertion test nothing.
	e.BodyText = "Rescheduled the site visit"
	if _, err := s.Put(e, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.DB().QueryRow(
		`select count(*) from entries_fts where entries_fts match 'levy'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("stale prose index after edit: %d hits for a removed word", n)
	}
	if err := s.DB().QueryRow(
		`select count(*) from entries_ident where entries_ident match '00066864'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("stale identifier index after edit: %d hits for a removed id", n)
	}
	// and the new text is findable
	if err := s.DB().QueryRow(
		`select count(*) from entries_fts where entries_fts match 'rescheduled'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("new text not indexed: %d hits", n)
	}
}

func TestAttachmentsAreReplacedNotAccumulated(t *testing.T) {
	s := open(t)
	e := entry("mail:<a@x>", "see attached")
	if _, err := s.Put(e, nil, []Attachment{{Name: "one.csv"}, {Name: "two.csv"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Put(e, nil, []Attachment{{Name: "one.csv"}}); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := s.DB().QueryRow(`select count(*) from attachments`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("attachments: got %d, want 1 after re-ingest", n)
	}
}

func TestSightingsRecordEveryPlaceSeen(t *testing.T) {
	s := open(t)
	orig, err := s.Put(entry("quote:abc", "the original text"), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	f1, _ := s.Put(entry("mail:<fwd1@x>", "fwd one"), nil, nil)
	f2, _ := s.Put(entry("mail:<fwd2@x>", "fwd two"), nil, nil)

	// the same original quoted inside two different forwards
	for _, in := range []int64{f1.ID, f2.ID} {
		if err := s.Sight(orig.ID, in, "quoted", "unspooled"); err != nil {
			t.Fatal(err)
		}
	}
	// and re-recording one is idempotent
	if err := s.Sight(orig.ID, f1.ID, "quoted", "unspooled again"); err != nil {
		t.Fatal(err)
	}

	var n int
	if err := s.DB().QueryRow(
		`select count(*) from sightings where entry_id=?`, orig.ID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("sightings: got %d, want 2", n)
	}
}
