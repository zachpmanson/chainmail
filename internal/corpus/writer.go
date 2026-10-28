package corpus

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"time"
)

// Ordinary, contextless producers wait up to 30s per lock acquisition. This
// accommodates the measured ~10s initial fold publication, not arbitrarily
// long writers. Contention beyond this budget still returns SQLITE_BUSY.
const writerBusyMS = 30_000

// contextWriter pins a connection with SQLite's uninterruptible busy sleep
// disabled. Only BEGIN is retried, never a statement or transaction prefix.
// Once acquired, WAL has no competing writer; cancellation can interrupt work.
type contextWriter struct {
	*sql.Conn
	priorTimeout int
	active       bool
}

func (s *Store) beginWriter(ctx context.Context, budget time.Duration) (*contextWriter, error) {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	w := &contextWriter{Conn: conn}
	if err = conn.QueryRowContext(ctx, "pragma busy_timeout").Scan(&w.priorTimeout); err != nil {
		return nil, errors.Join(err, w.discard())
	}
	if _, err = conn.ExecContext(ctx, "pragma busy_timeout=0"); err != nil {
		return nil, errors.Join(err, w.discard())
	}
	started := time.Now()
	for {
		if err = ctx.Err(); err != nil {
			return nil, errors.Join(err, w.close())
		}
		_, err = conn.ExecContext(ctx, "begin immediate")
		if err == nil {
			w.active = true
			return w, nil
		}
		if !foldBusy(err) {
			// A cancelled driver call can leave acquisition's outcome uncertain.
			// Closing the physical connection rolls back before any pool reuse.
			return nil, errors.Join(err, w.discard())
		}
		if time.Since(started) >= budget {
			return nil, errors.Join(fmt.Errorf("writer busy after %s: %w", budget, err), w.close())
		}
		timer := time.NewTimer(min(10*time.Millisecond, budget-time.Since(started)))
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, errors.Join(ctx.Err(), w.close())
		case <-timer.C:
		}
	}
}

func (w *contextWriter) commit(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := w.ExecContext(ctx, "commit"); err != nil {
		return err
	}
	w.active = false
	return nil
}

func (w *contextWriter) close() error {
	// Cleanup must outlive the caller's cancelled context. Never pool a
	// connection with a transaction or a modified busy policy still attached.
	if w.active {
		if _, err := w.ExecContext(context.Background(), "rollback"); err != nil {
			return errors.Join(err, w.discard())
		}
		w.active = false
	}
	if _, err := w.ExecContext(context.Background(), fmt.Sprintf("pragma busy_timeout=%d", w.priorTimeout)); err != nil {
		return errors.Join(err, w.discard())
	}
	return w.Conn.Close()
}

func (w *contextWriter) discard() error {
	// database/sql removes a physical connection when Raw returns ErrBadConn.
	err := w.Raw(func(any) error { return driver.ErrBadConn })
	if errors.Is(err, driver.ErrBadConn) {
		return nil
	}
	return err
}
