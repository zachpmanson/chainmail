package spec

import (
	"testing"
	"time"

	"github.com/zachpmanson/chainmail/internal/corpus"
)

// The whole reason RenderTrail exists: a view that draws messages without
// building a page has to draw what a page draws. If these ever diverge, the
// inbox pane and the page built from the same thread have quietly become two
// renderers again — which is the defect this path was added to remove — and the
// divergence would show up as a difference in a bubble rather than as a failure.
//
// Compared against a real build rather than against a literal: what the
// conversion produces is not the claim, that the two agree is. That covers the
// recipient line as well as the body, because both come out of the one load, and
// a pane whose "to" line is written differently from the page's is the same
// defect wearing a smaller hat.
func TestRenderedEntriesAreTheEntriesAPageBuilds(t *testing.T) {
	s := trail(t)
	sp := generate(t, s, Options{Containers: []string{"T1"}})

	ids := make([]string, 0, len(sp.Messages))
	for _, m := range sp.Messages {
		ids = append(ids, m.ExtID)
	}
	rendered, err := RenderTrail(s, ids)
	if err != nil {
		t.Fatalf("RenderTrail: %v", err)
	}

	// The fixture states recipients — a To and a Cc — so an empty line here would
	// mean the pane reads "to —" for a message that named them.
	if len(ids) > 0 && rendered[ids[0]].To == "" {
		t.Fatal("no entry came back with a recipient line, so the comparison proves nothing")
	}
	for _, m := range sp.Messages {
		got, ok := rendered[m.ExtID]
		if !ok {
			t.Errorf("%s: not rendered at all", m.ExtID)
			continue
		}
		if got.HTML != m.Body {
			t.Errorf("%s: the pane and the page disagree\n pane: %q\n page: %q", m.ExtID, got.HTML, m.Body)
		}
		if got.To != m.To {
			t.Errorf("%s: the pane and the page write different recipients\n pane: %q\n page: %q",
				m.ExtID, got.To, m.To)
		}
		// The address behind the sender's name, which is what a hover names. The
		// pane has no other way to know it: a page build reads it off the same row
		// this does, and a pane that offered none (or a different one) would be the
		// same divergence wearing a smaller hat.
		if got.FromEmail != m.FromEmail {
			t.Errorf("%s: the pane and the page name different sender addresses\n pane: %q\n page: %q",
				m.ExtID, got.FromEmail, m.FromEmail)
		}
	}
}

// A caller renders the trail it found. An id the corpus does not hold is absent
// from the answer rather than fatal: refusing the whole trail over one entry
// would throw away the messages the caller does have.
func TestRenderingAnUnknownEntryIsAbsentRatherThanFatal(t *testing.T) {
	s := trail(t)

	rendered, err := RenderTrail(s, []string{"mail:<a@loomworks>", "mail:<nobody@nowhere>"})
	if err != nil {
		t.Fatalf("RenderTrail: %v", err)
	}
	if _, ok := rendered["mail:<nobody@nowhere>"]; ok {
		t.Error("an entry the corpus does not hold came back rendered")
	}
	if _, ok := rendered["mail:<a@loomworks>"]; !ok {
		t.Error("the entry the corpus does hold was not rendered")
	}

	empty, err := RenderTrail(s, nil)
	if err != nil || len(empty) != 0 {
		t.Errorf("nothing asked for = %v, %v; want an empty result and no error", empty, err)
	}
}

// The pane's path, on the same evidence: a recovered entry's recipients come from
// the participants table, and the line is made by the same function the page uses,
// so the two views cannot disagree about who a message went to.
func TestARecoveredEntryKeepsItsRecipientsInThePane(t *testing.T) {
	s, sp := quotedTo(t)
	var want string
	for _, m := range sp.Messages {
		if m.ExtID == "quote:sha-to" {
			want = m.To
		}
	}
	if want == "" {
		t.Fatal("the page has no recipient line for the recovered entry, so there is nothing to match")
	}

	rendered, err := RenderTrail(s, []string{"quote:sha-to"})
	if err != nil {
		t.Fatalf("RenderTrail: %v", err)
	}
	if got := rendered["quote:sha-to"].To; got != want {
		t.Errorf("pane to = %q, page to = %q", got, want)
	}
}

// quotedPane is a store holding one message recovered from quoted text, the two
// messages it was found inside, and one message of the trail that was found
// inside one of them too.
//
// Both hosts are outside the trail the pane is asked to render, which is the
// ordinary shape rather than a convenience: the pane names one entry at a time,
// and the message that quoted it is a different entry the caller never named.
// They are also built to the two kinds the corpus actually holds — one a real
// message with a From header, one an entry the corpus has a person for and no
// address on, which is every Slack message and any mail whose header block never
// arrived — because what the pane does with a missing half is part of the claim.
func quotedPane(t *testing.T) *corpus.Store {
	t.Helper()
	s := trail(t)
	ada := personOf(t, s, "ada@loomworks.example")
	bo := personOf(t, s, "bo@fjordline.example")

	quoter := put(t, s, msg{
		ext: "mail:<quoter@loomworks>", ts: "2026-03-04T09:00:00+11:00", tz: "AEDT",
		person: ada, container: "T1", subject: "Loom cutover",
		messageID: "<quoter@loomworks>", from: "Ada Byron <ada@loomworks.example>",
		to: "Bo Halvorsen <bo@fjordline.example>", gmail: "g-quoter",
	})
	forwarder := put(t, s, msg{
		ext: "mail:<forwarder@fjordline>", ts: "2026-03-05T09:00:00+11:00", tz: "AEDT",
		person: bo, container: "T1", subject: "Loom cutover",
		messageID: "<forwarder@fjordline>", gmail: "g-forwarder",
	})

	ts, _ := time.Parse(time.RFC3339, "2026-03-01T08:00:00+11:00")
	id, _, err := s.PutQuoted(corpus.Entry{
		Source: corpus.SourceMail, ExtID: "quote:recovered", TS: ts, TZ: "AEDT",
		PersonID: ada, Container: "T1", Subject: "Loom cutover", BodyText: "invented body",
	})
	if err != nil {
		t.Fatalf("PutQuoted: %v", err)
	}
	// Quoted twice by the one message: a sighting is keyed by (entry, host, kind),
	// so a host that both quoted and forwarded this entry is two rows in the store
	// and one person to a reader.
	for _, kind := range []string{"quoted", "forwarded"} {
		if err := s.Sight(id, quoter, kind, ""); err != nil {
			t.Fatalf("Sight %s: %v", kind, err)
		}
	}
	if err := s.Sight(id, forwarder, "quoted", ""); err != nil {
		t.Fatalf("Sight forwarder: %v", err)
	}
	// And a message of the trail that was seen quoted as well, so the pane has the
	// case where a host exists and the entry has an address of its own anyway.
	if err := s.Sight(findEntryID(t, s, "mail:<a@loomworks>"), quoter, "quoted", ""); err != nil {
		t.Fatalf("Sight the direct entry: %v", err)
	}
	return s
}

// personOf resolves a person the fixture already holds, so a host added later is
// the same human rather than a second row wearing their name and address — which
// would be a second person for the ops screen to try to merge.
func personOf(t *testing.T, s *corpus.Store, addr string) int64 {
	t.Helper()
	id, err := corpus.PersonByIdentity(s, corpus.KindEmail, addr)
	if err != nil {
		t.Fatalf("resolving %s: %v", addr, err)
	}
	return id
}

// findEntryID is a corpus id by ext id, for the sightings a fixture has to attach
// to an entry another fixture function created.
func findEntryID(t *testing.T, s *corpus.Store, ext string) int64 {
	t.Helper()
	var id int64
	if err := s.DB().QueryRow(`select id from entries where ext_id = ?`, ext).Scan(&id); err != nil {
		t.Fatalf("looking up %s: %v", ext, err)
	}
	return id
}

// A recovered entry has no address of its own, so the pane's hover names that
// absence and the people whose messages it was recovered from — the one address
// in that question which is evidence rather than a guess.
//
// Every name in the answer comes from a row outside the trail: the pane was asked
// for one entry, so the host is not in what it rendered, and the answer can only
// come from a load of its own. A pane that could name only what it was rendering
// would say nothing for exactly the entries this is about.
func TestARecoveredEntryNamesTheMessagesItWasRecoveredFrom(t *testing.T) {
	s := quotedPane(t)
	rendered, err := RenderTrail(s, []string{"quote:recovered"})
	if err != nil {
		t.Fatalf("RenderTrail: %v", err)
	}
	if len(rendered) != 1 {
		t.Fatalf("the pane rendered %d entries; the hosts have to be outside the trail "+
			"for this test to mean what it says", len(rendered))
	}
	got := rendered["quote:recovered"]
	if got.FromEmail != "" {
		t.Fatalf("FromEmail = %q; the fixture entry is meant to have no address of its own", got.FromEmail)
	}
	// The quoter twice over is one person named once, in sighting order; and the
	// host with no address on it is its name alone rather than a name followed by
	// empty brackets.
	want := "Ada Byron <ada@loomworks.example>, Bo Halvorsen"
	if got.QuotedBy != want {
		t.Errorf("quotedBy = %q, want %q", got.QuotedBy, want)
	}
}

// A message with an address of its own has nothing for a quoter to answer: naming
// one beside the sender's address would hand the reader two people to be looking
// at, and the second is not what the entry says about itself.
func TestADirectEntryNamesNoQuoter(t *testing.T) {
	s := quotedPane(t)
	rendered, err := RenderTrail(s, []string{"mail:<a@loomworks>"})
	if err != nil {
		t.Fatalf("RenderTrail: %v", err)
	}
	got := rendered["mail:<a@loomworks>"]
	if got.FromEmail != "ada@loomworks.example" {
		t.Fatalf("FromEmail = %q, want the fixture's own address", got.FromEmail)
	}
	if got.QuotedBy != "" {
		t.Errorf("quotedBy = %q for an entry that has an address of its own", got.QuotedBy)
	}
}
