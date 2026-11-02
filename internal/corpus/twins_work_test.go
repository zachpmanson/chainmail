package corpus

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

func workState(t *testing.T, s *Store) TwinsWorkState {
	t.Helper()
	r, err := s.TwinsWorkState()
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// Trigger-only tests isolate one mutation without running destructive maintenance.
func clearTestWork(t *testing.T, s *Store) {
	t.Helper()
	if _, err := s.db.Exec(`delete from twins_dirty_people; update twins_work_state set ready=1,covered_version=3,rules_version=3,full_revision=null,full_reason=null`); err != nil {
		t.Fatal(err)
	}
}
func queuedPeople(t *testing.T, s *Store) []int64 {
	t.Helper()
	rows, err := s.db.Query(`select person_id from twins_dirty_people order by person_id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		out = append(out, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestTwinsWorkMutationMatrix(t *testing.T) {
	for _, tc := range []struct {
		name, sql string
		args      func(p, q, a, b, h int64) []any
		want      func(p, q int64) []int64
		full      bool
	}{
		{"body", `update entries set body_text='changed' where id=?`, func(p, q, a, b, h int64) []any { return []any{a} }, func(p, q int64) []int64 { return []int64{p} }, false},
		{"author", `update entries set person_id=? where id=?`, func(p, q, a, b, h int64) []any { return []any{q, a} }, func(p, q int64) []int64 { return []int64{p, q} }, false},
		{"null author", `update entries set person_id=null where id=?`, func(p, q, a, b, h int64) []any { return []any{a} }, func(p, q int64) []int64 { return []int64{p} }, false},
		{"source", `update entries set source='slack' where id=?`, func(p, q, a, b, h int64) []any { return []any{a} }, func(p, q int64) []int64 { return []int64{p} }, false},
		{"nonmail host time", `update entries set ts=ts+1 where id=?`, func(p, q, a, b, h int64) []any { return []any{h} }, func(p, q int64) []int64 { return []int64{p} }, false},
		{"nonmail host author", `update entries set person_id=? where id=?`, func(p, q, a, b, h int64) []any { return []any{p, h} }, func(p, q int64) []int64 { return []int64{p} }, false},
		{"parent set", `update entries set parent_id=? where id=?`, func(p, q, a, b, h int64) []any { return []any{h, a} }, func(p, q int64) []int64 { return []int64{p} }, true},
		{"delete", `delete from participants where entry_id=(select id from entries where ext_id='quote:matrix-other'); delete from sightings where entry_id=(select id from entries where ext_id='quote:matrix-other'); delete from entries where id=?`, func(p, q, a, b, h int64) []any { return []any{b} }, func(p, q int64) []int64 { return []int64{q} }, true},
		{"sighting remove", `delete from sightings where entry_id=?`, func(p, q, a, b, h int64) []any { return []any{a} }, func(p, q int64) []int64 { return []int64{p} }, false},
		{"sighting repoint", `update sightings set entry_id=? where entry_id=?`, func(p, q, a, b, h int64) []any { return []any{b, a} }, func(p, q int64) []int64 { return []int64{p, q} }, false},
		{"sighting host repoint", `update sightings set seen_in=? where entry_id=?`, func(p, q, a, b, h int64) []any { return []any{b, a} }, func(p, q int64) []int64 { return []int64{p, q} }, false},
		{"sighting kind", `update sightings set kind='forward' where entry_id=?`, func(p, q, a, b, h int64) []any { return []any{a} }, func(p, q int64) []int64 { return []int64{p} }, false},
		{"attachment insert", `insert into attachments(entry_id,name) values(?,'veto')`, func(p, q, a, b, h int64) []any { return []any{a} }, func(p, q int64) []int64 { return []int64{p} }, false},

		{"attachment repoint", "update attachments set entry_id=? where entry_id=?", func(p, q, a, b, h int64) []any { return []any{b, a} }, func(p, q int64) []int64 { return []int64{p, q} }, false},
		{"attachment remove", "delete from attachments where entry_id=?", func(p, q, a, b, h int64) []any { return []any{a} }, func(p, q int64) []int64 { return []int64{p} }, false},
		{"sighting insert", "insert into sightings(entry_id,seen_in,kind) values(?,?,'quote')", func(p, q, a, b, h int64) []any { return []any{a, b} }, func(p, q int64) []int64 { return []int64{p, q} }, false},
		{"new mail", "insert into entries(source,ext_id,person_id,ts,body_text,body_sha,ingested_at) values('mail','new-matrix',?,0,'New unrelated synthetic body','synthetic',0)", func(p, q, a, b, h int64) []any { return []any{q} }, func(p, q int64) []int64 { return []int64{q} }, false},
		{"new structural mail", "insert into entries(source,ext_id,person_id,parent_id,ts,body_text,body_sha,ingested_at) values('mail','new-matrix',?,?,0,'New unrelated synthetic body','synthetic',0)", func(p, q, a, b, h int64) []any { return []any{q, h} }, func(p, q int64) []int64 { return []int64{q} }, true},
		{"host becomes mail", "update entries set source='mail' where id=?", func(p, q, a, b, h int64) []any { return []any{h} }, func(p, q int64) []int64 { return []int64{p, q} }, false},
		{"author tombstone", "update entries set person_id=null where person_id=?; delete from identities where person_id=?; delete from participants where person_id=?; delete from people where id=?", func(p, q, a, b, h int64) []any { return []any{p, p, p, p} }, func(p, q int64) []int64 { return []int64{p} }, false},
		{"identity only", `insert into identities(person_id,kind,value) values(?,'slack_uid','matrix-id')`, func(p, q, a, b, h int64) []any { return []any{p} }, func(p, q int64) []int64 { return []int64{} }, false},
		{"new person", `insert into people(display_name) values('New synthetic sender')`, func(p, q, a, b, h int64) []any { return nil }, func(p, q int64) []int64 { return []int64{} }, false},
		{"name", `update people set display_name='Changed name' where id=?`, func(p, q, a, b, h int64) []any { return []any{p} }, func(p, q int64) []int64 { return []int64{} }, false},
		{"ignored", `update entries set parent_ref='ref',tz='UTC',ingested_at=42 where id=?`, func(p, q, a, b, h int64) []any { return []any{a} }, func(p, q int64) []int64 { return []int64{} }, false},
		{"no op", `update entries set person_id=person_id,body_text=body_text,parent_id=parent_id`, func(p, q, a, b, h int64) []any { return nil }, func(p, q int64) []int64 { return []int64{} }, false},
	} {
		for _, pending := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/pending=%t", tc.name, pending), func(t *testing.T) {
				s := open(t)
				p := person(t, s, "matrix1@example.test", "Matrix one")
				q := person(t, s, "matrix2@example.test", "Matrix two")
				at := time.Unix(1700000000, 0)
				hres, err := s.Put(Entry{Source: SourceSlack, ExtID: "matrix-host", PersonID: q, TS: at, BodyText: "Invented nonmail host"}, nil, nil)
				if err != nil {
					t.Fatal(err)
				}
				a := recovered(t, s, p, "matrix-copy", askQuoted, at, hres.ID)
				b := recovered(t, s, q, "matrix-other", otherQuoted, at, 0)
				if _, err := s.db.Exec("insert into attachments(entry_id,name) values(?,'existing veto')", a); err != nil {
					t.Fatal(err)
				}
				clearTestWork(t, s)
				if pending {
					if _, err := s.db.Exec("insert into twins_dirty_people values(-123,1); update twins_work_state set work_revision=work_revision+1,full_revision=work_revision+1"); err != nil {
						t.Fatal(err)
					}
				}
				captured, err := captureTwinWork(s.db)
				if err != nil {
					t.Fatal(err)
				}
				before := workState(t, s)
				source := reconcileState(t, s)
				tx, err := s.db.Begin()
				if err != nil {
					t.Fatal(err)
				}
				if _, err := tx.Exec(tc.sql, tc.args(p, q, a, b, hres.ID)...); err != nil {
					tx.Rollback()
					t.Fatal(err)
				}
				tx.Rollback()
				if workState(t, s) != before || reconcileState(t, s) != source {
					t.Fatal("rollback changed coverage")
				}
				if _, err := s.db.Exec(tc.sql, tc.args(p, q, a, b, hres.ID)...); err != nil {
					t.Fatal(err)
				}
				want := tc.want(p, q)
				sort.Slice(want, func(i, j int) bool { return want[i] < want[j] })
				after := workState(t, s)
				got := queuedPeople(t, s)
				changed := len(want) > 0
				if after.Full.Valid != (tc.full || pending) {
					t.Fatal("missing global coverage", after)
				}
				if pending {
					if !reflect.DeepEqual(got, []int64{-123}) {
						t.Fatal("full fanout changed retained tokens", got)
					}
				} else if !tc.full && !reflect.DeepEqual(got, want) {
					t.Fatalf("queue=%v want=%v state=%+v", got, want, after)
				}
				if changed {
					if after.Revision <= before.Revision {
						t.Fatal("counter not advanced")
					}
					if after.Full.Valid && after.Full.Int64 != after.Revision {
						t.Fatal("full does not cover omitted author work", after)
					}
				} else if after.Revision != before.Revision {
					t.Fatal("irrelevant/no-op input advanced work", before, after)
				}
				if pending {
					expected := reconcileState(t, s).Source
					if _, err := s.guardedReconcileWrite(context.Background(), &expected, func(w corpusWriter) error { return captured.acknowledge(w, true) }); err != nil {
						t.Fatal(err)
					}
					if workState(t, s).Full.Valid != changed {
						t.Fatal("exact full acknowledgment lost newer coverage", before, workState(t, s))
					}
					if _, err := ReconcileTwinsContext(context.Background(), s); err != nil {
						t.Fatal(err)
					}
					if !workState(t, s).Clean() {
						t.Fatal("eventual coverage did not settle", workState(t, s))
					}
				}
			})
		}
	}
}

func TestTwinsWorkOldEvidenceRollbackAndTombstones(t *testing.T) {
	s := open(t)
	keep, drop := twinFixture(t, s)
	var p int64
	if err := s.db.QueryRow(`select person_id from entries where id=?`, drop).Scan(&p); err != nil {
		t.Fatal(err)
	}
	clearTestWork(t, s)
	before := workState(t, s)
	tx, err := s.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`update entries set person_id=null where id=?`, drop); err != nil {
		t.Fatal(err)
	}
	tx.Rollback()
	if got := workState(t, s); got != before {
		t.Fatalf("rollback %+v != %+v", got, before)
	}
	if _, err := s.db.Exec(`insert into attachments(entry_id,name) values(?,'veto')`, drop); err != nil {
		t.Fatal(err)
	}
	clearTestWork(t, s)
	if _, err := s.db.Exec(`update attachments set entry_id=? where entry_id=?`, keep, drop); err != nil {
		t.Fatal(err)
	}
	if got := queuedPeople(t, s); !reflect.DeepEqual(got, []int64{p}) {
		t.Fatal(got)
	}
	clearTestWork(t, s)
	if _, err := s.db.Exec(`delete from attachments where entry_id=?`, keep); err != nil {
		t.Fatal(err)
	}
	if got := queuedPeople(t, s); !reflect.DeepEqual(got, []int64{p}) {
		t.Fatal(got)
	}
	clearTestWork(t, s)
	if _, err := s.db.Exec(`update entries set person_id=null where person_id=?; delete from identities where person_id=?; delete from participants where person_id=?; delete from people where id=?`, p, p, p, p); err != nil {
		t.Fatal(err)
	}
	if got := queuedPeople(t, s); !reflect.DeepEqual(got, []int64{p}) {
		t.Fatal("deleted author lost", got)
	}
	plan, err := ReconcileTwinsContext(context.Background(), s)
	if err != nil || plan.BodiesScanned != 0 || !workState(t, s).Clean() {
		t.Fatalf("tombstone %+v %v", plan, err)
	}
}

func TestTwinsWorkMigrationVersionsAndDryRun(t *testing.T) {
	path := filepath.Join(t.TempDir(), "work.db")
	s := fileFoldStore(t, path)
	if workState(t, s).Clean() {
		t.Fatal("initial full missing")
	}
	twinFixture(t, s)
	before := workState(t, s)
	tokens := queuedPeople(t, s)
	source := reconcileState(t, s)
	if _, err := CollapseTwins(s, false); err != nil {
		t.Fatal(err)
	}
	if before != workState(t, s) || source != reconcileState(t, s) || !reflect.DeepEqual(tokens, queuedPeople(t, s)) {
		t.Fatal("dry run consumed work")
	}
	plan, err := ReconcileTwinsContext(context.Background(), s)
	if err != nil || plan.Mode != "full" || plan.Removed != 1 || workState(t, s).Clean() {
		t.Fatalf("initial %+v %v", plan, err)
	}
	// Own deletion/graph/provenance writes must survive acknowledgment.
	if _, err := ReconcileTwinsContext(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	if !workState(t, fileFoldStore(t, path)).Clean() {
		t.Fatal("restart lost settled state")
	}
	if _, err := s.db.Exec(`update twins_work_state set covered_version=2`); err != nil {
		t.Fatal(err)
	}
	plan, err = ReconcileTwinsContext(context.Background(), s)
	if err != nil || plan.Fallback != "version" || !workState(t, s).Clean() {
		t.Fatalf("upgrade %+v %v", plan, err)
	}
	if _, err := s.db.Exec(`update twins_work_state set rules_version=4`); err != nil {
		t.Fatal(err)
	}
	if _, err := ReconcileTwinsContext(context.Background(), s); err == nil {
		t.Fatal("newer state accepted")
	}
	for _, apply := range []bool{false, true} {
		if _, err := CollapseTwins(s, apply); err == nil {
			t.Fatal("forced full accepted newer work rules")
		}
	}
	if _, err := s.db.Exec(`delete from twins_work_state`); err != nil {
		t.Fatal(err)
	}
	if workState(t, s).Clean() {
		t.Fatal("missing state accepted")
	}
}

func TestTwinsWorkAcknowledgmentRaces(t *testing.T) {
	for _, where := range []string{"before-ack", "after-ack", "full-epoch", "self", "exhaust"} {
		t.Run(where, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "ack.db")
			s := fileFoldStore(t, path)
			other := fileFoldStore(t, path)
			p := person(t, s, "ack@example.test", "Ack author")
			at := time.Unix(1700000000, 0)
			a := recovered(t, s, p, "negative", askQuoted, at, 0)
			if _, err := ReconcileTwinsContext(context.Background(), s); err != nil {
				t.Fatal(err)
			}
			if _, err := other.db.Exec(`update entries set subject='dirty' where id=?`, a); err != nil {
				t.Fatal(err)
			}
			if where == "full-epoch" {
				if _, err := other.db.Exec(`update entries set parent_id=? where id=?`, a, a); err != nil {
					t.Fatal(err)
				}
			}
			calls := 0
			plan, err := runTwinsContext(context.Background(), s, true, true, func(ctx context.Context, expected *int64, apply func(corpusWriter) error) (time.Duration, error) {
				calls++
				mutate := func() {
					query := `update entries set ts=ts+1 where id=?`
					if where == "full-epoch" {
						query = `update entries set parent_id=null where id=?`
					}
					if _, err := other.db.Exec(query, a); err != nil {
						t.Fatal(err)
					}
				}
				if where == "exhaust" || (calls == 1 && (where == "before-ack" || where == "full-epoch")) {
					mutate()
				}
				wait, err := s.guardedReconcileWrite(ctx, expected, apply)
				if calls == 1 && err == nil && (where == "after-ack" || where == "self") {
					mutate()
				}
				return wait, err
			})
			if where == "exhaust" {
				if !errors.Is(err, ErrReconcileConflict) || calls != 3 || plan.Retries != 3 || workState(t, s).Clean() {
					t.Fatalf("%+v %v calls=%d", plan, err, calls)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if where == "before-ack" || where == "full-epoch" {
					if plan.Retries != 1 || calls != 2 || !workState(t, s).Clean() {
						t.Fatalf("%+v calls=%d", plan, calls)
					}
				} else if workState(t, s).Clean() {
					t.Fatal("post ack enqueue lost")
				}
			}
		})
	}
}

func TestTwinsWorkQueryPlans(t *testing.T) {
	s := open(t)
	for _, tc := range []struct{ query, index string }{
		{`select entry_id from sightings where seen_in=123`, "sightings_seen_in"},
		{`select e.person_id from sightings s join entries e on e.id=s.entry_id where e.source='mail' and s.seen_in in (123,456)`, "sightings_seen_in"},
		{`select exists(select 1 from entries where parent_id=123)`, "entries_parent"},
		{`select id from entries where person_id=123 and source='mail' order by ts,id`, "entries_person"},
		{`select count(*) from attachments where entry_id=123`, "attachments_entry"},
	} {
		rows, err := s.db.Query("explain query plan " + tc.query)
		if err != nil {
			t.Fatal(err)
		}
		var details []string
		for rows.Next() {
			var a, b, c int
			var detail string
			if err := rows.Scan(&a, &b, &c, &detail); err != nil {
				t.Fatal(err)
			}
			details = append(details, detail)
		}
		rows.Close()
		joined := strings.Join(details, ";")
		t.Log(joined)
		if !strings.Contains(joined, tc.index) || strings.Contains(joined, "SCAN entries") || strings.Contains(joined, "SCAN sightings") {
			t.Fatal(joined)
		}
	}
}

func TestTwinsWorkExplicitPlannerScope(t *testing.T) {
	s := open(t)
	fixture := retryFixture(t, s)
	var p int64
	if err := s.db.QueryRow(`select person_id from entries where id=?`, fixture.Collapse[1].Keep).Scan(&p); err != nil {
		t.Fatal(err)
	}
	for _, ids := range [][]int64{nil, {}, {-12345}, {p, p, -12345}} {
		plan, err := planTwinsPeople(s.db, ids)
		if err != nil {
			t.Fatal(err)
		}
		if len(ids) < 2 {
			if plan.PeopleScanned != 0 || plan.BodiesScanned != 0 {
				t.Fatalf("empty scope scanned %+v", plan)
			}
		} else if plan.PeopleScanned != 1 || plan.BodiesScanned != 2 || !reflect.DeepEqual(plan.Collapse, fixture.Collapse[1:2]) {
			t.Fatalf("scope %+v", plan)
		}
	}
}

func TestTwinsWorkTrueV27Upgrade(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v27.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`create table schema_version(v integer primary key)`); err != nil {
		t.Fatal(err)
	}
	for i, migration := range migrations[:27] {
		if _, err := db.Exec(migration); err != nil {
			t.Fatalf("v%d: %v", i+1, err)
		}
		if _, err := db.Exec(`insert into schema_version values(?)`, i+1); err != nil {
			t.Fatal(err)
		}
	}
	old := &Store{db: db}
	p, err := Resolve(old, KindEmail, "historical@example.test", "Historical synthetic sender")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := old.PutQuoted(Entry{Source: SourceMail, ExtID: "old-quote", PersonID: p, TS: time.Unix(1700000000, 0), BodyText: "Historical synthetic quote"}); err != nil {
		t.Fatal(err)
	}
	db.Close()
	s := fileFoldStore(t, path)
	state := workState(t, s)
	if state.Ready || !state.Full.Valid || len(queuedPeople(t, s)) != 0 {
		t.Fatalf("migration scanned or covered history %+v", state)
	}
	plan, err := ReconcileTwinsContext(context.Background(), s)
	if err != nil || plan.Mode != "full" || plan.PeopleScanned != 1 || plan.BodiesScanned != 1 || !workState(t, s).Clean() {
		t.Fatalf("upgrade %+v %v", plan, err)
	}
	var version int
	if err := s.db.QueryRow(`select max(v) from schema_version`).Scan(&version); err != nil || version != 28 {
		t.Fatalf("version %d %v", version, err)
	}
	checkRepairIndexes(t, s)
}

func TestTwinsWorkExactTokenAndEpochAcknowledgment(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token.db")
	s := fileFoldStore(t, path)
	other := fileFoldStore(t, path)
	p := person(t, s, "token@example.test", "Synthetic token author")
	id := recovered(t, s, p, "token-quote", otherBody, time.Unix(1700000000, 0), 0)
	if _, err := ReconcileTwinsContext(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`update entries set ts=ts+1 where id=?`, id); err != nil {
		t.Fatal(err)
	}
	calls := 0
	plan, err := runTwinsContext(context.Background(), s, true, true, func(ctx context.Context, expected *int64, apply func(corpusWriter) error) (time.Duration, error) {
		calls++
		if calls == 1 {
			// Isolate work-only invalidation from the independent source guard.
			if _, err := other.db.Exec(`update twins_work_state set work_revision=work_revision+1,full_revision=work_revision+1,full_reason=null; update twins_dirty_people set revision=(select work_revision from twins_work_state)`); err != nil {
				t.Fatal(err)
			}
		}
		return s.guardedReconcileWrite(ctx, expected, apply)
	})
	if err != nil || plan.Retries != 0 || calls != 1 {
		t.Fatalf("%+v %v", plan, err)
	}
	state := workState(t, s)
	if state.Clean() || !state.Full.Valid || state.PendingAuthors != 1 {
		t.Fatal("concurrent token or full epoch lost", state)
	}
	next, err := ReconcileTwinsContext(context.Background(), s)
	if err != nil || next.Mode != "full" || next.Fallback != "pending-full" || !workState(t, s).Clean() {
		t.Fatalf("reasonless full skipped %+v %v", next, err)
	}
}

func TestTwinsWorkEveryParentEdgeAndEntryIDFallsBack(t *testing.T) {
	for _, source := range []string{SourceMail, SourceSlack} {
		t.Run(source, func(t *testing.T) {
			s := open(t)
			at := time.Unix(1700000000, 0)
			ids := []int64{}
			for i := 0; i < 3; i++ {
				res, err := s.Put(Entry{Source: source, ExtID: fmt.Sprintf("graph-%d", i), TS: at, BodyText: "Synthetic graph-only input"}, nil, nil)
				if err != nil {
					t.Fatal(err)
				}
				ids = append(ids, res.ID)
			}
			for _, parent := range []any{ids[1], ids[2], nil} {
				clearTestWork(t, s)
				if _, err := s.db.Exec(`update entries set parent_id=? where id=?`, parent, ids[0]); err != nil {
					t.Fatal(err)
				}
				state := workState(t, s)
				if !state.Full.Valid || state.Reason != "graph-edge" {
					t.Fatalf("all-source edge not full %+v", state)
				}
			}
			clearTestWork(t, s)
			if _, err := s.db.Exec(`update entries set id=id+1000 where id=?`, ids[0]); err != nil {
				t.Fatal(err)
			}
			state := workState(t, s)
			if !state.Full.Valid || state.Reason != "entry-id" {
				t.Fatalf("id mutation not full %+v", state)
			}
		})
	}
}

func TestTwinsForcedFullStillExhaustiveWhenBothClean(t *testing.T) {
	s := open(t)
	p := person(t, s, "forced-clean@example.test", "Synthetic forced-full author")
	recovered(t, s, p, "negative-clean", otherBody, time.Unix(1700000000, 0), 0)
	checkpoint(t, s)
	source, work := reconcileState(t, s), workState(t, s)
	if !source.Clean() || !work.Clean() {
		t.Fatal(source, work)
	}
	for _, apply := range []bool{false, true} {
		plan, err := CollapseTwins(s, apply)
		if err != nil || plan.PeopleScanned != 1 || plan.BodiesScanned != 1 || plan.Mode != "full" {
			t.Fatal(plan, err)
		}
		if reconcileState(t, s) != source || workState(t, s) != work {
			t.Fatal("standalone full certified or consumed work")
		}
	}
}
