package boiler

import (
	"strings"
	"testing"

	"github.com/zachpmanson/chainmail/internal/unnest"
)

// Every block here is invented; only the rendering differences are real, and
// they are shapes rather than anybody's correspondence.

// linked and bare are one signature as two clients spell it. The words are
// identical; the link, the image, the mailto, the bold and the wrapping are not.
const (
	linked = "Regards\nAda Byron\nHead of Metering | www.loomworks.example <https://www.loomworks.example>\n" +
		"E ada@loomworks.example<mailto:ada@loomworks.example>"
	bare = "Regards\n*Ada Byron*\nHead of Metering  |   www.loomworks.example\nE ada@loomworks.example"
)

func detectOf(t *testing.T, bodies ...string) map[int64]Fold {
	t.Helper()
	var msgs []Message
	for i, b := range bodies {
		lines, ok := Lines(b, false)
		if !ok {
			t.Fatalf("body %d has no visible lines", i)
		}
		msgs = append(msgs, Message{ID: int64(i + 1), Author: ada,
			Domain: "loomworks.example", Lines: Match(lines)})
	}
	return Detect(msgs, Default())
}

func TestOneSignatureInTwoRenditionsIsOneBlock(t *testing.T) {
	// Three messages, two of which came back through a client that renders the
	// href beside the label. Verbatim this is two blocks of one and two, neither
	// clearing three; the sender keeps their signature on screen for no reason
	// but which mailbox the copy came from.
	got := detectOf(t, "Numbers attached.\n"+linked, "Same again.\n"+bare, "And once more.\n"+linked)
	for id, want := range map[int64]int{1: 4, 2: 4, 3: 4} {
		if f := got[id]; f.Lines != want || f.Count != 3 || f.Scope != Author {
			t.Errorf("body %d folded %+v, want %d lines over 3 messages at author scope",
				id, f, want)
		}
	}
}

func TestAnInlineImageIsTheSameBlockWhicheverPlaceholderTheClientLeft(t *testing.T) {
	// The same logo under the same sign-off: one client names the file in angle
	// brackets, one brackets the alt text and the href it linked to, one emits the
	// "[image: ...]" that unnest already folds away.
	angle := "Regards\nAda Byron\n<logo.png>"
	alt := "Regards\nAda Byron\n[Loomworks]<https://www.loomworks.example/>"
	inline := "Regards\nAda Byron\n[image: logo.png]"
	got := detectOf(t, "One.\n"+angle, "Two.\n"+alt, "Three.\n"+inline)
	for id := int64(1); id <= 3; id++ {
		if f := got[id]; f.Count != 3 || f.Scope != Author {
			t.Errorf("body %d folded %+v, want a block over all 3 messages", id, f)
		}
	}
}

func TestTwoDifferentSignaturesDoNotMerge(t *testing.T) {
	// The normalisation takes off the client's rendering and must take off nothing
	// else. These two sign-offs differ in the words, so no amount of it may make
	// them one block: three messages each, and each side folds only its own.
	var msgs []Message
	for i := 0; i < 3; i++ {
		for who, sig := range map[int64][]string{
			ada:   {"Ada Byron", "Head of Metering | www.loomworks.example <https://www.loomworks.example>"},
			grace: {"Grace Hopper", "Head of Retail | www.loomworks.example <https://www.loomworks.example>"},
		} {
			lines, _ := Lines("Point taken.\n"+strings.Join(sig, "\n"), false)
			msgs = append(msgs, Message{ID: int64(len(msgs) + 1), Author: who,
				Domain: "loomworks.example", Lines: Match(lines)})
		}
	}
	for id, f := range Detect(msgs, Default()) {
		if f.Count != 3 {
			t.Errorf("body %d folded a block over %d messages; there are only 3 of each "+
				"signature, so a higher count is two signatures read as one", id, f.Count)
		}
	}
}

func TestAnOrgNoticeStillYieldsToTheSenderWhoseSignatureItSitsUnder(t *testing.T) {
	// The domain pass spans senders, so it is where a normalisation that reached
	// too far would show: the notice below every signature at the domain matches
	// on more messages than any one signature does. The author block is the more
	// specific claim and starts higher, and has to keep winning.
	notice := []string{"This message is confidential.", "Delete it if it was not meant for you."}
	var msgs []Message
	add := func(id, who int64, sig ...string) {
		msgs = append(msgs, Message{ID: id, Author: who, Domain: "loomworks.example",
			Lines: append(append([]string{"Noted."}, sig...), notice...)})
	}
	for i := 0; i < 3; i++ {
		add(int64(10+i), ada, "Ada Byron", "Head of Metering")
		add(int64(20+i), grace, "Grace Hopper")
	}
	got := Detect(msgs, Default())
	if f := got[10]; f.Scope != Author || f.Lines != 4 {
		t.Errorf("Ada's fold = %+v, want her whole 4-line block at author scope", f)
	}
	if f := got[20]; f.Scope != Author || f.Lines != 3 {
		t.Errorf("Grace's fold = %+v, want her whole 3-line block at author scope", f)
	}
}

func TestMatchIsForComparisonAndTheFoldedTextIsTheSenderS(t *testing.T) {
	// The invariant the whole file rests on: normalising for comparison and then
	// folding the normalised form would rewrite bodies that this repository exists
	// to preserve. Match may only ever decide how many of Visible's lines go
	// behind the disclosure, and Visible must be the sender's bytes.
	lines := unnest.Normalise("Numbers attached.\n" + linked)
	vis, match := Visible(lines), Match(lines)
	if len(vis) != len(match) {
		t.Fatalf("Visible has %d lines and Match %d; a fold counted on one and applied "+
			"to the other folds a block nobody chose", len(vis), len(match))
	}
	for i, want := range strings.Split("Numbers attached.\n"+linked, "\n") {
		if vis[i] != want {
			t.Errorf("visible line %d = %q, want the sender's own %q", i, vis[i], want)
		}
	}
	if match[3] == vis[3] {
		t.Error("Match left the rendered href in place, so nothing was normalised")
	}
}

func TestATailOfNothingButPlaceholdersIsNotEvidence(t *testing.T) {
	// A body ending in an image and nothing else normalises to empty lines. Every
	// such tail in a corpus agrees with every other, and folding on that agreement
	// is folding on no words at all.
	got := detectOf(t,
		"The June file is attached.\n<logo.png>\n<divider.png>",
		"Unrelated question about metering.\n<logo.png>\n<divider.png>",
		"Third thing entirely.\n<logo.png>\n<divider.png>")
	for id := int64(1); id <= 3; id++ {
		if f := got[id]; f.Scope != NoScope {
			t.Errorf("body %d folded %+v on a tail with no words in it", id, f)
		}
	}
}
