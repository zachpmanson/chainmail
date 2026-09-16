package corpus

import (
	"fmt"
	"sort"
	"strings"
)

// The organisation a mail domain belongs to is a judgement about the reader's
// mail, not a fact in it, so the corpus stores the reader's answer beside the
// mail rather than deriving one on every read.
//
// Two shapes it has to be able to say, and they are different claims:
//
//   - "termina.io and threadlet.com.au are one organisation, called Termina" —
//     two rows with the same org. Grouping *is* the shared name; there is no
//     separate group entity to keep in step with it.
//   - "bigpond.com is not an organisation" — a row whose org is empty. Which is
//     not the same as no row at all: no row means the corpus guesses from the
//     domain, and a guess is only wrong until someone says so.
type OrgRule struct {
	Domain string
	// Org is the organisation label, or empty for "this domain is not one".
	Org string
}

// OrgRules reads every stored rule, as the map the page's org resolver takes.
// An empty label is present in the map on purpose: it is a decision, and a
// caller that dropped it could not tell it from having no opinion.
func (s *Store) OrgRules() (map[string]string, error) {
	rows, err := s.db.Query(`select domain, org from org_domains`)
	if err != nil {
		return nil, fmt.Errorf("reading the org rules: %w", err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var domain, org string
		if err := rows.Scan(&domain, &org); err != nil {
			return nil, err
		}
		out[domain] = org
	}
	return out, rows.Err()
}

// PutOrgRule records what a domain is: an organisation by that name, or — with
// an empty org — not an organisation at all.
//
// The domain is lowercased and trimmed, because a domain is case-insensitive
// and "Termina.IO" and "termina.io" being two rows is the kind of pair that
// exists only to get out of step. Nothing else is validated: a domain the
// corpus has no mail from is a rule about mail that has not arrived yet, which
// is a thing an operator may know before the corpus does.
func (s *Store) PutOrgRule(domain, org string) error {
	domain = strings.ToLower(strings.TrimSpace(domain))
	if domain == "" {
		return fmt.Errorf("an org rule needs a domain")
	}
	org = strings.TrimSpace(org)
	_, err := s.db.Exec(`
		insert into org_domains (domain, org) values (?, ?)
		on conflict(domain) do update set org = excluded.org`, domain, org)
	if err != nil {
		return fmt.Errorf("recording the org for %s: %w", domain, err)
	}
	return nil
}

// ClearOrgRule drops a rule, which puts the domain back to being guessed from
// its own name. Distinct from storing an empty label, which says the guess is
// wrong: one is "decide nothing here", the other is "there is nothing to
// decide".
func (s *Store) ClearOrgRule(domain string) error {
	domain = strings.ToLower(strings.TrimSpace(domain))
	if _, err := s.db.Exec(`delete from org_domains where domain = ?`, domain); err != nil {
		return fmt.Errorf("clearing the org for %s: %w", domain, err)
	}
	return nil
}

// SenderDomain is one mail domain the corpus holds mail from, and what it is
// currently understood to be. The operator's view of the colour rules: every
// domain, how much mail depends on it, and what would change.
type SenderDomain struct {
	Domain string
	// Messages counts the entries whose own From header names this domain —
	// which is exactly the set a rule about it repaints. A recovered entry has no
	// header, so no domain, so it is never counted here; it takes its sender's
	// colour from the person instead, and moves when they do.
	Messages int
	// People counts the distinct senders behind those entries.
	People int
	// Org is the stored rule, empty both for "not an organisation" and for no
	// rule at all — see Stored.
	Org string
	// Stored says whether a rule exists, so the two empty answers can be told
	// apart.
	Stored bool
}

// SenderDomains lists every domain mail has been sent from, busiest first.
//
// It counts from `mail_detail`, so the numbers are about mail: a Slack author
// and a recovered entry belong to no domain and are absent, which is the truth
// about them rather than a zero.
func (s *Store) SenderDomains() ([]SenderDomain, error) {
	rows, err := s.db.Query(`
		select coalesce(d.from_addr, ''), coalesce(e.person_id, 0)
		from mail_detail d join entries e on e.id = d.entry_id`)
	if err != nil {
		return nil, fmt.Errorf("reading the sender domains: %w", err)
	}
	defer rows.Close()

	type tally struct {
		messages int
		people   map[int64]bool
	}
	byDomain := map[string]*tally{}
	order := []string{}
	for rows.Next() {
		var from string
		var person int64
		if err := rows.Scan(&from, &person); err != nil {
			return nil, err
		}
		domain := MailDomain(from)
		if domain == "" {
			continue
		}
		t, ok := byDomain[domain]
		if !ok {
			t = &tally{people: map[int64]bool{}}
			byDomain[domain] = t
			order = append(order, domain)
		}
		t.messages++
		if person != 0 {
			t.people[person] = true
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	rules, err := s.OrgRules()
	if err != nil {
		return nil, err
	}
	out := make([]SenderDomain, 0, len(order))
	for _, domain := range order {
		t := byDomain[domain]
		org, stored := rules[domain]
		out = append(out, SenderDomain{
			Domain: domain, Messages: t.messages, People: len(t.people),
			Org: org, Stored: stored,
		})
	}
	// Busiest first, then alphabetical, so the list is the same on every run and
	// the domain a rule is most likely to be about is at the top.
	sort.Slice(out, func(i, j int) bool {
		if out[i].Messages != out[j].Messages {
			return out[i].Messages > out[j].Messages
		}
		return out[i].Domain < out[j].Domain
	})
	return out, nil
}
