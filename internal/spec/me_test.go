package spec

import (
	"testing"

	"github.com/zachpmanson/chainmail/internal/corpus"
)

// The reader is one human, resolved two ways: the address they named, and the
// person the corpus merged that address into.
//
// The second is what makes "sent by you" a claim about a person. A reader's mail
// arrives from addresses they never list — a `+tag` of their own mailbox, a work
// address, an alias — and naming one of them must mark all of it, or the page
// contradicts the corpus about who the reader is. A recovered entry has no From
// header at all, so only the person can mark it, and that is the entry a reader
// is most likely to spot.
func TestTheReaderIsResolvedByAddressAndThenByPerson(t *testing.T) {
	s := open(t)
	reader := person(t, s, "Ada Byron", "ada@loomworks.example")
	// A second address of the same human, which the corpus has already decided
	// belongs to them.
	if err := corpus.AddAlias(s, reader, "email", "ada+salsa@loomworks.example", "test"); err != nil {
		t.Fatalf("AddAlias: %v", err)
	}
	bo := person(t, s, "Bo Halvorsen", "bo@fjordline.example")

	me, err := newMeSet(s, []string{"ada@loomworks.example"})
	if err != nil {
		t.Fatalf("newMeSet: %v", err)
	}

	cases := []struct {
		what    string
		person  int64
		address string
		want    bool
	}{
		{"an address the reader named", reader, "ADA@Loomworks.example ", true},
		{"another address of theirs, which the corpus merged", reader, "ada+salsa@loomworks.example", true},
		{"a recovered entry, which has no address to match", reader, "", true},
		{"somebody else's mail", bo, "bo@fjordline.example", false},
		{"an entry whose author is unknown", 0, "", false},
	}
	for _, c := range cases {
		if got := me.wrote(c.person, c.address); got != c.want {
			t.Errorf("%s: wrote = %v, want %v", c.what, got, c.want)
		}
	}

	// Naming an address the corpus has never seen still marks that address's
	// mail: the fallback to the string is what a reader gets for naming somebody
	// the corpus has not met, and it is a claim about the string they typed.
	stranger, err := newMeSet(s, []string{"nobody@nowhere.example"})
	if err != nil {
		t.Fatalf("newMeSet: %v", err)
	}
	if !stranger.wrote(0, "nobody@nowhere.example") {
		t.Error("an address the reader named does not mark its own mail")
	}
	if stranger.wrote(reader, "ada@loomworks.example") {
		t.Error("naming a stranger's address marked the mail of a person, not of a string")
	}

	// And the reader who has named nothing marks nothing, which is the state the
	// pane is in for everyone who has never filled the field in.
	none, err := newMeSet(s, nil)
	if err != nil {
		t.Fatalf("newMeSet: %v", err)
	}
	if none.wrote(reader, "") || none.wrote(reader, "ada@loomworks.example") {
		t.Error("a reader who has named no address has mail marked as theirs")
	}
}

// An empty entry in the list is not an address, and must not become one: every
// entry recovered from a quote has no From header, so an empty string on the
// reader's list would mark their whole quoted history as their own outbound
// mail. A stray comma is a typo, and this is the loudest possible reading of it.
func TestAnEmptyEntryInTheListMarksNothing(t *testing.T) {
	s := open(t)
	reader := person(t, s, "Ada Byron", "ada@loomworks.example")

	me, err := newMeSet(s, []string{"ada@loomworks.example", "", "   "})
	if err != nil {
		t.Fatalf("newMeSet: %v", err)
	}
	if me.wrote(0, "") {
		t.Error("a blank entry matched the empty address of a recovered entry")
	}
	if me.wrote(0, "bo@fjordline.example") {
		t.Error("a blank entry matched a stranger")
	}
	if !me.wrote(reader, "ada@loomworks.example") {
		t.Error("the address that was named stopped marking the reader's mail")
	}
}

// The pane marks the reader's own mail from the stored setting, and by the same
// rule the page build uses.
//
// Both halves matter and neither is decoration. The setting is the only place
// the pane can learn who the reader is — there is no form beside a trail saying
// so — and the rule has to be the page's: a pane that resolved the addresses
// itself, or resolved them differently, would show one thread two ways
// depending on which surface the reader was looking at.
func TestThePaneMarksTheReadersMailFromTheStoredSetting(t *testing.T) {
	s := trail(t)
	if err := s.PutSetting(corpus.SettingMe, "ada@loomworks.example"); err != nil {
		t.Fatalf("PutSetting: %v", err)
	}
	const reader = "ada@loomworks.example"
	sp := generate(t, s, Options{Containers: []string{"T1"}, Me: []string{reader}})

	ids := make([]string, 0, len(sp.Messages))
	for _, m := range sp.Messages {
		ids = append(ids, m.ExtID)
	}
	rendered, err := RenderTrail(s, ids)
	if err != nil {
		t.Fatalf("RenderTrail: %v", err)
	}

	marked, plain := 0, 0
	for _, m := range sp.Messages {
		got, ok := rendered[m.ExtID]
		if !ok {
			t.Errorf("%s: not rendered at all", m.ExtID)
			continue
		}
		// The claim is that the two surfaces agree, not that a particular entry
		// is whose: what the page decided is what the pane has to say.
		if got.Mine != m.Me {
			t.Errorf("%s: the pane and the page disagree about whose mail this is\n pane: %v\n page: %v",
				m.ExtID, got.Mine, m.Me)
		}
		if got.Mine {
			marked++
		} else {
			plain++
		}
	}
	// A trail where everything is marked, or nothing is, would pass the
	// comparison above while proving nothing about who the reader is.
	if marked == 0 || plain == 0 {
		t.Fatalf("the fixture marked %d of %d entries, so the comparison proves nothing",
			marked, len(sp.Messages))
	}
}

// A reader who has never named an address sees no marks at all.
//
// The corpus knows perfectly well that Ada sent some of these — she is a person
// in the fixture, with an identity and a display name — and that is not the
// question. Nobody has told chainmail which mailbox this is, so nothing may be
// claimed, and a pane that marked the corpus's best guess would be inventing a
// fact about the reader rather than reporting one.
func TestAPaneWithNoStoredAddressesMarksNothing(t *testing.T) {
	s := trail(t)
	rendered, err := RenderTrail(s, []string{"mail:<a@loomworks>", "mail:<b@fjordline>"})
	if err != nil {
		t.Fatalf("RenderTrail: %v", err)
	}
	for ext, r := range rendered {
		if r.Mine {
			t.Errorf("%s is marked as the reader's, who has named no address", ext)
		}
	}
}
