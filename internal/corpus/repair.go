package corpus

import (
	"database/sql"
	"fmt"
	"strings"
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

// PlusAddressRepair is what RepairPlusAddresses did.
type PlusAddressRepair struct {
	Merged   int           // people folded into the holder of their base mailbox
	Anchored int           // base mailboxes nobody held, now recorded on the person
	Renamed  int           // people whose name was the tagged address they arrived as
	Left     []LeftAddress // tagged addresses this would not reduce
}

// LeftAddress is a subaddress the repair declined to reduce. It stays as it is
// and is reported, so declining is visible rather than quiet.
type LeftAddress struct {
	Value  string
	Reason string
}

// RepairPlusAddresses folds together the people one mailbox split into once it
// was written with RFC 5233 tags (see plusBaseAddress). Twenty-two rows for one
// human is the ordinary outcome: every signup gets its own tag, every tag is its
// own identity value, and each one mints a person holding their own share of the
// sent/received counts.
//
// The parse resolves tagged addresses to the base mailbox now, so no new corpus
// grows these. That does nothing for the rows already stored — identities are
// keyed on the value — which is why this exists alongside the parse fix rather
// than instead of it, the same division as RepairMailtoIdentities.
//
// The base mailbox's person survives, for the reason the mailto repair keeps the
// clean half: the base address is what a Slack profile and a correctly-quoted
// header match, so keeping them means no later sighting needs repairing again.
// Where nobody holds the base it is recorded on the person who holds the tag —
// under RFC 5233 that mailbox is already theirs, and recording it is what makes
// the next tag land on this person instead of minting another.
//
// A role mailbox is reduced like any other: `support+noreply@d` is delivered to
// `support@d` whatever genericLocalPart thinks of the word. That gate is about
// inferring a human across two domains, which is a different question from where
// one domain delivers its mail.
func RepairPlusAddresses(s *Store) (PlusAddressRepair, error) {
	var r PlusAddressRepair

	values, err := subaddressedValues(s)
	if err != nil {
		return r, err
	}
	for _, value := range values {
		base, _, ok := plusBaseAddress(value)
		if !ok {
			r.Left = append(r.Left, LeftAddress{
				Value: value, Reason: leftReason(value)})
			continue
		}
		// Through the alias table, so the reduction lands where resolution would:
		// on the current domain rather than one a rebrand has moved off.
		base, _, err := CanonicalAddress(s, base)
		if err != nil {
			return r, err
		}

		// Re-read the holder rather than trusting the listing: a merge earlier in
		// this pass may already have moved this value to somebody else.
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
			KindEmail, base).Scan(&owner)
		switch {
		case err == sql.ErrNoRows:
			if _, err := s.db.Exec(
				`insert into identities (person_id, kind, value, rule) values (?,?,?,?)`,
				holder, KindEmail, base, "repair:plus-address"); err != nil {
				return r, fmt.Errorf("recording base mailbox %s: %w", base, err)
			}
			r.Anchored++
		case err != nil:
			return r, fmt.Errorf("looking up %s: %w", base, err)
		case owner != holder:
			// The tagged identity moves with the merge rather than being deleted:
			// which tag a message arrived under is the thing this repair is careful
			// not to spend.
			if err := mergeWithReason(s, owner, holder,
				"repair:plus-address ("+value+")"); err != nil {
				return r, err
			}
			r.Merged++
		}

		// A person first seen as a tagged address carries it as their display name.
		// Named for the mailbox rather than the tag, because the tag describes one
		// signup and the person is all of them.
		res, err := s.db.Exec(`
			update people set display_name = ?
			 where display_name = ?
			   and exists (select 1 from identities i
			                where i.person_id = people.id and i.kind = ? and i.value = ?)`,
			base, value, KindEmail, base)
		if err != nil {
			return r, fmt.Errorf("renaming the holder of %s: %w", value, err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return r, err
		}
		r.Renamed += int(n)
	}
	return r, nil
}

// leftReason names why plusBaseAddress refused a subaddressed value, for a report
// a human has to act on.
func leftReason(value string) string {
	at := strings.LastIndex(value, "@")
	if at >= 0 && strings.HasPrefix(value[:at], "+") {
		return "no mailbox in front of the tag"
	}
	return "the tag names a different mailbox"
}

func subaddressedValues(s *Store) ([]string, error) {
	rows, err := s.db.Query(`
		select value from identities
		 where kind = ? and value like '%+%'
		 order by value`, KindEmail)
	if err != nil {
		return nil, fmt.Errorf("finding subaddressed identities: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		// A `+` in the domain is not a tag, and a value holding one has nothing
		// here to decide about — not even a refusal to report.
		if hasSubaddress(v) {
			out = append(out, v)
		}
	}
	return out, rows.Err()
}

// TruncatedNameRepair is what RepairTruncatedNames did.
type TruncatedNameRepair struct {
	Merged   int            // placeholders folded into the person the fragment names
	Cleaned  int            // identities that lost the bracket with nobody to merge into
	Renamed  int            // people carrying the bracket in their display name
	Welded   int            // names split from the address written inside them
	Declined []DeclinedName // pairs the evidence does not settle, left for review
}

// DeclinedName is a truncated identity the repair would not resolve by itself.
// It stays in the corpus and is reported by MergeCandidates, so declining is
// visible rather than quiet.
type DeclinedName struct {
	Value      string  // the identity as stored, bracket and all
	Clean      string  // the name it means
	Reason     string  // why the evidence was not enough
	Candidates []int64 // people of that name, where there were any
}

// RepairTruncatedNames fixes name-only identities left where a folded recipient
// list was split through the middle of an address (see rejoinFoldedAddresses).
// One fold, two shapes, by where the cut fell:
//
//   - cut at the bracket, `Name <`. The address is gone; the name survived and
//     kept the `<` that introduced it. A display name never legitimately ends in
//     one, so these are unambiguously corrupt.
//   - cut inside the bracket, `Name <addr`. The address survived, welded into
//     the name. Handled by repairWeldedName, which has real evidence to work
//     from rather than the weighing this one has to do.
//
// A `<` anywhere else with no address behind it is left alone: a name is the
// whole of a name-only participant's evidence, and one written with a bracket in
// it is still that person.
//
// For the first shape the address cannot be recovered — it was never stored — so unlike
// RepairMailtoIdentities this cannot reduce a value to the thing it always meant.
// All it can do is decide who the name belongs to, and a name is weak evidence:
// two colleagues can share one. So a merge is made only on two signals together:
//
//   - the target holds a machine-derived identity — an address or a Slack uid —
//     which makes them a confirmed human rather than a second placeholder. Folding
//     one placeholder into another buys nothing and risks everything.
//   - the target appears on every entry the placeholder appears on. That is the
//     fingerprint of this corruption: the placeholder exists only because one
//     sighting of a header was cut, and another sighting of the same conversation
//     carried the address. Two same-named colleagues do not track each other
//     through a whole corpus.
//
// Exactly one candidate must satisfy both. Two that do is the ambiguous case
// RepairMailtoIdentities refuses on too, and the reasoning is the same: the
// wrong choice attributes a stranger's mail to someone and Merge is deliberately
// hard to undo. Where no merge is made the value is still cleaned if the cleaned
// name is free, so the next correctly-parsed sighting lands on this person rather
// than minting yet another.
func RepairTruncatedNames(s *Store) (TruncatedNameRepair, error) {
	var r TruncatedNameRepair

	values, err := truncatedNameValues(s)
	if err != nil {
		return r, err
	}
	for _, value := range values {
		if name, addr, ok := weldedAddress(value); ok {
			if err := repairWeldedName(s, &r, value, name, addr); err != nil {
				return r, err
			}
			continue
		}
		if !strings.HasSuffix(value, "<") {
			continue
		}
		clean := CleanDisplayName(value)
		if clean == "" {
			r.Declined = append(r.Declined, DeclinedName{
				Value: value, Reason: "names nobody at all"})
			continue
		}
		clean, err := NormaliseIdentity(KindDisplayName, clean)
		if err != nil {
			return r, err
		}

		// Re-read the holder: a merge earlier in this pass may already have moved
		// this value to somebody else.
		var holder int64
		err = s.db.QueryRow(`select person_id from identities where kind=? and value=?`,
			KindDisplayName, value).Scan(&holder)
		if err == sql.ErrNoRows {
			continue
		} else if err != nil {
			return r, err
		}

		cands, err := peopleNamed(s, clean, holder)
		if err != nil {
			return r, err
		}
		strong, reason, err := strongestTarget(s, holder, cands)
		if err != nil {
			return r, err
		}
		if strong == 0 {
			r.Declined = append(r.Declined, DeclinedName{
				Value: value, Clean: clean, Reason: reason, Candidates: cands})
		} else {
			if err := mergeWithReason(s, strong, holder,
				"repair:truncated-name ("+value+")"); err != nil {
				return r, err
			}
			r.Merged++
		}

		// Whether or not the halves were folded, the bracket goes — unless the
		// clean name is already somebody's identity, in which case rewriting would
		// collide with it. That pair is a merge decision, and MergeCandidates
		// reports it rather than this deciding it.
		var taken int
		if err := s.db.QueryRow(
			`select count(*) from identities where kind=? and value=?`,
			KindDisplayName, clean).Scan(&taken); err != nil {
			return r, err
		}
		if taken > 0 {
			continue
		}
		if _, err := s.db.Exec(
			`update identities set value=?, rule=? where kind=? and value=?`,
			clean, "repair:truncated-name", KindDisplayName, value); err != nil {
			return r, fmt.Errorf("cleaning identity %q: %w", value, err)
		}
		if strong == 0 {
			r.Cleaned++
		}
	}

	renamed, err := repairTruncatedPeopleNames(s)
	if err != nil {
		return r, err
	}
	r.Renamed = renamed
	return r, nil
}

// weldedAddress splits a display-name identity that swallowed the address it was
// introducing: `Name <addr`, the fold cut a few characters past the bracket. ok
// is false where there is no address behind the bracket, which is the display
// name that merely contains a `<` and is nobody's corruption.
//
// The name may come back empty (`<addr`): the address alone still names the
// person, and there is no name left to contradict it.
func weldedAddress(value string) (name, addr string, ok bool) {
	i := strings.Index(value, "<")
	if i < 0 {
		return "", "", false
	}
	addr = strings.ToLower(reHeaderEmail.FindString(value[i+1:]))
	if addr == "" {
		return "", "", false
	}
	return CleanDisplayName(value[:i]), addr, true
}

// repairWeldedName resolves one welded identity into the name and the address it
// holds. Unlike the truncated shape this needs no weighing of names: the address
// is right there, so the person is whoever holds that address, which is exactly
// what a correctly-parsed header would have decided.
//
// One gate, and it is the surname gate the dedupe rules use. `Cy Marrow
// <ellen@…>` is a real header — a person writing from somebody else's mailbox —
// and where the address is demonstrably somebody else's the fragment names a
// second human whose own address was lost, not a duplicate of the first. That
// pair is refused and left welded so `corpus candidates` can show it; a merge
// cannot be undone, and a name that is merely absent or an address costs nothing
// here because it contradicts nothing.
func repairWeldedName(s *Store, r *TruncatedNameRepair, value, name, addr string) error {
	var holder int64
	err := s.db.QueryRow(`select person_id from identities where kind=? and value=?`,
		KindDisplayName, value).Scan(&holder)
	if err == sql.ErrNoRows {
		return nil // an earlier merge in this pass already moved it
	} else if err != nil {
		return err
	}
	// Through both reductions, so this lands on the person resolution would have
	// landed on rather than on a domain a rebrand moved off or a mailbox a tag
	// split in two.
	addr, _, err = CanonicalAddress(s, addr)
	if err != nil {
		return err
	}
	if base, _, ok := plusBaseAddress(addr); ok {
		addr = base
	}

	// Set where this wrote something, so a second pass over a corpus it already
	// took apart reports the nothing it did.
	var changed bool

	var owner int64
	err = s.db.QueryRow(`select person_id from identities where kind=? and value=?`,
		KindEmail, addr).Scan(&owner)
	switch {
	case err == sql.ErrNoRows:
		// Nobody holds the address, so there is nothing to merge into and no
		// decision to make: this person simply becomes reachable by the address
		// they were carrying, and the next sighting of it lands on them.
		if _, err := s.db.Exec(
			`insert into identities (person_id, kind, value, rule) values (?,?,?,?)`,
			holder, KindEmail, addr, "repair:welded-address"); err != nil {
			return fmt.Errorf("recording welded address %s: %w", addr, err)
		}
		changed = true
	case err != nil:
		return fmt.Errorf("looking up %s: %w", addr, err)
	case owner == holder:
		// Already one person, so only the name half is left to take apart.
	default:
		ownerName, err := displayNameOf(s, owner)
		if err != nil {
			return err
		}
		if disagreeOnSurname(name, ownerName) {
			r.Declined = append(r.Declined, DeclinedName{
				Value: value, Clean: name,
				Reason:     "the welded address belongs to " + ownerName + ", a different surname",
				Candidates: []int64{owner}})
			return nil
		}
		if err := mergeWithReason(s, owner, holder,
			"repair:welded-address ("+value+")"); err != nil {
			return err
		}
		r.Merged++
		changed = true
	}

	if name == "" {
		if changed {
			r.Welded++
		}
		return nil
	}
	clean, err := NormaliseIdentity(KindDisplayName, name)
	if err != nil {
		// Nothing usable to rename it to; the address carried the person.
		if changed {
			r.Welded++
		}
		return nil
	}
	// As in the truncated path: the bracket goes unless the clean name is already
	// somebody's identity, in which case rewriting would collide and the pair is a
	// merge decision MergeCandidates reports rather than this deciding it.
	var taken int
	if err := s.db.QueryRow(`select count(*) from identities where kind=? and value=?`,
		KindDisplayName, clean).Scan(&taken); err != nil {
		return err
	}
	if taken == 0 {
		if _, err := s.db.Exec(
			`update identities set value=?, rule=? where kind=? and value=?`,
			clean, "repair:welded-address", KindDisplayName, value); err != nil {
			return fmt.Errorf("splitting identity %q: %w", value, err)
		}
		changed = true
	}
	if changed {
		r.Welded++
	}
	return nil
}

// disagreeOnSurname reports whether two display names carry surnames that
// contradict each other. A name that is an address, a single first name or
// nothing at all contradicts nobody: "Camille" does not disagree with "Camille
// Cruz", where "Camille Nguyen" does.
func disagreeOnSurname(a, b string) bool {
	pa, aNamed := personNameParts(a)
	pb, bNamed := personNameParts(b)
	if !aNamed || !bNamed {
		return false
	}
	return len(distinctSurnames([]orgPerson{pa, pb})) > 1
}

func displayNameOf(s *Store, person int64) (string, error) {
	var name string
	err := s.db.QueryRow(`select display_name from people where id=?`, person).Scan(&name)
	return name, err
}

// WeldedNameCandidates pairs every display-name identity still holding an address
// with the person that address belongs to. After a repair pass what is left here
// is what the repair refused, which is the point: a refusal that nothing reports
// is indistinguishable from a corpus with nothing wrong in it.
func WeldedNameCandidates(s *Store) ([]MergeCandidate, error) {
	rows, err := s.db.Query(`
		select i.person_id, p.display_name, i.value from identities i
		  join people p on p.id = i.person_id
		 where i.kind = ? and i.value like '%<%'
		 order by i.value`, KindDisplayName)
	if err != nil {
		return nil, fmt.Errorf("finding welded identities: %w", err)
	}
	type welded struct {
		id    int64
		name  string
		value string
	}
	var todo []welded
	for rows.Next() {
		var w welded
		if err := rows.Scan(&w.id, &w.name, &w.value); err != nil {
			rows.Close()
			return nil, err
		}
		todo = append(todo, w)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	var out []MergeCandidate
	for _, w := range todo {
		_, addr, ok := weldedAddress(w.value)
		if !ok {
			continue
		}
		addr, _, err := CanonicalAddress(s, addr)
		if err != nil {
			return nil, err
		}
		if base, _, ok := plusBaseAddress(addr); ok {
			addr = base
		}
		var owner int64
		err = s.db.QueryRow(`select person_id from identities where kind=? and value=?`,
			KindEmail, addr).Scan(&owner)
		if err == sql.ErrNoRows || owner == w.id {
			continue
		} else if err != nil {
			return nil, err
		}
		ownerName, err := displayNameOf(s, owner)
		if err != nil {
			return nil, err
		}
		addrs, err := emailsOf(s, owner)
		if err != nil {
			return nil, err
		}
		out = append(out, MergeCandidate{
			AID: w.id, AName: w.name, AAddresses: []string{w.value},
			BID: owner, BName: ownerName, BAddresses: addrs,
			Reason: "display name holds an address somebody else owns",
		})
	}
	return out, nil
}

// strongestTarget names the one candidate the evidence settles on, or reports why
// it settles on none. A returned id of 0 always comes with a reason.
func strongestTarget(s *Store, holder int64, cands []int64) (int64, string, error) {
	if len(cands) == 0 {
		return 0, "no other person of that name", nil
	}
	var withIdentity, strong []int64
	for _, c := range cands {
		ok, err := hasMachineIdentity(s, c)
		if err != nil {
			return 0, "", err
		}
		if !ok {
			continue
		}
		withIdentity = append(withIdentity, c)
		ok, err = sharesEveryEntry(s, holder, c)
		if err != nil {
			return 0, "", err
		}
		if ok {
			strong = append(strong, c)
		}
	}
	switch {
	case len(strong) == 1:
		return strong[0], "", nil
	case len(strong) > 1:
		return 0, fmt.Sprintf("%d people of that name fit equally well", len(strong)), nil
	case len(withIdentity) == 0:
		return 0, "no address-backed person of that name", nil
	default:
		return 0, "shares no conversation with the person of that name", nil
	}
}

// hasMachineIdentity reports whether a person is anchored to something derivable
// — an address or a Slack uid — rather than being a display name and nothing else.
func hasMachineIdentity(s *Store, person int64) (bool, error) {
	var n int
	err := s.db.QueryRow(`
		select count(*) from identities
		 where person_id=? and kind in (?,?)`, person, KindEmail, KindSlackUID).Scan(&n)
	return n > 0, err
}

// sharesEveryEntry reports whether other took part in every entry person did.
// A person on no entry at all fails: there is nothing there to corroborate, and
// a name match alone is not enough to merge on.
func sharesEveryEntry(s *Store, person, other int64) (bool, error) {
	var total, alone int
	if err := s.db.QueryRow(
		`select count(*) from participants where person_id=?`, person).Scan(&total); err != nil {
		return false, err
	}
	if total == 0 {
		return false, nil
	}
	if err := s.db.QueryRow(`
		select count(*) from participants a
		 where a.person_id = ?
		   and not exists (select 1 from participants b
		                    where b.entry_id = a.entry_id and b.person_id = ?)`,
		person, other).Scan(&alone); err != nil {
		return false, err
	}
	return alone == 0, nil
}

// peopleNamed lists everyone whose name is this one, by their display name or by
// a name-only identity, excluding the person asking.
func peopleNamed(s *Store, name string, exclude int64) ([]int64, error) {
	rows, err := s.db.Query(`
		select p.id, p.display_name from people p where p.id <> ?
		union
		select i.person_id, ? from identities i
		 where i.kind = ? and i.value = ? and i.person_id <> ?`,
		exclude, name, KindDisplayName, name, exclude)
	if err != nil {
		return nil, fmt.Errorf("finding people named %q: %w", name, err)
	}
	defer rows.Close()
	seen := map[int64]bool{}
	var out []int64
	for rows.Next() {
		var id int64
		var display string
		if err := rows.Scan(&id, &display); err != nil {
			return nil, err
		}
		// Compared after normalisation rather than in SQL, so "Tom  D" and "Tom D"
		// are the one name here exactly as they are in identities.
		norm, err := NormaliseIdentity(KindDisplayName, display)
		if err != nil || norm != name || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out, rows.Err()
}

// truncatedNameValues lists every display-name identity holding a `<`. Both
// shapes of the fold are in here, and so are the names that legitimately contain
// one; the caller tells them apart.
func truncatedNameValues(s *Store) ([]string, error) {
	rows, err := s.db.Query(`
		select value from identities
		 where kind = ? and value like '%<%'
		 order by value`, KindDisplayName)
	if err != nil {
		return nil, fmt.Errorf("finding truncated identities: %w", err)
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

// repairTruncatedPeopleNames sweeps the display names people were given from
// these fragments. Separate from the identities for the same reason the mailto
// sweep is: a merge can leave the bracket on a survivor who never held the
// identity that carried it.
func repairTruncatedPeopleNames(s *Store) (int, error) {
	rows, err := s.db.Query(
		`select id, display_name from people where display_name like '%<%'`)
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
		clean := CleanDisplayName(p.name)
		// A name with an address welded into it is cut at the bracket instead: the
		// address is the person's, but it is not part of what they are called.
		if name, _, ok := weldedAddress(p.name); ok && name != "" {
			clean = name
		}
		if clean == "" || clean == p.name {
			continue
		}
		if _, err := s.db.Exec(`update people set display_name=? where id=?`,
			clean, p.id); err != nil {
			return n, fmt.Errorf("renaming person %d: %w", p.id, err)
		}
		n++
	}
	return n, nil
}
