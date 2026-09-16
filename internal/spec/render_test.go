package spec

import "testing"

// The whole reason RenderBodies exists: a view that draws messages without
// building a page has to draw what a page draws. If these ever diverge, the
// inbox pane and the page built from the same thread have quietly become two
// renderers again — which is the defect this path was added to remove — and the
// divergence would show up as a difference in a bubble rather than as a failure.
//
// Compared against a real build rather than against a literal: what the
// conversion produces is not the claim, that the two agree is.
func TestRenderedBodiesAreTheBodiesAPageBuilds(t *testing.T) {
	s := trail(t)
	sp := generate(t, s, Options{Containers: []string{"T1"}})

	ids := make([]string, 0, len(sp.Messages))
	for _, m := range sp.Messages {
		ids = append(ids, m.ExtID)
	}
	bodies, err := RenderBodies(s, ids)
	if err != nil {
		t.Fatalf("RenderBodies: %v", err)
	}

	for _, m := range sp.Messages {
		got, ok := bodies[m.ExtID]
		if !ok {
			t.Errorf("%s: not rendered at all", m.ExtID)
			continue
		}
		if got != m.Body {
			t.Errorf("%s: the pane and the page disagree\n pane: %q\n page: %q", m.ExtID, got, m.Body)
		}
	}
}

// A caller renders the trail it found. An id the corpus does not hold is absent
// from the answer rather than fatal: refusing the whole trail over one entry
// would throw away the messages the caller does have.
func TestRenderingAnUnknownEntryIsAbsentRatherThanFatal(t *testing.T) {
	s := trail(t)

	bodies, err := RenderBodies(s, []string{"mail:<a@loomworks>", "mail:<nobody@nowhere>"})
	if err != nil {
		t.Fatalf("RenderBodies: %v", err)
	}
	if _, ok := bodies["mail:<nobody@nowhere>"]; ok {
		t.Error("an entry the corpus does not hold came back rendered")
	}
	if _, ok := bodies["mail:<a@loomworks>"]; !ok {
		t.Error("the entry the corpus does hold was not rendered")
	}

	empty, err := RenderBodies(s, nil)
	if err != nil || len(empty) != 0 {
		t.Errorf("nothing asked for = %v, %v; want an empty result and no error", empty, err)
	}
}
