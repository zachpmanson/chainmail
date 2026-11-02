package corpus

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
)

func TestCheckpointRejectsPendingTwinsInsideWriter(t *testing.T) {
	s := integrityStore(t)
	start, err := s.ReconciliationState()
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := s.CheckpointReconciliation(context.Background(), start); err != nil || ok {
		t.Fatalf("pending work certified: %t %v", ok, err)
	}
	if _, err := ReconcileTwinsContext(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	// Eligibility was established, but a work-only epoch arrives before the CAS.
	if _, err := s.db.Exec(`update twins_work_state set work_revision=work_revision+1,full_revision=work_revision+1`); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.CheckpointReconciliation(context.Background(), start); err != nil || ok {
		t.Fatalf("CAS gap certified: %t %v", ok, err)
	}
	state, err := s.ReconciliationState()
	if err != nil || state.Clean() {
		t.Fatal(state, err)
	}
	work, err := s.TwinsWorkState()
	if err != nil || !work.Full.Valid {
		t.Fatal(work, err)
	}
	if _, err := ReconcileTwinsContext(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.CheckpointReconciliation(context.Background(), start); err != nil || !ok {
		t.Fatal(ok, err)
	}
}

// The callback runs after the first state has been scanned, before the second
// query executes, without a Store/global test hook.
type betweenStateReads struct {
	corpusQuery
	between func()
	reads   int
}

func (q *betweenStateReads) QueryRow(query string, args ...any) *sql.Row {
	q.reads++
	if q.reads == 2 {
		q.between()
	}
	return q.corpusQuery.QueryRow(query, args...)
}

func TestMaintenanceStateSnapshot(t *testing.T) {
	for _, identityOnly := range []bool{false, true} {
		t.Run(fmt.Sprint(identityOnly), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "snapshot.db")
			s, other := fileFoldStore(t, path), fileFoldStore(t, path)
			checkpoint(t, s)
			if _, err := other.db.Exec(`update twins_work_state set work_revision=work_revision+1,full_revision=work_revision+1`); err != nil {
				t.Fatal(err)
			}
			tx, err := s.db.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			q := &betweenStateReads{corpusQuery: tx, between: func() {
				if _, err := other.db.Exec(`insert into people(display_name) values('Concurrent identity')`); err != nil {
					t.Fatal(err)
				}
				if identityOnly {
					if _, err := ReconcileTwinsContext(context.Background(), other); err != nil {
						t.Fatal(err)
					}
				} else {
					if _, err := other.db.Exec(`insert into entries(source,ext_id,ts,body_text,body_sha,ingested_at) values('mail','after-snapshot',0,'Invented new evidence','synthetic',0)`); err != nil {
						t.Fatal(err)
					}
				}
			}}
			got, err := readMaintenanceState(q)
			if err != nil || !got.Source.Clean() || !got.Twins.Full.Valid || got.Clean() {
				t.Fatal(got, err)
			}
			next, err := other.MaintenanceState()
			if err != nil || next.Source.Clean() || (identityOnly && !next.Twins.Clean()) || (!identityOnly && next.Twins.Revision <= got.Twins.Revision) {
				t.Fatal(got, next, err)
			}
		})
	}
}

func TestMaintenanceStateInitializationAndErrors(t *testing.T) {
	for _, table := range []string{"reconcile_state", "twins_work_state"} {
		t.Run(table, func(t *testing.T) {
			s := open(t)
			if _, err := s.db.Exec(`delete from ` + table); err != nil {
				t.Fatal(err)
			}
			got, err := s.MaintenanceState()
			if err != nil || got.Clean() {
				t.Fatal(got, err)
			}
			if _, err := s.db.Exec(`drop table ` + table); err != nil {
				t.Fatal(err)
			}
			if _, err := s.MaintenanceState(); err == nil {
				t.Fatal("state read error hidden")
			}
		})
	}
}
