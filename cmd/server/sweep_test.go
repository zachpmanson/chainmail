package main

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"
)

// The status route carries the ingest the page cannot otherwise see: a scheduled
// sweep has no button behind it, and a corpus mid-rebuild reads as a threading
// bug (mailbox parents are linked per batch, see mailingest) rather than as work
// in progress. These tests pin the two facts the indicator is built from — a run
// in flight, and how the last one ended.

func TestStatusShowsAnIngestInFlight(t *testing.T) {
	srv, _ := sweepable(t, "")
	started := make(chan struct{})
	release := make(chan struct{})
	srv.runSlurp = func(context.Context, string) ([]byte, error) {
		close(started)
		<-release
		return []byte("slurp\n  mail     complete   in:anywhere: created 2, changed 0\n"), nil
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		if _, err := srv.slurpOnce(context.Background(), srv.runSlurp); err != nil {
			t.Errorf("slurpOnce: %v", err)
		}
	}()
	<-started

	res := srv.do(t, "GET", "/v1/status", nil)
	if res.status != http.StatusOK {
		t.Fatalf("status = %d: %s", res.status, res.body)
	}
	inFlight := decode[statusResponse](t, res).Sweep
	if !inFlight.Running {
		t.Error("status says no ingest is running while one is blocked in runSlurp")
	}
	if inFlight.StartedAt == "" {
		t.Fatal("a running ingest carries no startedAt, so a reader cannot say how long for")
	}
	if _, err := time.Parse(time.RFC3339, inFlight.StartedAt); err != nil {
		t.Errorf("startedAt = %q, want a UTC RFC3339 stamp: %v", inFlight.StartedAt, err)
	}

	close(release)
	<-done

	after := decode[statusResponse](t, srv.do(t, "GET", "/v1/status", nil)).Sweep
	if after.Running {
		t.Error("status still says an ingest is running after the one that ran returned")
	}
	if after.Outcome != sweepComplete {
		t.Errorf("outcome = %q after a clean transcript, want %q", after.Outcome, sweepComplete)
	}
	if after.FinishedAt == "" {
		t.Error("a finished ingest carries no finishedAt, so a reader cannot say when it ended")
	}
}

// A run that stopped at a bound is the one a reader most needs told: the corpus
// is whole enough to read and short of what the walk was asked for, so the pages
// built over it are missing mail with nothing on them to say so.
func TestStatusShowsAnIngestThatStoppedEarly(t *testing.T) {
	srv, _ := sweepable(t, "")
	srv.runSlurp = func(context.Context, string) ([]byte, error) {
		return []byte("slurp\n  mail     INCOMPLETE stopped at the -limit of 200; re-run to continue from the cursor\n" +
			"INCOMPLETE: mail left work unread — re-run to continue from the cursor\n"), nil
	}
	if res := srv.do(t, "POST", "/v1/slurp", nil); res.status != http.StatusOK {
		t.Fatalf("slurp status = %d: %s", res.status, res.body)
	}

	got := decode[statusResponse](t, srv.do(t, "GET", "/v1/status", nil)).Sweep
	if got.Outcome != sweepIncomplete {
		t.Errorf("outcome = %q, want %q", got.Outcome, sweepIncomplete)
	}
	if got.Running {
		t.Error("the ingest returned but status still says one is running")
	}
}

// A phase that broke fails the run, and the route says so rather than reporting
// the last phase's word: the reader's question is whether the corpus is what the
// walk was asked to produce.
func TestStatusShowsAnIngestThatFailed(t *testing.T) {
	srv, _ := sweepable(t, "")
	srv.runSlurp = func(context.Context, string) ([]byte, error) {
		return []byte("slurp\n  twins    FAILED     database is locked\n"), errors.New("exit status 1")
	}
	if res := srv.do(t, "POST", "/v1/slurp", nil); res.status != http.StatusBadGateway {
		t.Fatalf("slurp status = %d, want 502 for a run that failed: %s", res.status, res.body)
	}

	got := decode[statusResponse](t, srv.do(t, "GET", "/v1/status", nil)).Sweep
	if got.Outcome != sweepFailed {
		t.Errorf("outcome = %q, want %q", got.Outcome, sweepFailed)
	}
}

// And the reading itself, which is the part a note can fool: the outcome is the
// column of the phase line, not a word anywhere in the transcript.
func TestSweepOutcomeReadsTheOutcomeColumn(t *testing.T) {
	cases := []struct {
		name string
		out  string
		err  error
		want string
	}{
		{
			name: "a clean run",
			out: "slurp\n  mail      complete   in:anywhere: created 2, changed 0\n" +
				"5 phases: 5 complete, 0 incomplete, 0 skipped, 0 failed\n",
			want: sweepComplete,
		},
		{
			name: "a run that stopped at its bound",
			out: "slurp\n  mail      INCOMPLETE stopped at the -limit of 200\n" +
				"INCOMPLETE: mail left work unread — re-run to continue from the cursor\n",
			want: sweepIncomplete,
		},
		{
			name: "a phase that broke",
			out:  "slurp\n  twins     FAILED     database is locked\n",
			want: sweepFailed,
		},
		{
			name: "a note that happens to say failed",
			out:  "slurp\n  mail      complete   the mailbox failed to answer twice, then did\n",
			want: sweepComplete,
		},
		{
			name: "a walk that returned an error",
			out:  "slurp\n  mail      complete   in:anywhere\n",
			err:  errors.New("signal: killed"),
			want: sweepFailed,
		},
		{
			name: "a skipped phase is not unfinished work",
			out:  "slurp\n  slack     skipped    no slackdump on PATH\n",
			want: sweepComplete,
		},
		{
			name: "the worse of a broken and an unfinished phase wins",
			out: "slurp\n  mail      INCOMPLETE stopped at the -limit of 200\n" +
				"  twins     FAILED     database is locked\n",
			want: sweepFailed,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := sweepOutcome([]byte(c.out), c.err); got != c.want {
				t.Errorf("sweepOutcome = %q, want %q", got, c.want)
			}
		})
	}
}