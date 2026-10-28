package corpus

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zachpmanson/chainmail/internal/boiler"
	"github.com/zachpmanson/chainmail/internal/embed"
	"modernc.org/sqlite"
)

var writerGates sync.Map
var writerGateID atomic.Int64
var registerWriterGate sync.Once

type writerGate struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
	onEnter func()
}

// A TEMP trigger gates the actual fold publisher on its physical connection,
// after it owns the SQLite writer. No private corpus or guessed start timing.
func gateTableWriter(t *testing.T, s *Store, table string, onEnter func()) *writerGate {
	t.Helper()
	registerWriterGate.Do(func() {
		sqlite.MustRegisterScalarFunction("chainmail_test_writer_gate", 1, func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
			v, ok := writerGates.Load(args[0].(int64))
			if !ok {
				return nil, errors.New("unknown test gate")
			}
			g := v.(*writerGate)
			g.once.Do(func() {
				close(g.entered)
				if g.onEnter != nil {
					g.onEnter()
				}
			})
			select {
			case <-g.release:
				return int64(1), nil
			case <-time.After(10 * time.Second):
				return nil, errors.New("test gate was not released")
			}
		})
	})
	// Registration affects newly opened connections only.
	s.db.SetMaxIdleConns(0)
	conn, err := s.db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	s.db.SetMaxIdleConns(1)
	s.db.SetMaxOpenConns(1)
	g := &writerGate{entered: make(chan struct{}), release: make(chan struct{}), onEnter: onEnter}
	id := writerGateID.Add(1)
	writerGates.Store(id, g)
	_, err = conn.ExecContext(context.Background(), fmt.Sprintf(`create temp trigger gate_test_writer after insert on %s
 begin select chainmail_test_writer_gate(%d); end`, table, id))
	conn.Close()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { writerGates.Delete(id) })
	return g
}

func awaitWriter(t *testing.T, g *writerGate) {
	t.Helper()
	select {
	case <-g.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("fold writer never reached publication gate")
	}
}

func awaitResult(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("writer did not return within test budget")
		return nil
	}
}

func TestFoldWriterWaitsForActualProducers(t *testing.T) {
	for _, producer := range []string{"embedding", "mail", "slack", "unread", "quoted"} {
		t.Run(producer, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "writers.db")
			s := fileFoldStore(t, path)
			other := fileFoldStore(t, path)
			other.db.SetMaxOpenConns(1)
			body := "An invented message with enough words to embed.\nRegards,\nSynthetic"
			e := Entry{Source: SourceMail, ExtID: "synthetic@example.test", BodyText: body, TS: time.Unix(1700000000, 0)}
			m := &Mail{GmailID: "invented-gmail-id", From: "writer@example.test", Labels: []string{"INBOX", "UNREAD"}}
			res, err := s.PutForAccount("legacy", e, m, nil)
			if err != nil {
				t.Fatal(err)
			}
			if producer == "quoted" {
				foldExec(t, s, "update entries set quoted=1 where id=?", res.ID)
			}
			g := gateTableWriter(t, s, "fold_members", nil)
			var release sync.Once
			unlock := func() { release.Do(func() { close(g.release) }) }
			defer unlock()
			refresh := make(chan error, 1)
			go func() { _, err := s.RefreshFoldIndex(context.Background(), boiler.Default()); refresh <- err }()
			awaitWriter(t, g)
			done := make(chan error, 1)
			fake := embed.NewFake(8)
			go func() {
				var err error
				switch producer {
				case "embedding":
					var rep BackfillReport
					rep, err = other.BackfillEmbeddings(context.Background(), fake, BackfillOptions{})
					if err == nil && rep.Embedded != 1 {
						err = fmt.Errorf("embedded %d, want 1", rep.Embedded)
					}
				case "mail":
					e.BodyText = body + "\nMail update."
					_, err = other.PutForAccount("legacy", e, m, nil)
				case "slack":
					_, err = other.PutSlack(Entry{ExtID: "synthetic-slack", BodyText: body, TS: e.TS}, Slack{ChannelID: "C_TEST", TS: "1700000000.000001"}, nil)
				case "unread":
					_, err = other.ReconcileGmailUnread("legacy", nil)
				case "quoted":
					err = other.EnrichQuoted(res.ID, Entry{BodyText: body + "\nLonger quoted evidence."})
				}
				done <- err
			}()
			select {
			case err := <-done:
				t.Fatalf("producer returned before writer release: %v", err)
			case <-time.After(100 * time.Millisecond):
			}
			unlock()
			if err := awaitResult(t, refresh); err != nil {
				t.Fatal(err)
			}
			if err := awaitResult(t, done); err != nil {
				t.Fatal(err)
			}
			switch producer {
			case "embedding":
				v, err := other.Embedding(res.ID, fake.Model())
				text, _ := EmbedTextFor(SourceMail, "", body)
				want, _ := embed.Vectors(context.Background(), fake, embed.Document, []string{text})
				if err != nil || !reflect.DeepEqual(v, want[0]) {
					t.Fatalf("stored vector mismatch: %v", err)
				}
			case "mail", "quoted":
				var got string
				if err := other.db.QueryRow("select body_text from entries where id=?", res.ID).Scan(&got); err != nil || len(got) <= len(body) {
					t.Fatalf("source update missing: %v", err)
				}
			case "slack":
				var got string
				if err := other.db.QueryRow("select body_text from entries where source='slack'").Scan(&got); err != nil || got != body {
					t.Fatalf("Slack update missing: %v", err)
				}
			case "unread":
				copies, err := other.GmailCopies(res.ID)
				if err != nil || len(copies) != 1 || Unread(copies[0].Labels) {
					t.Fatalf("unread reconciliation missing: %v", err)
				}
			}
			foldParity(t, other, boiler.Default())
		})
	}
}

func TestDeferredSnapshotCannotUpgradeAfterFoldPublication(t *testing.T) {
	path := filepath.Join(t.TempDir(), "snapshot.db")
	s := fileFoldStore(t, path)
	other := fileFoldStore(t, path)
	say(t, s, 0, "snapshot@example.test", "snapshot", "Invented content\nRegards,\nSynthetic")
	// ReadOnly bypasses _txlock=immediate in modernc. It is not enforcement:
	// deliberately attempting a write below reproduces the stale snapshot.
	tx, err := s.db.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var generation int
	if err := tx.QueryRow("select generation from fold_state").Scan(&generation); err != nil {
		t.Fatal(err)
	}
	if _, err := other.WarmFolds(); err != nil {
		t.Fatal(err)
	}
	_, err = tx.Exec("update entries set subject='obsolete snapshot'")
	var code interface{ Code() int }
	if !errors.As(err, &code) || code.Code() != 517 {
		t.Fatalf("got %v, want SQLITE_BUSY_SNAPSHOT(517)", err)
	}
	// Retrying the statement in the same transaction cannot repair its snapshot.
	_, err = tx.Exec("update entries set subject='still obsolete'")
	if !errors.As(err, &code) || code.Code() != 517 {
		t.Fatalf("retry got %v, want SQLITE_BUSY_SNAPSHOT(517)", err)
	}
}

func TestContextWritersCancelWithoutLateWrites(t *testing.T) {
	for _, operation := range []string{"refresh", "embedding"} {
		t.Run(operation, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "cancel.db")
			s := fileFoldStore(t, path)
			s.db.SetMaxOpenConns(1)
			other := fileFoldStore(t, path)
			say(t, s, 0, "cancel@example.test", "cancel", "An invented message with enough words to embed.")
			before := foldPublishedSnapshot(t, s)
			blocker, err := other.db.Begin()
			if err != nil {
				t.Fatal(err)
			}
			defer blocker.Rollback()
			ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
			defer cancel()
			started := time.Now()
			if operation == "refresh" {
				_, err = s.RefreshFoldIndex(ctx, boiler.Default())
			} else {
				var rep BackfillReport
				rep, err = s.BackfillEmbeddings(ctx, embed.NewFake(8), BackfillOptions{})
				if rep.Embedded != 0 || rep.Skipped != 0 {
					t.Fatal("cancelled backfill reported committed work")
				}
			}
			if !errors.Is(err, context.DeadlineExceeded) || time.Since(started) > 500*time.Millisecond {
				t.Fatalf("cancellation was not prompt: %v (%s)", err, time.Since(started))
			}
			if err := blocker.Rollback(); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, foldPublishedSnapshot(t, s)) {
				t.Fatal("cancelled operation changed fold publication or dirty work")
			}
			var vectors int
			if err := s.db.QueryRow("select count(*) from entry_embeddings").Scan(&vectors); err != nil || vectors != 0 {
				t.Fatalf("cancelled vectors appeared: %d %v", vectors, err)
			}
			checkConnectionPolicy(t, s.db, writerBusyMS)
			backfill(t, s, embed.NewFake(8), BackfillOptions{})
			foldParity(t, s, boiler.Default())
		})
	}
}

func checkConnectionPolicy(t *testing.T, q foldQuery, timeout int) {
	t.Helper()
	var busy, fk int
	if err := q.QueryRowContext(context.Background(), "pragma busy_timeout").Scan(&busy); err != nil {
		t.Fatal(err)
	}
	if err := q.QueryRowContext(context.Background(), "pragma foreign_keys").Scan(&fk); err != nil {
		t.Fatal(err)
	}
	if busy != timeout || fk != 1 {
		t.Fatalf("connection policy: busy=%d FK=%d, want %d/1", busy, fk, timeout)
	}
}

func TestEmbeddingCancellationAfterInsertRollsBack(t *testing.T) {
	s := fileFoldStore(t, filepath.Join(t.TempDir(), "inflight-embed.db"))
	say(t, s, 0, "inflight@example.test", "inflight", "An invented message with enough words to embed.")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	g := gateTableWriter(t, s, "entry_embeddings", cancel)
	close(g.release)
	rep, err := s.BackfillEmbeddings(ctx, embed.NewFake(8), BackfillOptions{})
	if err == nil || rep.Embedded != 0 || rep.Skipped != 0 {
		t.Fatalf("cancelled batch reported success: %+v %v", rep, err)
	}
	select {
	case <-g.entered:
	default:
		t.Fatal("embedding never reached insert trigger")
	}
	var n int
	if err := s.db.QueryRow("select count(*) from entry_embeddings").Scan(&n); err != nil || n != 0 {
		t.Fatalf("cancelled insert survived rollback: %d %v", n, err)
	}
	checkConnectionPolicy(t, s.db, writerBusyMS)
	backfill(t, s, embed.NewFake(8), BackfillOptions{})
}

func TestWriterAcquisitionBudgetAndImmediateBegin(t *testing.T) {
	path := filepath.Join(t.TempDir(), "budget.db")
	s := fileFoldStore(t, path)
	s.db.SetMaxOpenConns(1)
	other := fileFoldStore(t, path)
	// No reads or writes have happened inside this ordinary transaction yet.
	tx, err := other.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	started := time.Now()
	_, err = s.beginWriter(context.Background(), 40*time.Millisecond)
	if !foldBusy(err) || time.Since(started) > 500*time.Millisecond {
		t.Fatalf("BEGIN did not reserve writer ownership or budget was ignored: %v", err)
	}
	checkConnectionPolicy(t, s.db, writerBusyMS)
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	w, err := s.beginWriter(context.Background(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.close(); err != nil {
		t.Fatal(err)
	}
}

func TestWriterPoolPolicyRestorationAndRollback(t *testing.T) {
	s := fileFoldStore(t, filepath.Join(t.TempDir(), "pool.db"))
	var conns []*sql.Conn
	for i := 0; i < 4; i++ {
		c, err := s.db.Conn(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		conns = append(conns, c)
		checkConnectionPolicy(t, c, writerBusyMS)
	}
	for _, c := range conns {
		c.Close()
	}
	s.db.SetMaxOpenConns(1)
	foldExec(t, s, "pragma busy_timeout=1234")
	w, err := s.beginWriter(context.Background(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	checkConnectionPolicy(t, w, 0)
	if _, err := w.ExecContext(context.Background(), "insert into domain_aliases(from_domain,to_domain,added_at) values('rolled.example','back.example',0)"); err != nil {
		t.Fatal(err)
	}
	if err := w.close(); err != nil {
		t.Fatal(err)
	}
	checkConnectionPolicy(t, s.db, 1234)
	var n int
	if err := s.db.QueryRow("select count(*) from domain_aliases").Scan(&n); err != nil || n != 0 {
		t.Fatalf("rollback failed: %d %v", n, err)
	}
	w, err = s.beginWriter(context.Background(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.ExecContext(context.Background(), "insert into domain_aliases(from_domain,to_domain,added_at) values('commit.example','ok.example',0)"); err != nil {
		t.Fatal(err)
	}
	if err := w.commit(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := w.close(); err != nil {
		t.Fatal(err)
	}
	checkConnectionPolicy(t, s.db, 1234)
	if err := s.db.QueryRow("select count(*) from domain_aliases").Scan(&n); err != nil || n != 1 {
		t.Fatalf("commit failed: %d %v", n, err)
	}
	// An unexpected rollback failure must discard, not pool the zero timeout.
	w, err = s.beginWriter(context.Background(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.ExecContext(context.Background(), "rollback"); err != nil {
		t.Fatal(err)
	}
	if err := w.close(); err == nil {
		t.Fatal("cleanup hid rollback failure")
	}
	checkConnectionPolicy(t, s.db, writerBusyMS)
	foldExec(t, s, "update domain_aliases set to_domain='reused.example'")
}
