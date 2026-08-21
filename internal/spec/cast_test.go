package spec

import (
	"strings"
	"testing"
	"time"

	"github.com/zachpmanson/chainmail/internal/corpus"
)

// All test data here is invented. See the note in generate_test.go.

// The invariant these tests defend: every sender the transcript shows has a row
// in the participants panel. The panel is the page's only statement of who was
// involved, so a name in the transcript and not in the panel reads as someone
// the tool lost.
func castNames(sp Spec) map[string]Participant {
	out := map[string]Participant{}
	for _, p := range sp.Participants {
		out[p.Name] = p
	}
	return out
}

func missingSenders(sp Spec) []string {
	named := castNames(sp)
	var out []string
	seen := map[string]bool{}
	for _, m := range sp.Messages {
		if m.Sender == "" || seen[m.Sender] {
			continue
		}
		seen[m.Sender] = true
		if _, ok := named[m.Sender]; !ok {
			out = append(out, m.Sender)
		}
	}
	return out
}

func TestEverySenderIsInTheCast(t *testing.T) {
	s := trail(t)
	sp := generate(t, s, Options{Containers: []string{"T1"}})
	if got := missingSenders(sp); len(got) > 0 {
		t.Fatalf("senders absent from the panel: %v", got)
	}
	// The other direction: a panel no larger than the senders has stopped
	// carrying recipients, which is the reason it exists at all.
	senders := map[string]bool{}
	for _, m := range sp.Messages {
		senders[m.Sender] = true
	}
	silent := 0
	for _, p := range sp.Participants {
		if !senders[p.Name] {
			silent++
		}
	}
	if silent == 0 {
		t.Fatalf("no recipient-only participant in %+v", sp.Participants)
	}
}

// A sender whose corpus display name differs from the one their own header
// carried is the common shape of this bug: the transcript names them from the
// corpus and the panel used to name them from the header, so one human appeared
// as two and one of the two was missing.
func TestTheCastNamesASenderAsTheTranscriptDoes(t *testing.T) {
	s := open(t)
	id := person(t, s, "Ada Byron", "ada@loomworks.example")
	e := put(t, s, msg{
		ext: "mail:<n@loomworks>", ts: "2026-05-04T09:00:00+10:00", tz: "AEST",
		person: id, container: "TN", subject: "Two spellings",
		messageID: "<n@loomworks>", from: `"A. Byron (Loomworks)" <ada@loomworks.example>`,
		to: "Bo Halvorsen <bo@fjordline.example>", gmail: "g-n",
	})
	if err := s.Sight(e, 0, "direct", ""); err != nil {
		t.Fatalf("Sight: %v", err)
	}
	sp := generate(t, s, Options{Containers: []string{"TN"}})

	if got := sp.Messages[0].Sender; got != "Ada Byron" {
		t.Fatalf("sender = %q, want the corpus display name", got)
	}
	p, ok := castNames(sp)["Ada Byron"]
	if !ok {
		t.Fatalf("the panel names %+v, not the sender the page shows", sp.Participants)
	}
	if p.Email != "ada@loomworks.example" {
		t.Errorf("participant email = %q", p.Email)
	}
	if len(sp.Participants) != 2 {
		t.Errorf("participants = %+v, want one row per human", sp.Participants)
	}
}

// A Slack message has no From header at all: its author exists only as a person
// id and a participants row.
func TestSlackAuthorsAreInTheCast(t *testing.T) {
	s := open(t)
	ida := person(t, s, "Ada Byron", "ada@loomworks.example")
	idb := person(t, s, "Bo Halvorsen", "bo@fjordline.example")
	for _, m := range []struct {
		ext, ts string
		person  int64
	}{
		{"slack:C1:1000.1", "2026-05-04T09:00:00Z", ida},
		{"slack:C1:1000.2", "2026-05-04T09:05:00Z", idb},
	} {
		ts, _ := time.Parse(time.RFC3339, m.ts)
		res, err := s.PutSlack(corpus.Entry{
			Source: corpus.SourceSlack, ExtID: m.ext, TS: ts, PersonID: m.person,
			Container: "C1", BodyText: "invented body",
		}, corpus.Slack{ChannelID: "C1", ChannelName: "cutover", TS: strings.TrimPrefix(m.ext, "slack:C1:")}, nil)
		if err != nil {
			t.Fatalf("PutSlack: %v", err)
		}
		if err := s.Sight(res.ID, 0, "direct", ""); err != nil {
			t.Fatalf("Sight: %v", err)
		}
		// As slackingest does: the author, and only the author.
		if err := corpus.Participate(s, res.ID, m.person, corpus.RoleFrom); err != nil {
			t.Fatalf("Participate: %v", err)
		}
	}
	sp := generate(t, s, Options{Containers: []string{"C1"}, Title: "#cutover"})

	if got := missingSenders(sp); len(got) > 0 {
		t.Fatalf("senders absent from the panel: %v", got)
	}
	// Authors only, and every one of them: a channel post is addressed to a
	// channel, so there is no recipient to add and the panel is the list of
	// people who spoke. It says nothing it cannot support.
	if len(sp.Participants) != 2 {
		t.Fatalf("participants = %+v, want the two authors", sp.Participants)
	}
	// The mailbox the corpus knows for them, so the panel can still be acted on.
	if p := castNames(sp)["Ada Byron"]; p.Email != "ada@loomworks.example" || p.Org != "Loomworks" {
		t.Errorf("slack author = %+v, want the address and org the corpus holds", p)
	}
}

// An entry recovered from quoted text has no mail_detail row, so neither its
// author nor its recipients are reachable from a header. They are in the
// participants table, which is where the ingest put them.
func TestQuotedEntryPeopleAreInTheCast(t *testing.T) {
	s := trail(t)
	// Neither is anywhere else in the trail, so every sighting of them is a line
	// in someone else's forward.
	ed := person(t, s, "Ed Nakamura", "ed@loomworks.example")
	dee := person(t, s, "Dee Farrow", "dee@fjordline.example")

	ts, _ := time.Parse(time.RFC3339, "2026-03-01T08:00:00+11:00")
	id, _, err := s.PutQuoted(corpus.Entry{
		Source: corpus.SourceMail, ExtID: "quote:sha-cast", TS: ts, TZ: "AEDT",
		PersonID: ed, Container: "T1", Subject: "Loom cutover", BodyText: "invented body",
	})
	if err != nil {
		t.Fatalf("PutQuoted: %v", err)
	}
	if err := s.Sight(id, 0, "quoted", ""); err != nil {
		t.Fatalf("Sight: %v", err)
	}
	if err := corpus.Participate(s, id, ed, corpus.RoleFrom); err != nil {
		t.Fatalf("Participate: %v", err)
	}
	if err := corpus.Participate(s, id, dee, corpus.RoleTo); err != nil {
		t.Fatalf("Participate: %v", err)
	}
	sp := generate(t, s, Options{Containers: []string{"T1"}})

	if got := missingSenders(sp); len(got) > 0 {
		t.Fatalf("senders absent from the panel: %v", got)
	}
	named := castNames(sp)
	if _, ok := named["Dee Farrow"]; !ok {
		t.Fatalf("a recipient of a quoted entry is not in the panel: %+v", sp.Participants)
	}
	// Every sighting of Dee came out of someone else's quoted history, so the
	// panel says so rather than letting a forwarded To: line read like a
	// delivered message.
	if got := named["Dee Farrow"].Note; got != "seen only in quoted text" {
		t.Errorf("note = %q, want the weaker evidence declared", got)
	}
	// Ed sent the quoted message and nothing else, so the same holds for them.
	if got := named["Ed Nakamura"].Note; got != "seen only in quoted text" {
		t.Errorf("note = %q for a quoted-only sender", got)
	}
	// Cy Okafor was cc'd on a real message, so a cc on a quoted one adds nothing
	// and takes nothing away.
	if got := named["Cy Okafor"].Note; got != "" {
		t.Errorf("note = %q on someone a mailbox copy also names", got)
	}
	// Ada sent a real message, so nothing qualifies her.
	if got := named["Ada Byron"].Note; got != "" {
		t.Errorf("note = %q on someone seen in the mailbox itself", got)
	}
}

// The guard, on a spec built by hand: Generate cannot produce one of these,
// which is the point of it — the check is what keeps that true as the generator
// changes, and what makes it safe for the page to name the cast in one place.
func TestASenderMissingFromTheCastIsAnError(t *testing.T) {
	sp := Spec{
		Title:        "Invented",
		Participants: []Participant{{Name: "Ada Byron", Email: "ada@loomworks.example"}},
		Messages: []Entry{
			{ID: "m1", Date: "2026-03-02", Sender: "Ada Byron", Body: "<p>one</p>"},
			{ID: "m2", Date: "2026-03-03", Sender: "Cy Okafor", Body: "<p>two</p>"},
		},
	}
	err := checkCastCoversSenders(sp)
	if err == nil {
		t.Fatal("want an error: a sender the panel does not list")
	}
	if !strings.Contains(err.Error(), "Cy Okafor") || !strings.Contains(err.Error(), "m2") {
		t.Errorf("error must name who was lost and where: %v", err)
	}
	sp.Participants = append(sp.Participants, Participant{Name: "Cy Okafor"})
	if err := checkCastCoversSenders(sp); err != nil {
		t.Errorf("complete cast rejected: %v", err)
	}
}

// A person the trail names but never gives an address for is still listed. The
// panel renders the absence explicitly, which is a smaller lie than leaving
// someone visible in the transcript out of the cast.
func TestAPersonWithNoAddressIsStillListed(t *testing.T) {
	sp := one(t, msg{
		ext: "mail:<bare@loomworks>", ts: "2026-06-01T09:00:00+10:00", tz: "AEST",
		container: "TB", subject: "No address", from: "Reception",
		to: "Bo Halvorsen <bo@fjordline.example>", gmail: "g-bare",
	})
	if got := missingSenders(sp); len(got) > 0 {
		t.Fatalf("senders absent from the panel: %v", got)
	}
	p, ok := castNames(sp)["Reception"]
	if !ok {
		t.Fatalf("participants = %+v", sp.Participants)
	}
	if p.Email != "" {
		t.Errorf("email = %q — an address is never invented", p.Email)
	}
}

// The panel is read as the key to the transcript's colours, so one person must
// resolve to one org whichever side asks. A Slack author is the sharp case: they
// have no From header, so the panel found their org through the mailbox the
// corpus holds for them and their messages found none at all, and one person was
// named at an org in the panel and left on the unknown slot in the transcript.
func TestAPersonsOrgIsTheSameInThePanelAndOnTheirMessages(t *testing.T) {
	s := open(t)
	id := person(t, s, "Ada Byron", "ada@loomworks.example")
	ts, _ := time.Parse(time.RFC3339, "2026-05-04T09:00:00Z")
	res, err := s.PutSlack(corpus.Entry{
		Source: corpus.SourceSlack, ExtID: "slack:C2:1000.1", TS: ts, PersonID: id,
		Container: "C2", BodyText: "invented body",
	}, corpus.Slack{ChannelID: "C2", ChannelName: "cutover", TS: "1000.1"}, nil)
	if err != nil {
		t.Fatalf("PutSlack: %v", err)
	}
	if err := s.Sight(res.ID, 0, "direct", ""); err != nil {
		t.Fatalf("Sight: %v", err)
	}
	if err := corpus.Participate(s, res.ID, id, corpus.RoleFrom); err != nil {
		t.Fatalf("Participate: %v", err)
	}
	sp := generate(t, s, Options{Containers: []string{"C2"}, Title: "#cutover"})

	if got := sp.Messages[0].Org; got != "Loomworks" {
		t.Errorf("message org = %q, want the org of the address the corpus holds", got)
	}
	if got := castNames(sp)["Ada Byron"].Org; got != sp.Messages[0].Org {
		t.Errorf("panel org = %q, message org = %q; a page cannot key itself off two answers",
			got, sp.Messages[0].Org)
	}
}

// An org is never invented, so someone the corpus holds no work address for
// stays without one. The renderer shows that as the unknown slot, which is an
// honest gap; a guessed org would colour them as a colleague of people they have
// nothing to do with.
func TestNoAddressMeansNoOrgRatherThanAGuess(t *testing.T) {
	s := open(t)
	// A freemail domain says who hosts their mail, not who they work for.
	id := person(t, s, "Cy Renner", "cy.renner@gmail.com")
	ts, _ := time.Parse(time.RFC3339, "2026-05-04T09:00:00Z")
	res, err := s.PutSlack(corpus.Entry{
		Source: corpus.SourceSlack, ExtID: "slack:C3:1000.1", TS: ts, PersonID: id,
		Container: "C3", BodyText: "invented body",
	}, corpus.Slack{ChannelID: "C3", ChannelName: "cutover", TS: "1000.1"}, nil)
	if err != nil {
		t.Fatalf("PutSlack: %v", err)
	}
	if err := s.Sight(res.ID, 0, "direct", ""); err != nil {
		t.Fatalf("Sight: %v", err)
	}
	if err := corpus.Participate(s, res.ID, id, corpus.RoleFrom); err != nil {
		t.Fatalf("Participate: %v", err)
	}
	sp := generate(t, s, Options{Containers: []string{"C3"}, Title: "#cutover"})

	if got := sp.Messages[0].Org; got != "" {
		t.Errorf("message org = %q, want none: nothing here says where they work", got)
	}
	if got := castNames(sp)["Cy Renner"].Org; got != "" {
		t.Errorf("panel org = %q, want none", got)
	}
}
