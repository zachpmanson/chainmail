package refresh

import (
	"context"
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
