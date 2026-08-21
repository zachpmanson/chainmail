package corpus

import (
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/zachpmanson/chainmail/internal/embed"
	"github.com/zachpmanson/chainmail/internal/unnest"
)

// Semantic retrieval stores one vector per entry and none per chain, which is
// the load-bearing choice in this file.
//
// Entries are the unit that has a body, a hash and a reply edge, so an entry
// vector is incremental, verifiable and cheap to invalidate. A chain vector
// would be the better ranking signal for "is this thread about X at all" — but
// only as a vector of a *generated summary*, since a centroid of members
// averages a six-month thread into the middle of nothing. Generating summaries
// means a second, generative model in the loop, with its own prompt, its own
// nondeterminism and its own invalidation problem (a chain changes every time
// it gets a reply). SearchChains already answers thread-level topicality by
// aggregating entry hits with harmonic damping, which is evidence-based and
// needs no model at all.
//
// The cost of that decision: a thread that is collectively about a topic while
// no single message says so ranks below one where a single message does. That is
// the case a chain summary would win, and it is the reason this table is keyed
// so a chain_embeddings table can sit beside it later rather than replace it.

const (
	// prepVersion versions the text handed to the model. Bumping it re-embeds
	// the corpus, which is the point: the preparation drops quoted history and
	// rewrites URLs, so a change to it changes every vector while leaving every
	// body_sha identical.
	prepVersion = 1

	// minEmbedWords is the floor for embedding an entry at all. "thanks, will
	// do" carries no topic, embeds to whatever direction those three tokens
	// happen to point, and then competes for result slots against messages that
	// mean something. Below the floor the entry keeps its lexical indexes, where
	// a short message is found by the words it actually contains.
	minEmbedWords = 5

	// maxEmbedRunes caps the text sent per entry. nomic-embed-text takes 2048
	// tokens and silently truncates beyond it, so the cap is here to make the
	// truncation ours and visible rather than the server's and invisible.
	// The alternative — chunking a long message into several vectors — would
	// make an entry's identity no longer one row, and long messages in this
	// corpus put their subject matter at the top.
	maxEmbedRunes = 6000

	// Skip reasons, stored in entry_embeddings.skip.
	skipEmpty = "no text"
	skipShort = "too short"

	defaultEmbedBatch = 32

	// defaultSemanticTopK is how deep the vector ranking votes. Well below
	// Query.CandidateLimit on purpose: see SemanticQuery.TopK.
	defaultSemanticTopK = 100
)

// SemanticQuery is a pre-computed query vector.
//
// The caller embeds the query text rather than handing search an Embedder, so
// that "ollama is down" surfaces where the user typed the query instead of as
// an empty result set from a function whose job is to return few results.
type SemanticQuery struct {
	// Vector is the query embedding, L2-normalised and of the model's width.
	Vector []float32
	// Model must match what was stored, or nothing is comparable. A mismatch
	// finds no rows, which is why EmbedStats reports per-model counts.
	Model string
	// Only ranks by vector alone. Off, the vector ranking is fused with the
	// lexical ones; on, the lexical indexes are not consulted at all.
	Only bool

	// TopK is how many entries the vector ranking contributes to the fusion.
	// It exists because a vector scan is unlike a keyword index in the way that
	// matters here: an index returns only the entries containing the terms,
	// whereas every entry in the corpus has *some* cosine to the query, so an
	// untruncated vector ranking casts a vote for the entire corpus and its long
	// tail of near-irrelevance dilutes what the lexical indexes found. Zero
	// means defaultSemanticTopK.
	TopK int

	// MinSimilarity drops anything below a cosine floor. Zero means no floor at
	// all, including no floor on anti-correlated entries; write a tiny positive
	// value to exclude those.
	//
	// It is deliberately not defaulted to a number. The cosine at which a real
	// model stops being relevant is a property of that model — an embedding
	// model puts two unrelated English sentences well above zero — so a
	// corpus-wide constant here would be a guess wearing a threshold's clothes,
	// and it would silently cut recall on the queries semantic search exists
	// for. TopK is the model-independent cut; this is for a caller who has
	// calibrated against their own model.
	MinSimilarity float64
}

// EmbedTextFor renders the string that represents an entry to the model.
//
// Three decisions are baked in here.
//
// Quoted history is dropped: for mail, only the first peeled block — what the
// sender actually wrote — is used. The quoted remainder is already stored as
// entries of its own, so embedding it again would give one long forward a vector
// that is the average of twenty messages, and would make every message in a
// thread near-identical to every other. That is the failure the corpus's
// deduplication exists to prevent, reappearing in vector space.
//
// The subject is included, the author is not. A subject is a topic label and is
// the only text some replies have. A name is not a topic: it is already an exact
// structural filter (Query.People, Query.Involving), and adding it to the vector
// would pull every message one person sent towards every other, which is a
// worse answer to "who said this" and a corruption of "what is this about".
//
// A URL is reduced to its host. The path and query of a signed link are
// high-entropy noise that consumes the token budget, while the host is real
// evidence of who a thread is with — on one trail the counterparty's domain was
// the only place the topic word appeared at all.
//
// The returned reason is non-empty when the entry should not be embedded, and
// the text is then empty.
func EmbedTextFor(source, subject, bodyText string) (text, reason string) {
	body := bodyText
	if source == SourceMail {
		body = ownWords(bodyText)
	}
	body = reduceURLs(body)
	subject = stripReplyPrefixes(subject)

	var b strings.Builder
	if subject != "" {
		b.WriteString(subject)
		b.WriteString("\n\n")
	}
	b.WriteString(body)
	text = strings.TrimSpace(collapseBlankRuns(b.String()))

	switch {
	case text == "":
		return "", skipEmpty
	case countContentWords(text) < minEmbedWords:
		return "", skipShort
	}
	return clipRunes(text, maxEmbedRunes), ""
}

// ownWords is the part of a mail body its sender wrote, and nothing else.
//
// It is the first peeled block, but only when that block is the sender's own:
// unquoted, at depth zero and introduced by no boundary. A body that opens
// straight into "---------- Forwarded message ---------", or into an
// attribution, is a bare forward or an empty reply — its first block is already
// somebody else's text, stored as an entry of its own, and taking it here would
// give this entry a vector of a message it merely relayed. The honest answer is
// that such an entry contributed no words, so it falls back on its subject and
// usually fails the content gate, which is correct: there is nothing here to
// find that is not findable at the original.
func ownWords(bodyText string) string {
	blocks := unnest.Peel(bodyText)
	if len(blocks) == 0 {
		return ""
	}
	first := blocks[0]
	if first.Depth != 0 || first.Kind != unnest.KindNone || first.Sentinel != "" {
		return ""
	}
	return first.Text
}

var (
	reURL = regexp.MustCompile(`(?i)\bhttps?://([^\s/?#]+)[^\s]*`)
	// A subject can accumulate a dozen of these; the leading run is stripped as
	// a whole rather than one prefix at a time.
	reReplyPrefix = regexp.MustCompile(`(?i)^\s*(?:(?:re|aw|fw|fwd|rv|sv|vs|antw)\s*(?:\[\d+\])?\s*:\s*)+`)
	reBlankRuns   = regexp.MustCompile(`\n{3,}`)
)

func reduceURLs(s string) string {
	return reURL.ReplaceAllString(s, "$1")
}

func stripReplyPrefixes(s string) string {
	return strings.TrimSpace(reReplyPrefix.ReplaceAllString(s, ""))
}

func collapseBlankRuns(s string) string {
	return reBlankRuns.ReplaceAllString(s, "\n\n")
}

// countContentWords counts whitespace-separated runs holding at least one
// letter. Digits alone do not count: a Slack post that is nothing but a
// timestamp or a row of figures has no topic to embed, and the trigram index is
// the right place to find a number anyway.
func countContentWords(s string) int {
	n := 0
	for _, f := range strings.Fields(s) {
		if strings.IndexFunc(f, unicode.IsLetter) >= 0 {
			n++
		}
	}
	return n
}

func clipRunes(s string, limit int) string {
	rs := []rune(s)
	if len(rs) <= limit {
		return s
	}
	cut := string(rs[:limit])
	// Cut at a word boundary when one is near, so the last token handed to the
	// model is a word rather than a fragment of one.
	if i := strings.LastIndexFunc(cut, unicode.IsSpace); i > limit-64 {
		cut = cut[:i]
	}
	return strings.TrimSpace(cut)
}

// encodeVector renders a vector as little-endian float32, which is the widest
// format worth using here: float16 or int8 quantisation halves or quarters the
// bytes at a small recall cost, and starts to matter when the corpus is large
// enough that the scan is I/O bound rather than at ~20k rows.
func encodeVector(v []float32) []byte {
	b := make([]byte, 4*len(v))
	for i, x := range v {
		binary.LittleEndian.PutUint32(b[4*i:], math.Float32bits(x))
	}
	return b
}

// decodeVector reads a stored vector, refusing anything that is not exactly dim
// floats wide. A blob of the wrong width is the one corruption that would
// otherwise produce a similarity — computed over the prefix two vectors happen
// to share — rather than an error.
func decodeVector(b []byte, dim int) ([]float32, error) {
	v := make([]float32, dim)
	if err := decodeVectorInto(v, b); err != nil {
		return nil, err
	}
	return v, nil
}

// decodeVectorInto decodes into a caller-owned buffer, whose length is the
// expected width. The scan reuses one buffer for every row: at 768 dimensions a
// fresh slice per row is 3 KB of garbage per entry, which over a whole corpus
// costs more than the arithmetic it feeds.
func decodeVectorInto(dst []float32, b []byte) error {
	if len(b) != 4*len(dst) {
		return fmt.Errorf("%w: %d-byte vector is not %d float32s",
			embed.ErrDimMismatch, len(b), len(dst))
	}
	for i := range dst {
		dst[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[4*i:]))
	}
	return nil
}

// pendingEmbed is one entry that needs a vector.
type pendingEmbed struct {
	id      int64
	source  string
	subject string
	body    string
	bodySHA string
}

// embedSelection is the shared body of "which entries want embedding".
//
// Noise is excluded here rather than embedded and filtered later: bots and
// channel join/leave chatter are a third of the Slack half of this corpus, they
// are already excluded from every default search, and embedding them would
// spend the model's time to produce vectors nothing will ever rank. The
// consequence is that a search with IncludeNoise set finds noise lexically but
// never semantically, which is the right trade when the alternative is paying
// for 8,000 vectors of "has joined the channel".
func embedSelection(model string, dim int) (string, []any) {
	noise := DefaultNoiseFilter()
	return `
		from entries e
		left join mail_detail md on md.entry_id = e.id
		left join slack_detail sd on sd.entry_id = e.id
		left join entry_embeddings v on v.entry_id = e.id and v.model = ?
		where ` + notNoiseSQL(noise) + `
		  and (v.entry_id is null or v.body_sha <> e.body_sha
		       or v.prep <> ? or v.dim <> ?)`,
		[]any{model, prepVersion, dim}
}

// BackfillOptions tunes a backfill run.
type BackfillOptions struct {
	// Batch is how many entries are prepared, embedded and committed together.
	// It is also the granularity of an interruption: a killed run loses at most
	// one batch, and re-running redoes exactly that batch.
	Batch int
	// Limit stops the run after this many entries have been dealt with. Zero
	// means the whole corpus.
	Limit int
	// Progress, if set, is called after each committed batch.
	Progress func(BackfillProgress)
}

// BackfillProgress is a running count, reported per committed batch.
type BackfillProgress struct {
	Done     int
	Pending  int // as measured at the start of the run
	Embedded int
	Skipped  int
}

// BackfillReport is what a run did.
type BackfillReport struct {
	Model    string
	Dim      int
	Pending  int // needing work when the run started
	Embedded int
	Skipped  int
}

// BackfillEmbeddings embeds every entry that has no current vector.
//
// Resumable by construction rather than by bookkeeping: what needs doing is
// re-derived from the corpus each batch, so there is no cursor to persist and
// no way for a crash to leave a claimed-but-unembedded range. Each batch is one
// transaction, and entries are taken in id order so two runs see the same work
// in the same sequence — a run interrupted at entry 900 resumes at 901, and a
// run interrupted mid-commit redoes that batch and no other.
func (s *Store) BackfillEmbeddings(ctx context.Context, e embed.Embedder, opt BackfillOptions) (BackfillReport, error) {
	rep := BackfillReport{Model: e.Model(), Dim: e.Dim()}
	if rep.Model == "" || rep.Dim <= 0 {
		return rep, errors.New("embedder must report a model name and a positive dimension")
	}
	batch := opt.Batch
	if batch <= 0 {
		batch = defaultEmbedBatch
	}

	sel, selArgs := embedSelection(rep.Model, rep.Dim)
	if err := s.db.QueryRowContext(ctx,
		`select count(*) `+sel, selArgs...).Scan(&rep.Pending); err != nil {
		return rep, fmt.Errorf("counting entries to embed: %w", err)
	}

	for rep.Embedded+rep.Skipped < rep.Pending {
		if err := ctx.Err(); err != nil {
			return rep, err
		}
		want := batch
		if opt.Limit > 0 {
			left := opt.Limit - (rep.Embedded + rep.Skipped)
			if left <= 0 {
				break
			}
			want = min(want, left)
		}
		batchDone, err := s.embedBatch(ctx, e, sel, selArgs, want)
		if err != nil {
			return rep, err
		}
		if batchDone.embedded+batchDone.skipped == 0 {
			// Nothing left, or nothing progressing. Either way, looping again
			// would spin: the selection is the same query it was a moment ago.
			break
		}
		rep.Embedded += batchDone.embedded
		rep.Skipped += batchDone.skipped
		if opt.Progress != nil {
			opt.Progress(BackfillProgress{
				Done:     rep.Embedded + rep.Skipped,
				Pending:  rep.Pending,
				Embedded: rep.Embedded,
				Skipped:  rep.Skipped,
			})
		}
	}
	return rep, nil
}

type batchResult struct{ embedded, skipped int }

func (s *Store) embedBatch(ctx context.Context, e embed.Embedder, sel string, selArgs []any, want int) (batchResult, error) {
	var res batchResult
	args := append(append([]any{}, selArgs...), want)
	rows, err := s.db.QueryContext(ctx, `
		select e.id, e.source, coalesce(e.subject,''), coalesce(e.body_text,''), e.body_sha `+
		sel+`
		order by e.id
		limit ?`, args...)
	if err != nil {
		return res, fmt.Errorf("selecting entries to embed: %w", err)
	}
	var pend []pendingEmbed
	for rows.Next() {
		var p pendingEmbed
		if err := rows.Scan(&p.id, &p.source, &p.subject, &p.body, &p.bodySHA); err != nil {
			rows.Close()
			return res, err
		}
		pend = append(pend, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return res, err
	}
	if len(pend) == 0 {
		return res, nil
	}

	// Prepare first, so the model is only asked about entries that will keep a
	// vector. A skipped entry still gets a row: without one, every future run
	// re-examines the same third of the corpus that has nothing to embed.
	type job struct {
		p      pendingEmbed
		text   string
		reason string
	}
	jobs := make([]job, 0, len(pend))
	var texts []string
	for _, p := range pend {
		text, reason := EmbedTextFor(p.source, p.subject, p.body)
		jobs = append(jobs, job{p: p, text: text, reason: reason})
		if reason == "" {
			texts = append(texts, text)
		}
	}

	var vecs [][]float32
	if len(texts) > 0 {
		vecs, err = e.Embed(ctx, texts)
		if err != nil {
			return res, err
		}
		if len(vecs) != len(texts) {
			return res, fmt.Errorf("embedder returned %d vectors for %d texts",
				len(vecs), len(texts))
		}
	}

	tx, err := s.db.Begin()
	if err != nil {
		return res, err
	}
	defer tx.Rollback()
	next := 0
	for _, j := range jobs {
		if j.reason != "" {
			if err := putEmbedding(tx, j.p.id, e.Model(), e.Dim(), j.p.bodySHA, nil, j.reason); err != nil {
				return res, err
			}
			res.skipped++
			continue
		}
		v := vecs[next]
		next++
		if len(v) != e.Dim() {
			return res, fmt.Errorf("%w: entry %d got a %d-wide vector from %s, expected %d",
				embed.ErrDimMismatch, j.p.id, len(v), e.Model(), e.Dim())
		}
		if err := putEmbedding(tx, j.p.id, e.Model(), e.Dim(), j.p.bodySHA, v, ""); err != nil {
			return res, err
		}
		res.embedded++
	}
	if err := tx.Commit(); err != nil {
		return res, fmt.Errorf("committing %d embeddings: %w", res.embedded+res.skipped, err)
	}
	return res, nil
}

func putEmbedding(tx *sql.Tx, entryID int64, model string, dim int, bodySHA string, v []float32, reason string) error {
	var blob any
	if v != nil {
		blob = encodeVector(v)
	}
	_, err := tx.Exec(`
		insert into entry_embeddings
		  (entry_id, model, dim, body_sha, prep, vec, skip, embedded_at)
		values (?,?,?,?,?,?,?,?)
		on conflict(entry_id, model) do update set
		  dim=excluded.dim, body_sha=excluded.body_sha, prep=excluded.prep,
		  vec=excluded.vec, skip=excluded.skip, embedded_at=excluded.embedded_at`,
		entryID, model, dim, bodySHA, prepVersion, blob, nullStr(reason), time.Now().Unix())
	if err != nil {
		return fmt.Errorf("storing embedding for entry %d: %w", entryID, err)
	}
	return nil
}

// Embedding returns one entry's stored vector for a model, or nil if it has
// none. Present for verification: a vector that does not survive the round trip
// through the blob column is undetectable from the ranking alone.
func (s *Store) Embedding(entryID int64, model string) ([]float32, error) {
	var blob []byte
	var dim int
	err := s.db.QueryRow(
		`select dim, vec from entry_embeddings where entry_id=? and model=?`,
		entryID, model).Scan(&dim, &blob)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if blob == nil {
		return nil, nil
	}
	return decodeVector(blob, dim)
}

// EmbedModelStats is coverage for one model.
type EmbedModelStats struct {
	Model    string
	Dim      int
	Vectors  int
	Skipped  int
	Stale    int // stored, but the entry's text or the preparation moved on
	Eligible int // entries that would be embedded by a full backfill
}

// EmbedStats reports coverage per model, so a half-finished migration is
// visible as such rather than as a corpus that mysteriously lost recall.
func (s *Store) EmbedStats() ([]EmbedModelStats, error) {
	// max(dim) is a group-by formality: a model's rows all share its width,
	// because a response of any other width is refused before it is stored.
	rows, err := s.db.Query(`
		select model, max(dim),
		       sum(vec is not null), sum(vec is null),
		       sum(v.body_sha <> e.body_sha or v.prep <> ?)
		from entry_embeddings v join entries e on e.id = v.entry_id
		group by model order by model`, prepVersion)
	if err != nil {
		return nil, fmt.Errorf("summarising embeddings: %w", err)
	}
	defer rows.Close()
	var out []EmbedModelStats
	for rows.Next() {
		var m EmbedModelStats
		if err := rows.Scan(&m.Model, &m.Dim, &m.Vectors, &m.Skipped, &m.Stale); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Eligible is a second query per model rather than part of the aggregate,
	// because it counts entries that have no row in this table at all — which is
	// the number that says how much of a backfill is left.
	for i := range out {
		sel, args := embedSelection(out[i].Model, out[i].Dim)
		if err := s.db.QueryRow(`select count(*) `+sel, args...).Scan(&out[i].Eligible); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// PruneEmbeddings drops every vector not produced by keep, and reports how many
// rows went. Run it after a model migration finishes: until then the superseded
// rows are what keeps search answering.
func (s *Store) PruneEmbeddings(keep string) (int64, error) {
	r, err := s.db.Exec(`delete from entry_embeddings where model <> ?`, keep)
	if err != nil {
		return 0, fmt.Errorf("pruning embeddings: %w", err)
	}
	return r.RowsAffected()
}

// semanticRow is one entry ranked by vector similarity.
type semanticRow struct {
	id  int64
	sim float64
}

// semanticRank scores every stored vector against the query and returns the
// best, most similar first.
//
// The scan is deliberate. modernc.org/sqlite cannot load native extensions, so
// sqlite-vec is not available at all — but at this corpus's size an ANN index
// would buy nothing anyway: the whole pass, blob decode included, is
// milliseconds. The structural filters are applied in SQL so the scan reads only
// the rows the query could return, which makes a narrow query cheaper than a
// broad one instead of the same price.
func (s *Store) semanticRank(q Query, sem SemanticQuery, where string, args []any) ([]semanticRow, error) {
	dim := len(sem.Vector)
	if dim == 0 {
		return nil, errors.New("semantic search needs a query vector")
	}
	if sem.Model == "" {
		return nil, errors.New("semantic search needs the model that produced the vector")
	}
	topK := sem.TopK
	if topK <= 0 {
		topK = defaultSemanticTopK
	}
	topK = min(topK, q.CandidateLimit)
	// dim is matched in SQL as well as checked per row: a stored vector of
	// another width is excluded before its bytes are read, rather than read and
	// then rejected.
	stmt := `
		select v.entry_id, v.vec
		from entry_embeddings v
		join entries e on e.id = v.entry_id
		left join mail_detail md on md.entry_id = e.id
		left join slack_detail sd on sd.entry_id = e.id
		where v.model = ? and v.dim = ? and v.vec is not null and ` + where
	callArgs := append([]any{sem.Model, dim}, args...)
	rows, err := s.db.Query(stmt, callArgs...)
	if err != nil {
		return nil, fmt.Errorf("scanning embeddings: %w", err)
	}
	defer rows.Close()

	var out []semanticRow
	scratch := make([]float32, dim)
	for rows.Next() {
		var id int64
		var blob []byte
		if err := rows.Scan(&id, &blob); err != nil {
			return nil, err
		}
		if err := decodeVectorInto(scratch, blob); err != nil {
			return nil, fmt.Errorf("entry %d: %w", id, err)
		}
		sim, err := embed.Dot(scratch, sem.Vector)
		if err != nil {
			return nil, fmt.Errorf("entry %d: %w", id, err)
		}
		if sem.MinSimilarity != 0 && sim < sem.MinSimilarity {
			continue
		}
		out = append(out, semanticRow{id: id, sim: sim})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Ties broken by id, for the same reason the lexical fusion does it: a
	// ranking that wobbles between runs is indistinguishable from a bug.
	sort.Slice(out, func(i, j int) bool {
		if out[i].sim != out[j].sim {
			return out[i].sim > out[j].sim
		}
		return out[i].id < out[j].id
	})
	if len(out) > topK {
		out = out[:topK]
	}
	return out, nil
}
