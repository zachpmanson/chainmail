package unnest

import (
	"strings"
	"testing"
	"time"
)

// The whole point: the same original, quoted by two different clients that each
// rewrapped it, must collapse to one entry. Hashing the text cannot do this,
// which is why the key is (address, time).
func TestDedupCollapsesRequotedCopiesDespiteRewrapping(t *testing.T) {
	at := func(s string) time.Time {
		v, err := time.Parse(time.RFC3339, s)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	a := Recovered{
		Address: "ro.laren@daystrom.fed", Sent: at("2026-08-19T07:52:00Z"),
		Block: Block{Text: "the levy column is\nwrapped at seventy-two"},
	}
	b := Recovered{
		Address: "Ro.Laren@daystrom.fed", Sent: at("2026-08-19T07:52:00Z"),
		Block: Block{Text: "the levy column\nis wrapped at\nforty"},
		// This copy came from a client that also wrote a Subject.
		Subject: "the export",
	}
	a.Key, b.Key = quoteKey(a), quoteKey(b)
	if a.Key != b.Key {
		t.Fatalf("rewrapped copies got different keys:\n  %s\n  %s", a.Key, b.Key)
	}
	got := Dedup([]Recovered{a, b})
	if len(got) != 1 {
		t.Fatalf("got %d entries, want 1", len(got))
	}
	// The union knows more than either copy alone.
	if got[0].Subject != "the export" {
		t.Errorf("merge lost the subject only one copy carried: %+v", got[0])
	}
	// The fuller text wins: a deeper quote is the elided one.
	if !strings.Contains(got[0].Block.Text, "seventy-two") {
		t.Errorf("kept the shorter text: %q", got[0].Block.Text)
	}
}

func TestDedupKeepsGenuinelyDifferentMessagesApart(t *testing.T) {
	base := time.Date(2026, 8, 19, 7, 52, 0, 0, time.UTC)
	rs := []Recovered{
		{Address: "a@x.fed", Sent: base, Block: Block{Text: "one"}},
		{Address: "b@x.fed", Sent: base, Block: Block{Text: "two"}},                  // other sender
		{Address: "a@x.fed", Sent: base.Add(time.Hour), Block: Block{Text: "three"}}, // other time
	}
	for i := range rs {
		rs[i].Key = quoteKey(rs[i])
	}
	if got := Dedup(rs); len(got) != 3 {
		t.Fatalf("collapsed distinct messages: got %d, want 3", len(got))
	}
}

// Without an address or a time there is nothing stable to key on, so the text
// hash is the fallback — and it must still collapse an exact repeat.
func TestDedupFallsBackToTextWhenNothingElseIsKnown(t *testing.T) {
	rs := []Recovered{
		{Block: Block{Text: "the  levy\ncolumn"}},
		{Block: Block{Text: "the levy column"}}, // same text, rewrapped
		{Block: Block{Text: "a different message"}},
	}
	for i := range rs {
		rs[i].Key = quoteKey(rs[i])
	}
	got := Dedup(rs)
	if len(got) != 2 {
		t.Fatalf("got %d, want 2 (whitespace must not distinguish)", len(got))
	}
}

// A Message-ID outranks everything: two copies carrying one must never split,
// even if their stated times disagree because a client rewrote the date format.
func TestMessageIDWinsOverTimeAndAddress(t *testing.T) {
	a := Recovered{MessageID: "abc@x", Address: "a@x.fed", Block: Block{Text: "one"}}
	b := Recovered{MessageID: "abc@x", Address: "b@x.fed",
		Sent: time.Now().UTC(), Block: Block{Text: "one but longer"}}
	a.Key, b.Key = quoteKey(a), quoteKey(b)
	if a.Key != b.Key {
		t.Fatalf("Message-ID did not dominate: %s vs %s", a.Key, b.Key)
	}
	if got := Dedup([]Recovered{a, b}); len(got) != 1 {
		t.Fatalf("got %d, want 1", len(got))
	}
}

// The measurement that motivated the key. The anonymised corpus CANNOT test
// cross-message dedup — each fixture was anonymised independently, so one
// original carries different fake names in different fixtures — but within a
// single body a chain still quotes the same message repeatedly, and that is
// measurable here.
func TestDedupReducesWithinABody(t *testing.T) {
	var raw, deduped int
	for _, f := range fixtures(t) {
		var rs []Recovered
		for _, b := range Peel(f.Body) {
			if b.Sentinel == "" {
				continue
			}
			rs = append(rs, Parse(b))
		}
		raw += len(rs)
		deduped += len(Dedup(rs))
	}
	t.Logf("sentinel blocks=%d after dedup=%d (%.1f%% collapsed)",
		raw, deduped, 100*float64(raw-deduped)/float64(raw))
	if deduped > raw {
		t.Fatal("dedup increased the count")
	}
	if raw == 0 {
		t.Fatal("no blocks")
	}
}
