package corpus

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/zachpmanson/chainmail/internal/embed"
)

func integrityStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "probe.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}
func integrityMust(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// Regressions for quote-first ingestion, enrichment freshness and identity gates.
func TestIntegrityPromotion(t *testing.T) {
	s := integrityStore(t)
	now := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	e := Entry{Source: SourceMail, ExtID: "mail:<promotion@example.test>", TS: now, BodyText: "initial quote"}
	id, _, err := s.PutQuoted(e)
	integrityMust(t, err)
	e.BodyText = "Authoritative mailbox body with actual sender wording."
	res, err := s.Put(e, &Mail{GmailID: "gmail-promoted", MessageID: "<promotion@example.test>"}, nil)
	integrityMust(t, err)
	if res.ID != id {
		t.Fatal("entry did not converge")
	}
	shown, err := s.Show(e.ExtID)
	integrityMust(t, err)
	if shown.Quoted {
		t.Fatal("fetched entry was not promoted")
	}
	integrityMust(t, s.EnrichQuoted(id, Entry{BodyText: "Later quoted rendition that rewrites the authoritative mailbox body and adds a long legal disclaimer to become longer."}))
	shown, err = s.Show(e.ExtID)
	integrityMust(t, err)
	if shown.Body != e.BodyText {
		t.Fatal("quoted enrichment overwrote the authoritative body")
	}
	t.Logf("mailbox entry ID=%d quoted=%v; later EnrichQuoted protected body %q", id, shown.Quoted, shown.Body)
}

func TestIntegrityEnrichmentEmbedding(t *testing.T) {
	s := integrityStore(t)
	now := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	id, _, err := s.PutQuoted(Entry{Source: SourceMail, ExtID: "quote:enrich", TS: now, BodyText: "The meter reading needs checking before settlement."})
	integrityMust(t, err)
	fake := embed.NewFake(32)
	before, err := s.BackfillEmbeddings(context.Background(), fake, BackfillOptions{})
	integrityMust(t, err)
	var oldHash string
	integrityMust(t, s.DB().QueryRow(`select body_sha from entries where id=?`, id).Scan(&oldHash))
	body := "The meter reading needs checking before settlement. The invoice also contains a substantial overcharge requiring a credit note for the excess electricity bill."
	integrityMust(t, s.EnrichQuoted(id, Entry{BodyText: body, Subject: "Billing dispute"}))
	var hash, actualBody, actualSubject string
	integrityMust(t, s.DB().QueryRow(`select body_sha,body_text,subject from entries where id=?`, id).Scan(&hash, &actualBody, &actualSubject))
	after, err := s.BackfillEmbeddings(context.Background(), fake, BackfillOptions{})
	integrityMust(t, err)
	if hash == oldHash || hash != BodySHA(body, actualSubject) || after.Pending != 1 || after.Embedded != 1 || fake.Calls != 2 || actualBody != body {
		t.Fatalf("stale enrichment: hash=%s old=%s report=%+v", hash, oldHash, after)
	}
	hits, err := s.SearchEntries(Query{Text: "overcharge"})
	integrityMust(t, err)
	if len(hits) != 1 {
		t.Fatalf("lexical enrichment missing: %+v", hits)
	}
	t.Logf("first backfill embedded=%d; enrichment changed subject/body; body_sha unchanged=%v; second backfill pending=%d embedded=%d; model calls=%d; lexical new-word hits=%d", before.Embedded, hash == oldHash, after.Pending, after.Embedded, fake.Calls, len(hits))
}

func TestIntegrityAliasChain(t *testing.T) {
	s := integrityStore(t)
	_, err := AddDomainAlias(s, "old.example.test", "middle.example.test", "")
	integrityMust(t, err)
	_, err = AddDomainAlias(s, "middle.example.test", "current.example.test", "")
	integrityMust(t, err)
	current, err := ResolveAddress(s, Address{Addr: "alice@current.example.test", Name: "Alice Example"}, "probe")
	integrityMust(t, err)
	old, err := ResolveAddress(s, Address{Addr: "alice@old.example.test", Name: "Alice Example"}, "probe")
	integrityMust(t, err)
	middle, err := ResolveAddress(s, Address{Addr: "alice@middle.example.test", Name: "Alice Example"}, "probe")
	integrityMust(t, err)
	canon, _, err := CanonicalAddress(s, "alice@old.example.test")
	integrityMust(t, err)
	if old != current || middle != current || canon != "alice@current.example.test" {
		t.Fatal("chained aliases split an identity")
	}
	t.Logf("two configured rebrands: old resolves to person=%d; current=%d; middle=%d; CanonicalAddress(old)=%q", old, current, middle, canon)
}

func TestIntegrityUnrelatedDerived(t *testing.T) {
	s := integrityStore(t)
	alice, err := Resolve(s, KindEmail, "alice@example.test", "Alice Example")
	integrityMust(t, err)
	now := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	base := "Hi team. Please find the invoice for project Alpha. The construction materials have arrived onsite and the concrete foundation work starts tomorrow morning. Arrange inspection with the structural engineer before pouring begins. Regards Alice."
	later := "Hi team. Please find the newly corrected and independently approved final invoice for project Beta. Overseas software licences expire this week so renew all subscriptions using the attached procurement authorisation. Regards Alice."
	_, err = s.Put(Entry{Source: SourceMail, ExtID: "mail:<alpha@example.test>", TS: now, PersonID: alice, Subject: "Alpha construction", Container: "thread-alpha", BodyText: base}, &Mail{MessageID: "<alpha@example.test>"}, nil)
	integrityMust(t, err)
	clock := now.Add(23*time.Hour + 7*time.Minute)
	_, twin, err := FindTwin(s, alice, clock, later, "Beta software", "thread-beta")
	integrityMust(t, err)
	d, derived, err := FindDerived(s, alice, clock, later, "Beta software", "thread-beta")
	integrityMust(t, err)
	if twin || derived {
		t.Fatalf("identity gates failed: twin=%v derived=%v result=%+v", twin, derived, d)
	}
	t.Logf("different subjects/threads, different project/body, impossible mailbox render offset +23:07: twin=%v derived=%v base=%d reason=%s", twin, derived, d.Base, d.Why)
}

func TestIntegrityAliasPreviewRepairAndCycles(t *testing.T) {
	s := integrityStore(t)
	old, err := Resolve(s, KindEmail, "alice@old.test", "Alice Example")
	integrityMust(t, err)
	current, err := Resolve(s, KindEmail, "alice@current.test", "Alice Example")
	integrityMust(t, err)
	_, err = AddDomainAlias(s, "old.test", "middle.test", "")
	integrityMust(t, err)
	preview, err := PreviewDomainAlias(s, "middle.test", "current.test")
	integrityMust(t, err)
	if preview.Merged != 1 || preview.Applied {
		t.Fatalf("preview=%+v", preview)
	}
	var owner int64
	integrityMust(t, s.db.QueryRow(`select person_id from identities where value='alice@old.test'`).Scan(&owner))
	if owner != old {
		t.Fatal("preview changed literal ownership")
	}
	applied, err := AddDomainAlias(s, "middle.test", "current.test", "")
	integrityMust(t, err)
	if applied.Merged != preview.Merged {
		t.Fatalf("applied=%+v preview=%+v", applied, preview)
	}
	resolved, err := ResolveAddress(s, Address{Addr: "alice@old.test"}, "test")
	integrityMust(t, err)
	if resolved != current {
		t.Fatalf("upstream identity not repaired: %d != %d", resolved, current)
	}
	for _, previewOnly := range []bool{true, false} {
		if previewOnly {
			_, err = PreviewDomainAlias(s, "current.test", "old.test")
		} else {
			_, err = AddDomainAlias(s, "current.test", "old.test", "")
		}
		if err == nil {
			t.Fatal("accepted alias cycle")
		}
	}
	aliases, err := DomainAliases(s)
	integrityMust(t, err)
	if aliases["current.test"] != "" {
		t.Fatal("rejected cycle was persisted")
	}
	if _, err := s.db.Exec(`insert into domain_aliases(from_domain,to_domain,added_at) values ('cycle-a','cycle-b',0),('cycle-b','cycle-a',0)`); err != nil {
		t.Fatal(err)
	}
	if _, _, err := CanonicalAddress(s, "alice@cycle-a"); err == nil {
		t.Fatal("existing cycle was not rejected")
	}
}

func TestIntegrityEnrichmentReconsidersEmbeddingSkip(t *testing.T) {
	s := integrityStore(t)
	id, _, err := s.PutQuoted(Entry{Source: SourceMail, ExtID: "quote:skip", TS: time.Now(), BodyText: "Thanks"})
	integrityMust(t, err)
	fake := embed.NewFake(32)
	before, err := s.BackfillEmbeddings(context.Background(), fake, BackfillOptions{})
	integrityMust(t, err)
	if before.Skipped != 1 {
		t.Fatalf("before=%+v", before)
	}
	integrityMust(t, s.EnrichQuoted(id, Entry{Subject: "Invoice", BodyText: "Thanks for checking the electricity meter reading. The invoice contains an overcharge and the account requires a credit note before the next payment."}))
	after, err := s.BackfillEmbeddings(context.Background(), fake, BackfillOptions{})
	integrityMust(t, err)
	if after.Embedded != 1 {
		t.Fatalf("after=%+v", after)
	}
}

func TestIntegrityAliasChainPreviewCountsPeopleNotAddresses(t *testing.T) {
	s := integrityStore(t)
	for _, domain := range []string{"old.test", "middle.test", "current.test"} {
		_, err := Resolve(s, KindEmail, "alice@"+domain, "Alice Example")
		integrityMust(t, err)
	}
	_, err := AddDomainAlias(s, "old.test", "middle.test", "")
	integrityMust(t, err)
	preview, err := PreviewDomainAlias(s, "middle.test", "current.test")
	integrityMust(t, err)
	applied, err := AddDomainAlias(s, "middle.test", "current.test", "")
	integrityMust(t, err)
	if preview.Merged != 1 || applied.Merged != 1 {
		t.Fatalf("preview=%+v applied=%+v", preview, applied)
	}
}

func TestIntegritySubjectOnlyEnrichmentChangesHash(t *testing.T) {
	s := integrityStore(t)
	body := "The invoice requires a credit note for an incorrect electricity meter reading before settlement."
	id, _, err := s.PutQuoted(Entry{Source: SourceMail, ExtID: "quote:subject", TS: time.Now(), BodyText: body})
	integrityMust(t, err)
	var initial string
	integrityMust(t, s.db.QueryRow(`select body_sha from entries where id=?`, id).Scan(&initial))
	if initial != BodySHA(body, "") {
		t.Fatal("PutQuoted hash order differs from Put")
	}
	integrityMust(t, s.EnrichQuoted(id, Entry{Subject: "Credit request"}))
	var final string
	integrityMust(t, s.db.QueryRow(`select body_sha from entries where id=?`, id).Scan(&final))
	if final == initial || final != BodySHA(body, "Credit request") {
		t.Fatal("subject-only enrichment did not refresh hash")
	}
}
