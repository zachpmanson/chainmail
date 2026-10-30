package main

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

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
	fullTwins, fullRepair := d.twins, d.repair
	d.twins = func(apply bool) error { twins++; return fullTwins(apply) }
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
			fullTwins, fullRepair := d.twins, d.repair
			d.twins = func(apply bool) error { twins++; return fullTwins(apply) }
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
			d.reconciliationState = func() (corpus.ReconcileState, error) {
				state, err := s.ReconciliationState()
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
			d.reconciliationState = func() (corpus.ReconcileState, error) {
				reads++
				if statusDone {
					return corpus.ReconcileState{}, errors.New("synthetic final read failure")
				}
				return s.ReconciliationState()
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
			d.twins = func(bool) error { twins++; return nil }
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
			d.repair = func() error {
				if which == "repair-fail" {
					return errors.New("synthetic repair failure")
				}
				return nil
			}
			if which == "state-fail" {
				d.reconciliationState = func() (corpus.ReconcileState, error) {
					return corpus.ReconcileState{}, errors.New("synthetic unsupported version")
				}
			}
			if which == "twins-only" || which == "repair-only" {
				d.reconciliationState = func() (corpus.ReconcileState, error) {
					t.Fatal("partial selection read pair state")
					return corpus.ReconcileState{}, nil
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
	d.reconciliationState = real.reconciliationState
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
