package corpus

import (
	"database/sql"
	"errors"
	"fmt"
	"net/mail"
	"regexp"
	"strconv"
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
// quoted display names containing commas ("Vantel, Bo" <b@x>) correctly. It
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
	for _, frag := range rejoinFoldedAddresses(splitOutsideQuotes(header)) {
		if a, ok := ParseAddress(frag); ok {
			out = append(out, a)
		}
	}
	return out
}

// rejoinFoldedAddresses undoes a split made through the middle of one address.
//
// A recipient list recovered from quoted text arrives with its folds flattened,
// and the flattening marks a fold with the same comma that separates two
// recipients — so a list that wrapped between a display name and the address it
// introduces reaches here as `Name <` followed by `addr>`. Split there, the
// address is lost outright: the leading half degrades to a name-only participant
// and stands as a second person for a human whose address was on the very next
// fragment, and no later sighting can supply what was dropped.
//
// The join is made only when the next fragment carries an address, so a display
// name that genuinely ends in a bracket cannot swallow the recipient behind it.
func rejoinFoldedAddresses(frags []string) []string {
	out := make([]string, 0, len(frags))
	for i := 0; i < len(frags); i++ {
		f := frags[i]
		if strings.HasSuffix(f, "<") && i+1 < len(frags) &&
			reHeaderEmail.MatchString(frags[i+1]) {
			f += frags[i+1]
			i++
		}
		out = append(out, f)
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
	// Bracket matching is wrong for the hyperlinked form and must not see it: see
	// mailtoAddress. A fragment it cannot reduce falls through and degrades as any
	// other unparseable one does.
	if hasMailtoLink(frag) {
		if name, addr, ok := mailtoAddress(frag); ok {
			return Address{Addr: addr, Name: name, Raw: frag}, true
		}
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
		name := CleanDisplayName(frag[:i])
		if addr != "" {
			return Address{Addr: addr, Name: name, Raw: frag}, true
		}
		if name != "" {
			return Address{Name: name, Raw: frag}, true
		}
		return Address{}, false
	} else if i >= 0 {
		// The same fold CleanDisplayName copes with, cut a few characters later: the
		// closing bracket went and the address it introduced did not. Reading it
		// here is what keeps `Name <addr` from being stored whole as a display
		// name — a second person for a human whose address is sitting inside their
		// own name, unreachable by every later sighting of that address.
		if addr := reHeaderEmail.FindString(frag[i+1:]); addr != "" {
			return Address{
				Addr: strings.ToLower(addr),
				Name: CleanDisplayName(frag[:i]),
				Raw:  frag,
			}, true
		}
	}
	if strings.Contains(frag, "@") && !strings.ContainsAny(frag, " \t") {
		return Address{Addr: strings.ToLower(frag), Raw: frag}, true
	}
	// A fragment of nothing but brackets is the one thing worth dropping: it names
	// nobody, so there is no participant in it to lose.
	if name := CleanDisplayName(frag); name != "" {
		return Address{Name: name, Raw: frag}, true
	}
	return Address{}, false
}

// CleanDisplayName strips what a header fragment leaves around a display name:
// the quotes it may be written in, and the `<` left behind where the address that
// followed it was lost to a fold. A `<` anywhere but the end is kept, because a
// name is the whole of a name-only participant's evidence and one written with a
// bracket in it is still that person.
func CleanDisplayName(name string) string {
	// Twice around the quotes, because either can wrap the other: a fragment may
	// be written `"Dai Rhys" <` or, once a quoted value has been re-quoted, `"Dai
	// Rhys <"`.
	for range 2 {
		name = strings.TrimRight(strings.TrimSpace(name), " <")
		name = strings.Trim(name, `"'`)
	}
	return strings.TrimSpace(name)
}

// reHeaderEmail matches an address-shaped run inside a header fragment. It
// decides only whether a fragment is address-shaped enough for the hyperlinked
// and folded paths to act on it, so a domain with no dot (an intranet address)
// staying unmatched costs nothing: such a fragment is left to bracket matching.
var reHeaderEmail = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)

// hasMailtoLink reports whether a fragment carries a mailto: link. The colon is
// required, so a display name containing the word "mailto" is not diverted into
// mailtoAddress and keeps its brackets parsed normally.
func hasMailtoLink(frag string) bool {
	return strings.Contains(strings.ToLower(frag), "mailto:")
}

// mailtoAddress reduces Outlook's hyperlinked rendering of an address to the
// address itself and the name in front of it. In plain-text mail Outlook writes
// a linked address as `Name <addr <mailto:addr>>` — sometimes with no space
// before the inner bracket, and sometimes with the closers lost to a line wrap —
// so the FIRST address in the fragment is the real one. Bracket matching takes
// the span between the first `<` and the last `>` instead, which makes the
// address `addr <mailto:addr>`: one human then arrives as several people, and
// none of the malformed halves can ever match a Slack profile email.
//
// ok is false when the fragment holds no address, or holds two that differ. A
// fragment that does not reduce to exactly one address is left alone and
// reported rather than guessed at, because the wrong guess here is a merge of
// two humans and merges are what this file exists to keep honest.
//
// internal/unnest.parsePerson makes the same reduction for attribution
// sentinels. The duplication is deliberate: that side parses free prose and this
// side parses a header, the two share no types, and a package holding one regex
// in common would tie the extraction half of the pipeline to the storage half
// for the sake of four lines.
func mailtoAddress(frag string) (name, addr string, ok bool) {
	ms := reHeaderEmail.FindAllStringIndex(frag, -1)
	if len(ms) == 0 {
		return "", "", false
	}
	addr = strings.ToLower(frag[ms[0][0]:ms[0][1]])
	for _, m := range ms[1:] {
		if !strings.EqualFold(frag[m[0]:m[1]], addr) {
			return "", "", false
		}
	}
	// Whatever precedes the address is the display name, minus the punctuation
	// that introduced the address and minus the scheme itself when the fragment
	// began with the link.
	name = strings.Trim(strings.TrimSpace(frag[:ms[0][0]]), ` <>([,:"'`)
	if strings.EqualFold(name, "mailto") || strings.EqualFold(name, addr) {
		name = ""
	}
	return strings.TrimSpace(name), addr, true
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

// plusBaseAddress reduces an address carrying an RFC 5233 subaddress to the
// mailbox it is delivered to, and returns the tag separately. Everything from
// the first `+` to the `@` is a detail the recipient chose — which signup, which
// vendor — so `zachpmanson+salsa@gmail.com` and `zachpmanson@gmail.com` are one
// mailbox and therefore one person.
//
// ok is false where there is no tag to take off, and where taking it off would
// be a guess:
//
//   - a `+` in the domain is not a subaddress, so only the local part is read.
//   - a local part that is nothing but a tag (`+x@d`) leaves no mailbox behind.
//   - a tag encoding another mailbox is not a tag (see plusTagEncodesMailbox).
//
// The tag is returned rather than discarded because callers keep it: the corpus
// is asked which signup produced a message, and an address reduced at parse time
// can never answer that.
func plusBaseAddress(addr string) (base, tag string, ok bool) {
	at := strings.LastIndex(addr, "@")
	if at < 0 {
		return "", "", false
	}
	local, domain := addr[:at], addr[at+1:]
	plus := strings.Index(local, "+")
	if plus <= 0 {
		return "", "", false
	}
	tag = local[plus+1:]
	if plusTagEncodesMailbox(tag) {
		return "", "", false
	}
	return local[:plus] + "@" + domain, tag, true
}

// hasSubaddress reports whether an address is shaped like a subaddress at all,
// whatever plusBaseAddress then decides about it. It is what separates "there
// was nothing here to reduce" from "this one was refused", so a refusal can be
// reported and a domain holding a `+` is not.
func hasSubaddress(addr string) bool {
	at := strings.LastIndex(addr, "@")
	return at >= 0 && strings.Contains(addr[:at], "+")
}

// rePlusEncodedMailbox matches an address written with `=` where the `@` goes,
// which is how a forwarder encodes one mailbox inside another's tag.
var rePlusEncodedMailbox = regexp.MustCompile(`[A-Za-z0-9._%+\-]+=[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)

// plusTagEncodesMailbox reports whether a tag names a mailbox instead of
// describing one. Gmail's forwarding artefact `zach+caf_=ellen=example.com@…`
// is not a subaddress of zach's mailbox and not a person at all: the tag says
// which account the mail was forwarded for, so reducing it would file somebody
// else's mail under zach.
//
// Two signals, because either alone leaves a hole: `caf_` is the marker Gmail
// writes, and the encoded form catches the same shape from any other forwarder
// or bounce handler that spells an address with `=`.
func plusTagEncodesMailbox(tag string) bool {
	t := strings.ToLower(tag)
	return strings.HasPrefix(t, "caf_") || rePlusEncodedMailbox.MatchString(t)
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
	if kind == KindEmail {
		if base, _, ok := plusBaseAddress(v); ok {
			return resolveSubaddressed(s, v, base, displayName, rule)
		}
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

// resolveSubaddressed resolves a tagged address to the person holding the base
// mailbox and records the tagged form as a second identity of theirs.
//
// Both notions are kept on purpose. Normalising the tag away at parse time is
// simpler and would need none of this, but the tag is evidence — it says which
// signup or which vendor produced the message — and a corpus that drops it can
// never be asked what arrived via +salsa. So the base address is the identity of
// record, because it is what a Slack profile and a correctly-quoted header will
// match, and the tagged address hangs off the same person as a spelling of it.
//
// A tagged address already belonging to somebody else is left where it is and
// this still answers with the base mailbox's person: the mailbox is theirs, and
// re-pointing an identity is a merge, which RepairPlusAddresses decides and
// records rather than a resolution doing it silently.
func resolveSubaddressed(s *Store, tagged, base, displayName, rule string) (int64, error) {
	id, err := ResolveWithRule(s, KindEmail, base, displayName, rule)
	if err != nil {
		return 0, err
	}
	var owner int64
	err = s.db.QueryRow(`select person_id from identities where kind=? and value=?`,
		KindEmail, tagged).Scan(&owner)
	switch {
	case err == nil:
		return id, nil
	case err != sql.ErrNoRows:
		return 0, fmt.Errorf("looking up %s: %w", tagged, err)
	}
	if _, err := s.db.Exec(
		`insert into identities (person_id, kind, value, rule) values (?,?,?,?)`,
		id, KindEmail, tagged, rule+":subaddress"); err != nil {
		return 0, fmt.Errorf("recording subaddress %s: %w", tagged, err)
	}
	return id, nil
}

// ResolveAddress resolves a parsed header address: by address when there is one,
// and by display name when there is not. A name-only participant deliberately
// becomes a real person rather than being skipped — someone cc'd as "Johan" is a
// participant, and leaving them out is what made recipients invisible before.
func ResolveAddress(s *Store, a Address, rule string) (int64, error) {
	if a.Addr != "" {
		// An address the corpus already holds as an identity resolves on that
		// identity, alias or no alias. Almost always this is the same answer the
		// alias would give — a folded pair leaves the old address pointing at the
		// survivor — but it differs in the one case that matters: where an alias
		// matched two people and refused to fold them, because their names say
		// they are two humans. Canonicalising there would send every later sighting
		// of the old address to the OTHER person and quietly orphan this one, so
		// the refusal would repair nothing and corrupt attribution from then on.
		literal, err := NormaliseIdentity(KindEmail, a.Addr)
		if err != nil {
			return 0, err
		}
		var known int64
		switch err := s.db.QueryRow(`select person_id from identities where kind=? and value=?`,
			KindEmail, literal).Scan(&known); {
		case err == nil:
			return ResolveWithRule(s, KindEmail, literal, a.Name, rule)
		case err != sql.ErrNoRows:
			return 0, fmt.Errorf("looking up %s: %w", literal, err)
		}
		// Otherwise apply any configured domain alias, so an account on a
		// pre-rebrand domain resolves to the same person as the current one rather
		// than creating a second. The alias, where one applied, replaces the rule so
		// the reason stays traceable.
		canon, aliasRule, err := CanonicalAddress(s, a.Addr)
		if err != nil {
			return 0, err
		}
		if aliasRule != "" {
			rule = aliasRule
		}
		return ResolveWithRule(s, KindEmail, canon, a.Name, rule)
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
// execer is the smallest writer an identity write needs, satisfied by both
// *sql.DB and *sql.Tx — AddAlias takes the first, and a multi-part edit takes a
// transaction so it either happens whole or not at all.
type execer interface {
	Exec(query string, args ...any) (sql.Result, error)
	QueryRow(query string, args ...any) *sql.Row
}

func AddAlias(s *Store, personID int64, kind, value, rule string) error {
	return addAlias(s.db, personID, kind, value, rule)
}

// addAlias is AddAlias against either handle: one identity pointed at one
// person, refusing to take one another person holds.
func addAlias(q execer, personID int64, kind, value, rule string) error {
	v, err := NormaliseIdentity(kind, value)
	if err != nil {
		return err
	}
	if rule == "" {
		rule = "manual:alias"
	}
	var owner int64
	err = q.QueryRow(
		`select person_id from identities where kind=? and value=?`, kind, v).Scan(&owner)
	switch {
	case err == nil && owner == personID:
		_, err := q.Exec(
			`update identities set rule=? where kind=? and value=?`, rule, kind, v)
		return err
	case err == nil:
		var held string
		if err := q.QueryRow(`select display_name from people where id=?`, owner).Scan(&held); err != nil {
			return err
		}
		return &IdentityTakenError{Identity: kind + ":" + v, PersonID: owner, Name: held}
	case err != sql.ErrNoRows:
		return err
	}
	var exists int
	if err := q.QueryRow(`select count(*) from people where id=?`, personID).
		Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		return fmt.Errorf("no person %d", personID)
	}
	if _, err := q.Exec(
		`insert into identities (person_id, kind, value, rule) values (?,?,?,?)`,
		personID, kind, v, rule); err != nil {
		return fmt.Errorf("adding alias %s %s: %w", kind, v, err)
	}
	return nil
} // Merge folds drop into keep: every identity and every reference is repointed,
// then the emptied person row is deleted. All of it in one transaction, because a
// half-merged person is worse than either an unmerged or a merged one — it is a
// person with references to a row that no longer exists.
//
// No identity is lost: the dropped person's addresses still resolve, they just
// resolve to keep, and person_merges says when and why they moved. The same goes
// for the one setting that names a person — the reader's own (SettingMePerson) —
// so a merge cannot leave the reader reading as nobody.
//
// A merge is not reversible, and callers should not treat person_merges as
// though it were. The row names the two people, the dropped display name and the
// reason; it does not name which identities, entries or participant rows moved,
// and the participant rows that collided with keep's own were deleted rather
// than moved. Nothing left in the corpus distinguishes an identity keep always
// had from one it inherited, so splitting them apart again is guesswork. What
// the trail is for is telling a human that a merge happened and on what
// evidence, so a wrong one can be found and fixed by hand — which is why the
// callers that decide merges automatically are the ones that owe a preview.
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
	// The reader's own person follows the merge for the same reason their
	// identities do: the setting names a row, and this takes that row away. Left
	// pointing at the emptied id, the reader would read as nobody and their own
	// mail would stop being marked — on a merge they may well have run to fold the
	// two halves of their own address together.
	if _, err := tx.Exec(`update settings set value=? where key=? and value=?`,
		strconv.FormatInt(keep, 10), SettingMePerson, strconv.FormatInt(drop, 10)); err != nil {
		return fmt.Errorf("repointing the reader's own person: %w", err)
	}
	// The render-offset measurements — whose client rendered a quoted clock —
	// are facts about the same human, so they follow the merge just like
	// participation, with the same collision handling: repoint what does not
	// collide, and drop what does. A merge that skipped this table would die on
	// the foreign key when the emptied person row was deleted.
	if _, err := tx.Exec(
		`update or ignore render_offsets set person_id=? where person_id=?`, keep, drop); err != nil {
		return fmt.Errorf("repointing render offsets of %d: %w", drop, err)
	}
	if _, err := tx.Exec(`delete from render_offsets where person_id=?`, drop); err != nil {
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

// PeopleForAddresses resolves addresses to the people the corpus has decided
// they belong to.
//
// This is the identity graph answering a question about a human rather than
// about a string. A reader who names one of their addresses names the person
// every address of theirs was merged into — `zachpmanson@gmail.com` and
// `zachpmanson+salsa@gmail.com` are one mailbox and so one person here — and
// that is what lets a statement about a reader's own mail be true of all of it
// rather than of the one address they happened to type.
//
// An address the corpus has never seen — a new work address, a typo — is absent
// from the result rather than an error. The caller is naming their own
// addresses; one of them not being in the corpus yet is a fact about the corpus,
// not a mistake by the reader.
func PeopleForAddresses(s *Store, addrs []string) (map[int64]bool, error) {
	out := map[int64]bool{}
	seen := map[string]bool{}
	vals := make([]string, 0, len(addrs))
	for _, a := range addrs {
		v, err := NormaliseIdentity(KindEmail, a)
		if err != nil || seen[v] {
			continue
		}
		seen[v] = true
		vals = append(vals, v)
	}
	if len(vals) == 0 {
		return out, nil
	}
	ph, args := placeholders(vals)
	rows, err := s.db.Query(
		`select person_id from identities where kind=? and value in (`+ph+`)`,
		append([]any{KindEmail}, args...)...)
	if err != nil {
		return nil, fmt.Errorf("resolving addresses to people: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// ErrNoPerson is returned when an id names nobody in `people`. Its own error
// rather than a message, because the callers that meet it have to decide between
// a refusal and an absence — a settings write is a 400, a setting that reads back
// as nobody is not an error at all (see SetMePerson, MePerson).
var ErrNoPerson = errors.New("no person with that id")

// personMailbox reads a person the corpus holds: their name, and the mailboxes
// they are known by, sorted so two reads of one person agree.
//
// An id the corpus does not hold is ErrNoPerson rather than an empty answer, so no
// caller can read "there is no such person" and "this person has no address" as
// the same fact: the first is a mistake to report and the second is a person
// nobody could have received mail from.
func personMailbox(s *Store, id int64) (name string, emails []string, err error) {
	if err := s.db.QueryRow(`select display_name from people where id=?`, id).Scan(&name); err != nil {
		if err == sql.ErrNoRows {
			return "", nil, fmt.Errorf("%w: %d", ErrNoPerson, id)
		}
		return "", nil, err
	}
	emails, err = emailsOf(s, id)
	return name, emails, err
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

// AddHeader records participants from a header WITHOUT clearing the role first.
//
// RecordHeader replaces a role wholesale, which is right for a real message: its
// header is complete, so a re-ingest should mirror it exactly. It is wrong for a
// message recovered from quoted text, where each forward shows a different
// subset of the recipients — replacing would let the narrowest copy seen last
// decide who was involved. Here the union across sightings is the better answer.
func AddHeader(s *Store, entryID int64, role, header string) ([]int64, error) {
	switch role {
	case RoleFrom, RoleTo, RoleCc:
	default:
		return nil, fmt.Errorf("unknown participation role %q", role)
	}
	var ids []int64
	for _, a := range ParseAddresses(header) {
		id, err := ResolveAddress(s, a, "quote:"+role+"-header")
		if err != nil {
			continue
		}
		if err := Participate(s, entryID, id, role); err != nil {
			return ids, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// PersonEdit is one hand-made change to a person's setup: a name to write, and
// identities to attach or detach. It is what the ops screen's own editor sends,
// and it is deliberately not a merge — merging is a decision about two people
// with evidence behind it (see Dedupe), while this is a reader correcting one.
type PersonEdit struct {
	// DisplayName, when not empty, becomes the person's name. The identities the
	// name was read from are kept: a name is evidence like any other, and the
	// screen's edit is a statement about what to CALL someone, not about which
	// spellings they have been written as.
	DisplayName string
	// Add and Remove are identities as "kind:value" — the form identitiesOf
	// returns and the wire carries, e.g. "email:ada@example.com".
	Add    []string
	Remove []string
}

// ErrBadIdentity is an identity string that is not one: not "kind:value", an
// unknown kind, or a value that normalises to nothing. It is the caller's own
// mistake, which is why the API turns it into a 400 rather than a conflict.
var ErrBadIdentity = errors.New("not an identity")

// opsRule marks an identity a human wrote on the ops screen, so a reader asking
// why two people hold one address can tell a hand from a rule.
const opsRule = "ops:manual"

// IdentityTakenError is an edit that would move an identity onto a person who
// does not hold it. Addresses are the corpus's own finding, and a screen that
// could silently reassign one would be a screen that can undo a merge by
// accident. Nothing here merges: the reader is told whose it is and can say so
// on the merge plan, where the evidence is.
type IdentityTakenError struct {
	Identity string
	PersonID int64
	Name     string
}

func (e *IdentityTakenError) Error() string {
	return fmt.Sprintf("%s belongs to %s (#%d) — merge instead: moving an address "+
		"between people is the same act as merging them, and the merge plan is where "+
		"that act has evidence behind it", e.Identity, e.Name, e.PersonID)
}

// UpdatePerson applies one PersonEdit to one person, in a transaction, and
// answers with the person as it now stands.
//
// Identities are normalised on the way in (NormaliseIdentity), so the screen's
// box cannot create a second spelling of an address the corpus already holds,
// and every identity it writes is marked with the rule that put it there —
// opsRule — because the next person to ask why two people hold one address
// should be able to see that a human said so.
func UpdatePerson(s *Store, id int64, edit PersonEdit) (PersonSummary, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return PersonSummary{}, err
	}
	defer tx.Rollback()

	var name string
	if err := tx.QueryRow(`select display_name from people where id = ?`, id).Scan(&name); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return PersonSummary{}, fmt.Errorf("%w: no person #%d", ErrNoPerson, id)
		}
		return PersonSummary{}, err
	}

	if want := strings.TrimSpace(edit.DisplayName); want != "" && want != name {
		if _, err := tx.Exec(`update people set display_name = ? where id = ?`, want, id); err != nil {
			return PersonSummary{}, err
		}
		// And as an identity, so the name is a thing the corpus resolves and
		// folds by rather than only a label on a row. Its rule is this screen's,
		// so the name is traceable to the hand that wrote it.
		if err := addAlias(tx, id, KindDisplayName, want, opsRule); err != nil {
			return PersonSummary{}, err
		}
	}

	for _, raw := range edit.Add {
		kind, value, err := splitIdentity(raw)
		if err != nil {
			return PersonSummary{}, err
		}
		if err := addAlias(tx, id, kind, value, opsRule); err != nil {
			return PersonSummary{}, err
		}
	}
	for _, raw := range edit.Remove {
		kind, value, err := splitIdentity(raw)
		if err != nil {
			return PersonSummary{}, err
		}
		if _, err := tx.Exec(
			`delete from identities where person_id = ? and kind = ? and value = ?`,
			id, kind, value); err != nil {
			return PersonSummary{}, err
		}
	}

	if err := tx.Commit(); err != nil {
		return PersonSummary{}, err
	}
	return PersonByID(s, id)
}

// splitIdentity reads one "kind:value" identity, normalised.
func splitIdentity(raw string) (string, string, error) {
	kind, value, ok := strings.Cut(strings.TrimSpace(raw), ":")
	if !ok {
		return "", "", fmt.Errorf("%w: %q must be written kind:value, e.g. email:ada@example.com",
			ErrBadIdentity, raw)
	}
	kind = strings.TrimSpace(kind)
	value = strings.TrimSpace(value)
	norm, err := NormaliseIdentity(kind, value)
	if err != nil {
		return "", "", fmt.Errorf("%w: %v", ErrBadIdentity, err)
	}
	return kind, norm, nil
}

// PersonByID is one person as People() reports them, for a screen that has just
// changed one and needs to show what it now says.
func PersonByID(s *Store, id int64) (PersonSummary, error) {
	var p PersonSummary
	err := s.db.QueryRow(`
		select pe.id, pe.display_name,
		       (select count(*) from participants x where x.person_id = pe.id and x.role = 'from'),
		       (select count(*) from participants x where x.person_id = pe.id and x.role in ('to','cc'))
		from people pe where pe.id = ?`, id).
		Scan(&p.PersonID, &p.DisplayName, &p.Sent, &p.Received)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return p, fmt.Errorf("%w: no person #%d", ErrNoPerson, id)
		}
		return p, err
	}
	if p.Identities, err = identitiesOf(s, id); err != nil {
		return p, err
	}
	return p, nil
}
