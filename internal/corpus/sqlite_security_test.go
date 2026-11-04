package corpus

import (
	"database/sql"
	"fmt"
	"slices"
	"testing"
)

// Only synthetic, bounded SQL belongs here; never replay hostile database bytes.
func sqliteSecurityDB(t *testing.T) *sql.DB {
	t.Helper()
	home := t.TempDir()
	for _, name := range []string{"HOME", "XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME"} {
		t.Setenv(name, home)
	}
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	var version string
	if err := db.QueryRow(`select sqlite_version()`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	// Gate these cases on the reviewed dependency, not the host sqlite3 CLI.
	if version != "3.54.1" {
		t.Fatalf("embedded SQLite = %q, want reviewed 3.54.1", version)
	}
	return db
}

func TestSQLiteSecurityFTS5PrefixNegativeRowids(t *testing.T) {
	for _, detail := range []string{"none", "column", "full"} {
		t.Run(detail, func(t *testing.T) {
			db := sqliteSecurityDB(t)
			if _, err := db.Exec(`create virtual table docs using fts5(body, detail=` + detail + `)`); err != nil {
				t.Fatal(err)
			}
			// Distinct terms sharing a prefix merge doclists, including negative
			// first rowids. No prefix index shortcuts that path (CVE-2026-119373).
			want := make([]int64, 0, 32)
			for i := range 32 {
				id := int64(i - 16)
				want = append(want, id)
				if _, err := db.Exec(`insert into docs(rowid, body) values (?, ?)`, id, fmt.Sprintf("prefix%c unrelated", 'a'+rune(i%8))); err != nil {
					t.Fatal(err)
				}
			}
			check := func() {
				t.Helper()
				rows, err := db.Query(`select rowid from docs where docs match ? order by rowid`, "prefix*")
				if err != nil {
					t.Fatal(err)
				}
				defer rows.Close()
				var got []int64
				for rows.Next() {
					var id int64
					if err := rows.Scan(&id); err != nil {
						t.Fatal(err)
					}
					got = append(got, id)
				}
				if err := rows.Err(); err != nil {
					t.Fatal(err)
				}
				if !slices.Equal(got, want) {
					t.Fatalf("prefix rowids = %v, want %v", got, want)
				}
			}
			check()
			if _, err := db.Exec(`insert into docs(docs) values ('optimize')`); err != nil {
				t.Fatal(err)
			}
			check()
			if _, err := db.Exec(`insert into docs(docs) values ('integrity-check')`); err != nil {
				t.Fatal(err)
			}
			var integrity string
			if err := db.QueryRow(`pragma integrity_check`).Scan(&integrity); err != nil {
				t.Fatal(err)
			}
			if integrity != "ok" {
				t.Fatalf("integrity_check = %q", integrity)
			}
		})
	}
}
