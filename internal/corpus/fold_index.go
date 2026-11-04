package corpus

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/zachpmanson/chainmail/internal/boiler"
)

const foldVersion = 1

// FoldWarm reports durable maintenance work, not the size of the corpus.
type FoldWarm struct {
	Bodies      int // bodies normalized, including negative reductions
	Lines       int // visible lines normalized
	Memberships int
	Tails       int
	CounterKeys int
	Pending     int // unique entries considered from the consumed durable work
	Generation  int64
	Rebuilt     bool
	LockWait    time.Duration
	Took        time.Duration
}

type foldQuery interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type foldState struct {
	aliasDirty                                        bool
	ready, exact, direct, normal, fingerprint, window int
	generation                                        int64
	dirty                                             bool
}

func readFoldState(ctx context.Context, q foldQuery, window int) (foldState, error) {
	var st foldState
	var pending bool
	err := q.QueryRowContext(ctx, `select ready,exact_version,direct_version,normalize_version,
 fingerprint_version,window,generation,aliases_dirty,
 exists(select 1 from fold_dirty_entries) or exists(select 1 from fold_dirty_people)
 from fold_state where id=1`).Scan(&st.ready, &st.exact, &st.direct, &st.normal, &st.fingerprint, &st.window, &st.generation, &st.aliasDirty, &pending)
	if err != nil {
		return st, err
	}
	if st.exact > foldVersion || st.direct > foldVersion || st.normal > foldVersion || st.fingerprint > foldVersion {
		return st, errors.New("fold index requires a newer binary")
	}
	st.dirty = st.aliasDirty || pending || st.rebuild(window)
	return st, nil
}
func (st foldState) rebuild(window int) bool {
	return st.ready == 0 || st.exact != foldVersion || st.direct != foldVersion || st.normal != foldVersion || st.fingerprint != foldVersion || st.window != window
}

// WarmFolds lazily initializes or refreshes the durable index. Open does no backfill.
func (s *Store) WarmFolds() (FoldWarm, error) {
	return s.RefreshFoldIndex(context.Background(), boiler.Default())
}

// RefreshFoldIndex publishes all pending deltas atomically. Threshold changes do
// not rebuild; window and reduction/fingerprint versions are exact parameters.
func (s *Store) RefreshFoldIndex(ctx context.Context, r boiler.Rules) (stats FoldWarm, err error) {
	r = boiler.FilledRules(r)
	started := time.Now()
	defer func() { stats.Took = time.Since(started) }()
	initial, err := readFoldState(ctx, s.db, r.Window)
	if err != nil {
		return stats, err
	}
	stats.Generation = initial.generation
	if !initial.dirty {
		return stats, nil
	}
	// Bound lock acquisition independently of the amount of normalization work.
	lockStart := time.Now()
	writer, err := s.beginWriter(ctx, 5*time.Second)
	stats.LockWait = time.Since(lockStart)
	if err != nil {
		return stats, err
	}
	defer func() { err = errors.Join(err, writer.close()) }()
	conn := writer.Conn
	st, err := readFoldState(ctx, conn, r.Window)
	if err != nil {
		return stats, err
	}
	stats.Generation = st.generation
	if !st.dirty {
		return stats, nil
	}
	aliases, err := foldAliases(ctx, conn)
	if err != nil {
		return stats, err
	}
	if st.rebuild(r.Window) {
		stats.Rebuilt = true
		err = rebuildFoldIndex(ctx, conn, r, aliases, &stats)
	} else {
		err = refreshFoldDeltas(ctx, conn, r, aliases, st.aliasDirty, &stats)
	}
	if err != nil {
		return stats, err
	}
	_, err = conn.ExecContext(ctx, `delete from fold_dirty_entries; delete from fold_dirty_people;
 update fold_state set ready=1,exact_version=?,direct_version=?,normalize_version=?,fingerprint_version=?,window=?,generation=generation+1,aliases_dirty=0 where id=1`, foldVersion, foldVersion, foldVersion, foldVersion, r.Window)
	if err != nil {
		return stats, err
	}
	if err = writer.commit(ctx); err != nil {
		return stats, err
	}
	stats.Generation++
	return stats, nil
}

// Ordinary refreshes retain per-member subtract/add deltas, not group recounts.
func refreshFoldDeltas(ctx context.Context, conn *sql.Conn, r boiler.Rules, aliases map[string]string, aliasDirty bool, stats *FoldWarm) error {
	// Ordinary person changes inspect only fallback memberships. A rare alias
	// change inspects all published metadata, never all raw bodies.
	query := foldWorkQuery(aliasDirty)
	rows, err := conn.QueryContext(ctx, query)
	if err != nil {
		return err
	}
	work := map[int64]bool{}
	for rows.Next() {
		var id int64
		var source bool
		if err = rows.Scan(&id, &source); err != nil {
			break
		}
		work[id] = work[id] || source
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return err
	}
	stats.Pending = len(work)
	domains := map[int64]string{}
	for id, source := range work {
		if err = ctx.Err(); err != nil {
			return err
		}
		old, e := loadFoldMember(ctx, conn, id)
		if e != nil {
			return e
		}
		var next *foldMember
		if source {
			var text, from string
			var person int64
			var direct bool
			e = conn.QueryRowContext(ctx, foldSourceQuery, id).Scan(&person, &from, &text, &direct)
			if e != nil && !errors.Is(e, sql.ErrNoRows) {
				return e
			}
			if e == nil {
				digest := sha256.Sum256([]byte(text))
				next = &foldMember{id: id, person: person, raw: canonicalDomain(from, nil), digest: digest[:], direct: direct}
				if old != nil && bytes.Equal(old.digest, next.digest) && old.direct == direct {
					next.ok = old.ok
					next.lines = old.lines
					next.tails = old.tails
				} else {
					stats.Bodies++
					if raw, ok := boiler.Lines(text, direct); ok {
						lines := boiler.Match(raw)
						next.ok = true
						next.lines = len(lines)
						stats.Lines += len(lines)
						for i, t := range boiler.Tails(lines, r.Window) {
							if t.Valid {
								next.tails = append(next.tails, foldTail{n: i + 1, fp: append([]byte(nil), t.Digest[:]...)})
							}
						}
					}
				}
			}
		} else if old != nil {
			cp := *old
			next = &cp
		}
		if next != nil {
			next.domain = next.raw
			if to, ok := aliases[next.domain]; ok {
				next.domain = to
			}
			if next.domain == "" {
				d, ok := domains[next.person]
				if !ok {
					d, e = foldSoleDomain(ctx, conn, next.person, aliases)
					if e != nil {
						return e
					}
					domains[next.person] = d
				}
				next.domain = d
			}
		}
		if sameFoldMember(old, next) {
			continue
		}
		if e = publishFoldMember(ctx, conn, old, next, stats); e != nil {
			return fmt.Errorf("publishing fold contribution: %w", e)
		}
	}
	return nil
}

func foldWorkQuery(aliasDirty bool) string {
	query := `select entry_id,1 from fold_dirty_entries`
	if aliasDirty {
		query += ` union all select entry_id,0 from fold_members`
	} else {
		// Keep the dirty queue outermost, even when most entries lack From headers.
		query += ` union all select m.entry_id,0 from fold_dirty_people p
 cross join fold_members m indexed by fold_members_person
 where m.person_id=p.person_id and m.raw_domain=''`
	}
	return query
}

func foldBusy(err error) bool {
	var code interface{ Code() int }
	return errors.As(err, &code) && (code.Code()&255 == 5 || code.Code()&255 == 6)
}
func foldAliases(ctx context.Context, q foldQuery) (map[string]string, error) {
	rows, err := q.QueryContext(ctx, `select from_domain,to_domain from domain_aliases`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var a, b string
		if err = rows.Scan(&a, &b); err != nil {
			return nil, err
		}
		out[a] = b
	}
	return out, rows.Err()
}
func foldSoleDomain(ctx context.Context, q foldQuery, person int64, aliases map[string]string) (string, error) {
	rows, err := q.QueryContext(ctx, `select value from identities where person_id=? and kind='email'`, person)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	seen := map[string]bool{}
	for rows.Next() {
		var addr string
		if err = rows.Scan(&addr); err != nil {
			return "", err
		}
		if d := canonicalDomain(addr, aliases); d != "" {
			seen[d] = true
		}
	}
	if err = rows.Err(); err != nil {
		return "", err
	}
	if len(seen) == 1 {
		for d := range seen {
			return d, nil
		}
	}
	return "", nil
}

type foldTail struct {
	n  int
	fp []byte
}
type foldMember struct {
	id, person  int64
	digest      []byte
	direct, ok  bool
	raw, domain string
	lines       int
	tails       []foldTail
}

func loadFoldMember(ctx context.Context, q foldQuery, id int64) (*foldMember, error) {
	m := &foldMember{id: id}
	err := q.QueryRowContext(ctx, `select exact_digest,direct,person_id,raw_domain,domain,ok,line_count from fold_members where entry_id=?`, id).Scan(&m.digest, &m.direct, &m.person, &m.raw, &m.domain, &m.ok, &m.lines)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	rows, err := q.QueryContext(ctx, `select n,fp from fold_tails where entry_id=? order by n`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var t foldTail
		if err = rows.Scan(&t.n, &t.fp); err != nil {
			return nil, err
		}
		m.tails = append(m.tails, t)
	}
	return m, rows.Err()
}
func sameFoldMember(a, b *foldMember) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.person == b.person && a.direct == b.direct && a.ok == b.ok && a.raw == b.raw && a.domain == b.domain && a.lines == b.lines && bytes.Equal(a.digest, b.digest)
}

type foldCountKey struct {
	kind      int
	group, fp string
}
type foldRefKey struct {
	domain, fp string
	person     int64
}

func publishFoldMember(ctx context.Context, c *sql.Conn, old, next *foldMember, stats *FoldWarm) error {
	counts := map[foldCountKey]int{}
	refs := map[foldRefKey]int{}
	add := func(m *foldMember, sign int) {
		if m == nil || !m.ok {
			return
		}
		for _, t := range m.tails {
			if m.person != 0 {
				counts[foldCountKey{1, strconv.FormatInt(m.person, 10), string(t.fp)}] += sign
			}
			if m.domain != "" {
				counts[foldCountKey{2, m.domain, string(t.fp)}] += sign
				refs[foldRefKey{m.domain, string(t.fp), m.person}] += sign
			}
		}
	}
	add(old, -1)
	add(next, 1)
	senderDelta := map[foldCountKey]int{}
	for k, delta := range refs {
		if delta == 0 {
			continue
		}
		var was int
		err := c.QueryRowContext(ctx, `select refs from fold_sender_refs where domain=? and fp=? and person_id=?`, k.domain, []byte(k.fp), k.person).Scan(&was)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		now := was + delta
		if now < 0 {
			return errors.New("fold sender ref underflow")
		}
		ck := foldCountKey{2, k.domain, k.fp}
		if was == 0 && now > 0 {
			senderDelta[ck]++
		}
		if was > 0 && now == 0 {
			senderDelta[ck]--
		}
		if now == 0 {
			_, err = c.ExecContext(ctx, `delete from fold_sender_refs where domain=? and fp=? and person_id=?`, k.domain, []byte(k.fp), k.person)
		} else {
			_, err = c.ExecContext(ctx, `insert into fold_sender_refs values(?,?,?,?) on conflict(domain,fp,person_id) do update set refs=excluded.refs`, k.domain, []byte(k.fp), k.person, now)
		}
		if err != nil {
			return err
		}
	}
	for k, delta := range counts {
		sd := senderDelta[k]
		if delta == 0 && sd == 0 {
			continue
		}
		var was, senders int
		err := c.QueryRowContext(ctx, `select count,senders from fold_counts where kind=? and group_key=? and fp=?`, k.kind, k.group, []byte(k.fp)).Scan(&was, &senders)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		now := was + delta
		senders += sd
		if k.kind == 1 {
			senders = 0
			if now > 0 {
				senders = 1
			}
		}
		if now < 0 || senders < 0 || (now == 0) != (senders == 0) || senders > now {
			return errors.New("fold counter/ref inconsistency")
		}
		stats.CounterKeys++
		if now == 0 {
			_, err = c.ExecContext(ctx, `delete from fold_counts where kind=? and group_key=? and fp=?`, k.kind, k.group, []byte(k.fp))
		} else {
			_, err = c.ExecContext(ctx, `insert into fold_counts values(?,?,?,?,?) on conflict(kind,group_key,fp) do update set count=excluded.count,senders=excluded.senders`, k.kind, k.group, []byte(k.fp), now, senders)
		}
		if err != nil {
			return err
		}
	}
	id := int64(0)
	if old != nil {
		id = old.id
	}
	if next != nil {
		id = next.id
	}
	if _, err := c.ExecContext(ctx, `delete from fold_members where entry_id=?`, id); err != nil {
		return err
	}
	stats.Memberships++
	if next == nil {
		return nil
	}
	m := next
	if _, err := c.ExecContext(ctx, `insert into fold_members values(?,?,?,?,?,?,?,?)`, m.id, m.digest, m.direct, m.person, m.raw, m.domain, m.ok, m.lines); err != nil {
		return err
	}
	for _, t := range m.tails {
		if _, err := c.ExecContext(ctx, `insert into fold_tails values(?,?,?)`, id, t.n, t.fp); err != nil {
			return err
		}
		stats.Tails++
	}
	return nil
}

const foldSourceQuery = `select coalesce(e.person_id,0),coalesce(d.from_addr,''),e.body_text,
   exists(select 1 from sightings g where g.entry_id=e.id and g.kind='direct')
   from entries e left join mail_detail d on d.entry_id=e.id
   where e.id=? and e.source='mail' and e.body_text is not null and e.body_text!=''`
