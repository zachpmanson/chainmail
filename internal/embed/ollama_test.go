package embed

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// serve returns an Ollama pointed at a handler, with the timeouts a test wants.
func serve(t *testing.T, dim int, h http.HandlerFunc) *Ollama {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return &Ollama{
		BaseURL: srv.URL, Name: "test-model", Dimension: dim, Batch: 2,
		Client: &http.Client{Timeout: 5 * time.Second},
	}
}

// The distinction this test protects is the whole reason ErrDaemonDown exists:
// a user with no ollama running must be told that, not handed an empty result
// set that reads as "the corpus does not contain this".
func TestDownDaemonIsDistinctFromAnEmptyResult(t *testing.T) {
	// A server bound and immediately closed leaves a port nothing listens on.
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close()

	down := &Ollama{BaseURL: url, Name: "test-model", Dimension: 4,
		Client: &http.Client{Timeout: 2 * time.Second}}
	_, downErr := down.Embed(context.Background(), []string{"anything"})
	if !errors.Is(downErr, ErrDaemonDown) {
		t.Fatalf("unreachable daemon: err = %v, want ErrDaemonDown", downErr)
	}
	if errors.Is(downErr, ErrModelMissing) {
		t.Error("a down daemon must not also read as a missing model")
	}

	// The contrasting case: a daemon that is up answers with vectors and no
	// error. Which is to say the two situations are not reachable through the
	// same value, in either direction.
	up := serve(t, 4, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"embeddings":[[1,0,0,0]]}`)
	})
	vs, err := up.Embed(context.Background(), []string{"anything"})
	if err != nil {
		t.Fatalf("live daemon: %v", err)
	}
	if len(vs) != 1 {
		t.Fatalf("live daemon returned %d vectors, want 1", len(vs))
	}
}

func TestUnpulledModelSaysWhichModelToPull(t *testing.T) {
	o := serve(t, 4, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"error":"model \"test-model\" not found, try pulling it first"}`)
	})
	_, err := o.Embed(context.Background(), []string{"anything"})
	if !errors.Is(err, ErrModelMissing) {
		t.Fatalf("err = %v, want ErrModelMissing", err)
	}
	if errors.Is(err, ErrDaemonDown) {
		t.Error("a missing model must not read as a down daemon: the fix is a pull, not a start")
	}
	if want := "ollama pull test-model"; !contains(err.Error(), want) {
		t.Errorf("error %q does not tell the user to %q", err, want)
	}
}

// A model swapped behind an unchanged name is the one corruption the stored
// dimension exists to catch, so it has to be caught at the wire and not written.
func TestUnexpectedDimensionIsRefused(t *testing.T) {
	o := serve(t, 4, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"embeddings":[[1,0,0]]}`)
	})
	_, err := o.Embed(context.Background(), []string{"anything"})
	if !errors.Is(err, ErrDimMismatch) {
		t.Fatalf("err = %v, want ErrDimMismatch", err)
	}
}

// A short response cannot be matched back to its inputs, so it is an error
// rather than a partial success the caller has to guess the alignment of.
func TestShortResponseIsAnError(t *testing.T) {
	o := serve(t, 4, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"embeddings":[[1,0,0,0]]}`)
	})
	_, err := o.Embed(context.Background(), []string{"one", "two"})
	if err == nil {
		t.Fatal("two texts answered with one vector: want an error")
	}
}

// Batching is server-side, over /api/embed's array input. The count of requests
// is the observable that says so.
func TestEmbedBatchesAndKeepsInputOrder(t *testing.T) {
	var requests int
	o := serve(t, 2, func(w http.ResponseWriter, r *http.Request) {
		requests++
		if got := r.URL.Path; got != "/api/embed" {
			t.Errorf("posted to %s, want /api/embed", got)
		}
		var req embedRequest
		if err := decodeJSON(r, &req); err != nil {
			t.Fatal(err)
		}
		// Answer with a vector that encodes its input's first byte, so the
		// caller's ordering is checkable.
		out := make([][]float32, 0, len(req.Input))
		for _, in := range req.Input {
			out = append(out, []float32{float32(in[0]), 1})
		}
		writeJSON(w, embedResponse{Embeddings: out})
	})

	vs, err := o.Embed(context.Background(), []string{"a", "b", "c", "d", "e"})
	if err != nil {
		t.Fatal(err)
	}
	if len(vs) != 5 {
		t.Fatalf("got %d vectors, want 5", len(vs))
	}
	if requests != 3 { // batch of 2 over 5 inputs
		t.Errorf("made %d requests for 5 texts at batch 2, want 3", requests)
	}
	// Normalisation happens on the way out, so the ordering is checked through
	// the sign and relative size of the first component rather than its value.
	for i := range vs[:len(vs)-1] {
		if !(vs[i][0] < vs[i+1][0]) {
			t.Fatalf("vectors came back out of input order: %v", vs)
		}
	}
}

func TestAvailableSeparatesUpFromUnpulled(t *testing.T) {
	live := serve(t, 4, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"embeddings":[[1,0,0,0]]}`)
	})
	ok, err := live.Available(context.Background())
	if err != nil || !ok {
		t.Errorf("live daemon: ok = %v, err = %v", ok, err)
	}

	unpulled := serve(t, 4, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"error":"model not found"}`)
	})
	// Up but unpulled is a false with no error: the daemon is fine, the setup is
	// one command short, and that is not a failure to report as one.
	ok, err = unpulled.Available(context.Background())
	if ok || err != nil {
		t.Errorf("unpulled model: ok = %v, err = %v, want false and no error", ok, err)
	}
}
