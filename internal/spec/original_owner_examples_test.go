package spec

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Owner-confirmed desired modes, not snapshots of the current false negatives.
func TestOriginalAnalysisOwnerExamples(t *testing.T) {
	for _, name := range []string{"example-1", "example-2", "example-3"} {
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join("../../testdata/original-owner-examples", name+".html"))
			if err != nil {
				t.Fatal(err)
			}
			input := string(raw)
			got := AnalyseOriginal(input)
			t.Logf("receipt: %+v", got)
			wantScore := map[string]int{"example-1": 4, "example-2": 3, "example-3": 2}[name]
			if !got.DefaultsToOriginal || got.Uncertain || got.Score != wantScore || !hasOriginalRule(got, "whole message in a table") {
				t.Errorf("owner expects original rendering with score %d; got %+v", wantScore, got)
			}
			if name == "example-3" && (hasOriginalRule(got, "designed action") || hasOriginalRule(got, "prominent graphic") || hasOriginalRule(got, "visual hierarchy or modules")) {
				t.Fatalf("synthetic case needs only layout and table containment: %+v", got)
			}
			renamed := regexp.MustCompile(`\bc\d+\b`).ReplaceAllString(input, "opaque-renamed")
			renamed = regexp.MustCompile(`\bid\d+\b`).ReplaceAllString(renamed, "opaque-id")
			renamed = strings.NewReplacer("example.invalid", "renamed.test", "example.test", "renamed.test").Replace(renamed)
			for i, mutation := range []string{
				renamed,
				`<div><section>` + input + `</section></div>`,
				input + `<div class="gmail_quote">` + designedOriginal + `</div><div class="gmail_signature">` + designedOriginal + `</div>`,
			} {
				before, _ := json.Marshal(got)
				after, _ := json.Marshal(AnalyseOriginal(mutation))
				if string(before) != string(after) {
					t.Errorf("mutation %d changed receipt: %s / %s", i, before, after)
				}
			}
		})
	}
}
