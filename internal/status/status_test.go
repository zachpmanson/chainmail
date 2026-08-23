package status

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func byID(services []Service) map[string]Service {
	m := map[string]Service{}
	for _, s := range services {
		m[s.ID] = s
	}
	return m
}

// A snapshot that cannot parse is the unchecked state, not an error: the server
// has to show something reasonable on a machine whose snapshot file was torn by
// a crash mid-write.
func TestParseTurnsGarbageIntoUnchecked(t *testing.T) {
	snap := Parse([]byte("not json"))
	if snap.CheckedAt != "" {
		t.Errorf("checkedAt = %q, want empty", snap.CheckedAt)
	}
	if len(snap.Services) != len(Known) {
		t.Errorf("%d services, want the %d known backends", len(snap.Services), len(Known))
	}
	for _, s := range snap.Services {
		if s.Status != Unchecked {
			t.Errorf("%s = %q, want unchecked", s.ID, s.Status)
		}
	}
}

// A snapshot that names a backend this build does not know must drop it, and a
// known one it omits must read as unchecked: the grid is closed on both sides,
// so a newer or older snapshot can never show a service the screen cannot name.
func TestParseFillsKnownBackendsThatWereOmitted(t *testing.T) {
	blob, _ := json.Marshal(map[string]any{
		"checkedAt": "2026-08-22T15:04:00Z",
		"services": []any{
			map[string]any{"id": "mail", "label": "Gmail (docket)", "status": OK, "detail": "fine"},
			map[string]any{"id": "widget", "label": "Unknown", "status": OK},
		},
	})
	snap := Parse(blob)
	if snap.CheckedAt != "2026-08-22T15:04:00Z" {
		t.Errorf("checkedAt = %q", snap.CheckedAt)
	}
	m := byID(snap.Services)
	if len(m) != len(Known) {
		t.Errorf("%d services, want the %d known backends", len(m), len(Known))
	}
	var sawUnknown bool
	for _, s := range snap.Services {
		if s.ID == "widget" {
			sawUnknown = true
		}
	}
	if sawUnknown {
		t.Error("an unknown backend is preserved, not dropped")
	}
	if m["mail"].Status != OK || m["mail"].Detail != "fine" {
		t.Errorf("mail = %+v", m["mail"])
	}
	if m["embed"].Status != Unchecked {
		t.Errorf("embed = %+v, want unchecked (it was omitted)", m["embed"])
	}
}

// Marshal writes the known order and omits nothing a probe ran for.
func TestMarshalRoundTripsThroughParse(t *testing.T) {
	in := Snapshot{CheckedAt: "2026-08-22T15:04:00Z", Services: []Service{
		{ID: "mail", Label: "Gmail (docket)", Status: OK},
		{ID: "embed", Label: "Embedding daemon (ollama)", Status: Down, Detail: "start it"},
	}}
	back := Parse(in.Marshal())
	if back.CheckedAt != in.CheckedAt {
		t.Errorf("checkedAt = %q, want %q", back.CheckedAt, in.CheckedAt)
	}
	if len(back.Services) != len(Known) {
		t.Fatalf("%d services after round-trip, want %d", len(back.Services), len(Known))
	}
	m := byID(back.Services)
	if m["mail"].Status != OK || m["embed"].Detail != "start it" {
		t.Errorf("round-trip = %+v", m)
	}
}

// FileName places the snapshot beside the corpus, under the state dir. That is
// the one place the CLI writes it and the server reads it back from.
func TestFileNameSitsBesideTheCorpus(t *testing.T) {
	if got := FileName("/var/lib/chainmail/corpus.db"); got != "/var/lib/chainmail/status.json" {
		t.Errorf("FileName = %q", got)
	}
	// And it is actually writable and readable where it claims.
	dir, err := os.MkdirTemp("", "chainmail-status-")
	if err != nil {
		t.Fatal(err)
	}
	p := FileName(filepath.Join(dir, "corpus.db"))
	if err := os.WriteFile(p, []byte(`{"services":[]}`), 0o600); err != nil {
		t.Fatalf("writing status.json: %v", err)
	}
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("reading status.json: %v", err)
	}
	if !bytes.Equal(data, []byte(`{"services":[]}`)) {
		t.Errorf("status.json changed in round-trip: %v", data)
	}
}
