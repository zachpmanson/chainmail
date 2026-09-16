package spec

import "testing"

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
