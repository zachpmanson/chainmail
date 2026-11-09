package corpus

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/zachpmanson/chainmail/internal/unnest"
)

// OpenReadOnly neither migrates nor changes journal mode. Version 28 is the
// last schema before quote evidence; absent evidence columns mean legacy NULL.
func OpenReadOnly(path string) (*Store, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	u := url.URL{Scheme: "file", Path: abs}
	db, err := sql.Open("sqlite", u.String()+"?mode=ro&_pragma=query_only(1)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	var v int
	if err = db.QueryRow(`select coalesce(max(v),0) from schema_version`).Scan(&v); err != nil || v < 28 || v > len(migrations) {
		db.Close()
		return nil, fmt.Errorf("readonly quote preview requires schema 28..%d (got %d): %v", len(migrations), v, err)
	}
	return &Store{db: db}, nil
}

type QuoteSelection struct {
	ExtID     string
	Container string
	AfterID   int64
}
type QuoteLimits struct {
	Entries int
	Hosts   int
	Bytes   int64
}

var DefaultQuoteLimits = QuoteLimits{100, 1000, 32 << 20}

func (l QuoteLimits) validate() error {
	if l.Entries < 1 || l.Entries > 100 || l.Hosts < 1 || l.Hosts > 1000 || l.Bytes < 1 || l.Bytes > 32<<20 {
		return errors.New("quote limits must be within 100 entries, 1000 hosts, 32 MiB")
	}
	return nil
}

type QuoteEvidence struct {
	HostID         int64
	HostDigest     string
	SentinelDigest string
}
type QuoteMetadataChange struct {
	ID           int64
	ExtID        string
	ParsedKey    string
	OldTS        int64
	OldTZ        string
	OldBasis     string
	OldPrecision string
	TS           int64
	TZ           string
	Basis        string
	Precision    string
	RowDigest    string
	Evidence     []QuoteEvidence
}
type QuoteMetadataSkip struct {
	ID     int64
	ExtID  string
	Reason string
}

// QuoteMetadataPlan contains local identifiers and digests, never source prose.
// Digest detects edits, not authorisation: the operator must review this file.
type QuoteMetadataPlan struct {
	Version        int
	ParserVersion  int
	SourceRevision int64
	Selection      QuoteSelection
	Limits         QuoteLimits
	Changes        []QuoteMetadataChange
	Skipped        []QuoteMetadataSkip
	Hosts          int
	Bytes          int64
	More           bool
	NextID         int64
	Digest         string
}

func quoteDigest(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func planDigest(p QuoteMetadataPlan) string { p.Digest = ""; return quoteDigest(p) }

// Validate checks the reviewed envelope before opening a writable database.
// Apply also compares it with a freshly derived plan under the writer lock.
func (p QuoteMetadataPlan) Validate() error {
	if p.Version != 1 || p.ParserVersion != unnest.DateParserVersion || p.Digest != planDigest(p) {
		return errors.New("invalid quote plan digest/version")
	}
	if (p.Selection.ExtID == "") == (p.Selection.Container == "") || p.Selection.AfterID < 0 {
		return errors.New("invalid quote plan selection")
	}
	return p.Limits.validate()
}

type quoteRow struct {
	ID        int64
	Ext       string
	TS        int64
	TZ        string
	Basis     string
	Precision string
	Person    int64
	Body      string
	Quoted    bool
	Derived   bool
	Authority bool
}

func quoteCapabilities(q corpusQuery) (bool, error) {
	var n int
	err := q.QueryRow(`select count(*) from pragma_table_info('entries') where name='quote_time_basis'`).Scan(&n)
	return n == 1, err
}
func readQuoteRow(q corpusQuery, id int64, modern bool) (quoteRow, error) {
	var r quoteRow
	cols := `'', ''`
	if modern {
		cols = `coalesce(quote_time_basis,''),coalesce(quote_time_precision,'')`
	}
	err := q.QueryRow(`select id,ext_id,ts,coalesce(tz,''),`+cols+`,coalesce(person_id,0),coalesce(body_text,''),quoted,derived,
 exists(select 1 from mail_detail where entry_id=e.id) or exists(select 1 from gmail_copies where entry_id=e.id)
 or exists(select 1 from sightings where entry_id=e.id and kind<>'quoted')
 from entries e where id=? and source='mail'`, id).Scan(&r.ID, &r.Ext, &r.TS, &r.TZ, &r.Basis, &r.Precision, &r.Person, &r.Body, &r.Quoted, &r.Derived, &r.Authority)
	return r, err
}

type quoteHost struct {
	evidence QuoteEvidence
	quotes   []unnest.Recovered
}
type quoteBudget struct {
	limits QuoteLimits
	hosts  map[int64]quoteHost
	bytes  int64
}

var errQuoteLimit = errors.New("evidence_limit")

func (b *quoteBudget) host(ctx context.Context, q corpusQuery, id int64) (quoteHost, error) {
	if err := ctx.Err(); err != nil {
		return quoteHost{}, err
	}
	if h, ok := b.hosts[id]; ok {
		return h, nil
	}
	var size int64
	err := q.QueryRow(`select length(cast(coalesce(body_text,'') as blob))+length(cast(coalesce(body_html,'') as blob)) from entries where id=?`, id).Scan(&size)
	if err != nil {
		return quoteHost{}, err
	}
	if len(b.hosts) >= b.limits.Hosts || size > b.limits.Bytes-b.bytes {
		return quoteHost{}, errQuoteLimit
	}
	var source, ext, body, html string
	var ts int64
	if err = q.QueryRow(`select source,ext_id,ts,coalesce(body_text,''),coalesce(body_html,'') from entries where id=?`, id).Scan(&source, &ext, &ts, &body, &html); err != nil {
		return quoteHost{}, err
	}
	h := quoteHost{evidence: QuoteEvidence{HostID: id, HostDigest: quoteDigest([]any{source, ext, ts, body, html})}}
	for _, block := range unnest.Peel(body) {
		if block.Sentinel != "" {
			h.quotes = append(h.quotes, unnest.Parse(block))
		}
	}
	b.hosts[id] = h
	b.bytes += size
	return h, nil
}
func hostSightings(q corpusQuery, id int64, limit int) ([]int64, bool, error) {
	rows, err := q.Query(`select seen_in,coalesce(detail,'') from sightings where entry_id=? and kind='quoted' order by seen_in limit ?`, id, limit+1)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	var ids []int64
	inferred := false
	for rows.Next() {
		var host sql.NullInt64
		var detail string
		if err = rows.Scan(&host, &detail); err != nil {
			return nil, false, err
		}
		if !host.Valid {
			return nil, false, errors.New("missing_host")
		}
		ids = append(ids, host.Int64)
		inferred = inferred || strings.Contains(detail, "time inferred from host")
	}
	if len(ids) > limit {
		return nil, false, errQuoteLimit
	}
	return ids, inferred, rows.Err()
}
func recoveredAuthor(q corpusQuery, rec unnest.Recovered, person int64) bool {
	if person == 0 || rec.Address == "" {
		return false
	}
	a, ok := ParseAddress(rec.Address)
	if !ok {
		return false
	}
	var id int64
	return q.QueryRow(`select person_id from identities where kind='email' and value=?`, strings.ToLower(a.Addr)).Scan(&id) == nil && id == person
}

// quoteProposal is shared by explicit planning and the narrow ingest key bridge.
// Every sighting host must account exactly once for this nonempty legacy body.
func quoteProposal(ctx context.Context, q corpusQuery, r quoteRow, modern bool, b *quoteBudget) (QuoteMetadataChange, string, error) {
	var c QuoteMetadataChange
	refuse := func(s string) (QuoteMetadataChange, string, error) { return c, s, nil }
	if !r.Quoted || r.Authority {
		return refuse("mailbox_authority")
	}
	if r.Derived {
		return refuse("derived")
	}
	if r.Basis == "stated" {
		return refuse("already_stated")
	}
	if strings.TrimSpace(r.Body) == "" || r.Ext != unnest.TextKey(r.Body) {
		return refuse("not_legacy_text_key")
	}
	ids, inferred, err := hostSightings(q, r.ID, b.limits.Hosts)
	if err != nil {
		if errors.Is(err, errQuoteLimit) {
			return refuse("evidence_limit")
		}
		return refuse("missing_host")
	}
	if len(ids) == 0 {
		return refuse("missing_host")
	}
	if r.Basis != "inferred" && !inferred {
		return refuse("unproved_inference")
	}
	var consensus unnest.Recovered
	for _, id := range ids {
		h, err := b.host(ctx, q, id)
		if err != nil {
			if ctx.Err() != nil {
				return c, "", ctx.Err()
			}
			if errors.Is(err, errQuoteLimit) {
				return refuse("evidence_limit")
			}
			return refuse("missing_host")
		}
		var matches []unnest.Recovered
		for _, rec := range h.quotes {
			if rec.Block.Text == r.Body {
				matches = append(matches, rec)
			}
		}
		if len(matches) != 1 {
			return refuse("ambiguous_or_partial_host")
		}
		rec := matches[0]
		if rec.Sent.IsZero() || (rec.DatePrecision != unnest.PrecisionMinute && rec.DatePrecision != unnest.PrecisionSecond) {
			return refuse("incomplete_date")
		}
		if !recoveredAuthor(q, rec, r.Person) {
			return refuse("author_mismatch")
		}
		if consensus.Key != "" && (rec.Key != consensus.Key || rec.TZ != consensus.TZ || rec.DatePrecision != consensus.DatePrecision) {
			return refuse("host_disagreement")
		}
		consensus = rec
		ev := h.evidence
		ev.SentinelDigest = quoteDigest(rec.Block.Sentinel)
		c.Evidence = append(c.Evidence, ev)
	}
	if r.TZ != "" && r.TZ != consensus.TZ {
		return refuse("zone_conflict")
	}
	var collision int
	if err = q.QueryRow(`select count(*) from entries where source='mail' and ext_id=? and id<>?`, consensus.Key, r.ID).Scan(&collision); err != nil {
		return c, "", err
	}
	if collision != 0 {
		return refuse("key_collision")
	}
	if modern {
		if err = q.QueryRow(`select count(*) from quote_key_aliases where source='mail' and ((parsed_key=? and entry_id<>?) or (entry_id=? and parsed_key<>?))`, consensus.Key, r.ID, r.ID, consensus.Key).Scan(&collision); err != nil {
			return c, "", err
		}
		if collision != 0 {
			return refuse("alias_conflict")
		}
		exists, err := aliasTarget(q, consensus.Key)
		if err != nil {
			return c, "", err
		}
		if exists != 0 {
			if _, err = validatedQuoteAlias(ctx, q, consensus.Key, b); err != nil {
				return refuse("stale_alias")
			}
		}
	}
	c.ID = r.ID
	c.ExtID = r.Ext
	c.ParsedKey = consensus.Key
	c.OldTS = r.TS
	c.OldTZ = r.TZ
	c.OldBasis = r.Basis
	c.OldPrecision = r.Precision
	c.TS = consensus.Sent.Unix()
	c.Basis = "stated"
	c.TZ = consensus.TZ
	c.Precision = consensus.DatePrecision
	c.RowDigest = quoteDigest(r)
	return c, "", nil
}

func (s *Store) PlanQuotedMetadata(ctx context.Context, selection QuoteSelection, limits QuoteLimits) (QuoteMetadataPlan, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return QuoteMetadataPlan{}, err
	}
	defer tx.Rollback()
	return planQuotedMetadata(ctx, tx, selection, limits)
}
func planQuotedMetadata(ctx context.Context, q corpusQuery, sel QuoteSelection, limits QuoteLimits) (QuoteMetadataPlan, error) {
	p := QuoteMetadataPlan{Version: 1, ParserVersion: unnest.DateParserVersion, Selection: sel, Limits: limits}
	if (sel.ExtID == "") == (sel.Container == "") || sel.AfterID < 0 {
		return p, errors.New("select exactly one extId or container and a nonnegative cursor")
	}
	if err := limits.validate(); err != nil {
		return p, err
	}
	state, err := readReconcileState(q)
	if err != nil {
		return p, err
	}
	p.SourceRevision = state.Source
	modern, err := quoteCapabilities(q)
	if err != nil {
		return p, err
	}
	field, value := "ext_id", sel.ExtID
	if sel.Container != "" {
		field, value = "container", sel.Container
	}
	rows, err := q.Query(`select id from entries where source='mail' and `+field+`=? and id>? order by id limit ?`, value, sel.AfterID, limits.Entries+1)
	if err != nil {
		return p, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return p, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return p, err
	}
	if len(ids) > limits.Entries {
		p.More = true
		ids = ids[:limits.Entries]
	}
	b := &quoteBudget{limits: limits, hosts: map[int64]quoteHost{}}
	for _, id := range ids {
		if err = ctx.Err(); err != nil {
			return p, err
		}
		p.NextID = id
		var size int64
		var ext string
		if err = q.QueryRow(`select ext_id,length(cast(coalesce(body_text,'') as blob)) from entries where id=?`, id).Scan(&ext, &size); err != nil {
			return p, err
		}
		if size > limits.Bytes-b.bytes {
			p.Skipped = append(p.Skipped, QuoteMetadataSkip{id, ext, "evidence_limit"})
			continue
		}
		b.bytes += size
		r, err := readQuoteRow(q, id, modern)
		if err != nil {
			return p, err
		}
		c, why, err := quoteProposal(ctx, q, r, modern, b)
		if err != nil {
			return p, err
		}
		if why != "" {
			p.Skipped = append(p.Skipped, QuoteMetadataSkip{id, r.Ext, why})
		} else {
			p.Changes = append(p.Changes, c)
		}
	}
	p.Hosts = len(b.hosts)
	p.Bytes = b.bytes
	p.Digest = planDigest(p)
	return p, nil
}

// ApplyQuotedMetadata revalidates the exact reviewed plan under the existing
// writer lock. A repeated application is a stale refusal, never a second write.
func (s *Store) ApplyQuotedMetadata(ctx context.Context, p QuoteMetadataPlan) (int, error) {
	if err := p.Validate(); err != nil {
		return 0, err
	}
	expected := p.SourceRevision
	n := 0
	_, err := s.guardedReconcileWrite(ctx, &expected, func(w corpusWriter) error {
		fresh, err := planQuotedMetadata(ctx, w, p.Selection, p.Limits)
		if err != nil {
			return err
		}
		if fresh.Digest != p.Digest {
			return ErrReconcileConflict
		}
		for _, c := range p.Changes {
			if _, err = w.Exec(`update entries set ts=?,tz=?,quote_time_basis='stated',quote_time_precision=? where id=?`, c.TS, nullStr(c.TZ), c.Precision, c.ID); err != nil {
				return err
			}
			if err = recordQuoteAlias(w, c); err != nil {
				return err
			}
			n++
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return n, nil
}
func recordQuoteAlias(w corpusWriter, c QuoteMetadataChange) error {
	for _, ev := range c.Evidence {
		_, err := w.Exec(`insert into quote_key_aliases(source,parsed_key,entry_id,host_id,old_ext_id,parser_version,host_digest,sentinel_digest,original_host_id) values('mail',?,?,?,?,?,?,?,?) on conflict do nothing`, c.ParsedKey, c.ID, ev.HostID, c.ExtID, unnest.DateParserVersion, ev.HostDigest, ev.SentinelDigest, ev.HostID)
		if err != nil {
			return err
		}
	}
	return nil
}
