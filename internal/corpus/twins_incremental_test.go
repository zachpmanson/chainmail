package corpus

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"
)

func init() { twinsOracleRound = pendingTwinRound }

func pendingTwinRound(s *Store) error {
	if _, err := ReconcileTwinsContext(context.Background(), s); err != nil {
		return err
	}
	if _, err := RepairQuotedBodies(s); err != nil {
		return err
	}
	if _, err := RepairMailtoIdentities(s); err != nil {
		return err
	}
	if _, err := RepairPlusAddresses(s); err != nil {
		return err
	}
	if _, err := RepairTruncatedNames(s); err != nil {
		return err
	}
	if _, err := s.RepairGraph(); err != nil {
		return err
	}
	if _, err := s.ReassertParents(); err != nil {
		return err
	}
	_, err := s.RepairDanglingQuoteParents()
	return err
}

func TestTwinsStructuralFallbackReusesSelectedPlans(t *testing.T) {
	for _, derived := range []bool{false, true} {
		t.Run(map[bool]string{false: "collapse", true: "derived"}[derived], func(t *testing.T) {
			s := open(t)
			fixture := retryFixture(t, s)
			if derived {
				if err := s.EnrichQuoted(fixture.Collapse[1].Drop[0], Entry{BodyText: askAnnotated}); err != nil {
					t.Fatal(err)
				}
			}
			full, _ := snapshotTwinPlan(t, s)
			clearTestWork(t, s)
			// Only one selected author; the full expansion must normalize every body once.
			if _, err := s.db.Exec(`update entries set ts=ts+1 where id=?`, fixture.Collapse[1].Keep); err != nil {
				t.Fatal(err)
			}
			full, _ = snapshotTwinPlan(t, s)
			work, err := captureTwinWork(s.db)
			if err != nil {
				t.Fatal(err)
			}
			plan, expanded, err := planPendingTwins(s.db, work)
			if err != nil || !expanded || plan.Fallback != "structural-plan" || plan.BodiesScanned != full.BodiesScanned || plan.Comparisons != full.Comparisons || !reflect.DeepEqual(plan.Collapse, full.Collapse) || !reflect.DeepEqual(plan.Derived, full.Derived) {
				t.Fatalf("expanded %+v full %+v err=%v", plan, full, err)
			}
			if derived && len(plan.Derived) == 0 {
				t.Fatal("derived fallback untested")
			}
			applied, err := ReconcileTwinsContext(context.Background(), s)
			if err != nil || applied.Fallback != "structural-plan" || workState(t, s).Clean() {
				t.Fatalf("self invalidation lost %+v %v", applied, err)
			}
		})
	}
}

func TestTwinsWorkMergeOldAndNewAuthors(t *testing.T) {
	s := open(t)
	p := person(t, s, "merge-old@example.test", "Synthetic old")
	q := person(t, s, "merge-new@example.test", "Synthetic new")
	at := time.Unix(1700000000, 0)
	recovered(t, s, p, "old-mail", otherBody, at, 0)
	recovered(t, s, q, "new-mail", askBody, at, 0)
	clearTestWork(t, s)
	if err := Merge(s, q, p); err != nil {
		t.Fatal(err)
	}
	got := queuedPeople(t, s)
	if len(got) != 2 || got[0] != p || got[1] != q {
		t.Fatal("merge lost old/new tombstone", got)
	}
	if _, err := ReconcileTwinsContext(context.Background(), s); err != nil {
		t.Fatal(err)
	}
}

func TestTwinsPendingFailureAfterCommittedPrefixSurvivesReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "prefix-work.db")
	s := fileFoldStore(t, path)
	twinFixture(t, s)
	calls := 0
	failure := errors.New("invented interruption before ack")
	plan, err := runTwinsContext(context.Background(), s, true, true, func(ctx context.Context, expected *int64, apply func(corpusWriter) error) (time.Duration, error) {
		calls++
		if calls == 2 {
			return 0, failure
		}
		return s.guardedReconcileWrite(ctx, expected, apply)
	})
	if !errors.Is(err, failure) || plan.Removed != 1 || plan.Measured != 1 || workState(t, s).Clean() {
		t.Fatalf("%+v %v", plan, err)
	}
	reopened := fileFoldStore(t, path)
	final, err := ReconcileTwinsContext(context.Background(), reopened)
	if err != nil || final.Removed != 0 || final.Measured != 0 || !workState(t, reopened).Clean() {
		t.Fatalf("prefix replayed %+v %v", final, err)
	}
	assertRetryIntegrity(t, reopened, plan.Collapse)
}

func TestTwinsPendingExternalProcessConflict(t *testing.T) {
	path := filepath.Join(t.TempDir(), "process-work.db")
	s := fileFoldStore(t, path)
	_, drop := twinFixture(t, s)
	calls := 0
	plan, err := runTwinsContext(context.Background(), s, true, true, func(ctx context.Context, expected *int64, apply func(corpusWriter) error) (time.Duration, error) {
		calls++
		if calls == 1 {
			cmd := exec.Command(os.Args[0], "-test.run=^TestReconcileExternalProcessWriter$")
			cmd.Env = append(os.Environ(), "CHAINMAIL_RECONCILE_TEST_DB="+path)
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("external writer %s %v", output, err)
			}
		}
		return s.guardedReconcileWrite(ctx, expected, apply)
	})
	if err != nil || plan.Retries != 1 || plan.Removed != 0 || !workState(t, s).Clean() {
		t.Fatalf("%+v %v", plan, err)
	}
	var n int
	if err := s.db.QueryRow(`select count(*) from entries where id=?`, drop).Scan(&n); err != nil || n != 1 {
		t.Fatal("external veto lost", err)
	}
	checkRepairIndexes(t, s)
}

func TestTwinsSelectedCollapseEnablesUnselectedDerivedInSameRound(t *testing.T) {
	seed := func(s *Store) (int64, int64, int64) {
		keep, drop := twinFixture(t, s)
		var h int64
		if err := s.db.QueryRow(`select seen_in from sightings where entry_id=?`, drop).Scan(&h); err != nil {
			t.Fatal(err)
		}
		p := person(t, s, "unselected-derived@example.test", "Synthetic unselected derived author")
		at := time.Unix(1701000000, 0)
		copy := recovered(t, s, p, "cross-author-annotated", askAnnotated, at.Add(10*time.Hour), h)
		base := mailbox(t, s, p, "cross-author-base", askBody, at)
		for _, edge := range [][2]int64{{keep, h}, {drop, copy}, {base, drop}} {
			if err := s.SetParent(edge[0], edge[1]); err != nil {
				t.Fatal(err)
			}
		}
		bad, err := closesCycle(s.db, copy, base)
		if err != nil || !bad {
			t.Fatal("fixture must initially refuse derived edge", err)
		}
		clearTestWork(t, s)
		if _, err := s.db.Exec(`update entries set ts=ts+1 where id=?`, keep); err != nil {
			t.Fatal(err)
		}
		var selected int64
		if err := s.db.QueryRow(`select person_id from entries where id=?`, keep).Scan(&selected); err != nil {
			t.Fatal(err)
		}
		if got := queuedPeople(t, s); !reflect.DeepEqual(got, []int64{selected}) {
			t.Fatal("derived author was not unselected", got)
		}
		return copy, base, drop
	}
	full := open(t)
	pending := open(t)
	copy, base, _ := seed(full)
	seed(pending)
	if _, err := CollapseTwins(full, true); err != nil {
		t.Fatal(err)
	}
	plan, err := ReconcileTwinsContext(context.Background(), pending)
	if err != nil || plan.Fallback != "structural-plan" || plan.Removed != 1 || len(plan.Derived) != 1 {
		t.Fatalf("%+v %v", plan, err)
	}
	var parent int64
	if err := pending.db.QueryRow(`select parent_id from entries where id=?`, copy).Scan(&parent); err != nil || parent != base {
		t.Fatalf("unselected derived decision lost parent=%d %v", parent, err)
	}
	if !reflect.DeepEqual(oracleSemantic(allTwinTables(t, full)), oracleSemantic(allTwinTables(t, pending))) {
		t.Fatal("same-round full parity failed")
	}
}

func TestTwinsConcurrentCoordinatorsDoNotReplayCommittedGroups(t *testing.T) {
	path := filepath.Join(t.TempDir(), "two-runners.db")
	a := fileFoldStore(t, path)
	b := fileFoldStore(t, path)
	fixture := retryFixture(t, a)
	gate := make(chan struct{})
	var ready sync.WaitGroup
	ready.Add(2)
	type result struct {
		plan TwinPlan
		err  error
	}
	results := make(chan result, 2)
	for _, s := range []*Store{a, b} {
		go func(s *Store) {
			first := true
			plan, err := runTwinsContext(context.Background(), s, true, true, func(ctx context.Context, expected *int64, apply func(corpusWriter) error) (time.Duration, error) {
				if first {
					first = false
					ready.Done()
					<-gate
				}
				return s.guardedReconcileWrite(ctx, expected, apply)
			})
			results <- result{plan, err}
		}(s)
	}
	ready.Wait()
	close(gate)
	removed, measured := 0, 0
	for i := 0; i < 2; i++ {
		r := <-results
		if r.err != nil && !errors.Is(r.err, ErrReconcileConflict) {
			t.Fatal(r.err)
		}
		removed += r.plan.Removed
		measured += r.plan.Measured
	}
	if removed != 3 || measured != 3 {
		t.Fatalf("committed totals replayed removed=%d measured=%d", removed, measured)
	}
	if _, err := ReconcileTwinsContext(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	if !workState(t, a).Clean() {
		t.Fatal("work never settled")
	}
	assertRetryIntegrity(t, a, fixture.Collapse)
}
