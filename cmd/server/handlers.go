package main

import (
	"context"
	"database/sql"
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
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/zachpmanson/chainmail/internal/corpus"
	mailembed "github.com/zachpmanson/chainmail/internal/embed"
	"github.com/zachpmanson/chainmail/internal/mailingest"
	"github.com/zachpmanson/chainmail/internal/refresh"
	"github.com/zachpmanson/chainmail/internal/spec"
	"github.com/zachpmanson/chainmail/internal/status"

	docketauth "github.com/zachpmanson/docket/gmail/auth"
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
	// statusPath is the connection snapshot the operator's probe wrote; the
	// server serves it read-only, so the credential checks stay where the
	// credentials are.
	statusPath string

	specSlots chan struct{}
	// slotWait is how long a caller waits for a slot before being told to retry.
	slotWait  time.Duration
	embedder  func() *mailembed.Ollama
	embedWait time.Duration

	// login holds a pending authorization-code flow started by /auth/login.
	// A process can host only one at a time by construction: starting another
	// while one is pending replaces it (single admin, 10-minute consent
	// window, and the callback verifies state before touching it).
	login        *docketauth.Pending
	loginExpires time.Time
	// loginPort is the port this server bound, used to spell the Google-
	// accepted pathless redirect URI http://localhost:<port>.
	loginPort string
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
	mux.HandleFunc("/v1/ops/plan", get(s.opsPlan))
	mux.HandleFunc("/v1/ops/merge", post(s.opsMerge))
	mux.HandleFunc("/v1/search", get(s.search))
	mux.HandleFunc("/v1/status", get(s.status))
	mux.HandleFunc("/v1/spec", post(s.spec))
	mux.HandleFunc("/v1/specs", get(s.savedSpecs))
	mux.HandleFunc("/v1/specs/{name}", get(s.savedSpec))
	mux.HandleFunc("/v1/entries/{extId}", get(s.entry))
	mux.HandleFunc("/v1/chains/{rootExtId}", get(s.chain))
	mux.HandleFunc("/v1/stats", get(s.stats))
	mux.HandleFunc("/v1/people", get(s.people))
	mux.HandleFunc("/auth/status", get(s.authStatus))
	mux.HandleFunc("/auth/login", get(s.authLogin))
	mux.HandleFunc("/", s.authCallbackOr(s.webRoot()))
	return mux
}

// webRoot serves the embedded web client alongside the API: the app shell at
// / and /index.html, static assets by name, and the client's routes — which
// are every other non-/v1/ path — as the shell. The client owns the routes
// (TanStack Router knows them all), so an unknown path reaching the shell
// here is not a bug but the point: the client renders its own 404 view, which
// is the only thing that can truthfully say "no page here".
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
	// but the same bytes must also answer for a bare /index.html and every
	// client route; open index.html directly so they all get the same headers.
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
		if p == "" || p == "index.html" {
			shell(w, r)
			return
		}
		if _, err := fs.Stat(sub, p); err == nil {
			fileServer.ServeHTTP(w, r)
			return
		}
		// Not a file: a client route, present or mistyped. Either way the shell
		// answers and the client decides what belongs there.
		shell(w, r)
	}
}

// authStatus reports whether a Google token is present in the store the
// slurps read (chainmail's own store — HOME=/var/lib/chainmail in the nix
// module). Deliberately shallow: a file check, not a live refresh, so the
// endpoint never reaches the network or blocks on a token exchange.
func (s *server) authStatus(w http.ResponseWriter, r *http.Request) {
	path, err := docketauth.TokenPath()
	signedIn := false
	if err == nil {
		if _, err := os.Stat(path); err == nil {
			signedIn = true
		}
	}
	send(w, http.StatusOK, authStatusResponse{SignedIn: signedIn})
}

// loginWindow is how long a pending authorization flow stays usable before
// the callback is refused.
const loginWindow = 10 * time.Minute

// authLogin starts a Google authorization-code flow for the work mailbox and
// redirects the browser to Google's consent page. The token lands in the same
// store the hourly slurp reads — chainmail's own — so once the callback
// completes, the next slurp runs with it.
//
// The redirect URI is a pathless http://localhost:<port>: Google matches
// loopback redirects by host+port for the Thunderbird client docket's config
// registers, so the callback arrives at this server's root with
// ?code=&state=. See docket-design.md §3.
func (s *server) authLogin(w http.ResponseWriter, r *http.Request) {
	cfg, err := docketauth.LoadConfig()
	if err != nil {
		fail(w, http.StatusInternalServerError,
			fmt.Errorf("loading docket config: %w", err))
		return
	}
	pending, err := docketauth.BeginLogin(cfg.Provider,
		fmt.Sprintf("http://localhost:%s", s.loginPort))
	if err != nil {
		fail(w, http.StatusInternalServerError,
			fmt.Errorf("starting authorization flow: %w", err))
		return
	}
	s.login = pending
	s.loginExpires = time.Now().Add(loginWindow)
	w.Header().Set("Location", pending.AuthURL())
	http.Error(w, "redirecting to Google…", http.StatusFound)
}

// authCallbackOr answers a Google consent callback when one is in flight and
// defers everything else to fallback. The callback is a GET on this server's
// root (?code=&state=...); every other root request — the shell, index.html,
// a client route — must fall through untouched.
func (s *server) authCallbackOr(fallback http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		code := q.Get("code")
		denied := q.Get("error")
		pending := s.login
		valid := pending != nil && q.Get("state") == pending.State &&
			time.Now().Before(s.loginExpires)
		if (code != "" || denied != "") && valid {
			s.finishLogin(w, code, denied)
			return
		}
		fallback(w, r)
	}
}

// finishLogin exchanges the code the consent page returned, persists the
// token into the store the slurps read, and shows a done page. state was
// already verified by authCallbackOr.
func (s *server) finishLogin(w http.ResponseWriter, code string, denied string) {
	pending := s.login
	s.login = nil
	if denied != "" {
		http.Error(w, "authorization denied: "+denied, http.StatusBadRequest)
		return
	}
	tok, err := docketauth.ExchangeCode(context.Background(), pending, code)
	if err != nil {
		fail(w, http.StatusInternalServerError,
			fmt.Errorf("exchanging code: %w", err))
		return
	}
	path, err := docketauth.TokenPath()
	if err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	if err := docketauth.SaveToken(tok, path); err != nil {
		fail(w, http.StatusInternalServerError,
			fmt.Errorf("saving token: %w", err))
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w,
		"<!doctype html><meta charset=\"utf-8\"><title>chainmail</title>"+
			"<p>Signed in to Google. You can close this tab.</p>")
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

// savedSpecs is the index half of the specs dir: every page POST /v1/spec
// saved, newest saved/refreshed first, so a saved build can be reopened
// without remembering its name. It surfaces only name, title and mtime per
// page — never a full page body — so the index stays cheap however many pages
// accumulate.
func (s *server) savedSpecs(w http.ResponseWriter, r *http.Request) {
	specs, err := s.listSpecs()
	if err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	if specs == nil {
		specs = []savedSpecSummary{}
	}
	send(w, http.StatusOK, specListResponse{Specs: specs})
}

// maxListedSpecs caps the index so it cannot balloon with every refresh of one
// page: each saved page is one entry no matter how often it is rewritten, but a
// page saved under dozens of distinct names still deserves a bounded list. The
// cap is generous because the dir only grows one row per distinct name.
const maxListedSpecs = 200

// listSpecs reads the server's specs dir and returns every saved page, newest
// (by mtime, i.e. last saved or refreshed) first.
func (s *server) listSpecs() ([]savedSpecSummary, error) {
	dir, err := os.ReadDir(s.specs)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // no saved pages yet: an empty list, not an error
		}
		return nil, err
	}
	out := make([]savedSpecSummary, 0, len(dir))
	for _, de := range dir {
		if de.IsDir() || !strings.HasSuffix(de.Name(), ".json") {
			continue
		}
		name := strings.TrimSuffix(de.Name(), ".json")
		if !validSpecName(name) {
			// A file that cannot name a route is not a saved page; don't list it.
			continue
		}
		info, err := de.Info()
		if err != nil {
			continue
		}
		out = append(out, savedSpecSummary{
			Name:    name,
			Title:   specTitleOf(filepath.Join(s.specs, de.Name())),
			SavedAt: stamp(info.ModTime()),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].SavedAt > out[j].SavedAt })
	if len(out) > maxListedSpecs {
		out = out[:maxListedSpecs]
	}
	return out, nil
}

// specTitleOf reads just the title out of a saved page — the one field the
// index surfaces before opening it. A page that reads or parses cleanly has a
// title by contract (timeline.schema.json marks it required); a page that does
// not is treated as titleless rather than breaking the whole index.
func specTitleOf(path string) string {
	blob, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var meta struct {
		Title string `json:"title"`
	}
	if err := json.Unmarshal(blob, &meta); err != nil {
		return ""
	}
	return meta.Title
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
//
// One mutation it does perform is the same twins sweep `corpus slurp` runs:
// a quoted copy stored before its mailbox original arrived is one message
// stored twice, and a page re-derived over them would show it twice. The
// sweep refuses rather than guesses, so a corpus with no twins is untouched.
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
		// Proposals run hybrid: the recorded queries are embedded so the
		// vector half joins discovery, and a semantic-only chain must clear
		// the model's chain floor before it is offered (see refresh.Options).
		Embed: s.embedder(),
	})
	if err != nil {
		// The failure is the previous spec not being reproducible — nothing
		// recorded to re-run, or a recorded selection that cannot be re-run.
		// That is the caller's spec being wrong, not the server failing.
		fail(w, http.StatusBadRequest, err)
		return
	}
	if req.Name != "" {
		// The saved page must not drift from what the client just received: the
		// refresh rewrites the file exactly as POST /v1/spec would, so a reload
		// of /view/<name> lands on this run, not the stale one.
		if err := s.saveSpec(req.Name, next); err != nil {
			fail(w, http.StatusInternalServerError,
				fmt.Errorf("saving the refreshed page as %q: %w", req.Name, err))
			return
		}
	}
	send(w, http.StatusOK, refreshResponse{Spec: next, Report: toRefreshReport(rep)})
}

// opsPlan is the review surface for people merges, all read-only: the dedupe
// plan the CLI's dry run prints (merges and refusals), the pairs MergeCandidates
// offers a human glance at, the twins pass's declined entries aggregated by
// reason, and the person_merges trail of merges so far. Nothing here changes
// the corpus, so a browser refetch is always a fresh view; the one mutation
// this surface owns is POST /v1/ops/merge, and the UI must call that for an
// apply.
func (s *server) opsPlan(w http.ResponseWriter, r *http.Request) {
	plan, err := corpus.Dedupe(s.store, false)
	if err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	out := opsPlanResponse{
		People:        int64(plan.Before),
		Merges:        make([]opsMerge, 0, len(plan.Merges)),
		Refusals:      make([]opsRefusal, 0, len(plan.Refusals)),
		Candidates:    make([]opsCandidate, 0, 8),
		TwinsDeclined: make([]twinsDecline, 0, 8),
		Trail:         make([]opsMergeRecord, 0, 8),
	}
	for _, m := range plan.Merges {
		out.Merges = append(out.Merges, opsMerge{
			Rule: m.Rule, KeepID: m.KeepID, KeepName: m.KeepName,
			KeepIdentities: m.KeepIDs, DropID: m.DropID, DropName: m.DropName,
			DropIdentities: m.DropIDs, Evidence: m.Evidence,
			Applicable: opsApplicable(m.Rule),
		})
	}
	for _, rf := range plan.Refusals {
		out.Refusals = append(out.Refusals, opsRefusal{
			Rule: rf.Rule, Subject: rf.Subject, Reason: rf.Reason, People: rf.People})
	}
	cs, _, err := corpus.MergeCandidates(s.store)
	if err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	for _, c := range cs {
		out.Candidates = append(out.Candidates, opsCandidate{
			AID: c.AID, AName: c.AName, AAddresses: c.AAddresses,
			BID: c.BID, BName: c.BName, BAddresses: c.BAddresses,
			Reason: c.Reason, Suggest: c.Suggest})
	}
	tps, err := corpus.CollapseTwins(s.store, false)
	if err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	byReason := map[string]int{}
	for _, d := range tps.Declined {
		byReason[d.Reason]++
	}
	out.TwinsDeclined = topTwinsDeclines(byReason)
	rows, err := s.store.DB().Query(`
		select pm.kept_id, p.display_name, pm.dropped_id, pm.dropped_name,
		       pm.reason, pm.merged_at
		  from person_merges pm left join people p on p.id = pm.kept_id
		 order by pm.merged_at desc, pm.kept_id desc`)
	if err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		rec, ok, err := readMergeRecord(rows)
		if err != nil {
			fail(w, http.StatusInternalServerError, err)
			return
		}
		if ok {
			out.Trail = append(out.Trail, rec)
		}
	}
	if err := rows.Err(); err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	send(w, http.StatusOK, out)
}

// opsMerge applies exactly one planned merge, and only one the current plan
// would make: the plan is re-derived here (a dry run and an apply of the same
// corpus produce the same plan, by the property documented on Dedupe), so a
// pair that is not in it — already applied, or the corpus changed since the
// screen loaded — is a 409 telling the client to refetch, not a retry.
//
// The apply surface is enforced server-side (see opsApplicable), so the
// same-name/same-thread boundary holds even against a hand-rolled request.
func (s *server) opsMerge(w http.ResponseWriter, r *http.Request) {
	var req opsMergeRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRequestBody))
	// A misspelled field is a caller reading a different contract; a plan that
	// was half-understood must not apply a merge.
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		fail(w, http.StatusBadRequest, fmt.Errorf("reading the request body: %w", err))
		return
	}
	if req.KeepID == 0 || req.DropID == 0 {
		fail(w, http.StatusBadRequest,
			errors.New("keepId and dropId are required, and neither may be 0"))
		return
	}
	plan, err := corpus.Dedupe(s.store, false)
	if err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	var m *corpus.PlannedMerge
	for i := range plan.Merges {
		if plan.Merges[i].KeepID == req.KeepID && plan.Merges[i].DropID == req.DropID {
			m = &plan.Merges[i]
		}
	}
	if m == nil {
		fail(w, http.StatusConflict, fmt.Errorf(
			"%d <- %d is not in the current dedupe plan — already merged, or the "+
				"corpus changed since this screen loaded; GET /v1/ops/plan for the plan now",
			req.KeepID, req.DropID))
		return
	}
	if !opsApplicable(m.Rule) {
		fail(w, http.StatusConflict, fmt.Errorf(
			"%s needs a human reading the whole corpus; the ops screen shows that tier "+
				"read-only", m.Rule))
		return
	}
	if err := corpus.MergePlanned(s.store, *m); err != nil {
		fail(w, http.StatusConflict, err)
		return
	}
	// The record person_merges just wrote, for the trail and the response. The
	// pair is unambiguous: the dropped row is gone, so a second write of the
	// same pair is impossible.
	rows, err := s.store.DB().Query(`
		select pm.kept_id, p.display_name, pm.dropped_id, pm.dropped_name,
		       pm.reason, pm.merged_at
		  from person_merges pm left join people p on p.id = pm.kept_id
		 where pm.kept_id=? and pm.dropped_id=?`, req.KeepID, req.DropID)
	if err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	defer rows.Close()
	if !rows.Next() {
		rows.Close()
		fail(w, http.StatusInternalServerError,
			errors.New("the merge wrote no person_merges row — nothing happened"))
		return
	}
	rec, ok, err := readMergeRecord(rows)
	rows.Close()
	if err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	if !ok {
		fail(w, http.StatusInternalServerError,
			errors.New("the merge wrote no person_merges row — nothing happened"))
		return
	}
	send(w, http.StatusOK, opsMergeResponse{Merge: rec})
}

// opsApplicable is the boundary the review UI's apply surface is drawn to: the
// two same-name tiers whose evidence a browser can hold up for a human. The
// first-name-and-org tier needs a human reading the whole corpus by design and
// the webmail tier is kept out beside it; both are shown read-only, and every
// tier stays one command away in the CLI (`corpus dedupe -apply`).
func opsApplicable(rule string) bool {
	return rule == corpus.RuleSameName || rule == corpus.RuleNameInThread
}

// readMergeRecord reads one person_merges row joined with its survivor's
// current display name. ok is false only for a NULL kept_id, which a left join
// yields when the survivor was deleted by hand — a record whose left side no
// longer exists is still part of the trail, so it is skipped rather than fatal.
func readMergeRecord(rows *sql.Rows) (opsMergeRecord, bool, error) {
	var rec opsMergeRecord
	var at int64
	var dropName, reason sql.NullString
	if err := rows.Scan(&rec.KeepID, &rec.KeepName, &rec.DropID,
		&dropName, &reason, &at); err != nil {
		return rec, false, err
	}
	if rec.KeepID == 0 {
		return rec, false, nil
	}
	if dropName.Valid {
		rec.DropName = dropName.String
	}
	if reason.Valid {
		rec.Reason = reason.String
	}
	rec.MergedAt = stamp(time.Unix(at, 0))
	return rec, true, nil
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

func (s *server) status(w http.ResponseWriter, r *http.Request) {
	// The snapshot is the operator's copy of the machine's state; a missing or
	// unparseable one is the unchecked grid, not an error. The server only ever
	// reads it — reaching docket or slackdump here would break the read-only
	// contract, so that stays the operator's probe's job.
	var snap status.Snapshot
	if blob, err := os.ReadFile(s.statusPath); err != nil {
		snap = status.Empty()
	} else {
		snap = status.Parse(blob)
	}
	send(w, http.StatusOK, toStatusResponse(snap))
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
