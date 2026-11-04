package corpus

import (
	"bufio"
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zachpmanson/chainmail/internal/boiler"
)

func fileFoldStore(t *testing.T, path string) *Store {
	t.Helper()
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestFoldWALSnapshotAndExternalSourceWriter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wal.db")
	s := fileFoldStore(t, path)
	other := fileFoldStore(t, path)
	p := sender(t, s, "one@wal.example", "Synthetic")
	var ids []int64
	for i := 0; i < 3; i++ {
		ids = append(ids, say(t, s, p, "one@wal.example", fmt.Sprint("mail:", i), "Opening\nRegards,\nSynthetic"))
	}
	foldParity(t, s, boiler.Default())
	err := s.WithFoldSnapshot(context.Background(), ids, boiler.Default(), func(tx *sql.Tx, folds map[int64]boiler.Fold) error {
		if len(folds) != 3 {
			return fmt.Errorf("expected initial folds")
		}
		// A WAL writer must complete while this reader holds the old generation.
		if _, err := other.db.Exec(`update entries set body_text='New unique body' where id=?`, ids[0]); err != nil {
			return err
		}
		var text string
		if err := tx.QueryRow(`select body_text from entries where id=?`, ids[0]).Scan(&text); err != nil {
			return err
		}
		if text != "Opening\nRegards,\nSynthetic" {
			return fmt.Errorf("snapshot changed underneath fold")
		}
		again, err := selectedFoldRows(context.Background(), tx, ids, boiler.Default())
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(folds, again) {
			return fmt.Errorf("counts changed inside snapshot")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	foldParity(t, s, boiler.Default())
	got, err := other.SelectedFolds(context.Background(), ids, boiler.Default())
	if err != nil || len(got) != 0 {
		t.Fatalf("external changes not visible: %v %v", got, err)
	}
	// Multiple refreshers must recheck after acquiring the same database lock.
	foldExec(t, other, `update entries set body_text='Opening\nRegards,\nSynthetic'`)
	done := make(chan error, 2)
	for _, store := range []*Store{s, other} {
		go func(store *Store) { _, err := store.WarmFolds(); done <- err }(store)
	}
	for i := 0; i < 2; i++ {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	foldParity(t, s, boiler.Default())
}

func TestFoldCancellationAndWriterDeadline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cancel.db")
	s := fileFoldStore(t, path)
	other := fileFoldStore(t, path)
	p := sender(t, s, "a@cancel.example", "Synthetic")
	say(t, s, p, "a@cancel.example", "mail:cancel", "Opening\nRegards,\nSynthetic")
	foldParity(t, s, boiler.Default())
	foldExec(t, s, `update entries set body_text='Changed\nRegards,\nSynthetic'`)
	conn, err := other.db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err = conn.ExecContext(context.Background(), `begin immediate`); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	started := time.Now()
	if _, err = s.RefreshFoldIndex(ctx, boiler.Default()); err == nil {
		t.Fatal("lock wait ignored cancellation")
	}
	if time.Since(started) > time.Second {
		t.Fatal("cancellation did not bound lock wait")
	}
	_, _ = conn.ExecContext(context.Background(), `rollback`)
	a, _, _ := foldPending(t, s)
	if a == 0 {
		t.Fatal("cancelled refresh consumed queue")
	}
	// A context cancelled before acquisition must not consume work either.
	ctx, cancel2 := context.WithCancel(context.Background())
	cancel2()
	if _, err = s.RefreshFoldIndex(ctx, boiler.Default()); err == nil {
		t.Fatal("cancelled refresh succeeded")
	}
	foldParity(t, s, boiler.Default())
}

func TestFoldSubprocess(t *testing.T) {
	path := os.Getenv("CHAINMAIL_FOLD_TEST_CHILD")
	if path == "" {
		return
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	switch os.Getenv("CHAINMAIL_FOLD_TEST_ACTION") {
	case "source":
		if _, err = s.db.Exec(`update entries set body_text='Externally written\nNew\nTail'`); err != nil {
			t.Fatal(err)
		}
	case "refresh":
		if _, err = s.WarmFolds(); err != nil {
			t.Fatal(err)
		}
	case "uncommitted":
		c, err := s.db.Conn(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		if _, err = c.ExecContext(context.Background(), `begin immediate;
   delete from fold_counts;delete from fold_sender_refs;delete from fold_members;
   delete from fold_dirty_entries; update fold_state set generation=generation+1;`); err != nil {
			t.Fatal(err)
		}
		fmt.Println("uncommitted-publication")
		// Killed by the parent with an atomic publication still uncommitted.
		time.Sleep(time.Minute)
	}
}
func foldChild(path, action string) *exec.Cmd {
	cmd := exec.Command(os.Args[0], "-test.run=^TestFoldSubprocess$", "-test.v")
	cmd.Env = append(os.Environ(), "CHAINMAIL_FOLD_TEST_CHILD="+path, "CHAINMAIL_FOLD_TEST_ACTION="+action)
	return cmd
}
func TestFoldSubprocessSourceRefreshAndKillRecovery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "subprocess.db")
	s := fileFoldStore(t, path)
	p := sender(t, s, "one@subprocess.example", "Synthetic")
	for i := 0; i < 3; i++ {
		say(t, s, p, "one@subprocess.example", fmt.Sprint("mail:", i), "Opening\nRegards,\nSynthetic")
	}
	foldParity(t, s, boiler.Default())
	for _, action := range []string{"source", "refresh"} {
		if out, err := foldChild(path, action).CombinedOutput(); err != nil {
			t.Fatalf("child %s: %v %s", action, err, out)
		}
	}
	foldParity(t, s, boiler.Default())
	foldExec(t, s, `update entries set body_text='Queued before crash\nRegards,\nSynthetic'`)
	var generation int64
	if err := s.db.QueryRow(`select generation from fold_state`).Scan(&generation); err != nil {
		t.Fatal(err)
	}
	cmd := foldChild(path, "uncommitted")
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	killed := false
	defer func() {
		if !killed {
			cmd.Process.Kill()
			cmd.Wait()
		}
	}()
	ready := make(chan bool, 1)
	go func() {
		scanner := bufio.NewScanner(out)
		for scanner.Scan() {
			if scanner.Text() == "uncommitted-publication" {
				ready <- true
				return
			}
		}
		ready <- false
	}()
	select {
	case ok := <-ready:
		if !ok {
			t.Fatal("child failed before publication")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("child timed out")
	}
	if err = cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	killed = true
	reopened := fileFoldStore(t, path)
	var got int64
	var pending int
	if err = reopened.db.QueryRow(`select generation,(select count(*) from fold_dirty_entries) from fold_state`).Scan(&got, &pending); err != nil {
		t.Fatal(err)
	}
	if got != generation || pending != 3 {
		t.Fatal("killed transaction published or lost dirty work")
	}
	foldParity(t, reopened, boiler.Default())
}

func foldPublishedSnapshot(t *testing.T, s *Store) []string {
	t.Helper()
	tx, err := s.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var out []string
	for _, table := range []string{"fold_members", "fold_tails", "fold_counts", "fold_sender_refs", "fold_state", "fold_dirty_entries", "fold_dirty_people"} {
		rows, err := tx.Query("select * from " + table + " order by rowid")
		if err != nil {
			t.Fatal(err)
		}
		cols, err := rows.Columns()
		if err != nil {
			rows.Close()
			t.Fatal(err)
		}
		for rows.Next() {
			values := make([]any, len(cols))
			dest := make([]any, len(cols))
			for i := range dest {
				dest[i] = &values[i]
			}
			if err = rows.Scan(dest...); err != nil {
				rows.Close()
				t.Fatal(err)
			}
			// Copy byte buffers before the next driver step. Never log these values.
			for i, v := range values {
				if b, ok := v.([]byte); ok {
					values[i] = string(b)
				}
			}
			out = append(out, table+fmt.Sprint(values))
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	return out
}

func TestFoldCancellationAfterWriterAcquisitionRollsBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "inflight-cancel.db")
	s := fileFoldStore(t, path)
	observer := fileFoldStore(t, path)
	p := sender(t, s, "one@inflight.example", "Synthetic")
	id := say(t, s, p, "one@inflight.example", "mail:inflight", "Opening\nRegards,\nSynthetic")
	foldParity(t, s, boiler.Default())
	// Enough reducible input to observe active writer ownership, not a timer
	// guessing whether refresh has started. No source writer runs during polling.
	foldExec(t, s, `update entries set body_text=? where id=?`, strings.Repeat("Synthetic normalization line <https://example.test>\n", 30000), id)
	before := foldPublishedSnapshot(t, s)
	conn, err := observer.db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	// This observer probes ownership rather than waiting to become a writer.
	if _, err = conn.ExecContext(context.Background(), "pragma busy_timeout=0"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := s.RefreshFoldIndex(ctx, boiler.Default()); done <- err }()
	deadline := time.After(5 * time.Second)
	acquired := false
	for !acquired {
		_, err = conn.ExecContext(context.Background(), "begin immediate")
		if foldBusy(err) {
			acquired = true
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if _, err = conn.ExecContext(context.Background(), "rollback"); err != nil {
			t.Fatal(err)
		}
		select {
		case err := <-done:
			t.Fatalf("refresh completed before active writer observation: %v", err)
		case <-deadline:
			t.Fatal("refresh never acquired writer lock")
		case <-time.After(time.Millisecond):
		}
	}
	// SQLITE_BUSY from this otherwise idle observer proves RefreshFoldIndex is
	// inside its immediate writer transaction. Cancel only after that signal.
	cancel()
	select {
	case err = <-done:
		if err == nil {
			t.Fatal("active publication ignored cancellation")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("active cancellation did not return")
	}
	after := foldPublishedSnapshot(t, s)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("cancelled in-flight refresh changed publication or consumed queues")
	}
	foldParity(t, s, boiler.Default())
}
