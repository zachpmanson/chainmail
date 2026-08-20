package unnest

import (
	"regexp"
	"testing"
)

var reAddr = regexp.MustCompile(`<[^<>]*>`)

// The six wrapped-attribution variants from the prior-art survey. Measured
// results there: Crisp's patterns handle only case A; quotequail corrupts the
// captured sender on B and C and misses D and E entirely; talon finds no
// attribution below depth 0. The whole point of separating depth from content is
// to pass all six.
func TestWrappedAttributionAtEveryDepth(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"A depth 0, closer on line 2",
			"On Wed, 17 Sep 2025 at 6:22 pm, Alice <a@ex.com>\nwrote:\ntext\n"},
		{"B depth 1, > on both lines",
			"> On Wed, 17 Sep 2025 at 6:22 pm, Alice <a@ex.com>\n> wrote:\n> text\n"},
		{"C mismatch, > then >>",
			"> On Wed, 17 Sep 2025 at 6:22 pm, Alice <a@ex.com>\n>> wrote:\n>> text\n"},
		{"D mismatch, >> then > (continuation shallower)",
			">> On Wed, 17 Sep 2025 at 6:22 pm, Alice <a@ex.com>\n> wrote:\n> text\n"},
		{"E three-line wrap at depth 0",
			"On Wed, 17 Sep 2025 at 6:22 pm,\nAlice\n<a@ex.com> wrote:\ntext\n"},
		{"F unwrapped control",
			"On Wed, 17 Sep 2025 at 6:22 pm, Alice <a@ex.com> wrote:\ntext\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			lines := Normalise(c.body)
			b, ok := FindAttribution(lines, 0)
			if !ok {
				t.Fatalf("no attribution found in:\n%s", c.body)
			}
			if b.Kind != KindAttribution {
				t.Fatalf("kind = %v", b.Kind)
			}
			// The sender must come out clean — no quote markers leaking in, which
			// is the specific way quotequail fails rather than erroring.
			if got := b.Text; !contains(got, "Alice") || !contains(got, "a@ex.com") {
				t.Fatalf("sender lost: %q", got)
			}
			// A leaked marker is a ">" outside an <address>. Checking for any ">"
			// would trip on the address itself, which is exactly how quotequail's
			// corruption ("Jane Smith <j@x> >") hides: the marker looks like part
			// of a legitimate bracket pair.
			if bare := reAddr.ReplaceAllString(b.Text, ""); contains(bare, ">") {
				t.Fatalf("quote marker leaked into the sentinel: %q", b.Text)
			}
			// Depth is the FIRST line's, whatever the continuation did.
			want := lines[0].Depth
			if b.Depth != want {
				t.Fatalf("depth = %d, want %d (the first line's)", b.Depth, want)
			}
		})
	}
}

// The greedy-On bug, which GitHub's corpus names `greedy_on`: a prose "On" ahead
// of a real attribution must not be swallowed into it, or the parsed sender is
// prose. This is what the "tempered dot" defends against, expressed here as
// "never cross a second opener".
func TestProseOpenerIsNotSwallowed(t *testing.T) {
	lines := Normalise("On the whole I agree with this.\n\nOn Mon, 15 Sep, Bob <b@ex.com> wrote:\ntext\n")
	if b, ok := FindAttribution(lines, 0); ok {
		t.Fatalf("matched from the prose line: %q", b.Text)
	}
	b, ok := FindAttribution(lines, 2)
	if !ok {
		t.Fatal("missed the real attribution on line 2")
	}
	if contains(b.Text, "the whole") {
		t.Fatalf("prose leaked into the attribution: %q", b.Text)
	}
}

// The tempering rule proper: a second opener on the IMMEDIATELY following line,
// with no blank line to stop the scan. Without "never cross a second opener" the
// join runs from the prose line to the real attribution's closer and captures a
// sender belonging to neither — the `greedy_on` bug.
//
// The earlier prose test passes even without the rule, because a blank line ends
// the scan first; this one isolates the rule itself.
func TestScanRefusesToCrossASecondOpener(t *testing.T) {
	lines := Normalise("On the whole I agree.\nOn Mon, 15 Sep, Bob <b@ex.com> wrote:\ntext\n")
	if b, ok := FindAttribution(lines, 0); ok {
		t.Fatalf("crossed into the next attribution: %q", b.Text)
	}
	b, ok := FindAttribution(lines, 1)
	if !ok {
		t.Fatal("missed the real attribution on line 1")
	}
	if contains(b.Text, "the whole") {
		t.Fatalf("prose leaked into the attribution: %q", b.Text)
	}
	if !contains(b.Text, "Bob") {
		t.Fatalf("wrong sender captured: %q", b.Text)
	}
}

func TestAttributionDoesNotStraddleABlankLine(t *testing.T) {
	lines := Normalise("On Wed, Alice <a@ex.com>\n\nwrote:\n")
	if _, ok := FindAttribution(lines, 0); ok {
		t.Fatal("joined across a blank line")
	}
}

func TestDepthCountingFollowsRFC3676(t *testing.T) {
	for _, c := range []struct {
		raw   string
		text  string
		depth int
	}{
		{"no markers", "no markers", 0},
		{"> one", "one", 1},
		{">> two", "two", 2},
		{"> > two, space-stuffed", "two, space-stuffed", 2},
		{">>> three", "three", 3},
		{"  > leading whitespace", "leading whitespace", 1},
	} {
		text, depth := splitDepth(c.raw)
		if text != c.text || depth != c.depth {
			t.Errorf("%q -> (%q, %d), want (%q, %d)", c.raw, text, depth, c.text, c.depth)
		}
	}
}

// Gmail writes the time with U+202F and Outlook with U+00A0; Go's \s matches
// neither, and 238 messages in this mailbox contain the former. Folding them in
// normalisation is what keeps every downstream pattern simple.
func TestOddWhitespaceIsFolded(t *testing.T) {
	lines := Normalise("On Wed, 17 Sep 2025 at 6:22 pm, Alice <a@ex.com> wrote:\n")
	if _, ok := FindAttribution(lines, 0); !ok {
		t.Fatal("narrow no-break space defeated the match")
	}
	lines = Normalise("On Wed, 17 Sep 2025 at 6:22 pm, Alice <a@ex.com> wrote:\n")
	if _, ok := FindAttribution(lines, 0); !ok {
		t.Fatal("non-breaking space defeated the match")
	}
}

func TestCRLFBodiesAreHandled(t *testing.T) {
	lines := Normalise("On Wed, Alice <a@ex.com>\r\nwrote:\r\ntext\r\n")
	if _, ok := FindAttribution(lines, 0); !ok {
		t.Fatal("CRLF defeated the match — the \\r was not stripped")
	}
}

// Two consecutive keys are required. One key is not a block: 32 messages here
// have a From: line with no address, so requiring an address rejects real blocks,
// while "Passcode: 1234" would be accepted by anything looser.
func TestHeaderBlockNeedsTwoKeys(t *testing.T) {
	if _, ok := FindHeaderBlock(Normalise("From: Alice | Acme\nnot a header\n"), 0); ok {
		t.Fatal("accepted a single header key")
	}
	if _, ok := FindHeaderBlock(Normalise("Passcode: 1234\nMeeting ID: 999\n"), 0); ok {
		t.Fatal("accepted Key: value prose as a header block")
	}
	b, ok := FindHeaderBlock(Normalise("From: Alice | Acme\nSent: Monday, 29 September 2025 9:00 AM\nTo: Bob\n"), 0)
	if !ok {
		t.Fatal("rejected a real three-key block with no address")
	}
	if b.End != 3 {
		t.Fatalf("consumed %d lines, want 3", b.End)
	}
}

// All four Outlook dialects must reach one pattern.
func TestBoldHeaderDialects(t *testing.T) {
	for _, body := range []string{
		"From: Alice\nTo: Bob\n",
		"*From:* Alice\n*To:* Bob\n",
		"*From: *Alice\n*To: *Bob\n",
		"*From:* *Alice*\n*To:* Bob\n",
	} {
		if _, ok := FindHeaderBlock(Normalise(body), 0); !ok {
			t.Fatalf("dialect rejected:\n%s", body)
		}
	}
}

func contains(s, sub string) bool { return len(s) >= len(sub) && stringsContains(s, sub) }
func stringsContains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
