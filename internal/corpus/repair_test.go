package corpus

import (
	"fmt"
	"strings"
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

// The shape the repair below exists for. Every recipient in a real trail was
// written as a hyperlinked address and the recipient list wrapped, so the fold
// fell between a display name and the bracket introducing its address.
var foldedCc = []string{
	`Ada Fenwick <ada.fenwick@quarry.fed <mailto:ada.fenwick@quarry.fed>>; Bram Teale <`,
	`bram.teale@quarry.fed <mailto:bram.teale@quarry.fed>>; Cleo Ward <`,
	`cleo.ward@quarry.fed <mailto:cleo.ward@quarry.fed>>`,
}

// A folded value is flattened by joining its lines with the same comma that
// separates two recipients, which is what puts a delimiter in the middle of an
// address.
func flatten(lines []string) string { return strings.Join(lines, ", ") }

// Cutting a recipient at the bracket loses the address outright: the leading
// half becomes a name-only person and the address behind the fold is credited to
// nobody. Both halves are the same human, and no later sighting can supply what
// was dropped, so the parse must not make the cut in the first place.
func TestParsingAFoldedRecipientListKeepsEveryAddress(t *testing.T) {
	got := ParseAddresses(flatten(foldedCc))
	want := []Address{
		{Name: "Ada Fenwick", Addr: "ada.fenwick@quarry.fed"},
		{Name: "Bram Teale", Addr: "bram.teale@quarry.fed"},
		{Name: "Cleo Ward", Addr: "cleo.ward@quarry.fed"},
	}
	if len(got) != len(want) {
		t.Fatalf("parsed %d addresses, want %d: %+v", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i].Addr != w.Addr || got[i].Name != w.Name {
			t.Errorf("address %d = %q %q, want %q %q",
				i, got[i].Name, got[i].Addr, w.Name, w.Addr)
		}
	}
}

// And recording that header stores three addresses and no placeholder, which is
// the property that keeps the repair below from having anything to do.
func TestRecordingAFoldedHeaderCreatesNoPlaceholder(t *testing.T) {
	s := open(t)
	e := entry("mail:<folded@x>", "confirming the meter numbers")
	rec, err := s.Put(e, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	ids, err := RecordHeader(s, rec.ID, RoleCc, flatten(foldedCc))
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 3 {
		t.Fatalf("recorded %d participants, want 3", len(ids))
	}
	var n int
	if err := s.DB().QueryRow(
		`select count(*) from identities where kind=? and value like '%<'`,
		KindDisplayName).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("%d name-only identities were cut off at a bracket", n)
	}
	if err := s.DB().QueryRow(`select count(*) from people`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("people: got %d, want 3 — one per recipient", n)
	}
}

// Each shape seen in the corpus, including a mailbox address that is a role
// rather than a person, is recognised as corrupt: no display name legitimately
// ends in a bracket.
func TestTruncatedNamesAreRecognisedWhateverTheyName(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"ada fenwick <", "ada fenwick"},
		{"bram mcteale <", "bram mcteale"},
		{"review <", "review"},
		{"cleo ward-hunt <", "cleo ward-hunt"},
		{`"Dai Rhys" <`, "Dai Rhys"},
		{"ada fenwick  <  ", "ada fenwick"},
	} {
		if got := CleanDisplayName(tc.in); got != tc.want {
			t.Errorf("CleanDisplayName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// A bracket anywhere but the end is part of the name as written. A name is the
// whole of a placeholder's evidence, so trimming inside it would leave a
// different person's name behind.
func TestABracketInsideANameIsNotTruncation(t *testing.T) {
	for _, name := range []string{
		"pipe < valve co",
		"ada <the second> fenwick",
		"<no reply> desk",
	} {
		if got := CleanDisplayName(name); got != name {
			t.Errorf("CleanDisplayName(%q) = %q, want it untouched", name, got)
		}
	}
	s := open(t)
	id := placeholder(t, s, "pipe < valve co")
	if _, err := RepairTruncatedNames(s); err != nil {
		t.Fatal(err)
	}
	if got := nameOf(t, s, id); got != "pipe < valve co" {
		t.Fatalf("display name = %q, want it untouched", got)
	}
	if _, err := PersonByIdentity(s, KindDisplayName, "pipe < valve co"); err != nil {
		t.Fatalf("the identity was rewritten: %v", err)
	}
}

// The merge this repair is for: a placeholder whose name belongs to somebody who
// holds an address and who is on every conversation the placeholder is on. That
// is the fingerprint of the cut — one sighting of a header lost the address, the
// other carried it — and folding them is what puts the messages back on one
// person.
func TestRepairMergesAPlaceholderIntoTheAddressBackedPersonOfThatName(t *testing.T) {
	s := open(t)
	ada := person(t, s, "ada.fenwick@quarry.fed", "Ada Fenwick")
	ghost := placeholder(t, s, "Ada Fenwick <")
	onEntry(t, s, "mail:<one@x>", ada, ghost)
	onEntry(t, s, "mail:<two@x>", ada, ghost)

	r, err := RepairTruncatedNames(s)
	if err != nil {
		t.Fatal(err)
	}
	if r.Merged != 1 || len(r.Declined) != 0 {
		t.Fatalf("repair = %+v, want one merge and nothing declined", r)
	}
	var n int
	if err := s.DB().QueryRow(`select count(*) from people`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("people: got %d, want 1", n)
	}
	// the survivor is the address-backed half, and the name now resolves to them
	if got, err := PersonByIdentity(s, KindDisplayName, "ada fenwick"); err != nil {
		t.Fatalf("the cleaned name resolves to nobody: %v", err)
	} else if got != ada {
		t.Fatalf("the name resolves to %d, want %d", got, ada)
	}
	if got := nameOf(t, s, ada); got != "Ada Fenwick" {
		t.Fatalf("display name = %q, want the bracket gone", got)
	}
	// and the merge is recorded, naming the value that caused it, so it is both
	// auditable and reversible
	var kept, dropped int64
	var reason string
	if err := s.DB().QueryRow(
		`select kept_id, dropped_id, reason from person_merges`).
		Scan(&kept, &dropped, &reason); err != nil {
		t.Fatal(err)
	}
	if kept != ada || dropped != ghost {
		t.Fatalf("merge recorded %d <- %d, want %d <- %d", kept, dropped, ada, ghost)
	}
	if !strings.HasPrefix(reason, "repair:truncated-name (") {
		t.Fatalf("merge reason = %q, want it attributed to the repair", reason)
	}
}

// Two placeholders are not evidence of anything. Neither holds an address, so
// folding one into the other only guesses which of two same-named people the
// mail belongs to — and it is reported instead, where a human can see both.
func TestRepairWillNotFoldOnePlaceholderIntoAnother(t *testing.T) {
	s := open(t)
	named := placeholder(t, s, "Ada Fenwick")
	ghost := placeholder(t, s, "Ada Fenwick <")
	onEntry(t, s, "mail:<one@x>", named, ghost)

	r, err := RepairTruncatedNames(s)
	if err != nil {
		t.Fatal(err)
	}
	if r.Merged != 0 || len(r.Declined) != 1 {
		t.Fatalf("repair = %+v, want no merge and one decline", r)
	}
	if r.Declined[0].Reason != "no person of that name holds a mailbox a human answers" {
		t.Fatalf("decline reason = %q", r.Declined[0].Reason)
	}
	var n int
	if err := s.DB().QueryRow(`select count(*) from people`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("people: got %d, want both left alone", n)
	}
	if !candidatePair(t, s, named, ghost) {
		t.Fatal("the declined pair is invisible: it is in no candidate")
	}
}

// Two people of one name that the evidence cannot separate is the case
// RepairMailtoIdentities refuses on too, for the same reason: the wrong choice
// hands one human's mail to another, and Merge is deliberately hard to undo.
func TestRepairRefusesWhenTwoPeopleFitTheNameEquallyWell(t *testing.T) {
	s := open(t)
	one := person(t, s, "ada.fenwick@quarry.fed", "Ada Fenwick")
	two := person(t, s, "a.fenwick@millrace.example", "Ada Fenwick")
	ghost := placeholder(t, s, "Ada Fenwick <")
	onEntry(t, s, "mail:<one@x>", one, two, ghost)

	r, err := RepairTruncatedNames(s)
	if err != nil {
		t.Fatal(err)
	}
	if r.Merged != 0 || len(r.Declined) != 1 {
		t.Fatalf("repair = %+v, want no merge and one decline", r)
	}
	if r.Declined[0].Reason != "2 people of that name fit equally well" {
		t.Fatalf("decline reason = %q", r.Declined[0].Reason)
	}
	if len(r.Declined[0].Candidates) != 2 {
		t.Fatalf("declined names %v, want both people it could be",
			r.Declined[0].Candidates)
	}
	var n int
	if err := s.DB().QueryRow(`select count(*) from people`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("people: got %d, want all three left alone", n)
	}
	// both readings are on the review list, so the refusal is visible
	if !candidatePair(t, s, one, ghost) || !candidatePair(t, s, two, ghost) {
		t.Fatal("a refused reading is in no candidate")
	}
}

// A namesake who shares no conversation with the placeholder is a namesake, not
// the same human. The name is still cleaned — the next correctly-parsed sighting
// should land on this person rather than mint a third — but nothing is folded.
func TestRepairDeclinesANamesakeFromAnotherConversation(t *testing.T) {
	s := open(t)
	ada := person(t, s, "ada.fenwick@quarry.fed", "Ada Fenwick")
	ghost := placeholder(t, s, "Ada Fenwick <")
	onEntry(t, s, "mail:<hers@x>", ada)
	onEntry(t, s, "mail:<theirs@x>", ghost)

	r, err := RepairTruncatedNames(s)
	if err != nil {
		t.Fatal(err)
	}
	if r.Merged != 0 || r.Cleaned != 1 || len(r.Declined) != 1 {
		t.Fatalf("repair = %+v, want the name cleaned and the merge declined", r)
	}
	if r.Declined[0].Reason != "shares no entry with the person of that name" {
		t.Fatalf("decline reason = %q", r.Declined[0].Reason)
	}
	got, err := PersonByIdentity(s, KindDisplayName, "ada fenwick")
	if err != nil {
		t.Fatal(err)
	}
	if got != ghost {
		t.Fatalf("the cleaned name moved to %d, want the person who held it, %d",
			got, ghost)
	}
	if !candidatePair(t, s, ada, ghost) {
		t.Fatal("the declined pair is in no candidate")
	}
}

// Nobody of that name at all: the placeholder keeps every message it is on and
// only loses the bracket, so a later sighting of the name resolves here instead
// of creating yet another person.
func TestRepairCleansALonePlaceholder(t *testing.T) {
	s := open(t)
	ghost := placeholder(t, s, "Bram Teale <")
	onEntry(t, s, "mail:<one@x>", ghost)

	r, err := RepairTruncatedNames(s)
	if err != nil {
		t.Fatal(err)
	}
	if r.Merged != 0 || r.Cleaned != 1 || r.Renamed != 1 {
		t.Fatalf("repair = %+v, want the one name cleaned and renamed", r)
	}
	if r.Declined[0].Reason != "no other person of that name" {
		t.Fatalf("decline reason = %q", r.Declined[0].Reason)
	}
	got, err := PersonByIdentity(s, KindDisplayName, "bram teale")
	if err != nil {
		t.Fatal(err)
	}
	if got != ghost {
		t.Fatalf("the cleaned name resolves to %d, want %d", got, ghost)
	}
	if name := nameOf(t, s, ghost); name != "Bram Teale" {
		t.Fatalf("display name = %q, want the bracket gone", name)
	}
}

// Whatever the repair decided, deciding it again must change nothing: it is a
// pass to run after every ingest, not a one-shot script.
func TestRepairTruncatedNamesIsIdempotent(t *testing.T) {
	s := open(t)
	ada := person(t, s, "ada.fenwick@quarry.fed", "Ada Fenwick")
	merged := placeholder(t, s, "Ada Fenwick <")
	onEntry(t, s, "mail:<one@x>", ada, merged)
	// a decline as well as a merge, since the two leave different rows behind
	declined := placeholder(t, s, "Cleo Ward <")
	sameName := placeholder(t, s, "Cleo Ward")
	onEntry(t, s, "mail:<two@x>", declined, sameName)

	if _, err := RepairTruncatedNames(s); err != nil {
		t.Fatal(err)
	}
	before := snapshot(t, s)

	again, err := RepairTruncatedNames(s)
	if err != nil {
		t.Fatal(err)
	}
	if again.Merged != 0 || again.Cleaned != 0 || again.Renamed != 0 {
		t.Fatalf("second run = %+v, want nothing changed", again)
	}
	if after := snapshot(t, s); after != before {
		t.Fatalf("second run changed the corpus:\n%s\nwas\n%s", after, before)
	}
}

// Two humans who share nothing but a corpus must come out the other side as two
// humans, whichever of them the truncated name resembles.
func TestRepairTruncatedNamesDoesNotMergeDifferentPeople(t *testing.T) {
	s := open(t)
	ada := person(t, s, "ada.fenwick@quarry.fed", "Ada Fenwick")
	bram := person(t, s, "bram.teale@quarry.fed", "Bram Teale")
	ghost := placeholder(t, s, "Cleo Ward <")
	onEntry(t, s, "mail:<one@x>", ada, bram, ghost)

	r, err := RepairTruncatedNames(s)
	if err != nil {
		t.Fatal(err)
	}
	if r.Merged != 0 {
		t.Fatalf("merged %d pairs of unrelated people", r.Merged)
	}
	for _, addr := range []string{"ada.fenwick@quarry.fed", "bram.teale@quarry.fed"} {
		if _, err := PersonByIdentity(s, KindEmail, addr); err != nil {
			t.Fatalf("%s: %v", addr, err)
		}
	}
	if got := nameOf(t, s, ghost); got != "Cleo Ward" {
		t.Fatalf("the placeholder is now %q", got)
	}
	if ada == bram || ada == ghost {
		t.Fatal("distinct people collapsed")
	}
}

// placeholder is what a cut recipient leaves behind: a person known by a name and
// nothing else, exactly as the quoted-header path records one.
func placeholder(t *testing.T, s *Store, name string) int64 {
	t.Helper()
	id, err := ResolveWithRule(s, KindDisplayName, name, name, "quote:cc-header:name-only")
	if err != nil {
		t.Fatalf("resolving %q: %v", name, err)
	}
	return id
}

// onEntry puts an entry and cc's everyone named on it, which is the corroboration
// the repair weighs.
func onEntry(t *testing.T, s *Store, ext string, people ...int64) int64 {
	t.Helper()
	rec, err := s.Put(entry(ext, "about the meter numbers"), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range people {
		if err := Participate(s, rec.ID, p, RoleCc); err != nil {
			t.Fatal(err)
		}
	}
	return rec.ID
}

func candidatePair(t *testing.T, s *Store, a, b int64) bool {
	t.Helper()
	cs, _, err := MergeCandidates(s)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cs {
		if (c.AID == a && c.BID == b) || (c.AID == b && c.BID == a) {
			return true
		}
	}
	return false
}

// The case this exists for: one Gmail mailbox arrived under twenty-two different
// +tags, each tag its own identity value and so its own person, each holding a
// share of the counts. Fixing the parse cannot undo that — identities are keyed
// on the value — so the stored rows are folded, and the tag itself is kept.
func TestRepairFoldsTaggedAddressesIntoTheirBaseMailbox(t *testing.T) {
	s := open(t)
	base := person(t, s, "dai@post.fed", "Dai Rhys")
	tags := []string{"dai+salsa@post.fed", "dai+books@post.fed", "dai+gst@post.fed"}
	var split []int64
	for _, tag := range tags {
		split = append(split, taggedPerson(t, s, tag, ""))
	}
	e := entry("mail:<ask@x>", "the invoice for the salsa order")
	rec, err := s.Put(e, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := Participate(s, rec.ID, split[0], RoleTo); err != nil {
		t.Fatal(err)
	}

	r, err := RepairPlusAddresses(s)
	if err != nil {
		t.Fatalf("RepairPlusAddresses: %v", err)
	}
	if r.Merged != 3 || r.Anchored != 0 || len(r.Left) != 0 {
		t.Fatalf("repair = %+v, want 3 merged and nothing left", r)
	}

	var n int
	if err := s.DB().QueryRow(`select count(*) from people`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("people: got %d, want 1", n)
	}
	// the base mailbox's person survives, and the tags are still on the record
	for _, tag := range tags {
		got, err := PersonByIdentity(s, KindEmail, tag)
		if err != nil {
			t.Fatalf("the tag %s was not kept: %v", tag, err)
		}
		if got != base {
			t.Fatalf("%s belongs to %d, want the base mailbox's person %d", tag, got, base)
		}
	}
	if err := s.DB().QueryRow(
		`select count(*) from participants where person_id=? and role='to'`, base).
		Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("the folded person's participation did not move: %d rows", n)
	}
	var reason string
	if err := s.DB().QueryRow(
		`select reason from person_merges where dropped_id=?`, split[0]).Scan(&reason); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reason, "repair:plus-address") ||
		!strings.Contains(reason, "dai+salsa@post.fed") {
		t.Errorf("merge reason %q names neither the rule nor the value", reason)
	}
}

// A tag whose base mailbox nobody holds: there is nobody to fold into, so the
// mailbox is recorded on the person who holds the tag and the next tag lands on
// them instead of minting another.
func TestRepairRecordsABaseMailboxNobodyHolds(t *testing.T) {
	s := open(t)
	taggedPerson(t, s, "dai+salsa@post.fed", "Dai Rhys")
	taggedPerson(t, s, "dai+books@post.fed", "")

	r, err := RepairPlusAddresses(s)
	if err != nil {
		t.Fatal(err)
	}
	if r.Anchored != 1 || r.Merged != 1 {
		t.Fatalf("repair = %+v, want one mailbox recorded and one person folded", r)
	}
	base, err := PersonByIdentity(s, KindEmail, "dai@post.fed")
	if err != nil {
		t.Fatalf("the base mailbox was not recorded: %v", err)
	}
	// both tags and the mailbox are one person, whichever tag was reached first
	for _, tag := range []string{"dai+salsa@post.fed", "dai+books@post.fed"} {
		got, err := PersonByIdentity(s, KindEmail, tag)
		if err != nil {
			t.Fatalf("the tag %s was not kept: %v", tag, err)
		}
		if got != base {
			t.Fatalf("%s belongs to %d, want the mailbox's person %d", tag, got, base)
		}
	}
	var n int
	if err := s.DB().QueryRow(`select count(*) from people`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("people: got %d, want 1", n)
	}
}

// A person named by the tag they arrived as is renamed to the mailbox: the tag
// describes one signup, and the person is all of them.
func TestRepairNamesATaggedPersonForTheirMailbox(t *testing.T) {
	s := open(t)
	id := taggedPerson(t, s, "dai+salsa@post.fed", "")
	r, err := RepairPlusAddresses(s)
	if err != nil {
		t.Fatal(err)
	}
	if r.Renamed != 1 {
		t.Fatalf("repair = %+v, want one person renamed", r)
	}
	var name string
	if err := s.DB().QueryRow(`select display_name from people where id=?`, id).
		Scan(&name); err != nil {
		t.Fatal(err)
	}
	if name != "dai@post.fed" {
		t.Errorf("display name = %q, want the base mailbox", name)
	}
}

// A forwarding artefact's tag names a different mailbox, so folding it would file
// somebody else's mail under the address it is written on. Refused and reported.
func TestRepairRefusesAForwardingArtefact(t *testing.T) {
	s := open(t)
	dai := person(t, s, "dai@post.fed", "Dai Rhys")
	fwd, err := ResolveWithRule(s, KindEmail, "dai+caf_=bram=post.fed@post.fed", "", "mail:to-header")
	if err != nil {
		t.Fatal(err)
	}
	// a plus in the DOMAIN is not a subaddress and is not even a refusal
	inDomain := person(t, s, "cleo@post+west.fed", "Cleo Fenwick")

	r, err := RepairPlusAddresses(s)
	if err != nil {
		t.Fatal(err)
	}
	if r.Merged != 0 || len(r.Left) != 1 {
		t.Fatalf("repair = %+v, want nothing merged and one value left", r)
	}
	if got, err := PersonByIdentity(s, KindEmail, "dai+caf_=bram=post.fed@post.fed"); err != nil ||
		got != fwd || got == dai {
		t.Fatalf("the artefact resolved to %d (%v), want its own person %d", got, err, fwd)
	}
	if got, err := PersonByIdentity(s, KindEmail, "cleo@post+west.fed"); err != nil || got != inDomain {
		t.Fatalf("the domain plus resolved to %d (%v), want %d untouched", got, err, inDomain)
	}
}

// The other half of the fold: cut inside the bracket, the address survives welded
// into the display name. Unlike the truncated shape the address is recoverable,
// so the fragment splits and folds into the person holding it.
func TestRepairSplitsAWeldedAddressAndFoldsItIntoTheAddressHolder(t *testing.T) {
	s := open(t)
	real := person(t, s, "dai@post.fed", "Dai Rhys")
	welded, err := ResolveWithRule(s, KindDisplayName,
		"Dai Rhys <dai@post.fed", "Dai Rhys <dai@post.fed", "quote:to-header:name-only")
	if err != nil {
		t.Fatal(err)
	}
	if welded == real {
		t.Fatal("expected two people before the repair")
	}

	r, err := RepairTruncatedNames(s)
	if err != nil {
		t.Fatalf("RepairTruncatedNames: %v", err)
	}
	if r.Merged != 1 || r.Welded != 1 || len(r.Declined) != 0 {
		t.Fatalf("repair = %+v, want one welded address split and folded", r)
	}
	var n int
	if err := s.DB().QueryRow(`select count(*) from people`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("people: got %d, want 1", n)
	}
	// the name half is kept as an identity of the survivor, without the address
	got, err := PersonByIdentity(s, KindDisplayName, "dai rhys")
	if err != nil {
		t.Fatalf("the name half was not kept: %v", err)
	}
	if got != real {
		t.Fatalf("the name half belongs to %d, want %d", got, real)
	}
	var reason string
	if err := s.DB().QueryRow(
		`select reason from person_merges where dropped_id=?`, welded).Scan(&reason); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reason, "repair:welded-address") {
		t.Errorf("merge reason %q does not name the rule", reason)
	}
}

// Nobody holds the welded address, so there is nothing to fold into — but the
// address is right there, so the person becomes reachable by it.
func TestRepairGivesAWeldedAddressToThePersonCarryingIt(t *testing.T) {
	s := open(t)
	id, err := ResolveWithRule(s, KindDisplayName,
		"Bram Fenwick <bram@post.fed", "Bram Fenwick <bram@post.fed", "quote:cc-header:name-only")
	if err != nil {
		t.Fatal(err)
	}
	r, err := RepairTruncatedNames(s)
	if err != nil {
		t.Fatal(err)
	}
	if r.Merged != 0 || r.Welded != 1 {
		t.Fatalf("repair = %+v, want the address recovered and nothing merged", r)
	}
	got, err := PersonByIdentity(s, KindEmail, "bram@post.fed")
	if err != nil {
		t.Fatalf("the welded address was not recovered: %v", err)
	}
	if got != id {
		t.Fatalf("the address went to %d, want %d", got, id)
	}
}

// `Michael Vantel <ellen@…>` is a real header — a person writing from somebody
// else's mailbox — so a welded address on a differently-named person is two
// humans, not one. Refused, left visible, and reported as a candidate.
func TestRepairRefusesAWeldedAddressHeldByADifferentlyNamedPerson(t *testing.T) {
	s := open(t)
	owner := person(t, s, "ellen@post.fed", "Ellen Sowerby")
	welded, err := ResolveWithRule(s, KindDisplayName,
		"Bram Fenwick <ellen@post.fed", "Bram Fenwick <ellen@post.fed", "quote:from-header:name-only")
	if err != nil {
		t.Fatal(err)
	}

	r, err := RepairTruncatedNames(s)
	if err != nil {
		t.Fatal(err)
	}
	if r.Merged != 0 || len(r.Declined) != 1 {
		t.Fatalf("repair = %+v, want nothing merged and one refusal", r)
	}
	if !strings.Contains(r.Declined[0].Reason, "different surname") {
		t.Errorf("refusal reason = %q", r.Declined[0].Reason)
	}
	var n int
	if err := s.DB().QueryRow(`select count(*) from people`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("people: got %d, want the two the repair refused to fold", n)
	}
	// left as it was, so the pair is still there to be reviewed
	cs, _, err := MergeCandidates(s)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, c := range cs {
		if (c.AID == welded && c.BID == owner) || (c.AID == owner && c.BID == welded) {
			found = true
		}
	}
	if !found {
		t.Fatalf("the refused pair is not in %d candidates", len(cs))
	}
}

// A display name holding a bracket and no address is that person's whole
// evidence: neither repair may touch it.
func TestRepairLeavesANameWithABracketAndNoAddressAlone(t *testing.T) {
	s := open(t)
	id, err := ResolveWithRule(s, KindDisplayName,
		"nights < weekends roster", "nights < weekends roster", "quote:to-header:name-only")
	if err != nil {
		t.Fatal(err)
	}
	other := person(t, s, "roster@post.fed", "nights < weekends roster")
	r, err := RepairTruncatedNames(s)
	if err != nil {
		t.Fatal(err)
	}
	if r.Merged != 0 || r.Welded != 0 || r.Cleaned != 0 || r.Renamed != 0 {
		t.Fatalf("repair = %+v, want nothing touched", r)
	}
	if got, err := PersonByIdentity(s, KindDisplayName, "nights < weekends roster"); err != nil ||
		got != id || id == other {
		t.Fatalf("the name became %d (%v), want %d untouched", got, err, id)
	}
}

// Both repairs are run again over what they already fixed: a corpus that is
// repaired twice must be the corpus that was repaired once.
func TestBothRepairsAreIdempotent(t *testing.T) {
	s := open(t)
	person(t, s, "dai@post.fed", "Dai Rhys")
	for _, tag := range []string{"dai+salsa@post.fed", "dai+books@post.fed"} {
		taggedPerson(t, s, tag, "")
	}
	if _, err := ResolveWithRule(s, KindDisplayName,
		"Dai Rhys <dai@post.fed", "Dai Rhys <dai@post.fed", "quote:to-header:name-only"); err != nil {
		t.Fatal(err)
	}
	// somebody else already holds the clean name, so the welded value cannot be
	// rewritten and stays for every later pass to see
	if _, err := ResolveWithRule(s, KindDisplayName,
		"Dai Rhys", "Dai Rhys", "quote:cc-header:name-only"); err != nil {
		t.Fatal(err)
	}
	person(t, s, "ellen@post.fed", "Ellen Sowerby")
	if _, err := ResolveWithRule(s, KindDisplayName,
		"Bram Fenwick <ellen@post.fed", "Bram Fenwick <ellen@post.fed", "quote:from-header:name-only"); err != nil {
		t.Fatal(err)
	}

	if _, err := RepairPlusAddresses(s); err != nil {
		t.Fatal(err)
	}
	if _, err := RepairTruncatedNames(s); err != nil {
		t.Fatal(err)
	}
	before := snapshot(t, s)

	pr, err := RepairPlusAddresses(s)
	if err != nil {
		t.Fatal(err)
	}
	tr, err := RepairTruncatedNames(s)
	if err != nil {
		t.Fatal(err)
	}
	if pr.Merged != 0 || pr.Anchored != 0 || pr.Renamed != 0 {
		t.Errorf("the second plus-address pass did work: %+v", pr)
	}
	if tr.Merged != 0 || tr.Cleaned != 0 || tr.Renamed != 0 || tr.Welded != 0 {
		t.Errorf("the second name pass did work: %+v", tr)
	}
	if after := snapshot(t, s); after != before {
		t.Errorf("the second pass changed the corpus:\n%s\nwant:\n%s", after, before)
	}
}

// taggedPerson stores a tagged address the way an ingest before the parse fix
// stored it: its own person, keyed on the tag. Written directly, because
// resolution now files a tag under its base mailbox and so cannot produce the
// corpus this repair exists for.
func taggedPerson(t *testing.T, s *Store, addr, name string) int64 {
	t.Helper()
	if name == "" {
		name = addr
	}
	res, err := s.DB().Exec(`insert into people (display_name) values (?)`, name)
	if err != nil {
		t.Fatal(err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB().Exec(
		`insert into identities (person_id, kind, value, rule) values (?,?,?,?)`,
		id, KindEmail, addr, "mail:to-header"); err != nil {
		t.Fatal(err)
	}
	return id
}
