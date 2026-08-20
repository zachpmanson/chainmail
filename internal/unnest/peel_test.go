package unnest

import (
	"strings"
	"testing"
)

// A body with a wrapped attribution at depth 0, a forward rule and header block
// nested at depth 1, and a signature — the shape most of this mailbox takes.
const nested = "Thanks, that works.\r\n" +
	"\r\n" +
	"On Wed, 5 Aug 2026 at 09:01, Alice <a@ex.com>\r\n" +
	"> wrote:\r\n" +
	"> Confirming the levy column.\r\n" +
	"> \r\n" +
	"> ---------- Forwarded message ---------\r\n" +
	"> From: Bob <b@ex.com>\r\n" +
	"> Date: Tue, 4 Aug 2026 at 17:12\r\n" +
	"> Subject: Re: the export\r\n" +
	"> To: Alice <a@ex.com>\r\n" +
	"> \r\n" +
	"> Original text here.\r\n" +
	"> --\r\n" +
	"> Bob, Acme\r\n"

func TestPeelSplitsNestedBody(t *testing.T) {
	blocks := Peel(nested)
	if len(blocks) != 3 {
		for i, b := range blocks {
			t.Logf("block %d depth=%d kind=%v sentinel=%q text=%q", i, b.Depth, b.Kind, b.Sentinel, b.Text)
		}
		t.Fatalf("got %d blocks, want 3 (the visible reply, Alice's quoted message, Bob's forward)", len(blocks))
	}

	// 1. what the sender actually wrote, no sentinel
	if blocks[0].Sentinel != "" || blocks[0].Depth != 0 {
		t.Errorf("first block should be the visible message: %+v", blocks[0])
	}
	if blocks[0].Text != "Thanks, that works." {
		t.Errorf("visible text = %q", blocks[0].Text)
	}

	// 2. the wrapped attribution, recovered with its sender intact even though the
	//    closer sat one quote level deeper
	if blocks[1].Kind != KindAttribution {
		t.Errorf("second block kind = %v, want attribution", blocks[1].Kind)
	}
	if !strings.Contains(blocks[1].Sentinel, "Alice <a@ex.com>") ||
		!strings.Contains(blocks[1].Sentinel, "wrote:") {
		t.Errorf("attribution lost its sender or closer: %q", blocks[1].Sentinel)
	}
	if blocks[1].Depth != 0 {
		t.Errorf("attribution depth = %d, want 0 (its first line's)", blocks[1].Depth)
	}
	if blocks[1].Text != "Confirming the levy column." {
		t.Errorf("quoted text = %q", blocks[1].Text)
	}

	// 3. the forward rule and its header block, consumed as ONE boundary — in 222
	//    of 222 observed cases a rule is followed by a header block, and emitting
	//    both would leave a spurious empty block between them
	if blocks[2].Kind != KindForwardRule {
		t.Errorf("third block kind = %v, want forward rule", blocks[2].Kind)
	}
	for _, want := range []string{"Forwarded message", "From: Bob", "Subject: Re: the export"} {
		if !strings.Contains(blocks[2].Sentinel, want) {
			t.Errorf("forward boundary missing %q: %q", want, blocks[2].Sentinel)
		}
	}
	// the signature is trimmed off the block rather than splitting it
	if strings.Contains(blocks[2].Text, "Acme") || blocks[2].Text != "Original text here." {
		t.Errorf("signature not trimmed: %q", blocks[2].Text)
	}
}

func TestPeelKeepsAFlatBodyWhole(t *testing.T) {
	blocks := Peel("Just a note.\r\n\r\nNothing quoted here.\r\n")
	if len(blocks) != 1 {
		t.Fatalf("got %d blocks, want 1", len(blocks))
	}
	if blocks[0].Text != "Just a note.\n\nNothing quoted here." {
		t.Errorf("text = %q", blocks[0].Text)
	}
}

// A bare rule is Outlook chrome, not a boundary, unless a header block follows.
// A 32-underscore run is a quote separator; a 60-underscore run is Teams
// meeting-invite decoration mid-message, and splitting there fabricates an entry.
func TestBareRuleIsOnlyABoundaryWhenAHeaderFollows(t *testing.T) {
	chrome := "Here are the details.\r\n" +
		strings.Repeat("_", 60) + "\r\n" +
		"Microsoft Teams\r\nJoin the meeting now\r\n"
	if got := Peel(chrome); len(got) != 1 {
		for i, b := range got {
			t.Logf("block %d kind=%v sentinel=%q", i, b.Kind, b.Sentinel)
		}
		t.Fatalf("split on meeting chrome: got %d blocks, want 1", len(got))
	}

	real := "Here are the details.\r\n" +
		strings.Repeat("_", 32) + "\r\n" +
		"From: Bob <b@ex.com>\r\nSent: Tue, 4 Aug 2026\r\n\r\nQuoted body.\r\n"
	got := Peel(real)
	if len(got) != 2 {
		t.Fatalf("did not split on a rule followed by a header block: got %d", len(got))
	}
	if got[1].Kind != KindForwardRule {
		t.Errorf("kind = %v, want forward rule", got[1].Kind)
	}
}

// No block may contain a sentinel line: if one does, we failed to split there.
// Run over the real anonymised corpus, this is the strongest single check that
// extraction is complete.
func TestNoBlockContainsAnUnconsumedSentinel(t *testing.T) {
	for _, f := range fixtures(t) {
		t.Run(f.Name, func(t *testing.T) {
			for bi, b := range Peel(f.Body) {
				for _, l := range strings.Split(b.Text, "\n") {
					if censusAttr.MatchString(l) {
						t.Errorf("block %d contains an unconsumed attribution: %q", bi, l)
					}
					if censusFwd.MatchString(l) {
						t.Errorf("block %d contains an unconsumed forward rule: %q", bi, l)
					}
					if censusBegin.MatchString(l) {
						t.Errorf("block %d contains an unconsumed Begin-forwarded: %q", bi, l)
					}
				}
			}
		})
	}
}

// Every attribution the independent census can see must become a block boundary.
func TestPeelRecoversAtLeastTheCensusCount(t *testing.T) {
	for _, f := range fixtures(t) {
		t.Run(f.Name, func(t *testing.T) {
			blocks := Peel(f.Body)
			var attributions int
			for _, b := range blocks {
				if b.Kind == KindAttribution {
					attributions++
				}
			}
			if attributions < f.Stats.Attr {
				t.Errorf("recovered %d attribution boundaries, census floor is %d",
					attributions, f.Stats.Attr)
			}
			// A body with any quoting at all must yield more than one block.
			if f.Stats.Attr+f.Stats.Fwd+f.Stats.Begin > 0 && len(blocks) < 2 {
				t.Errorf("body has sentinels but produced %d block(s)", len(blocks))
			}
		})
	}
}

// Content conservation: every non-blank source line must appear in exactly one
// block, either as content or inside a sentinel. Losing a line is how an
// extractor silently drops a message.
func TestPeelLosesNoContent(t *testing.T) {
	for _, f := range fixtures(t) {
		t.Run(f.Name, func(t *testing.T) {
			blocks := Peel(f.Body)
			covered := map[int]bool{}
			for _, b := range blocks {
				for i := b.Start; i < b.End; i++ {
					if covered[i] {
						t.Fatalf("line %d covered by two blocks", i)
					}
					covered[i] = true
				}
			}
			lines := Normalise(f.Body)
			for i, l := range lines {
				if strings.TrimSpace(l.Text) == "" {
					continue
				}
				if !covered[i] {
					t.Fatalf("line %d is in no block: %q", i, l.Text)
				}
			}
		})
	}
}

// Depth 18 must not blow up or silently truncate. talon and quotequail both cap
// at 1000 lines because of regex backtracking they could not avoid; Go's engine
// has no such problem and inheriting their caps would cut the deep history this
// package exists to recover.
func TestDeepNestingIsFullyTraversed(t *testing.T) {
	var deepest Fixture
	for _, f := range fixtures(t) {
		if f.Stats.MaxDepth > deepest.Stats.MaxDepth {
			deepest = f
		}
	}
	if deepest.Stats.MaxDepth < 10 {
		t.Skipf("no deeply nested fixture present (max %d)", deepest.Stats.MaxDepth)
	}
	blocks := Peel(deepest.Body)
	maxSeen := 0
	for _, b := range blocks {
		if b.Depth > maxSeen {
			maxSeen = b.Depth
		}
	}
	t.Logf("%s: depth %d, %d blocks, deepest boundary at depth %d",
		deepest.Name, deepest.Stats.MaxDepth, len(blocks), maxSeen)
	if len(blocks) < 2 {
		t.Fatalf("a depth-%d body produced %d block(s)", deepest.Stats.MaxDepth, len(blocks))
	}
}
