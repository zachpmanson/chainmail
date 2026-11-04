package corpus

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func sqliteSecurityContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func sqliteSecurityIntegrity(t *testing.T, db *sql.DB) {
	t.Helper()
	ctx := sqliteSecurityContext(t)
	var integrity string
	if err := db.QueryRowContext(ctx, `pragma integrity_check`).Scan(&integrity); err != nil || integrity != "ok" {
		t.Fatalf("integrity_check = %q, err=%v", integrity, err)
	}
	var violations int
	if err := db.QueryRowContext(ctx, `select count(*) from pragma_foreign_key_check`).Scan(&violations); err != nil || violations != 0 {
		t.Fatalf("foreign_key_check = %d violations, err=%v", violations, err)
	}
}

func TestSQLiteSecurityFTS5MutationCompatibility(t *testing.T) {
	for _, tokenizer := range []string{"porter unicode61", "trigram"} {
		t.Run(tokenizer, func(t *testing.T) {
			db := sqliteSecurityDB(t)
			ctx := sqliteSecurityContext(t)
			if _, err := db.ExecContext(ctx, `create virtual table docs using fts5(body, tokenize='`+tokenizer+`');
				insert into docs(rowid,body) values(1,'Confirming invoice ZZQ-4417'),(2,'Confirming invoice ZZQ-5528')`); err != nil {
				t.Fatal(err)
			}
			term, replacement := "confirm", "rescheduling"
			if tokenizer == "trigram" {
				term, replacement = "Q-4417", "Q-6639"
			}
			check := func(query string, want []int64) {
				t.Helper()
				rows, err := db.QueryContext(ctx, `select rowid from docs where docs match ? order by rowid`, query)
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
					t.Fatalf("MATCH %q = %v, want %v", query, got, want)
				}
			}
			want := []int64{1, 2}
			if tokenizer == "trigram" {
				want = []int64{1}
			}
			check(quoteFTS(term), want)
			if _, err := db.ExecContext(ctx, `update docs set body='Rescheduled invoice ZZQ-6639' where rowid=1; delete from docs where rowid=2`); err != nil {
				t.Fatal(err)
			}
			for _, phase := range []string{"mutated", "rebuilt"} {
				if phase == "rebuilt" {
					if _, err := db.ExecContext(ctx, `insert into docs(docs) values('rebuild')`); err != nil {
						t.Fatal(err)
					}
				}
				check(quoteFTS(term), nil)
				if tokenizer == "trigram" {
					check(quoteFTS("Q-5528"), nil)
				}
				check(quoteFTS(replacement), []int64{1})
				if _, err := db.ExecContext(ctx, `insert into docs(docs) values('integrity-check')`); err != nil {
					t.Fatal(err)
				}
			}
			sqliteSecurityIntegrity(t, db)
		})
	}
}

func TestSQLiteSecurityPreparedRebind(t *testing.T) {
	db := sqliteSecurityDB(t)
	ctx := sqliteSecurityContext(t)
	if _, err := db.ExecContext(ctx, `create table binding_keys(id integer primary key, value);
		create index binding_value on binding_keys(value);
		create table binding_rank(key_id integer references binding_keys(id), position integer);
		insert into binding_keys values(1,0),(2,1),(3,NULL),(4,'alpha'),(5,x'0001ff');
		insert into binding_rank select id,1 from binding_keys;
		insert into binding_rank select id,2 from binding_keys`); err != nil {
		t.Fatal(err)
	}
	// Reuse one statement while changing both storage class and sort direction.
	// This checks results, not whether SQLite internally replans the statement.
	stmt, err := db.PrepareContext(ctx, `select k.id,r.position,typeof(k.value)
		from binding_keys k join binding_rank r on r.key_id=k.id where k.value is ?1
		order by case when ?2=0 then r.position end asc,
		case when ?2=1 then r.position end desc, k.id`)
	if err != nil {
		t.Fatal(err)
	}
	defer stmt.Close()
	for round := range 3 {
		for _, tc := range []struct {
			value any
			id    int
			kind  string
		}{{nil, 3, "null"}, {int64(1), 2, "integer"}, {[]byte{0, 1, 255}, 5, "blob"}, {int64(0), 1, "integer"}, {"alpha", 4, "text"}} {
			for direction := range 2 {
				rows, err := stmt.QueryContext(ctx, tc.value, direction)
				if err != nil {
					t.Fatal(err)
				}
				var got []string
				for rows.Next() {
					var id, position int
					var kind string
					if err := rows.Scan(&id, &position, &kind); err != nil {
						rows.Close()
						t.Fatal(err)
					}
					got = append(got, fmt.Sprintf("%d/%d/%s", id, position, kind))
				}
				if err := rows.Err(); err != nil {
					rows.Close()
					t.Fatal(err)
				}
				if err := rows.Close(); err != nil {
					t.Fatal(err)
				}
				want := []string{fmt.Sprintf("%d/1/%s", tc.id, tc.kind), fmt.Sprintf("%d/2/%s", tc.id, tc.kind)}
				if direction == 1 {
					slices.Reverse(want)
				}
				if !slices.Equal(got, want) {
					t.Fatalf("round=%d bind=%T(%v) direction=%d: got %v want %v", round, tc.value, tc.value, direction, got, want)
				}
			}
		}
	}
}

func TestSQLiteSecurityPreCancelledContexts(t *testing.T) {
	db := sqliteSecurityDB(t)
	ctx := sqliteSecurityContext(t)
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	stmt, err := conn.PrepareContext(cancelled, `select ?`)
	if stmt != nil {
		stmt.Close()
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("PrepareContext: %v", err)
	}
	tx, err := conn.BeginTx(cancelled, nil)
	if tx != nil {
		tx.Rollback()
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("BeginTx: %v", err)
	}
	var n int
	if err := conn.QueryRowContext(cancelled, `select 1`).Scan(&n); !errors.Is(err, context.Canceled) {
		t.Fatalf("QueryRowContext: %v", err)
	}
	if err := conn.QueryRowContext(ctx, `select 42`).Scan(&n); err != nil || n != 42 {
		t.Fatalf("connection after cancellation: %d, %v", n, err)
	}
}

func TestSQLiteSecurityBusyCancellationAndRollback(t *testing.T) {
	sqliteSecurityDB(t)
	ctx := sqliteSecurityContext(t)
	s, err := Open(filepath.Join(t.TempDir(), "busy.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	// Pin both physical connections before acquiring the writer lock: no pool
	// wait or scheduling sleep can masquerade as SQLite lock cancellation.
	holder, err := s.db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Close()
	waiter, err := s.db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer waiter.Close()
	if _, err := waiter.ExecContext(ctx, `pragma busy_timeout=15000`); err != nil {
		t.Fatal(err)
	}
	tx, err := holder.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `insert into people(display_name) values('Rolled back fixture')`); err != nil {
		t.Fatal(err)
	}
	deadline, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	started := time.Now()
	_, err = waiter.ExecContext(deadline, `insert into people(display_name) values('Cancelled fixture')`)
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(started) > 10*time.Second {
		t.Fatalf("busy cancellation: %v after %s", err, time.Since(started))
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := waiter.QueryRowContext(ctx, `select count(*) from people`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("cancelled/rolled-back writes survived: count=%d err=%v", count, err)
	}
	if _, err := waiter.ExecContext(ctx, `insert into people(display_name) values('Subsequent fixture')`); err != nil {
		t.Fatalf("write after rollback/cancellation: %v", err)
	}
	if err := holder.QueryRowContext(ctx, `select count(*) from people`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("connection after rollback: count=%d err=%v", count, err)
	}
}

func TestSQLiteSecurityWALReopenAndReadOnly(t *testing.T) {
	sqliteSecurityDB(t)
	ctx := sqliteSecurityContext(t)
	path := filepath.Join(t.TempDir(), "wal.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.Put(entry("mail:wal@example.invalid", "Invented checkpoint fixture"), nil, nil); err != nil {
		t.Fatal(err)
	}
	sqliteSecurityIntegrity(t, s.db)
	var version int
	if err := s.db.QueryRowContext(ctx, `select max(v) from schema_version`).Scan(&version); err != nil || version != len(migrations) {
		t.Fatalf("fresh schema version=%d err=%v", version, err)
	}
	// A normal mode=ro connection keeps the WAL open across Store.Close/Open.
	// The app uses no custom read-only VFS.
	ro, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer ro.Close()
	ro.SetMaxOpenConns(1)
	var body string
	if err := ro.QueryRowContext(ctx, `select body_text from entries`).Scan(&body); err != nil || body != "Invented checkpoint fixture" {
		t.Fatalf("read-only WAL read: %q err=%v", body, err)
	}
	_, err = ro.ExecContext(ctx, `insert into people(display_name) values('Forbidden fixture')`)
	var code interface{ Code() int }
	if !errors.As(err, &code) || code.Code()&0xff != 8 {
		t.Fatalf("mode=ro write: %v, want SQLITE_READONLY", err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var mode string
	if err := s.db.QueryRowContext(ctx, `pragma journal_mode`).Scan(&mode); err != nil || mode != "wal" {
		t.Fatalf("reopened journal mode=%q err=%v", mode, err)
	}
	var busy, log, checkpointed int
	if err := s.db.QueryRowContext(ctx, `pragma wal_checkpoint(truncate)`).Scan(&busy, &log, &checkpointed); err != nil || busy != 0 || log != 0 || checkpointed != 0 {
		t.Fatalf("checkpoint: busy=%d log=%d checkpointed=%d err=%v", busy, log, checkpointed, err)
	}
	if err := ro.QueryRowContext(ctx, `select body_text from entries`).Scan(&body); err != nil || body != "Invented checkpoint fixture" {
		t.Fatalf("read-only after checkpoint: %q err=%v", body, err)
	}
	sqliteSecurityIntegrity(t, s.db)
}
