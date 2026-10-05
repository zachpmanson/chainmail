package mailingest

import (
	"testing"

	"github.com/zachpmanson/chainmail/internal/corpus"
)

func TestIngestProgressIsIndependentPerGmailAccount(t *testing.T) {
	s := store(t)
	for _, account := range []corpus.GmailAccount{
		{ID: "work", DisplayName: "Work"},
		{ID: "personal", DisplayName: "Personal"},
	} {
		if err := s.PutGmailAccount(account); err != nil {
			t.Fatal(err)
		}
	}
	for _, accountID := range []string{"work", "personal"} {
		f := newFake(2, 4)
		got, err := IngestForAccount(s, f, "in:anywhere", Bound{PageSize: 4}, accountID)
		if err != nil {
			t.Fatalf("ingest %s: %v", accountID, err)
		}
		if got.Seen == 0 || !got.Stop.Covered() {
			t.Fatalf("account %s reused another account's cursor: %+v", accountID, got)
		}
	}
	for _, accountID := range []string{"work", "personal"} {
		cursor, err := corpus.LoadGmailCursor(s, accountID, "in:anywhere")
		if err != nil {
			t.Fatal(err)
		}
		if !cursor.Exists || !cursor.Complete {
			t.Errorf("account %s cursor = %+v, want independent completed coverage", accountID, cursor)
		}
	}
	entries, err := corpus.Cursors(s)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[0].Source != corpus.SourceMail || entries[1].Source != corpus.SourceMail {
		t.Fatalf("cursor listing = %+v, want both mailbox cursors", entries)
	}
}
