package corpus

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"
)

// Search is lexical only, and deliberately so. FTS5 wins decisively on the
// tokens this corpus is dense with — invoice numbers, ICPs, Message-IDs — and
// the structural half of a real question ("everything Alice sent about X after
// June") is a WHERE clause, not something to hope a similarity score encodes.

// Tunables. The only number with a literature behind it is rrfK; the rest are
// pool sizes and presentation defaults.
const (
	// rrfK is the Reciprocal Rank Fusion constant. 60 is the value from the
	// original TREC work and is not sensitive: it only sets how quickly the
	// contribution of a deep rank decays.
	rrfK = 60.0

	// subjectWeight boosts the subject column inside one bm25 computation. This
	// is a within-index column weight, not a fusion weight — it says a mail
	// subject is a stronger topic signal than a line of body prose, and it does
	// not affect how the two indexes are combined.
	subjectWeight = 4.0
	bodyWeight    = 1.0

	defaultLimit          = 20
	defaultCandidateLimit = 500
	defaultSnippetTokens  = 12
	defaultPerChain       = 3

	// walkDepthCap bounds the reply-graph walks. parent_id is a foreign key set
	// once on resolution so a cycle should be impossible, but an unbounded
	// recursive CTE turns "should be impossible" into a hung query.
	walkDepthCap = 64

	snippetOpen  = "["
	snippetClose = "]"
	snippetElide = "…"
)

// NoiseFilter names the entries that are indexed but should not be allowed to
// dominate a ranking: bots, channel join/leave chatter, and the like. The corpus
// ingests everything on purpose; ranking everything equally is a separate
// mistake.
//
// Predicates are SQL boolean expressions evaluated against the search query's
// FROM clause, where `e` is entries, `md` is mail_detail and `sd` is
// slack_detail (both outer-joined, so both may be all-NULL). An entry matching
// ANY predicate is noise; a predicate that evaluates to NULL — the normal case
// when a Slack rule meets a mail row — counts as "not noise".
//
// Slack has the columns for this. Mail has none, and gains none here — a
// from_addr denylist or a future mail_detail column is added by appending one
// predicate over `md`, which is why this is a list rather than hard-coded SQL.
type NoiseFilter struct {
	Predicates []string
}

// DefaultNoiseFilter excludes Slack bots and the membership/housekeeping
// subtypes. The subtype list is a denylist rather than an allowlist because an
// unrecognised subtype is far more likely to be real speech than noise.
func DefaultNoiseFilter() NoiseFilter {
	return NoiseFilter{Predicates: []string{
		`sd.is_bot = 1`,
		`sd.subtype in (
			'channel_join','channel_leave','channel_topic','channel_purpose',
			'channel_name','channel_archive','channel_unarchive',
			'group_join','group_leave','bot_message','reminder_add',
			'pinned_item','unpinned_item','tombstone','file_comment'
		)`,
	}}
}

// Query is a search: free text plus structural filters, all ANDed. Every field
// is optional; the zero Query matches the whole corpus ordered by recency.
type Query struct {
	// Text is free text. Double-quoted runs are kept as phrases; everything
	// else is a term, and all terms must match. Terms that look like
	// identifiers are additionally sent to the trigram index.
	Text string

	// Since and Until bound entry timestamps as the half-open range
	// [Since, Until). Either may be zero for unbounded.
	Since time.Time
	Until time.Time

	// People matches the *author* — entries.person_id — against an email
	// address, a Slack uid or a display name; the caller does not have to say
	// which. PersonIDs is the same filter when the caller already has ids, and
	// the two are unioned.
	People    []string
	PersonIDs []int64

	// Involving matches anyone recorded on the entry at all, author or
	// recipient. It is a different question from People: on a real trail a
	// quarter of the participants appear only in To:/Cc:, so "threads Alice was
	// on" and "things Alice said" cannot be one filter. Restrict to particular
	// roles with InvolvingRoles (RoleFrom / RoleTo / RoleCc); empty means any.
	Involving      []string
	InvolvingRoles []string

	// Containers is mail thread ids / Slack channel ids.
	Containers []string
	// Sources is SourceMail / SourceSlack.
	Sources []string

	// HasAttachment: nil to not care, else require/forbid attachments.
	HasAttachment *bool

	// IncludeNoise disables noise exclusion entirely. Noise overrides which
	// predicates define noise; nil means DefaultNoiseFilter.
	IncludeNoise bool
	Noise        *NoiseFilter

	// Limit is the number of results returned (entries, or chains).
	Limit int
	// CandidateLimit is how deep each index is read before fusion. Chain
	// aggregation needs a pool wider than the answer, since a chain's evidence
	// is spread over many entries.
	CandidateLimit int
	// SnippetTokens is the approximate width of a match excerpt.
	SnippetTokens int
	// PerChain is how many best-matching entries to attach to each chain.
	PerChain int
}

func (q Query) withDefaults() Query {
	if q.Limit <= 0 {
		q.Limit = defaultLimit
	}
	if q.CandidateLimit <= 0 {
		q.CandidateLimit = defaultCandidateLimit
	}
	if q.SnippetTokens <= 0 {
		q.SnippetTokens = defaultSnippetTokens
	}
	if q.PerChain <= 0 {
		q.PerChain = defaultPerChain
	}
	if q.Noise == nil {
		f := DefaultNoiseFilter()
		q.Noise = &f
	}
	return q
}

// EntryHit is one matching entry, with the excerpt that explains the match.
type EntryHit struct {
	ID        int64
	ExtID     string
	Source    string
	TS        time.Time
	PersonID  int64
	Person    string
	Container string
	Subject   string
	Permalink string

	// Snippet is the FTS5 excerpt, with matched terms wrapped in [ ]. Empty for
	// a structural-only query, which has nothing to highlight.
	Snippet string

	// Score is the fused RRF score. ProseRank and IdentRank are the 1-based
	// positions in each index's ranking, or 0 for "that index did not find it" —
	// kept because they are how you tell a prose match from an id match.
	Score     float64
	ProseRank int
	IdentRank int
}

// ChainHit is a conversation, ranked by the aggregate relevance of its entries.
// This is the headline result: the question people actually ask is "which
// threads are about this", not "which sentences".
type ChainHit struct {
	// RootID is the entry the reply graph terminates at; RootExtID is its
	// durable name, and the only chain identity worth storing anywhere.
	RootID    int64
	RootExtID string

	Subject   string
	Container string
	Sources   []string

	// Entries is the size of the whole chain; Matched is how much of it the
	// query hit. The ratio is the honest measure of "is this thread about it".
	Entries int
	Matched int

	First time.Time
	Last  time.Time

	Score float64
	// Best are the top-scoring entries of this chain, most relevant first.
	Best []EntryHit
}

// SearchEntries returns matching entries, best first.
func (s *Store) SearchEntries(q Query) ([]EntryHit, error) {
	q = q.withDefaults()
	fused, err := s.candidates(q)
	if err != nil {
		return nil, err
	}
	if len(fused) > q.Limit {
		fused = fused[:q.Limit]
	}
	return s.hydrate(fused)
}

// SearchChains aggregates entry hits up to their conversations and returns
// chains ordered by aggregate relevance.
//
// A chain is defined by walking parent_id to the root, not by grouping on
// container, for three reasons:
//
//   - For Slack, `container` is the channel id. Grouping on it would make an
//     entire channel a single "chain", which is not a conversation and would
//     swamp every mail thread in the ranking. `thread_ts` is the Slack thread,
//     and it reaches the entries table only as a parent_id edge.
//   - Entries recovered from quoted text ('quote:<sha>') and originals
//     forwarded across sources have no container in common with the trail they
//     belong to. parent_id is exactly the graph that does connect them, and
//     schema.go materialises it so this need not be re-derived per query.
//   - An entry whose parent_ref never resolved becomes its own root. That is
//     the truthful answer — the parent is outside the mailbox — where a
//     container grouping would quietly merge unrelated fragments that Gmail
//     happened to file together.
//
// The cost is that a trail split across two Gmail threads with broken reply
// headers ranks as two chains. Container is returned on each chain so a caller
// can coalesce if it wants to; the ranking does not assume it.
func (s *Store) SearchChains(q Query) ([]ChainHit, error) {
	q = q.withDefaults()
	fused, err := s.candidates(q)
	if err != nil {
		return nil, err
	}
	if len(fused) == 0 {
		return nil, nil
	}

	ids := make([]int64, len(fused))
	for i, c := range fused {
		ids[i] = c.id
	}
	roots, err := s.rootsOf(ids)
	if err != nil {
		return nil, err
	}

	// Group candidates by root, keeping them in fused (descending score) order.
	order := []int64{}
	byRoot := map[int64][]candidate{}
	for _, c := range fused {
		root, ok := roots[c.id]
		if !ok {
			// Should not happen: every candidate came from entries. Falling back
			// to the entry itself keeps a hit visible rather than dropping it.
			root = c.id
		}
		if _, seen := byRoot[root]; !seen {
			order = append(order, root)
		}
		byRoot[root] = append(byRoot[root], c)
	}

	// Chain score: the members' scores summed with harmonic damping, i.e.
	// s1 + s2/2 + s3/3 + ... over the chain's hits in descending order. A plain
	// sum lets a 200-message thread with 200 weak hits beat a three-message
	// thread that is squarely on topic; a plain max throws away the fact that
	// twelve messages in this trail discuss the invoice and one message in that
	// one mentions it. Damping keeps corroboration worth something while making
	// it cheaper each time. It is the same shape as RRF, so no new constant.
	type scoredRoot struct {
		root  int64
		score float64
	}
	scored := make([]scoredRoot, 0, len(order))
	for _, root := range order {
		var sum float64
		for i, c := range byRoot[root] {
			sum += c.score / float64(i+1)
		}
		scored = append(scored, scoredRoot{root: root, score: sum})
	}
	sort.SliceStable(scored, func(i, j int) bool { return scored[i].score > scored[j].score })
	if len(scored) > q.Limit {
		scored = scored[:q.Limit]
	}

	rootIDs := make([]int64, len(scored))
	for i, sr := range scored {
		rootIDs[i] = sr.root
	}
	meta, err := s.chainMeta(rootIDs)
	if err != nil {
		return nil, err
	}

	// Hydrate only the entries actually returned.
	var want []candidate
	for _, sr := range scored {
		members := byRoot[sr.root]
		if len(members) > q.PerChain {
			members = members[:q.PerChain]
		}
		want = append(want, members...)
	}
	hits, err := s.hydrate(want)
	if err != nil {
		return nil, err
	}
	byID := map[int64]EntryHit{}
	for _, h := range hits {
		byID[h.ID] = h
	}

	out := make([]ChainHit, 0, len(scored))
	for _, sr := range scored {
		m := meta[sr.root]
		ch := ChainHit{
			RootID:    sr.root,
			RootExtID: m.extID,
			Subject:   m.subject,
			Container: m.container,
			Sources:   m.sources,
			Entries:   m.entries,
			Matched:   len(byRoot[sr.root]),
			First:     m.first,
			Last:      m.last,
			Score:     sr.score,
		}
		members := byRoot[sr.root]
		if len(members) > q.PerChain {
			members = members[:q.PerChain]
		}
		for _, c := range members {
			if h, ok := byID[c.id]; ok {
				ch.Best = append(ch.Best, h)
			}
		}
		// A root with no subject of its own inherits the best hit's, which is
		// what a reader recognises the thread by.
		if ch.Subject == "" && len(ch.Best) > 0 {
			ch.Subject = ch.Best[0].Subject
		}
		out = append(out, ch)
	}
	return out, nil
}

// candidate is a fused, un-hydrated hit.
type candidate struct {
	id        int64
	score     float64
	snippet   string
	proseRank int
	identRank int
}

// candidates runs the structural filter, then each text index, then fuses.
func (s *Store) candidates(q Query) ([]candidate, error) {
	where, args := q.filters()

	prose, ident := MatchExpressions(q.Text)
	if prose == "" && ident == "" {
		return s.byRecency(q, where, args)
	}

	// Reciprocal Rank Fusion: each index votes with 1/(k + rank) and the votes
	// are summed. Ranks, not scores, so the two bm25 scales — one over stemmed
	// words, one over trigrams — never have to be made commensurable, and there
	// is no weight to tune. If one index finds nothing its votes are simply
	// absent, which is why an all-prose or all-identifier query needs no special
	// case.
	fused := map[int64]*candidate{}
	get := func(id int64) *candidate {
		c, ok := fused[id]
		if !ok {
			c = &candidate{id: id}
			fused[id] = c
		}
		return c
	}

	if prose != "" {
		rows, err := s.rankOne(q, "entries_fts", prose, where, args, true)
		if err != nil {
			return nil, err
		}
		for i, r := range rows {
			c := get(r.id)
			c.proseRank = i + 1
			c.score += 1 / (rrfK + float64(i+1))
			c.snippet = r.snippet
		}
	}
	if ident != "" {
		rows, err := s.rankOne(q, "entries_ident", ident, where, args, false)
		if err != nil {
			return nil, err
		}
		for i, r := range rows {
			c := get(r.id)
			c.identRank = i + 1
			c.score += 1 / (rrfK + float64(i+1))
			// The prose excerpt is the more readable of the two, so it wins when
			// both exist; the trigram one is a fallback for an id-only match.
			if c.snippet == "" {
				c.snippet = r.snippet
			}
		}
	}

	out := make([]candidate, 0, len(fused))
	for _, c := range fused {
		out = append(out, *c)
	}
	// Ties are broken by id so results are stable across runs; map iteration is
	// not, and a wobbling ranking is indistinguishable from a bug.
	sort.Slice(out, func(i, j int) bool {
		if out[i].score != out[j].score {
			return out[i].score > out[j].score
		}
		return out[i].id < out[j].id
	})
	if len(out) > q.CandidateLimit {
		out = out[:q.CandidateLimit]
	}
	return out, nil
}

type rankedRow struct {
	id      int64
	snippet string
}

// rankOne reads one FTS index, filtered and ordered by bm25.
func (s *Store) rankOne(q Query, table, match, where string, args []any, weighted bool) ([]rankedRow, error) {
	// snippet()'s column argument is -1 for "pick the best column". The literals
	// are constants, not caller input, so inlining them is safe and keeps the
	// bound-parameter order simple.
	bm25 := fmt.Sprintf("bm25(%s)", table)
	if weighted {
		bm25 = fmt.Sprintf("bm25(%s, %g, %g)", table, subjectWeight, bodyWeight)
	}
	stmt := fmt.Sprintf(`
		select %[1]s.rowid,
		       snippet(%[1]s, -1, '%[2]s', '%[3]s', '%[4]s', %[5]d)
		from %[1]s
		join entries e on e.id = %[1]s.rowid
		left join mail_detail md on md.entry_id = e.id
		left join slack_detail sd on sd.entry_id = e.id
		where %[1]s match ? and %[6]s
		order by %[7]s
		limit ?`,
		table, snippetOpen, snippetClose, snippetElide, q.SnippetTokens, where, bm25)

	callArgs := append([]any{match}, args...)
	callArgs = append(callArgs, q.CandidateLimit)
	rows, err := s.db.Query(stmt, callArgs...)
	if err != nil {
		return nil, fmt.Errorf("searching %s for %q: %w", table, match, err)
	}
	defer rows.Close()
	var out []rankedRow
	for rows.Next() {
		var r rankedRow
		if err := rows.Scan(&r.id, &r.snippet); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// byRecency answers a structural-only query. Scores are assigned from the
// recency rank using the same 1/(k+rank) shape, so chain aggregation behaves
// identically whether or not text was supplied.
func (s *Store) byRecency(q Query, where string, args []any) ([]candidate, error) {
	stmt := `
		select e.id
		from entries e
		left join mail_detail md on md.entry_id = e.id
		left join slack_detail sd on sd.entry_id = e.id
		where ` + where + `
		order by e.ts desc, e.id desc
		limit ?`
	rows, err := s.db.Query(stmt, append(append([]any{}, args...), q.CandidateLimit)...)
	if err != nil {
		return nil, fmt.Errorf("filtering entries: %w", err)
	}
	defer rows.Close()
	var out []candidate
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, candidate{id: id, score: 1 / (rrfK + float64(len(out)+1))})
	}
	return out, rows.Err()
}

// filters renders the structural half of the query. It always returns a
// non-empty predicate so callers can concatenate without special-casing.
func (q Query) filters() (string, []any) {
	var preds []string
	var args []any

	if !q.Since.IsZero() {
		preds = append(preds, "e.ts >= ?")
		args = append(args, q.Since.UTC().Unix())
	}
	if !q.Until.IsZero() {
		// Half-open: an "until 1 July" filter must not depend on whether the
		// caller remembered to say 23:59:59.
		preds = append(preds, "e.ts < ?")
		args = append(args, q.Until.UTC().Unix())
	}
	if len(q.Sources) > 0 {
		ph, a := placeholders(q.Sources)
		preds = append(preds, "e.source in ("+ph+")")
		args = append(args, a...)
	}
	if len(q.Containers) > 0 {
		ph, a := placeholders(q.Containers)
		preds = append(preds, "e.container in ("+ph+")")
		args = append(args, a...)
	}
	if len(q.People) > 0 || len(q.PersonIDs) > 0 {
		var alts []string
		if len(q.PersonIDs) > 0 {
			ph, a := placeholders(q.PersonIDs)
			alts = append(alts, "e.person_id in ("+ph+")")
			args = append(args, a...)
		}
		if len(q.People) > 0 {
			sub, a := personSetSQL(q.People)
			alts = append(alts, "e.person_id in ("+sub+")")
			args = append(args, a...)
		}
		preds = append(preds, "("+strings.Join(alts, " or ")+")")
	}
	if len(q.Involving) > 0 {
		sub, a := personSetSQL(q.Involving)
		p := "exists (select 1 from participants pt where pt.entry_id = e.id" +
			" and pt.person_id in (" + sub + ")"
		args = append(args, a...)
		if len(q.InvolvingRoles) > 0 {
			ph, ra := placeholders(q.InvolvingRoles)
			p += " and pt.role in (" + ph + ")"
			args = append(args, ra...)
		}
		preds = append(preds, p+")")
	}
	if q.HasAttachment != nil {
		p := "exists (select 1 from attachments a where a.entry_id = e.id)"
		if !*q.HasAttachment {
			p = "not " + p
		}
		preds = append(preds, p)
	}
	if !q.IncludeNoise && q.Noise != nil && len(q.Noise.Predicates) > 0 {
		// coalesce is load-bearing, not defensive. mail_detail and slack_detail
		// are outer-joined, so a Slack predicate evaluates to NULL on a mail row;
		// `not (NULL or NULL)` is NULL, which WHERE treats as false, and every
		// mail entry would silently vanish from every search. Unknown means
		// "not known to be noise".
		preds = append(preds,
			"not coalesce("+strings.Join(q.Noise.Predicates, " or ")+", 0)")
	}

	if len(preds) == 0 {
		return "1", nil
	}
	return strings.Join(preds, " and "), args
}

// personSetSQL renders a subquery selecting the person ids named by names.
//
// A caller says "alice@example.com", "U04ABC" or "Alice" and should not have to
// declare which kind that is, so each name is normalised under every kind that
// accepts it and matched on (kind, value) — the identities primary key, so this
// is an index seek rather than a scan over lower(value). people.display_name is
// checked as well, since a person can be named in the people row before any
// display-name identity exists for them; that leg only trims and lowercases,
// because SQL cannot cheaply collapse internal whitespace the way
// NormaliseIdentity does, and the identities leg is the exact one.
func personSetSQL(names []string) (string, []any) {
	var rows []string
	var args []any
	var lowered []string
	for _, n := range names {
		for _, kind := range []string{KindEmail, KindSlackUID, KindDisplayName} {
			v, err := NormaliseIdentity(kind, n)
			if err != nil {
				// The name is not expressible as this kind; that is not an error,
				// it is how "which kind is this?" gets answered.
				continue
			}
			rows = append(rows, "(?,?)")
			args = append(args, kind, v)
			if kind == KindDisplayName {
				lowered = append(lowered, v)
			}
		}
	}
	if len(rows) == 0 {
		// Every name was blank. "Filter by nobody" must match nothing, not
		// everything — silently dropping the predicate would be the dangerous
		// reading.
		return "select null where 0", nil
	}
	ph, la := placeholders(lowered)
	args = append(args, la...)
	return `select person_id from identities where (kind, value) in (values ` +
		strings.Join(rows, ",") + `)
		union select id from people where lower(trim(display_name)) in (` + ph + `)`, args
}

// rootsOf walks parent_id upwards for each id and returns the root it reaches.
func (s *Store) rootsOf(ids []int64) (map[int64]int64, error) {
	ph, args := placeholders(ids)
	// The final predicate picks the one row per start where the walk stopped:
	// either the entry has no parent, or the depth cap fired.
	rows, err := s.db.Query(`
		with recursive walk(start, cur, depth) as (
		  select id, id, 0 from entries where id in (`+ph+`)
		  union all
		  select w.start, e.parent_id, w.depth + 1
		    from walk w join entries e on e.id = w.cur
		   where e.parent_id is not null and w.depth < ?
		)
		select w.start, w.cur
		from walk w join entries e on e.id = w.cur
		where e.parent_id is null or w.depth >= ?`,
		append(args, walkDepthCap, walkDepthCap)...)
	if err != nil {
		return nil, fmt.Errorf("walking to chain roots: %w", err)
	}
	defer rows.Close()
	out := map[int64]int64{}
	for rows.Next() {
		var start, root int64
		if err := rows.Scan(&start, &root); err != nil {
			return nil, err
		}
		out[start] = root
	}
	return out, rows.Err()
}

type chainMetaRow struct {
	extID     string
	subject   string
	container string
	sources   []string
	entries   int
	first     time.Time
	last      time.Time
}

// chainMeta walks each root's descendants so a chain can report its true size
// and span, not just the part of it that matched.
func (s *Store) chainMeta(roots []int64) (map[int64]chainMetaRow, error) {
	if len(roots) == 0 {
		return map[int64]chainMetaRow{}, nil
	}
	ph, args := placeholders(roots)
	rows, err := s.db.Query(`
		with recursive down(root, id, depth) as (
		  select id, id, 0 from entries where id in (`+ph+`)
		  union all
		  select d.root, e.id, d.depth + 1
		    from down d join entries e on e.parent_id = d.id
		   where d.depth < ?
		)
		select d.root,
		       (select ext_id from entries where id = d.root),
		       (select coalesce(subject,'') from entries where id = d.root),
		       (select coalesce(container,'') from entries where id = d.root),
		       count(distinct d.id),
		       min(e.ts), max(e.ts),
		       group_concat(distinct e.source)
		from down d join entries e on e.id = d.id
		group by d.root`,
		append(args, walkDepthCap)...)
	if err != nil {
		return nil, fmt.Errorf("summarising chains: %w", err)
	}
	defer rows.Close()
	out := map[int64]chainMetaRow{}
	for rows.Next() {
		var root int64
		var m chainMetaRow
		var first, last int64
		var srcs sql.NullString
		if err := rows.Scan(&root, &m.extID, &m.subject, &m.container,
			&m.entries, &first, &last, &srcs); err != nil {
			return nil, err
		}
		m.first = time.Unix(first, 0).UTC()
		m.last = time.Unix(last, 0).UTC()
		if srcs.Valid && srcs.String != "" {
			m.sources = strings.Split(srcs.String, ",")
			sort.Strings(m.sources)
		}
		out[root] = m
	}
	return out, rows.Err()
}

// hydrate loads the display fields for candidates, preserving their order.
func (s *Store) hydrate(cs []candidate) ([]EntryHit, error) {
	if len(cs) == 0 {
		return nil, nil
	}
	ids := make([]int64, len(cs))
	for i, c := range cs {
		ids[i] = c.id
	}
	ph, args := placeholders(ids)
	rows, err := s.db.Query(`
		select e.id, e.ext_id, e.source, e.ts, coalesce(e.person_id, 0),
		       coalesce(p.display_name, ''), coalesce(e.container, ''),
		       coalesce(e.subject, ''), coalesce(e.permalink, '')
		from entries e left join people p on p.id = e.person_id
		where e.id in (`+ph+`)`, args...)
	if err != nil {
		return nil, fmt.Errorf("loading hits: %w", err)
	}
	defer rows.Close()
	byID := map[int64]EntryHit{}
	for rows.Next() {
		var h EntryHit
		var ts int64
		if err := rows.Scan(&h.ID, &h.ExtID, &h.Source, &ts, &h.PersonID,
			&h.Person, &h.Container, &h.Subject, &h.Permalink); err != nil {
			return nil, err
		}
		h.TS = time.Unix(ts, 0).UTC()
		byID[h.ID] = h
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]EntryHit, 0, len(cs))
	for _, c := range cs {
		h, ok := byID[c.id]
		if !ok {
			continue
		}
		h.Score, h.Snippet = c.score, c.snippet
		h.ProseRank, h.IdentRank = c.proseRank, c.identRank
		out = append(out, h)
	}
	return out, nil
}

// MatchExpressions compiles free text into the two FTS5 MATCH expressions: one
// for the porter index over prose, one for the trigram index over identifiers.
// Either may be empty, meaning "do not consult that index".
//
// Every term goes to the prose index — porter splits CINV-00066864 into `cinv`
// and `00066864`, which is a usable if imprecise match. Only identifier-shaped
// terms go to the trigram index, because sending ordinary words there would
// match them as substrings of unrelated words and drown the fusion in noise.
func MatchExpressions(text string) (prose, ident string) {
	terms := QueryTerms(text)
	if len(terms) == 0 {
		return "", ""
	}
	var pq, iq []string
	for _, t := range terms {
		pq = append(pq, quoteFTS(t))
		// Trigram cannot match anything shorter than a trigram.
		if LooksLikeIdentifier(t) && len([]rune(t)) >= 3 {
			iq = append(iq, quoteFTS(t))
		}
	}
	return strings.Join(pq, " AND "), strings.Join(iq, " AND ")
}

// QueryTerms splits user text into terms. A double-quoted run is one term (an
// FTS5 phrase); otherwise terms are whitespace-separated with leading and
// trailing punctuation trimmed, so a trailing comma does not become part of an
// invoice number.
func QueryTerms(text string) []string {
	var terms []string
	rs := []rune(text)
	for i := 0; i < len(rs); {
		switch {
		case unicode.IsSpace(rs[i]):
			i++
		case rs[i] == '"':
			i++
			start := i
			for i < len(rs) && rs[i] != '"' {
				i++
			}
			if t := trimTerm(string(rs[start:i])); t != "" {
				terms = append(terms, t)
			}
			if i < len(rs) {
				i++ // closing quote
			}
		default:
			start := i
			for i < len(rs) && !unicode.IsSpace(rs[i]) && rs[i] != '"' {
				i++
			}
			if t := trimTerm(string(rs[start:i])); t != "" {
				terms = append(terms, t)
			}
		}
	}
	return terms
}

func trimTerm(s string) string {
	return strings.TrimFunc(s, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

// LooksLikeIdentifier reports whether a term is the shape the word tokenizers
// mangle: it contains a digit, or a hyphen between two alphanumerics. That
// covers CINV-00066864, 0000035363EA0FE and an ICP. It also catches ordinary
// hyphenated words like "re-send", and that is tolerable on purpose: a false
// positive only adds a second opinion to the fusion, whereas a false negative
// loses the only index that can find the term at all.
func LooksLikeIdentifier(term string) bool {
	rs := []rune(term)
	for i, r := range rs {
		if unicode.IsDigit(r) {
			return true
		}
		if r == '-' && i > 0 && i < len(rs)-1 &&
			isAlnum(rs[i-1]) && isAlnum(rs[i+1]) {
			return true
		}
	}
	return false
}

func isAlnum(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }

// quoteFTS renders a term as an FTS5 string literal, which is the only way to
// stop a term that happens to read as an operator (AND, NEAR, *) from being
// parsed as one.
func quoteFTS(term string) string {
	return `"` + strings.ReplaceAll(term, `"`, `""`) + `"`
}

// placeholders renders `?,?,?` and the matching args for a slice.
func placeholders[T any](vs []T) (string, []any) {
	args := make([]any, len(vs))
	for i, v := range vs {
		args[i] = v
	}
	return strings.TrimSuffix(strings.Repeat("?,", len(vs)), ","), args
}
