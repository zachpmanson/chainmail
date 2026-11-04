package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zachpmanson/chainmail/internal/corpus"
	"github.com/zachpmanson/chainmail/internal/embed"
)

func embeddingBarrier(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal("worker barrier timed out")
	}
}

func embeddingSettled(t *testing.T, w *embeddingWorker) embeddingStatus {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		state := w.snapshot()
		if !state.Running && !state.Queued && state.Outcome != "" {
			return state
		}
		select {
		case <-deadline:
			t.Fatal("worker did not settle")
		default:
			runtime.Gosched()
		}
	}
}

func TestEmbeddingWorkerCoalescesFollowupAndPublishesCommittedProgress(t *testing.T) {
	entered := make(chan struct{}, 3)
	release := make(chan struct{}, 3)
	var active, maxActive, calls atomic.Int32
	w := newEmbeddingWorker(context.Background(), "test-model", 8, func(ctx context.Context, progress func(corpus.BackfillProgress)) (corpus.BackfillReport, error) {
		n := active.Add(1)
		defer active.Add(-1)
		if n > maxActive.Load() {
			maxActive.Store(n)
		}
		calls.Add(1)
		progress(corpus.BackfillProgress{Done: 3, Pending: 9, Embedded: 2, Skipped: 1})
		entered <- struct{}{}
		select {
		case <-release:
		case <-ctx.Done():
			return corpus.BackfillReport{}, ctx.Err()
		}
		return corpus.BackfillReport{Pending: 9, Embedded: 2, Skipped: 1}, nil
	})
	defer w.stop()
	// Idle bursts are serialized with the worker's short state lock.
	w.mu.Lock()
	w.wake <- struct{}{}
	w.state.Queued = true
	w.mu.Unlock()
	embeddingBarrier(t, entered)
	for range 100 {
		w.enqueue()
	}
	state := w.snapshot()
	if !state.Running || !state.Queued || state.Done != 3 || state.Embedded != 2 || state.Skipped != 1 || state.Model != "test-model" || state.Dim != 8 || state.StartedAt == "" {
		t.Fatalf("progress snapshot: %+v", state)
	}
	// The snapshot is a value, not live mutable worker state.
	state.Done = 100
	if w.snapshot().Done != 3 {
		t.Fatal("snapshot aliases worker state")
	}
	release <- struct{}{}
	embeddingBarrier(t, entered)
	if w.snapshot().Queued {
		t.Fatal("burst queued more than one followup")
	}
	release <- struct{}{}
	final := embeddingSettled(t, w)
	if calls.Load() != 2 || maxActive.Load() != 1 || final.Outcome != "complete" || final.FinishedAt == "" {
		t.Fatalf("calls=%d max=%d final=%+v", calls.Load(), maxActive.Load(), final)
	}
}

func TestEmbeddingWorkerFailureSkipAndLaterTriggerRetry(t *testing.T) {
	for _, failure := range []error{errors.New(strings.Repeat("broken", 200)), embed.ErrDaemonDown, embed.ErrModelMissing} {
		t.Run(failure.Error()[:min(20, len(failure.Error()))], func(t *testing.T) {
			var calls atomic.Int32
			w := newEmbeddingWorker(context.Background(), "test", 8, func(context.Context, func(corpus.BackfillProgress)) (corpus.BackfillReport, error) {
				if calls.Add(1) == 1 {
					return corpus.BackfillReport{}, failure
				}
				return corpus.BackfillReport{Pending: 1, Embedded: 1}, nil
			})
			defer w.stop()
			w.enqueue()
			state := embeddingSettled(t, w)
			want := "failed"
			if errors.Is(failure, embed.ErrDaemonDown) || errors.Is(failure, embed.ErrModelMissing) {
				want = "skipped"
			}
			if state.Outcome != want || state.Error == "" || len([]rune(state.Error)) > 512 {
				t.Fatalf("failure=%+v", state)
			}
			w.enqueue()
			state = embeddingSettled(t, w)
			if state.Outcome != "complete" || state.Error != "" || state.Done != 1 || calls.Load() != 2 {
				t.Fatalf("retry=%+v calls=%d", state, calls.Load())
			}
		})
	}
}

func TestEmbeddingWorkerShutdownCancelsJoinsAndDropsQueuedWork(t *testing.T) {
	entered := make(chan struct{})
	cancelled := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	w := newEmbeddingWorker(context.Background(), "test", 8, func(ctx context.Context, _ func(corpus.BackfillProgress)) (corpus.BackfillReport, error) {
		calls.Add(1)
		close(entered)
		<-ctx.Done()
		close(cancelled)
		<-release
		return corpus.BackfillReport{}, ctx.Err()
	})
	w.enqueue()
	embeddingBarrier(t, entered)
	w.enqueue()
	stopped := make(chan struct{})
	go func() { w.stop(); close(stopped) }()
	embeddingBarrier(t, cancelled)
	select {
	case <-stopped:
		t.Fatal("stop returned before runner joined")
	default:
	}
	close(release)
	embeddingBarrier(t, stopped)
	w.enqueue()
	state := w.snapshot()
	if calls.Load() != 1 || state.Running || state.Queued || state.Outcome != "cancelled" {
		t.Fatalf("shutdown calls=%d status=%+v", calls.Load(), state)
	}
}

func TestIngestQueuesIndependentEmbeddingEvenAfterPartialFailure(t *testing.T) {
	for _, outcome := range []string{"complete", "incomplete", "failed"} {
		t.Run(outcome, func(t *testing.T) {
			s := testServer(t)
			s.slurpEnabled = true
			s.slurpTimeout = time.Second
			entered := make(chan struct{}, 2)
			var calls atomic.Int32
			s.embedding = newEmbeddingWorker(context.Background(), "test", 8, func(ctx context.Context, _ func(corpus.BackfillProgress)) (corpus.BackfillReport, error) {
				calls.Add(1)
				entered <- struct{}{}
				<-ctx.Done()
				return corpus.BackfillReport{}, ctx.Err()
			})
			defer s.embedding.stop()
			s.runSlurp = func(context.Context, string) ([]byte, error) {
				switch outcome {
				case "failed":
					return []byte("mail wrote a partial batch"), errors.New("ingest failed")
				case "incomplete":
					return []byte("mail INCOMPLETE reached bound"), nil
				default:
					return []byte("mail OK"), nil
				}
			}
			res := s.do(t, "POST", "/v1/slurp", nil)
			want := http.StatusOK
			if outcome == "failed" {
				want = http.StatusBadGateway
			}
			if res.status != want {
				t.Fatalf("response %d: %s", res.status, res.body)
			}
			embeddingBarrier(t, entered)
			if state := s.sweepStatus(); state.Running || state.Outcome != outcome {
				t.Fatalf("ingest inherited embedding: %+v", state)
			}
			status := s.do(t, "GET", "/v1/status", nil)
			loadAPI(t).assert(t, "StatusResponse", status.body)
			wire := decode[statusResponse](t, status)
			if !wire.Embedding.Running || wire.Sweep.Outcome != outcome {
				t.Fatalf("independent status = %+v", wire)
			}
			// Rejections must not enqueue another pass.
			s.slurpEnabled = false
			if res := s.do(t, "POST", "/v1/slurp", nil); res.status != 403 {
				t.Fatal(res.status)
			}
			s.slurpEnabled = true
			s.sweeping.Store(true)
			if res := s.do(t, "POST", "/v1/slurp", nil); res.status != 409 {
				t.Fatal(res.status)
			}
			s.sweeping.Store(false)
			if state := s.embedding.snapshot(); state.Queued || calls.Load() != 1 {
				t.Fatalf("rejection queued work: %+v", state)
			}
		})
	}
}

func TestEmbeddingFailureDoesNotFailSuccessfulIngest(t *testing.T) {
	s := testServer(t)
	s.slurpEnabled = true
	s.slurpTimeout = time.Second
	s.embedding = newEmbeddingWorker(context.Background(), "test", 8, func(context.Context, func(corpus.BackfillProgress)) (corpus.BackfillReport, error) {
		return corpus.BackfillReport{}, errors.New("embedding failed")
	})
	defer s.embedding.stop()
	s.runSlurp = func(context.Context, string) ([]byte, error) { return []byte("mail OK"), nil }
	if res := s.do(t, "POST", "/v1/slurp", nil); res.status != 200 {
		t.Fatalf("response %d: %s", res.status, res.body)
	}
	if state := embeddingSettled(t, s.embedding); state.Outcome != "failed" {
		t.Fatal(state)
	}
	if state := s.sweepStatus(); state.Outcome != "complete" {
		t.Fatal(state)
	}
}

func TestEmbeddingStartupGrantAndReadinessCancellation(t *testing.T) {
	s := testServer(t)
	// Disabled startup must not even construct a model client.
	s.embedder = func() *embed.Ollama { t.Fatal("disabled host constructed embedder"); return nil }
	s.startEmbedding(context.Background())
	if s.embedding != nil || s.embedding.snapshot().Enabled {
		t.Fatal("disabled worker started")
	}
	entered := make(chan struct{})
	exited := make(chan struct{})
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		close(entered)
		<-r.Context().Done()
		close(exited)
	}))
	defer endpoint.Close()
	s.slurpEnabled = true
	s.embedWait = time.Minute
	s.embedder = func() *embed.Ollama {
		return &embed.Ollama{BaseURL: endpoint.URL, Name: "configured-model", Dimension: 8}
	}
	s.startEmbedding(context.Background())
	embeddingBarrier(t, entered)
	if state := s.embedding.snapshot(); !state.Running || state.Model != "configured-model" || state.Dim != 8 {
		t.Fatalf("startup = %+v", state)
	}
	s.embedding.stop()
	embeddingBarrier(t, exited)
	if state := s.embedding.snapshot(); state.Running || state.Outcome != "cancelled" {
		t.Fatalf("readiness outlived stop: %+v", state)
	}
}

func TestServerIngestPhasesExcludeEmbedding(t *testing.T) {
	for _, phases := range []string{manualPhases, sweepPhases} {
		if strings.Contains(phases, "embed") {
			t.Fatalf("server still waits for embedding: %s", phases)
		}
	}
}

func TestEmbeddingContinuesAfterHTTPDisconnectAndScheduledIngestQueues(t *testing.T) {
	s := testServer(t)
	s.slurpEnabled = true
	s.slurpTimeout = time.Second
	lifecycle, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.lifecycleCtx = lifecycle
	entered := make(chan struct{}, 1)
	modelCtx := make(chan context.Context, 1)
	s.embedding = newEmbeddingWorker(lifecycle, "test", 8, func(ctx context.Context, _ func(corpus.BackfillProgress)) (corpus.BackfillReport, error) {
		modelCtx <- ctx
		entered <- struct{}{}
		<-ctx.Done()
		return corpus.BackfillReport{}, ctx.Err()
	})
	defer s.embedding.stop()
	s.runSlurp = func(context.Context, string) ([]byte, error) { return []byte("mail OK"), nil }
	request, disconnect := context.WithCancel(context.Background())
	if res := s.doWithContext(t, request, "POST", "/v1/slurp", nil); res.status != 200 {
		t.Fatal(res.status)
	}
	embeddingBarrier(t, entered)
	ctx := <-modelCtx
	disconnect()
	select {
	case <-ctx.Done():
		t.Fatal("HTTP disconnect cancelled embedding")
	default:
	}
	if _, err := s.slurpOnce(lifecycle, "scheduled sweep", s.runSlurp); err != nil {
		t.Fatal(err)
	}
	if !s.embedding.snapshot().Queued {
		t.Fatal("scheduled ingest did not queue followup")
	}
}
