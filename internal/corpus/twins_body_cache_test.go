package corpus

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"
)

func cachedTwinPlan(t testing.TB, s *Store, publish bool) (TwinPlan, *twinBodyCache) {
	t.Helper()
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	state, err := readReconcileState(tx)
	if err != nil {
		t.Fatal(err)
	}
	cache := new(twinBodyCache)
	plan, err := planTwinsCached(tx, cache)
	tx.Rollback()
	if err != nil {
		t.Fatal(err)
	}
	if publish {
		if _, err := publishTwinBodies(context.Background(), cache.pending, state.Source, s.guardedReconcileWrite); err != nil {
			t.Fatal(err)
		}
	}
	return plan, cache
}

func TestTwinBodyCacheColdWarm(t *testing.T) {
	s := fileFoldStore(t, filepath.Join(t.TempDir(), "cache.db"))
	seedTwinOracle(t, s)
	want, err := CollapseTwins(s, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(want.Collapse) == 0 || len(want.Derived) == 0 || len(want.Declined) == 0 || want.Annotated == 0 || want.Measured == 0 {
		t.Fatal("fixture must exercise all ordered plan outputs")
	}
	before := reconcileState(t, s)
	cold, c := cachedTwinPlan(t, s, true)
	warm, w := cachedTwinPlan(t, s, true)
	if !reflect.DeepEqual(want, cold) || !reflect.DeepEqual(want, warm) || c.reductions != want.BodiesScanned || c.hits != 0 || w.reductions != 0 || w.hits != want.BodiesScanned || len(w.pending) != 0 {
		t.Fatalf("plans/counters differ: cold=%+v warm=%+v", c, w)
	}
	if got := reconcileState(t, s); got != before {
		t.Fatal("cache changed reconciliation state")
	}
}
