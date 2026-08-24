package corpus

import (
	"path/filepath"
	"strings"
	"testing"
)

// The search shadow tables are external-content fts5 over entries. A manual
// wipe of them can leave the index out of step with the entries table (and on
// a WAL-backed store with a re-insert, snippet() then throws "database disk
// image is malformed (11)" — the live incident, chainmail#43). ReindexFTS is
// the sanctioned wipe path: it clears and rebuilds both shadow tables through
// the virtual table's own commands. The contract this locks in: after the
// rebuild the index is exactly the entries table again, MATCH finds the text,
// and snippet() produces marked output.
func TestReindexFTSRebuildsTheSearchIndexFromEntries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "corpus.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	put(t, s, msg{id: "csv-1", subject: "Hewson CSV", body: "the CSV is in red and blue"})
	put(t, s, msg{id: "csv-2", subject: "Ruralco pilot", body: "the billing sheet"})

	// Run the rebuild on a healthy store: it must leave the index equal to the
	// entries table (it rebuilds from entries every time), which is exactly
	// what makes it the recovery path for a desynced or malformed index.
	if err := s.ReindexFTS(); err != nil {
		t.Fatalf("reindex: %v", err)
	}
	var idx, ents int
	if err := s.db.QueryRow(`select count(*) from entries_fts`).Scan(&idx); err != nil {
		t.Fatal(err)
	}
	if err := s.db.QueryRow(`select count(*) from entries`).Scan(&ents); err != nil {
		t.Fatal(err)
	}
	if idx != ents {
		t.Fatalf("after reindex, entries_fts = %d, want %d (the entries table)", idx, ents)
	}
	var got string
	if err := s.db.QueryRow(`select body_text from entries_fts where entries_fts match 'csv'`).Scan(&got); err != nil {
		t.Fatalf("search after reindex: %v", err)
	}
	if !strings.Contains(got, "red") {
		t.Fatalf("reindexed body = %q, want it to contain the text again", got)
	}
	// snippet() — the query the live search uses — produces marked output.
	var rendered string
	if err := s.db.QueryRow(`select snippet(entries_fts, -1, '<mark>', '</mark>', '…', 12)
		from entries_fts where entries_fts match 'csv'`).Scan(&rendered); err != nil {
		t.Fatalf("snippet() after reindex: %v", err)
	}
	if !strings.Contains(rendered, "<mark>") {
		t.Fatalf("snippet = %q, want a marked token after the rebuild", rendered)
	}
}
