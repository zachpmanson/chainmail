package corpus

import (
	"database/sql"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"
)

// Identity kinds. `email` and `slack_uid` are machine-derivable; `display_name`
// exists because some participants are only ever a first name in someone else's
// quoted header ("Ben", "Johan") and no rule can turn that into an address. Such
// an identity is a placeholder a human later merges into the right person.
const (
	KindEmail       = "email"
	KindSlackUID    = "slack_uid"
	KindDisplayName = "display_name"
)

// Participation roles, as they appear in participants.role.
const (
	RoleFrom = "from"
	RoleTo   = "to"
	RoleCc   = "cc"
)

// Address is one parsed entry from a From/To/Cc header. Raw is kept because a
// header that would not parse is still evidence, and the fallback path needs
// something to show for it.
type Address struct {
	Addr string // lowercased address; empty when the header held only a name
	Name string // display name as written
	Raw  string // the fragment this came from, trimmed
}

// ParseAddresses splits an address header into addresses. It never returns
// nothing for a non-empty header: unparseable junk comes back as a name-only
// Address rather than being dropped, because a recipient we cannot parse is
// still a recipient, and silently losing them is the bug this whole file exists
// to fix.
//
// net/mail.ParseAddressList is tried first — it is the only thing that handles
// quoted display names containing commas ("Dempster, Tom" <t@x>) correctly. It
// is all-or-nothing, though: one malformed entry fails the whole list, so the
// fallback re-splits on commas outside quotes and parses each fragment alone.
func ParseAddresses(header string) []Address {
	header = strings.TrimSpace(header)
	if header == "" {
		return nil
	}
	if list, err := mail.ParseAddressList(header); err == nil {
		out := make([]Address, 0, len(list))
		for _, a := range list {
			out = append(out, Address{
				Addr: strings.ToLower(strings.TrimSpace(a.Address)),
				Name: strings.TrimSpace(a.Name),
				Raw:  strings.TrimSpace(a.String()),
			})
		}
		return out
	}
	var out []Address
	for _, frag := range splitOutsideQuotes(header) {
		if a, ok := ParseAddress(frag); ok {
			out = append(out, a)
		}
	}
	return out
}

// ParseAddress parses a single address fragment, degrading rather than failing:
// a bracketed address, then a bare address, then a bare name. ok is false only
// when the fragment held nothing usable at all.
func ParseAddress(frag string) (Address, bool) {
	frag = strings.TrimSpace(frag)
	if frag == "" {
		return Address{}, false
	}
	if a, err := mail.ParseAddress(frag); err == nil {
		return Address{
			Addr: strings.ToLower(strings.TrimSpace(a.Address)),
			Name: strings.TrimSpace(a.Name),
			Raw:  frag,
		}, true
	}
	if i, j := strings.Index(frag, "<"), strings.LastIndex(frag, ">"); i >= 0 && j > i {
		addr := strings.ToLower(strings.TrimSpace(frag[i+1 : j]))
		name := strings.Trim(strings.TrimSpace(frag[:i]), `"'`)
		if addr != "" {
			return Address{Addr: addr, Name: strings.TrimSpace(name), Raw: frag}, true
		}
		if name != "" {
			return Address{Name: strings.TrimSpace(name), Raw: frag}, true
		}
		return Address{}, false
	}
	if strings.Contains(frag, "@") && !strings.ContainsAny(frag, " \t") {
		return Address{Addr: strings.ToLower(frag), Raw: frag}, true
	}
	return Address{Name: strings.Trim(frag, `"'`), Raw: frag}, true
}

// splitOutsideQuotes splits on commas and semicolons that are not inside a
// quoted string, so a display name of the "Surname, First" form survives.
func splitOutsideQuotes(s string) []string {
	var (
		out []string
		cur strings.Builder
		inQ bool
		esc bool
	)
	flush := func() {
		if t := strings.TrimSpace(cur.String()); t != "" {
			out = append(out, t)
		}
		cur.Reset()
	}
	for _, r := range s {
		switch {
		case esc:
			cur.WriteRune(r)
			esc = false
		case r == '\\' && inQ:
			cur.WriteRune(r)
			esc = true
		case r == '"':
			inQ = !inQ
			cur.WriteRune(r)
		case (r == ',' || r == ';') && !inQ:
			flush()
		default:
			cur.WriteRune(r)
		}
	}
	flush()
	return out
}

// NormaliseIdentity canonicalises an identity value for lookup. Addresses are
// case-insensitive; Slack ids are quoted as <@U0123> in message text and are
// stored bare; display names are folded on case and whitespace so "tom  d" and
// "Tom D" are one identity.
func NormaliseIdentity(kind, value string) (string, error) {
	v := strings.TrimSpace(value)
	switch kind {
	case KindEmail:
		v = strings.ToLower(v)
		v = strings.TrimSuffix(strings.TrimPrefix(v, "<"), ">")
	case KindSlackUID:
		v = strings.TrimSuffix(strings.TrimPrefix(v, "<@"), ">")
		v = strings.TrimPrefix(v, "@")
		v = strings.ToUpper(v)
	case KindDisplayName:
		v = strings.ToLower(strings.Join(strings.Fields(v), " "))
	default:
		return "", fmt.Errorf("unknown identity kind %q", kind)
	}
	if v == "" {
		return "", fmt.Errorf("empty %s identity", kind)
	}
	return v, nil
}

// Resolve finds or creates the person behind one identity, keyed on
// (kind, value) in identities. displayName is advisory: it names a newly created
// person, and it upgrades an existing person whose only name so far was an
// address — the first sighting of an address often has no name attached, and a
// corpus of raw addresses is unreadable.
//
// The rule that matched is recorded so a later bad merge can be traced to the
// step that produced it.
func Resolve(s *Store, kind, value, displayName string) (int64, error) {
	return ResolveWithRule(s, kind, value, displayName, "auto:"+kind)
}

// ResolveWithRule is Resolve with the matching rule named by the caller, e.g.
// "mail:from-header" or "manual:alias".
func ResolveWithRule(s *Store, kind, value, displayName, rule string) (int64, error) {
	v, err := NormaliseIdentity(kind, value)
	if err != nil {
		return 0, err
	}
	name := strings.TrimSpace(displayName)

	var id int64
	err = s.db.QueryRow(
		`select person_id from identities where kind=? and value=?`, kind, v).Scan(&id)
	switch {
	case err == nil:
		if name != "" {
			if _, err := s.db.Exec(`
				update people set display_name=?
				where id=? and (display_name='' or display_name like '%@%')
				  and ? not like '%@%'`, name, id, name); err != nil {
				return 0, fmt.Errorf("naming person %d: %w", id, err)
			}
		}
		return id, nil
	case err != sql.ErrNoRows:
		return 0, fmt.Errorf("looking up %s %s: %w", kind, v, err)
	}

	if name == "" {
		name = value
	}
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`insert into people (display_name) values (?)`, name)
	if err != nil {
		return 0, fmt.Errorf("creating person for %s: %w", v, err)
	}
	if id, err = res.LastInsertId(); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(
		`insert into identities (person_id, kind, value, rule) values (?,?,?,?)`,
		id, kind, v, rule); err != nil {
		return 0, fmt.Errorf("recording identity %s %s: %w", kind, v, err)
	}
	return id, tx.Commit()
}

// ResolveAddress resolves a parsed header address: by address when there is one,
// and by display name when there is not. A name-only participant deliberately
// becomes a real person rather than being skipped — someone cc'd as "Johan" is a
// participant, and leaving them out is what made recipients invisible before.
func ResolveAddress(s *Store, a Address, rule string) (int64, error) {
	if a.Addr != "" {
		return ResolveWithRule(s, KindEmail, a.Addr, a.Name, rule)
	}
	if strings.TrimSpace(a.Name) != "" {
		return ResolveWithRule(s, KindDisplayName, a.Name, a.Name, rule+":name-only")
	}
	return 0, fmt.Errorf("address %q has neither address nor name", a.Raw)
}

// AddAlias attaches an identity to an existing person by hand. This is the
// escape hatch for what cannot be inferred: a display-name-only participant, or
// a Slack account registered under a pre-rebrand domain that no address rule
// will ever tie to the current one.
//
// Re-pointing an identity that already belongs to someone else is an error, not
// a silent steal: that is a merge, and merges are recorded.
func AddAlias(s *Store, personID int64, kind, value, rule string) error {
	v, err := NormaliseIdentity(kind, value)
	if err != nil {
		return err
	}
	if rule == "" {
		rule = "manual:alias"
	}
	var owner int64
	err = s.db.QueryRow(
		`select person_id from identities where kind=? and value=?`, kind, v).Scan(&owner)
	switch {
	case err == nil && owner == personID:
		_, err := s.db.Exec(
			`update identities set rule=? where kind=? and value=?`, rule, kind, v)
		return err
	case err == nil:
		return fmt.Errorf("identity %s %s already belongs to person %d; merge instead",
			kind, v, owner)
	case err != sql.ErrNoRows:
		return err
	}
	var exists int
	if err := s.db.QueryRow(`select count(*) from people where id=?`, personID).
		Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		return fmt.Errorf("no person %d", personID)
	}
	if _, err := s.db.Exec(
		`insert into identities (person_id, kind, value, rule) values (?,?,?,?)`,
		personID, kind, v, rule); err != nil {
		return fmt.Errorf("adding alias %s %s: %w", kind, v, err)
	}
	return nil
}

// Merge folds drop into keep: every identity and every reference is repointed,
// then the emptied person row is deleted. All of it in one transaction, because a
// half-merged person is worse than either an unmerged or a merged one — it is a
// person with references to a row that no longer exists.
//
// No identity is lost. That is the property that makes a merge recoverable: the
// dropped person's addresses still resolve, they just resolve to keep, and
// person_merges says when and why they moved.
func Merge(s *Store, keep, drop int64) error {
	return mergeWithReason(s, keep, drop, "manual:merge")
}

func mergeWithReason(s *Store, keep, drop int64, reason string) error {
	if keep == drop {
		return errors.New("cannot merge a person into themselves")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var keepName, dropName string
	if err := tx.QueryRow(`select display_name from people where id=?`, keep).
		Scan(&keepName); err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("no person %d to keep", keep)
		}
		return err
	}
	if err := tx.QueryRow(`select display_name from people where id=?`, drop).
		Scan(&dropName); err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("no person %d to drop", drop)
		}
		return err
	}

	if _, err := tx.Exec(
		`update identities set person_id=? where person_id=?`, keep, drop); err != nil {
		return fmt.Errorf("repointing identities of %d: %w", drop, err)
	}
	if _, err := tx.Exec(
		`update entries set person_id=? where person_id=?`, keep, drop); err != nil {
		return fmt.Errorf("repointing entries of %d: %w", drop, err)
	}
	// Both halves may have participated in the same entry in the same role, and
	// (entry_id, person_id, role) is the primary key — so repoint what does not
	// collide, and drop what does. "or ignore" leaves the colliding rows behind,
	// which the delete then clears.
	if _, err := tx.Exec(
		`update or ignore participants set person_id=? where person_id=?`, keep, drop); err != nil {
		return fmt.Errorf("repointing participation of %d: %w", drop, err)
	}
	if _, err := tx.Exec(`delete from participants where person_id=?`, drop); err != nil {
		return err
	}
	if _, err := tx.Exec(
		`update person_merges set kept_id=? where kept_id=?`, keep, drop); err != nil {
		return err
	}
	if _, err := tx.Exec(`
		insert into person_merges (kept_id, dropped_id, dropped_name, reason, merged_at)
		values (?,?,?,?,?)`,
		keep, drop, nullStr(dropName), nullStr(reason), time.Now().Unix()); err != nil {
		return fmt.Errorf("recording merge %d<-%d: %w", keep, drop, err)
	}
	// A name beats an address: the surviving person should read as a human even
	// when the half that survived was first seen as a bare address.
	if strings.Contains(keepName, "@") && dropName != "" && !strings.Contains(dropName, "@") {
		if _, err := tx.Exec(`update people set display_name=? where id=?`, dropName, keep); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`delete from people where id=?`, drop); err != nil {
		return fmt.Errorf("deleting merged person %d: %w", drop, err)
	}
	return tx.Commit()
}

// MergeByEmail merges the person behind dropEmail into the person behind
// keepEmail. This is the shape the cross-domain case arrives in: someone reports
// two addresses that are one human, and neither address implies the other.
// Returns the surviving person id.
func MergeByEmail(s *Store, keepEmail, dropEmail string) (int64, error) {
	keep, err := PersonByIdentity(s, KindEmail, keepEmail)
	if err != nil {
		return 0, err
	}
	drop, err := PersonByIdentity(s, KindEmail, dropEmail)
	if err != nil {
		return 0, err
	}
	if keep == drop {
		return keep, nil // already one person; nothing to record
	}
	if err := mergeWithReason(s, keep, drop, "manual:merge-by-email "+dropEmail); err != nil {
		return 0, err
	}
	return keep, nil
}

// PersonByIdentity looks up an existing identity without creating one.
func PersonByIdentity(s *Store, kind, value string) (int64, error) {
	v, err := NormaliseIdentity(kind, value)
	if err != nil {
		return 0, err
	}
	var id int64
	err = s.db.QueryRow(
		`select person_id from identities where kind=? and value=?`, kind, v).Scan(&id)
	if err == sql.ErrNoRows {
		return 0, fmt.Errorf("no person with %s %s", kind, v)
	}
	return id, err
}

// Participate records that a person took part in an entry in a role. Idempotent,
// so a re-ingest is a no-op.
func Participate(s *Store, entryID, personID int64, role string) error {
	switch role {
	case RoleFrom, RoleTo, RoleCc:
	default:
		return fmt.Errorf("unknown participation role %q", role)
	}
	_, err := s.db.Exec(`
		insert into participants (entry_id, person_id, role) values (?,?,?)
		on conflict(entry_id, person_id, role) do nothing`, entryID, personID, role)
	if err != nil {
		return fmt.Errorf("recording %s participation of %d: %w", role, personID, err)
	}
	return nil
}

// RecordHeader parses one address header and records everyone in it as a
// participant of entryID in the given role, creating people as needed. Returns
// the person ids in header order.
//
// The entry's rows for that role are replaced wholesale, like attachments: the
// header is authoritative, so a corrected recipient list must not leave the old
// recipients behind.
func RecordHeader(s *Store, entryID int64, role, header string) ([]int64, error) {
	switch role {
	case RoleFrom, RoleTo, RoleCc:
	default:
		return nil, fmt.Errorf("unknown participation role %q", role)
	}
	if _, err := s.db.Exec(
		`delete from participants where entry_id=? and role=?`, entryID, role); err != nil {
		return nil, err
	}
	var ids []int64
	for _, a := range ParseAddresses(header) {
		id, err := ResolveAddress(s, a, "mail:"+role+"-header")
		if err != nil {
			// One unusable fragment must not cost us the rest of the header.
			continue
		}
		if err := Participate(s, entryID, id, role); err != nil {
			return ids, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// Participant is one person's involvement in one entry.
type Participant struct {
	PersonID    int64
	DisplayName string
	Role        string
}

// Participants lists everyone involved in an entry, senders first.
func Participants(s *Store, entryID int64) ([]Participant, error) {
	rows, err := s.db.Query(`
		select p.person_id, pe.display_name, p.role
		from participants p join people pe on pe.id = p.person_id
		where p.entry_id = ?
		order by case p.role when 'from' then 0 when 'to' then 1 else 2 end,
		         pe.display_name`, entryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Participant
	for rows.Next() {
		var p Participant
		if err := rows.Scan(&p.PersonID, &p.DisplayName, &p.Role); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// PersonSummary counts what a person did, which is how a recipient-only
// participant becomes visible: Sent 0, Received more than 0.
type PersonSummary struct {
	PersonID    int64
	DisplayName string
	Identities  []string
	Sent        int64
	Received    int64 // to + cc
}

// People lists everyone in the corpus with their identities and counts, ordered
// most-involved first.
func People(s *Store) ([]PersonSummary, error) {
	rows, err := s.db.Query(`
		select pe.id, pe.display_name,
		       (select count(*) from participants x
		         where x.person_id = pe.id and x.role = 'from'),
		       (select count(*) from participants x
		         where x.person_id = pe.id and x.role in ('to','cc'))
		from people pe
		order by 3 desc, 4 desc, pe.display_name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PersonSummary
	for rows.Next() {
		var p PersonSummary
		if err := rows.Scan(&p.PersonID, &p.DisplayName, &p.Sent, &p.Received); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		ids, err := identitiesOf(s, out[i].PersonID)
		if err != nil {
			return nil, err
		}
		out[i].Identities = ids
	}
	return out, nil
}

func identitiesOf(s *Store, personID int64) ([]string, error) {
	rows, err := s.db.Query(
		`select kind, value from identities where person_id=? order by kind, value`, personID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var kind, value string
		if err := rows.Scan(&kind, &value); err != nil {
			return nil, err
		}
		out = append(out, kind+":"+value)
	}
	return out, rows.Err()
}
