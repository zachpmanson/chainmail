package corpus

import (
	"database/sql"
	"fmt"
	"strings"
)

// MailtoRepair is what RepairMailtoIdentities did.
type MailtoRepair struct {
	Rewritten int      // identities reduced to the address they always meant
	Renamed   int      // people whose display name was one of those values
	Merged    int      // people that turned out to be one, once reduced
	Ambiguous []string // values naming more than one address, left untouched
}

// RepairMailtoIdentities reduces stored email identities that carry Outlook's
// hyperlinked rendering (see mailtoAddress) to the address they name, and folds
// together the people that reduction reveals to be one human.
//
// The parse fix alone is not enough, and cannot be: identities are keyed on the
// value, so every address already stored in the malformed form stays a second
// person forever, with their own share of the sent/received counts and no way to
// match a Slack profile email. Repairing in the same call that would otherwise
// leave a known-broken row behind follows AddDomainAlias, for the same reason —
// you find out about the split by seeing the duplicate.
//
// Where both forms of an address exist, the clean identity's person survives:
// that is the one every correctly-quoted header and every Slack profile will go
// on matching, so keeping it means no future sighting has to be repaired again.
func RepairMailtoIdentities(s *Store) (MailtoRepair, error) {
	var r MailtoRepair

	type row struct {
		person int64
		value  string
	}
	rows, err := s.db.Query(`
		select person_id, value from identities
		 where kind = ? and value like '%mailto:%'
		 order by value`, KindEmail)
	if err != nil {
		return r, fmt.Errorf("finding malformed identities: %w", err)
	}
	var todo []row
	for rows.Next() {
		var it row
		if err := rows.Scan(&it.person, &it.value); err != nil {
			rows.Close()
			return r, err
		}
		todo = append(todo, it)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return r, err
	}

	for _, it := range todo {
		_, addr, ok := mailtoAddress(it.value)
		if !ok {
			r.Ambiguous = append(r.Ambiguous, it.value)
			continue
		}
		// Through the alias table, so the repair lands where resolution would
		// have landed rather than on an address a rebrand has since moved.
		clean, _, err := CanonicalAddress(s, addr)
		if err != nil {
			return r, err
		}
		if clean == it.value {
			continue
		}

		// A person first seen as one of these values carries it as their display
		// name as well, and the address reads better than the address twice.
		res, err := s.db.Exec(`
			update people set display_name = ? where id = ? and display_name like '%mailto:%'`,
			clean, it.person)
		if err != nil {
			return r, fmt.Errorf("renaming person %d: %w", it.person, err)
		}
		if n, _ := res.RowsAffected(); n > 0 {
			r.Renamed++
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
				clean, "repair:mailto", KindEmail, it.value); err != nil {
				return r, fmt.Errorf("repairing identity %s: %w", it.value, err)
			}
		case err != nil:
			return r, fmt.Errorf("looking up %s: %w", clean, err)
		default:
			// The value is duplicate evidence for an address already recorded, so
			// it goes rather than being kept as a second spelling nothing will ever
			// look up. Any references behind it move first.
			if owner != it.person {
				if err := mergeWithReason(s, owner, it.person,
					"repair:mailto ("+it.value+")"); err != nil {
					return r, err
				}
				r.Merged++
			}
			if _, err := s.db.Exec(`delete from identities where kind=? and value=?`,
				KindEmail, it.value); err != nil {
				return r, fmt.Errorf("dropping identity %s: %w", it.value, err)
			}
		}
		r.Rewritten++
	}

	// A display name can carry one of these values without its owner holding a
	// malformed identity — a name-only sighting, or a person whose malformed half
	// was merged away by hand already.
	named, err := s.db.Query(
		`select id, display_name from people where display_name like '%mailto:%'`)
	if err != nil {
		return r, err
	}
	var stragglers []row
	for named.Next() {
		var it row
		if err := named.Scan(&it.person, &it.value); err != nil {
			named.Close()
			return r, err
		}
		stragglers = append(stragglers, it)
	}
	named.Close()
	if err := named.Err(); err != nil {
		return r, err
	}
	for _, it := range stragglers {
		_, addr, ok := mailtoAddress(it.value)
		if !ok || addr == it.value {
			continue
		}
		if _, err := s.db.Exec(`update people set display_name=? where id=?`,
			addr, it.person); err != nil {
			return r, fmt.Errorf("renaming person %d: %w", it.person, err)
		}
		r.Renamed++
	}
	return r, nil
}

// MalformedIdentities lists email identities carrying a mailto: link, so the
// damage can be counted before anything is changed.
func MalformedIdentities(s *Store) ([]string, error) {
	rows, err := s.db.Query(`
		select value from identities where kind = ? and value like '%mailto:%' order by value`,
		KindEmail)
	if err != nil {
		return nil, err
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

// hasMailto is the stored-value counterpart of hasMailtoLink, kept so callers
// outside this package can count the damage without duplicating the predicate.
func HasMailtoLink(value string) bool { return strings.Contains(strings.ToLower(value), "mailto:") }
