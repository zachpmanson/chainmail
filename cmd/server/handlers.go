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
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/zachpmanson/chainmail/internal/corpus"
	mailembed "github.com/zachpmanson/chainmail/internal/embed"
	"github.com/zachpmanson/chainmail/internal/gmailclient"
	"github.com/zachpmanson/chainmail/internal/mailingest"
	"github.com/zachpmanson/chainmail/internal/media"
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
	// mediaTimeout bounds one message's media pull. The work is a mailbox round
	// trip per attachment plus a decode, so it is seconds — bounded anyway,
	// because a request that ends must not leave a fetch half-done.
	mediaTimeout = 2 * time.Minute
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
	// corpusPath is the path passed to corpus.Open, kept so the slurp subprocess
	// can point CHAINMAIL_CORPUS at the same database this process has open.
	corpusPath string
	specs      string // dir for saved pages (POST /v1/spec writes here, GET /v1/specs reads)
	// statusPath is the connection snapshot the operator's probe wrote; the
	// server serves it read-only, so the credential checks stay where the
	// credentials are.
	statusPath string

	// slurpEnabled enables POST /v1/slurp: reach the work mailbox and ingest it,
	// so a page's refresh can build over mail that arrived since the hourly cron
	// run. Opt-in (`-slurp`), and when off the surface stays read-most and never
	// touches the mailbox. See defaultSlurp for the boundary the switch crosses.
	slurpEnabled bool
	// slurpTimeout bounds one ingest. A slurp walks the mail query, then twins,
	// repair, dedupe (reported, never applied) and embed, so it takes minutes
	// rather than seconds — and a request that ends must not leave that walk
	// half-done, which is what the context carries.
	slurpTimeout time.Duration
	// runSlurp is the mailbox-reaching operation, injected so the handler can be
	// tested without a mailbox. The real one delegates to the sibling `corpus`
	// binary (see defaultSlurp), so every phase and its ordering stays there.
	runSlurp func(ctx context.Context, corpusPath string) ([]byte, error)
	// runSweep is the same operation on the schedule's terms: the same pipeline
	// without dedupe, because a plan nobody is watching is work nobody reads.
	// Nil when this host has nothing to sweep with, which leaves the loop idle.
	runSweep func(ctx context.Context, corpusPath string) ([]byte, error)
	// sweeping is the one-ingest-at-a-time latch, shared by POST /v1/slurp and
	// the scheduler: they write the same tables from the same mailbox, and an
	// overlapping pair would be two walks of one query. See slurpOnce.
	sweeping atomic.Bool

	// mediaEnabled enables POST /v1/media/pull: fetch the bytes behind one
	// message's attachments, so the page can show a file instead of sending the
	// reader to Gmail for it. Opt-in (`-media`) and off by default — same posture
	// as -slurp, because it is the second thing here that reaches the mailbox,
	// once per attachment part. See defaultMediaPull for what the switch crosses.
	mediaEnabled bool

	// markReadEnabled is the -mark-read grant: whether POST /v1/read may change
	// the mailbox, rather than only read it. Off unless the host says otherwise,
	// and the one switch here whose absence is a 403 rather than a missing
	// feature — the read state lives in Gmail, and half of it is not a state.
	markReadEnabled bool
	// mailWriteEnabled is the -mail-write grant: whether POST /v1/mail may
	// archive, trash or move the messages of a chain. Off unless the host says so,
	// for the same reason and with the same 403: these change what is in the
	// mailbox, and the difference between this and -mark-read is which field of
	// the message changes rather than how far the server reaches.
	mailWriteEnabled bool
	// sendMailEnabled is the -send-mail grant: whether POST /v1/send may answer a
	// message in the mailbox. Off unless the host says so, for the same reason as
	// the other two writers and with the same 403 — and it is the third grant
	// rather than a piece of either of them, because a send is the one write here
	// that cannot be undone or retried: a label can be put back by the same button
	// and an archived thread is still in All Mail, while a message that has gone
	// out is out.
	sendMailEnabled bool
	// openMailbox opens the mailbox the writes go through, called once per
	// request and only when the chain has something writable in it, so a chain of
	// recovered text never needs a grant. Injected so the handlers are testable
	// without a mailbox (see mailbox).
	openMailbox func() (mailbox, error)
	// runMediaPull is one message's pull, injected so the handler can be tested
	// without a mailbox. The real one (defaultMediaPull) is the same
	// internal/media walk the `corpus media pull` command runs, called in-process
	// — a pull needs no phase ordering, so it needs no subprocess.
	runMediaPull func(ctx context.Context, entry string) (media.Result, error)

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

	// rev is the revision this binary was built from, passed in by whatever
	// started it (`-rev`), and startedAt is when this process came up. Together
	// they are the deploy stamp the header shows: what is running, and since when.
	// Neither is a fact about the corpus, which is why they are fields on the
	// server rather than anything the store is asked about.
	rev       string
	startedAt time.Time
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
	mux.HandleFunc("/v1/read", post(s.markRead))
	mux.HandleFunc("/v1/mail", post(s.mailAction))
	mux.HandleFunc("/v1/send", post(s.sendReply))
	mux.HandleFunc("/v1/media/pull", post(s.mediaPull))
	mux.HandleFunc("/v1/attachments/{sha}", get(s.attachment))
	// One person at a time, for the ops screen's own editor: a write is the shape
	// of the settings write — the fields it names are written, the ones it leaves
	// out are left alone — and it answers with the person as it now stands.
	mux.HandleFunc("/v1/people/{personId}", post(s.editPerson))
	mux.HandleFunc("/v1/ops/plan", get(s.opsPlan))
	mux.HandleFunc("/v1/ops/merge", post(s.opsMerge))
	// The colour rules: one path for the list and the write, because a rule is one
	// resource and every write answers with the rules as they now stand. The
	// preview beside it is a POST because it is a question about a set of rules
	// nobody has stored — there is nothing to GET.
	mux.HandleFunc("/v1/ops/orgs", methods(map[string]http.HandlerFunc{
		http.MethodGet:  s.opsOrgs,
		http.MethodPost: s.setOpsOrg,
	}))
	mux.HandleFunc("/v1/ops/orgs/preview", post(s.opsOrgsPreview))
	mux.HandleFunc("/v1/search", get(s.search))
	mux.HandleFunc("/v1/slurp", post(s.slurp))
	mux.HandleFunc("/v1/status", get(s.status))
	mux.HandleFunc("/v1/spec", post(s.spec))
	mux.HandleFunc("/v1/specs", get(s.savedSpecs))
	mux.HandleFunc("/v1/specs/{name}", get(s.savedSpec))
	mux.HandleFunc("/v1/entries/{extId}", get(s.entry))
	// The entry's own markup, under the entry rather than beside it: it is one
	// field of that resource (whether it exists is `original` on the reads), and
	// keeping it here means the corpus's id space is enough to ask for it — a
	// client never has to hold a second id for the same message.
	mux.HandleFunc("/v1/entries/{extId}/original", get(s.original))
	mux.HandleFunc("/v1/chains/{rootExtId}", get(s.chain))
	mux.HandleFunc("/v1/stats", get(s.stats))
	mux.HandleFunc("/v1/labels", get(s.labels))
	// The deploy stamp: what is running and since when. Served rather than baked
	// into the bundle because the bundle is the same bytes across deploys of one
	// revision — the thing that changes when a fix goes live is the process, and
	// a stamp baked at build time could not say so.
	mux.HandleFunc("/v1/version", get(s.version))
	// GET reads the settings, POST writes them: one path, because a preference is
	// one resource rather than a collection of endpoints.
	mux.HandleFunc("/v1/settings", methods(map[string]http.HandlerFunc{
		http.MethodGet:  s.getSettings,
		http.MethodPost: s.setSettings,
	}))
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
	//
	// The shell must not be cached without revalidation, and it must not be cached
	// *at all* by a client that cannot revalidate: it names the hashed bundle, so a
	// stale shell is an entire stale app — the reader reloads, sees the build from
	// before the fix, and has no way to tell. Nothing here can carry a validator
	// (an embedded file has no modtime), which is exactly why the header has to be
	// explicit rather than left to a heuristic.
	shell := func(w http.ResponseWriter, r *http.Request) {
		f, err := sub.Open("index.html")
		if err != nil {
			json404(w, r)
			return
		}
		defer f.Close()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		io.Copy(w, f)
	}
	// A hashed asset is immutable: the name is the content hash, so those bytes
	// under that name can never change, and re-fetching them per navigation is
	// paying for a promise the filename already made.
	assets := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		fileServer.ServeHTTP(w, r)
	})
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
			assets.ServeHTTP(w, r)
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

// methods routes one path's verbs, for the first path that answers more than
// one. The Allow header lists them in a fixed order so a caller reading it gets
// the same answer twice, and a verb not taken gets the same JSON error shape
// method does.
func methods(m map[string]http.HandlerFunc) http.HandlerFunc {
	allow := make([]string, 0, len(m))
	for verb := range m {
		allow = append(allow, verb)
	}
	sort.Strings(allow)
	return func(w http.ResponseWriter, r *http.Request) {
		h, ok := m[r.Method]
		if !ok {
			w.Header().Set("Allow", strings.Join(allow, ", "))
			fail(w, http.StatusMethodNotAllowed,
				fmt.Errorf("%s takes %s, not %s", r.URL.Path, strings.Join(allow, " or "), r.Method))
			return
		}
		h(w, r)
	}
}

func (s *server) search(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Query()
	text, person, since := p.Get("q"), p.Get("person"), p.Get("since")
	before := p.Get("before")
	// Every label named on the URL, not the first: the store takes a list, and a
	// caller asking for two folders is asking a question it can already answer.
	// An empty one is dropped rather than passed through — `label=` sent
	// literally would be a filter on a label nobody has, which is a different
	// (and silently empty) search from not filtering.
	labels := make([]string, 0, len(p["label"]))
	for _, l := range p["label"] {
		if l != "" {
			labels = append(labels, l)
		}
	}
	// With no filter at all this is the inbox: the corpus in the order it
	// arrived, which is a list a reader can act on without typing anything.
	// Deliberately not a ranking — the store orders a query with no ranking
	// input by each chain's newest message (corpus.Query.ranked) — and the
	// ranking modes still refuse it, because there is nothing to be similar to.
	// The old refusal here existed to stop an arbitrary slice of the corpus
	// coming back dressed as a ranked answer; as a list in time order it is not
	// dressed as anything.
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
	q := corpus.Query{Text: text, Limit: limit, Labels: labels}
	if since != "" {
		t, err := time.Parse("2006-01-02", since)
		if err != nil {
			fail(w, http.StatusBadRequest, fmt.Errorf("since %q: want YYYY-MM-DD", since))
			return
		}
		q.Since = t
	}
	if before != "" {
		t, err := time.Parse(time.RFC3339, before)
		if err != nil {
			fail(w, http.StatusBadRequest,
				fmt.Errorf("before %q: want an RFC 3339 timestamp", before))
			return
		}
		// The cursor includes its own second. Two chains can end in the same one
		// (a message and its twin, a pair of scheduled notifications), and a
		// strictly-older bound would drop whichever of them the page did not
		// reach — the failure that shows nothing at all, rather than the same
		// thread twice. Entries are stamped to the second, so a half-open
		// [., Until) one second later is exactly "ts <= before", and a caller
		// paging on the last row's `last` loses nothing.
		q.Until = t.Add(time.Second)
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
		// A pulled attachment renders from the corpus, so the web client sees the
		// same page a spec built on the host would — without the archive directory.
		Blobs: s.store.BlobBytes,
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
	blob, err := s.readSpec(name)
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

// readSpec is the bytes of a saved page: the read half of saveSpec, shared by
// GET /v1/specs/{name} and by a pull that has to bring the page it was asked
// for up to date.
func (s *server) readSpec(name string) ([]byte, error) {
	return os.ReadFile(filepath.Join(s.specs, name+".json"))
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

// slurp reaches the work mailbox and ingests it into the corpus, so a
// subsequent /v1/refresh can build a page over mail that arrived since the last
// ingest. It is the browser surface's door to `corpus slurp`, and it is the one
// thing here that writes to the corpus.
//
// Opt-in and off by default. Without -slurp the server keeps its read-most,
// never-touches-the-mailbox posture and this answers 403: handing a page the
// ability to fire a real mailbox ingest is switching that off, deliberately and
// per host (see defaultSlurp for what the switch crosses).
func (s *server) slurp(w http.ResponseWriter, r *http.Request) {
	if !s.slurpEnabled {
		fail(w, http.StatusForbidden, fmt.Errorf(
			"slurping is disabled: this server was started without -slurp, so it "+
				"cannot reach the work mailbox. A restart with -slurp enables POST /v1/slurp."))
		return
	}
	out, err := s.slurpOnce(r.Context(), s.runSlurp)
	if errors.Is(err, errSweepRunning) {
		// Asked for while the schedule (or another press) was already walking the
		// mailbox: the answer is that the work is being done, not that it failed.
		fail(w, http.StatusConflict, fmt.Errorf(
			"a sweep is already running: the mailbox is being ingested right now, "+
				"and this page will be built over what that run brings in"))
		return
	}
	if err != nil {
		fail(w, http.StatusBadGateway, fmt.Errorf("slurp failed: %w", err))
		return
	}
	send(w, http.StatusOK, slurpResponse{Report: string(out)})
}

// slurpResponse is the outcome of POST /v1/slurp: the ingest's own text, the
// per-phase lines the CLI prints. It is returned rather than logged because the
// page shows it — what was fetched is the reason someone pressed the button,
// and a phase that found nothing is worth seeing too.
type slurpResponse struct {
	Report string `json:"report"`
}

// defaultSlurp returns a function that runs `corpus slurp` against the corpus
// and returns the ingest transcript.
//
// It delegates to the sibling `corpus` binary rather than re-implementing the
// phases: the ingest order, fail-closed threading check, dedupe-as-dry-run,
// embed-skip reporting and connection-snapshot probe all live there, and the
// server shares none of that logic. The sibling ships beside this binary in the
// same nix package (corpus lands next to chainmail-server in $out/bin).
//
// Mail reaches the mailbox the way every other ingest on this host does — the
// in-process library reading the OAuth grant in this unit's own HOME, which the
// nix module points at the state directory the server and the slurp units share.
// That is what makes the switch cheap to grant: -slurp asks for no credential
// the server did not already have, and `-backend` needs no spelling out because
// the ingest and the server are one package with one default.
//
// Which phases run is the caller's choice, in one list, because the two callers
// differ by exactly one phase: `manualPhases` is what a person pressing the
// button gets — the dedupe plan is part of what they asked to see, and it stays
// a dry run either way (see cmd/corpus/slurp.go) — and `sweepPhases` is the same
// pipeline without it, since a plan printed into the journal of a sweep nobody
// is watching is work nobody reads. Both include unread, which is the phase that
// makes a scheduled sweep worth having: it fixes the badges a phone has moved on
// from, and the person most likely to notice is the one looking at that row.
//
// CHAINMAIL_CORPUS pins the same database this process has open; being WAL, the
// ingest writes beside the reader.
const (
	manualPhases = "mail,twins,repair,dedupe,unread,embed"
	sweepPhases  = "mail,twins,repair,unread,embed"
)

func defaultSlurp(phases string) func(ctx context.Context, corpusPath string) ([]byte, error) {
	return func(ctx context.Context, corpusPath string) ([]byte, error) {
		corpus, err := siblingBin("corpus")
		if err != nil {
			return nil, err
		}
		args := []string{"slurp", "-q", "in:anywhere", "-only", phases}
		cmd := exec.CommandContext(ctx, corpus, args...)
		cmd.Env = append(os.Environ(), "CHAINMAIL_CORPUS="+corpusPath)
		return cmd.CombinedOutput()
	}
}

// siblingBin resolves a command installed beside this server's own binary — in
// the nix package both `corpus` and `chainmail-server` land in $out/bin — so
// the server can hand the ingest to the real CLI wherever it is installed.
// Falls back to PATH, for a `go run` dev build with no sibling.
func siblingBin(name string) (string, error) {
	if exe, err := os.Executable(); err == nil {
		if p, err := exec.LookPath(filepath.Join(filepath.Dir(exe), name)); err == nil {
			return p, nil
		}
	}
	if p, err := exec.LookPath(name); err == nil {
		return p, nil
	}
	return "", fmt.Errorf("cannot find the %q binary to run slurp; it is not beside this server and not on PATH", name)
}

// mediaPull fetches the bytes behind one message's attachments: the button
// under a message's chips, and the only way this surface can show a file rather
// than link back to Gmail for it.
//
// Opt-in and off by default, like -slurp and for the same reason. Without
// -media the surface keeps its read-most posture and answers 403, naming the
// switch: granting it hands a page the ability to spend mailbox round trips,
// which is a per-host decision (see defaultMediaPull for what it crosses).
//
// One message per call, deliberately. Each attachment costs a mailbox round
// trip, so the browser is the narrow end of the scope the CLI takes — a whole
// thread is a conversation-sized decision a person makes with `corpus media
// pull -container`, not something a page offers on one click.
func (s *server) mediaPull(w http.ResponseWriter, r *http.Request) {
	if !s.mediaEnabled {
		fail(w, http.StatusForbidden, fmt.Errorf(
			"media pulls are disabled: this server was started without -media, so it "+
				"will not fetch attachment bytes. A restart with -media enables POST /v1/media/pull."))
		return
	}
	var req mediaPullRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRequestBody))
	// A misspelled field is a caller reading a different contract, and this call
	// spends mailbox round trips — refusing is cheaper than guessing.
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		fail(w, http.StatusBadRequest, fmt.Errorf("reading the request body: %w", err))
		return
	}
	entry := strings.TrimSpace(req.Entry)
	if entry == "" {
		fail(w, http.StatusBadRequest, errors.New(
			"a pull needs an entry: POST /v1/media/pull takes {\"entry\": \"mail:<...>\"}, "+
				"the extId the spec carries on the message whose files are wanted"))
		return
	}
	// The page name is a path segment on the way back out, so it is checked
	// before any bytes are spent rather than when the rebuild tries to write.
	if req.Name != "" && !validSpecName(req.Name) {
		fail(w, http.StatusBadRequest, fmt.Errorf(
			"name %q: use letters, digits, '.' '_' '-' (no slashes, no '..')", req.Name))
		return
	}
	// The entry must exist. That keeps a typo from answering "pulled nothing" as
	// though the mailbox had said so, and it is the 404 the rest of the surface
	// already answers with.
	if _, err := s.store.Show(entry); err != nil {
		failLookup(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), mediaTimeout)
	defer cancel()
	started := time.Now()
	res, err := s.runMediaPull(ctx, entry)
	if err != nil {
		fail(w, http.StatusBadGateway, fmt.Errorf("media pull failed: %w", err))
		return
	}
	// The journal keeps the trace the browser cannot be relied on for. A pull
	// spends mailbox round trips, and whoever asked for it may already have
	// navigated away — so "did it run, and what did it say" has to be
	// answerable from the host, the same way a spec build is.
	log.Printf("media: %s wanted=%d pulled=%d skipped=%d failed=%d in %s",
		entry, res.Wanted, res.Pulled, res.Skipped, res.Failed,
		time.Since(started).Round(time.Millisecond))
	for _, it := range res.Items {
		switch {
		case it.Reason != "":
			log.Printf("media: %s: %s declined: %s", entry, it.Name, it.Reason)
		case it.Err != nil:
			log.Printf("media: %s: %s failed: %v", entry, it.Name, it.Err)
		}
	}
	out := toMediaResponse(res)
	// A page that asked for files is handed its page back up to date, here
	// rather than in the browser: the bytes are only visible on a re-derived
	// page, and the one who pressed the button may have reloaded, navigated or
	// closed the tab by now. The spec is read from disk — the saved page is
	// this call's subject, so the server needs nothing from the client but its
	// name, and a caller that vanishes still leaves the page correct.
	if req.Name != "" {
		if blob, err := s.readSpec(req.Name); err != nil {
			log.Printf("media: %s: page %q: %v", entry, req.Name, err)
		} else {
			page := refreshRequest{Name: req.Name}
			if err := json.Unmarshal(blob, &page.Spec); err != nil {
				log.Printf("media: %s: page %q: reading the saved spec: %v", entry, req.Name, err)
			} else if next, rep, err := s.rebuildPage(page); err != nil {
				// The pull succeeded and the bytes are in the corpus; a page that
				// would not re-derive is this call's bad news, not a failed
				// fetch, and the client still has its own refresh to fall back on.
				log.Printf("media: %s: page %q: %v", entry, req.Name, err)
			} else {
				report := toRefreshReport(rep)
				out.Spec, out.Report = &next, &report
			}
		}
	}
	send(w, http.StatusOK, out)
}

// mailbox is the mailbox write this server makes: mark one message read or
// unread, or move it between labels — and in both cases answer with the labels
// the mailbox reports afterwards.
//
// An interface rather than the concrete client, for the reason runMediaPull is a
// field: the handler's own contract is which message changes and what is stored
// afterwards, and that is testable without a mailbox, a grant or a network.
// Returning the labels rather than taking them is the whole point — the local
// copy is what the mailbox said, not what the caller assumed it would say.
//
// One interface for the writes rather than three, because they are one power:
// the server is allowed to change what is in the mailbox. Reading state and
// folder are different fields of the same message, and a reply is a new message
// in it — a host that granted one but not another would be a distinction without
// a difference to the reader, and the grants that DO differ are separate switches
// on the server rather than separate methods here.
//
// Read is on it because the reply is filed into the corpus in the same request
// that sends it (see sendReply), and the ingest path takes a mailbox — so the
// one write's own read and the file-it-away that follows it come through one
// transport here, rather than two built from the same credential.
type mailbox interface {
	SetUnread(id string, unread bool) ([]string, error)
	SetLabels(id string, add, remove []string) ([]string, error)
	Reply(id string, body gmailclient.ReplyBody, all, send bool) (gmailclient.ReplyPlan, error)
	Read(id string) (mailingest.Message, error)
}

// markRead is the one write this server makes to the mailbox itself: every
// message of a chain is marked read or unread in Gmail, and the labels the
// mailbox returns are stored beside them in the same pass.
//
// Opt-in and off by default, like -slurp and -media, and for a stronger reason
// than either: those spend mailbox round trips, this changes what is in the
// mailbox. A host that has not granted -mark-read answers 403, naming the
// switch.
//
// Chain-level rather than per message, because a chain is what the list shows
// and the pane reads: resolving it is the store's walk down the reply graph
// (ChainEntries), which a browser cannot do with the entries it happens to hold
// — the row it draws carries three of them and no others.
//
// Messages with no mailbox copy are counted and reported, never failed: a
// message recovered from somebody's quote, or a Slack post, is a real part of
// the chain and has no Gmail id to change. A chain of nothing else answers
// marked: 0, which is the truth about it.
func (s *server) markRead(w http.ResponseWriter, r *http.Request) {
	if !s.markReadEnabled {
		fail(w, http.StatusForbidden, fmt.Errorf(
			"marking mail read is disabled: this server was started without -mark-read, so "+
				"it will not change anything in the mailbox. A restart with -mark-read enables "+
				"POST /v1/read."))
		return
	}
	var req markReadRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRequestBody))
	// A misspelled field is a caller reading a different contract, and this call
	// writes to a real mailbox — refusing is cheaper than guessing.
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		fail(w, http.StatusBadRequest, fmt.Errorf("reading the request body: %w", err))
		return
	}
	chain := strings.TrimSpace(req.Chain)
	if chain == "" {
		fail(w, http.StatusBadRequest, errors.New(
			"a read-state change needs a chain: POST /v1/read takes "+
				"{\"chain\": \"mail:<...>\", \"unread\": false}, the root extId a chain hit "+
				"carries as rootExtId"))
		return
	}

	entries, err := s.store.ChainEntries(chain)
	if err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	if len(entries) == 0 {
		fail(w, http.StatusNotFound, fmt.Errorf(
			"no chain at %q: no entry has that extId, so there is nothing to mark", chain))
		return
	}

	markable := 0
	for _, e := range entries {
		if e.GmailID != "" {
			markable++
		}
	}
	// Opened only when there is something to change. A chain of recovered
	// messages is answered without reaching the mailbox at all, which is also
	// what keeps this endpoint usable on a host that has no grant but does have
	// such a chain on screen.
	var mb mailbox
	if markable > 0 {
		if mb, err = s.openMailbox(); err != nil {
			fail(w, http.StatusBadGateway, fmt.Errorf("opening the mailbox: %w", err))
			return
		}
	}

	out := markReadResponse{Chain: chain, Unread: req.Unread}
	for _, e := range entries {
		if e.GmailID == "" {
			out.Skipped++
			continue
		}
		labels, err := mb.SetUnread(e.GmailID, req.Unread)
		if err != nil {
			// The count already marked is in the error because a chain is more
			// than one message: the caller has to know whether the chain is half
			// changed, and the mailbox's own error says nothing about that.
			fail(w, http.StatusBadGateway, fmt.Errorf(
				"the mailbox refused %s: %w (%d of %d messages in this chain were changed before this)",
				e.ExtID, err, out.Marked, markable))
			return
		}
		if err := s.store.SetLabels(e.ID, labels); err != nil {
			fail(w, http.StatusInternalServerError, fmt.Errorf(
				"the mailbox changed %s but storing its labels failed: %w", e.ExtID, err))
			return
		}
		out.Marked++
	}

	// Journaled like a media pull, and for the same reason: this changes the
	// reader's real mailbox, so "did it run, and what did it say" has to be
	// answerable from the host after the tab is gone.
	log.Printf("read: %s unread=%t marked=%d skipped=%d",
		chain, req.Unread, out.Marked, out.Skipped)
	send(w, http.StatusOK, out)
}

// mailAction is the second write this server makes to the mailbox: a chain's
// messages are archived, trashed or moved to a label, and the labels the mailbox
// returns are stored beside them in the same pass.
//
// Opt-in and off by default, like -mark-read and for the same reason, with its
// own switch rather than the same one: the reader who wants the read circle to
// work has not thereby asked the page to be able to delete their mail. A host
// without -mail-write answers 403, naming the switch.
//
// Chain-level plural, because the bar acts on what is ticked and a reader who
// ticked four threads meant four. Every chain is resolved *before* anything is
// written: a set with one unknown id in it is a caller working from a stale list,
// and half-applying that would leave the reader with a mailbox that matches
// neither the list they were looking at nor the one they will see next.
//
// What each action means is the mailbox's own vocabulary, not this server's:
// archive is the removal of INBOX and nothing else, trash is the Trash label,
// and a move is the target label plus the way out of the inbox. A message that
// already carries the label is changed by the same call and counts the same:
// "it is in Work" is the outcome, and whether it was there before is not
// something the reader asked.
//
// Messages with no mailbox copy — recovered from somebody's quote, or a Slack
// post — are counted and reported, never failed, exactly as in /v1/read.
func (s *server) mailAction(w http.ResponseWriter, r *http.Request) {
	if !s.mailWriteEnabled {
		fail(w, http.StatusForbidden, fmt.Errorf(
			"changing mail is disabled: this server was started without -mail-write, so "+
				"it will not archive, trash or move anything. A restart with -mail-write "+
				"enables POST /v1/mail."))
		return
	}
	var req mailActionRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRequestBody))
	// A misspelled field is a caller reading a different contract, and this call
	// changes a real mailbox — refusing is cheaper than guessing.
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		fail(w, http.StatusBadRequest, fmt.Errorf("reading the request body: %w", err))
		return
	}

	action, ok := mailActions[req.Action]
	if !ok {
		fail(w, http.StatusBadRequest, fmt.Errorf(
			"unknown action %q: POST /v1/mail takes one of archive, trash or move", req.Action))
		return
	}
	// A move names where it goes. An empty label list is not an error to the
	// mailbox — it is leaving the inbox with no destination — so it is refused
	// here, where the message can say what to do instead.
	if action.needsLabels && len(req.Labels) == 0 {
		fail(w, http.StatusBadRequest, errors.New(
			"a move needs labels: POST /v1/mail takes {\"chains\": [...], \"action\": "+
				"\"move\", \"labels\": [\"Work\"]} — the word the mailbox knows, not a folder id"))
		return
	}
	if len(req.Chains) == 0 {
		fail(w, http.StatusBadRequest, errors.New(
			"a mail change needs chains: POST /v1/mail takes the rootExtId of every chain "+
				"hit that was ticked"))
		return
	}

	add, remove := action.labels(req.Labels)

	// Resolve every chain first. The mailbox is not touched until all of them are
	// known to exist, so a stale id leaves the mailbox exactly as it was.
	trail := make([][]corpus.ChainEntry, 0, len(req.Chains))
	for _, chain := range req.Chains {
		entries, err := s.store.ChainEntries(chain)
		if err != nil {
			fail(w, http.StatusInternalServerError, err)
			return
		}
		if len(entries) == 0 {
			fail(w, http.StatusNotFound, fmt.Errorf(
				"no chain at %q: no entry has that extId, and nothing has been changed", chain))
			return
		}
		trail = append(trail, entries)
	}

	// The mailbox is opened only when there is something in it to change: a set of
	// chains that are all recovered text is answered without a grant and without a
	// round trip, which is what keeps this usable on a mailbox-less host.
	writable := 0
	for _, entries := range trail {
		for _, e := range entries {
			if e.GmailID != "" {
				writable++
			}
		}
	}
	var mb mailbox
	if writable > 0 {
		var err error
		if mb, err = s.openMailbox(); err != nil {
			fail(w, http.StatusBadGateway, fmt.Errorf("opening the mailbox: %w", err))
			return
		}
	}

	out := mailActionResponse{Action: req.Action, Chains: make([]mailActionChain, 0, len(req.Chains))}
	if action.needsLabels {
		out.Labels = append([]string(nil), req.Labels...)
	}
	for i, entries := range trail {
		row := mailActionChain{RootExtID: req.Chains[i]}
		for _, e := range entries {
			if e.GmailID == "" {
				row.Skipped++
				out.Skipped++
				continue
			}
			labels, err := mb.SetLabels(e.GmailID, add, remove)
			if err != nil {
				// The counts are in the error because a chain is more than one
				// message, and because this call may be part way through several
				// chains: "what has already changed" is the first thing the caller
				// needs to know and the mailbox's error says nothing about it.
				fail(w, http.StatusBadGateway, fmt.Errorf(
					"the mailbox refused %s: %w (%d of %d messages changed before this, %d chains done)",
					e.ExtID, err, out.Changed, writable, i))
				return
			}
			if err := s.store.SetLabels(e.ID, labels); err != nil {
				fail(w, http.StatusInternalServerError, fmt.Errorf(
					"the mailbox changed %s but storing its labels failed: %w", e.ExtID, err))
				return
			}
			row.Changed++
			out.Changed++
		}
		out.Chains = append(out.Chains, row)
	}

	// Journaled like a media pull and a read-state write, and for the same reason:
	// this changes the reader's real mailbox, so "did it run, and what did it say"
	// has to be answerable from the host after the tab is gone. The labels are
	// named for a move because which folder it went to is the whole question.
	if action.needsLabels {
		log.Printf("mail: %s %d chains labels=%s changed=%d skipped=%d",
			req.Action, len(req.Chains), strings.Join(req.Labels, ","), out.Changed, out.Skipped)
	} else {
		log.Printf("mail: %s %d chains changed=%d skipped=%d",
			req.Action, len(req.Chains), out.Changed, out.Skipped)
	}
	send(w, http.StatusOK, out)
}

// mailActionSpec is what one action does to a message, as label names in the
// mailbox's own vocabulary (see gmailclient.InboxLabel). Every action removes
// INBOX except trash, which puts the message in the Trash and takes it out of
// the inbox in the same write — the two are not independent: a message can
// carry both, and one that is in the Trash should not also be in the inbox.
type mailActionSpec struct {
	needsLabels bool
	labels      func(labels []string) (add, remove []string)
}

// mailActions is the vocabulary of POST /v1/mail, spelled once: the handler's
// validation, its 403 message and the contract in api/openapi.json all name
// these words.
var mailActions = map[string]mailActionSpec{
	"archive": {labels: func([]string) ([]string, []string) {
		return nil, []string{gmailclient.InboxLabel}
	}},
	"trash": {labels: func([]string) ([]string, []string) {
		return []string{gmailclient.TrashLabel}, []string{gmailclient.InboxLabel}
	}},
	"move": {
		needsLabels: true,
		labels: func(labels []string) ([]string, []string) {
			return labels, []string{gmailclient.InboxLabel}
		},
	},
}

// mailActionRequest is what a caller may say: which chains, what to do to them,
// and — for a move — where. Labels are the mailbox's names, which is also what
// /v1/labels serves, so a caller never has to know a folder id.
type mailActionRequest struct {
	Chains []string `json:"chains"`
	Action string   `json:"action"`
	Labels []string `json:"labels,omitempty"`
}

// mailActionResponse is the contract's MailActionResponse: what changed, in
// total and per chain, and what could not be changed because it has no mailbox
// copy. Per chain as well as in total because the reader ticked a set: a summary
// that says 12 changed cannot tell them which of the four threads did not move.
type mailActionResponse struct {
	Action  string            `json:"action"`
	Labels  []string          `json:"labels,omitempty"`
	Changed int               `json:"changed"`
	Skipped int               `json:"skipped"`
	Chains  []mailActionChain `json:"chains"`
}

// mailActionChain is one chain's outcome: the root the caller named, and how
// much of its trail the mailbox holds.
type mailActionChain struct {
	RootExtID string `json:"rootExtId"`
	Changed   int    `json:"changed"`
	Skipped   int    `json:"skipped"`
}

// sendReply is the third write this server makes to the mailbox, and the only one
// that creates something: one message is answered, and the answer is a new
// message in the thread.
//
// Opt-in and off by default, with its own switch (-send-mail), for a stronger
// reason than the two writes beside it: both of those change a field of mail that
// is already there and can be changed back from the same control, while this one
// cannot be undone at all. A host without the grant answers 403, naming it.
//
// Reply-ALL, and the shape follows from that rather than from a reluctance to
// build a composer. There is no recipient field anywhere in this request: the
// message answered is named by its corpus id and everything else — who it goes to
// and the subject — comes from the mailbox's own headers (see gmailclient.Reply).
// A surface that can only answer mail the corpus already holds has no arbitrary
// recipient, which is what makes it a reading tool that can reply rather than a
// mail-sending endpoint sitting inside a page that binds to loopback with no
// authentication.
//
// Answering everyone the message was addressed to is the same property rather
// than a widening of it: the audience is the original message's own To and Cc,
// minus every address the mailbox doing the replying owns — which the mailbox
// itself decides, because the mail a reader answers is usually addressed to a
// send-as alias and that is exactly the address a guessed list would CC them on
// (docket reads the account's profile and its aliases; see gmailclient.Reply). So
// the set a reply can reach is not one the caller composes or the reader types:
// it is the set the answered message already carried, which is what keeps
// "nowhere but back down a thread that is already in the corpus" true.
//
// Whether the rest of that audience is on the reply is the one thing the caller
// decides (`all`), because it is a choice about the conversation rather than about
// deliverability: answering a dozen people when one asked you something is a
// different act from answering the person who asked. Either way the addresses are
// the answered message's own, so the property above holds for both settings.
//
// Two steps, and the first one sends nothing: without `confirm` the reply is
// PREPARED and answered with the plan — the recipients the mailbox will use, the
// subject, and the whole body including the quote of the message being answered.
// That is the preview the reader confirms, and it is of the message rather than of
// a draft, because the body the reader is shown is the body that was handed to the
// mailbox. The second call repeats the prepare and executes it.
func (s *server) sendReply(w http.ResponseWriter, r *http.Request) {
	if !s.sendMailEnabled {
		fail(w, http.StatusForbidden, fmt.Errorf(
			"answering mail is disabled: this server was started without -send-mail, so "+
				"it will not send anything. A restart with -send-mail enables POST /v1/send."))
		return
	}
	var req sendRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRequestBody))
	// A misspelled field is a caller reading a different contract, and `confirm` is
	// the field that decides whether this call writes to a real mailbox: guessing at
	// an unknown one is the one place a typo sends mail.
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		fail(w, http.StatusBadRequest, fmt.Errorf("reading the request body: %w", err))
		return
	}
	if strings.TrimSpace(req.Entry) == "" {
		fail(w, http.StatusBadRequest, errors.New(
			"a reply answers one message: POST /v1/send takes {\"entry\": "+
				"\"mail:<message-id>\", \"body\": \"...\"}, naming the entry as the thread "+
				"read carries it"))
		return
	}
	if strings.TrimSpace(req.Body) == "" {
		fail(w, http.StatusBadRequest, errors.New(
			"a reply needs something to say: the quote of the message being answered is "+
				"added by this server, and an empty body would send that alone"))
		return
	}

	target, err := s.store.ReplyTarget(req.Entry)
	if err != nil {
		failLookup(w, err)
		return
	}
	if target.GmailID == "" {
		// The opposite of /v1/read and /v1/mail, where an entry with no mailbox copy
		// is skipped and counted: those act on the part of a set that can be acted
		// on, and this cannot be half-performed. There is no mailbox message to
		// thread an answer against — a message recovered from somebody's quote is
		// part of the chain and is not a thing the mailbox holds.
		fail(w, http.StatusBadRequest, fmt.Errorf(
			"%q has no mailbox copy, so there is nothing to answer: it was recovered from "+
				"somebody else's message (or is not mail at all), and a reply threads against "+
				"the message the mailbox holds", req.Entry))
		return
	}

	// The body, quoted and attributed, composed here rather than by the caller: what
	// the plan previews and what the mailbox is handed are then the same bytes, and
	// a second client cannot compose a quote this package would have written
	// differently (see spec.ComposeReply). Both forms come out of the same call, so
	// the text part and the HTML part of one reply are two renderings of one message
	// rather than two messages that happen to agree: the reader's words, the heading
	// and the reader's own half of the message are written once, and only the quote is
	// read twice — from each form the answered message was sent in, which is the point
	// of quoting it that way.
	body := spec.ComposeReply(req.Body, target)

	// Absent means everyone, which is what this endpoint has always answered: a
	// client that has not been rebuilt must not silently start answering one person
	// instead of the message's whole audience.
	all := req.All == nil || *req.All

	// Both renderings are composed and one may be dropped here rather than asked
	// for separately: the HTML is built from the same reading of the words as the
	// text (see spec.ComposeReply), so a call that does not send it has still been
	// composed as the pair it is, and the two can never be two messages wearing one
	// envelope.
	html := req.HTML == nil || *req.HTML
	if !html {
		body.HTML = ""
	}

	mb, err := s.openMailbox()
	if err != nil {
		fail(w, http.StatusBadGateway, fmt.Errorf("opening the mailbox: %w", err))
		return
	}
	plan, err := mb.Reply(target.GmailID, gmailclient.ReplyBody{
		Text: body.Text, HTML: body.HTML,
	}, all, req.Confirm)
	if err != nil {
		// Which half failed is the whole of what a reader can act on, and the two
		// cases demand the opposite thing: a prepare that failed sent nothing, so
		// pressing send again is safe, while a send that did not answer may have
		// gone out and pressing send again might send it twice.
		if errors.Is(err, gmailclient.ErrUnsent) {
			fail(w, http.StatusBadGateway, fmt.Errorf(
				"the mailbox would not prepare the reply, so nothing was sent: %w", err))
			return
		}
		fail(w, http.StatusBadGateway, fmt.Errorf(
			"the mailbox did not answer the send, so whether it went out is not known from "+
				"here — check Gmail before sending it again: %w", err))
		return
	}

	out := sendResponse{
		Entry: target.ExtID, To: plan.To, Cc: plan.Cc, Subject: plan.Subject, Body: plan.Body,
		Sent: plan.GmailID != "",
	}
	if out.Sent {
		out.GmailID = plan.GmailID
		s.fileSent(mb, plan.GmailID)
	}
	// Journaled like the other two writes, and for a stronger reason: this one
	// cannot be taken back, so "what did the server send, and to whom" has to be
	// answerable from the host afterwards. Who it reached is part of that, so the
	// Cc is in the line rather than only the To.
	log.Printf("send: %s to=%s cc=%s html=%t sent=%t", target.ExtID, plan.To, plan.Cc, html, out.Sent)
	send(w, http.StatusOK, out)
}

// fileSent puts a message the server has just sent into the corpus, so the answer
// is in the trail the reader is looking at rather than missing until the next
// slurp — which is up to a cadence away, and on a thread the reader is reading
// right now that reads as the reply having failed.
//
// Failures are logged, never raised. The message is already sent: answering 502
// here would tell the reader their reply did not go out when it did, which is the
// one wrong thing this handler could say. The mailbox is the source of truth and
// the reply is in it, so a corpus that missed it holds a gap the next ingest
// fills, and the ingest is idempotent by body hash.
func (s *server) fileSent(mb mailbox, id string) {
	msg, err := mb.Read(id)
	if err != nil {
		log.Printf("send: sent %s but could not read it back to file it: %v", id, err)
		return
	}
	res, err := mailingest.Put(s.store, msg)
	if err != nil {
		log.Printf("send: sent %s but storing it failed: %v", id, err)
		return
	}
	// Resolve after the put, not before: the reply carries the In-Reply-To the
	// mailbox built for it (gmailclient.Reply), and this is what joins the answer to
	// the message it answers in the trail's own reply graph rather than leaving it a
	// chain of its own.
	if _, err := s.store.ResolveParents(); err != nil {
		log.Printf("send: filed %s but joining it to its thread failed: %v", id, err)
		return
	}
	log.Printf("send: filed %s (created=%t skipped=%t)", id, res.Created, res.Skipped)
}

// sendRequest is what a caller may say, and it is the whole of the surface: which
// message is being answered, the reader's own words, and whether this call is the
// preview or the send.
type sendRequest struct {
	// Entry is the corpus ext id of the message being answered — as a thread read
	// carries it in entries[].extId. One entry rather than a chain, because a reply
	// is to a message: which thread it belongs to is the mailbox's answer, by way of
	// the In-Reply-To it threads on.
	Entry string `json:"entry"`
	// Body is the reader's own words, with no quote in them: the quoted message and
	// its attribution are added by this server, and the answer's body is what the
	// two make together.
	Body string `json:"body"`
	// Confirm false asks what the reply would be; true sends it. Nothing is written
	// without it, which is why a client that forgets the field previews rather than
	// sends.
	Confirm bool `json:"confirm,omitempty"`
	// All answers everyone the message was addressed to — its sender in To, the rest
	// of its audience in Cc — rather than the sender alone. Absent means everyone,
	// so the older shape of this request keeps the answer it always had.
	//
	// It is not a recipient list and cannot become one: which addresses are on the
	// reply is still the answered message's own headers, and this only says whether
	// the ones beyond its sender are on it. There is no value of this field that
	// reaches an address the message did not carry.
	All *bool `json:"all,omitempty"`
	// HTML says whether the reply carries the HTML part of it, the same message
	// marked up, beside the plain text (see spec.ComposeReply, which composes the
	// two together). Absent means it does, which is what this endpoint has always
	// sent, so the older shape of this request keeps the message it always sent.
	//
	// What is on the wire is one message either way: the words and the quote are
	// not the caller's to supply, and this only decides whether the second
	// rendering of them travels with the first. A reader who knows their
	// correspondent reads plain text can say so, and the text part is byte for byte
	// the one they would have had anyway.
	HTML *bool `json:"html,omitempty"`
}

// sendResponse is the contract's SendResponse: the reply as the mailbox has it,
// and whether this call sent it.
//
// To, Cc, Subject and Body are the mailbox's, not the caller's: the recipients and
// the subject come from the headers of the message being answered, and the body is
// the reader's words with the quote under them. The same four are answered by the
// preview and by the send, so a client that showed a reader a plan can be checked
// against what actually went out rather than trusting that it did.
type sendResponse struct {
	Entry string `json:"entry"`
	To    string `json:"to"`
	// Cc is everyone else the answered message was addressed to, absent when it
	// went to the reader alone. Absent rather than empty: a client drawing "cc "
	// with nothing after it would be describing a recipient list it does not have.
	Cc      string `json:"cc,omitempty"`
	Subject string `json:"subject"`
	Body    string `json:"body"`
	Sent    bool   `json:"sent"`
	// GmailID is the id of the message that went out, and is absent from a preview:
	// nothing has an id until it exists.
	GmailID string `json:"gmailId,omitempty"`
}

// markReadRequest is what a caller may say: which chain, and the state it wants
// every mailbox message in it to be left in. No message list and no "mark all":
// the chain IS the scope, and the store resolves it.
type markReadRequest struct {
	Chain  string `json:"chain"`
	Unread bool   `json:"unread"`
}

// markReadResponse is the contract's MarkReadResponse: what changed, and what
// could not be changed because it has no mailbox copy. The counts are separate
// from an error on purpose — a chain with nothing markable in it is a complete
// answer about a chain that is partly recovered text, not a failure.
type markReadResponse struct {
	Chain   string `json:"chain"`
	Unread  bool   `json:"unread"`
	Marked  int    `json:"marked"`
	Skipped int    `json:"skipped"`
}

// defaultMailbox is the real write path: the docket library's own calls
// (PrepareLabel + Execute for a label change, Users.Messages.Modify; and
// PrepareReply + SendPlan.Execute for a reply, Users.Messages.Send), through the
// mail grant this unit already holds in its own HOME — the same credential the
// ingest and the media pull read, so -mark-read, -mail-write and -send-mail ask
// for no access the host had not already given this user. What they change is that
// the server now changes the mailbox, and in the send's case adds to it, rather
// than only reading it.
//
// It is one opener for all three writers because it is one power and one
// credential: which of them a host granted is a switch on the server (see the
// handlers' own 403s), not three transports built from the same token.
//
// The client is opened per call rather than kept: this is a one-click write a
// reader makes, the label cache it loads is fresh on every pass, and a
// long-lived token source in a process that may run for weeks is a worse trade
// than one token read per click.
func defaultMailbox() func() (mailbox, error) {
	return func() (mailbox, error) { return gmailclient.New() }
}

// mediaPullRequest is what a caller may say: which message, and — when the
// reader is looking at a saved page — which page to bring up to date once the
// bytes land. The scope, the size cap and the image-only filter are the CLI's,
// and a page has no business loosening them.
type mediaPullRequest struct {
	Entry string `json:"entry"`
	Name  string `json:"name,omitempty"`
}

// mediaResponse is the outcome of POST /v1/media/pull (the contract's
// MediaPullResponse): the counts, plus one row per file, because "fetched
// nothing" and "fetched the wrong thing" are different answers and a count
// cannot tell them apart. A file already stored, and one already declined, are
// absent — neither has anything left to decide.
type mediaResponse struct {
	Wanted  int         `json:"wanted"`
	Pulled  int         `json:"pulled"`
	Skipped int         `json:"skipped"`
	Failed  int         `json:"failed"`
	Bytes   int64       `json:"bytes"`
	Files   []mediaFile `json:"files"`
	// The page this pull brought up to date, when the caller named one. Absent
	// for a call that named no page, and absent when the rebuild could not run
	// — the bytes are stored either way, and a client that wants the page can
	// still ask POST /v1/refresh for it.
	Spec   *spec.Spec     `json:"spec,omitempty"`
	Report *refreshReport `json:"report,omitempty"`
}

// mediaFile is one attachment's outcome. Reason is the recorded word for a
// skip; Error is what a retryable failure said, and the two are separate fields
// because only one of them means "do not ask again".
type mediaFile struct {
	Name   string `json:"name"`
	Source string `json:"source,omitempty"`
	SHA    string `json:"sha,omitempty"`
	Bytes  int64  `json:"bytes,omitempty"`
	Reason string `json:"reason,omitempty"`
	Error  string `json:"error,omitempty"`
}

// toMediaResponse is the whole mapping from the pull's result to the wire. The
// pull's own error type is deliberately not carried through: a client cannot do
// anything with a Go error chain, and the words are what a page can show.
func toMediaResponse(res media.Result) mediaResponse {
	out := mediaResponse{
		Wanted:  res.Wanted,
		Pulled:  res.Pulled,
		Skipped: res.Skipped,
		Failed:  res.Failed,
		Bytes:   res.Bytes,
		Files:   make([]mediaFile, 0, len(res.Items)),
	}
	for _, it := range res.Items {
		f := mediaFile{Name: it.Name, Source: it.Source, SHA: it.SHA, Bytes: it.Bytes, Reason: it.Reason}
		if it.Err != nil {
			f.Error = it.Err.Error()
		}
		out.Files = append(out.Files, f)
	}
	return out
}

// defaultMediaPull returns the real pull: the same internal/media walk the
// `corpus media pull` command runs, called in-process rather than as a
// subprocess, because a pull is one library call and not a sequence of phases.
//
// The store is the handle this process already has open — blobs, and the reasons
// for the files that could not be fetched, are written there (WAL) beside the
// reader. Uploads is the archive root, where a Slack attachment's bytes live; a
// mail part comes down the transport instead.
//
// The transport is deferred so that opening it fails only when a mail part is
// actually fetched: a pull of a Slack-only message must not need a mailbox. The
// credential is the one this unit already holds in its own HOME (the same grant
// the ingest reads), so -media asks for no access the host had not already given
// this user — what changes is when the fetch runs, not what it may touch.
func defaultMediaPull(store *corpus.Store, uploads string) func(ctx context.Context, entry string) (media.Result, error) {
	return func(ctx context.Context, entry string) (media.Result, error) {
		return media.Pull(ctx, media.Options{
			Store:   store,
			Uploads: uploads,
			Fetcher: media.Deferred(func() (media.Fetcher, error) {
				return gmailclient.New()
			}),
		}, corpus.MediaScope{Entry: entry})
	}
}

// refresh brings a page that has already been built up to date: the caller
// posts the previous spec (as POST /v1/spec returned it) and any selection
// overrides, and the server regenerates the page from the corpus.
//
// This is the read half of the CLI's `refresh` command. The fetching half is
// deliberately absent here: reaching the mailbox is `corpus ingest`'s job and
// that belongs to the CLI, the cron, or POST /v1/slurp when -slurp is on — not
// to a refresh, which only re-derives what the corpus already holds.
//
// One mutation it does perform is the same twins sweep `corpus slurp` runs:
// a quoted copy stored before its mailbox original arrived is one message
// stored twice, and a page re-derived over them would show it twice. The
// sweep refuses rather than guesses, so a corpus with no twins is untouched.
//
// The other thing a caller can change is the page's record of searches: a
// chain found by the page's own add-email search was found by a query the
// spec does not hold, so accepting it and recording that query are one call.
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

	next, rep, err := s.rebuildPage(req)
	if err != nil {
		// The failure is either the previous spec not being reproducible —
		// nothing recorded to re-run, or a recorded selection that cannot be
		// re-run, which is the caller's spec being wrong — or this host failing
		// to write the page it was asked for.
		if errors.Is(err, errSavingPage) {
			fail(w, http.StatusInternalServerError, err)
			return
		}
		fail(w, http.StatusBadRequest, err)
		return
	}
	send(w, http.StatusOK, refreshResponse{Spec: next, Report: toRefreshReport(rep)})
}

// errSavingPage separates the two ways a rebuild can fail: a spec the server
// cannot reproduce (the caller's page is wrong, a 400) and a page it cannot
// write (the server's disk is wrong, a 500). Both callers — POST /v1/refresh
// and the rebuild a pull runs behind the reader's back — have to tell them
// apart, so the distinction travels in the error rather than in a status the
// helper has no business choosing.
var errSavingPage = errors.New("saving the page")

// rebuildPage re-derives a page from the corpus and, when the request names
// one, rewrites the saved copy so a reload of /view/<name> lands on this run.
//
// This is the half POST /v1/refresh and POST /v1/media/pull share. A pull used
// to hand the rebuild back to the client — fetch the files, then ask for the
// page again — which meant a reader who reloaded while the bytes were coming
// down lost both: the files landed in the corpus and the page they were
// expected to appear on was never rewritten. Here the page is brought up to date
// by the side that already knows the files landed.
func (s *server) rebuildPage(req refreshRequest) (spec.Spec, refresh.Report, error) {
	rep, next, err := refresh.Run(s.store, noMailbox{}, req.Spec, refresh.Options{
		Title:      req.Title,
		Person:     req.Person,
		Since:      req.Since,
		Limit:      req.Limit,
		Me:         req.Me,
		IncludeNew: req.IncludeNew,
		Accept:     req.Accept,
		Queries:    req.Queries,
		Uploads:    s.uploads,
		// Fetch stays false: this server cannot reach the mailbox, on purpose.
		Fetch: false,
		// Proposals run hybrid: the recorded queries are embedded so the
		// vector half joins discovery, and a semantic-only chain must clear
		// the model's chain floor before it is offered (see refresh.Options).
		Embed: s.embedder(),
	})
	if err != nil {
		return spec.Spec{}, refresh.Report{}, err
	}
	if req.Name != "" {
		// The saved page must not drift from what the client just received: the
		// refresh rewrites the file exactly as POST /v1/spec would, so a reload
		// of /view/<name> lands on this run, not the stale one.
		if err := s.saveSpec(req.Name, next); err != nil {
			return spec.Spec{}, refresh.Report{},
				fmt.Errorf("%w as %q: %w", errSavingPage, req.Name, err)
		}
	}
	return next, rep, nil
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

// opsOrgs lists every mail domain the corpus holds mail from, with the mail
// behind it and the organisation that mail is drawn as.
//
// A separate endpoint from /v1/ops/plan because the two answer independently — a
// colour rule does not move when a pair of people is merged — and this is much
// the cheaper of the two: one pass over the mail the corpus holds, without the
// dedupe or twins passes.
func (s *server) opsOrgs(w http.ResponseWriter, r *http.Request) {
	out, err := s.orgRules()
	if err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	send(w, http.StatusOK, out)
}

// orgRules is the list as both halves of the surface answer it: a writer that
// says what it stored in the same terms the reader read it in, and a reader that
// sees the rules as they now stand rather than as it asked for them.
func (s *server) orgRules() (orgsResponse, error) {
	ds, err := s.store.SenderDomains()
	if err != nil {
		return orgsResponse{}, fmt.Errorf("listing the sender domains: %w", err)
	}
	out := orgsResponse{Domains: make([]orgRuleResponse, 0, len(ds))}
	for _, d := range ds {
		// The guess is what an undecided domain is drawn as. It is derived here
		// rather than taken from a build's resolver because for a domain there is
		// only the domain: the person-side half of a build's answer is about the
		// trail, and this screen is about the corpus.
		guess, _ := spec.OrgForDomain(d.Domain, nil)
		org := guess
		if d.Stored {
			org = d.Org
		}
		out.Domains = append(out.Domains, orgRuleResponse{
			Domain: d.Domain, Messages: d.Messages, People: d.People,
			Org: org, Stored: d.Stored, Guess: guess,
		})
	}
	return out, nil
}

// setOpsOrg records what one domain is: a named organisation, an organisation-less
// domain, or nothing at all — the last of those putting the domain back to being
// read from its own name.
func (s *server) setOpsOrg(w http.ResponseWriter, r *http.Request) {
	domain, org, ok := s.readOrgRule(w, r)
	if !ok {
		return
	}
	if org == nil {
		if err := s.store.ClearOrgRule(domain); err != nil {
			fail(w, http.StatusInternalServerError, err)
			return
		}
	} else if err := s.store.PutOrgRule(domain, *org); err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	s.opsOrgs(w, r)
}

// opsOrgsPreview answers what one proposed rule would redraw, storing nothing.
// This is the shape the rest of Ops is built on: the consequence is shown before
// the write, and it is computed by the resolver that will do the work rather than
// by a second estimate of it. See spec.OrgShiftFor.
func (s *server) opsOrgsPreview(w http.ResponseWriter, r *http.Request) {
	domain, org, ok := s.readOrgRule(w, r)
	if !ok {
		return
	}
	stored, err := s.store.OrgRules()
	if err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	// A copy: a preview may not be able to change what is stored, and OrgShiftFor
	// reads the stored set itself to know what the corpus is drawn as now.
	proposed := make(map[string]string, len(stored)+1)
	for d, o := range stored {
		proposed[d] = o
	}
	if org == nil {
		delete(proposed, domain)
	} else {
		proposed[domain] = *org
	}
	shift, err := spec.OrgShiftFor(s.store, proposed)
	if err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	send(w, http.StatusOK, orgShiftResponse{
		Domain: domain, Messages: shift.Messages, People: shift.People,
		Ambiguous: shift.Ambiguous,
	})
}

// readOrgRule decodes the one body both writes take. The domain is lowercased
// and trimmed — a domain is case-insensitive, and "Termina.IO" and "termina.io"
// being two rules is a pair that exists only to get out of step — and the label
// is trimmed but not otherwise judged: a label is prose the reader chose, and the
// corpus has no business correcting it.
func (s *server) readOrgRule(w http.ResponseWriter, r *http.Request) (string, *string, bool) {
	var in orgRuleRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRequestBody))
	// A misspelled field is a caller reading a different contract, and storing a
	// rule they did not ask for is worse than refusing.
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		fail(w, http.StatusBadRequest, fmt.Errorf("reading the request body: %w", err))
		return "", nil, false
	}
	domain := strings.ToLower(strings.TrimSpace(in.Domain))
	if domain == "" {
		fail(w, http.StatusBadRequest, errors.New(
			"a rule needs a domain: the domain is what a rule is about, and one about "+
				"nothing has no mail to colour"))
		return "", nil, false
	}
	if in.Org == nil {
		return domain, nil, true
	}
	org := strings.TrimSpace(*in.Org)
	return domain, &org, true
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

// original serves the entry's own text/html part: the sender's markup, sanitised
// for a reader who wants to see it as it was written rather than as this renderer
// reads it.
//
// Sanitised, not raw. The corpus stores what the sender sent, and caddy puts a
// Basic-auth gate in front of this server in production (issue #14), but neither
// of those is a reason to hand a browser markup with a script tag in it: the
// client mounts this into a shadow root in a page that is otherwise signed in to
// the reader's own mailbox, and "the gate is in front of it" stops being true the
// moment anyone else gets the panel. spec.OriginalBody is the decision — a second
// rule set that keeps the sender's CSS, classes and ids (which is the whole point:
// the stripped-down body is what failed to draw a calendar invite) while refusing
// what a shadow root cannot contain.
//
// no-cache, not no-store: this is derived from a corpus that is rewritten by
// every slurp, so an intermediary holding it is holding yesterday's message. The
// client is the thing that makes a flip cheap — it holds the part for the length
// of the session (src/lib/original.ts), which is what a reader comparing the two
// renderings is doing — and the part is only asked for on the first flip, which
// is why this is a route of its own rather than a field on every chain read.
func (s *server) original(w http.ResponseWriter, r *http.Request) {
	extID := r.PathValue("extId")
	raw, err := s.store.OriginalHTML(extID)
	if err != nil {
		failLookup(w, err)
		return
	}
	body, ok := spec.OriginalBody(raw)
	if !ok {
		// Distinguishable from the 404 above on purpose: "no such entry" and "an
		// entry with no html part" are different answers, and a client that only
		// ever toggles on entries its own read marked `original` never sees this.
		fail(w, http.StatusNotFound, fmt.Errorf(
			"%s carries no text/html part of its own, so there is no original to show", extID))
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	if _, err := io.WriteString(w, body); err != nil {
		// Nothing to do about a write that failed to a client that has gone: the
		// status line and the content type are already out.
		log.Printf("serving the original of %s: %v", extID, err)
	}
}

func (s *server) entry(w http.ResponseWriter, r *http.Request) {
	shown, err := s.store.Show(r.PathValue("extId"))
	if err != nil {
		failLookup(w, err)
		return
	}
	drawing, err := s.rendered(shown.ExtID)
	if err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	send(w, http.StatusOK, toCorpusEntry(shown, drawing))
}

// rendered is one entry as a page build would draw it. The failure is returned
// rather than written because the two handlers that ask for it report it
// differently: one is answering for a trail, the other for a single entry.
func (s *server) rendered(extID string) (spec.Rendered, error) {
	entries, err := spec.RenderTrail(s.store, []string{extID})
	if err != nil {
		return spec.Rendered{}, fmt.Errorf("rendering %s: %w", extID, err)
	}
	return entries[extID], nil
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
	ids := make([]string, 0, len(shown))
	for _, sh := range shown {
		ids = append(ids, sh.ExtID)
	}
	// One render for the whole trail rather than one per entry: the conversion is
	// per entry, but the queries behind it are not, and a trail is read as a unit.
	// A failure here fails the request: the trail's whole purpose is to be read,
	// and a client that asks for it and gets entries it cannot draw would be
	// shown a broken thread instead of a retryable error. See RenderTrail for
	// what this deliberately does not do (no zone inference, no participation).
	rendered, err := spec.RenderTrail(s.store, ids)
	if err != nil {
		fail(w, http.StatusInternalServerError, fmt.Errorf("rendering the trail: %w", err))
		return
	}
	out := chainResponse{RootExtID: id, Entries: make([]corpusEntry, 0, len(shown))}
	for _, sh := range shown {
		out.Entries = append(out.Entries, toCorpusEntry(sh, rendered[sh.ExtID]))
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
	send(w, http.StatusOK, toStatusResponse(snap, s.nextSlurpAt()))
}

// labels is the folder list the home page's button opens. It is the mailbox's
// own labels with the counts they carry, kept as one plain read: the labels are
// what Zach filed his mail under, and nothing here decides what a folder should
// be on his behalf.
func (s *server) labels(w http.ResponseWriter, r *http.Request) {
	ls, err := s.store.Labels()
	if err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	send(w, http.StatusOK, toLabelsResponse(ls))
}

// version is the deploy stamp. It reads two things the process knows about
// itself and nothing about the mail: the revision it was started with, and when
// it started.
//
// Deliberately not baked into the bundle. The bundle is byte-identical across
// deploys of one revision, so a stamp built into it cannot distinguish "the new
// code is live" from "my tab is still the old one" — which is the only question
// this answers. StartedAt is the process's own clock: for a deploy that is when
// it went live.
func (s *server) version(w http.ResponseWriter, r *http.Request) {
	send(w, http.StatusOK, versionResponse{
		Rev:       s.rev,
		StartedAt: s.startedAt.UTC().Format(time.RFC3339),
	})
}

// getSettings reads the choices that are about the reader rather than about the
// mail: the folder the home page opens in, and the person whose mail is theirs.
// Each is served with the absence of a choice preserved — a client has to be
// able to tell "no default" from "a default of nothing", and "nobody has said
// who the reader is" from a reader who has — and an omitted key is how that is
// said.
func (s *server) getSettings(w http.ResponseWriter, r *http.Request) {
	out := settingsResponse{}
	// Unlike the reader's other choices this one is always served: the cadence is
	// in force whether or not anyone has chosen it (DefaultSlurpEvery), and a page
	// showing an unset control would be hiding the schedule the server is keeping.
	out.SlurpEvery = s.slurpEveryWord()
	folder, ok, err := s.store.Setting(corpus.SettingDefaultFolder)
	if err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	if ok && folder != "" {
		out.DefaultFolder = &folder
	}
	me, err := s.store.MeAddresses()
	if err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	// Omitted rather than served as an empty array, so that "the reader has
	// never said" is not answered with a list a client would have to interpret.
	if len(me) > 0 {
		out.Me = me
	}
	// The person those addresses are, when the setting names one: the addresses
	// are read out of the identity graph rather than stored, and a client cannot
	// get back to the person from them — two people can share an address in the
	// list a merge has yet to make, and one person's addresses are several.
	id, ok, err := s.store.MePerson()
	if err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	if ok {
		out.MePersonID = &id
	}
	send(w, http.StatusOK, out)
}

// setSettings records them. Each field the body names is written, and a field
// the body leaves out is left as it stands.
//
// That is the opposite of the rule while there was one preference — absent meant
// cleared — and the rule could not survive a second one: both settings travel in
// the same body, so a reader saving the folder they are in would clear the
// person that says which mail is theirs, and every save would have to send the
// whole state or silently destroy the part it did not mention. Nothing on the
// wire changed meaning with it: every caller already names the field it writes,
// including the empty string it sends to clear one, so a cleared folder is still
// asked for with "defaultFolder": "". What is new is that not mentioning a field
// is now a way to leave it alone, which is what lets one screen write one
// preference without speaking for the other.
//
// Nothing is validated against the label list. A folder may be one the next
// slurp brings in, and refusing it would be the corpus arguing with the reader
// about a mailbox it is behind on; a folder that is not there shows an empty
// list under its own name, which is exactly true and one click from being fixed.
//
// The reader is written as a person (corpus.SetMePerson) and not as the addresses
// that person is: which addresses are one human is the identity graph's answer,
// and a server that accepted a list would be storing a second one that goes stale
// the first time an alias is learned or two people are merged. The id is
// validated, unlike the folder — a person that is not in the corpus would mark
// nothing, and no later sweep can bring a person into existence the way it can
// bring in a folder — and the addresses the setting used to be are cleared with
// it, so a corpus configured before this cannot answer with a stale list.
func (s *server) setSettings(w http.ResponseWriter, r *http.Request) {
	var in settingsRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRequestBody))
	// A misspelled field is a caller reading a different contract, and storing a
	// preference they did not ask for is worse than refusing.
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		fail(w, http.StatusBadRequest, fmt.Errorf("reading the request body: %w", err))
		return
	}
	if in.DefaultFolder != nil {
		folder := strings.TrimSpace(*in.DefaultFolder)
		if err := s.store.PutSetting(corpus.SettingDefaultFolder, folder); err != nil {
			fail(w, http.StatusInternalServerError, err)
			return
		}
	}
	if in.SlurpEvery != nil {
		// An empty word clears the setting rather than being refused, the same
		// way an emptied folder does: it means "stop choosing, sweep at the
		// default", which is a state a reader can want and the one the corpus
		// starts in. Anything else is validated rather than stored as sent: the
		// cadence drives a walk of the whole mailbox, so a value the server
		// cannot honour has to be refused where the caller can see why, not
		// stored and silently defaulted. The canonical word is what is written,
		// so the page's control finds the value among the ones it offers.
		word := ""
		if strings.TrimSpace(*in.SlurpEvery) != "" {
			_, canonical, err := parseSlurpEvery(*in.SlurpEvery)
			if err != nil {
				fail(w, http.StatusBadRequest, err)
				return
			}
			word = canonical
		}
		if err := s.store.PutSetting(corpus.SettingSlurpEvery, word); err != nil {
			fail(w, http.StatusInternalServerError, err)
			return
		}
	}
	if in.MePersonID != nil {
		// 0 is nobody, and it is not a person id: clearing needs no second field,
		// and the caller who wants to stop being anyone says so with a zero.
		if err := s.store.SetMePerson(*in.MePersonID); err != nil {
			if errors.Is(err, corpus.ErrNoPerson) {
				fail(w, http.StatusBadRequest, fmt.Errorf("mePersonId: %w", err))
				return
			}
			fail(w, http.StatusInternalServerError, err)
			return
		}
	}
	// The settings as they now stand, so a caller sees what it stored rather than
	// what it asked for.
	s.getSettings(w, r)
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

// editPerson is the ops screen's people editor: one hand-made change to one
// person's setup, applied and answered with the person as it now stands.
//
// It is the corpus's first write of an identity by hand, and it is deliberately
// small. Nothing here merges two people: an identity another person already
// holds is refused, naming them, because moving an address is the same act as
// merging and the merge plan is where that act has evidence behind it
// (POST /v1/ops/merge). A rename is not a merge either — it is what to CALL
// someone — so it writes the name and adds it as an identity without touching
// the spellings the corpus read from headers.
//
// No switch gates this one. It writes to the corpus but reaches nothing else —
// no mailbox, no credential, no third party — and the screen it serves is the
// human review surface the rest of the ops routes belong to. The file it edits
// is the corpus, which the reader can already rebuild from the mailbox.
func (s *server) editPerson(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("personId"), 10, 64)
	if err != nil || id == 0 {
		fail(w, http.StatusBadRequest, fmt.Errorf(
			"personId %q: want the id of a person, e.g. /v1/people/12", r.PathValue("personId")))
		return
	}
	var req personEditRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRequestBody))
	// A misspelled field is a caller reading a different contract, and a
	// half-understood edit would write identities nobody asked for.
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		fail(w, http.StatusBadRequest, fmt.Errorf("reading the request body: %w", err))
		return
	}
	if req.DisplayName == "" && len(req.AddIdentities) == 0 && len(req.RemoveIdentities) == 0 {
		fail(w, http.StatusBadRequest, errors.New(
			"nothing to do: name at least one of displayName, addIdentities, removeIdentities"))
		return
	}
	person, err := corpus.UpdatePerson(s.store, id, corpus.PersonEdit{
		DisplayName: req.DisplayName,
		Add:         req.AddIdentities,
		Remove:      req.RemoveIdentities,
	})
	switch {
	case errors.Is(err, corpus.ErrNoPerson):
		fail(w, http.StatusNotFound, err)
		return
	case err != nil:
		var taken *corpus.IdentityTakenError
		if errors.As(err, &taken) {
			// A conflict, not a bad request: the body was well-formed and the
			// corpus refuses the change it asks for.
			fail(w, http.StatusConflict, err)
			return
		}
		// A malformed identity is the caller's own mistake, and the only other
		// error the corpus reports from here.
		if errors.Is(err, corpus.ErrBadIdentity) {
			fail(w, http.StatusBadRequest, err)
			return
		}
		fail(w, http.StatusInternalServerError, err)
		return
	}
	send(w, http.StatusOK, personResponse{Person: personSummary{
		PersonID: person.PersonID, DisplayName: person.DisplayName,
		Identities: person.Identities, Sent: person.Sent, Received: person.Received,
	}})
}
