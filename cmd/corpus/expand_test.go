package main

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/zachpmanson/chainmail/internal/corpus"
)

// Everything here is invented. The corpus this command reads holds real
// correspondence, so fixtures use example.com addresses and content written for
// the test.

var expandDay = time.Date(2026, 4, 2, 9, 0, 0, 0, time.UTC)

// fakeChain stands in for Store.Chain: a chain is its entries in time order,
// which is the only property of the real walk the printing depends on.
func fakeChain(chains map[string][]corpus.Shown) chainSource {
	return func(extID string) ([]corpus.Shown, error) {
		items, ok := chains[extID]
		if !ok {
			return nil, fmt.Errorf("%q: %w", extID, corpus.ErrNotFound)
		}
		return items, nil
	}
}

func shown(ext, author, body string, min int) corpus.Shown {
	return corpus.Shown{
		ExtID: ext, Source: corpus.SourceMail, Author: author, Body: body,
		TS: expandDay.Add(time.Duration(min) * time.Minute),
	}
}

// hit describes a matched entry as ChainHit.Best carries it.
func hit(ext string, prose int, snippet string) corpus.EntryHit {
	return corpus.EntryHit{ExtID: ext, ProseRank: prose, Snippet: snippet}
}

func chainHit(root, subject string, entries int, best ...corpus.EntryHit) corpus.ChainHit {
	return corpus.ChainHit{
		RootExtID: root, Subject: subject,
		Entries: entries, Matched: len(best),
		First: expandDay, Last: expandDay.Add(time.Hour),
		Best: best,
	}
}

func render(t *testing.T, chains []corpus.ChainHit, src chainSource, o expandOpts) string {
	t.Helper()
	var b strings.Builder
	if err := printChains(&b, src, chains, o); err != nil {
		t.Fatalf("printChains: %v", err)
	}
	return b.String()
}

// The regression that matters. Expansion is additive, and the flag is the only
// thing that may change what a reader who did not ask for it sees.
func TestDefaultOutputIsUnchangedWithoutTheFlag(t *testing.T) {
	chains := []corpus.ChainHit{
		chainHit("mail:<r1@example.com>", "Levy reconciliation", 4,
			hit("mail:<a@example.com>", 1, "the [levy] as reconciled"),
			hit("mail:<b@example.com>", 3, "a second [levy] line")),
		chainHit("mail:<r2@example.com>", "", 1,
			hit("mail:<c@example.com>", 9, "one [levy] in passing")),
	}
	src := fakeChain(nil) // never consulted: nothing is expanded

	want := "" +
		"Levy reconciliation                              2/4   matched  2026-04-02 -> 2026-04-02\n" +
		"    root mail:<r1@example.com>\n" +
		"mail:<r2@example.com>                            1/1   matched  2026-04-02 -> 2026-04-02\n" +
		"    root mail:<r2@example.com>\n" +
		"\n2 chains\n"
	if got := render(t, chains, src, expandOpts{}); got != want {
		t.Errorf("summary output changed\n got %q\nwant %q", got, want)
	}
}

// The gate, from both sides, in one fixture: same flags, one chain over the
// corroboration floor and one under it.
func TestOnlyAChainAboveTheGateExpands(t *testing.T) {
	chains := []corpus.ChainHit{
		chainHit("root-over", "Levy reconciliation", 3,
			hit("over-1", 1, "the [levy] as reconciled"),
			hit("over-3", 2, "confirming the [levy]")),
		chainHit("root-under", "Weekly standup", 40,
			hit("under-1", 8, "someone mentioned the [levy]")),
	}
	src := fakeChain(map[string][]corpus.Shown{
		"root-over": {
			shown("over-1", "Ada Quill", "the levy as reconciled", 0),
			shown("over-2", "Bo Marsh", "no keyword of the query here at all", 5),
			shown("over-3", "Ada Quill", "confirming the levy", 9),
		},
		"root-under": {shown("under-1", "Cy Nolan", "someone mentioned the levy", 0)},
	})

	got := render(t, chains, src, expandOpts{Top: 5, MinMatched: 2})

	for _, want := range []string{"over-1", "over-2", "over-3"} {
		if !strings.Contains(got, want) {
			t.Errorf("qualifying chain did not expand: %s missing\n%s", want, got)
		}
	}
	if strings.Contains(got, "Cy Nolan") {
		t.Errorf("a chain with 1 matched entry expanded under -expand-min 2\n%s", got)
	}
	if !strings.Contains(got, "not expanded: 1 matched, -expand-min is 2") {
		t.Errorf("the gate declined a chain without saying why\n%s", got)
	}
}

// An expanded chain mixes what the query found with what came in by reply edge.
// A reader who cannot separate those cannot judge the result, so the labels are
// asserted per entry rather than merely asserted to exist somewhere.
func TestMatchedAndExpandedEntriesAreLabelledApart(t *testing.T) {
	chains := []corpus.ChainHit{
		chainHit("root", "Levy reconciliation", 3,
			hit("e1", 1, "the [levy] as reconciled"),
			hit("e3", 4, "confirming the [levy]")),
	}
	src := fakeChain(map[string][]corpus.Shown{"root": {
		shown("e1", "Ada Quill", "the levy as reconciled", 0),
		shown("e2", "Bo Marsh", "will check with the team tomorrow", 5),
		shown("e3", "Ada Quill", "confirming the levy", 9),
	}})

	got := render(t, chains, src, expandOpts{Top: 1, MinMatched: 2})
	// The label of the line naming an entry, found by the id rather than by
	// column, since a display name may hold spaces.
	labelOf := func(ext string) string {
		for _, line := range strings.Split(got, "\n") {
			f := strings.Fields(line)
			if len(f) < 4 || (f[0] != "match" && f[0] != "chain") {
				continue
			}
			for _, tok := range f[3:] {
				if tok == ext {
					return f[0]
				}
			}
		}
		return ""
	}
	for ext, want := range map[string]string{"e1": "match", "e2": "chain", "e3": "match"} {
		if got := labelOf(ext); got != want {
			t.Errorf("%s labelled %q, want %q", ext, got, want)
		}
	}
	// A matched entry keeps the ranking that found it, so the label is
	// corroborated by something and not the only evidence for itself.
	if !strings.Contains(got, "[prose 1]") {
		t.Errorf("a matched entry lost its ranking provenance\n%s", got)
	}
}

// Search reports the entry that matched, not the chain root. Expanding a mid-chain
// hit must therefore pull in what came BEFORE it: a reply without its question is
// the failure this whole feature exists to fix.
func TestExpansionReachesAncestorsAndDescendants(t *testing.T) {
	chains := []corpus.ChainHit{
		chainHit("root", "Levy reconciliation", 3,
			hit("middle", 1, "the [levy] again"),
			hit("middle2", 2, "still the [levy]")),
	}
	src := fakeChain(map[string][]corpus.Shown{"root": {
		shown("first", "Ada Quill", "the question nobody quoted the words of", 0),
		shown("middle", "Bo Marsh", "the levy again", 5),
		shown("middle2", "Bo Marsh", "still the levy", 6),
		shown("last", "Cy Nolan", "the answer, in different words", 9),
	}})

	got := render(t, chains, src, expandOpts{Top: 1, MinMatched: 2})
	if !strings.Contains(got, "first") {
		t.Errorf("expansion did not reach the ancestor of the match\n%s", got)
	}
	if !strings.Contains(got, "last") {
		t.Errorf("expansion did not reach the descendant of the match\n%s", got)
	}
}

// The ratio gate is off by default because a long on-topic thread has a low
// ratio for being long. Assert it exists and bites when asked for, since a knob
// that does nothing is worse than no knob.
func TestTheRatioGateAppliesOnlyWhenAskedFor(t *testing.T) {
	// 2 of 134: the shape of an incidental mention in a busy channel.
	chains := []corpus.ChainHit{
		chainHit("root", "#general", 134,
			hit("a", 1, "the [levy]"), hit("b", 2, "the [levy] again")),
	}
	src := fakeChain(map[string][]corpus.Shown{"root": {
		shown("a", "Ada Quill", "the levy", 0),
		shown("b", "Bo Marsh", "the levy again", 5),
	}})

	off := render(t, chains, src, expandOpts{Top: 1, MinMatched: 2})
	if !strings.Contains(off, "Ada Quill") {
		t.Errorf("no ratio gate was set, yet the chain did not expand\n%s", off)
	}

	on := render(t, chains, src, expandOpts{Top: 1, MinMatched: 2, MinRatio: 0.2})
	if strings.Contains(on, "Ada Quill") {
		t.Errorf("-expand-ratio 0.20 did not stop a chain matched 0.01 of\n%s", on)
	}
	if !strings.Contains(on, "-expand-ratio is 0.20") {
		t.Errorf("the ratio gate declined a chain without naming itself\n%s", on)
	}
}

// The rank cutoff is what bounds output when every chain is genuinely relevant,
// so it has to bite independently of the relevance gates.
func TestTheRankCutoffBoundsHowManyChainsExpand(t *testing.T) {
	var chains []corpus.ChainHit
	src := map[string][]corpus.Shown{}
	for i := 0; i < 4; i++ {
		root := fmt.Sprintf("root-%d", i)
		a, b := root+"-a", root+"-b"
		chains = append(chains, chainHit(root, "Levy "+root, 2,
			hit(a, 1, "the [levy]"), hit(b, 2, "the [levy] again")))
		src[root] = []corpus.Shown{
			shown(a, "Ada Quill", "the levy", 0),
			shown(b, "Bo Marsh", "the levy again", 5),
		}
	}

	got := render(t, chains, fakeChain(src), expandOpts{Top: 2, MinMatched: 2})
	if !strings.Contains(got, "2 expanded, 4 entries shown in full") {
		t.Errorf("-expand 2 expanded something other than 2 chains\n%s", got)
	}
	// The chains past the cutoff are still reported, as summaries.
	if !strings.Contains(got, "root root-3") {
		t.Errorf("a chain past the cutoff was dropped rather than summarised\n%s", got)
	}
	if !strings.Contains(got, "not expanded: -expand is 2 and that many already were") {
		t.Errorf("the cutoff declined a chain without saying so\n%s", got)
	}
}

// A chain of one entry is a real chain: a message whose parent is outside the
// mailbox is its own root. Expanding it must return it, so nothing has to
// special-case the degenerate case.
func TestAChainOfOneExpandsToItself(t *testing.T) {
	chains := []corpus.ChainHit{
		chainHit("solo", "Levy, once", 1, hit("solo", 1, "the [levy], once")),
	}
	src := fakeChain(map[string][]corpus.Shown{
		"solo": {shown("solo", "Ada Quill", "the levy, once", 0)},
	})

	got := render(t, chains, src, expandOpts{Top: 1, MinMatched: 1})
	if !strings.Contains(got, "match  2026-04-02 09:00  Ada Quill") {
		t.Errorf("a one-entry chain did not expand to itself\n%s", got)
	}
	if !strings.Contains(got, "1 expanded, 1 entry shown in full") {
		t.Errorf("expansion miscounted a chain of one\n%s", got)
	}
}

// Output volume is the reason expansion is not simply always on. The cap is
// checked between chains, so the bound is the cap plus one chain — asserted, so
// a later change to mid-chain truncation is a deliberate one.
func TestTheCapBoundsTotalExpandedEntries(t *testing.T) {
	var chains []corpus.ChainHit
	src := map[string][]corpus.Shown{}
	for i := 0; i < 5; i++ {
		root := fmt.Sprintf("root-%d", i)
		var best []corpus.EntryHit
		var items []corpus.Shown
		for j := 0; j < 10; j++ {
			ext := fmt.Sprintf("%s-%d", root, j)
			items = append(items, shown(ext, "Ada Quill", "the levy", j))
			if j < 2 {
				best = append(best, hit(ext, j+1, "the [levy]"))
			}
		}
		chains = append(chains, chainHit(root, "Levy "+root, 10, best...))
		src[root] = items
	}

	got := render(t, chains, fakeChain(src), expandOpts{Top: 5, MinMatched: 2, Cap: 25})
	// Three chains: the third starts at 20 printed, still under 25, and is not
	// cut short; the fourth is refused.
	if !strings.Contains(got, "3 expanded, 30 entries shown in full") {
		t.Errorf("the cap did not bound expansion at a chain boundary\n%s", got)
	}
	if !strings.Contains(got, "not expanded: -expand-cap of 25 entries reached") {
		t.Errorf("the cap declined a chain without naming itself\n%s", got)
	}
}

// The cap must not take the blame for a chain the relevance gates would have
// refused anyway, or the reader tunes the wrong knob.
func TestTheReasonNamesTheBindingConstraint(t *testing.T) {
	chains := []corpus.ChainHit{
		chainHit("big", "Levy reconciliation", 2,
			hit("big-a", 1, "the [levy]"), hit("big-b", 2, "the [levy] again")),
		chainHit("thin", "A passing mention", 40, hit("thin-a", 9, "the [levy]")),
	}
	src := fakeChain(map[string][]corpus.Shown{
		"big": {
			shown("big-a", "Ada Quill", "the levy", 0),
			shown("big-b", "Bo Marsh", "the levy again", 5),
		},
		"thin": {shown("thin-a", "Cy Nolan", "the levy", 0)},
	})

	got := render(t, chains, src, expandOpts{Top: 5, MinMatched: 2, Cap: 1})
	if !strings.Contains(got, "not expanded: 1 matched, -expand-min is 2") {
		t.Errorf("a chain below the floor was blamed on the cap\n%s", got)
	}
}

// End to end over a real store, because the unit tests above stub the chain
// walk and so cannot catch the two ways the wiring fails: a PerChain that clips
// ChainHit.Best (matched entries then print as "chain"), and a root the walk
// disagrees with (the chain then expands to a fragment).
//
// The fixture is the shape the measurement on the real corpus found: a long
// conversation where only some messages contain the query's words.
func TestExpansionRecoversTheEntriesSearchAloneMisses(t *testing.T) {
	s, err := corpus.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	var parent int64
	const chainLen = 12
	var want []string
	for i := 0; i < chainLen; i++ {
		ext := fmt.Sprintf("mail:<m%02d@example.com>", i)
		want = append(want, ext)
		// Only every fourth message says the words, and the shared subject does not
		// say them either. The rest are the replies that carry a thread without
		// restating what it is about, which is what entry-level retrieval cannot see.
		body := "noted, and I will come back to you on the rest"
		if i%4 == 0 {
			body = "the pasture levy schedule, as reconciled"
		}
		res, err := s.Put(corpus.Entry{
			Source: corpus.SourceMail, ExtID: ext, Kind: "message",
			TS:      expandDay.Add(time.Duration(i) * time.Hour),
			Subject: "the March schedule", BodyText: body,
		}, &corpus.Mail{MessageID: ext}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if parent != 0 {
			if err := s.SetParent(res.ID, parent); err != nil {
				t.Fatal(err)
			}
		}
		parent = res.ID
	}

	q := corpus.Query{Text: "pasture levy", Limit: 5}
	hits, err := s.SearchEntries(q)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) >= chainLen {
		t.Fatalf("entry search found %d of %d; the fixture no longer has anything to recover",
			len(hits), chainLen)
	}

	q.PerChain = expandPerChain
	chains, err := s.SearchChains(q)
	if err != nil {
		t.Fatal(err)
	}
	got := render(t, chains, s.Chain, expandOpts{Top: 1, MinMatched: 2, Cap: 300})
	for _, ext := range want {
		if !strings.Contains(got, ext) {
			t.Errorf("expansion missed %s; entry search found %d of %d\n%s",
				ext, len(hits), chainLen, got)
		}
	}
	// The mid-chain matches must not have been relabelled by the round trip.
	if strings.Count(got, "match  ") != 3 {
		t.Errorf("matched entries = %d, want the 3 that contain the words\n%s",
			strings.Count(got, "match  "), got)
	}
}
