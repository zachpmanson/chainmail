package corpus

import "testing"

func person(t *testing.T, s *Store, addr, name string) int64 {
	t.Helper()
	id, err := ResolveAddress(s, Address{Addr: addr, Name: name}, "test")
	if err != nil {
		t.Fatalf("resolving %s: %v", addr, err)
	}
	return id
}

// The case this exists for: accounts predating a rename keep the old domain, so
// the same human arrives as alice@old and alice@new and address-keyed resolution
// makes two people. Adding the alias must repair the rows already ingested, not
// only prevent future splits — you discover the rebrand *by* seeing the duplicate.
func TestAddDomainAliasMergesPeopleAlreadySplit(t *testing.T) {
	s := open(t)
	oldID := person(t, s, "alice@old.example", "Alice")
	newID := person(t, s, "alice@new.example", "Alice Smith")
	if oldID == newID {
		t.Fatal("expected two people before the alias existed")
	}

	merged, err := AddDomainAlias(s, "old.example", "new.example", "rebrand")
	if err != nil {
		t.Fatalf("AddDomainAlias: %v", err)
	}
	if merged != 1 {
		t.Fatalf("merged %d pairs, want 1", merged)
	}

	// both addresses now name the surviving person, and neither identity was lost
	for _, addr := range []string{"alice@old.example", "alice@new.example"} {
		got, err := PersonByIdentity(s, KindEmail, addr)
		if err != nil {
			t.Fatalf("%s: %v", addr, err)
		}
		if got != newID {
			t.Fatalf("%s resolved to %d, want the surviving person %d", addr, got, newID)
		}
	}
	var n int
	if err := s.DB().QueryRow(`select count(*) from people`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("people: got %d, want 1 after the merge", n)
	}
	// and the merge is on the record, attributed to the alias
	var reason string
	if err := s.DB().QueryRow(`select reason from person_merges`).Scan(&reason); err != nil {
		t.Fatal(err)
	}
	if reason == "" || reason[:13] != "domain-alias:" {
		t.Fatalf("merge reason = %q, want it attributed to the alias", reason)
	}
}

func TestAliasAppliesToLaterResolution(t *testing.T) {
	s := open(t)
	if _, err := AddDomainAlias(s, "@old.example", "@new.example", ""); err != nil {
		t.Fatal(err)
	}
	// leading @ is tolerated on both sides
	canonical := person(t, s, "bob@new.example", "Bob")
	viaOld := person(t, s, "bob@old.example", "Bob")
	if viaOld != canonical {
		t.Fatalf("old-domain address made a second person (%d vs %d)", viaOld, canonical)
	}
	// the identity is stored canonically, so there is one row not two
	var n int
	if err := s.DB().QueryRow(
		`select count(*) from identities where kind='email'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("identities: got %d, want 1 canonical row", n)
	}
}

func TestAliasDoesNotMergeDifferentPeople(t *testing.T) {
	s := open(t)
	a := person(t, s, "alice@old.example", "Alice")
	b := person(t, s, "bob@new.example", "Bob")
	merged, err := AddDomainAlias(s, "old.example", "new.example", "")
	if err != nil {
		t.Fatal(err)
	}
	if merged != 0 {
		t.Fatalf("merged %d pairs of unrelated people", merged)
	}
	if a == b {
		t.Fatal("distinct locals collapsed")
	}
}

func TestAliasRejectsNonsense(t *testing.T) {
	s := open(t)
	for _, c := range []struct{ from, to string }{
		{"", "new.example"},
		{"old.example", ""},
		{"same.example", "same.example"}, // a no-op that would silently do nothing
	} {
		if _, err := AddDomainAlias(s, c.from, c.to, ""); err == nil {
			t.Fatalf("AddDomainAlias(%q, %q) was accepted", c.from, c.to)
		}
	}
}

// Candidates are reported, never merged: two colleagues can share a local part
// across unrelated domains, so this is evidence for a human, not a decision.
func TestMergeCandidatesReportsWithoutMerging(t *testing.T) {
	s := open(t)
	person(t, s, "alice@one.example", "Alice")
	person(t, s, "alice@two.example", "A. Smith")
	person(t, s, "bob@one.example", "Bob")

	cands, err := MergeCandidates(s)
	if err != nil {
		t.Fatal(err)
	}
	if len(cands) != 1 {
		t.Fatalf("candidates: got %d, want 1", len(cands))
	}
	if cands[0].Reason == "" {
		t.Fatal("candidate carries no reason")
	}
	var n int
	if err := s.DB().QueryRow(`select count(*) from people`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("people: got %d, want 3 — reporting must not merge", n)
	}
}
