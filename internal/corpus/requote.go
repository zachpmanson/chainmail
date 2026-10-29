package corpus

import (
	"database/sql"
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
	// Missing is quoted entries no eligible host accounts for. Not an
	// error, and never resolved: the host's text may have been truncated, and an
	// entry deleted on that evidence would lose a message nothing else holds.
	Missing []string
	// Ambiguous is entries whose hosts recover different texts for the same
	// identity. Preserve the stored body rather than guess which copy is true.
	Ambiguous []string
}

// RepairQuotedBodies re-derives the entries recovered from quoted text out of
// the host bodies they came from. It repairs each entry once only when all
// recoverable hosts agree on the text; differing variants are reported and left
// unchanged, not selected by length, popularity or arrival order.
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

	// A quote can occur in several hosts. Resolve their evidence together before
	// writing: per-sighting comparisons against the loaded body can oscillate
	// between versions and retract the same old FTS tokens more than once.
	return repeelPass(s, cands)
}

// quotedCandidate is one stored quoted entry and the host it was found in.
type quotedCandidate struct {
	id       int64
	ext      string
	subject  string
	body     string
	host     int64
	hostBody string
}

// repeelPass peels each host once and proposes at most one write per entry.
// Missing hosts abstain. Divergent texts are not votes: forwards can truncate or
// annotate a message, so neither length, majority nor host order proves truth.
func repeelPass(s *Store, cands []quotedCandidate) (QuotedRepair, error) {
	var rep QuotedRepair
	byHost := map[int64]map[string]string{}
	for _, c := range cands {
		if _, ok := byHost[c.host]; !ok {
			texts := map[string]string{}
			for _, r := range RecoverQuotes(c.hostBody).Distinct {
				texts[r.Key] = r.Block.Text
			}
			byHost[c.host] = texts
			rep.Hosts++
		}
	}
	type evidence struct {
		candidate quotedCandidate
		texts     map[string]struct{}
	}
	byEntry := map[int64]*evidence{}
	var order []int64
	for _, c := range cands {
		e := byEntry[c.id]
		if e == nil {
			e = &evidence{candidate: c, texts: map[string]struct{}{}}
			byEntry[c.id] = e
			order = append(order, c.id)
		}
		if text, ok := byHost[c.host][c.ext]; ok {
			e.texts[text] = struct{}{}
		}
	}
	for _, id := range order {
		e := byEntry[id]
		c := e.candidate
		switch len(e.texts) {
		case 0:
			rep.Missing = append(rep.Missing, c.ext)
			continue
		case 1:
			// All hosts that account for this identity agree exactly.
		default:
			rep.Ambiguous = append(rep.Ambiguous, c.ext)
			continue
		}
		for text := range e.texts {
			if text == c.body {
				continue
			}
			changed, err := rewriteQuotedBody(s, c.id, c.subject, c.body, text)
			if err != nil {
				return rep, err
			}
			if changed {
				rep.Fixed++
			}
		}
	}
	return rep, nil
}

// rewriteQuotedBody serializes a conditional source update and both FTS writes.
// Read the actual indexed values under the writer transaction, never retract
// with a stale sighting snapshot. An intervening source change is retried by a
// later repair; a mailbox copy is never rewritten. Hash-only changes are not a
// repair: preserve existing hashes/vectors when the body already agrees.
func rewriteQuotedBody(s *Store, id int64, subject, oldBody, body string) (bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var quoted int
	var source, actualSubject, actualBody string
	if err := tx.QueryRow(`select source, quoted, coalesce(subject,''), coalesce(body_text,'') from entries where id=?`,
		id).Scan(&source, &quoted, &actualSubject, &actualBody); err != nil {
		if err == sql.ErrNoRows {
			return false, nil
		}
		return false, err
	}
	if source != SourceMail || quoted == 0 || actualBody == body || actualSubject != subject || actualBody != oldBody {
		return false, nil
	}
	if _, err := tx.Exec(`update entries set body_text=?, body_sha=? where id=?`,
		body, BodySHA(body, actualSubject), id); err != nil {
		return false, fmt.Errorf("rewriting the body of entry %d: %w", id, err)
	}
	if err := s.reindex(tx, id, false, actualSubject, actualBody); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}
