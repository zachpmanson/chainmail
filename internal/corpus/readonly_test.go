package corpus

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func freezeQuoteFixture(t *testing.T, path string) {
	t.Helper()
	if err := os.Chmod(path, 0400); err != nil {
		t.Fatal(err)
	}
}

func assertNoSQLiteCompanions(t *testing.T, path string) {
	t.Helper()
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		if _, err := os.Lstat(path + suffix); !os.IsNotExist(err) {
			t.Fatalf("unexpected companion %s: %v", suffix, err)
		}
	}
}

func TestOpenReadOnlyFrozenWALSnapshotNoSidecarsWhileOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "frozen.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	seedLegacyLocale(t, s)
	schema := querySnapshot(t, s, `select type,name,sql from sqlite_schema order by type,name`)
	protected := protectedLocaleSnapshot(t, s)
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	freezeQuoteFixture(t, path)
	assertNoSQLiteCompanions(t, path)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	ro, err := OpenReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	defer ro.Close()
	requireLocaleChange(t, localePlan(t, ro))
	assertNoSQLiteCompanions(t, path)
	if schema != querySnapshot(t, ro, `select type,name,sql from sqlite_schema order by type,name`) || !reflect.DeepEqual(protected, protectedLocaleSnapshot(t, ro)) {
		t.Fatal("preview changed schema/data")
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("preview changed main file", err)
	}
	if _, err = ro.DB().Exec(`update entries set ts=0`); err == nil {
		t.Fatal("query-only reader accepted write")
	}
}

func TestOpenReadOnlyRefusesWritableCompanionsAndSymlinks(t *testing.T) {
	for _, mode := range []string{"writable", "wal", "shm", "journal", "symlink", "directory", "active_wal"} {
		t.Run(mode, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "snapshot.db")
			s, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			seedLegacyLocale(t, s)
			if mode == "active_wal" {
				defer s.Close()
				// Committed data remains in WAL while the writer stays open. Never read
				// just the main file with immutable=1 in this state.
				var n int
				if err = s.DB().QueryRow(`select count(*) from entries`).Scan(&n); err != nil || n == 0 {
					t.Fatal(n, err)
				}
				if _, err = os.Stat(path + "-wal"); err != nil {
					t.Fatal(err)
				}
			} else if err = s.Close(); err != nil {
				t.Fatal(err)
			}
			if mode != "writable" {
				freezeQuoteFixture(t, path)
			}
			switch mode {
			case "wal", "shm", "journal":
				if err = os.WriteFile(path+"-"+mode, nil, 0600); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				link := path + ".link"
				if err = os.Symlink(path, link); err != nil {
					t.Fatal(err)
				}
				path = link
			case "directory":
				path = filepath.Dir(path)
			}
			before, err := os.Lstat(path)
			if err != nil {
				t.Fatal(err)
			}
			ro, err := OpenReadOnly(path)
			if err == nil {
				ro.Close()
				t.Fatal("unsafe snapshot accepted")
			}
			after, err := os.Lstat(path)
			if err != nil || before.Mode() != after.Mode() {
				t.Fatal("opener mutated permissions", err)
			}
		})
	}
}
