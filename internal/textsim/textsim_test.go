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

// The distinction the positional measure exists for. The same count of extra
// words is an inline answer in one place and a footer in another, and only where
// they sit says which.
func TestDivergencesSeparatesAnInlineAnswerFromAFooter(t *testing.T) {
	base := Tokens("can you confirm the meter number for the depot before the pack goes out")
	inline := Tokens("can you confirm the meter number for the depot " +
		"yes it is on the last invoice before the pack goes out")
	footer := Tokens("can you confirm the meter number for the depot before the pack goes out " +
		"sent from a device that says so and scanned by an appliance")

	// Seven of the eight inserted words, not eight: the alignment matches the
	// "the" in the insertion against one of the base's own, which is what a
	// subsequence measure does with a common word and is why the threshold is set
	// well clear of the residue rather than at it.
	d := Divergences(base, inline)
	if !d.Measured || d.LongestInside != 7 || d.Appended != 0 {
		t.Fatalf("inline = %+v, want an interior run of 7 and nothing appended", d)
	}
	d = Divergences(base, footer)
	if !d.Measured || d.Inside != 0 || d.Appended == 0 {
		t.Fatalf("footer = %+v, want nothing inside and words appended", d)
	}
}

// Words the base has and the later rendition lacks are elision, which every
// requote does, and the measure must not report them as insertions.
func TestDivergencesIgnoresWhatTheLaterCopyElided(t *testing.T) {
	base := Tokens("can you confirm the meter number for the depot before the pack goes out")
	elided := Tokens("can you confirm the meter number before the pack goes out")
	if d := Divergences(base, elided); d.Inside != 0 || d.Appended != 0 || d.Before != 0 {
		t.Fatalf("elided = %+v, want no divergence attributed to the shorter copy", d)
	}
}

// A run of extra words at a point where the base also skips words of its own is
// two texts diverging rather than one containing the other. Counting it as an
// insertion is how a wrongly paired footer reads as an annotation.
func TestDivergencesHoldsBackWhereBothTextsDiverge(t *testing.T) {
	base := Tokens("please confirm the meter number and the account number for the depot")
	later := Tokens("please confirm the meter number wholly different words here for the depot")
	d := Divergences(base, later)
	if d.Inside != 0 || d.Astride == 0 {
		t.Fatalf("divergence = %+v, want the run counted astride rather than inside", d)
	}
}

// Align has to give the anchors, not just how many: a length cannot say where a
// gap between two matches falls.
func TestAlignReportsAnchorsInOrder(t *testing.T) {
	a := Tokens("one two three")
	b := Tokens("one two extra three")
	got := Align(a, b)
	want := [][2]int{{0, 0}, {1, 1}, {2, 3}}
	if len(got) != len(want) {
		t.Fatalf("anchors = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("anchors = %v, want %v", got, want)
		}
	}
}

// A pair too long to align says so rather than reporting a clean comparison, so
// a caller cannot read a skipped measurement as no divergence.
func TestDivergencesRefusesAPairTooLongToAlign(t *testing.T) {
	long := make([]string, maxAlignTokens+1)
	for i := range long {
		long[i] = "word"
	}
	if d := Divergences(long, long); d.Measured {
		t.Fatalf("divergence = %+v, want it to report that nothing was measured", d)
	}
}
