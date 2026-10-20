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

func TestMissingMessageIDIsScopedToGmailAccount(t *testing.T) {
	s := store(t)
	for _, id := range []string{"work", "personal"} {
		if err := s.PutGmailAccount(corpus.GmailAccount{ID: id, DisplayName: id}); err != nil {
			t.Fatal(err)
		}
	}
	m := msg("same-local-id", "alice@example.test", "bob@example.test", "")
	m.MessageID = ""
	m.Body = "work private text"
	work, err := PutForAccount(s, "work", m)
	if err != nil {
		t.Fatal(err)
	}
	m.From = "carol@example.test"
	m.Body = "unrelated personal text"
	personal, err := PutForAccount(s, "personal", m)
	if err != nil {
		t.Fatal(err)
	}
	if work.ID == personal.ID || !work.Created || !personal.Created {
		t.Fatalf("unrelated account-local messages merged: work=%+v personal=%+v", work, personal)
	}
	for account, want := range map[string]string{
		"work": "work private text", "personal": "unrelated personal text",
	} {
		var body string
		if err := s.DB().QueryRow(`select e.body_text from entries e
			join gmail_copies c on c.entry_id=e.id where c.account_id=? and c.gmail_id=?`,
			account, m.ID).Scan(&body); err != nil {
			t.Fatal(err)
		}
		if body != want {
			t.Errorf("%s body = %q, want %q", account, body, want)
		}
	}
	for _, tc := range []struct {
		account, from, body string
		id                  int64
	}{
		{"work", "alice@example.test", "work private text", work.ID},
		{"personal", "carol@example.test", "unrelated personal text", personal.ID},
	} {
		m.From, m.Body = tc.from, tc.body
		again, err := PutForAccount(s, tc.account, m)
		if err != nil {
			t.Fatal(err)
		}
		if again.ID != tc.id || again.Created || again.Changed {
			t.Fatalf("identical re-ingest changed %s: %+v", tc.account, again)
		}
	}
	var n int
	if err := s.DB().QueryRow(`select count(*) from entries where source='mail'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("logical entries = %d, want 2", n)
	}
}

func TestMissingMessageIDPreservesLegacyKey(t *testing.T) {
	s := store(t)
	m := msg("legacy-local-id", "alice@example.test", "bob@example.test", "")
	m.MessageID = ""
	// Seed the pre-account fallback key, as an existing corpus would have it.
	old, err := s.Put(corpus.Entry{Source: corpus.SourceMail, ExtID: "mail:gmail:" + m.ID}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, put := range []func(*corpus.Store, Message) (corpus.PutResult, error){
		Put,
		func(s *corpus.Store, m Message) (corpus.PutResult, error) {
			return PutForAccount(s, "legacy", m)
		},
	} {
		got, err := put(s, m)
		if err != nil {
			t.Fatal(err)
		}
		if got.ID != old.ID || got.Created {
			t.Fatalf("legacy re-ingest lost its existing key: old=%+v got=%+v", old, got)
		}
	}
}

func TestMessageIDStillDeduplicatesAcrossGmailAccounts(t *testing.T) {
	s := store(t)
	var id int64
	for _, account := range []string{"work", "personal"} {
		if err := s.PutGmailAccount(corpus.GmailAccount{ID: account, DisplayName: account}); err != nil {
			t.Fatal(err)
		}
		m := msg(account+"-local-id", "alice@example.test", "bob@example.test", "")
		m.MessageID = "<shared@example.test>"
		got, err := PutForAccount(s, account, m)
		if err != nil {
			t.Fatal(err)
		}
		if id != 0 && (got.ID != id || got.Created) {
			t.Fatalf("real Message-ID did not deduplicate: id=%d got=%+v", id, got)
		}
		id = got.ID
	}
}
