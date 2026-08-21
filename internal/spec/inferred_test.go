package spec

import (
	"strings"
	"testing"

	"github.com/zachpmanson/chainmail/internal/corpus"
)

// zoneTrail is a synthetic chain shaped like the real ones: two messages that
// arrived in the mailbox and state their offsets, and two recovered from the
// quoted text beneath them that state a wall clock and nothing else.
//
// Ada is placed — Europe/London explains both her stated offsets. Bo has never
// stated one.
// allNotes is every coverage line, joined, since an inference's evidence is one
// line among several.
func allNotes(sp Spec) string {
	var out []string
	for _, n := range sp.SourceNotes {
		out = append(out, n.Items...)
	}
	return strings.Join(out, " | ")
}

func zoneTrail(t *testing.T) *corpus.Store {
	s := open(t)
	ida := person(t, s, "Ada Byron", "ada@loomworks.example")
	idb := person(t, s, "Bo Halvorsen", "bo@fjordline.example")

	// Ada in winter and in summer: one zone, two offsets.
	put(t, s, msg{
		ext: "mail:<w@loomworks>", ts: "2026-01-20T09:00:00+00:00", tz: "GMT", offset: mins(0),
		person: ida, container: "T2", subject: "Winter", messageID: "<w@loomworks>",
		from: "Ada Byron <ada@loomworks.example>", gmail: "g-w",
	})

	// The recovered block: Bo wrote it, and Ada's client wrote the sentinel, so
	// the clock is Ada's local time.
	q := put(t, s, msg{
		ext: "quote:bo-1", ts: "2026-07-14T15:00:00Z",
		person: idb, container: "T1", subject: "Loom cutover",
		from: "Bo Halvorsen <bo@fjordline.example>",
	})
	// Ada's reply, in the mailbox, quoting it.
	r := put(t, s, msg{
		ext: "mail:<r@loomworks>", ts: "2026-07-14T15:30:00+01:00", tz: "BST", offset: mins(60),
		person: ida, container: "T1", subject: "Loom cutover",
		messageID: "<r@loomworks>", from: "Ada Byron <ada@loomworks.example>",
		to: "Bo Halvorsen <bo@fjordline.example>", gmail: "g-r",
	})
	// The reply is the child of the block it quotes, which is what makes it the
	// author of that block's sentinel.
	if err := s.SetParent(r, q); err != nil {
		t.Fatalf("SetParent: %v", err)
	}
	if err := s.Sight(q, r, "quoted", ""); err != nil {
		t.Fatalf("Sight: %v", err)
	}
	if err := s.Sight(r, 0, "direct", ""); err != nil {
		t.Fatalf("Sight: %v", err)
	}
	return s
}

// End to end: a recovered clock gets the zone of the client that wrote it, is
// published as inferred rather than stated, and the reasoning reaches the page.
func TestSpecPublishesAnInferredZoneAsAClaim(t *testing.T) {
	sp := generate(t, zoneTrail(t), Options{Containers: []string{"T1"}})

	var quoted, stated *Entry
	for i := range sp.Messages {
		if sp.Messages[i].Quoted {
			quoted = &sp.Messages[i]
		} else {
			stated = &sp.Messages[i]
		}
	}
	if quoted == nil || stated == nil {
		t.Fatalf("expected one recovered and one mailbox entry: %+v", sp.Messages)
	}
	if quoted.TZ != "+0100" || quoted.TZSource != tzInferred {
		t.Errorf("recovered entry = tz %q source %q, want +0100 inferred", quoted.TZ, quoted.TZSource)
	}
	// The clock is not moved: the sentinel wrote 15:00 in that zone, and 15:00 is
	// what a reader can check against the quoted text.
	if quoted.Time != "15:00" {
		t.Errorf("recovered clock = %q, want the wall clock as quoted", quoted.Time)
	}
	if stated.TZSource != tzStated {
		t.Errorf("mailbox entry = source %q, want stated", stated.TZSource)
	}
	if !strings.Contains(allNotes(sp), "Europe/London") {
		t.Errorf("the evidence for the inference must reach the page: %q", allNotes(sp))
	}
}

// Nothing is published for an entry nobody's client can place. The absent tz is
// the contract: the renderer shows an absent zone as unknown, and had a value
// been invented here there would have been nothing left to show as unknown.
func TestSpecPublishesNoZoneWhereNothingPlacesIt(t *testing.T) {
	s := open(t)
	idb := person(t, s, "Bo Halvorsen", "bo@fjordline.example")
	q := put(t, s, msg{
		ext: "quote:bo-2", ts: "2026-07-14T15:00:00Z",
		person: idb, container: "T3", subject: "Nothing stated",
		from: "Bo Halvorsen <bo@fjordline.example>",
	})
	if err := s.Sight(q, 0, "quoted", ""); err != nil {
		t.Fatalf("Sight: %v", err)
	}
	sp := generate(t, s, Options{Containers: []string{"T3"}})
	got := sp.Messages[0]
	if got.TZ != "" || got.TZSource != "" {
		t.Fatalf("published tz %q source %q, want neither", got.TZ, got.TZSource)
	}
	if items := zoneItems(sp); len(items) == 0 {
		t.Fatal("an unplaceable clock must be declared in the coverage notes")
	}
}
