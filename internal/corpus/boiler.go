package corpus

import (
	"fmt"
	"strings"

	"github.com/zachpmanson/chainmail/internal/boiler"
)

// BoilerplateFor finds the appended block at the end of each of these messages,
// and the evidence for it: a sender's signature, an organisation's
// confidentiality notice.
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
// Scoped rather than corpus-wide, which is the one place the argument for
// deriving it needed refining.
//
// The evidence has to be wider than the messages asked about — a signature
// is a fact about the person, an organisation's notice a fact about the domain,
// and a six-entry page and a sixty-entry page must fold the same block — but
// "wider than the selection" is not the same as "the whole corpus", and reading
// every mail body in the corpus to render one chain is what this fixes. On the
// live corpus a single-message chain cost the same 1.8s as an eleven-message one,
// all of it this pass.
//
// Scoping is exact rather than approximate, because a verdict is decided per
// group: boiler.tallies counts a candidate tail under the group it belongs to, so
// a message whose own group is entirely present is given exactly the answer the
// corpus-wide pass gives it, however many other groups are missing. What may
// never be scoped is the evidence *within* a group — half a person's messages, or
// half their domain's, is a different corpus and can only fold less than the one
// outside it. So the scope is drawn around whole groups: every message by a
// person who appears in the ids, and every message from a domain those messages
// were sent from, which is what lets a one-off sender inherit a notice that
// nobody's single message could prove.
//
// It is tested against the corpus-wide pass rather than argued: see
// TestASelectionIsFoldedTheWayTheWholeCorpusFoldsIt.
func (s *Store) BoilerplateFor(ids []int64) (map[int64]boiler.Fold, error) {
	sc, err := s.foldScope(ids)
	if err != nil {
		return nil, err
	}
	msgs, err := s.mailBodies(sc)
	if err != nil {
		return nil, err
	}
	return boiler.Detect(msgs, boiler.Default()), nil
}

// foldScope is the evidence a set of messages needs: the people they are from and
// the domains they were sent from.
//
// all is the whole corpus, which is what a caller with nothing to ask about gets
// — an empty selection has no group to draw a scope around, and narrowing on
// nothing would claim every message is out of scope.
type foldScope struct {
	all     bool
	people  map[int64]bool
	domains map[string]bool
}

// covers reports whether a message belongs to the scope, and so whether its body
// has to be reduced at all. A message outside cannot change a verdict inside.
func (sc foldScope) covers(person int64, domain string) bool {
	if sc.all {
		return true
	}
	return sc.people[person] || (domain != "" && sc.domains[domain])
}

// foldScope draws the scope for these ids around whole groups.
//
// The domains come from the ids themselves rather than from the people: a
// recovered message has no From header of its own and takes the domain of the
// person who wrote it, and a message attributed to nobody is still sent from
// somewhere. Reading the domains off the messages asked about covers all three
// cases, and every domain in scope then brings in every sender at it.
func (s *Store) foldScope(ids []int64) (foldScope, error) {
	if len(ids) == 0 {
		return foldScope{all: true}, nil
	}
	aliases, err := DomainAliases(s)
	if err != nil {
		return foldScope{}, err
	}
	personDomain, err := soleDomains(s, aliases)
	if err != nil {
		return foldScope{}, err
	}
	sc := foldScope{people: map[int64]bool{}, domains: map[string]bool{}}
	ph, args := placeholders(ids)
	rows, err := s.db.Query(`
		select coalesce(e.person_id, 0), coalesce(d.from_addr, '')
		from entries e
		left join mail_detail d on d.entry_id = e.id
		where e.id in (`+ph+`)`, args...)
	if err != nil {
		return foldScope{}, fmt.Errorf("reading the senders in scope: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var person int64
		var from string
		if err := rows.Scan(&person, &from); err != nil {
			return foldScope{}, err
		}
		if person != 0 {
			sc.people[person] = true
		}
		if d := canonicalDomain(from, aliases); d != "" {
			sc.domains[d] = true
		} else if d := personDomain[person]; d != "" {
			sc.domains[d] = true
		}
	}
	return sc, rows.Err()
}

// MailBodies reads every mail body in the corpus, reduced to the lines it shows,
// ready for boiler.Detect. It is the whole-corpus read: `corpus sigs` wants every
// group at once, and a caller rendering a selection wants BoilerplateFor.
//
// The reduction is boiler's own, not a second copy of it: a tail counted here
// and folded in internal/spec has to be measured the same way at both ends. What
// is counted is Match rather than Visible, since the same appended block reaches
// the corpus in as many spellings as there are clients that rendered it.
// Whether the quoted history is peeled follows the entry's provenance, exactly
// as it does at render time — a mailbox message's trail is elsewhere on the page
// and comes off, while a message recovered from inside a quote is already one
// peeled block and peeling it again could only misfire.
//
// Mail only. A Slack post has no signature block — the client puts the author's
// name outside the message — so the pool would gain 27,000 short bodies whose
// repeated two-line tails are people saying the same short thing twice, which is
// not boilerplate and should not be folded as if it were.
func (s *Store) MailBodies() ([]boiler.Message, error) {
	return s.mailBodies(foldScope{all: true})
}

// mailBodies is MailBodies over a scope, and the scope is what makes it cheap:
// the order below is what pays for it. A body's group is decided from its author
// and its From header alone, and a body whose group is not in scope cannot change
// any verdict that is — so it is dropped before the reduction, which is the
// expensive half of the pass.
func (s *Store) mailBodies(sc foldScope) ([]boiler.Message, error) {
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
		// Out of scope, out of the pass: the body is not evidence for any group that
		// is in it, so its lines never have to be reduced.
		if !sc.covers(m.Author, m.Domain) {
			continue
		}
		lines, ok := boiler.Lines(text, direct)
		if !ok {
			continue
		}
		m.Lines = boiler.Match(lines)
		if len(m.Lines) == 0 {
			continue
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
