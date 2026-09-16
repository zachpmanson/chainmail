package spec

import (
	"testing"

	"github.com/zachpmanson/chainmail/internal/corpus"
)

// identity records another address the corpus holds for a person, as an ingest
// records one. The org resolver falls back on these, so a test about whose
// organisation is whose has to be able to give a person two.
func identity(t *testing.T, s *corpus.Store, personID int64, addr string) {
	t.Helper()
	if _, err := s.DB().Exec(
		`insert into identities (person_id, kind, value, rule) values (?,?,?,?)`,
		personID, "email", addr, "test"); err != nil {
		t.Fatalf("insert identity %s: %v", addr, err)
	}
}

// rule stores one domain's organisation, as the Ops screen does.
func rule(t *testing.T, s *corpus.Store, domain, org string) {
	t.Helper()
	if err := s.PutOrgRule(domain, org); err != nil {
		t.Fatalf("PutOrgRule %s: %v", domain, err)
	}
}

// The colour a bubble is drawn in is a judgement the reader made, and it is
// stored beside the mail; a trail read and a page build both have to take it
// from there. That they agree is the claim; the guess is only what happens where
// nobody has decided.
func TestRenderTrailColoursByTheStoredRules(t *testing.T) {
	s := trail(t)
	rule(t, s, "loomworks.example", "The Loom")
	// An empty label is a decision — "this domain is not an organisation" — and
	// not the same claim as having no rule at all. If it were read as no rule, the
	// pane would colour Bo's mail "Fjordline" while the reader had said it is
	// nobody's, which is the whole difference the Ops screen offers.
	rule(t, s, "fjordline.example", "")

	rendered, err := RenderTrail(s, []string{"mail:<a@loomworks>", "mail:<b@fjordline>"})
	if err != nil {
		t.Fatalf("RenderTrail: %v", err)
	}
	if got := rendered["mail:<a@loomworks>"].Org; got != "The Loom" {
		t.Errorf("org under a stored rule = %q, want The Loom", got)
	}
	if got := rendered["mail:<b@fjordline>"].Org; got != "" {
		t.Errorf("org for a domain ruled out = %q, want none", got)
	}
}

// And the same rules decide a page build, which is the surface the pane is a key
// to: a page built beside the pane must not colour one sender two ways.
func TestGenerateBuildsUnderTheStoredRules(t *testing.T) {
	s := trail(t)
	rule(t, s, "loomworks.example", "The Loom")
	sp := generate(t, s, Options{Containers: []string{"T1"}})
	if got := sp.Messages[0].Org; got != "The Loom" {
		t.Errorf("page org = %q, want The Loom", got)
	}
}

// Clearing a rule is not the same act as ruling a domain out: it puts the domain
// back to being read from its own name, which is what the guess is for.
func TestClearingARuleRestoresTheGuess(t *testing.T) {
	s := trail(t)
	rule(t, s, "loomworks.example", "The Loom")
	if err := s.ClearOrgRule("loomworks.example"); err != nil {
		t.Fatalf("ClearOrgRule: %v", err)
	}
	rendered, err := RenderTrail(s, []string{"mail:<a@loomworks>"})
	if err != nil {
		t.Fatalf("RenderTrail: %v", err)
	}
	if got := rendered["mail:<a@loomworks>"].Org; got != "Loomworks" {
		t.Errorf("org after clearing = %q, want the guessed Loomworks", got)
	}
}

// A rule about a domain repaints that domain's mail, and the count the reader
// agrees to before saving has to be the count the resolver will produce. Grouping
// is nothing more than a shared name, so one domain taking another's name is the
// whole of what "these two are one organisation" means.
func TestOrgShiftCountsTheMailARuleMoves(t *testing.T) {
	s := trail(t)
	// The fixture's unrelated thread is Ada's too, so Loomworks holds three
	// entries and Fjordline one: moving Fjordline to Loomworks moves one message.
	shift, err := OrgShiftFor(s, map[string]string{"fjordline.example": "Loomworks"})
	if err != nil {
		t.Fatalf("OrgShiftFor: %v", err)
	}
	if shift.Messages != 1 || shift.People != 1 || shift.Ambiguous != 0 {
		t.Errorf("shift = %+v, want one message from one person", shift)
	}

	// Naming a domain what it was already called is not a change, and a preview
	// that reported one would be asking the reader to agree to nothing.
	same, err := OrgShiftFor(s, map[string]string{"fjordline.example": "Fjordline"})
	if err != nil {
		t.Fatalf("OrgShiftFor: %v", err)
	}
	if same.Messages != 0 {
		t.Errorf("a rule that renames nothing reports %d messages", same.Messages)
	}
}

// Some entries have no answer a corpus-wide pass can give: a message recovered
// from a quote has no address of its own, so it takes its sender's organisation,
// and a sender whose own mail names two of them has no single one to take. That
// is reported rather than resolved, because either answer would be this pass
// picking a trail order and presenting it as the corpus's.
func TestOrgShiftReportsWhatItCannotCount(t *testing.T) {
	s := trail(t)
	ada, err := corpus.PersonByIdentity(s, corpus.KindEmail, "ada@loomworks.example")
	if err != nil {
		t.Fatalf("resolving Ada's person: %v", err)
	}
	// A second organisation's address for the same human, which is what an
	// employer change looks like to the corpus.
	identity(t, s, ada, "ada@okoye.example")
	// And an entry of hers with no From header at all, quoted out of a reply.
	put(t, s, msg{
		ext: "quote:<q@loomworks>", ts: "2026-03-02T09:20:00+11:00", tz: "AEDT",
		person: ada, container: "T1", subject: "Loom cutover",
	})
	// A rule about an unrelated domain, so nothing here is asked to change.
	shift, err := OrgShiftFor(s, map[string]string{"fjordline.example": "Fjordline"})
	if err != nil {
		t.Fatalf("OrgShiftFor: %v", err)
	}
	if shift.Ambiguous != 1 {
		t.Errorf("ambiguous = %d, want the one entry whose sender names two organisations",
			shift.Ambiguous)
	}
}
