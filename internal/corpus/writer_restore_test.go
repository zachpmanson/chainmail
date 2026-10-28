package corpus

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"modernc.org/sqlite"
)

// Exercise restoration failure against real SQLite while injecting only the
// cleanup PRAGMA failure. Count physical opens/closes, not pooled handles.
type restoreFailureDriver struct {
	sqlite.Driver
	failRestore   atomic.Bool
	opens, closes atomic.Int32
}

type restoreFailureConn struct {
	driver.Conn
	owner *restoreFailureDriver
}

var errRestoreTimeout = errors.New("injected busy timeout restoration failure")

func (d *restoreFailureDriver) Open(dsn string) (driver.Conn, error) {
	c, err := d.Driver.Open(dsn)
	if err != nil {
		return nil, err
	}
	d.opens.Add(1)
	return &restoreFailureConn{Conn: c, owner: d}, nil
}

func (c *restoreFailureConn) Close() error {
	c.owner.closes.Add(1)
	return c.Conn.Close()
}

func (c *restoreFailureConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if c.owner.failRestore.Load() && query == "pragma busy_timeout=1234" {
		return nil, errRestoreTimeout
	}
	return c.Conn.(driver.ExecerContext).ExecContext(ctx, query, args)
}

func (c *restoreFailureConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	return c.Conn.(driver.QueryerContext).QueryContext(ctx, query, args)
}

func TestWriterDiscardsConnectionWhenTimeoutRestorationFails(t *testing.T) {
	d := &restoreFailureDriver{}
	name := fmt.Sprintf("chainmail-test-restore-%d", writerGateID.Add(1))
	sql.Register(name, d)
	db, err := sql.Open(name, filepath.Join(t.TempDir(), "restore.db")+fmt.Sprintf("?_pragma=foreign_keys(1)&_pragma=busy_timeout(%d)", writerBusyMS))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	foldExec(t, s, "create table work(value integer)")
	foldExec(t, s, "pragma busy_timeout=1234")
	w, err := s.beginWriter(context.Background(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.ExecContext(context.Background(), "insert into work values(1)"); err != nil {
		t.Fatal(err)
	}
	if err := w.commit(context.Background()); err != nil {
		t.Fatal(err)
	}
	d.failRestore.Store(true)
	if err := w.close(); !errors.Is(err, errRestoreTimeout) {
		t.Fatalf("restoration failure was hidden: %v", err)
	}
	if d.closes.Load() != 1 {
		t.Fatalf("bad physical connection was not closed: %d", d.closes.Load())
	}
	d.failRestore.Store(false)
	checkConnectionPolicy(t, db, writerBusyMS)
	if d.opens.Load() != 2 {
		t.Fatalf("pool reused bad connection: %d opens", d.opens.Load())
	}
	foldExec(t, s, "insert into work values(2)")
	var total int
	if err := db.QueryRow("select sum(value) from work").Scan(&total); err != nil || total != 3 {
		t.Fatalf("committed data or subsequent writes lost: %d %v", total, err)
	}
}
