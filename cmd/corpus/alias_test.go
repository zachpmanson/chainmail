package main

import (
	"strings"
	"testing"

	"github.com/zachpmanson/chainmail/internal/corpus"
)

// A refused merge has to be readable, not just counted: the number of merges
// alone reads identically whether the alias found nothing to do or declined
// everything it found, and only one of those leaves a duplicate behind.
func TestPrintAliasNamesEveryRefusedPair(t *testing.T) {
	var b strings.Builder
	printAlias(&b, corpus.AliasRepair{
		From: "old.example", To: "new.example", Merged: 3, Applied: true,
		Refused: []corpus.AliasRefusal{{
			Address: "vasa@old.example",
			KeepID:  325, KeepName: "Vasa Tolokau",
			DropID: 303, DropName: "Vasa Ngahere",
			Reason: "different surnames: ngahere, tolokau",
		}},
	})
	out := b.String()
	for _, want := range []string{
		"vasa@old.example", "Vasa Tolokau", "Vasa Ngahere",
		"303", "325", "different surnames", "refused 1 pair", "corpus merge",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output does not mention %q:\n%s", want, out)
		}
	}
}

func TestPrintAliasSaysWhenNothingWasWritten(t *testing.T) {
	var b strings.Builder
	printAlias(&b, corpus.AliasRepair{From: "old.example", To: "new.example", Merged: 2})
	out := b.String()
	if !strings.Contains(out, "would merge 2") || !strings.Contains(out, "nothing was written") {
		t.Errorf("a preview must say it wrote nothing:\n%s", out)
	}
}
