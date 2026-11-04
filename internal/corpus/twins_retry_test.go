package corpus

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func retryFixture(t *testing.T, s *Store) TwinPlan {
	t.Helper()
	twinFixture(t, s)
	for i := 1; i < 3; i++ {
		p := person(t, s, fmt.Sprintf("retry%d@example.test", i), "Invented retry author")
		h := person(t, s, fmt.Sprintf("retryhost%d@example.test", i), "Invented retry host")
		at := time.Unix(1701000000+int64(i)*86400, 0)
		mailbox(t, s, p, fmt.Sprintf("retry%d", i), askBody, at)
		hostID := host(t, s, h, fmt.Sprintf("retryhost%d", i), at.Add(time.Hour))
		recovered(t, s, p, fmt.Sprintf("retrycopy%d", i), askQuoted, at.Add(10*time.Hour), hostID)
	}
	plan, _ := snapshotTwinPlan(t, s)
	if len(plan.Collapse) != 3 || plan.Measured != 3 {
		t.Fatalf("fixture: %+v", plan)
	}
	return plan
}

func assertRetryIntegrity(t *testing.T, s *Store, committed []TwinCollapse) {
	t.Helper()
	checkRepairIndexes(t, s)
	rows, err := s.db.Query(`pragma foreign_key_check`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if rows.Next() {
		t.Fatal("foreign key violation")
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := s.db.QueryRow(`select count(*) from render_offsets`).Scan(&n); err != nil || n != len(committed) {
		t.Fatalf("offset count %d: %v", n, err)
	}
	for _, c := range committed {
		for _, o := range c.Offsets {
			if err := s.db.QueryRow(`select count(*) from render_offsets where person_id=? and entry_id=? and at=? and off=? and measured_from=?`, o.Person, o.Host, o.At.Unix(), o.Off, o.From).Scan(&n); err != nil || n != 1 {
				t.Fatalf("offset %+v: %d %v", o, n, err)
			}
		}
		for _, drop := range c.Drop {
			if err := s.db.QueryRow(`select count(*) from entries_fts where entries_fts match 'Rothwell' and rowid=?`, drop).Scan(&n); err != nil || n != 0 {
				t.Fatalf("stale search row %d: %d %v", drop, n, err)
			}
		}
	}
}

func TestCollapseTwinsContextReplansAfterCommittedPrefix(t *testing.T) {
	path := filepath.Join(t.TempDir(), "retry.db")
	s := fileFoldStore(t, path)
	other := fileFoldStore(t, path)
	plan := retryFixture(t, s)
	calls, conflicts := 0, 0
	edited := strings.Repeat("Independent incompatible synthetic evidence about observatories and telescopes. ", 20)
	result, err := retryTwinsContext(context.Background(), s, true, func(ctx context.Context, expected *int64, apply func(corpusWriter) error) (time.Duration, error) {
		calls++
		wait, err := s.guardedReconcileWrite(ctx, expected, apply)
		if errors.Is(err, ErrReconcileConflict) {
			conflicts++
		}
		if calls == 1 && err == nil {
			if e := other.EnrichQuoted(plan.Collapse[1].Drop[0], Entry{BodyText: edited}); e != nil {
				t.Fatal(e)
			}
		}
		return wait, err
	})
	want := []TwinCollapse{plan.Collapse[0], plan.Collapse[2]}
	if err != nil || conflicts != 1 || calls != 3 || result.Retries != 1 || result.Removed != 2 || result.Measured != 2 || result.WithMailbox != 2 || result.QuotedOnly != 0 || len(result.Derived) != 0 || !reflect.DeepEqual(result.Collapse, want) {
		t.Fatalf("result %+v calls=%d conflicts=%d err=%v", result, calls, conflicts, err)
	}
	if result.BodiesScanned <= plan.BodiesScanned || result.Comparisons <= plan.Comparisons {
		t.Fatal("attempted planning not accumulated")
	}
	var body string
	if err := s.db.QueryRow(`select body_text from entries where id=?`, plan.Collapse[1].Drop[0]).Scan(&body); err != nil || body != edited {
		t.Fatal("edited evidence lost", err)
	}
	assertRetryIntegrity(t, s, want)
	before := reconciliationSnapshot(t, s)
	stable, err := CollapseTwins(s, true)
	if err != nil || stable.Removed != 0 || stable.Measured != 0 || !reflect.DeepEqual(before, reconciliationSnapshot(t, s)) {
		t.Fatalf("unstable rerun %+v %v", stable, err)
	}
}

func TestCollapseTwinsContextConflictExhaustion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "exhaust.db")
	s := fileFoldStore(t, path)
	other := fileFoldStore(t, path)
	plan := retryFixture(t, s)
	start := reconcileState(t, s)
	calls, conflicts := 0, 0
	result, err := retryTwinsContext(context.Background(), s, true, func(ctx context.Context, expected *int64, apply func(corpusWriter) error) (time.Duration, error) {
		calls++
		if calls > 1 {
			if _, e := other.db.Exec(`update entries set ts=ts+1 where id=?`, plan.Collapse[1].Keep); e != nil {
				t.Fatal(e)
			}
		}
		wait, err := s.guardedReconcileWrite(ctx, expected, apply)
		if errors.Is(err, ErrReconcileConflict) {
			conflicts++
		}
		return wait, err
	})
	if !errors.Is(err, ErrReconcileConflict) || calls != 4 || conflicts != 3 || result.Retries != 3 || result.Removed != 1 || result.Measured != 1 || result.WithMailbox != 1 || !reflect.DeepEqual(result.Collapse, plan.Collapse[:1]) {
		t.Fatalf("result %+v calls=%d conflicts=%d err=%v", result, calls, conflicts, err)
	}
	assertRetryIntegrity(t, s, plan.Collapse[:1])
	reopened := fileFoldStore(t, path)
	if reconcileState(t, reopened).Clean() {
		t.Fatal("exhaustion certified")
	}
	if ok, e := reopened.CheckpointReconciliation(context.Background(), start); e != nil || ok {
		t.Fatalf("stale checkpoint %v %v", ok, e)
	}
	remaining, _ := snapshotTwinPlan(t, reopened)
	if len(remaining.Collapse) != 2 {
		t.Fatalf("pending groups lost %+v", remaining)
	}
}

func TestCollapseTwinsContextStopsOnError(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		t.Run(fmt.Sprint(cancelled), func(t *testing.T) {
			s := fileFoldStore(t, filepath.Join(t.TempDir(), "stop.db"))
			plan := retryFixture(t, s)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			failure := errors.New("invented execution failure")
			calls := 0
			result, err := retryTwinsContext(ctx, s, true, func(ctx context.Context, expected *int64, apply func(corpusWriter) error) (time.Duration, error) {
				calls++
				if calls == 2 {
					if cancelled {
						cancel()
						return s.guardedReconcileWrite(ctx, expected, apply)
					}
					return 0, failure
				}
				return s.guardedReconcileWrite(ctx, expected, apply)
			})
			want := failure
			if cancelled {
				want = context.Canceled
			}
			if !errors.Is(err, want) || calls != 2 || result.Retries != 0 || result.Removed != 1 {
				t.Fatalf("%+v %v calls=%d", result, err, calls)
			}
			assertRetryIntegrity(t, s, plan.Collapse[:1])
		})
	}
}

func TestCollapseTwinsContextReplansDerivedAfterPrefix(t *testing.T) {
	path := filepath.Join(t.TempDir(), "derived-retry.db")
	s := fileFoldStore(t, path)
	other := fileFoldStore(t, path)
	keep, _ := twinFixture(t, s)
	p := person(t, s, "derived-retry@example.test", "Synthetic derived author")
	at := time.Unix(1701000000, 0)
	annotated := recovered(t, s, p, "derived-retry-copy", askAnnotated, at.Add(10*time.Hour), keep)
	base := mailbox(t, s, p, "derived-retry-base", askBody, at)
	calls, conflicts := 0, 0
	plan, err := retryTwinsContext(context.Background(), s, true, func(ctx context.Context, expected *int64, apply func(corpusWriter) error) (time.Duration, error) {
		calls++
		if calls == 2 {
			// The prefix is committed. A real outside graph edit changes cycle evidence.
			if err := other.SetParent(base, annotated); err != nil {
				t.Fatal(err)
			}
		}
		wait, err := s.guardedReconcileWrite(ctx, expected, apply)
		if errors.Is(err, ErrReconcileConflict) {
			conflicts++
		}
		return wait, err
	})
	if err != nil || calls != 3 || conflicts != 1 || plan.Retries != 1 || plan.Removed != 1 || len(plan.Derived) != 1 || plan.Derived[0] != (DerivedRelation{Copy: annotated, Base: base}) {
		t.Fatalf("%+v %v calls=%d conflicts=%d", plan, err, calls, conflicts)
	}
	var derived bool
	var parent int64
	if err := s.db.QueryRow(`select derived,coalesce(parent_id,0) from entries where id=?`, annotated).Scan(&derived, &parent); err != nil || !derived || parent != 0 {
		t.Fatalf("stale cycle decision derived=%v parent=%d %v", derived, parent, err)
	}
	assertRetryIntegrity(t, s, plan.Collapse)
}

func retryTwinsContext(ctx context.Context, s *Store, apply bool, write twinWrite) (TwinPlan, error) {
	return collapseTwinsContext(ctx, s, apply, write, s.guardedReconcileWrite)
}
