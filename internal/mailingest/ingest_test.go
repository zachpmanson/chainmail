package mailingest

import (
	"testing"

	"github.com/zachpmanson/chainmail/internal/corpus"
)

// Invented mail only. Nothing from a real mailbox belongs in a committed test.
func msg(id, from, to, cc string) Message {
	m := Message{Body: "the body"}
	m.ID = id
	m.MessageID = "<" + id + "@example.com>"
	m.ThreadID = "t1"
	m.Date = "Mon, 02 Jan 2006 15:04:05 +1000"
	m.Subject = "Re: Fwd: the plan"
	m.From = from
	m.To = to
	m.Cc = cc
	return m
}

func store(t *testing.T) *corpus.Store {
	t.Helper()
	s, err := corpus.Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// The bug this fixes: only the From address became a person, so anyone who never
// sent anything did not exist. Here two of the three participants are silent.
func TestPutRecordsRecipientsNotJustTheSender(t *testing.T) {
	s := store(t)
	if _, err := Put(s, msg("m1", `Alice A <alice@example.com>`,
		`"Example, Bob" <bob@example.com>`, `carol@example.com`)); err != nil {
		t.Fatalf("Put: %v", err)
	}

	people, err := corpus.People(s)
	if err != nil {
		t.Fatal(err)
	}
	if len(people) != 3 {
		t.Fatalf("people: got %d, want 3: %+v", len(people), people)
	}
	byName := map[string]corpus.PersonSummary{}
	for _, p := range people {
		byName[p.DisplayName] = p
	}
	if p := byName["Example, Bob"]; p.Sent != 0 || p.Received != 1 {
		t.Fatalf("a quoted display name with a comma is one recipient: %+v", p)
	}
	if p := byName["Alice A"]; p.Sent != 1 {
		t.Fatalf("sender: %+v", p)
	}
}

// Re-ingest must be idempotent for participation too, and a changed recipient
// list must replace rather than accumulate.
func TestReIngestDoesNotAccumulateParticipants(t *testing.T) {
	s := store(t)
	first := msg("m1", `alice@example.com`, `bob@example.com, carol@example.com`, "")
	if _, err := Put(s, first); err != nil {
		t.Fatal(err)
	}
	if _, err := Put(s, first); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := s.DB().QueryRow(`select count(*) from participants`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("participants after re-ingest: got %d, want 3", n)
	}

	corrected := msg("m1", `alice@example.com`, `bob@example.com`, "")
	if _, err := Put(s, corrected); err != nil {
		t.Fatal(err)
	}
	if err := s.DB().QueryRow(`select count(*) from participants`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("participants after a corrected header: got %d, want 2", n)
	}
}

// A From header that will not parse must not fail the ingest: the body is still
// evidence, and an authorless entry is a truthful record of one.
func TestUnparseableFromStillStoresTheEntry(t *testing.T) {
	s := store(t)
	res, err := Put(s, msg("m1", `Mail Delivery Subsystem`, `bob@example.com`, ""))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if !res.Created {
		t.Fatal("entry not created")
	}
	// no address, so the sender is keyed on the display name instead
	if _, err := corpus.PersonByIdentity(s, corpus.KindDisplayName, "mail delivery subsystem"); err != nil {
		t.Fatalf("nameless sender lost: %v", err)
	}
}
