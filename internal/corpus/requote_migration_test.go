package corpus

import (
	"path/filepath"
	"testing"
	"time"
)

func TestQuoteRepairMigrationRebuildsBothIndexesWithoutChangingSource(t *testing.T) {
	path := filepath.Join(t.TempDir(), "before26.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if s != nil {
			s.Close()
		}
	}()
	id, _, err := s.PutQuoted(Entry{
		Source: SourceMail, ExtID: "quote:migration-fixture", TS: time.Unix(1_700_000_000, 0),
		Subject: "Invented migration subject", BodyText: "inventedmigrationtoken belongs in both search indexes",
	})
	if err != nil {
		t.Fatal(err)
	}
	beforeBody, beforeSHA := bodyOf(t, s, id)
	// Deliberately remove index terms without removing their source row. This
	// models an inconsistent v25 external-content index, not production data.
	if _, err := s.DB().Exec(`insert into entries_fts(entries_fts,rowid,subject,body_text) values('delete',?,?,?)`,
		id, "Invented migration subject", beforeBody); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB().Exec(`insert into entries_ident(entries_ident,rowid,body_text) values('delete',?,?)`,
		id, beforeBody); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"entries_fts", "entries_ident"} {
		if _, err := s.DB().Exec("insert into " + table + "(" + table + ",rank) values('integrity-check',1)"); err == nil {
			t.Fatalf("fixture must have an inconsistent %s index", table)
		}
	}
	if _, err := s.DB().Exec(`delete from schema_version where v>=26`); err != nil {
		t.Fatal(err)
	}
	// Remove the later migration too, modelling a real contiguous v25 schema.
	for _, in := range []string{"entries", "mail_detail", "sightings", "attachments", "people", "identities", "participants", "domain_aliases"} {
		for _, op := range []string{"insert", "update", "delete"} {
			if _, err := s.DB().Exec("drop trigger reconcile_" + in + "_" + op); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := s.DB().Exec("drop table reconcile_state"); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	checkRepairIndexes(t, s)
	if body, sha := bodyOf(t, s, id); body != beforeBody || sha != beforeSHA {
		t.Fatal("index migration changed source content/hash")
	}
	for _, table := range []string{"entries_fts", "entries_ident"} {
		var n int
		if err := s.DB().QueryRow("select count(*) from "+table+" where rowid=? and "+table+" match 'inventedmigrationtoken'", id).Scan(&n); err != nil || n != 1 {
			t.Fatalf("%s did not recover the stored token: count=%d err=%v", table, n, err)
		}
	}
	var version int
	if err := s.DB().QueryRow(`select max(v) from schema_version`).Scan(&version); err != nil || version != len(migrations) {
		t.Fatalf("schema version=%d, err=%v", version, err)
	}
}
