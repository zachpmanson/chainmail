package corpus

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// CanonicalAddress applies any configured domain alias to an address. Resolution
// keys on the canonical form, so a rebrand cannot split one human in two.
func CanonicalAddress(s *Store, addr string) (string, string, error) {
	addr = strings.ToLower(strings.TrimSpace(addr))
	at := strings.LastIndex(addr, "@")
	if at < 0 {
		return addr, "", nil
	}
	local, domain := addr[:at], addr[at+1:]

	var to string
	err := s.db.QueryRow(
		`select to_domain from domain_aliases where from_domain=?`, domain).Scan(&to)
	switch {
	case err == sql.ErrNoRows:
		return addr, "", nil
	case err != nil:
		return addr, "", fmt.Errorf("looking up alias for %s: %w", domain, err)
	}
	return local + "@" + to, "domain-alias:" + domain, nil
}

// AddDomainAlias records an alias and then repairs the split it was created to
// fix: any two people whose only difference is that domain are merged.
//
// Doing the repair here rather than leaving it to a separate pass matters,
// because an alias added after ingest is the normal case — you discover the
// rebrand by seeing the duplicate.
func AddDomainAlias(s *Store, from, to, note string) (merged int, err error) {
	from = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(from, "@")))
	to = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(to, "@")))
	if from == "" || to == "" {
		return 0, errors.New("alias needs both a from-domain and a to-domain")
	}
	if from == to {
		return 0, errors.New("an alias from a domain to itself does nothing")
	}

	if _, err := s.db.Exec(`
		insert into domain_aliases (from_domain, to_domain, note, added_at) values (?,?,?,?)
		on conflict(from_domain) do update set to_domain=excluded.to_domain, note=excluded.note`,
		from, to, nullStr(note), time.Now().Unix()); err != nil {
		return 0, fmt.Errorf("recording alias %s -> %s: %w", from, to, err)
	}

	// Find people holding an address on the old domain whose local part also
	// exists on the new one, and fold the old into the new.
	rows, err := s.db.Query(`
		select old.person_id, new.person_id, old.value
		  from identities old
		  join identities new
		    on new.kind='email'
		   and new.value = substr(old.value, 1, instr(old.value,'@')) || ?
		 where old.kind='email'
		   and old.value like '%@' || ?
		   and old.person_id <> new.person_id`, to, from)
	if err != nil {
		return 0, fmt.Errorf("finding people split by %s: %w", from, err)
	}
	type pair struct {
		keep, drop int64
		addr       string
	}
	var pairs []pair
	for rows.Next() {
		var p pair
		if err := rows.Scan(&p.drop, &p.keep, &p.addr); err != nil {
			rows.Close()
			return 0, err
		}
		pairs = append(pairs, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	for _, p := range pairs {
		// A previous iteration may already have folded one of these away.
		if err := mergeWithReason(s, p.keep, p.drop,
			"domain-alias:"+from+" ("+p.addr+")"); err != nil {
			if strings.Contains(err.Error(), "no person") {
				continue
			}
			return merged, err
		}
		merged++
	}
	return merged, nil
}

// DomainAliases lists the configured aliases.
func DomainAliases(s *Store) (map[string]string, error) {
	rows, err := s.db.Query(`select from_domain, to_domain from domain_aliases order by from_domain`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var f, t string
		if err := rows.Scan(&f, &t); err != nil {
			return nil, err
		}
		out[f] = t
	}
	return out, rows.Err()
}

// MergeCandidate is a pair of people that may be one human.
type MergeCandidate struct {
	AID, BID   int64
	AName      string
	BName      string
	Reason     string
	AAddresses []string
	BAddresses []string
}

// MergeCandidates reports pairs worth a human glance rather than merging them.
// Automatic matching gets some of these wrong — two colleagues can share a
// surname, and a local part can coincide across unrelated domains — so this
// surfaces evidence and leaves the decision alone.
func MergeCandidates(s *Store) ([]MergeCandidate, error) {
	rows, err := s.db.Query(`
		select a.person_id, b.person_id, a.value, b.value
		  from identities a join identities b
		    on a.kind='email' and b.kind='email'
		   and a.person_id < b.person_id
		   and substr(a.value, 1, instr(a.value,'@')) = substr(b.value, 1, instr(b.value,'@'))
		   and substr(a.value, instr(a.value,'@')) <> substr(b.value, instr(b.value,'@'))`)
	if err != nil {
		return nil, fmt.Errorf("finding merge candidates: %w", err)
	}
	defer rows.Close()

	var out []MergeCandidate
	for rows.Next() {
		var c MergeCandidate
		var aAddr, bAddr string
		if err := rows.Scan(&c.AID, &c.BID, &aAddr, &bAddr); err != nil {
			return nil, err
		}
		c.AAddresses = []string{aAddr}
		c.BAddresses = []string{bAddr}
		c.Reason = "same local part, different domain"
		_ = s.db.QueryRow(`select display_name from people where id=?`, c.AID).Scan(&c.AName)
		_ = s.db.QueryRow(`select display_name from people where id=?`, c.BID).Scan(&c.BName)
		out = append(out, c)
	}
	return out, rows.Err()
}
