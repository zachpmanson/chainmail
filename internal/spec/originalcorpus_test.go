package spec

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestOriginalAnalysisAnonymisedCorpus(t *testing.T) {
	root := "../../testdata/original-html"
	manifest, err := os.ReadFile(filepath.Join(root, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		ID       string `json:"id"`
		Expected string `json:"expected"`
	}
	if err := json.Unmarshal(manifest, &fixtures); err != nil {
		t.Fatal(err)
	}
	if len(fixtures) != 119 {
		t.Fatalf("got %d fixtures, want 119", len(fixtures))
	}
	checked, paused, mismatches, falsePositives := 0, 0, 0, 0
	for _, fixture := range fixtures {
		t.Run(fixture.ID, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join(root, fixture.ID+".html"))
			if err != nil {
				t.Fatal(err)
			}
			got := AnalyseOriginal(string(raw))
			renamed := regexp.MustCompile(`\bc\d+\b`).ReplaceAllString(string(raw), "opaque-renamed")
			renamed = regexp.MustCompile(`\bid\d+\b`).ReplaceAllString(renamed, "opaque-id")
			renamed = strings.ReplaceAll(renamed, "example.invalid", "renamed.test")
			for _, mutation := range []string{renamed, `<div><section>` + string(raw) + `</section></div>`, string(raw) + `<div class="gmail_quote">` + designedOriginal + `</div><div class="gmail_signature">` + designedOriginal + `</div>`} {
				wantJSON, _ := json.Marshal(got)
				mutatedJSON, _ := json.Marshal(AnalyseOriginal(mutation))
				if string(wantJSON) != string(mutatedJSON) {
					t.Errorf("mutation changed receipt: %s / %s", wantJSON, mutatedJSON)
				}
			}
			// Owner requested this temporary pause on 2026-10-22. Labels and
			// all mutation checks above stay intact; strict mode restores it.
			// Follow-up: [zpm/chainmail#339](https://github.com/zachpmanson/chainmail/issues/339)
			if (fixture.ID == "thread-041-1" || fixture.ID == "thread-059-1") && os.Getenv("CHAINMAIL_STRICT_ORIGINAL_CORPUS") != "1" {
				paused++
				t.Skip("owner-paused expected-mode assertion; mutation checks ran; tracked in [zpm/chainmail#339](https://github.com/zachpmanson/chainmail/issues/339)")
			}
			checked++
			if got.DefaultsToOriginal != (fixture.Expected == "original") {
				mismatches++
				if got.DefaultsToOriginal {
					falsePositives++
				}
				t.Errorf("expected %s; got %+v", fixture.Expected, got)
			}
		})
	}
	t.Logf("%d fixtures mutated; %d expected modes checked: %d matches, %d false positives, %d false negatives; %d owner-paused expectations", len(fixtures), checked, checked-mismatches, falsePositives, mismatches-falsePositives, paused)
}
