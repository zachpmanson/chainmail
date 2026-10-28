package corpus

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/zachpmanson/chainmail/internal/boiler"
)

// Opt-in ONLY on a disposable writable scratch copy. Never point this at an
// original export or a live database. Output contains aggregates, not mail IDs,
// contents, domains, fingerprints or paths. The caller owns copying and cleanup.
func TestFoldIndexScratch(t *testing.T) {
	path := os.Getenv("CHAINMAIL_FOLD_SCRATCH")
	if path == "" {
		t.Skip("set CHAINMAIL_FOLD_SCRATCH to a disposable writable database copy")
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal("scratch database must already exist")
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0200 == 0 || strings.Contains(strings.ToLower(filepath.Base(resolved)), "baseline") {
		t.Fatal("refusing non-writable/original database; supply a disposable scratch copy")
	}
	ctx := context.Background()
	r := boiler.Default()
	s, err := Open(resolved)
	if err != nil {
		t.Fatal("opening scratch copy failed")
	}
	defer func() {
		if s != nil {
			s.Close()
		}
	}()
	emit := func(phase string, value any) {
		encoded, err := json.Marshal(struct {
			Phase string `json:"phase"`
			Value any    `json:"value"`
		}{phase, value})
		if err != nil {
			t.Fatal("encoding aggregate failed")
		}
		t.Log(string(encoded))
	}
	stats, err := s.RefreshFoldIndex(ctx, r)
	if err != nil {
		t.Fatal("scratch refresh failed")
	}
	emit("refresh", stats)
	msgs, ids, err := freshFoldEvidence(ctx, s)
	if err != nil {
		t.Fatal("fresh reduction failed")
	}
	want := boiler.Detect(msgs, r)
	if len(want) == 0 {
		t.Fatal("scratch validation requires nonempty original folds")
	}
	got, err := s.SelectedFolds(ctx, ids, r)
	if err != nil || !reflect.DeepEqual(want, got) {
		t.Fatal("full original-Detect parity failed")
	}
	checkFoldLedger(t, s, msgs, r)
	emit("parity", map[string]int{"mail_rows": len(ids), "reducible_messages": len(msgs), "folds": len(want)})
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	selected := []int64{ids[len(ids)/2]}
	for _, id := range ids {
		if _, ok := want[id]; ok {
			selected = []int64{id}
			break
		}
	}
	// Verify selected-only map semantics independently before measuring reads.
	one, err := s.SelectedFolds(ctx, selected, r)
	if err != nil || len(one) != 1 || one[selected[0]] != want[selected[0]] {
		t.Fatal("selected original-Detect parity failed")
	}
	msgs = nil
	want = nil
	got = nil
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	times := make([]time.Duration, 100)
	for i := range times {
		start := time.Now()
		if _, err = s.SelectedFolds(ctx, selected, r); err != nil {
			t.Fatal("selected read failed")
		}
		times[i] = time.Since(start)
	}
	runtime.ReadMemStats(&after)
	sort.Slice(times, func(i, j int) bool { return times[i] < times[j] })
	emit("selected", map[string]any{"iterations": len(times), "p50_ns": times[49].Nanoseconds(), "p95_ns": times[94].Nanoseconds(), "p99_ns": times[98].Nanoseconds(), "allocated_bytes": after.TotalAlloc - before.TotalAlloc, "allocations": after.Mallocs - before.Mallocs})
	if err = s.Close(); err != nil {
		t.Fatal("closing scratch failed")
	}
	s, err = Open(resolved)
	if err != nil {
		t.Fatal("reopening scratch failed")
	}
	clean, err := s.WarmFolds()
	if err != nil || clean.Bodies != 0 || clean.Memberships != 0 {
		t.Fatal("clean restart performed normalization or membership writes")
	}
	emit("clean_restart", clean)
}
