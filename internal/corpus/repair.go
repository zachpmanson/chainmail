package corpus

import (
	"database/sql"
	"fmt"
)

// MailtoRepair is what RepairMailtoIdentities did.
type MailtoRepair struct {
	Rewritten int      // identities reduced to the address they always meant
	Renamed   int      // people who carried such a value as their display name
	Merged    int      // people that turned out to be one, once reduced
	Ambiguous []string // values naming more than one address, left untouched
}

// RepairMailtoIdentities reduces stored email identities carrying Outlook's
// hyperlinked rendering (see mailtoAddress) to the address they name, and folds
// together the people that reduction reveals to be one human.
//
// A parse fix alone cannot do this, and that is the point: identities are keyed
// on the value, so an address already stored in the malformed form remains a
// second person forever, holding their own share of the sent/received counts and
// matching no Slack profile email. Repairing the rows rather than only the parser
// follows AddDomainAlias, for the same reason — the split is what makes you go
// looking in the first place.
//
// Where both forms of one address exist the clean identity's person survives:
// that is the person every correctly-quoted header and every Slack profile will
// go on matching, so keeping them means no later sighting needs repairing again.
func RepairMailtoIdentities(s *Store) (MailtoRepair, error) {
	var r MailtoRepair

	values, err := malformedValues(s)
	if err != nil {
		return r, err
	}
	for _, value := range values {
		_, addr, ok := mailtoAddress(value)
		if !ok {
			r.Ambiguous = append(r.Ambiguous, value)
			continue
		}
		// Through the alias table, so the repair lands where resolution would have
		// landed rather than on a domain a rebrand has since moved off.
		clean, _, err := CanonicalAddress(s, addr)
		if err != nil {
			return r, err
		}

		// Re-read the holder rather than trusting the listing: an earlier merge in
		// this same pass may already have moved this value to someone else.
		var holder int64
		err = s.db.QueryRow(`select person_id from identities where kind=? and value=?`,
			KindEmail, value).Scan(&holder)
		if err == sql.ErrNoRows {
			continue
		} else if err != nil {
			return r, err
		}

		var owner int64
		err = s.db.QueryRow(`select person_id from identities where kind=? and value=?`,
			KindEmail, clean).Scan(&owner)
		switch {
		case err == sql.ErrNoRows:
			// Nobody holds the clean address, so this person simply becomes
			// reachable by it and keeps every reference they already had.
			if _, err := s.db.Exec(`
				update identities set value = ?, rule = ? where kind = ? and value = ?`,
				clean, "repair:mailto", KindEmail, value); err != nil {
				return r, fmt.Errorf("repairing identity %s: %w", value, err)
			}
		case err != nil:
			return r, fmt.Errorf("looking up %s: %w", clean, err)
		default:
			// Duplicate evidence for an address already recorded, so it goes rather
			// than staying as a second spelling nothing will ever look up. Whatever
			// hangs off it moves first.
			if owner != holder {
				if err := mergeWithReason(s, owner, holder,
					"repair:mailto ("+value+")"); err != nil {
					return r, err
				}
				r.Merged++
			}
			if _, err := s.db.Exec(`delete from identities where kind=? and value=?`,
				KindEmail, value); err != nil {
				return r, fmt.Errorf("dropping identity %s: %w", value, err)
			}
		}
		r.Rewritten++
	}

	// A person first seen as one of these values carries it as their display name
	// too. Swept separately from the identities because the two do not correspond:
	// a merge can leave the value on a survivor who never held the identity, and a
	// name-only sighting can carry it with no identity behind it at all.
	renamed, err := repairMailtoNames(s)
	if err != nil {
		return r, err
	}
	r.Renamed = renamed
	return r, nil
}

func malformedValues(s *Store) ([]string, error) {
	rows, err := s.db.Query(`
		select value from identities
		 where kind = ? and value like '%mailto:%'
		 order by value`, KindEmail)
	if err != nil {
		return nil, fmt.Errorf("finding malformed identities: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func repairMailtoNames(s *Store) (int, error) {
	rows, err := s.db.Query(
		`select id, display_name from people where display_name like '%mailto:%'`)
	if err != nil {
		return 0, err
	}
	type named struct {
		id   int64
		name string
	}
	var todo []named
	for rows.Next() {
		var n named
		if err := rows.Scan(&n.id, &n.name); err != nil {
			rows.Close()
			return 0, err
		}
		todo = append(todo, n)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	var n int
	for _, p := range todo {
		_, addr, ok := mailtoAddress(p.name)
		if !ok {
			continue
		}
		if _, err := s.db.Exec(`update people set display_name=? where id=?`,
			addr, p.id); err != nil {
			return n, fmt.Errorf("renaming person %d: %w", p.id, err)
		}
		n++
	}
	return n, nil
}
