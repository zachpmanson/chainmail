package eval

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeSet(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "set.json")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// A judged set is hand-written, and every one of these mistakes would otherwise
// produce a confident number rather than a complaint.
func TestASetThatCannotMeanAnythingIsRejected(t *testing.T) {
	for _, c := range []struct{ name, body, want string }{
		{"a misspelled field", `{"cases":[{"query":"x","relevent":["mail:a@example.com"]}]}`, "relevent"},
		{"no judgement either way", `{"cases":[{"query":"x"}]}`, "judges nothing"},
		{"both at once", `{"cases":[{"query":"x","expect_empty":true,"relevant":["mail:a@example.com"]}]}`, "expects nothing"},
		{"no query", `{"cases":[{"relevant":["mail:a@example.com"]}]}`, "no query"},
		{"no cases", `{"cases":[]}`, "no cases"},
		{"an unknown level", `{"level":"sentence","cases":[{"query":"x","expect_empty":true}]}`, "level"},
	} {
		_, err := LoadSet(writeSet(t, c.body))
		if err == nil {
			t.Errorf("%s was accepted", c.name)
			continue
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: error %q does not mention %q", c.name, err, c.want)
		}
	}
}

func TestALevellessSetJudgesEntries(t *testing.T) {
	// Entry level is the default because it is the strict one: a chain surfaces
	// as soon as any member does, so a set that meant chains and got entries
	// would score too low, while the reverse would score too high.
	set, err := LoadSet(writeSet(t, `{"cases":[{"query":"x","expect_empty":true}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if set.Level != LevelEntry {
		t.Errorf("level defaulted to %q, want %q", set.Level, LevelEntry)
	}
}

func TestConfigSpecParsing(t *testing.T) {
	c, err := ParseConfig("name=after,db=/tmp/x.db,mode=semantic,topk=40,minsim=0,noprefix=1")
	if err != nil {
		t.Fatal(err)
	}
	if c.Name != "after" || c.DB != "/tmp/x.db" || c.Mode != "semantic" || c.TopK != 40 {
		t.Errorf("parsed %+v", c)
	}
	if c.MinSim == nil || *c.MinSim != 0 {
		t.Errorf("minsim=0 parsed as %v, want an explicit zero", c.MinSim)
	}
	if !c.NoPrefix {
		t.Error("noprefix=1 did not take")
	}

	// An unset floor must stay unset, or a configuration that says nothing about
	// the floor would silently be measuring one.
	c, err = ParseConfig("mode=lexical")
	if err != nil {
		t.Fatal(err)
	}
	if c.MinSim != nil {
		t.Errorf("an unmentioned floor parsed as %v, want nil", *c.MinSim)
	}

	for _, spec := range []string{"mode=telepathy", "wobble=3", "topk=lots", "db"} {
		if _, err := ParseConfig(spec); err == nil {
			t.Errorf("%q was accepted", spec)
		}
	}
}
