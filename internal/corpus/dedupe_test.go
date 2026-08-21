package corpus

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// All names, addresses and domains here are invented.

func dedupe(t *testing.T, s *Store, apply bool) DedupePlan {
	t.Helper()
	plan, err := Dedupe(s, apply)
	if err != nil {
		t.Fatalf("Dedupe(apply=%v): %v", apply, err)
	}
	return plan
}

func refusalFor(plan DedupePlan, subject string) *Refusal {
	for i := range plan.Refusals {
		if plan.Refusals[i].Subject == subject {
			return &plan.Refusals[i]
		}
	}
	return nil
}

func countPeople(t *testing.T, s *Store) int {
	t.Helper()
	var n int
	if err := s.DB().QueryRow(`select count(*) from people`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// The case rule one exists for: a recipient cc'd as a bare name is a person of
// their own until something says which person, and the thing that says so is the
// rest of the corpus — the human who holds an address appears on every entry the
// bare name does. The address-holder survives, because they are who every later
// sighting and every Slack profile will go on matching.
func TestDedupeFoldsANameOnlyPersonIntoTheHumanTheyName(t *testing.T) {
	s := open(t)
	real := person(t, s, "dai.rhys@quarry.fed", "Dai Rhys")
	ghost := placeholder(t, s, "Dai Rhys")
	onEntry(t, s, "mail:<one@x>", real, ghost)
	onEntry(t, s, "mail:<two@x>", real, ghost)

	plan := dedupe(t, s, true)
	if len(plan.Merges) != 1 {
		t.Fatalf("merges = %d, want 1: %+v", len(plan.Merges), plan.Merges)
	}
	m := plan.Merges[0]
	if m.Rule != RuleSameName {
		t.Fatalf("rule = %q, want %q", m.Rule, RuleSameName)
	}
	if m.KeepID != real || m.DropID != ghost {
		t.Fatalf("kept %d dropped %d, want the address-holder %d to survive",
			m.KeepID, m.DropID, real)
	}
	if strings.Join(m.KeepIDs, " ") == "" || strings.Join(m.DropIDs, " ") == "" {
		t.Fatal("a plan a human is asked to approve must name both sides' identities")
	}
	if countPeople(t, s) != 1 {
		t.Fatalf("people = %d, want 1", countPeople(t, s))
	}
	if plan.Before != 2 || plan.After != 1 {
		t.Fatalf("counts = %d -> %d, want 2 -> 1", plan.Before, plan.After)
	}
}

// Two people of one name that the evidence fits equally is the case where the
// wrong answer is invisible: whichever is chosen, the survivor reads as an
// ordinary person carrying a stranger's mail. So it is refused, and counted, and
// stays two people for `corpus candidates` to keep offering.
func TestDedupeRefusesWhenTwoPeopleOfTheNameFitEqually(t *testing.T) {
	s := open(t)
	one := person(t, s, "dai.rhys@quarry.fed", "Dai Rhys")
	two := person(t, s, "d.rhys@millrace.fed", "Dai Rhys")
	ghost := placeholder(t, s, "Dai Rhys")
	// the placeholder's only entry carries both, so neither is better corroborated
	onEntry(t, s, "mail:<both@x>", one, two, ghost)

	plan := dedupe(t, s, true)
	if len(plan.Merges) != 0 {
		t.Fatalf("merged %+v; with two equal candidates there is nothing to choose",
			plan.Merges)
	}
	r := refusalFor(plan, "dai rhys")
	if r == nil {
		t.Fatalf("the refusal was not reported: %+v", plan.Refusals)
	}
	if !strings.Contains(r.Reason, "equally") {
		t.Fatalf("refusal reason = %q, want it to name the ambiguity", r.Reason)
	}
	if len(r.People) != 3 {
		t.Fatalf("refusal names %v, want all three people", r.People)
	}
	if countPeople(t, s) != 3 {
		t.Fatalf("people = %d, want 3 untouched", countPeople(t, s))
	}
	_ = ghost
}

// A role mailbox is owned by an organisation, not a person, so two of them are
// two mailboxes however alike their local parts are. This is the merge that would
// be worst: info@ at two firms is the busiest address either has, and fusing them
// mixes two companies' correspondence into one entity that looks entirely normal.
func TestDedupeNeverMergesRoleMailboxes(t *testing.T) {
	s := open(t)
	a := person(t, s, "info@quarry.fed", "Trellis Support")
	b := person(t, s, "info@millrace.fed", "Trellis Support")
	if a == b {
		t.Fatal("two domains' info@ arrived as one person")
	}

	plan := dedupe(t, s, true)
	if len(plan.Merges) != 0 {
		t.Fatalf("merged %+v; info@ is not a person", plan.Merges)
	}
	if countPeople(t, s) != 2 {
		t.Fatalf("people = %d, want 2", countPeople(t, s))
	}
}

// The same refusal where every other objection has been removed: one
// organisation, one display name, so only the local parts being role mailboxes
// stands between two shared inboxes and one fused person.
func TestDedupeNeverMergesTwoRoleMailboxesAtOneOrganisation(t *testing.T) {
	s := open(t)
	person(t, s, "info@quarry.fed", "Trellis Support")
	person(t, s, "hello@quarry.fed", "Trellis Support")

	plan := dedupe(t, s, true)
	if len(plan.Merges) != 0 {
		t.Fatalf("merged %+v; a shared inbox is not a person", plan.Merges)
	}
	if countPeople(t, s) != 2 {
		t.Fatalf("people = %d, want 2", countPeople(t, s))
	}
}

// The case rule two exists for: a rebrand and a rename together leave one human
// as two accounts whose local parts differ and whose domains differ, so neither
// the alias repair nor a shared local part can find them. One first name at one
// declared organisation can.
func TestDedupeFoldsOneFirstNameAtOneOrganisation(t *testing.T) {
	s := open(t)
	old := person(t, s, "camille@quarry.fed", "Camille Vaughn")
	cur := person(t, s, "camille.vaughn@millrace.fed", "Camille Vaughn")
	onEntry(t, s, "mail:<later@x>", cur)
	if _, err := AddDomainAlias(s, "quarry.fed", "millrace.fed", "rebrand"); err != nil {
		t.Fatal(err)
	}
	if old == cur {
		t.Fatal("the alias should not have matched: the local parts differ")
	}

	plan := dedupe(t, s, true)
	if len(plan.Merges) != 1 {
		t.Fatalf("merges = %d, want 1: %+v", len(plan.Merges), plan.Merges)
	}
	m := plan.Merges[0]
	if m.Rule != RuleFirstNameOrg {
		t.Fatalf("rule = %q, want %q", m.Rule, RuleFirstNameOrg)
	}
	// the account still in use survives, since it is the one later sightings match
	if m.KeepID != cur || m.DropID != old {
		t.Fatalf("kept %d dropped %d, want the busier account %d to survive",
			m.KeepID, m.DropID, cur)
	}
	for _, addr := range []string{"camille@quarry.fed", "camille.vaughn@millrace.fed"} {
		got, err := PersonByIdentity(s, KindEmail, addr)
		if err != nil {
			t.Fatalf("%s: %v", addr, err)
		}
		if got != cur {
			t.Fatalf("%s resolves to %d, want the survivor %d", addr, got, cur)
		}
	}
}

// Same first name, two employers, nothing saying the two are one: that is two
// people until somebody declares otherwise, and the refusal names the command
// that would declare it.
func TestDedupeDoesNotMergeOneFirstNameAcrossTwoOrganisations(t *testing.T) {
	s := open(t)
	a := person(t, s, "camille@quarry.fed", "Camille Vaughn")
	b := person(t, s, "camille.vaughn@millrace.fed", "Camille Vaughn")

	plan := dedupe(t, s, true)
	if len(plan.Merges) != 0 {
		t.Fatalf("merged %+v across two unrelated domains", plan.Merges)
	}
	r := refusalFor(plan, "camille")
	if r == nil {
		t.Fatalf("no refusal explained the pair: %+v", plan.Refusals)
	}
	if !strings.Contains(r.Reason, "corpus alias") {
		t.Fatalf("refusal reason = %q, want it to name what would settle it", r.Reason)
	}
	if a == b || countPeople(t, s) != 2 {
		t.Fatalf("people = %d, want 2", countPeople(t, s))
	}
}

// Two colleagues of one first name is unremarkable, and their surnames say so.
// Without this gate the rule the user asked for — first name plus company — folds
// them into one person.
func TestDedupeRefusesTwoSurnamesUnderOneFirstName(t *testing.T) {
	s := open(t)
	person(t, s, "bryn@quarry.fed", "Bryn Lowther")
	person(t, s, "bryn.eames@quarry.fed", "Bryn Eames")

	plan := dedupe(t, s, true)
	if len(plan.Merges) != 0 {
		t.Fatalf("merged %+v; two surnames are two people", plan.Merges)
	}
	r := refusalFor(plan, "bryn@quarry.fed")
	if r == nil {
		t.Fatalf("no refusal named the group: %+v", plan.Refusals)
	}
	if !strings.Contains(r.Reason, "surnames") {
		t.Fatalf("refusal reason = %q, want it to name the surname clash", r.Reason)
	}
}

// A From header may put anybody's name on anybody's mailbox — an assistant sends
// on her employer's behalf and the header reads `Bryn Lowther <nia@…>`. The name
// in it is no evidence about whose mailbox that is, so the local part has to
// agree with the name before the address counts as theirs.
func TestDedupeIgnoresAMailboxWhoseAddressContradictsTheName(t *testing.T) {
	s := open(t)
	person(t, s, "bryn@quarry.fed", "Bryn Lowther")
	person(t, s, "nia@quarry.fed", "Bryn Lowther")

	plan := dedupe(t, s, true)
	if len(plan.Merges) != 0 {
		t.Fatalf("merged %+v; nia@ is not Bryn's address", plan.Merges)
	}
	if countPeople(t, s) != 2 {
		t.Fatalf("people = %d, want 2", countPeople(t, s))
	}
}

// A merge is the step a human can get wrong, so it is on the record with the rule
// and the evidence that produced it, and person_merges keeps the dropped id.
func TestDedupeRecordsWhyItMerged(t *testing.T) {
	s := open(t)
	real := person(t, s, "dai.rhys@quarry.fed", "Dai Rhys")
	ghost := placeholder(t, s, "Dai Rhys")
	onEntry(t, s, "mail:<one@x>", real, ghost)

	dedupe(t, s, true)

	var kept, dropped int64
	var reason string
	if err := s.DB().QueryRow(
		`select kept_id, dropped_id, reason from person_merges`).
		Scan(&kept, &dropped, &reason); err != nil {
		t.Fatal(err)
	}
	if kept != real || dropped != ghost {
		t.Fatalf("recorded %d <- %d, want %d <- %d", kept, dropped, real, ghost)
	}
	if !strings.HasPrefix(reason, RuleSameName) || !strings.Contains(reason, "every entry") {
		t.Fatalf("reason = %q, want the rule and its evidence", reason)
	}
}

// The default has to be inert, because everything downstream is read through
// identity and a merge is awkward to walk back. Inert to the byte: not "wrote
// only what does not matter".
func TestDedupeDryRunWritesNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "corpus.db")
	func() {
		s, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		real := person(t, s, "dai.rhys@quarry.fed", "Dai Rhys")
		ghost := placeholder(t, s, "Dai Rhys")
		onEntry(t, s, "mail:<one@x>", real, ghost)
	}()
	before := digest(t, path)

	var plan DedupePlan
	func() {
		s, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		plan = dedupe(t, s, false)
	}()
	if plan.Applied {
		t.Fatal("a dry run reported itself as applied")
	}
	if len(plan.Merges) != 1 {
		t.Fatalf("merges = %d, want the one it would make", len(plan.Merges))
	}
	if plan.Before != 2 || plan.After != 1 {
		t.Fatalf("projection = %d -> %d, want 2 -> 1", plan.Before, plan.After)
	}
	if after := digest(t, path); after != before {
		t.Fatalf("the corpus changed under a dry run: %s -> %s", before, after)
	}
}

// A second apply must find nothing: the placeholder is gone and the group it was
// found in is one person. Anything else means the rules feed on their own output.
func TestDedupeAppliedTwiceIsANoOp(t *testing.T) {
	s := open(t)
	real := person(t, s, "dai.rhys@quarry.fed", "Dai Rhys")
	ghost := placeholder(t, s, "Dai Rhys")
	onEntry(t, s, "mail:<one@x>", real, ghost)
	other := person(t, s, "camille@quarry.fed", "Camille Vaughn")
	person(t, s, "camille.vaughn@millrace.fed", "Camille Vaughn")
	if _, err := AddDomainAlias(s, "millrace.fed", "quarry.fed", "rebrand"); err != nil {
		t.Fatal(err)
	}

	first := dedupe(t, s, true)
	if len(first.Merges) != 2 {
		t.Fatalf("first pass merged %d, want 2: %+v", len(first.Merges), first.Merges)
	}
	people := countPeople(t, s)

	second := dedupe(t, s, true)
	if len(second.Merges) != 0 {
		t.Fatalf("second pass merged %+v", second.Merges)
	}
	if countPeople(t, s) != people {
		t.Fatalf("people went %d -> %d on a second pass", people, countPeople(t, s))
	}
	var n int
	if err := s.DB().QueryRow(`select count(*) from person_merges`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("person_merges holds %d rows, want the 2 real merges", n)
	}
	_ = other
}

func digest(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// The sidecars are checkpointed away by the last Close, so their absence is
	// part of what is being compared: a stray -wal would be a write.
	for _, suffix := range []string{"-wal", "-shm"} {
		if _, err := os.Stat(path + suffix); err == nil {
			t.Fatalf("%s%s survived Close; the comparison would not see writes in it",
				path, suffix)
		}
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func TestGenericLocalPart(t *testing.T) {
	for _, c := range []struct {
		local string
		want  bool
	}{
		{"info", true},
		{"no-reply", true},
		{"sales-team", true}, // a role word inside a separated local part
		{"corporatesales.support", true},
		{"noreply+42", true}, // a plus tag does not disguise it
		{"manager.easterncreek", true},
		{"hr", true},
		{"tom", false},
		{"dai.rhys", false},
		{"l.mccorley", false}, // "hr" and friends must not fire from inside a name
		{"bryn", false},
	} {
		if got := genericLocalPart(c.local); got != c.want {
			t.Errorf("genericLocalPart(%q) = %v, want %v", c.local, got, c.want)
		}
	}
}

func TestAddressNamesPerson(t *testing.T) {
	for _, c := range []struct {
		local, name string
		want        bool
	}{
		{"bryn", "Bryn Lowther", true},
		{"bryn.lowther", "Bryn Lowther", true},
		{"brynlowther88", "Bryn Lowther", true},
		{"blowther", "Bryn Lowther", true}, // initial and surname
		{"blowth", "Bryn Lowther", true},   // initial and the start of the surname
		{"nia", "Bryn Lowther", false},     // somebody else's mailbox
		{"reception", "Bryn Lowther", false},
		{"bl", "Bryn Lowther", false},                // two letters name nobody
		{"support", "Nia Prentice (Support)", false}, // the annotation is not a name
		{"arvida", "Klara Belk | Arvida", false},
	} {
		if got := addressNamesPerson(c.local, c.name); got != c.want {
			t.Errorf("addressNamesPerson(%q, %q) = %v, want %v",
				c.local, c.name, got, c.want)
		}
	}
}
