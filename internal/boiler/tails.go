package boiler

// Candidate suffix fingerprints for the persisted index. Detect remains independent.

import (
	"crypto/sha256"
	"strings"
)

// FilledRules fills zero or negative thresholds with the detector's defaults.
func FilledRules(r Rules) Rules { return r.filled() }

// Tail is the group-independent fingerprint of a normalized suffix.
// Empty placeholders retain their line positions and can repeat a fingerprint;
// that multiplicity intentionally matches the existing implementation.
type Tail struct {
	Digest [16]byte
	Valid  bool
}

// Tails joins the last window lines once, then hashes suffix slices.
// It preserves tailKey's space-joining, empty-placeholder and wrapping behavior.
func Tails(lines []string, window int) []Tail {
	if window > len(lines) {
		window = len(lines)
	}
	if window <= 0 {
		return nil
	}
	lines = lines[len(lines)-window:]
	positions := make([]int, window)
	var joined strings.Builder
	for i, line := range lines {
		positions[i] = -1
		if line == "" {
			continue
		}
		if joined.Len() > 0 {
			joined.WriteByte(' ')
		}
		positions[i] = joined.Len()
		joined.WriteString(line)
	}
	text := joined.String()
	next := len(text)
	for i := len(positions) - 1; i >= 0; i-- {
		if positions[i] < 0 {
			positions[i] = next
		} else {
			next = positions[i]
		}
	}
	out := make([]Tail, window)
	for n := 1; n <= window; n++ {
		start := positions[window-n]
		if start == len(text) {
			continue
		}
		h := sha256.Sum256([]byte(text[start:]))
		copy(out[n-1].Digest[:], h[:16])
		out[n-1].Valid = true
	}
	return out
}
