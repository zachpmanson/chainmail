package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/zachpmanson/chainmail/internal/corpus"
	"github.com/zachpmanson/chainmail/internal/embed"
)

// A pass is bounded independently of HTTP requests and batch model timeouts.
const embeddingRunTimeout = 30 * time.Minute

type embeddingStatus struct {
	Enabled    bool   `json:"enabled"`
	Running    bool   `json:"running"`
	Queued     bool   `json:"queued"`
	Model      string `json:"model,omitempty"`
	Dim        int    `json:"dim"`
	StartedAt  string `json:"startedAt,omitempty"`
	FinishedAt string `json:"finishedAt,omitempty"`
	Outcome    string `json:"outcome,omitempty"`
	Error      string `json:"error,omitempty"`
	Done       int    `json:"done"`
	Pending    int    `json:"pending"`
	Embedded   int    `json:"embedded"`
	Skipped    int    `json:"skipped"`
}

type embeddingRunner func(context.Context, func(corpus.BackfillProgress)) (corpus.BackfillReport, error)

// One goroutine owns inference; a single buffered wake coalesces follow-up work.
// The mutex only protects snapshots and wake publication, never model or SQL calls.
type embeddingWorker struct {
	mu     sync.Mutex
	state  embeddingStatus
	wake   chan struct{}
	done   chan struct{}
	cancel context.CancelFunc
	ctx    context.Context
	run    embeddingRunner
}

func newEmbeddingWorker(ctx context.Context, model string, dim int, run embeddingRunner) *embeddingWorker {
	ctx, cancel := context.WithCancel(ctx)
	w := &embeddingWorker{ctx: ctx, cancel: cancel, wake: make(chan struct{}, 1), done: make(chan struct{}), run: run,
		state: embeddingStatus{Enabled: true, Model: model, Dim: dim}}
	go w.loop()
	return w
}

func (w *embeddingWorker) enqueue() {
	if w == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.ctx.Err() != nil {
		return
	}
	select {
	case w.wake <- struct{}{}:
		w.state.Queued = true
	default:
	}
}

func (w *embeddingWorker) snapshot() embeddingStatus {
	if w == nil {
		return embeddingStatus{}
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.state
}

func (w *embeddingWorker) stop() {
	if w == nil {
		return
	}
	w.cancel()
	<-w.done
}

func (w *embeddingWorker) loop() {
	defer close(w.done)
	defer func() { w.mu.Lock(); w.state.Queued = false; w.mu.Unlock() }()
	for {
		select {
		case <-w.ctx.Done():
			return
		case <-w.wake:
		}
		w.mu.Lock()
		if w.ctx.Err() != nil {
			w.mu.Unlock()
			return
		}
		w.state = embeddingStatus{Enabled: true, Model: w.state.Model, Dim: w.state.Dim, Running: true, Queued: len(w.wake) > 0,
			StartedAt: time.Now().UTC().Format(time.RFC3339)}
		w.mu.Unlock()
		ctx, cancel := context.WithTimeout(w.ctx, embeddingRunTimeout)
		rep, err := w.run(ctx, func(p corpus.BackfillProgress) {
			w.mu.Lock()
			w.state.Done = p.Done
			w.state.Pending = p.Pending
			w.state.Embedded = p.Embedded
			w.state.Skipped = p.Skipped
			w.mu.Unlock()
		})
		cancel()
		w.mu.Lock()
		w.state.Running = false
		w.state.FinishedAt = time.Now().UTC().Format(time.RFC3339)
		w.state.Pending = rep.Pending
		w.state.Embedded = rep.Embedded
		w.state.Skipped = rep.Skipped
		w.state.Done = rep.Embedded + rep.Skipped
		w.state.Outcome = "complete"
		if err != nil {
			w.state.Outcome = "failed"
			if w.ctx.Err() != nil {
				w.state.Outcome = "cancelled"
			} else if errors.Is(err, embed.ErrDaemonDown) || errors.Is(err, embed.ErrModelMissing) {
				w.state.Outcome = "skipped"
			}
			// Error text can contain unbounded upstream HTTP bodies.
			text := []rune(strings.ToValidUTF8(err.Error(), "�"))
			w.state.Error = string(text[:min(512, len(text))])
		}
		w.mu.Unlock()
	}
}

func (s *server) startEmbedding(ctx context.Context) {
	if !s.slurpEnabled {
		return
	}
	e := s.embedder()
	s.embedding = newEmbeddingWorker(ctx, e.Model(), e.Dim(), func(ctx context.Context, progress func(corpus.BackfillProgress)) (corpus.BackfillReport, error) {
		rep := corpus.BackfillReport{Model: e.Model(), Dim: e.Dim()}
		readyCtx, cancel := context.WithTimeout(ctx, s.embedWait)
		ok, err := e.Available(readyCtx)
		cancel()
		if err != nil {
			return rep, err
		}
		if !ok {
			return rep, fmt.Errorf("%w: %s", embed.ErrModelMissing, e.Model())
		}
		return s.store.BackfillEmbeddings(ctx, e, corpus.BackfillOptions{Progress: progress})
	})
	s.embedding.enqueue()
}
