package boiler

import "testing"

func TestSuffixFingerprintEquivalence(t *testing.T) {
	sets := [][]string{{"One", "Two words"}, {"One Two words"}, {"", "One", "", "Two words", ""}, {"", ""}, {"line\ninside", "unicode γ"}, {" ", "One"}}
	oldToNew := map[digest][16]byte{}
	newToOld := map[[16]byte]digest{}
	for _, lines := range sets {
		tails := Tails(lines, 25)
		for n, tail := range tails {
			old, ok := tailKey("same-group", lines, n+1)
			if ok != tail.Valid {
				t.Fatal("empty-tail mismatch")
			}
			if !ok {
				continue
			}
			if previous, exists := oldToNew[old]; exists && previous != tail.Digest {
				t.Fatal("previously equal tails split")
			}
			if previous, exists := newToOld[tail.Digest]; exists && previous != old {
				t.Fatal("previously distinct tails merged")
			}
			oldToNew[old] = tail.Digest
			newToOld[tail.Digest] = old
		}
	}
}
