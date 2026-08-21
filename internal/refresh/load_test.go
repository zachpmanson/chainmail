package refresh

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, name, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

const oneEntry = `{"specVersion":1,"title":"Fernlea site access",` +
	`"runLabel":"2 Feb 2026","messages":[{"date":"2 Feb 2026","body":"the survey"}]}`

func TestLoadReadsASpec(t *testing.T) {
	sp, err := Load(write(t, "spec.json", oneEntry))
	if err != nil {
		t.Fatal(err)
	}
	if sp.Title != "Fernlea site access" || len(sp.Messages) != 1 {
		t.Fatalf("spec = %+v", sp)
	}
}

// A rendered page is a valid input because it embeds the exact spec that made
// it. The escaped `</` is what the renderer writes so the island cannot close its
// own script tag early, and a loader that did not undo it would corrupt any body
// containing a closing tag.
func TestLoadRecoversASpecFromARenderedPage(t *testing.T) {
	page := `<!doctype html><body><h1>Fernlea site access</h1>` +
		`<script type="application/json" id="chainmail-spec">` +
		strings.Replace(oneEntry, `"the survey"`, `"the survey<\/p>"`, 1) +
		`</script></body>`
	sp, err := Load(write(t, "page.html", page))
	if err != nil {
		t.Fatal(err)
	}
	if len(sp.Messages) != 1 || sp.Messages[0].Body != "the survey</p>" {
		t.Fatalf("messages = %+v", sp.Messages)
	}
	if sp.RunLabel != "2 Feb 2026" {
		t.Errorf("runLabel = %q; without it the fetch has no window to narrow to", sp.RunLabel)
	}
}

func TestLoadRejectsAPageWithNoEmbeddedSpec(t *testing.T) {
	_, err := Load(write(t, "page.html", "<html><body>a page from somewhere else</body></html>"))
	if err == nil || !strings.Contains(err.Error(), "no embedded spec") {
		t.Fatalf("err = %v", err)
	}
}

// A version from the future is a refusal with a reason, never a panic and never
// a refresh that drops whatever the newer contract records.
func TestLoadRefusesANewerSpecVersion(t *testing.T) {
	_, err := Load(write(t, "spec.json",
		`{"specVersion":9,"title":"t","messages":[{"date":"2 Feb 2026","body":"x"}]}`))
	if err == nil {
		t.Fatal("a specVersion this build does not know must not be refreshed")
	}
	for _, want := range []string{"specVersion 9", "up to 1"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not say %q", err, want)
		}
	}
}

// An absent version is version 1, which is what the schema says, so a spec
// written before the field existed still refreshes.
func TestLoadTreatsAnAbsentVersionAsOne(t *testing.T) {
	if _, err := Load(write(t, "spec.json",
		`{"title":"t","messages":[{"date":"2 Feb 2026","body":"x"}]}`)); err != nil {
		t.Fatal(err)
	}
}

// The pre-contract snake_case spelling is named and redirected rather than half
// understood: decoding it here would silently lose runLabel and leave the fetch
// unbounded.
func TestLoadRedirectsAPreContractSpec(t *testing.T) {
	_, err := Load(write(t, "spec.json",
		`{"title":"t","run_label":"2 Feb 2026","messages":[{"date":"2 Feb 2026","body":"x"}]}`))
	if err == nil || !strings.Contains(err.Error(), "run_label") {
		t.Fatalf("err = %v", err)
	}
}

func TestLoadRejectsAnEmptySpec(t *testing.T) {
	_, err := Load(write(t, "spec.json", `{"title":"t","messages":[]}`))
	if err == nil || !strings.Contains(err.Error(), "no messages") {
		t.Fatalf("err = %v", err)
	}
}
