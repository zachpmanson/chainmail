package corpus

import (
	"context"
	"path/filepath"
	"testing"
)

// Search scope and pooled connection integrity regressions.
func TestIntegrityAccountLabelIsolation(t *testing.T) {
	s := open(t)
	for _, a := range []GmailAccount{{ID: "work", DisplayName: "Work"}, {ID: "personal", DisplayName: "Personal"}} {
		if err := s.PutGmailAccount(a); err != nil {
			t.Fatal(err)
		}
	}
	r, err := s.PutForAccount("work", entry("mail:<shared@test.invalid>", "shared"), &Mail{GmailID: "w", Labels: []string{"INBOX"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.PutGmailCopy(GmailCopy{AccountID: "personal", GmailID: "p", EntryID: r.ID, Labels: []string{"TRASH", "UNREAD"}}); err != nil {
		t.Fatal(err)
	}
	hits, err := s.SearchChains(Query{AccountID: "work", Labels: []string{"TRASH"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		t.Fatalf("work/TRASH leaked another account's labels: %+v", hits)
	}
	for _, tc := range []struct {
		account, label string
		unread         int
	}{{"work", "INBOX", 0}, {"personal", "TRASH", 1}, {"", "TRASH", 1}} {
		hits, err := s.SearchChains(Query{AccountID: tc.account, Labels: []string{tc.label}})
		if err != nil {
			t.Fatal(err)
		}
		if len(hits) != 1 || hits[0].Unread != tc.unread {
			t.Fatalf("scope=%q label=%q: %+v", tc.account, tc.label, hits)
		}
	}
}

func TestIntegrityForeignKeysOnSecondConnection(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "corpus.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	r, err := s.Put(entry("mail:<valid@test.invalid>", "valid"), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := s.DB().Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	var first int
	if err := conn.QueryRowContext(context.Background(), "pragma foreign_keys").Scan(&first); err != nil {
		t.Fatal(err)
	}
	var second int
	if err := s.DB().QueryRow("pragma foreign_keys").Scan(&second); err != nil {
		t.Fatal(err)
	}
	err = s.PutGmailCopy(GmailCopy{AccountID: "missing-account", GmailID: "g", EntryID: r.ID})
	if err == nil {
		t.Fatal("second connection accepted a dangling account reference")
	}
	var violations int
	if err := s.DB().QueryRow("select count(*) from pragma_foreign_key_check").Scan(&violations); err != nil {
		t.Fatal(err)
	}
	t.Logf("first connection foreign_keys=%d, second=%d, invalid copy error=%v, FK violations=%d", first, second, err, violations)
	if first != 1 || second != 1 || violations != 0 {
		t.Fatal("unexpected probe result")
	}
}

func TestIntegrityLegacyLabelsAreFallbackNotAnOverride(t *testing.T) {
	s := open(t)
	r, err := s.Put(entry("mail:legacy-labels", "legacy labels"), &Mail{GmailID: "legacy-g", Labels: []string{"INBOX", "UNREAD"}}, nil)
	integrityMust(t, err)
	integrityMust(t, s.PutGmailCopy(GmailCopy{AccountID: "legacy", GmailID: "legacy-g", EntryID: r.ID, Labels: []string{"SAVED"}}))
	for _, scope := range []string{"", "legacy"} {
		hits, err := s.SearchChains(Query{AccountID: scope, Labels: []string{"INBOX"}})
		integrityMust(t, err)
		if len(hits) != 0 {
			t.Fatalf("stale mail_detail leaked INBOX in scope %q", scope)
		}
		hits, err = s.SearchChains(Query{AccountID: scope, Labels: []string{"SAVED"}})
		integrityMust(t, err)
		if len(hits) != 1 || hits[0].Unread != 0 {
			t.Fatalf("scope %q: %+v", scope, hits)
		}
	}
	// Migration 17 copies CSV verbatim; it must remain searchable until re-ingest.
	_, err = s.db.Exec(`update gmail_copies set labels='INBOX,UNREAD' where entry_id=?`, r.ID)
	integrityMust(t, err)
	hits, err := s.SearchChains(Query{AccountID: "legacy", Labels: []string{"INBOX"}})
	integrityMust(t, err)
	if len(hits) != 1 || hits[0].Unread != 1 {
		t.Fatalf("migrated CSV labels lost: %+v", hits)
	}
	_, err = s.db.Exec(`update gmail_copies set labels='123' where entry_id=?`, r.ID)
	integrityMust(t, err)
	hits, err = s.SearchChains(Query{AccountID: "legacy", Labels: []string{"123"}})
	integrityMust(t, err)
	if len(hits) != 1 {
		t.Fatalf("numeric CSV label lost: %+v", hits)
	}

}

func TestIntegrityForeignKeyDSNPreservesExistingParameters(t *testing.T) {
	path := "file:" + filepath.Join(t.TempDir(), "uri.db") + "?mode=rwc&_pragma=busy_timeout(1234)"
	s, err := Open(path)
	integrityMust(t, err)
	defer s.Close()
	conn, err := s.db.Conn(context.Background())
	integrityMust(t, err)
	defer conn.Close()
	var timeout, enabled int
	integrityMust(t, s.db.QueryRow(`pragma busy_timeout`).Scan(&timeout))
	integrityMust(t, s.db.QueryRow(`pragma foreign_keys`).Scan(&enabled))
	if timeout != 1234 || enabled != 1 {
		t.Fatalf("timeout=%d FK=%d", timeout, enabled)
	}
}
