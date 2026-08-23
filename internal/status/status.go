// Package status owns the persisted connection snapshot: which of the pieces
// chainmail reads through is logged in as of the last probe. The browser-
// facing server is read-only on purpose, so it cannot ask docket or slackdump
// whether their sessions are still good — that is the operator's job, and the
// CLI's probe writes the answer here for the server to serve.
package status

import (
	"encoding/json"
	"path/filepath"
)

// The states a service is in. Four, and no more: the screen's whole job is a
// glance at which backends are usable, and the detail string under each badge
// says what would make a not-ok one ok.
const (
	OK        = "ok"
	Needs     = "needs-auth"
	Down      = "down"
	Unchecked = "unchecked"
)

// Service is one backend's connection at the last probe.
type Service struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

// Snapshot is what a probe writes and the server serves, in one shape.
//
// CheckedAt is a UTC RFC3339 stamp, the same convention the wire uses, so the
// file needs no date parsing and the client can show it verbatim. Omitted on
// the synthetic empty snapshot, which the screen reads as "never probed".
type Snapshot struct {
	CheckedAt string    `json:"checkedAt,omitempty"`
	Services  []Service `json:"services"`
}

// Known lists every backend, in the order the screen shows them. A probe
// reports all of them; the server falls back to all of them when it has no
// file. Anything outside this list is dropped, so an older snapshot can never
// smuggle a service onto a newer screen and vice versa.
var Known = []Service{
	{ID: "mail", Label: "Gmail (docket)", Status: Unchecked},
	{ID: "slack", Label: "Slack (slackdump)", Status: Unchecked},
	{ID: "embed", Label: "Embedding daemon (ollama)", Status: Unchecked},
}

// Empty is the snapshot shown before any probe has run: every backend known,
// each answered "unchecked" rather than absent. A missing snapshot is not an
// error and must not 404 the page; it is the honest empty state.
func Empty() Snapshot { return Snapshot{Services: Known} }

// Parse decodes a snapshot file into the closed set of known backends. A
// known backend with no row reads as unchecked, an unknown one is dropped, and
// a file that does not parse yields the unchecked snapshot rather than an
// error the screen would have to read as "cannot tell".
func Parse(blob []byte) Snapshot {
	var raw Snapshot
	if err := json.Unmarshal(blob, &raw); err != nil {
		return Empty()
	}
	byID := map[string]Service{}
	for _, s := range raw.Services {
		byID[s.ID] = s
	}
	out := Snapshot{CheckedAt: raw.CheckedAt}
	for _, k := range Known {
		if s, ok := byID[k.ID]; ok {
			out.Services = append(out.Services, s)
		} else {
			out.Services = append(out.Services, k)
		}
	}
	return out
}

// Marshal renders a snapshot for the file. Services are written in Known
// order so the file reads the way the screen shows it; a service the snapshot
// carries outside Known is dropped rather than persisted.
func (s Snapshot) Marshal() []byte {
	byID := map[string]Service{}
	for _, svc := range s.Services {
		byID[svc.ID] = svc
	}
	var ordered Snapshot
	if s.CheckedAt != "" {
		ordered.CheckedAt = s.CheckedAt
	}
	for _, k := range Known {
		if v, ok := byID[k.ID]; ok {
			ordered.Services = append(ordered.Services, v)
		} else {
			ordered.Services = append(ordered.Services, k)
		}
	}
	blob, err := json.MarshalIndent(ordered, "", " ")
	if err != nil {
		return []byte("{}\n")
	}
	return append(blob, '\n')
}

// FileName is the snapshot's path: a sibling of the corpus, so pointing both
// binaries at the same corpus points them at the same snapshot. The corpus
// directory is where the server already keeps saved specs.
func FileName(corpusPath string) string {
	return filepath.Join(filepath.Dir(corpusPath), "status.json")
}
