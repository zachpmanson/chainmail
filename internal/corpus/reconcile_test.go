package corpus

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

func reconcileState(t *testing.T, s *Store) ReconcileState {
	t.Helper()
	r, err := s.ReconciliationState()
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func checkpoint(t *testing.T, s *Store) {
	t.Helper()
	ok, err := s.CheckpointReconciliation(context.Background(), reconcileState(t, s))
	if err != nil || !ok {
		t.Fatalf("checkpoint: %v %v", ok, err)
	}
}

func TestReconcileEffectiveMutationMatrix(t *testing.T) {
	s := open(t)
	p := person(t, s, "one@example.test", "Synthetic One")
	q := person(t, s, "two@example.test", "Synthetic Two")
	at := time.Unix(1700000000, 0)
	a := mailbox(t, s, p, "source", askBody, at)
	h := host(t, s, q, "host", at.Add(time.Hour))
	quote := recovered(t, s, p, "copy", askQuoted, at.Add(10*time.Hour), h)
	if _, err := s.db.Exec("insert into gmail_copies(account_id,gmail_id,entry_id) values('legacy','synthetic-copy',?)", a); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("insert into entry_embeddings(entry_id,model,dim,body_sha,prep,skip,embedded_at) values(?,'synthetic',3,'synthetic-hash',1,'no topic',0)", a); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, sql string
		args      []any
		dirty     bool
	}{
		{"unchanged entry", `update entries set person_id=person_id,body_text=body_text,subject=subject,quoted=quoted`, nil, false},
		{"hash only", `update entries set body_sha='independent-hash' where id=?`, []any{a}, false},
		{"ingest time", `update entries set ingested_at=ingested_at+1`, nil, false},
		{"labels", `update mail_detail set labels='UNREAD'`, nil, false},
		{"embedding progress", `update entry_embeddings set prep=2,embedded_at=42`, nil, false},
		{"copy history", `update gmail_copies set labels='INBOX',seen_history_id='42'`, nil, false},
		{"preferences", `update people set org='colour',prefer_original=1`, nil, false},
		{"attachment hydration", `update attachments set mime='image/png',name='hydrated',size=42`, nil, false},
		{"sighting bookkeeping", `update sightings set detail='depth 9'`, nil, false},
		{"timestamp same hash", `update entries set ts=ts+60 where id=?`, []any{quote}, true},
		{"author same hash", `update entries set person_id=? where id=?`, []any{q, quote}, true},
		{"host HTML", `update entries set body_html='<p>New destination</p>' where id=?`, []any{h}, true},
		{"host body", `update entries set body_text='New host evidence' where id=?`, []any{h}, true},
		{"promotion", `update entries set quoted=0 where id=?`, []any{quote}, true},
		{"parent", `update entries set parent_id=? where id=?`, []any{h, a}, true},
		{"parent reference", `update entries set parent_ref='new-reference' where id=?`, []any{a}, true},
		{"derived", `update entries set derived=1 where id=?`, []any{quote}, true},
		{"header", `update mail_detail set refs='new-reference' where entry_id=?`, []any{a}, true},
		{"name", `update people set display_name='Synthetic Renamed' where id=?`, []any{q}, true},
		{"Slack identity", `insert into identities(person_id,kind,value) values(?,'slack_uid','synthetic-slack')`, []any{p}, true},
		{"identity owner", `update identities set person_id=? where kind='slack_uid'`, []any{q}, true},
		{"participant", `insert into participants(entry_id,person_id,role) values(?,?,'cc')`, []any{h, p}, true},
		{"alias", `insert into domain_aliases values('old.test','new.test','synthetic',0)`, nil, true},
		{"sighting host", `update sightings set seen_in=? where entry_id=?`, []any{a, quote}, true},
		{"sighting remove", `delete from sightings where entry_id=?`, []any{quote}, true},
		{"attachment safety", `insert into attachments(entry_id,name) values(?,'veto')`, []any{quote}, true},
		{"attachment remove", `delete from attachments where entry_id=?`, []any{quote}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			checkpoint(t, s)
			before := reconcileState(t, s)
			if _, err := s.db.Exec(tc.sql, tc.args...); err != nil {
				t.Fatal(err)
			}
			after := reconcileState(t, s)
			if (after.Source != before.Source) != tc.dirty || after.Clean() == tc.dirty {
				t.Fatalf("before=%+v after=%+v dirty=%t", before, after, tc.dirty)
			}
		})
	}
	checkpoint(t, s)
	before := reconcileState(t, s)
	tx, err := s.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(`update entries set body_text='rolled back'`); err != nil {
		t.Fatal(err)
	}
	tx.Rollback()
	if got := reconcileState(t, s); got != before {
		t.Fatalf("rollback changed state: %+v", got)
	}
	if err := Merge(s, p, q); err != nil {
		t.Fatal(err)
	}
	if reconcileState(t, s).Clean() {
		t.Fatal("merge did not invalidate")
	}
}

func TestReconcileDurabilityVersionsAndCheckpointCAS(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reconcile.db")
	s := fileFoldStore(t, path)
	other := fileFoldStore(t, path)
	initial := reconcileState(t, s)
	if initial.Clean() || initial.Ready {
		t.Fatal("new migration must start dirty")
	}
	checkpoint(t, s)
	if !reconcileState(t, other).Clean() {
		t.Fatal("checkpoint not durable")
	}
	start := reconcileState(t, s)
	if _, err := other.db.Exec(`insert into people(display_name) values('Outside process')`); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.CheckpointReconciliation(context.Background(), start); err != nil || ok {
		t.Fatalf("stale checkpoint=%v %v", ok, err)
	}
	reopened := fileFoldStore(t, path)
	if reconcileState(t, reopened).Clean() {
		t.Fatal("reopen lost pending work")
	}
	if _, err := s.db.Exec(`update reconcile_state set rules_version=0,covered_version=0`); err != nil {
		t.Fatal(err)
	}
	if reconcileState(t, s).Clean() {
		t.Fatal("old rules accepted")
	}
	checkpoint(t, s)
	if _, err := s.db.Exec(`update reconcile_state set rules_version=?`, reconciliationVersion+1); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReconciliationState(); err == nil {
		t.Fatal("newer rules accepted")
	}
	if _, err := s.CheckpointReconciliation(context.Background(), start); err == nil {
		t.Fatal("newer rules overwritten")
	}
	if _, err := s.db.Exec(`delete from reconcile_state`); err != nil {
		t.Fatal(err)
	}
	if reconcileState(t, s).Clean() {
		t.Fatal("missing state accepted")
	}
}

func twinFixture(t *testing.T, s *Store) (int64, int64) {
	t.Helper()
	p := person(t, s, "author@example.test", "Synthetic Author")
	q := person(t, s, "host@example.test", "Synthetic Host")
	at := time.Unix(1700000000, 0)
	keep := mailbox(t, s, p, "oracle", askBody, at)
	h := host(t, s, q, "oracle-host", at.Add(time.Hour))
	drop := recovered(t, s, p, "oracle-copy", askQuoted, at.Add(10*time.Hour), h)
	return keep, drop
}

func snapshotTwinPlan(t *testing.T, s *Store) (TwinPlan, int64) {
	t.Helper()
	tx, err := s.db.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	r, err := readReconcileState(tx)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := planTwins(tx)
	if err != nil {
		t.Fatal(err)
	}
	return plan, r.Source
}

func TestReconcileTwinApplyRejectsOutsideMutations(t *testing.T) {
	for _, change := range []string{
		`update entries set body_text='Different evidence' where id=?`,
		`insert into attachments(entry_id,name) values(?,'new attachment veto')`,
		`update entries set person_id=null where id=?`,
		`delete from sightings where entry_id=?`,
		`update entries set derived=1,parent_id=null where id=?`,
	} {
		t.Run(change, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "guard.db")
			s := fileFoldStore(t, path)
			other := fileFoldStore(t, path)
			_, drop := twinFixture(t, s)
			plan, expected := snapshotTwinPlan(t, s)
			if len(plan.Collapse) != 1 {
				t.Fatalf("plan=%+v", plan)
			}
			if strings.Contains(change, "body_text") {
				if err := other.EnrichQuoted(drop, Entry{BodyText: strings.Repeat("Different synthetic evidence unrelated to the original message. ", 20)}); err != nil {
					t.Fatal(err)
				}
			} else if _, err := other.db.Exec(change, drop); err != nil {
				t.Fatal(err)
			}
			called := false
			_, err := s.guardedReconcileWrite(context.Background(), &expected, func(w corpusWriter) error { called = true; return collapseGroup(w, s, plan.Collapse[0]) })
			if !errors.Is(err, ErrReconcileConflict) || called {
				t.Fatalf("stale apply: called=%v err=%v", called, err)
			}
			var n int
			if err := s.db.QueryRow(`select count(*) from entries where id=?`, drop).Scan(&n); err != nil || n != 1 {
				t.Fatal("stale destructive deletion", err)
			}
			if _, err := CollapseTwins(s, true); err != nil {
				t.Fatal(err)
			}
			checkRepairIndexes(t, s)
		})
	}
}

func TestReconcileSnapshotAndCommittedPrefix(t *testing.T) {
	path := filepath.Join(t.TempDir(), "prefix.db")
	s := fileFoldStore(t, path)
	other := fileFoldStore(t, path)
	keep, drop := twinFixture(t, s)
	p := person(t, s, "third@example.test", "Synthetic Third")
	at := time.Unix(1701000000, 0)
	keep2 := mailbox(t, s, p, "second", askBody, at)
	drop2 := recovered(t, s, p, "second-copy", askQuoted, at.Add(10*time.Hour), keep)
	tx, err := s.db.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	state, err := readReconcileState(tx)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := planTwins(tx)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Collapse) != 2 {
		t.Fatalf("plan=%+v", plan)
	}
	// A competing WAL writer completes without waiting for expensive planning.
	if _, err = other.db.Exec(`update entries set ts=ts+1 where id=?`, keep2); err != nil {
		t.Fatal(err)
	}
	frozen, err := readReconcileState(tx)
	if err != nil || frozen != state {
		t.Fatalf("snapshot changed: %+v %v", frozen, err)
	}
	tx.Rollback()
	plan, expected := snapshotTwinPlan(t, s)
	if _, err = s.guardedReconcileWrite(context.Background(), &expected, func(w corpusWriter) error { return collapseGroup(w, s, plan.Collapse[0]) }); err != nil {
		t.Fatal(err)
	}
	if _, err = other.db.Exec(`insert into attachments(entry_id,name) values(?,'veto')`, drop2); err != nil {
		t.Fatal(err)
	}
	if _, err = s.guardedReconcileWrite(context.Background(), &expected, func(w corpusWriter) error { return collapseGroup(w, s, plan.Collapse[1]) }); !errors.Is(err, ErrReconcileConflict) {
		t.Fatal("outside mutation after prefix not rejected", err)
	}
	result, err := CollapseTwins(s, true)
	if err != nil || result.Removed != 0 {
		t.Fatalf("prefix replayed: %+v %v", result, err)
	}
	var n int
	if err = s.db.QueryRow(`select count(*) from entries where id=?`, drop).Scan(&n); err != nil || n != 0 {
		t.Fatal("prefix not committed", err)
	}
	checkRepairIndexes(t, s)
}

func TestReconcileGuardRollbackAndCancellation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cancel.db")
	s := fileFoldStore(t, path)
	other := fileFoldStore(t, path)
	keep, _ := twinFixture(t, s)
	start := reconcileState(t, s)
	expected := start.Source
	_, err := s.guardedReconcileWrite(context.Background(), &expected, func(w corpusWriter) error {
		_, err := w.Exec(`update entries set derived=1 where id=?`, keep)
		if err != nil {
			return err
		}
		return fmt.Errorf("synthetic failure")
	})
	if err == nil || expected != start.Source || reconcileState(t, s) != start {
		t.Fatal("failed transaction changed revision")
	}
	w, err := other.beginWriter(context.Background(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	called := false
	_, err = s.guardedReconcileWrite(ctx, &expected, func(corpusWriter) error { called = true; return nil })
	w.close()
	if !errors.Is(err, context.DeadlineExceeded) || called {
		t.Fatalf("cancelled writer applied: %v %v", called, err)
	}
	if _, err = s.guardedReconcileWrite(context.Background(), &expected, func(corpusWriter) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if reconcileState(t, s).Clean() {
		t.Fatal("cancellation acknowledged work")
	}
}

// Snapshot all durable semantic rows, not deletion counts. Observation times
// are normalized, while IDs, hashes, provenance and graph decisions are not.
func reconciliationSnapshot(t *testing.T, s *Store) map[string][]string {
	t.Helper()
	out := map[string][]string{}
	for _, table := range []string{"entries", "people", "identities", "participants", "sightings", "attachments", "mail_detail", "gmail_copies", "render_offsets", "person_merges", "entry_embeddings"} {
		rows, err := s.db.Query("select * from " + table)
		if err != nil {
			t.Fatal(err)
		}
		cols, err := rows.Columns()
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			values := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range values {
				ptrs[i] = &values[i]
			}
			if err := rows.Scan(ptrs...); err != nil {
				t.Fatal(err)
			}
			var fields []string
			for i, v := range values {
				if cols[i] == "ingested_at" || cols[i] == "measured_at" || cols[i] == "merged_at" {
					continue
				}
				fields = append(fields, fmt.Sprintf("%s=%v", cols[i], v))
			}
			out[table] = append(out[table], strings.Join(fields, "|"))
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		rows.Close()
	}
	for table := range out {
		sort.Strings(out[table])
	}
	return out
}

func fullReconcileRound(s *Store) error {
	if _, err := CollapseTwins(s, true); err != nil {
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

func TestReconcileFullOnePassAndStableStateParity(t *testing.T) {
	full := open(t)
	gated := open(t)
	seed := func(s *Store) {
		keep, _ := twinFixture(t, s)
		p := person(t, s, "author@example.test", "Synthetic Author")
		at := time.Unix(1700000000, 0)
		recovered(t, s, p, "triplicate", askQuoted, at.Add(12*time.Hour), keep)
		recovered(t, s, p, "annotated", strings.Replace(askBody, "The retailer", "Here are five invented words inserted inline. The retailer", 1), at.Add(9*time.Hour), keep)
		vetoBase := mailbox(t, s, p, "veto-base", askBody, at.Add(72*time.Hour))
		veto := recovered(t, s, p, "veto", askQuoted, at.Add(82*time.Hour), vetoBase)
		if _, err := s.db.Exec(`insert into attachments(entry_id,name) values(?,'keep-me.pdf')`, veto); err != nil {
			t.Fatal(err)
		}
		// URL destination changes are an independent identity gate.
		for i, dest := range []string{"https://one.example.test/", "https://two.example.test/"} {
			body := askBody + "\nVisit " + dest
			recovered(t, s, p, fmt.Sprint("link-", i), body, at.Add(time.Duration(144+i)*time.Hour), keep)
		}
	}
	seed(full)
	seed(gated)
	for round := 0; round < 3; round++ {
		if err := fullReconcileRound(full); err != nil {
			t.Fatal(err)
		}
		before := reconcileState(t, gated)
		if !before.Clean() {
			if err := fullReconcileRound(gated); err != nil {
				t.Fatal(err)
			}
			if _, err := gated.CheckpointReconciliation(context.Background(), before); err != nil {
				t.Fatal(err)
			}
		}
		if !reflect.DeepEqual(reconciliationSnapshot(t, full), reconciliationSnapshot(t, gated)) {
			t.Fatalf("durable state differs in round %d", round)
		}
		checkRepairIndexes(t, full)
		checkRepairIndexes(t, gated)
		if round == 0 && reconcileState(t, gated).Clean() {
			t.Fatal("maintenance output falsely certified in one pass")
		}
	}
	if !reconcileState(t, gated).Clean() {
		t.Fatal("stable verification never covered")
	}
	before := reconciliationSnapshot(t, gated)
	if _, err := CollapseTwins(gated, false); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, reconciliationSnapshot(t, gated)) {
		t.Fatal("full dry run wrote source")
	}
}

func BenchmarkReconcileClean(b *testing.B) {
	for _, size := range []int{0, 1000} {
		b.Run(fmt.Sprint(size), func(b *testing.B) {
			s, err := Open(filepath.Join(b.TempDir(), "bench.db"))
			if err != nil {
				b.Fatal(err)
			}
			defer s.Close()
			tx, err := s.db.Begin()
			if err != nil {
				b.Fatal(err)
			}
			for i := 0; i < size; i++ {
				if _, err := tx.Exec(`insert into entries(source,ext_id,ts,body_text,body_sha,ingested_at) values('mail',?,0,?,'synthetic',0)`, fmt.Sprint(i), askBody); err != nil {
					b.Fatal(err)
				}
			}
			if err := tx.Commit(); err != nil {
				b.Fatal(err)
			}
			r, err := s.ReconciliationState()
			if err != nil {
				b.Fatal(err)
			}
			if ok, err := s.CheckpointReconciliation(context.Background(), r); err != nil || !ok {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				r, err := s.ReconciliationState()
				if err != nil || !r.Clean() {
					b.Fatal(r, err)
				}
			}
			b.ReportMetric(0, "bodies/op")
			b.ReportMetric(1, "state-reads/op")
		})
	}
}

func TestReconcileGraphSeamAndDerivedRejectStalePlans(t *testing.T) {
	path := filepath.Join(t.TempDir(), "graph.db")
	s := fileFoldStore(t, path)
	other := fileFoldStore(t, path)
	keep, drop := twinFixture(t, s)
	if _, err := s.db.Exec(`update entries set parent_id=? where id=?`, drop, keep); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`update entries set parent_id=? where id=?`, keep, drop); err != nil {
		t.Fatal(err)
	}
	tx, err := s.db.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	state, err := readReconcileState(tx)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := planGraph(tx)
	if err != nil {
		t.Fatal(err)
	}
	tx.Rollback()
	if plan.Edges != 1 {
		t.Fatalf("plan=%+v", plan)
	}
	child := plan.Severed[0].Child
	// Replace the old ring edge with a different, valid parent after planning.
	p := person(t, other, "replacement@example.test", "Synthetic Replacement")
	replacement := host(t, other, p, "replacement", time.Unix(1600000000, 0))
	if _, err := other.db.Exec(`update entries set parent_id=? where id=?`, replacement, child); err != nil {
		t.Fatal(err)
	}
	if _, err := s.guardedReconcileWrite(context.Background(), &state.Source, func(w corpusWriter) error { return applyGraphPlan(w, plan) }); !errors.Is(err, ErrReconcileConflict) {
		t.Fatal("stale graph accepted", err)
	}
	if rep, err := s.RepairGraph(); err != nil || rep.Edges != 0 {
		t.Fatalf("new graph severed: %+v %v", rep, err)
	}
	var parent int64
	if err := s.db.QueryRow(`select parent_id from entries where id=?`, child).Scan(&parent); err != nil || parent != replacement {
		t.Fatal("replacement edge lost", err)
	}
	expected := reconcileState(t, s).Source
	if _, err := other.db.Exec(`update entries set derived=1 where id=?`, keep); err != nil {
		t.Fatal(err)
	}
	if _, err := s.guardedReconcileWrite(context.Background(), &expected, func(w corpusWriter) error { return persistDerived(w, drop, keep) }); !errors.Is(err, ErrReconcileConflict) {
		t.Fatal("stale derived accepted", err)
	}
	// A fresh derived write must check cycles inside the same writer.
	if _, err := other.db.Exec(`update entries set parent_id=null where id=?`, drop); err != nil {
		t.Fatal(err)
	}
	if _, err := other.db.Exec(`update entries set parent_id=? where id=?`, drop, keep); err != nil {
		t.Fatal(err)
	}
	expected = reconcileState(t, s).Source
	if _, err := s.guardedReconcileWrite(context.Background(), &expected, func(w corpusWriter) error { return persistDerived(w, drop, keep) }); err != nil {
		t.Fatal(err)
	}
	var derived int
	if err := s.db.QueryRow(`select derived from entries where id=?`, drop).Scan(&derived); err != nil || derived != 1 {
		t.Fatal("derived mark missing", err)
	}
	bad, err := closesCycle(s.db, drop, keep)
	if err != nil || !bad {
		t.Fatal("cycle fixture invalid", err)
	}
	if err := s.db.QueryRow(`select coalesce(parent_id,0) from entries where id=?`, drop).Scan(&parent); err != nil || parent == keep {
		t.Fatal("derived update closed cycle", err)
	}
}

func TestReconcileQuoteHostAndSightingGuards(t *testing.T) {
	for _, change := range []string{"host body", "new sighting", "removed sighting"} {
		t.Run(change, func(t *testing.T) {
			s, _, host, id, _ := foldStored(t)
			before, _ := bodyOf(t, s, id)
			// The same candidate snapshot the full repair uses, before expensive peeling.
			state := reconcileState(t, s)
			var c quotedCandidate
			err := s.db.QueryRow(`select g.seen_in,e.id,e.ext_id,coalesce(e.subject,''),coalesce(e.body_text,''),coalesce(h.body_text,'') from sightings g join entries e on e.id=g.entry_id join entries h on h.id=g.seen_in where e.id=? limit 1`, id).Scan(&c.host, &c.id, &c.ext, &c.subject, &c.body, &c.hostBody)
			if err != nil {
				t.Fatal(err)
			}
			switch change {
			case "host body":
				if _, err := s.db.Exec(`update entries set body_text='new evidence' where id=?`, host); err != nil {
					t.Fatal(err)
				}
			case "new sighting":
				addRepairHost(t, s, id, "competing", strings.Replace(foldForwardBody(), foldMessage, foldMessage+" Different competing text.", 1))
			case "removed sighting":
				if _, err := s.db.Exec(`delete from sightings where entry_id=?`, id); err != nil {
					t.Fatal(err)
				}
			}
			rep, err := repeelPassAt(s, []quotedCandidate{c}, &state.Source)
			if !errors.Is(err, ErrReconcileConflict) || rep.Fixed != 0 {
				t.Fatalf("stale host plan accepted: %+v %v", rep, err)
			}
			if after, _ := bodyOf(t, s, id); after != before {
				t.Fatal("stale repair rewrote quoted body")
			}
		})
	}
}

func TestReconcileExternalProcessWriter(t *testing.T) {
	if path := os.Getenv("CHAINMAIL_RECONCILE_TEST_DB"); path != "" {
		s, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		if _, err := s.db.Exec(`insert into attachments(entry_id,name) select id,'external process attachment' from entries where ext_id='quote:oracle-copy'`); err != nil {
			t.Fatal(err)
		}
		return
	}
	path := filepath.Join(t.TempDir(), "process.db")
	s := fileFoldStore(t, path)
	_, drop := twinFixture(t, s)
	plan, expected := snapshotTwinPlan(t, s)
	start := reconcileState(t, s)
	cmd := exec.Command(os.Args[0], "-test.run=^TestReconcileExternalProcessWriter$")
	cmd.Env = append(os.Environ(), "CHAINMAIL_RECONCILE_TEST_DB="+path)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("external writer: %s %v", output, err)
	}
	if _, err := s.guardedReconcileWrite(context.Background(), &expected, func(w corpusWriter) error { return collapseGroup(w, s, plan.Collapse[0]) }); !errors.Is(err, ErrReconcileConflict) {
		t.Fatal("external process not detected", err)
	}
	if ok, err := s.CheckpointReconciliation(context.Background(), start); err != nil || ok {
		t.Fatal("external revision lost", ok, err)
	}
	if _, err := CollapseTwins(s, true); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := s.db.QueryRow(`select count(*) from entries where id=?`, drop).Scan(&n); err != nil || n != 1 {
		t.Fatal("external attachment veto lost", err)
	}
	checkRepairIndexes(t, s)
}

func TestReconcileMetadataOnlyPutAndForcedFull(t *testing.T) {
	s := open(t)
	e := Entry{Source: SourceMail, ExtID: "mail:metadata", TS: time.Unix(1700000000, 0), Subject: "Synthetic subject", BodyText: askBody}
	first, err := s.Put(e, &Mail{MessageID: "metadata"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	checkpoint(t, s)
	repeat, err := s.Put(e, &Mail{MessageID: "metadata", Labels: []string{"UNREAD"}}, nil)
	if err != nil || repeat.Created || repeat.Changed || !reconcileState(t, s).Clean() {
		t.Fatal("identical put/label write invalidated", repeat, err)
	}
	e.TS = e.TS.Add(time.Hour)
	repeat, err = s.Put(e, &Mail{MessageID: "metadata", Labels: []string{"INBOX"}}, nil)
	if err != nil || repeat.Created || repeat.Changed || reconcileState(t, s).Clean() {
		t.Fatal("metadata-only put missed", repeat, err)
	}
	if repeat.ID != first.ID {
		t.Fatal("put changed logical entry")
	}
	twinFixture(t, s)
	checkpoint(t, s)
	before := reconciliationSnapshot(t, s)
	plan, err := CollapseTwins(s, false)
	if err != nil || plan.PeopleScanned == 0 || plan.BodiesScanned == 0 || len(plan.Collapse) == 0 {
		t.Fatal("forced full dry run incorrectly gated", plan, err)
	}
	if !reflect.DeepEqual(before, reconciliationSnapshot(t, s)) {
		t.Fatal("dry run changed source")
	}
	if !reconcileState(t, s).Clean() {
		t.Fatal("dry run consumed or invalidated checkpoint")
	}
}
