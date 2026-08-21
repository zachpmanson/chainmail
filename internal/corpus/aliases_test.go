package corpus

import (
	"strings"
	"testing"
)

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

	r, err := AddDomainAlias(s, "old.example", "new.example", "rebrand")
	if err != nil {
		t.Fatalf("AddDomainAlias: %v", err)
	}
	if r.Merged != 1 || len(r.Refused) != 0 {
		t.Fatalf("merged %d, refused %d, want 1 and 0", r.Merged, len(r.Refused))
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

// Two local parts that differ are never a candidate at all: the match is on the
// local part, so this covers only the outer boundary — that an alias does not
// sweep up everyone on either domain. What one local part shared by two humans
// does is the subject of the tests below, and is where the damage lives.
func TestAliasDoesNotMergeAcrossDifferentLocalParts(t *testing.T) {
	s := open(t)
	a := person(t, s, "alice@old.example", "Alice")
	b := person(t, s, "bob@new.example", "Bob")
	r, err := AddDomainAlias(s, "old.example", "new.example", "")
	if err != nil {
		t.Fatal(err)
	}
	if r.Merged != 0 || len(r.Refused) != 0 {
		t.Fatalf("merged %d, refused %d, want 0 and 0 — neither is a candidate",
			r.Merged, len(r.Refused))
	}
	if a == b {
		t.Fatal("distinct locals collapsed")
	}
}

// The case this whole file turns on, and the one the corpus really held: two
// employees of one organisation who were given the same first name as a mailbox,
// one before the rename and one after. A shared local part is what an alias
// claims to explain and here does not, and folding them attributes one person's
// correspondence to the other with nothing afterwards to show for it.
func TestAliasRefusesTwoPeopleOfOneLocalPartWithDifferentSurnames(t *testing.T) {
	s := open(t)
	ngahere := person(t, s, "vasa@old.example", "Vasa Ngahere")
	tolokau := person(t, s, "vasa@new.example", "Vasa Tolokau")

	r, err := AddDomainAlias(s, "old.example", "new.example", "rebrand")
	if err != nil {
		t.Fatal(err)
	}
	if r.Merged != 0 {
		t.Fatalf("merged %d, want 0 — different surnames are different humans", r.Merged)
	}
	if len(r.Refused) != 1 {
		t.Fatalf("refused %d pairs, want 1: %+v", len(r.Refused), r.Refused)
	}
	f := r.Refused[0]
	if f.Address != "vasa@old.example" || f.Reason == "" {
		t.Fatalf("refusal names %q because %q; want the address and a reason",
			f.Address, f.Reason)
	}
	// A refusal a human is asked to act on has to name both halves.
	if f.KeepName == "" || f.DropName == "" || f.KeepID == 0 || f.DropID == 0 {
		t.Fatalf("refusal does not identify both people: %+v", f)
	}
	if !strings.Contains(f.Reason, "ngahere") || !strings.Contains(f.Reason, "tolokau") {
		t.Fatalf("reason %q does not name the surnames that conflict", f.Reason)
	}
	// and both still exist, holding their own addresses
	if countPeople(t, s) != 2 {
		t.Fatalf("people = %d, want 2", countPeople(t, s))
	}
	for addr, want := range map[string]int64{
		"vasa@old.example": ngahere, "vasa@new.example": tolokau,
	} {
		got, err := PersonByIdentity(s, KindEmail, addr)
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("%s resolved to %d, want %d", addr, got, want)
		}
	}
}

// Surnames agreeing is not enough on its own: an alias groups by local part, so
// unlike `corpus dedupe` it can be handed two people who never shared a first
// name. Two siblings at one employer is the ordinary way this happens.
func TestAliasRefusesTwoPeopleOfOneLocalPartWithDifferentFirstNames(t *testing.T) {
	s := open(t)
	person(t, s, "jhale@old.example", "Jerome Hale")
	person(t, s, "jhale@new.example", "Junia Hale")

	r, err := AddDomainAlias(s, "old.example", "new.example", "")
	if err != nil {
		t.Fatal(err)
	}
	if r.Merged != 0 || len(r.Refused) != 1 {
		t.Fatalf("merged %d, refused %d, want 0 and 1", r.Merged, len(r.Refused))
	}
	if !strings.Contains(r.Refused[0].Reason, "first name") {
		t.Fatalf("reason %q does not say the first names differ", r.Refused[0].Reason)
	}
}

// The rebrand this command exists for: one human, one name, two domains.
func TestAliasMergesOneLocalPartWhenTheNameIsTheSame(t *testing.T) {
	s := open(t)
	person(t, s, "delia@old.example", "Delia Rangi")
	keep := person(t, s, "delia@new.example", "Delia Rangi")

	r, err := AddDomainAlias(s, "old.example", "new.example", "")
	if err != nil {
		t.Fatal(err)
	}
	if r.Merged != 1 || len(r.Refused) != 0 {
		t.Fatalf("merged %d, refused %d, want 1 and 0", r.Merged, len(r.Refused))
	}
	if got, err := PersonByIdentity(s, KindEmail, "delia@old.example"); err != nil ||
		got != keep {
		t.Fatalf("old address resolved to %d (err %v), want the current-domain person %d",
			got, err, keep)
	}
}

// A name that is absent, or that is the address itself, is silence and not a
// contradiction, so it does not stop the merge. Refusing here instead would make
// the alias do nothing at all on the ordinary corpus — addresses harvested from
// headers that carried no display name stay split forever, and the duplicate the
// user ran the command to fix is still there.
func TestAliasMergesWhenOneSideWasNeverGivenAName(t *testing.T) {
	s := open(t)
	unnamed := person(t, s, "ilana@old.example", "")
	keep := person(t, s, "ilana@new.example", "Ilana Sorbo")
	// the half with no name carries the address as its display name, which is the
	// form the gate has to recognise as silence rather than tokenise into a name
	var shown string
	if err := s.DB().QueryRow(
		`select display_name from people where id=?`, unnamed).Scan(&shown); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(shown, "@") {
		t.Fatalf("display name = %q, want the address this test is about", shown)
	}

	r, err := AddDomainAlias(s, "old.example", "new.example", "")
	if err != nil {
		t.Fatal(err)
	}
	if r.Merged != 1 || len(r.Refused) != 0 {
		t.Fatalf("merged %d, refused %d, want 1 and 0", r.Merged, len(r.Refused))
	}
	if got, err := PersonByIdentity(s, KindEmail, "ilana@old.example"); err != nil ||
		got != keep {
		t.Fatalf("old address resolved to %d (err %v), want %d", got, err, keep)
	}
}

// A role mailbox merges, and merges without its names being consulted. The alias
// asserts the two domains are one organisation, and one organisation's support@
// is one inbox; the names on it are whoever last sent from it, so treating them
// as a contradiction would refuse the merge for a reason that was never a claim
// about identity. The opposite choice also strands the old half: resolution
// already sends every later sighting of support@old to the surviving person, so
// the unmerged one would keep its history and never gain another entry.
func TestAliasMergesARoleMailboxWhateverNamesItCarries(t *testing.T) {
	s := open(t)
	person(t, s, "support@old.example", "Vasa Ngahere")
	keep := person(t, s, "support@new.example", "Kit Tolokau")

	r, err := AddDomainAlias(s, "old.example", "new.example", "rebrand")
	if err != nil {
		t.Fatal(err)
	}
	if r.Merged != 1 || len(r.Refused) != 0 {
		t.Fatalf("merged %d, refused %d, want 1 and 0 — one org's support@ is one inbox",
			r.Merged, len(r.Refused))
	}
	if got, err := PersonByIdentity(s, KindEmail, "support@old.example"); err != nil ||
		got != keep {
		t.Fatalf("old role address resolved to %d (err %v), want %d", got, err, keep)
	}
}

// -dry-run has to be worth trusting, which means writing nothing whatsoever: not
// the alias row either, since an alias recorded without its repair leaves later
// sightings resolving to a person the split half will never join.
func TestAliasPreviewWritesNothing(t *testing.T) {
	s := open(t)
	person(t, s, "delia@old.example", "Delia Rangi")
	person(t, s, "delia@new.example", "Delia Rangi")
	person(t, s, "vasa@old.example", "Vasa Ngahere")
	person(t, s, "vasa@new.example", "Vasa Tolokau")

	preview, err := PreviewDomainAlias(s, "old.example", "new.example")
	if err != nil {
		t.Fatal(err)
	}
	if preview.Applied {
		t.Fatal("a preview reported itself as applied")
	}
	if preview.Merged != 1 || len(preview.Refused) != 1 {
		t.Fatalf("preview: merged %d, refused %d, want 1 and 1",
			preview.Merged, len(preview.Refused))
	}
	if countPeople(t, s) != 4 {
		t.Fatalf("people = %d, want 4 — a preview must not merge", countPeople(t, s))
	}
	aliases, err := DomainAliases(s)
	if err != nil {
		t.Fatal(err)
	}
	if len(aliases) != 0 {
		t.Fatalf("preview recorded %v", aliases)
	}

	// and the apply that follows makes exactly the plan that was reviewed
	applied, err := AddDomainAlias(s, "old.example", "new.example", "")
	if err != nil {
		t.Fatal(err)
	}
	if applied.Merged != preview.Merged || len(applied.Refused) != len(preview.Refused) {
		t.Fatalf("apply merged %d refused %d, preview said %d and %d",
			applied.Merged, len(applied.Refused), preview.Merged, len(preview.Refused))
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

	cands, _, err := MergeCandidates(s)
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

// The trap an alias sets when it refuses. Two humans hold the same local part at
// two domains of one organisation, so the alias declines to fold them — and then
// canonicalisation would send every later sighting of the old address to the
// OTHER person, which is worse than the split it was asked to repair: the split
// was visible and this is not.
func TestAliasedAddressStillResolvesToItsOwnPersonWhenTheAliasRefusedIt(t *testing.T) {
	s := open(t)
	old := person(t, s, "alys@quarry.fed", "Alys Salado")
	cur := person(t, s, "alys@millrace.fed", "Alys Nguyen")
	r, err := AddDomainAlias(s, "quarry.fed", "millrace.fed", "rebrand")
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Refused) != 1 {
		t.Fatalf("refused %d pairs, want the one with two surnames", len(r.Refused))
	}

	got, err := ResolveAddress(s, Address{Addr: "alys@quarry.fed", Name: "Alys Salado"},
		"mail:from-header")
	if err != nil {
		t.Fatal(err)
	}
	if got != old {
		t.Fatalf("a later sighting of alys@quarry.fed resolved to %d, want %d; "+
			"the alias must not hand one human's mail to another", got, old)
	}
	_ = cur
}

// The other half of that: an address the corpus has never seen still follows the
// alias, which is the whole point of declaring one.
func TestAnUnseenAddressOnTheOldDomainStillFollowsTheAlias(t *testing.T) {
	s := open(t)
	cur := person(t, s, "cass@millrace.fed", "Cass Enright")
	if _, err := AddDomainAlias(s, "quarry.fed", "millrace.fed", "rebrand"); err != nil {
		t.Fatal(err)
	}
	got, err := ResolveAddress(s, Address{Addr: "cass@quarry.fed", Name: "Cass Enright"},
		"mail:from-header")
	if err != nil {
		t.Fatal(err)
	}
	if got != cur {
		t.Fatalf("cass@quarry.fed resolved to %d, want the current account %d", got, cur)
	}
}

// A role mailbox at two organisations is two mailboxes, so the pair is not a
// merge to consider and listing it only teaches the reader to skim. It is counted
// instead, because "nothing was left out" and "eighty-eight things were left out"
// must not print the same.
func TestCandidatesLeavesOutRoleMailboxesAtTwoDomains(t *testing.T) {
	s := open(t)
	a := person(t, s, "info@quarry.fed", "Trellis Support")
	b := person(t, s, "info@millrace.fed", "Bramble Support")
	person(t, s, "noreply@quarry.fed", "Quarry")
	person(t, s, "noreply@millrace.fed", "Millrace")

	cs, suppressed, err := MergeCandidates(s)
	if err != nil {
		t.Fatal(err)
	}
	if suppressed != 2 {
		t.Fatalf("suppressed = %d, want the two role-mailbox pairs", suppressed)
	}
	for _, c := range cs {
		if c.AID == a && c.BID == b {
			t.Fatalf("info@ at two firms was listed as a candidate: %+v", c)
		}
	}
}

// A pair a human is asked to judge needs the command that would settle it beside
// it, and for a shared local part across domains there are two honest answers: an
// alias where the domains are one organisation, a merge where one human moved
// employer. The alias must point at the domain still in use.
func TestCandidatesNameTheCommandThatWouldSettleThem(t *testing.T) {
	s := open(t)
	person(t, s, "bryn@quarry.fed", "Bryn Lowther")
	person(t, s, "bryn@millrace.fed", "Bryn Lowther")
	person(t, s, "cass@millrace.fed", "Cass Enright")
	person(t, s, "dai@millrace.fed", "Dai Rhys")

	cs, _, err := MergeCandidates(s)
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 1 {
		t.Fatalf("candidates = %d, want the one split pair: %+v", len(cs), cs)
	}
	if !strings.Contains(cs[0].Suggest, "-from quarry.fed -to millrace.fed") {
		t.Fatalf("suggestion = %q, want the alias toward the live domain", cs[0].Suggest)
	}
	if !strings.Contains(cs[0].Suggest, "corpus merge -keep bryn@millrace.fed") {
		t.Fatalf("suggestion = %q, want the merge that keeps the live account", cs[0].Suggest)
	}
}

// A placeholder has no address, so `corpus merge -keep <email>` cannot name it.
// The suggestion has to be the id form or the refusal names nothing runnable.
func TestCandidatesSuggestIdsForAPersonWithNoAddress(t *testing.T) {
	s := open(t)
	real := person(t, s, "dai.rhys@quarry.fed", "Dai Rhys")
	ghost := placeholder(t, s, "Dai Rhys")

	cs, _, err := MergeCandidates(s)
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 1 {
		t.Fatalf("candidates = %d, want the one name pair: %+v", len(cs), cs)
	}
	want := "-keep-id"
	if !strings.Contains(cs[0].Suggest, want) {
		t.Fatalf("suggestion = %q, want %s: neither side can be named by address",
			cs[0].Suggest, want)
	}
	_, _ = real, ghost
}
