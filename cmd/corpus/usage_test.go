package main

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// The usage text and the dispatch drifted apart once already: seven of eleven
// subcommands existed but were listed nowhere, so they were reachable only by
// reading this file. Auditing that by hand works exactly until someone forgets,
// so it is asserted instead.
//
// These tests read main.go as text rather than reflecting over the code, because
// the thing under test IS the text: a flag is documented only if a human reading
// the usage string can see it.
func source(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

var (
	reCase = regexp.MustCompile(`(?m)^\tcase "([a-z-]+)":`)
	reFlag = regexp.MustCompile(`fs\.(?:String|Int|Int64|Bool)\("([a-z-]+)"`)
)

func TestEverySubcommandIsListedInUsage(t *testing.T) {
	s := source(t)
	for _, m := range reCase.FindAllStringSubmatch(s, -1) {
		name := m[1]
		switch name {
		case "-h", "--help", "help", "mail", "slack":
			// help aliases, and the positional sources of `ingest`, which the
			// usage text documents under their parent rather than as commands.
			continue
		}
		if !strings.Contains(usage, "\n  "+name) {
			t.Errorf("subcommand %q is dispatched but not listed in usage", name)
		}
	}
}

func TestEveryFlagAppearsInItsSubcommandUsage(t *testing.T) {
	s := source(t)
	// Split the file on dispatch cases so each block holds one subcommand's
	// flags and its own usage strings.
	idx := reCase.FindAllStringSubmatchIndex(s, -1)
	for i, m := range idx {
		name := s[m[2]:m[3]]
		end := len(s)
		if i+1 < len(idx) {
			end = idx[i+1][0]
		}
		block := s[m[1]:end]

		flags := reFlag.FindAllStringSubmatch(block, -1)
		if len(flags) == 0 {
			continue
		}
		// Every usage string reachable from this block, plus the shared consts,
		// since some subcommands document their flags in one of those.
		text := block + usage + ingestUsage
		for _, f := range flags {
			if !strings.Contains(text, "-"+f[1]) {
				t.Errorf("%s: flag -%s is accepted but appears in no usage text", name, f[1])
			}
		}
	}
}
