package refresh

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/zachpmanson/chainmail/internal/spec"
)

// maxSpecVersion is the contract this package knows how to reproduce. A spec
// from a later version may record selection parameters that are not read here,
// and reproducing a page from a spec you only half understand is worse than
// refusing: the output would carry the newer version number while having
// silently dropped whatever it added.
const maxSpecVersion = 1

// scriptTag matches the JSON island every rendered page embeds. `mt-spec` is the
// id the Python renderer used; its pages are still worth refreshing.
var scriptTag = regexp.MustCompile(
	`(?s)<script type="application/json" id="(?:chainmail-spec|mt-spec)">(.*?)</script>`)

// Load reads a previous run from either a spec JSON or a page rendered from one.
//
// The page path is not a scrape. Every rendered page embeds the exact spec that
// produced it, so recovering it is one regexp and one unescape — which is why
// this is reachable from Go at all, rather than needing the TypeScript renderer's
// extractSpec. What is NOT ported is that function's legacy snake_case
// normalisation; see legacyKeys for how a pre-contract page is handled.
func Load(path string) (spec.Spec, error) {
	blob, err := os.ReadFile(path)
	if err != nil {
		return spec.Spec{}, err
	}
	if isHTML(path, blob) {
		m := scriptTag.FindSubmatch(blob)
		if m == nil {
			return spec.Spec{}, fmt.Errorf("%s: no embedded spec in that page — it was not "+
				"rendered by chainmail, so there is nothing to refresh", path)
		}
		// The renderer escapes `</` on the way in so the island cannot close its
		// own tag early.
		blob = []byte(strings.ReplaceAll(string(m[1]), `<\/`, `</`))
	}
	var sp spec.Spec
	if err := json.Unmarshal(blob, &sp); err != nil {
		return spec.Spec{}, fmt.Errorf("%s: %w", path, err)
	}
	if err := checkVersion(path, blob, sp); err != nil {
		return spec.Spec{}, err
	}
	if len(sp.Messages) == 0 {
		return spec.Spec{}, fmt.Errorf("%s: the spec has no messages, so there is no "+
			"previous run to refresh", path)
	}
	return sp, nil
}

// checkVersion refuses what it cannot reproduce, and says which of the two ways
// it cannot: a version from the future, or a spec old enough to predate the
// camelCase contract, whose runLabel and queries would decode to nothing and
// leave the refresh silently unbounded and query-less.
func checkVersion(path string, blob []byte, sp spec.Spec) error {
	v := sp.SpecVersion
	if v == 0 {
		v = 1 // the schema's stated default; absent means 1.
	}
	if v > maxSpecVersion {
		return fmt.Errorf("%s: specVersion %d, but this build reproduces up to %d — "+
			"refreshing it would drop whatever the newer version records", path, v, maxSpecVersion)
	}
	if found := legacyKeys(blob); len(found) > 0 {
		return fmt.Errorf("%s: pre-contract spec (%s) — re-render it with `render` first, "+
			"which normalises those keys, then refresh the page it writes",
			path, strings.Join(found, ", "))
	}
	return nil
}

// legacyKeys names the snake_case spellings that carry something refresh needs.
// The TypeScript normaliser translates all of them; rather than port it for one
// caller, a spec written that way is sent back through the renderer, which
// already does the translation and writes a page this can read.
func legacyKeys(blob []byte) []string {
	var found []string
	for _, k := range []string{"run_label", "source_notes", "open_items"} {
		if strings.Contains(string(blob), `"`+k+`"`) {
			found = append(found, k)
		}
	}
	return found
}

func isHTML(path string, blob []byte) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".html", ".htm":
		return true
	case ".json":
		return false
	}
	// No usable extension: decide on the bytes, since a spec is JSON and starts
	// with a brace once whitespace is gone.
	return !strings.HasPrefix(strings.TrimSpace(string(blob)), "{")
}
