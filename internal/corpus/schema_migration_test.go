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

func TestUpgradeFrom20RepairsPromotionAndDirectSightings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "corpus.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`create table schema_version(v integer primary key)`); err != nil {
		t.Fatal(err)
	}
	for i, m := range migrations[:20] {
		if _, err := db.Exec(m); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`insert into schema_version values (?)`, i+1); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`
 insert into entries(id,source,ext_id,kind,ts,body_text,body_sha,quoted,ingested_at)
 values (1,'mail','mail:promote','message',1,'authoritative','hash',1,1),
        (2,'mail','quote:remain','message',1,'quote only','hash',1,1),
        (3,'mail','mail:old-detail','message',1,'legacy body','hash',1,1);
 insert into gmail_copies(account_id,gmail_id,entry_id,labels) values ('legacy','g',1,'[]');
 insert into mail_detail(entry_id,gmail_id) values (3,'old-g');
 insert into sightings(entry_id,seen_in,kind,detail) values
 (1,NULL,'mailbox','first'),(1,NULL,'mailbox','latest'),(1,NULL,'other','other'),(2,1,'quoted','nested');
 `); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for id, want := range map[int]int{1: 0, 2: 1, 3: 0} {
		var quoted int
		if err := s.db.QueryRow(`select quoted from entries where id=?`, id).Scan(&quoted); err != nil {
			t.Fatal(err)
		}
		if quoted != want {
			t.Fatalf("entry %d quoted=%d want %d", id, quoted, want)
		}
	}
	var detail string
	if err := s.db.QueryRow(`select detail from sightings where entry_id=1 and seen_in is null and kind='mailbox'`).Scan(&detail); err != nil {
		t.Fatal(err)
	}
	if detail != "latest" {
		t.Fatalf("detail=%q", detail)
	}
	for range 3 {
		if err := s.Sight(1, 0, "mailbox", "updated"); err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err := s.db.QueryRow(`select count(*) from sightings`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Fatalf("sightings=%d want three distinct facts", count)
	}
	if _, err := s.db.Exec(`insert into sightings(entry_id,kind) values (1,'mailbox')`); err == nil {
		t.Fatal("direct sighting uniqueness not enforced")
	}
	if err := s.migrate(); err != nil {
		t.Fatal(err)
	}
}

func TestMigration24SeedsOnlyUninitializedIndex(t *testing.T) {
	path := filepath.Join(t.TempDir(), "migration24.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`create table schema_version(v integer primary key)`); err != nil {
		t.Fatal(err)
	}
	for i, m := range migrations[:23] {
		if _, err = db.Exec(m); err != nil {
			t.Fatal(err)
		}
		if _, err = db.Exec(`insert into schema_version values(?)`, i+1); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = db.Exec(`insert into entries(source,ext_id,ts,ingested_at,body_sha,body_text) values('mail','synthetic',0,0,'test','Opening
Regards,
Synthetic')`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var ready, members, source, queued int
	if err = s.db.QueryRow(`select ready,(select count(*) from fold_members),(select count(*) from entries),(select count(*) from fold_dirty_entries) from fold_state`).Scan(&ready, &members, &source, &queued); err != nil {
		t.Fatal(err)
	}
	if ready != 0 || members != 0 || source != 1 || queued != 0 {
		t.Fatalf("migration backfilled or lost data: %d %d %d %d", ready, members, source, queued)
	}
	first, err := s.WarmFolds()
	if err != nil {
		t.Fatal(err)
	}
	if !first.Rebuilt || first.Bodies != 1 {
		t.Fatalf("lazy initialization: %+v", first)
	}
}
