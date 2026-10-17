package corpus

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestUpgradeFromMigration19PreservesMailboxLabels(t *testing.T) {
	path := filepath.Join(t.TempDir(), "corpus.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`create table schema_version(v integer primary key)`); err != nil {
		t.Fatal(err)
	}
	for i, migration := range migrations[:19] {
		if _, err := db.Exec(migration); err != nil {
			t.Fatalf("apply old migration %d: %v", i+1, err)
		}
		if _, err := db.Exec(`insert into schema_version(v) values (?)`, i+1); err != nil {
			t.Fatalf("record old migration %d: %v", i+1, err)
		}
	}
	if _, err := db.Exec(`insert into gmail_account_labels(account_id, name) values ('legacy', 'INBOX')`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := Open(path)
	if err != nil {
		t.Fatalf("upgrade from schema version 19: %v", err)
	}
	defer store.Close()

	var count int
	if err := store.DB().QueryRow(`select count(*) from gmail_account_labels where account_id='legacy' and name='INBOX'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("preserved mailbox labels = %d, want 1", count)
	}
	var column string
	rows, err := store.DB().Query(`pragma table_info(people)`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid, notnull, pk int
		var name, typ string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &typ, &notnull, &defaultValue, &pk); err != nil {
			t.Fatal(err)
		}
		if name == "prefer_original_set" {
			column = name
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if column == "" {
		t.Fatal("migration 20 did not add people.prefer_original_set")
	}
}
