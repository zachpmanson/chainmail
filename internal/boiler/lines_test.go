package boiler

import (
	"strings"
	"testing"

	"github.com/zachpmanson/chainmail/internal/unnest"
)

func TestVisibleDropsTheBlanksClientsDisagreeAbout(t *testing.T) {
	// The same signature from two clients, one of which puts a blank line above
	// the phone number. Counting blanks would make these two different blocks,
	// each with half the evidence.
	a := Visible(unnest.Normalise("Numbers attached.\n\nAda Byron\n+61 400 000 000"))
	b := Visible(unnest.Normalise("Numbers attached.\nAda Byron\n\n\n+61 400 000 000\n"))
	if strings.Join(a, "|") != strings.Join(b, "|") {
		t.Errorf("visible lines differ: %v vs %v", a, b)
	}
}

func TestTailStartTakesTheBlankLineAboveTheBlockWithIt(t *testing.T) {
	// Leaving the blank behind ends the visible body on empty space, with a gap
	// above the disclosure that nothing on the page explains.
	lines := unnest.Normalise("Numbers attached.\n\nAda Byron\n+61 400 000 000")
	at := TailStart(lines, 2)
	if at != 1 {
		t.Fatalf("TailStart = %d, want 1 — the blank line goes with the signature", at)
	}
	if got := Visible(lines[:at]); len(got) != 1 || got[0] != "Numbers attached." {
		t.Errorf("kept %v, want just the sender's own line", got)
	}
}

func TestTailStartDeclinesWhenThereAreTooFewLines(t *testing.T) {
	lines := unnest.Normalise("Ada Byron")
	if at := TailStart(lines, 4); at != len(lines) {
		t.Errorf("TailStart = %d, want %d — asking for more lines than exist folds nothing",
			at, len(lines))
	}
}

func TestLinesPeelsAMailboxBodyAndDeclinesABareForward(t *testing.T) {
	body := "Looking into it.\n\nOn Thu, 7 May 2026 at 04:38, Ada Byron <ada@loomworks.example> wrote:\n" +
		"> Has the review finished?"
	lines, ok := Lines(body, true)
	if !ok {
		t.Fatal("Lines declined a body whose own text is above the quote")
	}
	if got := Visible(lines); len(got) != 1 || got[0] != "Looking into it." {
		t.Errorf("visible = %v, want only the sender's own line", got)
	}
	if _, ok := Lines("On Thu, 7 May 2026 at 04:38, Ada wrote:\n> Anything?", true); ok {
		t.Error("Lines accepted a body that opens on a quote boundary")
	}
}
