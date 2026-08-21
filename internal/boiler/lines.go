package boiler

import (
	"strings"

	"github.com/zachpmanson/chainmail/internal/unnest"
)

// The reduction from a stored body to the lines a tail is counted over lives
// here so that the evidence pass and the fold that follows it cannot disagree.
// They run in different packages and at different times — one over the whole
// corpus, one over the entry being rendered — and a tail counted against one
// reduction and applied against another folds a number of lines nobody chose.

// Lines is the sequence of lines a body will show, and whether it shows any.
//
// peel drops the quoted history, and is the caller's decision for the same
// reason it is in internal/spec: only a body whose history has already been
// mined into entries of its own may lose it. false reports that the body opens
// on a quote boundary and so has nothing of its own — the sender forwarded
// without writing anything above it.
func Lines(text string, peel bool) ([]unnest.Line, bool) {
	lines := unnest.Normalise(text)
	if !peel {
		return lines, len(lines) > 0
	}
	blocks := unnest.Peel(text)
	if len(blocks) == 0 || blocks[0].Sentinel != "" {
		return nil, false
	}
	return lines[:blocks[0].End], true
}

// Visible is the lines a tail is counted over: the non-blank ones, in order.
//
// Blanks are dropped rather than counted because they are the one part of a
// signature that is not the sender's. Clients disagree about how much vertical
// space to put between a sign-off and a phone number, and the same block arrives
// with a blank line more or less depending on which client sent it; counting
// them would split one signature into two blocks, each with a third of the
// evidence.
func Visible(lines []unnest.Line) []string {
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		if strings.TrimSpace(l.Text) == "" {
			continue
		}
		out = append(out, l.Text)
	}
	return out
}

// TailStart returns the index in lines at which the last n visible lines begin,
// or len(lines) when there are fewer than n of them.
//
// The blank lines immediately above the block go with it. They were the space
// between the message and the signature, so leaving them behind ends the visible
// body on a blank line and puts a gap above the disclosure that nothing explains.
func TailStart(lines []unnest.Line, n int) int {
	if n <= 0 {
		return len(lines)
	}
	seen := 0
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(lines[i].Text) != "" {
			seen++
		}
		if seen < n {
			continue
		}
		for i > 0 && strings.TrimSpace(lines[i-1].Text) == "" {
			i--
		}
		return i
	}
	return len(lines)
}
