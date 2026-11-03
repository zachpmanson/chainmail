package corpus

import (
	"context"
	"reflect"
	"testing"
	"time"
)

func TestTwinBodyPendingBudgetsDoNotLimitPlan(t *testing.T) {
	for _, mode := range []string{"bytes", "residual-bytes", "rows"} {
		t.Run(mode, func(t *testing.T) {
			s := open(t)
			twinFixture(t, s)
			before := reconcileState(t, s)
			want, err := planTwins(s.db)
			if err != nil {
				t.Fatal(err)
			}
			cache := new(twinBodyCache)
			switch mode {
			case "bytes":
				cache.pendingBytes = twinBodyMaxPendingBytes
			case "residual-bytes":
				cache.pendingBytes = twinBodyMaxPendingBytes - 1
			case "rows":
				cache.pending = make([]twinBodyRow, twinBodyMaxPendingRows)
			}
			oldRows, oldBytes := len(cache.pending), cache.pendingBytes
			got, err := planTwinsCached(s.db, cache)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) || cache.reductions != 2 || cache.hits != 0 {
				t.Fatalf("budget limited logical plan: reductions=%d hits=%d", cache.reductions, cache.hits)
			}
			if len(cache.pending) != oldRows || cache.pendingBytes != oldBytes || reconcileState(t, s) != before {
				t.Fatal("budget overflow or canonical readiness changed")
			}
		})
	}
}

func TestTwinBodyPublicationByteBounds(t *testing.T) {
	s := open(t)
	state := reconcileState(t, s)
	sizes := []int{600 << 10, 500 << 10, twinBodyMaxPayload, twinBodyMaxPayload + 1, 64}
	rows := make([]twinBodyRow, len(sizes))
	for i, n := range sizes {
		rows[i] = twinBodyRow{id: int64(i + 1), payload: make([]byte, n)}
	}
	var batches []int
	_, err := publishTwinBodies(context.Background(), rows, state.Source, func(ctx context.Context, e *int64, f func(corpusWriter) error) (time.Duration, error) {
		return s.guardedReconcileWrite(ctx, e, func(w corpusWriter) error {
			var before, after int
			if err := w.QueryRow(`select coalesce(sum(length(payload)),0) from twins_body_cache`).Scan(&before); err != nil {
				return err
			}
			if err := f(w); err != nil {
				return err
			}
			if err := w.QueryRow(`select coalesce(sum(length(payload)),0) from twins_body_cache`).Scan(&after); err != nil {
				return err
			}
			batches = append(batches, after-before)
			return nil
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []int{600 << 10, 500 << 10, twinBodyMaxPayload, 64}
	if !reflect.DeepEqual(batches, want) {
		t.Fatalf("byte batches=%v want=%v", batches, want)
	}
	for _, n := range batches {
		if n > 1<<20 {
			t.Fatalf("oversized metadata transaction: %d", n)
		}
	}
	var skipped int
	scanTwinCache(t, s.db.QueryRow(`select count(*) from twins_body_cache where entry_id=4`), &skipped)
	if skipped != 0 || reconcileState(t, s) != state {
		t.Fatal("oversized row published or canonical state changed")
	}
}
