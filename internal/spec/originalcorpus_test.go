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
	mismatches, falsePositives := 0, 0
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
			if got.DefaultsToOriginal != (fixture.Expected == "original") {
				mismatches++
				if got.DefaultsToOriginal {
					falsePositives++
				}
				t.Errorf("expected %s; got %+v", fixture.Expected, got)
			}
		})
	}
	t.Logf("%d fixtures: %d matches, %d false positives, %d false negatives", len(fixtures), len(fixtures)-mismatches, falsePositives, mismatches-falsePositives)
}
