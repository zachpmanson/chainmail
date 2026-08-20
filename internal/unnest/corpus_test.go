package unnest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Fixture is one anonymised real message. See testdata/README.md.
type Fixture struct {
	Name    string `json:"name"`
	Role    string `json:"role"`
	From    string `json:"from"`
	To      string `json:"to"`
	Cc      string `json:"cc"`
	Subject string `json:"subject"`
	Date    string `json:"date"`
	Body    string `json:"body"`
	Stats   struct {
		Attr     int `json:"attr"`
		Wrapped  int `json:"wrapped"`
		Hdr      int `json:"hdr"`
		Fwd      int `json:"fwd"`
		Begin    int `json:"begin"`
		Orig     int `json:"orig"`
		MaxDepth int `json:"max_depth"`
	} `json:"stats"`
}

func fixtures(t *testing.T) []Fixture {
	t.Helper()
	paths, err := filepath.Glob("testdata/*.json")
	if err != nil {
		t.Fatal(err)
	}
	var out []Fixture
	for _, p := range paths {
		if filepath.Base(p) == "index.json" {
			continue
		}
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		var f Fixture
		if err := json.Unmarshal(b, &f); err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		out = append(out, f)
	}
	if len(out) == 0 {
		t.Fatal("no fixtures found")
	}
	return out
}

// An independent census of sentinel lines, by regex over the fixture body. This
// is deliberately a *different* implementation from the parser's, so the parser
// cannot define its own recall floor. Note `\r?$` everywhere: bodies are CRLF and
// Go's `$` does not consume the \r — an earlier version of this census silently
// reported zero attributions across every fixture for exactly that reason.
var (
	censusAttr    = regexp.MustCompile(`(?m)^[ \t]*(?:>[ \t]*)*On[ \t].{4,300}wrote:[ \t]*\r?$`)
	censusWrapped = regexp.MustCompile(`(?m)^[ \t]*(?:>[ \t]*)*wrote:[ \t]*\r?$`)
	censusFwd     = regexp.MustCompile(`(?mi)^[ \t]*(?:>[ \t]*)*-{2,}[ \t]?forwarded message[ \t]?-{2,}[ \t]*\r?$`)
	censusBegin   = regexp.MustCompile(`(?mi)^[ \t]*(?:>[ \t]*)*begin forwarded message[ \t]*:[ \t]*\r?$`)
	censusOrig    = regexp.MustCompile(`(?mi)^[ \t]*(?:>[ \t]*)*-{3,}[ \t]?original message[ \t]?-{3,}[ \t]*\r?$`)
)

func TestFixturesLoadAndMatchTheirCensus(t *testing.T) {
	for _, f := range fixtures(t) {
		t.Run(f.Name, func(t *testing.T) {
			if f.Body == "" {
				t.Fatal("empty body")
			}
			// The committed census must still describe the committed body —
			// anonymisation is supposed to preserve structure exactly, so a drift
			// here means the anonymiser damaged a sentinel.
			if got := len(censusAttr.FindAllString(f.Body, -1)); got != f.Stats.Attr {
				t.Errorf("attributions: body has %d, stats say %d", got, f.Stats.Attr)
			}
			if got := len(censusWrapped.FindAllString(f.Body, -1)); got != f.Stats.Wrapped {
				t.Errorf("wrapped closers: body has %d, stats say %d", got, f.Stats.Wrapped)
			}
			if got := len(censusFwd.FindAllString(f.Body, -1)); got != f.Stats.Fwd {
				t.Errorf("forward rules: body has %d, stats say %d", got, f.Stats.Fwd)
			}
		})
	}
}

func TestNormaliseMeasuresTheDepthTheCensusSaw(t *testing.T) {
	for _, f := range fixtures(t) {
		t.Run(f.Name, func(t *testing.T) {
			lines := Normalise(f.Body)
			max := 0
			for _, l := range lines {
				if l.Depth > max {
					max = l.Depth
				}
			}
			if max != f.Stats.MaxDepth {
				t.Errorf("max depth = %d, census says %d", max, f.Stats.MaxDepth)
			}
			// Normalisation must not eat content: a stripped line plus its markers
			// has to account for the original, modulo the noise we fold on purpose.
			for i, l := range lines {
				if strings.Contains(l.Text, "\r") {
					t.Fatalf("line %d still carries a CR: %q", i, l.Text)
				}
				if strings.HasPrefix(strings.TrimLeft(l.Text, " \t"), ">") {
					t.Fatalf("line %d still carries a quote marker: %q", i, l.Text)
				}
			}
		})
	}
}

// Recall floor: every attribution the independent census can see must be found.
// The census counts unwrapped attributions only, so this is a floor, not equality —
// the parser should find those *and* the wrapped ones the census cannot express.
func TestFindsAtLeastEveryAttributionInTheCensus(t *testing.T) {
	for _, f := range fixtures(t) {
		t.Run(f.Name, func(t *testing.T) {
			lines := Normalise(f.Body)
			found := 0
			for i := range lines {
				if _, ok := FindAttribution(lines, i); ok {
					found++
				}
			}
			floor := f.Stats.Attr
			if found < floor {
				t.Errorf("found %d attributions, census floor is %d", found, floor)
			}
			// And the wrapped ones should push it above the floor wherever the
			// census saw a stray `wrote:` line.
			if f.Stats.Wrapped > 0 && found <= floor {
				t.Errorf("found %d attributions with %d wrapped closers present: "+
					"the wrapped cases are not being recovered", found, f.Stats.Wrapped)
			}
		})
	}
}

func TestFindsEveryHeaderBlockRun(t *testing.T) {
	for _, f := range fixtures(t) {
		t.Run(f.Name, func(t *testing.T) {
			lines := Normalise(f.Body)
			var blocks, keys int
			for i := 0; i < len(lines); {
				if b, ok := FindHeaderBlock(lines, i); ok {
					blocks++
					// Count key lines, not the block's span: a block may now
					// legitimately cover a folded continuation line, which is
					// inside the block but is not itself a header key.
					for _, l := range strings.Split(b.Text, "\n") {
						if reHeaderKey.MatchString(unbold(strings.TrimSpace(l))) {
							keys++
						}
					}
					i = b.End
					continue
				}
				i++
			}
			// Every header-block line the census counted should land in some block,
			// except singletons, which are deliberately not blocks.
			if f.Stats.Hdr >= 4 && blocks == 0 {
				t.Errorf("census counted %d header lines but no block was found", f.Stats.Hdr)
			}
			if keys > f.Stats.Hdr {
				t.Errorf("claimed %d header lines, census counted only %d", keys, f.Stats.Hdr)
			}
		})
	}
}

// Sentinels found inside quoted regions are the whole point: on this corpus,
// markers are more often nested than top-level, and `Begin forwarded message:`
// is essentially never seen at depth 0.
func TestSentinelsAreFoundBelowDepthZero(t *testing.T) {
	nestedTotal := 0
	for _, f := range fixtures(t) {
		lines := Normalise(f.Body)
		for i := range lines {
			if b, ok := FindAttribution(lines, i); ok && b.Depth > 0 {
				nestedTotal++
			}
		}
	}
	if nestedTotal == 0 {
		t.Fatal("no attribution was found at depth > 0 across the whole corpus; " +
			"nested sentinels are the majority case here")
	}
	t.Logf("attributions found at depth > 0: %d", nestedTotal)
}

func TestParsingIsDeterministic(t *testing.T) {
	for _, f := range fixtures(t) {
		a, b := Normalise(f.Body), Normalise(f.Body)
		if len(a) != len(b) {
			t.Fatalf("%s: line count differs between runs", f.Name)
		}
		for i := range a {
			if a[i] != b[i] {
				t.Fatalf("%s: line %d differs between runs", f.Name, i)
			}
		}
	}
}
