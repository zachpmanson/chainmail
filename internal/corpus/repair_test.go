package corpus

import (
	"fmt"
	"testing"
)

// All names, addresses and domains here are invented.

// The case this exists for: a header bracket-matched before the parse understood
// Outlook's hyperlinked form left the same human as two people, one on the
// address and one on the address doubled through mailto:. Fixing the parse cannot
// undo that — identities are keyed on the value — so the stored rows are repaired
// and the halves folded, with the clean one surviving.
func TestRepairMergesPeopleSplitByAMailtoAddress(t *testing.T) {
	s := open(t)
	clean := person(t, s, "ada.fenwick@quarry.fed", "Ada Fenwick")
	broken, err := ResolveWithRule(s, KindEmail,
		"ada.fenwick@quarry.fed <mailto:ada.fenwick@quarry.fed", "", "mail:to-header")
	if err != nil {
		t.Fatal(err)
	}
	if broken == clean {
		t.Fatal("expected two people before the repair")
	}
	// the malformed half is a real participant, so the merge has references to move
	e := entry("mail:<ask@x>", "can you confirm the meter number")
	rec, err := s.Put(e, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := Participate(s, rec.ID, broken, RoleTo); err != nil {
		t.Fatal(err)
	}

	r, err := RepairMailtoIdentities(s)
	if err != nil {
		t.Fatalf("RepairMailtoIdentities: %v", err)
	}
	if r.Rewritten != 1 || r.Merged != 1 {
		t.Fatalf("repair = %+v, want 1 rewritten and 1 merged", r)
	}
	if len(r.Ambiguous) != 0 {
		t.Fatalf("nothing here is ambiguous, got %v", r.Ambiguous)
	}

	// the clean person is the survivor, and the participation followed them
	got, err := PersonByIdentity(s, KindEmail, "ada.fenwick@quarry.fed")
	if err != nil {
		t.Fatal(err)
	}
	if got != clean {
		t.Fatalf("survivor is %d, want the clean person %d", got, clean)
	}
	var n int
	if err := s.DB().QueryRow(`select count(*) from people`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("people: got %d, want 1", n)
	}
	if err := s.DB().QueryRow(
		`select count(*) from participants where person_id=? and role='to'`, clean).
		Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("the merged half's participation did not move: %d rows", n)
	}
	// the malformed value is gone rather than left as a spelling nothing looks up
	if err := s.DB().QueryRow(
		`select count(*) from identities where value like '%mailto:%'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("%d malformed identities survived the repair", n)
	}
	// and the merge is on the record, naming the value that caused it
	var reason string
	if err := s.DB().QueryRow(`select reason from person_merges`).Scan(&reason); err != nil {
		t.Fatal(err)
	}
	if reason == "" || reason[:14] != "repair:mailto " {
		t.Fatalf("merge reason = %q, want it attributed to the repair", reason)
	}
}

// With no clean counterpart there is nobody to merge into, so the person stays
// and only their identity — and the display name they were given from it — is
// reduced. Losing the person here would lose every message they are on.
func TestRepairRewritesALoneMalformedIdentityWithoutMerging(t *testing.T) {
	s := open(t)
	id, err := Resolve(s, KindEmail, "bram@quarry.fed<mailto:bram@quarry.fed", "")
	if err != nil {
		t.Fatal(err)
	}

	r, err := RepairMailtoIdentities(s)
	if err != nil {
		t.Fatal(err)
	}
	if r.Rewritten != 1 || r.Merged != 0 || r.Renamed != 1 {
		t.Fatalf("repair = %+v, want 1 rewritten, 0 merged, 1 renamed", r)
	}
	got, err := PersonByIdentity(s, KindEmail, "bram@quarry.fed")
	if err != nil {
		t.Fatalf("the repaired address does not resolve: %v", err)
	}
	if got != id {
		t.Fatalf("resolved to %d, want the same person %d", got, id)
	}
	if name := nameOf(t, s, id); name != "bram@quarry.fed" {
		t.Fatalf("display name = %q, want the address it always meant", name)
	}
}

// Several spellings of one address collapse onto a single person, whichever
// order they are seen in.
func TestRepairCollapsesEverySpellingOfOneAddress(t *testing.T) {
	s := open(t)
	for _, v := range []string{
		"cleo@quarry.fed",
		"cleo@quarry.fed <mailto:cleo@quarry.fed",
		"cleo@quarry.fed<mailto:cleo@quarry.fed",
		"mailto:cleo@quarry.fed",
	} {
		if _, err := Resolve(s, KindEmail, v, ""); err != nil {
			t.Fatal(err)
		}
	}
	r, err := RepairMailtoIdentities(s)
	if err != nil {
		t.Fatal(err)
	}
	if r.Rewritten != 3 || r.Merged != 3 {
		t.Fatalf("repair = %+v, want 3 rewritten and 3 merged", r)
	}
	var people, ids int
	if err := s.DB().QueryRow(`select count(*) from people`).Scan(&people); err != nil {
		t.Fatal(err)
	}
	if err := s.DB().QueryRow(
		`select count(*) from identities where kind='email'`).Scan(&ids); err != nil {
		t.Fatal(err)
	}
	if people != 1 || ids != 1 {
		t.Fatalf("got %d people over %d identities, want 1 and 1", people, ids)
	}
}

// Running the repair on an already-repaired corpus must be a no-op, so it is
// safe to wire into a routine pass rather than being a one-shot script.
func TestRepairIsIdempotent(t *testing.T) {
	s := open(t)
	person(t, s, "ada@quarry.fed", "Ada")
	if _, err := Resolve(s, KindEmail, "ada@quarry.fed <mailto:ada@quarry.fed", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := RepairMailtoIdentities(s); err != nil {
		t.Fatal(err)
	}
	before := snapshot(t, s)

	again, err := RepairMailtoIdentities(s)
	if err != nil {
		t.Fatal(err)
	}
	if again.Rewritten != 0 || again.Merged != 0 || again.Renamed != 0 ||
		len(again.Ambiguous) != 0 {
		t.Fatalf("second run = %+v, want nothing to do", again)
	}
	if after := snapshot(t, s); after != before {
		t.Fatalf("second run changed the corpus:\n%s\nwas\n%s", after, before)
	}
}

// The repair only ever folds two spellings of ONE address. Two people who happen
// to share a corpus, or a value naming two different addresses, must come out the
// other side untouched — a wrong merge here attributes messages to someone who
// was never on them, and Merge is deliberately hard to undo.
func TestRepairDoesNotMergeDifferentPeople(t *testing.T) {
	s := open(t)
	ada := person(t, s, "ada@quarry.fed", "Ada Fenwick")
	bram := person(t, s, "bram@quarry.fed", "Bram Fenwick")
	if _, err := Resolve(s, KindEmail,
		"cleo@quarry.fed <mailto:cleo@quarry.fed", "Cleo"); err != nil {
		t.Fatal(err)
	}

	r, err := RepairMailtoIdentities(s)
	if err != nil {
		t.Fatal(err)
	}
	if r.Merged != 0 {
		t.Fatalf("merged %d pairs of unrelated people", r.Merged)
	}
	if ada == bram {
		t.Fatal("distinct people collapsed")
	}
	for _, addr := range []string{"ada@quarry.fed", "bram@quarry.fed", "cleo@quarry.fed"} {
		if _, err := PersonByIdentity(s, KindEmail, addr); err != nil {
			t.Fatalf("%s: %v", addr, err)
		}
	}
	var n int
	if err := s.DB().QueryRow(`select count(*) from people`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("people: got %d, want 3", n)
	}
}

// A value naming two addresses is reported, not resolved. Either choice would be
// a guess, and the wrong one is a merge of two humans.
func TestRepairReportsAmbiguousValuesInsteadOfPicking(t *testing.T) {
	s := open(t)
	if _, err := Resolve(s, KindEmail,
		"ada@quarry.fed <mailto:bram@quarry.fed", ""); err != nil {
		t.Fatal(err)
	}
	r, err := RepairMailtoIdentities(s)
	if err != nil {
		t.Fatal(err)
	}
	if r.Rewritten != 0 || r.Merged != 0 {
		t.Fatalf("repair = %+v, want nothing changed", r)
	}
	if len(r.Ambiguous) != 1 {
		t.Fatalf("ambiguous = %v, want the one value reported", r.Ambiguous)
	}
	// the row is still there, still findable by the next human to look
	if _, err := PersonByIdentity(s, KindEmail,
		"ada@quarry.fed <mailto:bram@quarry.fed"); err != nil {
		t.Fatalf("the ambiguous identity was dropped: %v", err)
	}
}

// A configured rebrand applies to the repair too: the reduced address is
// canonicalised before it is stored, or the repair would reintroduce the very
// split the alias exists to close.
func TestRepairLandsOnTheCanonicalDomain(t *testing.T) {
	s := open(t)
	if _, err := AddDomainAlias(s, "old.example", "quarry.fed", "rebrand"); err != nil {
		t.Fatal(err)
	}
	keep := person(t, s, "ada@quarry.fed", "Ada")
	if _, err := Resolve(s, KindEmail, "ada@old.example <mailto:ada@old.example", ""); err != nil {
		t.Fatal(err)
	}
	r, err := RepairMailtoIdentities(s)
	if err != nil {
		t.Fatal(err)
	}
	if r.Merged != 1 {
		t.Fatalf("repair = %+v, want the alias applied and the halves merged", r)
	}
	got, err := PersonByIdentity(s, KindEmail, "ada@quarry.fed")
	if err != nil {
		t.Fatal(err)
	}
	if got != keep {
		t.Fatalf("survivor is %d, want %d", got, keep)
	}
}

// snapshot is every row the repair could touch, ordered, for comparing a corpus
// against itself across a second run.
func snapshot(t *testing.T, s *Store) string {
	t.Helper()
	var out string
	rows, err := s.DB().Query(`
		select 'person', id, display_name from people
		union all
		select 'identity', person_id, kind || ':' || value from identities
		union all
		select 'merge', kept_id, dropped_id || ' ' || coalesce(reason,'') from person_merges
		order by 1, 2, 3`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var a, c string
		var b int64
		if err := rows.Scan(&a, &b, &c); err != nil {
			t.Fatal(err)
		}
		out += fmt.Sprintf("%s %d %s\n", a, b, c)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}
