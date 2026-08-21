package textsim

import "testing"

// The case the head test exists for: two different questions from one person,
// signed and disclaimed identically. Unordered overlap says they are the same
// message; the opening says they are not.
func TestOrderedOpeningSeparatesTwoMessagesUnderOneSignature(t *testing.T) {
	boilerplate := " thanks deniz aslan operations quarry energy this email and any " +
		"attachments are confidential and may be privileged if you are not the " +
		"intended recipient please delete it and tell us at once"
	a := Tokens("can you confirm the meter number for the rothwell depot" + boilerplate)
	b := Tokens("do we have the august network invoice yet" + boilerplate)

	if got := Similarity(a, b); got < 0.7 {
		t.Fatalf("similarity = %.2f; the fixture no longer reproduces the case the "+
			"boilerplate defeats", got)
	}
	if got := HeadSimilarity(a, b, 8, 48); got >= 0.75 {
		t.Fatalf("head similarity = %.2f, want the openings to disagree", got)
	}
	if got := HeadSimilarity(a, a, 8, 48); got != 1 {
		t.Fatalf("a message's own opening scored %.2f, want 1", got)
	}
}

// A quoted copy is the same words elided and rewrapped, which is what recall of
// the shorter in the longer has to see through.
func TestAnElidedCopyIsStillContained(t *testing.T) {
	full := Tokens("Can you confirm the meter number for the Rothwell depot before " +
		"the tender pack goes out?\n\nThanks,\nDeniz")
	elided := Tokens("Can you confirm the meter number for the Rothwell depot before the " +
		"tender pack goes out? Thanks,")
	if n := Overlap(elided, full); n != len(elided) {
		t.Fatalf("%d of %d words of the elided copy are in the full one, want all",
			n, len(elided))
	}
}
