package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func captureTimingLogs(t *testing.T, w io.Writer) {
	t.Helper()
	old := log.Writer()
	log.SetOutput(w)
	t.Cleanup(func() { log.SetOutput(old) })
}

func TestSlurpTimingWriterStreamsMetadataAndKeepsTranscript(t *testing.T) {
	var logs bytes.Buffer
	captureTimingLogs(t, &logs)
	w := &slurpTimingWriter{source: "manual sweep"}
	parts := []string{
		"private mail detail\nslurp timing: phase=ma",
		"il outcome=started elapsed=0s",
		"\nslurp timing: phase=mail outcome=INCOMPLETE elapsed=2.345s\n",
		"slurp timing: phase=mail outcome=FAILED elapsed=private\n",
		"slurp timing: phase=mail outcome=FAILED elapsed=-1s\n",
		"slurp timing: phase=private outcome=FAILED elapsed=1s\n",
		"slurp timing: phase=mail outcome=FAILED elapsed=1s private detail\n",
		strings.Repeat("x", 10000) + "slurp timing: phase=mail outcome=FAILED elapsed=1s\n",
		"slurp timing: phase=status outcome=complete elapsed=12ms\n",
	}
	for i, part := range parts {
		if n, err := io.WriteString(w, part); err != nil || n != len(part) {
			t.Fatalf("Write = %d, %v", n, err)
		}
		if i < 2 && logs.Len() != 0 {
			t.Fatal("partial lines must wait for a newline")
		}
		if i == 2 && !strings.Contains(logs.String(), "manual sweep slurp timing: phase=mail outcome=started elapsed=0s") {
			t.Fatal("timing was buffered until subprocess completion")
		}
	}
	if got := logs.String(); strings.Count(got, "slurp timing:") != 3 || strings.Contains(got, "private") || strings.Contains(got, "FAILED") {
		t.Fatalf("unexpected live metadata:\n%s", got)
	}
	if w.transcript.String() != strings.Join(parts, "") {
		t.Fatal("the response transcript changed")
	}
}

type timingLogFunc func([]byte) (int, error)

func (f timingLogFunc) Write(p []byte) (int, error) { return f(p) }

func TestSlurpStreamsBeforeCancellationForBothCallers(t *testing.T) {
	for _, tc := range []struct{ phases, source string }{
		{manualPhases, "manual sweep"}, {sweepPhases, "scheduled sweep"},
	} {
		t.Run(tc.source, func(t *testing.T) {
			dir := t.TempDir()
			script := "#!/bin/sh\nprintf 'private existing output\\n'\nprintf 'slurp timing: phase=mail outcome=started elapsed=0s\\n' >&2\nexec sleep 30\n"
			if err := os.WriteFile(filepath.Join(dir, "corpus"), []byte(script), 0o755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			var logs bytes.Buffer
			live := false
			captureTimingLogs(t, timingLogFunc(func(p []byte) (int, error) {
				logs.Write(p)
				if strings.Contains(string(p), tc.source+" slurp timing: phase=mail outcome=started") {
					live = ctx.Err() == nil
					cancel() // only a live event can release the blocked subprocess
				}
				return len(p), nil
			}))
			out, err := defaultSlurp(tc.phases, tc.source)(ctx, "/tmp/unused-corpus")
			if err == nil || !live || ctx.Err() != context.Canceled {
				t.Fatalf("no live event before cancellation: live=%v, error=%v, ctx=%v", live, err, ctx.Err())
			}
			if !strings.Contains(string(out), "private existing output") || !strings.Contains(string(out), "outcome=started") {
				t.Fatalf("lost partial transcript: %q", out)
			}
			if strings.Contains(logs.String(), "private") || strings.Contains(logs.String(), "outcome=complete") {
				t.Fatalf("unexpected live output: %s", logs.String())
			}
		})
	}
}

func TestSweepTotalTimingIncludesFailuresAndIncompleteRuns(t *testing.T) {
	for _, outcome := range []string{sweepComplete, sweepIncomplete, sweepFailed} {
		t.Run(outcome, func(t *testing.T) {
			srv := testServer(t)
			srv.slurpTimeout = time.Second
			var logs bytes.Buffer
			captureTimingLogs(t, &logs)
			run := func(context.Context, string) ([]byte, error) {
				switch outcome {
				case sweepFailed:
					return []byte("partial report"), errors.New("ingest failed")
				case sweepIncomplete:
					return []byte("mail INCOMPLETE bound reached"), nil
				default:
					return []byte("mail complete"), nil
				}
			}
			_, _ = srv.slurpOnce(context.Background(), "scheduled sweep", run)
			for _, want := range []string{"scheduled sweep timing: phase=total outcome=started elapsed=0s", "scheduled sweep timing: phase=total outcome=" + outcome + " elapsed="} {
				if !strings.Contains(logs.String(), want) {
					t.Errorf("missing %q in %s", want, logs.String())
				}
			}
		})
	}
}
