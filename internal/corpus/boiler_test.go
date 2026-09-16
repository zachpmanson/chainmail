package corpus

import (
	"reflect"
	"testing"
	"time"

	"github.com/zachpmanson/chainmail/internal/boiler"
)

// All names, addresses and domains here are invented.

// wholeCorpusFolds is the answer BoilerplateFor is claiming to reproduce: every
// body in the corpus, folded. It is the reference the scoped call is tested
// against rather than a second implementation of it.
func wholeCorpusFolds(t *testing.T, s *Store) map[int64]boiler.Fold {
	t.Helper()
	msgs, err := s.MailBodies()
	if err != nil {
		t.Fatalf("reading every mail body: %v", err)
	}
	return boiler.Detect(msgs, boiler.Default())
}

// sender is a person with an address, so an entry can be attributed to them the
// way ingest attributes one.
func sender(t *testing.T, s *Store, addr, name string) int64 {
	t.Helper()
	id, err := Resolve(s, KindEmail, addr, name)
	if err != nil {
		t.Fatalf("resolving %s: %v", addr, err)
	}
	return id
}

// say stores one mail message from a person, and returns the entry's id (not the
// person's).
func say(t *testing.T, s *Store, person int64, addr, ext, body string) int64 {
	t.Helper()
	e := entry(ext, body)
	e.PersonID = person
	res, err := s.Put(e, &Mail{From: addr}, nil)
	if err != nil {
		t.Fatalf("putting %s: %v", ext, err)
	}
	return res.ID
}

// The three shapes a fold has to come out of: one person's signature, an
// organisation's notice repeated by two of its senders, and a third person whose
// messages the selection never asks about.
const (
	adaBody  = "Roof access is fine from the 14th.\n\nRegards,\nAda\n"
	adaSig   = "Regards,\nAda\n"
	loomBody = "Cutover at 06:00 on Monday.\n\nLoomworks Pty Ltd\nThis message is confidential.\n"
	danaBody = "The fence panels arrived.\n\nFarewell,\nDana\n"
)

func signedCorpus(t *testing.T) *Store {
	t.Helper()
	s := open(t)
	ada := sender(t, s, "ada@weave.example", "Ada Okoye")
	bo := sender(t, s, "bo@loom.example", "Bo Halvorsen")
	cy := sender(t, s, "cy@loom.example", "Cy Marsh")
	dana := sender(t, s, "dana@fence.example", "Dana Reyes")

	// Three of Ada's, so her two-line sign-off repeats past AuthorRepeats.
	for _, ext := range []string{"mail:<ada-1@weave.example>", "mail:<ada-2@weave.example>", "mail:<ada-3@weave.example>"} {
		say(t, s, ada, "Ada Okoye <ada@weave.example>", ext, adaBody)
	}
	// Three at one domain from two senders, which is the organisation's notice:
	// neither sender alone repeats it enough to prove anything.
	say(t, s, bo, "Bo Halvorsen <bo@loom.example>", "mail:<loom-1@loom.example>", loomBody)
	say(t, s, bo, "Bo Halvorsen <bo@loom.example>", "mail:<loom-2@loom.example>", loomBody)
	say(t, s, cy, "Cy Marsh <cy@loom.example>", "mail:<loom-3@loom.example>", loomBody)
	// And somebody the selection has no reason to read.
	for _, ext := range []string{"mail:<dana-1@fence.example>", "mail:<dana-2@fence.example>", "mail:<dana-3@fence.example>"} {
		say(t, s, dana, "Dana Reyes <dana@fence.example>", ext, danaBody)
	}
	return s
}

func idOf(t *testing.T, s *Store, ext string) int64 {
	t.Helper()
	var id int64
	if err := s.DB().QueryRow(`select id from entries where ext_id = ?`, ext).Scan(&id); err != nil {
		t.Fatalf("looking up %s: %v", ext, err)
	}
	return id
}

// The invariant the scope rests on: for the messages asked about, the answer is
// the corpus-wide answer. Not "similar to" — equal, because a verdict is decided
// per group and the scope is drawn around whole groups.
func TestASelectionIsFoldedTheWayTheWholeCorpusFoldsIt(t *testing.T) {
	s := signedCorpus(t)
	want := wholeCorpusFolds(t, s)

	for _, ext := range []string{
		"mail:<ada-1@weave.example>",
		"mail:<ada-2@weave.example>",
		"mail:<loom-1@loom.example>",
		"mail:<loom-3@loom.example>",
	} {
		id := idOf(t, s, ext)
		if want[id].Scope == boiler.NoScope {
			t.Fatalf("%s is not folded by the whole corpus, so the test proves nothing", ext)
		}
		got, err := s.BoilerplateFor([]int64{id})
		if err != nil {
			t.Fatalf("folding %s: %v", ext, err)
		}
		if !reflect.DeepEqual(got[id], want[id]) {
			t.Errorf("%s: scoped fold %+v, whole corpus %+v", ext, got[id], want[id])
		}
	}
}

// The case the scope is shaped around: an organisation is folded for a sender
// whose own message is the only one the selection holds. Cy sent the notice once;
// the two other copies that prove it belong to somebody else, and they are in the
// scope because the domain is — not because Cy's message could have found them.
func TestAOneOffSenderInheritsTheirOrganisationsNotice(t *testing.T) {
	s := signedCorpus(t)
	cy := idOf(t, s, "mail:<loom-3@loom.example>")

	got, err := s.BoilerplateFor([]int64{cy})
	if err != nil {
		t.Fatalf("folding Cy's message: %v", err)
	}
	fold, ok := got[cy]
	if !ok {
		t.Fatal("the notice was not folded for the sender who appears once")
	}
	if fold.Scope != boiler.Domain {
		t.Errorf("scope: got %v, want the organisation's", fold.Scope)
	}
}

// And the other half of being scoped: a group the selection does not touch is not
// read, so it cannot come back as a fold the caller never asked about.
func TestGroupsOutsideTheSelectionAreNotRead(t *testing.T) {
	s := signedCorpus(t)
	ada := idOf(t, s, "mail:<ada-1@weave.example>")
	dana := idOf(t, s, "mail:<dana-1@fence.example>")

	want := wholeCorpusFolds(t, s)
	if want[dana].Scope == boiler.NoScope {
		t.Fatal("Dana's signature is not folded by the whole corpus, so the test proves nothing")
	}

	got, err := s.BoilerplateFor([]int64{ada})
	if err != nil {
		t.Fatalf("folding Ada's message: %v", err)
	}
	if _, ok := got[dana]; ok {
		t.Error("a message in another group was folded anyway")
	}
	if got[ada].Lines == 0 {
		t.Error("Ada's own sign-off was not folded")
	}
}

// An empty selection is not a selection that excludes everything: a caller with
// nothing to ask about gets the whole corpus, which is what `corpus sigs` reads.
func TestAskingAboutNothingReadsEverything(t *testing.T) {
	s := signedCorpus(t)
	want := wholeCorpusFolds(t, s)

	got, err := s.BoilerplateFor(nil)
	if err != nil {
		t.Fatalf("folding with no selection: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("an empty selection folded %d messages, the corpus folds %d", len(got), len(want))
	}
}

// The reference itself, so a change that quietly stops folding anything cannot
// make every test above pass by folding nothing.
func TestTheFixturesAreFoldedAtAll(t *testing.T) {
	s := signedCorpus(t)
	folds := wholeCorpusFolds(t, s)

	ada := idOf(t, s, "mail:<ada-1@weave.example>")
	if got := folds[ada]; got.Lines != 2 || got.Scope != boiler.Author {
		t.Errorf("Ada's signature: got %d lines, scope %v; want 2 lines, %v", got.Lines, got.Scope, boiler.Author)
	}
	loom := idOf(t, s, "mail:<loom-3@loom.example>")
	if got := folds[loom]; got.Lines != 2 || got.Senders != 2 || got.Scope != boiler.Domain {
		t.Errorf("the notice: got %d lines over %d senders, scope %v; want 2 over 2, %v",
			got.Lines, got.Senders, got.Scope, boiler.Domain)
	}
}

// The cache's one hazard, and the reason its key is the body rather than the row:
// ingest rewrites a message it has already stored when the mailbox's copy of it
// changes (Put upserts on source+ext_id), and the fold has to follow the text.
func TestARewrittenBodyIsReducedAgain(t *testing.T) {
	s := signedCorpus(t)
	ext := "mail:<ada-1@weave.example>"
	id := idOf(t, s, ext)

	// Reduce it once, so the answer is in the cache.
	if _, err := s.BoilerplateFor([]int64{id}); err != nil {
		t.Fatalf("folding before the rewrite: %v", err)
	}

	// The same message, re-ingested with a different body: same row, new text.
	ada := sender(t, s, "ada@weave.example", "Ada Okoye")
	if _, err := s.Put(Entry{
		Source: SourceMail, ExtID: ext, TS: time.Unix(1_700_000_000, 0),
		PersonID: ada, BodyText: rewrittenAda, BodyHTML: "<p>rewritten</p>",
	}, &Mail{From: "Ada Okoye <ada@weave.example>"}, nil); err != nil {
		t.Fatalf("re-ingesting: %v", err)
	}

	msgs, err := s.MailBodies()
	if err != nil {
		t.Fatalf("reading bodies after the rewrite: %v", err)
	}
	var got []string
	for _, m := range msgs {
		if m.ID == id {
			got = m.Lines
		}
	}
	if len(got) == 0 {
		t.Fatal("the rewritten message is missing from the pass")
	}
	// The whole body, from the new text: the old opening is gone and the new one
	// is there, so this is not the row's first reduction coming back.
	if got[0] != "Roof access moves to the 21st." || got[len(got)-1] != "Ada" {
		t.Errorf("lines: got %q, want the rewritten body's", got)
	}
	for _, line := range got {
		if line == "Roof access is fine from the 14th." {
			t.Errorf("lines: %q still holds the body from before the rewrite", got)
		}
	}
}

// rewrittenAda is a different body from adaBody, with the same two-line sign-off:
// the lines have to come from the new text, not from the row's old reduction.
const rewrittenAda = "Roof access moves to the 21st.\n\nRegards,\nAda\n"
