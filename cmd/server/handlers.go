package main

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/zachpmanson/chainmail/internal/corpus"
	mailembed "github.com/zachpmanson/chainmail/internal/embed"
	"github.com/zachpmanson/chainmail/internal/mailingest"
	"github.com/zachpmanson/chainmail/internal/refresh"
	"github.com/zachpmanson/chainmail/internal/spec"
)

// The built web client, embedded so one binary serves both the API and the UI.
// Building the client is a separate step (vite build → cmd/server/dist/, wired
// into the package's build in flake.nix); when dist is absent the build fails
// so a server that claims to serve the UI always can. cmd/server/dist is
// gitignored; only the embedded result ships.
//
//go:embed all:dist
var webDist embed.FS

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
	specs   string // dir for saved pages (POST /v1/spec writes here, GET /v1/specs reads)

	specSlots chan struct{}
	// slotWait is how long a caller waits for a slot before being told to retry.
	slotWait  time.Duration
	embedder  func() *mailembed.Ollama
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
	mux.HandleFunc("/v1/refresh", post(s.refresh))
	mux.HandleFunc("/v1/search", get(s.search))
	mux.HandleFunc("/v1/spec", post(s.spec))
	mux.HandleFunc("/v1/specs/{name}", get(s.savedSpec))
	mux.HandleFunc("/v1/entries/{extId}", get(s.entry))
	mux.HandleFunc("/v1/chains/{rootExtId}", get(s.chain))
	mux.HandleFunc("/v1/stats", get(s.stats))
	mux.HandleFunc("/v1/people", get(s.people))
	mux.HandleFunc("/", s.webRoot())
	return mux
}

// webRoot serves the embedded web client alongside the API: the app shell at
// / and /index.html, static assets by name, the render route /view/<name> as
// the shell (a deep link or refresh of a saved page must land on the client,
// which then fetches GET /v1/specs/<name>), and every other non-/v1/ path as
// the API's JSON 404 (unknown endpoints stay in the one error shape — a path
// that is not a file is not a frontend route).
// /v1/* that matches no registered handler still reaches here via the catch-
// all and must keep the JSON 404 contract, never an HTML fallback.
func (s *server) webRoot() http.HandlerFunc {
	sub, err := fs.Sub(webDist, "dist")
	if err != nil {
		// The embed pattern guarantees dist exists inside the compiled binary.
		panic(err)
	}
	fileServer := http.FileServer(http.FS(sub))
	json404 := func(w http.ResponseWriter, r *http.Request) {
		fail(w, http.StatusNotFound,
			fmt.Errorf("no such endpoint: %s %s", r.Method, r.URL.Path))
	}
	// The app shell. FileServer would serve it for "/" via its implicit index,
	// but the same bytes must also answer for a bare /index.html and the render
	// route; open index.html directly so all three get the same headers.
	shell := func(w http.ResponseWriter, r *http.Request) {
		f, err := sub.Open("index.html")
		if err != nil {
			json404(w, r)
			return
		}
		defer f.Close()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		io.Copy(w, f)
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/v1/") {
			json404(w, r)
			return
		}
		p := strings.TrimPrefix(r.URL.Path, "/")
		// The route prefix is matched carefully — view, view/ and view/<name>
		// but not a file named "viewfoo" or "view-notes".
		isView := p == "view" || strings.HasPrefix(p, "view/")
		if p == "" || p == "index.html" || isView {
			shell(w, r)
			return
		}
		if _, err := fs.Stat(sub, p); err != nil {
			json404(w, r)
			return
		}
		fileServer.ServeHTTP(w, r)
	}
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
		case errors.Is(err, mailembed.ErrDaemonDown), errors.Is(err, mailembed.ErrModelMissing):
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
	// Name, when set, saves the built page under /view/<name> so it survives a
	// refresh (and a reboot) and can be reopened from its URL. The client
	// chooses it — the URL it pushes has to match — and the server validates it
	// rather than trusting it; a blank name builds the page without saving.
	Name string `json:"name,omitempty"`
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
	// A malformed name is refused before any work: building the page takes
	// seconds, and a name that cannot name a file should cost nothing.
	if req.Name != "" && !validSpecName(req.Name) {
		fail(w, http.StatusBadRequest, fmt.Errorf(
			"name %q: use letters, digits, '.' '_' '-' (no slashes, no '..')", req.Name))
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
	if req.Name != "" {
		if err := s.saveSpec(req.Name, sp); err != nil {
			fail(w, http.StatusInternalServerError,
				fmt.Errorf("saving the page as %q: %w", req.Name, err))
			return
		}
	}
	send(w, http.StatusOK, sp)
}

// specNameRe is what a saved page's name may be: it is joined onto a directory
// path and echoed into the URL, so slashes, dot-dot and control characters are
// refused outright rather than escaped.
var specNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

func validSpecName(name string) bool {
	return name != "." && name != ".." && specNameRe.MatchString(name)
}

// saveSpec writes the built page under the server's specs dir so GET
// /v1/specs/{name} can send it again after a refresh or a reboot. The bytes
// are the same the caller just received: marshal once, so the saved copy and
// the live response can never drift apart.
func (s *server) saveSpec(name string, sp spec.Spec) error {
	blob, err := json.Marshal(sp)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.specs, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.specs, name+".json"), blob, 0o644)
}

// savedSpec returns a page that POST /v1/spec named — the read half of the
// render route /view/<name>.
func (s *server) savedSpec(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !validSpecName(name) {
		fail(w, http.StatusBadRequest, fmt.Errorf(
			"name %q: use letters, digits, '.' '_' '-' (no slashes, no '..')", name))
		return
	}
	blob, err := os.ReadFile(filepath.Join(s.specs, name+".json"))
	if err != nil {
		if os.IsNotExist(err) {
			fail(w, http.StatusNotFound, fmt.Errorf(
				"no saved page named %q — build it first with POST /v1/spec (name: %q)", name, name))
			return
		}
		fail(w, http.StatusInternalServerError, err)
		return
	}
	send(w, http.StatusOK, json.RawMessage(blob))
}

// refresh brings a page that has already been built up to date: the caller
// posts the previous spec (as POST /v1/spec returned it) and any selection
// overrides, and the server regenerates the page from the corpus.
//
// This is the read half of the CLI's `refresh` command. The fetching half is
// deliberately absent here: reaching the mailbox is `corpus ingest`'s job and
// that belongs to the CLI and the cron, not a browser. So the refresh here is
// corpus-only — it re-derives the page, grows the chains that gained entries,
// and proposes new chains from the recorded queries, but never asks the
// mailbox for what arrived.
func (s *server) refresh(w http.ResponseWriter, r *http.Request) {
	var req refreshRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRequestBody))
	// A misspelled field is a caller reading a different contract; opening the
	// corpus with a half-understood spec is worse than refusing.
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		fail(w, http.StatusBadRequest, fmt.Errorf("reading the request body: %w", err))
		return
	}
	// A refresh has to know what to bring up to date. The CLI's Load() refuses
	// a spec with no previous messages, and so do we, with the same message.
	if len(req.Spec.Messages) == 0 {
		fail(w, http.StatusBadRequest, fmt.Errorf(
			"the spec has no messages, so there is no previous run to refresh — "+
				"build it first with POST /v1/spec"))
		return
	}

	// The previous spec may be from a newer build. Refresh refuses to reproduce
	// what it would have to drop, so a caller with a future-version spec must
	// rebuild the page rather than refresh it.
	version := req.Spec.SpecVersion
	if version == 0 {
		version = 1 // the schema's stated default; absent means 1.
	}
	if version > refresh.MaxSpecVersion {
		fail(w, http.StatusBadRequest, fmt.Errorf(
			"specVersion %d, but this server refreshes up to %d — "+
				"rebuild the page first (POST /v1/spec)", version, refresh.MaxSpecVersion))
		return
	}

	rep, next, err := refresh.Run(s.store, noMailbox{}, req.Spec, refresh.Options{
		Title:      req.Title,
		Person:     req.Person,
		Since:      req.Since,
		Limit:      req.Limit,
		Me:         req.Me,
		IncludeNew: req.IncludeNew,
		Accept:     req.Accept,
		Uploads:    s.uploads,
		// Fetch stays false: this server cannot reach the mailbox, on purpose.
		Fetch: false,
	})
	if err != nil {
		// The failure is the previous spec not being reproducible — nothing
		// recorded to re-run, or a recorded selection that cannot be re-run.
		// That is the caller's spec being wrong, not the server failing.
		fail(w, http.StatusBadRequest, err)
		return
	}
	send(w, http.StatusOK, refreshResponse{Spec: next, Report: toRefreshReport(rep)})
}

// noMailbox is the browser-surface's mailbox: the one that cannot reach the
// mailbox. The server is read-only on purpose (the mailbox is the CLI's
// domain), so any path that would call it is a bug, made loud rather than
// silent.
type noMailbox struct{}

func (m noMailbox) Search(_ string, _ int, _ string) ([]mailingest.Envelope, mailingest.Page, error) {
	return []mailingest.Envelope{}, mailingest.Page{}, fmt.Errorf(
		"the server cannot reach the mailbox: fetch belongs to `corpus ingest`")
}

func (m noMailbox) Read(_ string) (mailingest.Message, error) {
	return mailingest.Message{}, fmt.Errorf(
		"the server cannot reach the mailbox: fetch belongs to `corpus ingest`")
}

func (m noMailbox) Thread(_ string) (mailingest.ThreadResult, error) {
	return mailingest.ThreadResult{}, fmt.Errorf(
		"the server cannot reach the mailbox: fetch belongs to `corpus ingest`")
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
