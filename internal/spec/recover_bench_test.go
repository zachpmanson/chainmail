package spec

import (
	"fmt"
	"strings"
	"testing"

	"golang.org/x/net/html"

	"github.com/zachpmanson/chainmail/internal/textsim"
)

// host turns n levels of quoted history into markup shaped like the clients that
// produce it: a table layout, a paragraph of the sender's own text, then the trail
// they quoted. This is what recovery is pointed at in the corpus, and the shape
// matters to the benchmark — an unladen <div> per level is not what a mail client
// writes.
//
// The size is the point: a host that has been through twenty replies carries most
// of the thread, and 29 recovered entries in one chain were found inside four such
// documents. Scoring is what recovery exists to do; parsing the same document
// again for every entry is what this is here to price.
//
// A level of quoted history is a paragraph, not a line: this is what makes the
// document big enough to matter. The words are picked from a vocabulary by the
// level's own index, because forty copies of one sentence are not what recovery is
// pointed at in a corpus — they are one message quoted forty times, which recovery
// declines as ambiguous, and a benchmark of a declined correlation measures the
// bounds rather than the parse.
var benchWords = strings.Fields("survey load path schedule anchor bolt lintel rafter joist purlin noggin " +
	"flashing batten joiner plaster render screed dampcourse weep hole lintel soffit fascia gable " +
	"valley ridge hip verge cill head jamb reveal architrave skirting cornice picture rail dado " +
	"moulding shaker bead quirk stop chamfer splay mitre tenon mortice dovetail rebate dado")

func benchPara(i int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Level %d: the numbers look right to me, roof access from the 14th it is.", i)
	for j := 0; j < 160; j++ {
		fmt.Fprintf(&b, " %s %s %d.", benchWords[(i*7+j)%len(benchWords)], benchWords[(i*13+j*3)%len(benchWords)], i*100+j)
	}
	return b.String()
}

func host(levels int) string {
	var head, tail strings.Builder
	head.WriteString(`<html><head><title>Re: Loom</title></head><body>` +
		`<div dir="ltr">Numbers look right, thanks.</div>`)
	for i := 0; i < levels; i++ {
		// Gmail's shape, nested: each reply states a header and puts the message it
		// answers in the blockquote under it. The sender's own words and sign-off are
		// inside the blockquote, which is where the entry that quotes them will be
		// found.
		fmt.Fprintf(&head, `<div class="gmail_quote"><div class="gmail_attr">`+
			`On Mon, 14 Apr 2026 at 09:%02d, Person %d &lt;p%d@loomworks.example&gt; wrote:</div>`+
			`<blockquote class="gmail_quote"><p>%s</p><p>Regards,<br>Sender %d</p>`,
			i%60, i, i, benchPara(i), i)
		tail.WriteString(`</blockquote></div>`)
	}
	return head.String() + tail.String() + `</body></html>`
}

// needleFrom is the text rendition of one level, as an entry recovered out of the
// host holds it: the words, without the markup.
func needleFrom(levels, level int) string {
	return benchPara(level) + fmt.Sprintf("\nRegards,\nSender %d\n", level)
}

// The benchmark's own check: a needle that recovers nothing measures nothing, and
// an entry too short to correlate is exactly the case recovery declines early.
func TestTheBenchmarkHostRecovers(t *testing.T) {
	h := host(40)
	for _, level := range []int{0, 20, 39} {
		// The second return is "a signature was folded", not "it recovered" — the
		// html coming back is the check that recovery happened at all.
		got, _ := recoverHTML(needleFrom(40, level), []string{h}, bodyFold{})
		if got == "" {
			t.Fatalf("level %d did not recover; the benchmark would be timing an early return", level)
		}
		if !strings.Contains(got, fmt.Sprintf("Level %d", level)) {
			t.Errorf("level %d recovered %q", level, got[:min(80, len(got))])
		}
	}
}

func BenchmarkRecoverHTML(b *testing.B) {
	h := host(40)
	text := needleFrom(40, 3)
	b.SetBytes(int64(len(h)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		recoverHTML(text, []string{h}, bodyFold{})
	}
}

// The same corpus-shaped work a chain request does: many entries, all found inside
// the one host, as gmail and Outlook quote the whole thread.
func BenchmarkRecoverHTMLManyEntriesFromOneHost(b *testing.B) {
	h := host(40)
	hosts := []string{h}
	b.SetBytes(int64(len(h)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for level := 0; level < 40; level++ {
			recoverHTML(needleFrom(40, level), hosts, bodyFold{})
		}
	}
}

func BenchmarkCandidatesOnly(b *testing.B) {
	h := host(40)
	b.SetBytes(int64(len(h)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		candidates(h)
	}
}

// The cache's one hazard, and the reason extraction now copies: one host answers
// for every entry mined out of it, so the second entry must get its own block and
// the first entry's extraction must not have taken it away. Moving the run out of
// the document (what extraction used to do) fails this in two ways — the second
// entry finds the block gone, or the walk panics on a node whose parent changed.
func TestTwoEntriesFromOneHostBothRecover(t *testing.T) {
	h := host(6)
	first, _ := recoverHTML(needleFrom(6, 2), []string{h}, bodyFold{})
	second, _ := recoverHTML(needleFrom(6, 4), []string{h}, bodyFold{})
	if !strings.Contains(first, "Level 2") || !strings.Contains(second, "Level 4") {
		t.Fatalf("first = %.60q, second = %.60q", first, second)
	}
	// And the first one again, now that the host's blocks are cached and the second
	// entry has been through them.
	again, _ := recoverHTML(needleFrom(6, 2), []string{h}, bodyFold{})
	if again != first {
		t.Errorf("the same entry recovered differently the second time:\n%q\n%q", first, again)
	}
}

// A cold and a warm host are the same host. This is what lets the cache be
// believed: the answer does not depend on whether the document has been parsed
// before.
func TestAColdHostAndAWarmOneAgree(t *testing.T) {
	h := host(3)
	needle := needleFrom(3, 1)
	cold, _ := recoverHTML(needle, []string{h}, bodyFold{})
	warm, _ := recoverHTML(needle, []string{h}, bodyFold{})
	if cold != warm || cold == "" {
		t.Errorf("cold %q, warm %q", cold, warm)
	}
}

// A parsed host is shared between every entry mined out of it, so a block has to be
// able to leave the document without disturbing it. Extraction used to *move* the
// run out of the host — correct while a host belonged to one recovery, and wrong
// once one parse answers for all of them: a later candidate still points into the
// document, and RemoveChild panics on a node whose parent has changed.
//
// This is that invariant, without needing a host shape that trips it: enumerate the
// blocks of one parsed document twice, extracting each one in between, and the
// document must still hold the same blocks, saying the same words.
func TestExtractingABlockLeavesTheDocumentAlone(t *testing.T) {
	doc, err := html.Parse(strings.NewReader(host(4)))
	if err != nil {
		t.Fatal(err)
	}
	body := findBody(doc)
	if body == nil {
		t.Fatal("no body in the host")
	}
	before := candidatesIn(body)
	if len(before) < 2 {
		t.Fatalf("the host has %d blocks; nothing overlapping to test", len(before))
	}
	for _, c := range before {
		if got := nodeText(c.extract()); got == "" {
			t.Fatal("a block extracted to nothing")
		}
	}
	after := candidatesIn(body)
	if len(after) != len(before) {
		t.Fatalf("the document held %d blocks, then %d: extraction consumed them", len(before), len(after))
	}
	for i := range before {
		if textsim.Similarity(before[i].tokens, after[i].tokens) != 1 {
			t.Errorf("block %d changed: %q then %q", i, before[i].tokens, after[i].tokens)
		}
	}
}
