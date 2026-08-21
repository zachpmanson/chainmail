package corpus

import (
	"strings"
	"testing"
)

// All names, addresses and domains here are invented. The real cases they stand
// in for are described in words only.

func TestResolveIsFindOrCreateAndRecordsWhichRuleMatched(t *testing.T) {
	s := open(t)

	first, err := Resolve(s, KindEmail, "Alice@Example.com", "Alice A")
	if err != nil {
		t.Fatalf("first resolve: %v", err)
	}
	// case and surrounding angle brackets are not identity
	again, err := ResolveWithRule(s, KindEmail, "<alice@example.com>", "", "mail:to-header")
	if err != nil {
		t.Fatalf("second resolve: %v", err)
	}
	if again != first {
		t.Fatalf("same address resolved to %d then %d", first, again)
	}

	var n int
	if err := s.DB().QueryRow(`select count(*) from people`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("people: got %d, want 1", n)
	}
	// the rule of the FIRST match is what is kept: it is the step that created
	// the identity, and therefore the step a bad merge has to be traced to
	var rule string
	if err := s.DB().QueryRow(
		`select rule from identities where kind='email' and value='alice@example.com'`,
	).Scan(&rule); err != nil {
		t.Fatal(err)
	}
	if rule != "auto:email" {
		t.Fatalf("rule: got %q, want auto:email", rule)
	}
}

func TestResolveUpgradesAnAddressOnlyNameButNeverDowngradesAName(t *testing.T) {
	s := open(t)

	// first sighting had no display name at all, so the address is the name
	id, err := Resolve(s, KindEmail, "bob@example.com", "")
	if err != nil {
		t.Fatal(err)
	}
	if got := nameOf(t, s, id); got != "bob@example.com" {
		t.Fatalf("placeholder name: got %q", got)
	}
	if _, err := Resolve(s, KindEmail, "bob@example.com", "Bob B"); err != nil {
		t.Fatal(err)
	}
	if got := nameOf(t, s, id); got != "Bob B" {
		t.Fatalf("after a named sighting: got %q, want Bob B", got)
	}
	// a later address-shaped "name" must not undo that
	if _, err := Resolve(s, KindEmail, "bob@example.com", "bob@example.com"); err != nil {
		t.Fatal(err)
	}
	if got := nameOf(t, s, id); got != "Bob B" {
		t.Fatalf("name was downgraded to %q", got)
	}
}

func TestParseAddressesSurvivesQuotedCommasAndJunk(t *testing.T) {
	// a display name with a comma inside quotes is one recipient, not two
	got := ParseAddresses(`"Example, Alice" <alice@example.com>, bob@example.com`)
	if len(got) != 2 {
		t.Fatalf("quoted comma: got %d addresses, want 2: %+v", len(got), got)
	}
	if got[0].Addr != "alice@example.com" || got[0].Name != "Example, Alice" {
		t.Fatalf("first: %+v", got[0])
	}

	// one malformed fragment fails net/mail's all-or-nothing list parse; the
	// fallback must still surface the good ones AND the name-only participant,
	// because a recipient we cannot parse is still a recipient
	got = ParseAddresses(`Ben, Johan <johan@example.com>, not-an-address-at-all`)
	var names, addrs []string
	for _, a := range got {
		if a.Addr != "" {
			addrs = append(addrs, a.Addr)
		} else {
			names = append(names, a.Name)
		}
	}
	if len(addrs) != 1 || addrs[0] != "johan@example.com" {
		t.Fatalf("addresses from junk header: %v", addrs)
	}
	if len(names) != 2 || names[0] != "Ben" {
		t.Fatalf("name-only participants: %v, want Ben and the junk fragment", names)
	}

	if ParseAddresses("   ") != nil {
		t.Fatal("an empty header should yield nothing, not a phantom participant")
	}
}

func TestRecipientOnlyParticipantsAreVisible(t *testing.T) {
	s := open(t)
	sender, err := Resolve(s, KindEmail, "alice@example.com", "Alice A")
	if err != nil {
		t.Fatal(err)
	}
	e := entry("mail:<decision@x>", "going with option two")
	e.PersonID = sender
	r, err := s.Put(e, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := Participate(s, r.ID, sender, RoleFrom); err != nil {
		t.Fatal(err)
	}
	if _, err := RecordHeader(s, r.ID, RoleTo, `Bob B <bob@example.com>`); err != nil {
		t.Fatal(err)
	}
	// the case that motivated this: people who never send anything, one of them
	// a first name only in someone else's quoted header
	if _, err := RecordHeader(s, r.ID, RoleCc,
		`carol@example.com, "Dev, Dana" <dana@example.com>, Ben`); err != nil {
		t.Fatal(err)
	}

	people, err := People(s)
	if err != nil {
		t.Fatal(err)
	}
	if len(people) != 5 {
		t.Fatalf("people: got %d, want 5 (1 sender + 4 recipients)", len(people))
	}
	var silent int
	for _, p := range people {
		if p.Sent == 0 && p.Received > 0 {
			silent++
		}
	}
	if silent != 4 {
		t.Fatalf("recipient-only participants: got %d, want 4", silent)
	}
	// the name-only participant is a person with a display_name identity, so a
	// human can later say who they are
	if _, err := PersonByIdentity(s, KindDisplayName, "ben"); err != nil {
		t.Fatalf("name-only participant not resolvable: %v", err)
	}

	parts, err := Participants(s, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(parts) != 5 || parts[0].Role != RoleFrom {
		t.Fatalf("participants: %+v", parts)
	}
}

func TestRecordHeaderReplacesThatRoleWholesale(t *testing.T) {
	s := open(t)
	r, err := s.Put(entry("mail:<a@x>", "hello"), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RecordHeader(s, r.ID, RoleCc, `bob@example.com, carol@example.com`); err != nil {
		t.Fatal(err)
	}
	// a corrected header must not leave the dropped recipient attached
	if _, err := RecordHeader(s, r.ID, RoleCc, `bob@example.com`); err != nil {
		t.Fatal(err)
	}
	if n := countRole(t, s, r.ID, RoleCc); n != 1 {
		t.Fatalf("cc participants after re-ingest: got %d, want 1", n)
	}
	// but the person themselves survives: identities are never discarded
	if _, err := PersonByIdentity(s, KindEmail, "carol@example.com"); err != nil {
		t.Fatalf("person deleted with their participation: %v", err)
	}
}

// The real case: one colleague's Slack account sits under a pre-rebrand company
// domain while his mail is under the current one. No address rule ties them
// together, so the merge is the fix — and it must not lose either identity.
func TestMergeFoldsEveryIdentityAndReferenceIntoTheSurvivor(t *testing.T) {
	s := open(t)
	keep, err := Resolve(s, KindEmail, "dan@current.example", "Dan D")
	if err != nil {
		t.Fatal(err)
	}
	drop, err := Resolve(s, KindEmail, "dan@old.example", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := AddAlias(s, drop, KindSlackUID, "<@U0123>", "slack:profile"); err != nil {
		t.Fatal(err)
	}

	// the dropped half authored an entry and was cc'd on another, on which the
	// surviving half was ALSO cc'd — the primary-key collision case
	auth := entry("mail:<from-old@x>", "sent from the old domain")
	auth.PersonID = drop
	a, err := s.Put(auth, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.Put(entry("mail:<both-cc@x>", "cc'd twice"), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{keep, drop} {
		if err := Participate(s, b.ID, id, RoleCc); err != nil {
			t.Fatal(err)
		}
	}

	if err := Merge(s, keep, drop); err != nil {
		t.Fatalf("Merge: %v", err)
	}

	// no identity lost: all three still resolve, and all to the survivor
	for _, id := range [][2]string{
		{KindEmail, "dan@current.example"},
		{KindEmail, "dan@old.example"},
		{KindSlackUID, "U0123"},
	} {
		got, err := PersonByIdentity(s, id[0], id[1])
		if err != nil {
			t.Fatalf("%s %s lost by the merge: %v", id[0], id[1], err)
		}
		if got != keep {
			t.Fatalf("%s %s points at %d, want %d", id[0], id[1], got, keep)
		}
	}
	// authorship followed
	var author int64
	if err := s.DB().QueryRow(`select person_id from entries where id=?`, a.ID).
		Scan(&author); err != nil {
		t.Fatal(err)
	}
	if author != keep {
		t.Fatalf("entry author: got %d, want %d", author, keep)
	}
	// the doubled cc collapsed to one row rather than failing the merge
	if n := countRole(t, s, b.ID, RoleCc); n != 1 {
		t.Fatalf("cc rows after merge: got %d, want 1", n)
	}
	// the orphan is gone, and the merge is on the record
	var n int
	if err := s.DB().QueryRow(`select count(*) from people where id=?`, drop).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatal("dropped person still present")
	}
	if err := s.DB().QueryRow(
		`select count(*) from person_merges where kept_id=? and dropped_id=?`, keep, drop,
	).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("person_merges rows: got %d, want 1", n)
	}
}

func TestMergeByEmailAndItsRefusals(t *testing.T) {
	s := open(t)
	keep, err := Resolve(s, KindEmail, "erin@current.example", "Erin E")
	if err != nil {
		t.Fatal(err)
	}
	// the old-domain half was only ever seen as a bare address, so the surviving
	// display name should become the human one
	if _, err := Resolve(s, KindEmail, "erin@old.example", ""); err != nil {
		t.Fatal(err)
	}
	got, err := MergeByEmail(s, "erin@current.example", "erin@old.example")
	if err != nil {
		t.Fatalf("MergeByEmail: %v", err)
	}
	if got != keep {
		t.Fatalf("survivor: got %d, want %d", got, keep)
	}
	if name := nameOf(t, s, keep); name != "Erin E" {
		t.Fatalf("survivor name: %q", name)
	}
	// merging an already-merged pair is a no-op, not a second record
	if _, err := MergeByEmail(s, "erin@current.example", "erin@old.example"); err != nil {
		t.Fatalf("re-merge: %v", err)
	}
	var n int
	if err := s.DB().QueryRow(`select count(*) from person_merges`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("person_merges rows after re-merge: got %d, want 1", n)
	}
	if _, err := MergeByEmail(s, "erin@current.example", "nobody@example.com"); err == nil {
		t.Fatal("merging an unknown address should fail loudly")
	}
	if err := Merge(s, keep, keep); err == nil {
		t.Fatal("merging a person into themselves should fail")
	}
}

// A display-name-only participant ("Ben") cannot be resolved by any rule; a
// human has to say who they are. That is an alias, and it must not silently
// steal an identity that already belongs to someone else.
func TestAddAliasIsTheManualEscapeHatch(t *testing.T) {
	s := open(t)
	ben, err := Resolve(s, KindDisplayName, "Ben", "Ben")
	if err != nil {
		t.Fatal(err)
	}
	if err := AddAlias(s, ben, KindEmail, "BEN@example.com", ""); err != nil {
		t.Fatalf("AddAlias: %v", err)
	}
	got, err := PersonByIdentity(s, KindEmail, "ben@example.com")
	if err != nil || got != ben {
		t.Fatalf("alias did not resolve: %d %v", got, err)
	}
	var rule string
	if err := s.DB().QueryRow(
		`select rule from identities where kind='email' and value='ben@example.com'`,
	).Scan(&rule); err != nil {
		t.Fatal(err)
	}
	if rule != "manual:alias" {
		t.Fatalf("rule: got %q, want manual:alias", rule)
	}

	other, err := Resolve(s, KindEmail, "frank@example.com", "Frank F")
	if err != nil {
		t.Fatal(err)
	}
	err = AddAlias(s, other, KindEmail, "ben@example.com", "")
	if err == nil || !strings.Contains(err.Error(), "merge instead") {
		t.Fatalf("stealing an identity should be refused as a merge: %v", err)
	}
	if err := AddAlias(s, 999, KindEmail, "ghost@example.com", ""); err == nil {
		t.Fatal("aliasing onto a non-existent person should fail")
	}
}

func TestFailedMergeLeavesNothingHalfDone(t *testing.T) {
	s := open(t)
	keep, err := Resolve(s, KindEmail, "gail@example.com", "Gail G")
	if err != nil {
		t.Fatal(err)
	}
	if err := Merge(s, keep, 4242); err == nil {
		t.Fatal("merging a non-existent person should fail")
	}
	// the transaction rolled back: no merge recorded, keep untouched
	var n int
	if err := s.DB().QueryRow(`select count(*) from person_merges`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("person_merges rows: got %d, want 0", n)
	}
	if got, err := PersonByIdentity(s, KindEmail, "gail@example.com"); err != nil || got != keep {
		t.Fatalf("survivor damaged: %d %v", got, err)
	}
}

func TestUnknownKindsAndRolesAreRejected(t *testing.T) {
	s := open(t)
	if _, err := Resolve(s, "phone", "0400000000", ""); err == nil {
		t.Fatal("an unknown identity kind should be rejected, not stored")
	}
	if _, err := Resolve(s, KindEmail, "   ", ""); err == nil {
		t.Fatal("an empty identity should be rejected")
	}
	if err := Participate(s, 1, 1, "bcc"); err == nil {
		t.Fatal("an unknown role should be rejected")
	}
}

func nameOf(t *testing.T, s *Store, id int64) string {
	t.Helper()
	var name string
	if err := s.DB().QueryRow(`select display_name from people where id=?`, id).Scan(&name); err != nil {
		t.Fatal(err)
	}
	return name
}

func countRole(t *testing.T, s *Store, entryID int64, role string) int {
	t.Helper()
	var n int
	if err := s.DB().QueryRow(
		`select count(*) from participants where entry_id=? and role=?`, entryID, role).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// Outlook renders a hyperlinked address in plain-text mail as
// `Name <addr <mailto:addr>>`. Bracket matching reads that as the address
// `addr <mailto:addr>`, which is a second identity for a human who already has
// one — and one that can never match a Slack profile email, so cross-source
// resolution silently fails for exactly the people who use Outlook.
func TestParseAddressHandlesOutlooksHyperlinkedForm(t *testing.T) {
	for _, c := range []struct {
		frag, name, addr string
	}{
		// the fully-formed rendering: both halves must come back
		{`Ada Fenwick <ada.fenwick@quarry.fed <mailto:ada.fenwick@quarry.fed>>`,
			"Ada Fenwick", "ada.fenwick@quarry.fed"},
		// no display name, space before the bracket
		{`ada.fenwick@quarry.fed <mailto:ada.fenwick@quarry.fed>`,
			"", "ada.fenwick@quarry.fed"},
		// no display name, no space — Outlook emits both
		{`ada.fenwick@quarry.fed<mailto:ada.fenwick@quarry.fed>`,
			"", "ada.fenwick@quarry.fed"},
		// the closers lost to a line wrap, which is how these reached the corpus
		{`ada.fenwick@quarry.fed <mailto:ada.fenwick@quarry.fed`,
			"", "ada.fenwick@quarry.fed"},
		// the link doubled ahead of the name, seen in a forwarded header block
		{`<mailto:ada.fenwick@quarry.fed> <ada.fenwick@quarry.fed <mailto:ada.fenwick@quarry.fed>>`,
			"", "ada.fenwick@quarry.fed"},
		// the scheme alone, with nothing bracketing it
		{`mailto:ada.fenwick@quarry.fed`, "", "ada.fenwick@quarry.fed"},
		// a + tag and a subdomain must survive the reduction unaltered
		{`Ada <ada+billing@mail.quarry.fed <mailto:ada+billing@mail.quarry.fed>>`,
			"Ada", "ada+billing@mail.quarry.fed"},
	} {
		got, ok := ParseAddress(c.frag)
		if !ok {
			t.Fatalf("ParseAddress(%q) found nothing usable", c.frag)
		}
		if got.Addr != c.addr || got.Name != c.name {
			t.Errorf("ParseAddress(%q) = name %q addr %q, want name %q addr %q",
				c.frag, got.Name, got.Addr, c.name, c.addr)
		}
	}
}

// The reduction keys on "mailto:", not on the word, so a name that happens to
// contain it is parsed as any other name is.
func TestParseAddressDoesNotMangleAMailtoDisplayName(t *testing.T) {
	got, ok := ParseAddress(`"Mailto Fanclub" <fan@quarry.fed>`)
	if !ok || got.Addr != "fan@quarry.fed" || got.Name != "Mailto Fanclub" {
		t.Fatalf("got %+v", got)
	}
	// and even with the colon present, the name in front of the address survives
	got, ok = ParseAddress(`Mailto: Help Desk <help@quarry.fed>`)
	if !ok || got.Addr != "help@quarry.fed" || got.Name != "Mailto: Help Desk" {
		t.Fatalf("got %+v", got)
	}
}

// Two different addresses in one fragment are not reducible: whichever one were
// chosen would attribute a message to someone who did not send or receive it, so
// the fragment degrades to the old bracket-matched result instead.
func TestParseAddressRefusesToGuessBetweenTwoAddresses(t *testing.T) {
	got, ok := ParseAddress(`Ada <ada@quarry.fed <mailto:bram@quarry.fed>>`)
	if !ok {
		t.Fatal("an ambiguous fragment is still evidence and must not be dropped")
	}
	if got.Addr == "ada@quarry.fed" || got.Addr == "bram@quarry.fed" {
		t.Fatalf("picked %q from a fragment naming two people", got.Addr)
	}
}

// A header mixing the hyperlinked form with ordinary entries: net/mail's
// all-or-nothing list parse fails on it, and the fallback must still return
// everyone, hyperlinked or not.
func TestParseAddressesReducesHyperlinkedEntriesAlongsidePlainOnes(t *testing.T) {
	got := ParseAddresses(
		`Ada Fenwick <ada@quarry.fed <mailto:ada@quarry.fed>>, ` +
			`"Fenwick, Bram" <bram@quarry.fed>, cleo@quarry.fed<mailto:cleo@quarry.fed>`)
	if len(got) != 3 {
		t.Fatalf("got %d addresses, want 3: %+v", len(got), got)
	}
	want := []string{"ada@quarry.fed", "bram@quarry.fed", "cleo@quarry.fed"}
	for i, w := range want {
		if got[i].Addr != w {
			t.Errorf("address %d = %q, want %q", i, got[i].Addr, w)
		}
	}
	if got[0].Name != "Ada Fenwick" || got[1].Name != "Fenwick, Bram" {
		t.Errorf("names: %q, %q", got[0].Name, got[1].Name)
	}
}

// The case these exist for: one human held twenty-two identity rows because
// every signup got its own +tag and every tag was its own person.
func TestPlusBaseAddressReducesEveryTagShapeAndRefusesTheRest(t *testing.T) {
	for _, c := range []struct {
		addr, base, tag string
		ok              bool
	}{
		{"dai+salsa@quarry.fed", "dai@quarry.fed", "salsa", true},
		// everything from the FIRST plus is the tag, however many follow
		{"dai+books+2025@quarry.fed", "dai@quarry.fed", "books+2025", true},
		{"dai+@quarry.fed", "dai@quarry.fed", "", true},
		{"dai.rhys+gst@sub.quarry.fed", "dai.rhys@sub.quarry.fed", "gst", true},
		// a plus in the domain is not a subaddress and must survive untouched
		{"dai@quarry+west.fed", "", "", false},
		{"dai@quarry.fed", "", "", false},
		// nothing in front of the tag leaves no mailbox to reduce to
		{"+salsa@quarry.fed", "", "", false},
		// a forwarder's artefact: the tag names somebody else's mailbox
		{"dai+caf_=bram=quarry.fed@quarry.fed", "", "", false},
		{"dai+bram=quarry.fed@quarry.fed", "", "", false},
	} {
		base, tag, ok := plusBaseAddress(c.addr)
		if ok != c.ok || base != c.base || tag != c.tag {
			t.Errorf("plusBaseAddress(%q) = %q, %q, %v; want %q, %q, %v",
				c.addr, base, tag, ok, c.base, c.tag, c.ok)
		}
	}
}

// The tag is kept as an identity of the same person rather than normalised away:
// it says which signup produced the mail, and the corpus is asked that.
func TestResolveFilesATaggedAddressUnderTheBaseMailboxAndKeepsTheTag(t *testing.T) {
	s := open(t)
	base := person(t, s, "dai@quarry.fed", "Dai Rhys")

	tagged, err := ResolveWithRule(s, KindEmail, "dai+salsa@quarry.fed", "", "mail:to-header")
	if err != nil {
		t.Fatal(err)
	}
	if tagged != base {
		t.Fatalf("tagged address resolved to %d, want the base mailbox's person %d", tagged, base)
	}
	again, err := ResolveWithRule(s, KindEmail, "DAI+salsa@Quarry.fed", "", "mail:to-header")
	if err != nil {
		t.Fatal(err)
	}
	if again != base {
		t.Fatalf("second sighting resolved to %d, want %d", again, base)
	}

	var n int
	if err := s.DB().QueryRow(`select count(*) from people`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("people: got %d, want 1", n)
	}
	var rule string
	if err := s.DB().QueryRow(
		`select rule from identities where kind=? and value=?`,
		KindEmail, "dai+salsa@quarry.fed").Scan(&rule); err != nil {
		t.Fatalf("the tag was not kept: %v", err)
	}
	if !strings.Contains(rule, "subaddress") {
		t.Errorf("rule = %q, want it to name the subaddress", rule)
	}
}

// A tagged address seen before its base mailbox: the base is recorded too, so
// the next tag lands on this person instead of minting another.
func TestResolveRecordsTheBaseMailboxOfATagSeenFirst(t *testing.T) {
	s := open(t)
	first, err := ResolveWithRule(s, KindEmail, "dai+books@quarry.fed", "Dai Rhys", "mail:to-header")
	if err != nil {
		t.Fatal(err)
	}
	second, err := ResolveWithRule(s, KindEmail, "dai+gst@quarry.fed", "", "mail:to-header")
	if err != nil {
		t.Fatal(err)
	}
	plain, err := ResolveWithRule(s, KindEmail, "dai@quarry.fed", "", "mail:from-header")
	if err != nil {
		t.Fatal(err)
	}
	if second != first || plain != first {
		t.Fatalf("one mailbox resolved to %d, %d and %d", first, second, plain)
	}
	var name string
	if err := s.DB().QueryRow(`select display_name from people where id=?`, first).
		Scan(&name); err != nil {
		t.Fatal(err)
	}
	if name != "Dai Rhys" {
		t.Errorf("display name = %q, want the name the header gave", name)
	}
}

// A forwarding artefact is not a subaddress of the mailbox it is written on: the
// tag encodes a different mailbox entirely, so it stays its own row.
func TestResolveKeepsAForwardingArtefactApartFromTheMailboxItNames(t *testing.T) {
	s := open(t)
	dai := person(t, s, "dai@quarry.fed", "Dai Rhys")
	fwd, err := ResolveWithRule(s, KindEmail,
		"dai+caf_=bram=quarry.fed@quarry.fed", "", "mail:to-header")
	if err != nil {
		t.Fatal(err)
	}
	if fwd == dai {
		t.Fatal("the forwarding artefact was folded into the mailbox it is written on")
	}
}

// The other half of one fold: cut at the bracket the address is lost, cut inside
// it the address survives welded to the name — and must be read, not stored whole.
func TestParseAddressReadsAnAddressWeldedIntoTheName(t *testing.T) {
	got, ok := ParseAddress("Dai Rhys <dai@quarry.fed")
	if !ok {
		t.Fatal("the fragment names somebody and must not be dropped")
	}
	if got.Addr != "dai@quarry.fed" || got.Name != "Dai Rhys" {
		t.Fatalf("got %+v, want the name and the address apart", got)
	}
}

// A recipient list flattened through the middle of an address, where the second
// half lost its closing bracket too: both halves are one recipient.
func TestParseAddressesRejoinsAFoldWhoseClosingBracketWentMissing(t *testing.T) {
	got := ParseAddresses("Bram Fenwick <, bram@quarry.fed, cleo@quarry.fed")
	if len(got) != 2 {
		t.Fatalf("got %d addresses, want 2: %+v", len(got), got)
	}
	if got[0].Addr != "bram@quarry.fed" || got[0].Name != "Bram Fenwick" {
		t.Errorf("first = %+v, want the rejoined recipient", got[0])
	}
}

// A display name that legitimately holds a bracket is that person's whole
// evidence, so nothing may be taken off it.
func TestParseAddressKeepsABracketThatIntroducesNoAddress(t *testing.T) {
	got, ok := ParseAddress("Dai <site lead")
	if !ok {
		t.Fatal("dropped a named participant")
	}
	if got.Addr != "" || got.Name != "Dai <site lead" {
		t.Fatalf("got %+v, want a name-only participant", got)
	}
	got, ok = ParseAddress("nights < weekends roster")
	if !ok || got.Addr != "" || got.Name != "nights < weekends roster" {
		t.Fatalf("got %+v (%v), want the name untouched", got, ok)
	}
}
