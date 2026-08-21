package corpus

import (
	"fmt"
	"strings"

	"github.com/zachpmanson/chainmail/internal/boiler"
)

// Boilerplate finds the appended block at the end of every mail body in the
// corpus: each sender's signature, each organisation's confidentiality notice.
//
// Corpus-wide, however small the selection being rendered, for the same reason
// ZoneObservations is: what a person appends to their mail is a fact about them
// and not about the page they happen to appear on. A six-entry page holds far
// too little evidence to see a signature repeat, and evidence restricted to the
// selection would fold a block on one page and leave the identical block in view
// on another.
//
// Derived on each call rather than stored in a column. The detection is a fold
// over every message a person ever sent, so one new mail can lengthen a block or
// push a tail over the threshold; a stored verdict would be correct at ingest and
// quietly wrong afterwards, with nothing to say which rows were computed under
// which evidence. `people.org` is the warning in this schema: declared in
// migration 4, written by nothing, read by nothing. What deriving costs is that
// the answer is invisible to anything that does not ask, which is what
// `corpus sigs` exists to fix.
//
// Mail only. A Slack post has no signature block — the client puts the author's
// name outside the message — so the pool would gain 27,000 short bodies whose
// repeated two-line tails are people saying the same short thing twice, which is
// not boilerplate and should not be folded as if it were.
func (s *Store) Boilerplate() (map[int64]boiler.Fold, error) {
	msgs, err := s.MailBodies()
	if err != nil {
		return nil, err
	}
	return boiler.Detect(msgs, boiler.Default()), nil
}

// MailBodies reads every mail body reduced to the lines it shows, ready for
// boiler.Detect.
//
// The reduction is boiler's own, not a second copy of it: a tail counted here
// and folded in internal/spec has to be measured the same way at both ends.
// Whether the quoted history is peeled follows the entry's provenance, exactly
// as it does at render time — a mailbox message's trail is elsewhere on the page
// and comes off, while a message recovered from inside a quote is already one
// peeled block and peeling it again could only misfire.
func (s *Store) MailBodies() ([]boiler.Message, error) {
	aliases, err := DomainAliases(s)
	if err != nil {
		return nil, err
	}
	personDomain, err := soleDomains(s, aliases)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.Query(`
		select e.id, coalesce(e.person_id, 0), coalesce(d.from_addr, ''), e.body_text,
		       exists(select 1 from sightings g where g.entry_id = e.id and g.kind = 'direct')
		from entries e
		left join mail_detail d on d.entry_id = e.id
		where e.source = 'mail' and e.body_text is not null and e.body_text != ''`)
	if err != nil {
		return nil, fmt.Errorf("reading mail bodies: %w", err)
	}
	defer rows.Close()
	var out []boiler.Message
	for rows.Next() {
		var m boiler.Message
		var from, text string
		var direct bool
		if err := rows.Scan(&m.ID, &m.Author, &from, &text, &direct); err != nil {
			return nil, err
		}
		lines, ok := boiler.Lines(text, direct)
		if !ok {
			continue
		}
		m.Lines = boiler.Visible(lines)
		if len(m.Lines) == 0 {
			continue
		}
		m.Domain = canonicalDomain(from, aliases)
		if m.Domain == "" {
			// An entry recovered from quoted text has no From header of its own —
			// 1,710 of the 3,998 mail entries here — so its domain comes from the
			// person instead. Without this the domain pass sees only the mailbox's
			// own half of the corpus, which is where the notices are least likely to
			// clear the threshold: a one-off sender at some retailer appears only
			// inside somebody else's quote.
			m.Domain = personDomain[m.Author]
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// soleDomains is the one mail domain each person sends from, where there is
// exactly one.
//
// Exactly one, or nothing. A person with a work address and a webmail one has no
// single domain, and picking the more common of the two would attribute their
// employer's legal notice to whichever mailbox they happened to use more — which
// is a claim about who appended the block, and it is the claim the summary makes
// to the reader.
func soleDomains(s *Store, aliases map[string]string) (map[int64]string, error) {
	rows, err := s.db.Query(`select person_id, value from identities where kind = ?`, KindEmail)
	if err != nil {
		return nil, fmt.Errorf("reading addresses: %w", err)
	}
	defer rows.Close()
	seen := map[int64]map[string]bool{}
	for rows.Next() {
		var person int64
		var addr string
		if err := rows.Scan(&person, &addr); err != nil {
			return nil, err
		}
		d := canonicalDomain(addr, aliases)
		if d == "" {
			continue
		}
		if seen[person] == nil {
			seen[person] = map[string]bool{}
		}
		seen[person][d] = true
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make(map[int64]string, len(seen))
	for person, ds := range seen {
		if len(ds) != 1 {
			continue
		}
		for d := range ds {
			out[person] = d
		}
	}
	return out, nil
}

// canonicalDomain is the mail domain of a From header, after any configured
// domain alias.
//
// The alias matters here more than anywhere else: an organisation that rebranded
// appends one notice from two domains, and reading them as two domains halves
// the evidence for it and can drop both halves below the threshold. Which means
// a rebrand's notice is not seen as one until `corpus alias` records it — the
// same limit, and for the same reason, as the dedupe rules in dedupe.go.
func canonicalDomain(from string, aliases map[string]string) string {
	addr := from
	if i := strings.LastIndex(addr, "<"); i >= 0 {
		addr = addr[i+1:]
		addr = strings.TrimSuffix(strings.TrimSpace(addr), ">")
	}
	at := strings.LastIndex(addr, "@")
	if at < 0 {
		return ""
	}
	domain := strings.ToLower(strings.Trim(strings.TrimSpace(addr[at+1:]), ">"))
	if to, ok := aliases[domain]; ok {
		return to
	}
	return domain
}
