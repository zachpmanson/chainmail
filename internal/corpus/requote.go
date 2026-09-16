package corpus

import (
	"fmt"

	"github.com/zachpmanson/chainmail/internal/unnest"
)

// Quotes is one body's recovered messages.
type Quotes struct {
	// Blocks is every sentinel-bearing block found, before copies of one
	// message are collapsed.
	Blocks int
	// Distinct is those blocks after collapsing, in first-appearance order.
	Distinct []unnest.Recovered
}

// RecoverQuotes is the one implementation of "which messages does this body
// contain": every sentinel-bearing block, parsed, with the copies of one message
// that a trail repeats collapsed to the fullest of them.
//
// The ingest stores these as entries and RepairQuotedBodies re-derives them from
// the same host text, so a parser fix heals the rows the old parser wrote only
// if both callers agree on what the parser now says. One function is how they
// agree.
func RecoverQuotes(body string) Quotes {
	var q Quotes
	for _, b := range unnest.Peel(body) {
		if b.Sentinel == "" {
			continue
		}
		q.Blocks++
		q.Distinct = append(q.Distinct, unnest.Parse(b))
	}
	q.Distinct = unnest.Dedup(q.Distinct)
	return q
}

// QuotedRepair is what RepairQuotedBodies did.
type QuotedRepair struct {
	// Hosts peeled, which is the cost of the pass rather than a finding.
	Hosts int
	// Fixed is entries whose stored body was rewritten — the damage a parser fix
	// leaves behind, and the only thing this pass changes.
	Fixed int
	// Missing is quoted entries no block in their host accounts for. Not an
	// error, and never resolved: the host's text may have been truncated, and an
	// entry deleted on that evidence would lose a message nothing else holds.
	Missing []string
}

// RepairQuotedBodies re-derives the entries recovered from quoted text out of
// the host bodies they came from, and rewrites the ones that disagree.
//
// This is the same division as RepairMailtoIdentities and RepairPlusAddresses: a
// parse fix changes what extraction says, and cannot change what extraction has
// already written. PutQuoted is insert-or-leave-alone on purpose — a mailbox
// copy must not be overwritten by a rewrapped quote of it — so a corpus ingested
// before the fix keeps the old parser's text until something re-derives it.
//
// The damage is not only cosmetic. An entry recovered out of a header block
// whose last recipient line wrapped used to open with the tail of that line
// (zpm/chainmail#146), and the twin pass decides whether two entries are one
// message partly on how they open — so the duplicate it exists to collapse was
// left standing, holding a clock twelve hours off its own mailbox copy.
//
// Only the body is rewritten. The clock, the sender and the subject come from
// the sentinel, and a parser fix that moved one of those would be a different
// repair with a different argument; nothing here guesses at an entry's identity,
// so one its host no longer accounts for is reported and left alone.
func RepairQuotedBodies(s *Store) (QuotedRepair, error) {
	var rep QuotedRepair
	rows, err := s.db.Query(`
		select g.seen_in, e.id, e.ext_id, coalesce(e.subject,''),
		       coalesce(e.body_text,''), coalesce(h.body_text,'')
		from sightings g
		join entries e on e.id = g.entry_id
		join entries h on h.id = g.seen_in
		where e.quoted = 1 and e.source = ? and h.body_text is not null
		order by g.seen_in, e.id`, SourceMail)
	if err != nil {
		return rep, fmt.Errorf("reading the quoted entries and their hosts: %w", err)
	}
	defer rows.Close()

	var cands []quotedCandidate
	for rows.Next() {
		var c quotedCandidate
		if err := rows.Scan(&c.host, &c.id, &c.ext, &c.subject, &c.body, &c.hostBody); err != nil {
			return rep, err
		}
		cands = append(cands, c)
	}
	if err := rows.Err(); err != nil {
		return rep, err
	}

	// One pass is provably enough: a host is always a mailbox entry, because
	// extraction runs over mailbox bodies and records the host it found each block
	// inside (mailingest.ExtractQuoted). A quoted entry is never a host, so no
	// candidate's source text changes while the pass is walking them.
	fixed, hosts, err := repeelPass(s, cands)
	if err != nil {
		return rep, err
	}
	rep.Hosts, rep.Fixed = hosts, fixed
	for _, c := range cands {
		if c.missing {
			rep.Missing = append(rep.Missing, c.ext)
		}
	}
	return rep, nil
}

// quotedCandidate is one stored quoted entry and the host it was found in.
type quotedCandidate struct {
	id       int64
	ext      string
	subject  string
	body     string
	host     int64
	hostBody string
	// missing is set by a pass when the host no longer accounts for the entry.
	missing bool
}

// repeelPass rewrites every candidate whose host body now says something else,
// peeling each host once.
func repeelPass(s *Store, cands []quotedCandidate) (fixed, hosts int, err error) {
	byHost := map[int64]map[string]string{}
	for _, c := range cands {
		if _, ok := byHost[c.host]; !ok {
			texts := map[string]string{}
			for _, r := range RecoverQuotes(c.hostBody).Distinct {
				texts[r.Key] = r.Block.Text
			}
			byHost[c.host] = texts
			hosts++
		}
	}
	for i := range cands {
		c := &cands[i]
		text, ok := byHost[c.host][c.ext]
		if !ok {
			c.missing = true
			continue
		}
		c.missing = false
		if text == c.body {
			continue
		}
		if err := rewriteQuotedBody(s, c.id, c.subject, c.body, text); err != nil {
			return fixed, hosts, err
		}
		fixed++
	}
	return fixed, hosts, nil
}

// rewriteQuotedBody stores a re-derived body with its hash — which is what tells
// the embedder the vector is stale — and its search entries.
func rewriteQuotedBody(s *Store, id int64, subject, oldBody, body string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`update entries set body_text=?, body_sha=? where id=?`,
		body, BodySHA(subject, body), id); err != nil {
		return fmt.Errorf("rewriting the body of entry %d: %w", id, err)
	}
	if err := s.reindex(tx, id, false, subject, oldBody); err != nil {
		return err
	}
	return tx.Commit()
}
