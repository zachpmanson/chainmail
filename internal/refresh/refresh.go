// Package refresh brings a page that has already been generated up to date: it
// re-runs the collection a previous spec records and regenerates the spec with
// the same parameters.
//
// Two passes, over two different kinds of thing. `spec.threads` is authoritative
// membership — a chain list is a curation decision, not a leftover query result.
// `spec.queries` is discovery. So the passes have different authority:
//
//   - The thread pass applies automatically. A new message inside a chain already
//     on the page belongs there: the chain was accepted once and a reply needs no
//     fresh approval. This is the half a query-only refresh misses, because a
//     reply need not contain the query's words — "sounds good, Friday then"
//     matches nothing.
//   - The query pass proposes. A chain the query newly finds has never been
//     approved, so it is reported as a candidate and left out until a caller
//     accepts it. Auto-including would let a curated page re-widen on every
//     refresh, which makes the curation meaningless; never mentioning it would
//     hide the new relevant thread that is most of why a refresh gets run.
//
// Membership is therefore monotonic unless a caller says otherwise. A chain the
// queries no longer rank stays on the page and is only reported: dropping it
// would delete entries a reader has already read and permalinked, and
// `render --since` would not say so, because that diff marks what is new and
// revised, not what is gone.
package refresh

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/zachpmanson/chainmail/internal/corpus"
	"github.com/zachpmanson/chainmail/internal/mailingest"
	"github.com/zachpmanson/chainmail/internal/spec"
)

// MaxSpecVersion is the newest spec contract this package can reproduce.
// The CLI's Load refuses a newer spec, and so does the server, which sees
// specs as JSON rather than through Load.
//
// Public on purpose: the server answers 400 for a spec this build cannot
// reproduce before doing anything with it.
var MaxSpecVersion = maxSpecVersion

// Mailbox is the slice of the docket client refresh uses, and only when asked:
// see Options.Fetch. An interface so both passes can be exercised against a
// fake — everything else here is a query against a store a test can build in
// memory.
type Mailbox interface {
	// Search takes a page token and returns the next one, matching the client's
	// own signature: a refresh reads the first page of a narrowed query, so it
	// passes an empty token and ignores the continuation rather than paging —
	// finishing a walk is `ingest mail`'s job, and duplicating it here would give
	// two commands a cursor over the same query.
	Search(query string, limit int, pageToken string) ([]mailingest.Envelope, mailingest.Page, error)
	Read(id string) (mailingest.Message, error)
	Thread(id string) (mailingest.ThreadResult, error)
}

// Options carries the parameters that reproduce the previous page, plus the ones
// a spec does not record. Anything left zero falls back to what the spec says.
type Options struct {
	// Title, Me, Limit, Person and Since override what the previous spec
	// recorded. Empty means "keep what it recorded".
	Title  string
	Me     []string
	Limit  int
	Person string
	Since  string

	// Uploads is the archive upload root. Not recorded in a spec — it names a
	// home directory — so it is supplied every time.
	Uploads string

	// RunLabel names this pass. Defaults to today.
	RunLabel string

	// Fetch reaches the mailbox for both passes. Off by default: refresh's job is
	// to re-derive the page from the corpus, and the corpus is filled by
	// `corpus ingest`, which owns the mailbox, the rate limits and the failure
	// modes of doing so. With it off a refresh still picks up everything a later
	// ingest brought in — including the reply that matches no query, since the
	// thread pass selects by container rather than by words.
	Fetch bool

	// After bounds the fetching query pass as a Gmail date term (YYYY/MM/DD).
	// Empty means unbounded, which is what a spec with no readable runLabel
	// leaves it as.
	After string

	// IncludeNew accepts every chain the query pass proposes. Accept names
	// individual ones by chain root ext id, as the report prints them, so a
	// candidate can be taken later without re-running the search.
	IncludeNew bool
	Accept     []string
}

// QueryPass is what re-running one recorded query did. Fetched, Created and
// Changed stay zero unless Options.Fetch was set; Proposed is the discovery,
// which the corpus answers either way.
type QueryPass struct {
	Q        string
	Sent     string // the Gmail query as narrowed, empty when the mailbox was not asked
	Seen     int    // envelopes the mailbox returned
	Fetched  int    // of those, the ones not already in the corpus
	Created  int
	Changed  int
	Proposed int // chains this query found that the page does not have
}

// ThreadPass is what one recorded chain did on this refresh.
type ThreadPass struct {
	ID      string
	Subject string
	Seen    int
	Fetched int
	Created int
	Changed int
	// Skipped is set when the mailbox could not be asked about this chain, and
	// names why. The chain is still regenerated from the corpus.
	Skipped string
}

// Candidate is a chain the queries found that the page does not include. It
// carries what is needed to judge it without opening anything, and RootExtID is
// the handle that accepts it: a chain root's ext id is the one name that survives
// a corpus rebuild, where a row id does not and a Gmail thread id belongs to mail
// alone.
type Candidate struct {
	RootExtID string
	Subject   string
	Container string
	Entries   int
	Matched   int
	Span      string
	Query     string
}

// Growth is a chain that gained entries.
type Growth struct {
	ID      string
	Subject string
	Before  int
	After   int
}

// Report is what changed, in the terms a reader of the page would use.
type Report struct {
	Fetched bool
	Queries []QueryPass
	Threads []ThreadPass

	EntriesBefore int
	EntriesAfter  int
	// TwinsCollapsed is how many duplicate pairs the refresh collapsed before
	// redrawing. Ingestion is the cron's job, but refresh is where a page gets
	// re-derived, so it is also where a stored twin pair — a quoted copy and
	// the mailbox original it was recovered from, ingested on different days —
	// would otherwise sit until a human ran `corpus twins`. The sweep is the
	// same one the slurp pipeline runs; it refuses rather than guesses, and a
	// corpus with no twins leaves this at 0.
	TwinsCollapsed int
	// ChainsAdded are chains now on the page: accepted candidates, or a
	// container the corpus has newly attached to the reply graph.
	ChainsAdded []Growth
	ChainsGrown []Growth
	// ChainsProposed are candidates left out of the spec, awaiting a decision.
	ChainsProposed []Candidate
	// ChainsUnranked are chains still on the page that the queries no longer
	// return. They are kept; see the package comment.
	ChainsUnranked []string
}

// Created and Changed total the upserts across both passes.
func (r Report) Created() int {
	n := 0
	for _, p := range r.Queries {
		n += p.Created
	}
	for _, p := range r.Threads {
		n += p.Created
	}
	return n
}

func (r Report) Changed() int {
	n := 0
	for _, p := range r.Queries {
		n += p.Changed
	}
	for _, p := range r.Threads {
		n += p.Changed
	}
	return n
}

// NothingNew reports that the refresh looked and found nothing: no message
// stored, no chain grown or added, and nothing to propose.
//
// It is a distinct outcome from an error, and both callers and tests depend on
// that: a refresh that cannot reach the corpus or the mailbox returns an error,
// so a report existing at all means both passes ran to completion. A refresh
// that silently did nothing would otherwise be indistinguishable from a broken
// one.
func (r Report) NothingNew() bool {
	return r.Created() == 0 && r.Changed() == 0 &&
		r.EntriesAfter == r.EntriesBefore && r.TwinsCollapsed == 0 &&
		len(r.ChainsAdded) == 0 && len(r.ChainsGrown) == 0 &&
		len(r.ChainsProposed) == 0
}

// Run performs both passes and regenerates the spec.
func Run(store *corpus.Store, mb Mailbox, prev spec.Spec, opts Options) (Report, spec.Spec, error) {
	rep := Report{Fetched: opts.Fetch}
	opts = opts.merge(prev)

	if len(prev.Queries) == 0 && len(prev.Threads) == 0 {
		return rep, spec.Spec{}, fmt.Errorf("the spec records neither a query nor a thread, " +
			"so there is nothing to re-run — regenerate it with `corpus spec`")
	}

	// Collapse stored twins before the passes read from the store: a quote and
	// the mailbox copy it was recovered from, ingested on different days, are
	// one message stored twice, and a page re-derived over them would count it
	// twice. The sweep is idempotent; a corpus with none leaves the count 0.
	plan, err := corpus.CollapseTwins(store, true)
	if err != nil {
		return rep, spec.Spec{}, fmt.Errorf("collapsing stored twins: %w", err)
	}
	rep.TwinsCollapsed = len(plan.Collapse)

	known, err := knownMessageIDs(store)
	if err != nil {
		return rep, spec.Spec{}, err
	}
	sources, err := containerSources(store, prev.Threads)
	if err != nil {
		return rep, spec.Spec{}, err
	}

	if opts.Fetch {
		for _, q := range prev.Queries {
			p, err := queryFetch(store, mb, q.Q, opts, known)
			if err != nil {
				return rep, spec.Spec{}, err
			}
			rep.Queries = append(rep.Queries, p)
		}
		for _, th := range prev.Threads {
			p, err := threadFetch(store, mb, th, sources[th.ID], known)
			if err != nil {
				return rep, spec.Spec{}, err
			}
			rep.Threads = append(rep.Threads, p)
		}
		// Once, after both passes: a reply the thread pass brought in may be the
		// parent of one the query pass did, and either order of arrival has to end
		// with the same graph.
		if _, err := store.ResolveParents(); err != nil {
			return rep, spec.Spec{}, err
		}
	} else {
		for _, q := range prev.Queries {
			rep.Queries = append(rep.Queries, QueryPass{Q: q.Q})
		}
		for _, th := range prev.Threads {
			rep.Threads = append(rep.Threads, ThreadPass{ID: th.ID, Subject: th.Subject,
				Skipped: "the mailbox was not asked; pass -fetch to reach it"})
		}
	}

	// Discovery runs against the corpus either way, so a chain a plain
	// `corpus ingest` brought in is proposed on the next refresh.
	cands, err := candidates(store, prev, opts)
	if err != nil {
		return rep, spec.Spec{}, err
	}
	accepted, proposed := split(cands, opts)
	rep.ChainsProposed = proposed
	for i := range rep.Queries {
		for _, c := range proposed {
			if c.Query == rep.Queries[i].Q {
				rep.Queries[i].Proposed++
			}
		}
	}

	containers, kept := membership(prev, sources)
	next, err := spec.Generate(store, spec.Options{
		Containers: containers,
		ExtIDs:     append(accepted, kept...),
		Title:      opts.Title,
		Queries:    prev.Queries,
		Me:         opts.Me,
		RunLabel:   opts.RunLabel,
		UploadDir:  opts.Uploads,
		Params: &spec.RunParams{
			Me:     opts.Me,
			Limit:  opts.Limit,
			Person: opts.Person,
			Since:  opts.Since,
		},
	})
	if err != nil {
		return rep, spec.Spec{}, err
	}
	next.Runs = appendRun(prev.Runs, prev.RunLabel)
	next.Subtitle = prev.Subtitle
	next.Theme = prev.Theme
	next.OpenItems = prev.OpenItems
	next.OpenItemsTitle = prev.OpenItemsTitle

	rep.fill(prev, next)
	return rep, next, nil
}

// merge fills the options a spec already answers, so a refresh needs no flags
// beyond the ones a spec deliberately does not record.
func (o Options) merge(prev spec.Spec) Options {
	if o.Title == "" {
		o.Title = prev.Title
	}
	if o.RunLabel == "" {
		o.RunLabel = time.Now().Format("2 Jan 2006")
	}
	if p := prev.RunParams; p != nil {
		if len(o.Me) == 0 {
			o.Me = p.Me
		}
		if o.Limit == 0 {
			o.Limit = p.Limit
		}
		if o.Person == "" {
			o.Person = p.Person
		}
		if o.Since == "" {
			o.Since = p.Since
		}
	}
	if len(o.Me) == 0 {
		// A spec written before runParams existed still marks its own outbound
		// messages, and that is the reader's address by definition.
		o.Me = meFrom(prev)
	}
	if o.After == "" {
		o.After = AfterDate(prev.RunLabel)
	}
	return o
}

// AfterDate turns a run label into a Gmail `after:` term, so a fetching query
// pass asks only for what postdates the last one. The label's own date is used
// rather than the day after it: mail that arrived later on the day of the last
// run would otherwise fall in the gap between the two passes and be seen by
// neither. Re-seeing that one day costs nothing, because the known-id check
// discards those envelopes before they become reads.
//
// An unreadable or absent label yields "", leaving the pass unbounded. That is
// slow rather than wrong, and it is reported.
func AfterDate(runLabel string) string {
	for _, layout := range []string{"2 Jan 2006", "2 January 2006", "2006-01-02"} {
		if t, err := time.Parse(layout, strings.TrimSpace(runLabel)); err == nil {
			return t.Format("2006/01/02")
		}
	}
	return ""
}

// queryFetch re-runs one recorded query against the mailbox, so that a chain it
// would propose is in the corpus to be proposed from.
func queryFetch(store *corpus.Store, mb Mailbox, q string, opts Options, known map[string]bool) (QueryPass, error) {
	p := QueryPass{Q: q, Sent: q}
	if opts.After != "" {
		p.Sent = fmt.Sprintf("%s after:%s", q, opts.After)
	}
	envs, _, err := mb.Search(p.Sent, searchLimit(opts.Limit), "")
	if err != nil {
		return p, fmt.Errorf("re-running query %q: %w", p.Sent, err)
	}
	p.Seen = len(envs)
	p.Created, p.Changed, p.Fetched, err = ingest(store, mb, envs, known)
	return p, err
}

// threadFetch fetches one chain the page already shows, by id. This is the half
// that catches a reply matching no query.
//
// No date narrowing here, deliberately: the thread listing is a single call that
// returns every envelope whatever its date, and the ids are checked against the
// corpus before anything is read. An `after:` would buy nothing and would lose a
// message whose Date header is wrong or whose delivery ran past the window.
func threadFetch(store *corpus.Store, mb Mailbox, th spec.Thread, source string, known map[string]bool) (ThreadPass, error) {
	p := ThreadPass{ID: th.ID, Subject: th.Subject}
	switch {
	case th.ID == "":
		p.Skipped = "the spec records no id for this chain"
		return p, nil
	case source == "":
		p.Skipped = "no entries in this corpus, so its source is unknown"
		return p, nil
	case source != "mail":
		// A Slack channel is refreshed by re-ingesting the archive, which is a
		// local file and a different command; there is no thread to fetch.
		p.Skipped = "a " + source + " conversation, not a mail thread"
		return p, nil
	}
	res, err := mb.Thread(th.ID)
	if err != nil {
		return p, fmt.Errorf("fetching thread %s: %w", th.ID, err)
	}
	p.Seen = len(res.Messages)
	p.Created, p.Changed, p.Fetched, err = ingest(store, mb, res.Messages, known)
	return p, err
}

// ingest reads and stores the envelopes the corpus has not already seen.
//
// Skipping a known id is safe because a Gmail message id names immutable bytes:
// the body cannot change under it. What the check saves is a full-size read per
// message, and the thread pass re-lists whole threads on every refresh, so
// without it the cost of a refresh would grow with the length of the trail
// rather than with what arrived.
func ingest(store *corpus.Store, mb Mailbox, envs []mailingest.Envelope, known map[string]bool) (created, changed, fetched int, err error) {
	for _, env := range envs {
		if env.ID == "" || known[env.ID] {
			continue
		}
		msg, err := mb.Read(env.ID)
		if err != nil {
			return created, changed, fetched, fmt.Errorf("reading %s: %w", env.ID, err)
		}
		fetched++
		res, err := mailingest.Put(store, msg)
		if err != nil {
			return created, changed, fetched, err
		}
		known[env.ID] = true
		switch {
		case res.Created:
			created++
		case res.Changed:
			changed++
		}
	}
	return created, changed, fetched, nil
}

// candidates re-runs each query against the corpus and keeps the chains the page
// does not already contain. Chains are named by root, matching `corpus spec`: a
// trail is the whole conversation, not only the messages holding the words.
func candidates(store *corpus.Store, prev spec.Spec, opts Options) ([]Candidate, error) {
	have := map[string]bool{}
	for _, th := range prev.Threads {
		have[th.ID] = true
	}
	seen := map[string]bool{}
	var out []Candidate
	for _, q := range prev.Queries {
		cq := corpus.Query{Text: q.Q, Limit: opts.Limit}
		if opts.Since != "" {
			t, err := time.Parse("2006-01-02", opts.Since)
			if err != nil {
				return nil, fmt.Errorf("since %q recorded in the spec: want YYYY-MM-DD", opts.Since)
			}
			cq.Since = t
		}
		if opts.Person != "" {
			cq.Involving = []string{opts.Person}
		}
		chains, err := store.SearchChains(cq)
		if err != nil {
			return nil, err
		}
		for _, c := range chains {
			if have[c.Container] || seen[c.RootExtID] {
				continue
			}
			seen[c.RootExtID] = true
			out = append(out, Candidate{
				RootExtID: c.RootExtID,
				Subject:   c.Subject,
				Container: c.Container,
				Entries:   c.Entries,
				Matched:   c.Matched,
				Span:      span(c.First, c.Last),
				Query:     q.Q,
			})
		}
	}
	return out, nil
}

// split sorts candidates into the ones a caller has accepted and the ones still
// awaiting a decision. An -accept id that matches nothing is left alone rather
// than reported as an error: a chain can be accepted, then found again under a
// second query on a later run, and a refresh that failed because a decision was
// already taken would be a refresh nobody could re-run.
func split(cands []Candidate, opts Options) (accepted []string, proposed []Candidate) {
	want := map[string]bool{}
	for _, id := range opts.Accept {
		if id = strings.TrimSpace(id); id != "" {
			want[id] = true
		}
	}
	for _, c := range cands {
		if opts.IncludeNew || want[c.RootExtID] {
			accepted = append(accepted, c.RootExtID)
			continue
		}
		proposed = append(proposed, c)
	}
	// Any accepted id that named no candidate is passed through, so accepting a
	// chain twice is idempotent rather than a way to lose it.
	for id := range want {
		if !containsStr(accepted, id) {
			accepted = append(accepted, id)
		}
	}
	sort.Strings(accepted)
	return accepted, proposed
}

// knownMessageIDs is every Gmail id the corpus holds as a message of its own. An
// entry recovered only from somebody's quote of it has no id here, so a fetching
// pass reads it and upgrades it to a first-hand copy.
func knownMessageIDs(store *corpus.Store) (map[string]bool, error) {
	rows, err := store.DB().Query(
		`select gmail_id from mail_detail where gmail_id is not null and gmail_id != ''`)
	if err != nil {
		return nil, fmt.Errorf("reading known message ids: %w", err)
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// containerSources asks the corpus what each recorded chain is, because the spec
// does not say. A `threads` entry can name a Slack channel as readily as a mail
// thread, and handing a channel id to a mail API is a fetch that fails on some
// days and returns something unrelated on others.
func containerSources(store *corpus.Store, threads []spec.Thread) (map[string]string, error) {
	out := map[string]string{}
	ids := containerIDs(threads)
	if len(ids) == 0 {
		return out, nil
	}
	ph := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	rows, err := store.DB().Query(
		`select container, source, count(*) from entries where container in (`+ph+`)
		 group by container, source order by count(*) desc`, args...)
	if err != nil {
		return nil, fmt.Errorf("reading chain sources: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var container, source string
		var n int
		if err := rows.Scan(&container, &source, &n); err != nil {
			return nil, err
		}
		// Ordered by count, so a container holding both kinds is named by the one
		// it is mostly made of.
		if _, ok := out[container]; !ok {
			out[container] = source
		}
	}
	return out, rows.Err()
}

// membership resolves the recorded chains into what to select, which is not the
// same thing for every source.
//
// A mail thread is expanded whole: a Gmail thread IS one conversation, so every
// entry in it belongs, including a reply whose parent edge never resolved and
// which a reply-graph closure would therefore miss. A Slack container is a
// channel, not a conversation — expanding it would drag in hundreds of messages
// about something else — so those chains are held to the entries the previous
// page actually had.
//
// The handle for one of those entries is the spec's own `source` field, which for
// a first-hand entry is its corpus ext id verbatim. That is the only durable name
// a spec carries for a non-mail entry: mail has gmailId and threadId, Slack has
// neither.
func membership(prev spec.Spec, sources map[string]string) (containers, extIDs []string) {
	whole := map[string]bool{}
	for _, th := range prev.Threads {
		if th.ID == "" {
			continue
		}
		if src := sources[th.ID]; src == "" || src == "mail" {
			whole[th.ID] = true
			containers = append(containers, th.ID)
		}
	}
	for _, m := range prev.Messages {
		if m.ThreadID == "" || whole[m.ThreadID] || !isExtID(m.Source) {
			continue
		}
		extIDs = append(extIDs, m.Source)
	}
	return containers, extIDs
}

// isExtID distinguishes a corpus ext id from the prose the same field carries for
// an entry recovered from somebody's quote ("unspooled from msg …").
func isExtID(s string) bool {
	if strings.ContainsAny(s, " \t") {
		return false
	}
	for _, prefix := range []string{"mail:", "slack:"} {
		if strings.HasPrefix(s, prefix) {
			return true
		}
	}
	return false
}

func containerIDs(threads []spec.Thread) []string {
	var out []string
	for _, th := range threads {
		if th.ID != "" {
			out = append(out, th.ID)
		}
	}
	return out
}

// fill measures the two specs against each other. Counting entries per chain
// rather than diffing bodies keeps this reportable in the reader's terms — a
// chain grew, a chain appeared — and leaves word-level revisions to
// `render --since`, which already marks them in the page itself.
func (r *Report) fill(prev, next spec.Spec) {
	r.EntriesBefore = len(prev.Messages)
	r.EntriesAfter = len(next.Messages)

	before := map[string]spec.Thread{}
	for _, th := range prev.Threads {
		before[th.ID] = th
	}
	for _, th := range next.Threads {
		old, ok := before[th.ID]
		switch {
		case !ok:
			r.ChainsAdded = append(r.ChainsAdded, Growth{ID: th.ID, Subject: th.Subject, After: th.Count})
		case th.Count > old.Count:
			r.ChainsGrown = append(r.ChainsGrown, Growth{
				ID: th.ID, Subject: th.Subject, Before: old.Count, After: th.Count})
		}
	}

	// A chain the queries no longer rank is still on the page, since selection
	// includes every recorded container. Naming it is the only signal that the
	// query and the page have drifted apart.
	kept := map[string]bool{}
	for _, th := range next.Threads {
		kept[th.ID] = true
	}
	for _, th := range prev.Threads {
		if !kept[th.ID] {
			r.ChainsUnranked = append(r.ChainsUnranked, th.ID)
		}
	}
	sort.Strings(r.ChainsUnranked)
}

// searchLimit converts a chain budget into a message budget for the mailbox
// search. The recorded limit counts chains, and a chain is many messages, so
// using it directly would ask the mailbox for a fraction of what the page is
// made of.
func searchLimit(chains int) int {
	if chains <= 0 {
		chains = 10
	}
	return chains * 25
}

// meFrom recovers the reader's addresses from a spec that predates runParams: an
// entry already marked outbound names the mailbox the page was collected from.
func meFrom(prev spec.Spec) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range prev.Messages {
		if !m.Me || m.FromEmail == "" {
			continue
		}
		a := strings.ToLower(m.FromEmail)
		if !seen[a] {
			seen[a] = true
			out = append(out, a)
		}
	}
	sort.Strings(out)
	return out
}

// appendRun records the pass just superseded, so the page carries its own history
// of collection rather than only its latest label.
func appendRun(runs []string, label string) []string {
	if label == "" {
		return runs
	}
	if len(runs) > 0 && runs[len(runs)-1] == label {
		return runs
	}
	return append(runs, label)
}

func span(first, last time.Time) string {
	f := first.Format("2 Jan 2006")
	l := last.Format("2 Jan 2006")
	if f == l {
		return f
	}
	return f + " – " + l
}

func containsStr(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}
