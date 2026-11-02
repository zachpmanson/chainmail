package main

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zachpmanson/chainmail/internal/corpus"
	"github.com/zachpmanson/chainmail/internal/mailingest"
)

func gateStore(t *testing.T) (string, *corpus.Store) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "slurp.db")
	s, err := corpus.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return path, s
}

func TestSlurpDurableCleanGateAndRestart(t *testing.T) {
	path, s := gateStore(t)
	o := slurpOpts{only: []string{"twins", "repair"}}
	d := defaultSlurpDeps(path, o)
	twins, repair := 0, 0
	fullTwins, fullRepair := d.pendingTwins, d.repair
	d.pendingTwins = func() error { twins++; return fullTwins() }
	d.repair = func() error { repair++; return fullRepair() }
	d.status = func() error { return nil }
	var output strings.Builder
	if err := runSlurp(&output, o, d); err != nil {
		t.Fatal(err)
	}
	state, err := s.ReconciliationState()
	if err != nil || !state.Clean() {
		t.Fatal("verification did not cover", state, err)
	}
	for i := 0; i < 3; i++ {
		// default dependencies open a new Store each time; no in-memory checkpoint.
		if err := runSlurp(&output, o, d); err != nil {
			t.Fatal(err)
		}
	}
	if twins != 1 || repair != 1 {
		t.Fatalf("clean refresh rescanned twins=%d repair=%d", twins, repair)
	}
	if !strings.Contains(output.String(), "people=0 bodies=0") || !strings.Contains(output.String(), "hosts=0") {
		t.Fatal("missing zero-work evidence", output.String())
	}
	if _, err := s.DB().Exec(`insert into people(display_name) values('New synthetic identity')`); err != nil {
		t.Fatal(err)
	}
	if err := runSlurp(&output, o, d); err != nil {
		t.Fatal(err)
	}
	if twins != 2 || repair != 2 {
		t.Fatal("durable relevant write did not run full pair")
	}
}

func TestSlurpQuoteParserUpgradeInvalidatesOldCheckpoint(t *testing.T) {
	path, s := gateStore(t)
	if _, err := s.DB().Exec(`insert into people(display_name) values('Ada Example')`); err != nil {
		t.Fatal(err)
	}
	// Version 1 covered these exact source rows before the opener-boundary fix.
	if _, err := s.DB().Exec(`update reconcile_state set covered_revision=source_revision, rules_version=1, covered_version=1, ready=1`); err != nil {
		t.Fatal(err)
	}
	before, err := s.ReconciliationState()
	if err != nil || before.Source != before.Covered || !before.Ready || before.Clean() {
		t.Fatal("old parser checkpoint must require catch-up", before, err)
	}
	o := slurpOpts{only: []string{"twins", "repair"}}
	d := defaultSlurpDeps(path, o)
	twins, repair := 0, 0
	fullTwins, fullRepair := d.pendingTwins, d.repair
	d.pendingTwins = func() error { twins++; return fullTwins() }
	d.repair = func() error { repair++; return fullRepair() }
	d.status = func() error { return nil }
	var output strings.Builder
	if err := runSlurp(&output, o, d); err != nil {
		t.Fatal(err)
	}
	after, err := s.ReconciliationState()
	if err != nil || !after.Clean() || after.Source != before.Source || after.Rules <= before.Rules || twins != 1 || repair != 1 {
		t.Fatal("upgrade did not run and verify the full pair", after, twins, repair, err)
	}
	output.Reset()
	if err := runSlurp(&output, o, d); err != nil {
		t.Fatal(err)
	}
	if twins != 1 || repair != 1 || !strings.Contains(output.String(), "people=0 bodies=0") || !strings.Contains(output.String(), "hosts=0") {
		t.Fatal("verified upgraded corpus did not skip cleanly", twins, repair, output.String())
	}
}

func TestSlurpConcurrentMaintenanceInputsStayPending(t *testing.T) {
	for _, where := range []string{"clean-read", "clean-status", "checkpoint-status"} {
		t.Run(where, func(t *testing.T) {
			path, s := gateStore(t)
			other, err := corpus.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer other.Close()
			if where != "checkpoint-status" {
				if _, err := corpus.ReconcileTwinsContext(context.Background(), s); err != nil {
					t.Fatal(err)
				}
				state, err := s.ReconciliationState()
				if err != nil {
					t.Fatal(err)
				}
				if ok, err := s.CheckpointReconciliation(context.Background(), state); err != nil || !ok {
					t.Fatal("initial checkpoint", ok, err)
				}
			}
			o := slurpOpts{only: []string{"twins", "repair"}}
			d := defaultSlurpDeps(path, o)
			twins, repair, reads := 0, 0, 0
			fullTwins, fullRepair := d.pendingTwins, d.repair
			d.pendingTwins = func() error { twins++; return fullTwins() }
			d.repair = func() error { repair++; return fullRepair() }
			mutated, statusDone := false, false
			mutate := func() error {
				if mutated {
					return nil
				}
				mutated = true
				_, err := other.DB().Exec(`insert into people(display_name) values('Concurrent synthetic identity')`)
				return err
			}
			d.maintenanceState = func() (corpus.MaintenanceState, error) {
				state, err := s.MaintenanceState()
				reads++
				if reads == 1 && where == "clean-read" && err == nil {
					if !state.Clean() {
						t.Fatal("expected captured clean state", state)
					}
					err = mutate()
				}
				if reads == 2 && !statusDone {
					t.Error("final state read preceded status tail")
				}
				return state, err
			}
			d.status = func() error {
				statusDone = true
				if where != "clean-read" {
					return mutate()
				}
				return nil
			}
			var output strings.Builder
			if err := runSlurp(&output, o, d); err != nil {
				t.Fatal(err)
			}
			state, err := s.ReconciliationState()
			if err != nil || state.Clean() || state.Source == state.Covered {
				t.Fatal("concurrent invalidation lost", state, err)
			}
			initialCalls := 0
			if where == "checkpoint-status" {
				initialCalls = 1
			}
			if reads != 2 || twins != initialCalls || repair != initialCalls {
				t.Fatal("unexpected fast-path work", reads, twins, repair)
			}
			if !strings.Contains(output.String(), "3 phases: 2 complete, 1 incomplete, 0 skipped, 0 failed") ||
				!strings.Contains(output.String(), "verification pending") ||
				!strings.Contains(output.String(), "slurp timing: phase=total outcome=INCOMPLETE") {
				t.Fatal("missing pending summary", output.String())
			}
			output.Reset()
			if err := runSlurp(&output, o, d); err != nil {
				t.Fatal(err)
			}
			state, err = s.ReconciliationState()
			if err != nil || !state.Clean() || twins != initialCalls+1 || repair != initialCalls+1 {
				t.Fatal("subsequent full pair did not cover mutation", state, twins, repair, err)
			}
		})
	}
}

func TestSlurpFinalReconciliationStateReadFailure(t *testing.T) {
	for _, which := range []string{"clean", "pending", "failed"} {
		t.Run(which, func(t *testing.T) {
			path, s := gateStore(t)
			o := slurpOpts{only: []string{"twins", "repair"}}
			d := defaultSlurpDeps(path, o)
			if which == "clean" {
				if _, err := corpus.ReconcileTwinsContext(context.Background(), s); err != nil {
					t.Fatal(err)
				}
				state, err := s.ReconciliationState()
				if err != nil {
					t.Fatal(err)
				}
				if ok, err := s.CheckpointReconciliation(context.Background(), state); err != nil || !ok {
					t.Fatal("initial checkpoint", ok, err)
				}
			}
			d.twins = func(bool) error { return nil }
			d.repair = func() error {
				if which == "failed" {
					return errors.New("synthetic repair failure")
				}
				return nil
			}
			d.checkpointReconciliation = func(corpus.ReconcileState) (bool, error) { return false, nil }
			statusDone := false
			d.status = func() error { statusDone = true; return nil }
			reads := 0
			d.maintenanceState = func() (corpus.MaintenanceState, error) {
				reads++
				if statusDone {
					return corpus.MaintenanceState{}, errors.New("synthetic final read failure")
				}
				return s.MaintenanceState()
			}
			var output strings.Builder
			if err := runSlurp(&output, o, d); err == nil {
				t.Fatal("final read failure hidden")
			}
			if reads != 2 || !strings.Contains(output.String(), "synthetic final read failure") ||
				!strings.Contains(output.String(), "3 phases: 2 complete, 0 incomplete, 0 skipped, 1 failed") {
				t.Fatal("read failure duplicated or omitted", reads, output.String())
			}
			if which == "failed" && !strings.Contains(output.String(), "synthetic repair failure") {
				t.Fatal("original repair failure lost", output.String())
			}
		})
	}
}

func TestSlurpRepairCreatedWorkAndCheckpointRaceStayPending(t *testing.T) {
	for _, where := range []string{"repair", "checkpoint"} {
		t.Run(where, func(t *testing.T) {
			path, s := gateStore(t)
			o := slurpOpts{only: []string{"twins", "repair"}}
			d := defaultSlurpDeps(path, o)
			d.status = func() error { return nil }
			twins, repair := 0, 0
			pending := d.pendingTwins
			d.pendingTwins = func() error { twins++; return pending() }
			d.repair = func() error {
				repair++
				if where == "repair" && repair == 1 {
					_, err := s.DB().Exec(`insert into people(display_name) values('Repair-created synthetic evidence')`)
					return err
				}
				return nil
			}
			ack := d.checkpointReconciliation
			first := true
			d.checkpointReconciliation = func(start corpus.ReconcileState) (bool, error) {
				if where == "checkpoint" && first {
					first = false
					if _, err := s.DB().Exec(`insert into people(display_name) values('Competing synthetic evidence')`); err != nil {
						return false, err
					}
				}
				return ack(start)
			}
			var output strings.Builder
			if err := runSlurp(&output, o, d); err != nil {
				t.Fatal(err)
			}
			state, err := s.ReconciliationState()
			if err != nil || state.Clean() {
				t.Fatal("changed round falsely covered", state, err)
			}
			if !strings.Contains(output.String(), "verification pending") ||
				!strings.Contains(output.String(), "3 phases: 2 complete, 1 incomplete, 0 skipped, 0 failed") {
				t.Fatal(output.String())
			}
			if err := runSlurp(&output, o, d); err != nil {
				t.Fatal(err)
			}
			state, err = s.ReconciliationState()
			if err != nil || !state.Clean() || twins != 2 || repair != 2 {
				t.Fatal("next verification failed", state, twins, repair, err)
			}
		})
	}
}

func TestSlurpPartialAndFailedMaintenanceCannotCheckpoint(t *testing.T) {
	for _, which := range []string{"twins-only", "repair-only", "twins-fail", "repair-fail", "state-fail"} {
		t.Run(which, func(t *testing.T) {
			path, s := gateStore(t)
			o := slurpOpts{only: []string{"twins", "repair"}}
			if which == "twins-only" {
				o.only = []string{"twins"}
			}
			if which == "repair-only" {
				o.only = []string{"repair"}
			}
			d := defaultSlurpDeps(path, o)
			d.status = func() error { return nil }
			d.twins = func(bool) error {
				if which == "twins-fail" {
					return errors.New("synthetic twins failure")
				}
				return nil
			}
			d.pendingTwins = func() error { return d.twins(true) }
			d.repair = func() error {
				if which == "repair-fail" {
					return errors.New("synthetic repair failure")
				}
				return nil
			}
			if which == "state-fail" {
				d.maintenanceState = func() (corpus.MaintenanceState, error) {
					return corpus.MaintenanceState{}, errors.New("synthetic unsupported version")
				}
			}
			if which == "twins-only" || which == "repair-only" {
				d.maintenanceState = func() (corpus.MaintenanceState, error) {
					t.Fatal("partial selection read pair state")
					return corpus.MaintenanceState{}, nil
				}
			}
			ackCalls := 0
			d.checkpointReconciliation = func(corpus.ReconcileState) (bool, error) { ackCalls++; return true, nil }
			var output strings.Builder
			err := runSlurp(&output, o, d)
			if strings.HasSuffix(which, "fail") && err == nil {
				t.Fatal("failure hidden")
			}
			state, e := s.ReconciliationState()
			if e != nil || state.Clean() || ackCalls != 0 {
				t.Fatal("partial maintenance acknowledged", state, ackCalls, e)
			}
		})
	}
}

func TestSlurpLabelOnlyAndFailedIngestUseDurableState(t *testing.T) {
	path, s := gateStore(t)
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
	o := slurpOpts{since: "2026-01-01", only: []string{"mail", "twins", "repair"}}
	r := &recorder{mail: covered}
	d := r.deps()
	real := defaultSlurpDeps(path, o)
	d.maintenanceState = real.maintenanceState
	d.checkpointReconciliation = real.checkpointReconciliation
	var output strings.Builder
	if err := runSlurp(&output, o, d); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(r.calls, ","), "twins") {
		t.Fatal("zero ingest counters should not force scanning clean state")
	}
	// A failed producer may already have committed relevant changes. Its counters
	// cannot decide whether maintenance is needed.
	r.calls = nil
	oldIngest := d.ingestMail
	d.ingestMail = func(m mailOpts) (res mailingest.Result, err error) {
		oldIngest(m)
		_, err = s.DB().Exec(`insert into people(display_name) values('Partially ingested synthetic identity')`)
		if err != nil {
			return res, err
		}
		return res, errors.New("synthetic partial ingest")
	}
	if err := runSlurp(&output, o, d); err == nil {
		t.Fatal("ingest failure hidden")
	}
	calls := strings.Join(r.calls, ",")
	if !strings.Contains(calls, "twins,repair") {
		t.Fatal("committed work bypassed after ingest failure", calls)
	}
}

func TestSlurpPairCheckpointCannotSkipPendingTwins(t *testing.T) {
	path, s := gateStore(t)
	p, err := corpus.Resolve(s, corpus.KindEmail, "pending-pair@example.test", "Synthetic pending author")
	if err != nil {
		t.Fatal(err)
	}
	at := time.Unix(1700000000, 0)
	if _, err := s.Put(corpus.Entry{Source: corpus.SourceMail, ExtID: "pending-mail", PersonID: p, TS: at, BodyText: strings.Repeat("Independent invented body for pending duplicate evidence. ", 10)}, &corpus.Mail{MessageID: "pending-mail"}, nil); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.PutQuoted(corpus.Entry{Source: corpus.SourceMail, ExtID: "pending-quote", PersonID: p, TS: at.Add(time.Hour), BodyText: strings.Repeat("Independent invented body for pending duplicate evidence. ", 10)}); err != nil {
		t.Fatal(err)
	}
	// Strict checkpoint must refuse to certify unconsumed work.
	start, err := s.ReconciliationState()
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := s.CheckpointReconciliation(context.Background(), start); err != nil || ok {
		t.Fatal(ok, err)
	}
	o := slurpOpts{only: []string{"twins", "repair"}}
	d := defaultSlurpDeps(path, o)
	d.status = func() error { return nil }
	called := 0
	pending := d.pendingTwins
	d.pendingTwins = func() error { called++; return pending() }
	var output strings.Builder
	if err := runSlurp(&output, o, d); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := s.DB().QueryRow(`select count(*) from entries`).Scan(&count); err != nil || count != 1 || called != 1 {
		t.Fatalf("pending twins skipped count=%d called=%d %v", count, called, err)
	}
	if !strings.Contains(output.String(), "incomplete") {
		t.Fatal("self invalidation falsely certified", output.String())
	}
	if err := runSlurp(&output, o, d); err != nil {
		t.Fatal(err)
	}
	source, err := s.ReconciliationState()
	if err != nil {
		t.Fatal(err)
	}
	work, err := s.TwinsWorkState()
	if err != nil || !work.Clean() || !source.Clean() {
		t.Fatal("verification did not settle both states", source, work, err)
	}
}

func TestSlurpTwinsWorkReadFailureIsNotComplete(t *testing.T) {
	for _, tail := range []bool{false, true} {
		t.Run(fmt.Sprint(tail), func(t *testing.T) {
			path, _ := gateStore(t)
			o := slurpOpts{only: []string{"twins", "repair"}}
			d := defaultSlurpDeps(path, o)
			statusDone := false
			d.status = func() error { statusDone = true; return nil }
			state := d.maintenanceState
			d.maintenanceState = func() (corpus.MaintenanceState, error) {
				if !tail || statusDone {
					return corpus.MaintenanceState{}, errors.New("invented twins state read failure")
				}
				return state()
			}
			var output strings.Builder
			if err := runSlurp(&output, o, d); err == nil || !strings.Contains(output.String(), "invented twins state read failure") {
				t.Fatal("work-state failure hidden", err, output.String())
			}
		})
	}
}

func TestSlurpFailedProducerCommittedTwinWorkUsesCoordinator(t *testing.T) {
	path, s := gateStore(t)
	o := slurpOpts{since: "2026-01-01", only: []string{"mail", "twins", "repair"}}
	d := defaultSlurpDeps(path, o)
	d.status = func() error { return nil }
	d.mailAccounts = func() ([]string, error) { return []string{"synthetic"}, nil }
	d.ingestMail = func(mailOpts) (mailingest.Result, error) {
		p, err := corpus.Resolve(s, corpus.KindEmail, "partial-author@example.test", "Synthetic partial author")
		if err != nil {
			return mailingest.Result{}, err
		}
		_, _, err = s.PutQuoted(corpus.Entry{Source: corpus.SourceMail, ExtID: "partial-committed-quote", PersonID: p, TS: time.Unix(1700000000, 0), BodyText: "Independent committed synthetic quote from an interrupted ingest."})
		if err != nil {
			return mailingest.Result{}, err
		}
		return mailingest.Result{}, errors.New("invented producer interruption after commit")
	}
	called := 0
	pending := d.pendingTwins
	d.pendingTwins = func() error { called++; return pending() }
	var output strings.Builder
	if err := runSlurp(&output, o, d); err == nil || !strings.Contains(output.String(), "invented producer interruption after commit") {
		t.Fatal("partial producer failure hidden", err, output.String())
	}
	work, err := s.TwinsWorkState()
	if err != nil || !work.Clean() || called != 1 {
		t.Fatal("committed work bypassed", work, called, err)
	}
}

func TestSlurpTornDualStateNeverReportsComplete(t *testing.T) {
	path, s := gateStore(t)
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
	other, err := corpus.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	o := slurpOpts{only: []string{"twins", "repair"}}
	d := defaultSlurpDeps(path, o)
	tail := false
	d.status = func() error {
		tail = true
		_, err := other.DB().Exec(`update twins_work_state set work_revision=work_revision+1,full_revision=work_revision+1`)
		return err
	}
	read := d.maintenanceState
	d.maintenanceState = func() (corpus.MaintenanceState, error) {
		state, err := read()
		if err == nil && tail {
			if _, err = other.DB().Exec(`insert into people(display_name) values('Identity-only concurrent edit')`); err == nil {
				_, err = corpus.ReconcileTwinsContext(context.Background(), other)
			}
		}
		return state, err
	}
	var out strings.Builder
	if err := runSlurp(&out, o, d); err != nil {
		t.Fatal(err)
	}
	source, err := s.ReconciliationState()
	if err != nil || source.Clean() {
		t.Fatal(source, err)
	}
	work, err := s.TwinsWorkState()
	if err != nil || !work.Clean() {
		t.Fatal(work, err)
	}
	if !strings.Contains(out.String(), "phase=total outcome=INCOMPLETE") {
		t.Fatal("torn reads reported complete", out.String())
	}
	for _, phase := range []string{"twins", "repair", "status"} {
		if strings.Count(out.String(), "slurp timing: phase="+phase+" outcome=started") != 1 || strings.Count(out.String(), "slurp timing: phase="+phase+" outcome=") != 2 {
			t.Fatal("duplicated or missing phase timing", out.String())
		}
	}
	d.maintenanceState = read
	d.status = func() error { return nil }
	out.Reset()
	if err := runSlurp(&out, o, d); err != nil {
		t.Fatal(err)
	}
	final, err := s.MaintenanceState()
	if err != nil || !final.Clean() {
		t.Fatal(final, err)
	}
}

func TestSlurpWorkOnlyCheckpointGapStaysPending(t *testing.T) {
	path, s := gateStore(t)
	o := slurpOpts{only: []string{"twins", "repair"}}
	d := defaultSlurpDeps(path, o)
	d.status = func() error { return nil }
	ack := d.checkpointReconciliation
	d.checkpointReconciliation = func(start corpus.ReconcileState) (bool, error) {
		state, err := s.MaintenanceState()
		if err != nil || !state.Twins.Clean() {
			t.Fatal(state, err)
		}
		if _, err := s.DB().Exec(`update twins_work_state set work_revision=work_revision+1,full_revision=work_revision+1`); err != nil {
			t.Fatal(err)
		}
		return ack(start)
	}
	var out strings.Builder
	if err := runSlurp(&out, o, d); err != nil {
		t.Fatal(err)
	}
	state, err := s.MaintenanceState()
	if err != nil || state.Source.Clean() || !state.Twins.Full.Valid {
		t.Fatal(state, err)
	}
	if !strings.Contains(out.String(), "3 phases: 2 complete, 1 incomplete, 0 skipped, 0 failed") {
		t.Fatal(out.String())
	}
	d.checkpointReconciliation = ack
	if err := runSlurp(&out, o, d); err != nil {
		t.Fatal(err)
	}
	state, err = s.MaintenanceState()
	if err != nil || !state.Clean() {
		t.Fatal(state, err)
	}
}

func TestSlurpInputsAfterCombinedSnapshotBelongToNextReport(t *testing.T) {
	path, s := gateStore(t)
	o := slurpOpts{only: []string{"twins", "repair"}}
	d := defaultSlurpDeps(path, o)
	d.status = func() error { return nil }
	var out strings.Builder
	if err := runSlurp(&out, o, d); err != nil {
		t.Fatal(err)
	}
	read, reads := d.maintenanceState, 0
	d.maintenanceState = func() (corpus.MaintenanceState, error) {
		state, err := read()
		reads++
		if reads == 2 && err == nil {
			_, err = s.DB().Exec(`insert into people(display_name) values('Next report author'); insert into entries(source,ext_id,person_id,ts,body_text,body_sha,ingested_at) values('mail','next-report',last_insert_rowid(),0,'After snapshot evidence','synthetic',0)`)
		}
		return state, err
	}
	out.Reset()
	if err := runSlurp(&out, o, d); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "outcome=INCOMPLETE") {
		t.Fatal("post-snapshot input changed captured report", out.String())
	}
	next, err := s.MaintenanceState()
	if err != nil || next.Source.Clean() || next.Twins.Clean() {
		t.Fatal(next, err)
	}
	d.maintenanceState = read
	if err := runSlurp(&out, o, d); err != nil {
		t.Fatal(err)
	}
	next, err = s.MaintenanceState()
	if err != nil || !next.Clean() {
		t.Fatal(next, err)
	}
}

func TestSlurpCleanSourceDoesNotSkipWorkOnlyEpoch(t *testing.T) {
	path, s := gateStore(t)
	o := slurpOpts{only: []string{"twins", "repair"}}
	d := defaultSlurpDeps(path, o)
	d.status = func() error { return nil }
	var out strings.Builder
	if err := runSlurp(&out, o, d); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB().Exec(`update twins_work_state set work_revision=work_revision+1,full_revision=work_revision+1`); err != nil {
		t.Fatal(err)
	}
	before, err := s.MaintenanceState()
	if err != nil || !before.Source.Clean() || before.Twins.Clean() {
		t.Fatal(before, err)
	}
	calls := 0
	pending := d.pendingTwins
	d.pendingTwins = func() error { calls++; return pending() }
	if err := runSlurp(&out, o, d); err != nil {
		t.Fatal(err)
	}
	after, err := s.MaintenanceState()
	if err != nil || !after.Clean() || calls != 1 || after.Source != before.Source {
		t.Fatal(after, calls, err)
	}
}
