package corpus

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/zachpmanson/chainmail/internal/unnest"
)

func aliasTarget(q corpusQuery, key string) (int64, error) {
	var id int64
	err := q.QueryRow(`select coalesce(min(entry_id),0) from quote_key_aliases where source='mail' and parsed_key=?`, key).Scan(&id)
	return id, err
}

// An alias is evidence, not a permanent redirect. Validate the complete host set
// and the exact current sentinels every time; edited dates must not retarget it.
func validatedQuoteAlias(ctx context.Context, q corpusQuery, key string, b *quoteBudget) (int64, error) {
	rows, err := q.Query(`select entry_id,host_id,old_ext_id,parser_version,host_digest,sentinel_digest from quote_key_aliases where source='mail' and parsed_key=? order by host_id limit ?`, key, b.limits.Hosts+1)
	if err != nil {
		return 0, err
	}
	type fact struct {
		id      int64
		old     string
		version int
		ev      QuoteEvidence
	}
	var facts []fact
	for rows.Next() {
		var f fact
		if err = rows.Scan(&f.id, &f.ev.HostID, &f.old, &f.version, &f.ev.HostDigest, &f.ev.SentinelDigest); err != nil {
			rows.Close()
			return 0, err
		}
		facts = append(facts, f)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	if len(facts) == 0 {
		return 0, nil
	}
	if len(facts) > b.limits.Hosts {
		return 0, errQuoteLimit
	}
	id := facts[0].id
	hosts, _, err := hostSightings(q, id, b.limits.Hosts)
	if err != nil {
		return 0, err
	}
	if len(hosts) != len(facts) {
		return 0, errors.New("stale quote alias host set")
	}
	var person int64
	var collisions int
	if err = q.QueryRow(`select coalesce(person_id,0) from entries where id=?`, id).Scan(&person); err != nil {
		return 0, err
	}
	if err = q.QueryRow(`select count(*) from entries where source='mail' and ext_id=? and id<>?`, key, id).Scan(&collisions); err != nil {
		return 0, err
	}
	if collisions != 0 {
		return 0, errors.New("quote alias key collision")
	}
	for i, f := range facts {
		if f.id != id || f.version != unnest.DateParserVersion || hosts[i] != f.ev.HostID {
			return 0, errors.New("conflicting quote alias")
		}
		h, err := b.host(ctx, q, f.ev.HostID)
		if err != nil {
			return 0, err
		}
		if h.evidence.HostDigest != f.ev.HostDigest {
			return 0, errors.New("stale quote alias source")
		}
		count := 0
		for _, rec := range h.quotes {
			if rec.Key == key {
				if quoteDigest(rec.Block.Sentinel) != f.ev.SentinelDigest || !recoveredAuthor(q, rec, person) {
					return 0, errors.New("stale quote alias sentinel/author")
				}
				count++
			}
		}
		if count != 1 {
			return 0, errors.New("ambiguous quote alias")
		}
	}
	return id, nil
}

// ResolveQuotedKey runs before clock matching. A proven legacy mapping may add
// alias evidence, but it never changes the stored fallback time or its basis.
// A detected but unsafe bridge is an error, not permission to insert a duplicate.
func (s *Store) ResolveQuotedKey(ctx context.Context, hostID int64, rec unnest.Recovered) (int64, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	state, err := readReconcileState(tx)
	if err != nil {
		return 0, err
	}
	b := &quoteBudget{limits: DefaultQuoteLimits, hosts: map[int64]quoteHost{}}
	id, err := validatedQuoteAlias(ctx, tx, rec.Key, b)
	if err != nil {
		return 0, err
	}
	if id != 0 {
		var n int
		if err = tx.QueryRow(`select count(*) from quote_key_aliases where source='mail' and parsed_key=? and host_id=?`, rec.Key, hostID).Scan(&n); err != nil {
			return 0, err
		}
		if n != 1 {
			return 0, errors.New("quote alias lacks same-host evidence")
		}
		return id, nil
	}
	// Only a newly dated key can bridge an old undated key. Exact legacy-key
	// re-ingest remains insert-or-leave-alone and retains the inference marker.
	if rec.Key != unnest.TextKey(rec.Block.Text) && rec.Block.Text != "" {
		var legacy int64
		err = tx.QueryRow(`select id from entries where source='mail' and ext_id=?`, unnest.TextKey(rec.Block.Text)).Scan(&legacy)
		if err != nil && err != sql.ErrNoRows {
			return 0, err
		}
		if legacy != 0 {
			r, err := readQuoteRow(tx, legacy, true)
			if err != nil {
				return 0, err
			}
			c, why, err := quoteProposal(ctx, tx, r, true, b)
			if err != nil {
				return 0, err
			}
			if why != "" {
				return 0, fmt.Errorf("quote key bridge refused: %s", why)
			}
			sameHost := false
			for _, ev := range c.Evidence {
				sameHost = sameHost || ev.HostID == hostID
			}
			if !sameHost || c.ParsedKey != rec.Key || c.TS != rec.Sent.Unix() || c.TZ != rec.TZ || !recoveredAuthor(tx, rec, r.Person) {
				return 0, errors.New("quote key bridge lacks matching same-host evidence")
			}
			tx.Rollback()
			expected := state.Source
			_, err = s.guardedReconcileWrite(ctx, &expected, func(w corpusWriter) error { return recordQuoteAlias(w, c) })
			return legacy, err
		}
	}
	err = tx.QueryRow(`select id from entries where source='mail' and ext_id=?`, rec.Key).Scan(&id)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return id, err
}

// quotedClockEligibility retains legacy clocks only with original address/date
// key proof or complete matching Message-ID source evidence. Not text keys.
func quotedClockEligibility(q corpusQuery, person int64) (map[int64]bool, error) {
	return quotedClockEligibilityFor(q, person, "")
}

func quotedClockEligibilityFor(q corpusQuery, person int64, filter string, args ...any) (map[int64]bool, error) {
	params := append([]any{person}, args...)
	rows, err := q.Query(`select e.id,e.ext_id,e.ts,coalesce(e.tz,''),coalesce(e.quote_time_basis,''),
 exists(select 1 from sightings where entry_id=e.id and detail like '%time inferred from host%')
 from entries e where e.source='mail' and e.quoted=1 and e.person_id=?`+filter, params...)
	if err != nil {
		return nil, err
	}
	type clock struct {
		id       int64
		key      string
		ts       int64
		basis    string
		tz       string
		inferred bool
	}
	var cs []clock
	for rows.Next() {
		var c clock
		if err = rows.Scan(&c.id, &c.key, &c.ts, &c.tz, &c.basis, &c.inferred); err != nil {
			rows.Close()
			return nil, err
		}
		cs = append(cs, c)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	rows, err = q.Query(`select value from identities where person_id=? and kind='email'`, person)
	if err != nil {
		return nil, err
	}
	var addresses []string
	for rows.Next() {
		var a string
		if err = rows.Scan(&a); err != nil {
			rows.Close()
			return nil, err
		}
		addresses = append(addresses, a)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	out := map[int64]bool{}
	budget := &quoteBudget{limits: DefaultQuoteLimits, hosts: map[int64]quoteHost{}}
	for _, c := range cs {
		if c.basis == "stated" {
			out[c.id] = true
			continue
		}
		if c.basis == "inferred" || c.inferred {
			continue
		}
		for _, a := range addresses {
			if c.key == unnest.Key(unnest.Recovered{Address: a, Sent: time.Unix(c.ts, 0).UTC()}) {
				out[c.id] = true
				break
			}
		}
		if !out[c.id] && strings.HasPrefix(c.key, "mail:") {
			ok, err := legacyClockFromHosts(q, c.id, c.key, c.ts, c.tz, person, budget)
			if err != nil {
				return nil, err
			}
			out[c.id] = ok
		}
	}
	return out, nil
}
func quotedClockEligible(q corpusQuery, id int64) (bool, error) {
	var person int64
	var quoted bool
	if err := q.QueryRow(`select quoted,coalesce(person_id,0) from entries where id=?`, id).Scan(&quoted, &person); err != nil {
		return false, err
	}
	if !quoted {
		return true, nil
	}
	m, err := quotedClockEligibilityFor(q, person, " and e.id=?", id)
	return m[id], err
}

// Message-ID keys alone prove identity but not a clock. Legacy mail: quotes
// additionally need complete, agreeing current source evidence for the stored
// value. Never use this route for unverified legacy text keys.
func legacyClockFromHosts(q corpusQuery, id int64, key string, ts int64, tz string, person int64, b *quoteBudget) (bool, error) {
	hosts, inferred, err := hostSightings(q, id, b.limits.Hosts)
	if err != nil || inferred || len(hosts) == 0 {
		return false, nil
	}
	for _, id := range hosts {
		h, err := b.host(context.Background(), q, id)
		if err != nil {
			if errors.Is(err, errQuoteLimit) || errors.Is(err, sql.ErrNoRows) {
				return false, nil
			}
			return false, err
		}
		count := 0
		for _, rec := range h.quotes {
			if rec.Key == key {
				if rec.Sent.IsZero() || rec.Sent.Unix() != ts || rec.TZ != tz || !recoveredAuthor(q, rec, person) {
					return false, nil
				}
				count++
			}
		}
		if count != 1 {
			return false, nil
		}
	}
	return true, nil
}

func moveQuoteAliasHosts(w corpusWriter, keep, drop int64) error {
	rows, err := w.Query(`select source,parsed_key,entry_id,old_ext_id,parser_version,host_digest,sentinel_digest,original_host_id from quote_key_aliases where host_id=?`, drop)
	if err != nil {
		return err
	}
	type aliasFact struct {
		source, key                string
		entry                      int64
		old                        string
		version                    int
		hostDigest, sentinelDigest string
		original                   int64
	}
	var facts []aliasFact
	for rows.Next() {
		var f aliasFact
		if err = rows.Scan(&f.source, &f.key, &f.entry, &f.old, &f.version, &f.hostDigest, &f.sentinelDigest, &f.original); err != nil {
			rows.Close()
			return err
		}
		facts = append(facts, f)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, f := range facts {
		other := aliasFact{source: f.source, key: f.key}
		err = w.QueryRow(`select entry_id,old_ext_id,parser_version,host_digest,sentinel_digest,original_host_id from quote_key_aliases where source=? and parsed_key=? and host_id=?`, f.source, f.key, keep).Scan(&other.entry, &other.old, &other.version, &other.hostDigest, &other.sentinelDigest, &other.original)
		switch {
		case err == sql.ErrNoRows:
			_, err = w.Exec(`update quote_key_aliases set host_id=? where source=? and parsed_key=? and host_id=?`, keep, f.source, f.key, drop)
		case err != nil:
			return err
		case other == f:
			_, err = w.Exec(`delete from quote_key_aliases where source=? and parsed_key=? and host_id=?`, f.source, f.key, drop)
		default:
			return fmt.Errorf("conflicting quote alias host evidence: %w", ErrReconcileConflict)
		}
		if err != nil {
			return err
		}
	}
	return nil
}
