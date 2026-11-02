package refresh

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/zachpmanson/chainmail/internal/corpus"
)

func TestLegacyRefreshReportsPendingWithoutCertifyingFullPair(t *testing.T) {
	s, err := corpus.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	put(t, s, msg("legacy-synthetic", "Synthetic body for the existing page."))
	prev := prevRun(t, s, "thread-hedge", "synthetic")
	rep, next, err := Run(s, newMailbox(), prev, Options{})
	if err != nil {
		t.Fatal(err)
	}
	state, err := s.ReconciliationState()
	if err != nil {
		t.Fatal(err)
	}
	if state.Clean() || !rep.MaintenancePending || rep.MaintenanceSkipped {
		t.Fatal("partial path certified maintenance", rep, state)
	}
	// NothingNew still describes page/content changes, not maintenance coverage.
	rep, _, err = Run(s, newMailbox(), next, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !rep.NothingNew() || !rep.MaintenancePending {
		t.Fatal("page and maintenance outcomes conflated", rep)
	}
}

func TestLegacyRefreshConsumesCleanCheckpointWithoutAcknowledging(t *testing.T) {
	s, err := corpus.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	put(t, s, msg("legacy-synthetic", "Synthetic body for the existing page."))
	prev := prevRun(t, s, "thread-hedge", "synthetic")
	if _, err := corpus.ReconcileTwinsContext(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	state, err := s.ReconciliationState()
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := s.CheckpointReconciliation(context.Background(), state); err != nil || !ok {
		t.Fatal(err)
	}
	before, err := s.ReconciliationState()
	if err != nil {
		t.Fatal(err)
	}
	rep, _, err := Run(s, newMailbox(), prev, Options{})
	if err != nil {
		t.Fatal(err)
	}
	after, err := s.ReconciliationState()
	if err != nil {
		t.Fatal(err)
	}
	if !rep.MaintenanceSkipped || rep.MaintenancePending || rep.TwinsBodiesScanned != 0 || rep.QuoteHostsScanned != 0 || before != after {
		t.Fatal("clean partial path rescanned or acknowledged", rep, before, after)
	}
}

func TestLegacyRefreshCombinedSnapshotCannotTear(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	s, err := corpus.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	other, err := corpus.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	put(t, s, msg("legacy-synthetic", "Synthetic body for the existing page."))
	prev := prevRun(t, s, "thread-hedge", "synthetic")
	if _, err := corpus.ReconcileTwinsContext(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	start, err := s.ReconciliationState()
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := s.CheckpointReconciliation(context.Background(), start); err != nil || !ok {
		t.Fatal(ok, err)
	}
	reads := 0
	read := func() (corpus.MaintenanceState, error) {
		reads++
		if reads == 2 {
			if _, err := other.DB().Exec(`update twins_work_state set work_revision=work_revision+1,full_revision=work_revision+1`); err != nil {
				t.Fatal(err)
			}
		}
		state, err := s.MaintenanceState()
		if reads == 2 && err == nil {
			if _, err := other.DB().Exec(`insert into people(display_name) values('Concurrent legacy identity')`); err != nil {
				t.Fatal(err)
			}
			if _, err := corpus.ReconcileTwinsContext(context.Background(), other); err != nil {
				t.Fatal(err)
			}
		}
		return state, err
	}
	rep, next, err := runWithMaintenanceState(s, newMailbox(), prev, Options{}, read)
	if err != nil || !rep.MaintenancePending || !rep.MaintenanceSkipped || rep.ReconcileSource != start.Source {
		t.Fatal(rep, err)
	}
	actual, err := s.MaintenanceState()
	if err != nil || actual.Source.Clean() || !actual.Twins.Clean() {
		t.Fatal(actual, err)
	}
	rep, _, err = Run(s, newMailbox(), next, Options{})
	if err != nil || !rep.MaintenancePending || rep.MaintenanceSkipped || rep.ReconcileSource != actual.Source.Source {
		t.Fatal(rep, err)
	}
}
