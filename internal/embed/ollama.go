package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// DefaultModel is nomic-embed-text: 768 dimensions, first-class in ollama's
// library so `ollama pull nomic-embed-text` is the whole install. bge-small-en
// is smaller and would halve the stored bytes, but reaching it through ollama
// needs a hand-written Modelfile, which is a step a user has to get right
// before anything works at all.
const (
	DefaultModel = "nomic-embed-text"
	DefaultDim   = 768
	// DefaultBaseURL is ollama's own default. Nothing here authenticates, so a
	// non-loopback URL means the mail text crosses a network.
	DefaultBaseURL = "http://localhost:11434"

	// defaultBatch is how many texts go in one /api/embed call. Larger batches
	// amortise the request but hold everything in one server-side allocation
	// and one timeout, so an interruption loses more work.
	defaultBatch = 32

	// defaultTimeout covers a whole batch, not one text. A cold model load is
	// the slow case and can take tens of seconds.
	defaultTimeout = 2 * time.Minute
)

// Ollama is an Embedder backed by a local ollama daemon.
type Ollama struct {
	BaseURL string
	Name    string
	// Dimension is what the model is expected to return. A response of any
	// other width is rejected rather than accepted, because the alternative is
	// a corpus with two vector widths under one model name.
	Dimension int
	// Batch is the maximum number of texts per request; <= 0 means the default.
	Batch  int
	Client *http.Client
}

// NewOllama returns a client for the default model on the default endpoint.
func NewOllama() *Ollama {
	return &Ollama{
		BaseURL:   DefaultBaseURL,
		Name:      DefaultModel,
		Dimension: DefaultDim,
		Batch:     defaultBatch,
		Client:    &http.Client{Timeout: defaultTimeout},
	}
}

func (o *Ollama) Model() string { return o.Name }
func (o *Ollama) Dim() int      { return o.Dimension }

// Prefix is the task prefix for whatever model this client was pointed at, so
// the daemon client and the model's traits cannot drift apart: there is one
// name, and it decides both what is asked for and how the text is framed.
func (o *Ollama) Prefix(t Task) string { return TraitsFor(o.Name).Prefix(t) }

func (o *Ollama) batchSize() int {
	if o.Batch > 0 {
		return o.Batch
	}
	return defaultBatch
}

func (o *Ollama) http() *http.Client {
	if o.Client != nil {
		return o.Client
	}
	return &http.Client{Timeout: defaultTimeout}
}

// Embed vectorises texts, in chunks of Batch.
//
// /api/embed is used rather than the older /api/embeddings because it takes an
// array of inputs and returns an array of vectors: the batching happens inside
// ollama, where the model is already loaded, instead of as one HTTP round trip
// per message. Client-side pipelining across several concurrent requests would
// not help — ollama serialises work onto the model anyway, so the only thing
// concurrency adds here is several ways to be half-finished when the context is
// cancelled.
func (o *Ollama) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	out := make([][]float32, 0, len(texts))
	for start := 0; start < len(texts); start += o.batchSize() {
		end := min(start+o.batchSize(), len(texts))
		vecs, err := o.embedBatch(ctx, texts[start:end])
		if err != nil {
			return nil, err
		}
		out = append(out, vecs...)
	}
	return out, nil
}

type embedRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type embedResponse struct {
	Embeddings [][]float32 `json:"embeddings"`
	Error      string      `json:"error"`
}

func (o *Ollama) embedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	body, err := json.Marshal(embedRequest{Model: o.Name, Input: texts})
	if err != nil {
		return nil, err
	}
	base := o.BaseURL
	if base == "" {
		base = DefaultBaseURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimSuffix(base, "/")+"/api/embed", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := o.http().Do(req)
	if err != nil {
		// A refused connection or an unresolvable host is the "ollama is not
		// running" case, and it is the one a user hits first. Anything else —
		// a cancelled context, a timeout — is left as itself, because telling
		// someone to start a daemon that is already running wastes their time.
		if isDialFailure(err) {
			return nil, fmt.Errorf("%w at %s: start it with `ollama serve`: %w",
				ErrDaemonDown, base, err)
		}
		return nil, fmt.Errorf("embedding %d texts: %w", len(texts), err)
	}
	defer resp.Body.Close()

	// Bounded: a malformed endpoint answering with a stream should not be read
	// into memory forever. 64 MiB is far above a legitimate batch of 768-float
	// vectors rendered as JSON.
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return nil, fmt.Errorf("reading embedding response: %w", err)
	}

	var er embedResponse
	// Decode before branching on status: ollama reports a missing model as a
	// 404 whose body says which model, and that name is the actionable part.
	jsonErr := json.Unmarshal(raw, &er)
	if resp.StatusCode != http.StatusOK {
		msg := strings.TrimSpace(er.Error)
		if msg == "" {
			msg = strings.TrimSpace(string(raw))
		}
		if resp.StatusCode == http.StatusNotFound || strings.Contains(msg, "not found") {
			return nil, fmt.Errorf("%w: run `ollama pull %s` (%s)",
				ErrModelMissing, o.Name, msg)
		}
		return nil, fmt.Errorf("ollama %s: %s", resp.Status, msg)
	}
	if jsonErr != nil {
		return nil, fmt.Errorf("decoding embedding response: %w", jsonErr)
	}
	if er.Error != "" {
		return nil, fmt.Errorf("ollama: %s", er.Error)
	}
	if len(er.Embeddings) != len(texts) {
		return nil, fmt.Errorf("ollama returned %d vectors for %d texts",
			len(er.Embeddings), len(texts))
	}
	for i, v := range er.Embeddings {
		if o.Dimension > 0 && len(v) != o.Dimension {
			return nil, fmt.Errorf("%w: model %s returned %d dimensions, expected %d",
				ErrDimMismatch, o.Name, len(v), o.Dimension)
		}
		if err := Normalise(v); err != nil {
			return nil, fmt.Errorf("vector %d of %d from %s: %w", i+1, len(texts), o.Name, err)
		}
	}
	return er.Embeddings, nil
}

// isDialFailure reports whether err is a failure to reach the host at all, as
// opposed to a failure once connected.
func isDialFailure(err error) bool {
	var oe *net.OpError
	if errors.As(err, &oe) && oe.Op == "dial" {
		return true
	}
	var se *net.DNSError
	return errors.As(err, &se)
}

// Available reports whether the daemon answers and holds the model, so a caller
// can fail before doing minutes of other setup. A false with a nil error means
// the daemon is up but the model is not pulled.
func (o *Ollama) Available(ctx context.Context) (bool, error) {
	_, err := o.embedBatch(ctx, []string{"ping"})
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, ErrModelMissing):
		return false, nil
	default:
		return false, err
	}
}
