package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/zachpmanson/chainmail/internal/corpus"
	"github.com/zachpmanson/chainmail/internal/embed"
	"github.com/zachpmanson/chainmail/internal/spec"
)

// specConcurrency bounds spec builds in flight. Assembly recovers quoted
// bodies, measures repeated boilerplate and decodes attachment bytes, so a
// large selection is seconds of CPU; a client that fires a request per
// keystroke would otherwise put every core in HTML recovery and starve the
// cheap endpoints. There is no queue on purpose — a caller waiting on a slot
// gets a slot or a 429 it can retry, not a job id to poll.
const (
	specConcurrency = 2
	specSlotWait    = 2 * time.Second
	maxRequestBody  = 1 << 20
	maxLimit        = 200
	defaultLimit    = 20
)

// Retrieval modes, spelled as GET /v1/search takes them.
const (
	modeLexical  = "lexical"
	modeSemantic = "semantic"
	modeHybrid   = "hybrid"
)

type server struct {
	// One store for the process. database/sql is a pool and safe for concurrent
	// use, and the corpus is WAL, so the CLI can write while this reads. Opening
	// per request would instead re-run migrations on every request — which takes
	// the write lock, and fails outright against a read-only copy.
	store   *corpus.Store
	uploads string

	specSlots chan struct{}
	// slotWait is how long a caller waits for a slot before being told to retry.
	slotWait  time.Duration
	embedder  func() *embed.Ollama
	embedWait time.Duration
}

// routes maps the surface api/openapi.json declares, and nothing else.
//
// No CORS headers are sent, deliberately. Allowing an origin would let a page
// on that origin read this corpus out of the browser of whoever is running the
// server; the dev loop uses a Vite proxy so the client stays same-origin.
func (s *server) routes() http.Handler {
	mux := http.NewServeMux()
	// The method is checked in the handler rather than in the pattern, so that a
	// wrong one answers with the same JSON error shape as everything else
	// instead of ServeMux's plain text.
	mux.HandleFunc("/v1/search", get(s.search))
	mux.HandleFunc("/v1/spec", post(s.spec))
	mux.HandleFunc("/v1/entries/{extId}", get(s.entry))
	mux.HandleFunc("/v1/chains/{rootExtId}", get(s.chain))
	mux.HandleFunc("/v1/stats", get(s.stats))
	mux.HandleFunc("/v1/people", get(s.people))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fail(w, http.StatusNotFound, fmt.Errorf("no such endpoint: %s %s", r.Method, r.URL.Path))
	})
	return mux
}

func get(h http.HandlerFunc) http.HandlerFunc  { return method(http.MethodGet, h) }
func post(h http.HandlerFunc) http.HandlerFunc { return method(http.MethodPost, h) }

func method(want string, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != want {
			w.Header().Set("Allow", want)
			fail(w, http.StatusMethodNotAllowed,
				fmt.Errorf("%s takes %s, not %s", r.URL.Path, want, r.Method))
			return
		}
		h(w, r)
	}
}

func (s *server) search(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Query()
	text, person, since := p.Get("q"), p.Get("person"), p.Get("since")
	// An empty q is meaningful alongside person or since ("everything involving
	// X"), but with no filter at all the answer is an arbitrary slice of the
	// corpus that reads like a ranked one.
	if text == "" && person == "" && since == "" {
		fail(w, http.StatusBadRequest, errors.New("give at least one of q, person or since"))
		return
	}
	limit, err := intParam(p, "limit", defaultLimit, 1, maxLimit)
	if err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	asEntries, err := boolParam(p, "entries")
	if err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	q := corpus.Query{Text: text, Limit: limit}
	if since != "" {
		t, err := time.Parse("2006-01-02", since)
		if err != nil {
			fail(w, http.StatusBadRequest, fmt.Errorf("since %q: want YYYY-MM-DD", since))
			return
		}
		q.Since = t
	}
	if person != "" {
		// Involving, not People: a cc-only participant is invisible to an
		// author-only filter, and they are often the point.
		q.Involving = []string{person}
	}
	mode := p.Get("mode")
	if mode == "" {
		mode = modeLexical
	}
	sem, status, err := s.semanticFor(r.Context(), mode, text)
	if err != nil {
		fail(w, status, err)
		return
	}
	q.Semantic = sem

	out := searchResponse{Mode: mode}
	if asEntries {
		hits, err := s.store.SearchEntries(q)
		if err != nil {
			fail(w, http.StatusInternalServerError, err)
			return
		}
		es := make([]entryHit, 0, len(hits))
		for _, h := range hits {
			es = append(es, toEntryHit(h))
		}
		out.Entries = &es
	} else {
		hits, err := s.store.SearchChains(q)
		if err != nil {
			fail(w, http.StatusInternalServerError, err)
			return
		}
		cs := make([]chainHit, 0, len(hits))
		for _, h := range hits {
			cs = append(cs, toChainHit(h))
		}
		out.Chains = &cs
	}
	send(w, http.StatusOK, out)
}

// semanticFor embeds the query text, or returns nil for a lexical search.
//
// The embedding happens here rather than inside search, for the reason
// corpus.SemanticFor gives: a daemon that is not running then surfaces as an
// answer to what the caller asked, instead of as an empty result set. Its
// message is embed's own — "start it with `ollama serve`" — because the CLI
// already says the useful thing and a second wording would be a worse one.
func (s *server) semanticFor(ctx context.Context, mode, text string) (*corpus.SemanticQuery, int, error) {
	switch mode {
	case modeLexical:
		return nil, 0, nil
	case modeSemantic, modeHybrid:
	default:
		return nil, http.StatusBadRequest,
			fmt.Errorf("mode %q: want lexical, semantic or hybrid", mode)
	}
	if strings.TrimSpace(text) == "" {
		return nil, http.StatusBadRequest,
			fmt.Errorf("mode=%s needs q: there is nothing to be similar to", mode)
	}
	ctx, cancel := context.WithTimeout(ctx, s.embedWait)
	defer cancel()
	sem, err := corpus.SemanticFor(ctx, s.embedder(), text, corpus.SemanticOptions{Only: mode == modeSemantic})
	if err != nil {
		wrapped := fmt.Errorf("mode %q: %w", mode, err)
		switch {
		case errors.Is(err, embed.ErrDaemonDown), errors.Is(err, embed.ErrModelMissing):
			return nil, http.StatusServiceUnavailable, wrapped
		case errors.Is(err, context.DeadlineExceeded):
			return nil, http.StatusGatewayTimeout, wrapped
		}
		return nil, http.StatusInternalServerError, wrapped
	}
	return sem, 0, nil
}

type specRequest struct {
	Chains  []string     `json:"chains"`
	Title   string       `json:"title"`
	Me      []string     `json:"me"`
	Queries []spec.Query `json:"queries"`
}

func (s *server) spec(w http.ResponseWriter, r *http.Request) {
	var req specRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRequestBody))
	// A misspelled field is a caller reading a different contract, and silently
	// building the page they did not ask for is worse than refusing.
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		fail(w, http.StatusBadRequest, fmt.Errorf("reading the request body: %w", err))
		return
	}
	if len(req.Chains) == 0 {
		fail(w, http.StatusBadRequest, errors.New(
			"chains is empty: name the chains the page is built from (GET /v1/search reports their rootExtId)"))
		return
	}
	// Resolve every named chain before doing any work, so an id that is not in
	// the corpus is a 404 naming it rather than a page quietly missing a thread.
	for _, id := range req.Chains {
		if _, err := s.store.Show(id); err != nil {
			if errors.Is(err, corpus.ErrNotFound) {
				fail(w, http.StatusNotFound, err)
				return
			}
			fail(w, http.StatusInternalServerError, err)
			return
		}
	}
	if !s.acquireSpecSlot(r.Context()) {
		w.Header().Set("Retry-After", "5")
		fail(w, http.StatusTooManyRequests, fmt.Errorf(
			"%d spec builds already in flight; each is seconds of work, so retry rather than pile on",
			specConcurrency))
		return
	}
	defer func() { <-s.specSlots }()

	started := time.Now()
	sp, err := spec.Generate(s.store, spec.Options{
		ExtIDs:    req.Chains,
		Title:     req.Title,
		Me:        req.Me,
		Queries:   req.Queries,
		RunLabel:  time.Now().Format("2 Jan 2006"),
		UploadDir: s.uploads,
	})
	if err != nil {
		// Generate refuses a selection it cannot turn into a valid spec — no
		// entries behind the ids, no title to borrow. That is the caller's
		// selection being wrong, not the server failing.
		fail(w, http.StatusBadRequest, err)
		return
	}
	log.Printf("spec: %d entries from %d chains in %s", len(sp.Messages), len(req.Chains),
		time.Since(started).Round(time.Millisecond))
	send(w, http.StatusOK, sp)
}

// acquireSpecSlot waits briefly rather than refusing at once: a second click
// arriving while the first build finishes is normal, and a 429 for it would be
// noise.
func (s *server) acquireSpecSlot(ctx context.Context) bool {
	select {
	case s.specSlots <- struct{}{}:
		return true
	default:
	}
	t := time.NewTimer(s.slotWait)
	defer t.Stop()
	select {
	case s.specSlots <- struct{}{}:
		return true
	case <-t.C:
		return false
	case <-ctx.Done():
		return false
	}
}

func (s *server) entry(w http.ResponseWriter, r *http.Request) {
	shown, err := s.store.Show(r.PathValue("extId"))
	if err != nil {
		failLookup(w, err)
		return
	}
	send(w, http.StatusOK, toCorpusEntry(shown))
}

func (s *server) chain(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("rootExtId")
	// Store.Chain walks ancestors as well as descendants, so any member's id
	// returns the whole conversation. That is what makes the endpoint usable
	// from a search result, which names the entry that matched, not the root.
	shown, err := s.store.Chain(id)
	if err != nil {
		failLookup(w, err)
		return
	}
	out := chainResponse{RootExtID: id, Entries: make([]corpusEntry, 0, len(shown))}
	for _, sh := range shown {
		out.Entries = append(out.Entries, toCorpusEntry(sh))
	}
	send(w, http.StatusOK, out)
}

func (s *server) stats(w http.ResponseWriter, r *http.Request) {
	st, err := s.store.Stats()
	if err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	es, err := s.store.EmbedStats()
	if err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	out := statsResponse{
		Entries: st.Entries, BySource: st.BySource, People: st.People,
		ChainRoots: st.Roots, Unresolved: st.Unresolved,
		Embeddings: make([]embedModelStats, 0, len(es)),
	}
	if out.BySource == nil {
		out.BySource = map[string]int64{}
	}
	for _, m := range es {
		out.Embeddings = append(out.Embeddings, embedModelStats{
			Model: m.Model, Dim: m.Dim, Vectors: m.Vectors,
			Skipped: m.Skipped, Stale: m.Stale, Eligible: m.Eligible,
		})
	}
	send(w, http.StatusOK, out)
}

func (s *server) people(w http.ResponseWriter, r *http.Request) {
	ps, err := corpus.People(s.store)
	if err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	out := peopleResponse{People: make([]personSummary, 0, len(ps))}
	for _, p := range ps {
		out.People = append(out.People, personSummary{
			PersonID: p.PersonID, DisplayName: p.DisplayName,
			Identities: p.Identities, Sent: p.Sent, Received: p.Received,
		})
	}
	send(w, http.StatusOK, out)
}

func intParam(p map[string][]string, name string, def, min, max int) (int, error) {
	raw := firstOf(p, name)
	if raw == "" {
		return def, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s %q: want a whole number", name, raw)
	}
	if n < min || n > max {
		return 0, fmt.Errorf("%s %d: want %d to %d", name, n, min, max)
	}
	return n, nil
}

// boolParam takes only the two spellings JSON and HTML forms agree on, so that
// "entries=yes" is a visible mistake rather than a silent false.
func boolParam(p map[string][]string, name string) (bool, error) {
	switch raw := firstOf(p, name); raw {
	case "":
		return false, nil
	case "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return false, fmt.Errorf("%s %q: want true or false", name, raw)
	}
}

func firstOf(p map[string][]string, name string) string {
	if vs := p[name]; len(vs) > 0 {
		return vs[0]
	}
	return ""
}

func failLookup(w http.ResponseWriter, err error) {
	if errors.Is(err, corpus.ErrNotFound) {
		fail(w, http.StatusNotFound, err)
		return
	}
	fail(w, http.StatusInternalServerError, err)
}

// fail is the only way a non-2xx leaves this server, so every error a client
// meets has one shape to parse.
func fail(w http.ResponseWriter, status int, err error) {
	send(w, status, map[string]string{"error": err.Error()})
}

func send(w http.ResponseWriter, status int, body any) {
	blob, err := json.Marshal(body)
	if err != nil {
		// Marshalling failed after nothing has been written, so the status line
		// is still ours to choose.
		log.Printf("encoding a %d response: %v", status, err)
		blob = []byte(`{"error":"encoding the response failed"}`)
		status = http.StatusInternalServerError
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	w.Write(blob)
}
