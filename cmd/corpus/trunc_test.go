package main

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// Every string a column is clipped from is prose, and prose in this corpus
// carries curly apostrophes, en dashes and the occasional emoji. Test data is
// invented rather than lifted from the corpus, but it is the same shapes.
const (
	multiByte = "Dana’s café — 日本語のテキスト, 40 % of it 🙂"
	family    = "roster 👩‍👩‍👧 signed"
	flagged   = "depot 🇦🇺 and 🇳🇿 both"
	toned     = "approved 👍🏽 by Dana"
)

// A clip that splits a rune emits bytes no UTF-8 reader can decode, and one such
// line poisons the whole stream for anything consuming the output. There is no
// safe width to check: the boundary moves with the string, so every width is
// checked.
func TestTruncIsValidUTF8AtEveryWidth(t *testing.T) {
	for _, s := range []string{multiByte, family, flagged, toned} {
		for n := 1; n <= utf8.RuneCountInString(s)+2; n++ {
			got := trunc(s, n)
			if !utf8.ValidString(got) {
				t.Errorf("trunc(%q, %d) = %q, not valid UTF-8", s, n, got)
			}
		}
	}
}

// The width is a column width, so it is a count of runes: 96 means 96 characters
// of Japanese as readily as 96 of English.
func TestTruncCountsRunes(t *testing.T) {
	for _, s := range []string{multiByte, family, flagged, toned} {
		for n := 1; n <= utf8.RuneCountInString(s)+2; n++ {
			if got := utf8.RuneCountInString(trunc(s, n)); got > n {
				t.Errorf("trunc(%q, %d) is %d runes wide", s, n, got)
			}
		}
	}
}

// The ellipsis is a rune of the budget, not an extra one. A caller printing into
// a %-28s field has 28 columns and no more.
func TestTruncPaysForItsOwnEllipsis(t *testing.T) {
	s := strings.Repeat("é", 40)
	got := trunc(s, 10)
	if n := utf8.RuneCountInString(got); n != 10 {
		t.Fatalf("trunc(40 runes, 10) = %q, %d runes wide", got, n)
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("trunc(40 runes, 10) = %q, expected a clip marker", got)
	}
}

func TestTruncLeavesASCIIAlone(t *testing.T) {
	const s = "Depot roster for the night shift"
	for _, n := range []int{len(s), len(s) + 1, 200} {
		if got := trunc(s, n); got != s {
			t.Errorf("trunc(%q, %d) = %q, want it untouched", s, n, got)
		}
	}
	if got, want := trunc(s, 10), "Depot ros…"; got != want {
		t.Errorf("trunc(%q, 10) = %q, want %q", s, got, want)
	}
}

// A value that fits is the value, with nothing appended: a marker on an
// unclipped string claims text was dropped that never existed.
func TestTruncAddsNoMarkerWhenItFits(t *testing.T) {
	for _, s := range []string{multiByte, "", "short"} {
		n := utf8.RuneCountInString(s)
		if got := trunc(s, n); got != s {
			t.Errorf("trunc(%q, %d) = %q, want it untouched", s, n, got)
		}
	}
}

// Splitting a grapheme cluster is accepted — the base emoji renders in place of
// the composed one — but a joiner left at the end of the run modifies nothing
// and some renderers will pull the ellipsis into the cluster.
func TestTruncLeavesNoDanglingJoiner(t *testing.T) {
	for _, s := range []string{family, flagged, toned, "wave \U0001f642\ufe0f now"} {
		for n := 1; n <= utf8.RuneCountInString(s)+2; n++ {
			got := trunc(s, n)
			body := strings.TrimSuffix(got, "…")
			if body == got {
				continue // not clipped, so nothing was cut away from a joiner
			}
			last, _ := utf8.DecodeLastRuneInString(body)
			if last == '\u200d' || (last >= '\ufe00' && last <= '\ufe0f') {
				t.Errorf("trunc(%q, %d) = %q ends in a dangling joiner", s, n, got)
			}
		}
	}
}

// n is a budget, and a budget of nothing buys nothing — not even a marker. A
// width derived from a narrow terminal or a subtraction can arrive here as zero
// or negative, so it has to be an answer rather than a panic.
func TestTruncWithNoBudget(t *testing.T) {
	for _, n := range []int{0, -1, -100} {
		if got := trunc(multiByte, n); got != "" {
			t.Errorf("trunc(_, %d) = %q, want empty", n, got)
		}
	}
}

// oneLine is what the search printer actually calls, and it flattens before it
// clips; the flattening must not reintroduce a split rune of its own.
func TestOneLineIsValidUTF8AtEveryWidth(t *testing.T) {
	s := "Dana’s café\n\treport — 日本語\n\n40 % done 🙂"
	for n := 1; n <= utf8.RuneCountInString(s)+2; n++ {
		got := oneLine(s, n)
		if !utf8.ValidString(got) {
			t.Errorf("oneLine(_, %d) = %q, not valid UTF-8", n, got)
		}
		if strings.ContainsAny(got, "\n\t") {
			t.Errorf("oneLine(_, %d) = %q, still has whitespace to flatten", n, got)
		}
	}
}
