package corpus

import (
	"database/sql"
	"errors"
	"fmt"
	"sort"
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

// AliasRepair is what an alias did to the people already ingested, or would do.
type AliasRepair struct {
	From    string
	To      string
	Merged  int
	Refused []AliasRefusal
	Applied bool // false from PreviewDomainAlias, which writes nothing at all
}

// AliasRefusal is a pair an alias matched and would not fold.
//
// Reported and not merely skipped, because an alias that quietly declined half
// its work is indistinguishable from an alias that had none: both print a
// number, and the difference is a duplicate still sitting in the corpus with
// nothing pointing at it. The pair stays two people and `corpus candidates` goes
// on offering it.
type AliasRefusal struct {
	Address  string // the address on the old domain that matched
	KeepID   int64
	KeepName string
	DropID   int64
	DropName string
	Reason   string
}

// AddDomainAlias records an alias and repairs the split it was created to fix.
//
// Doing the repair here rather than leaving it to a separate pass matters,
// because an alias added after ingest is the normal case — you discover the
// rebrand by seeing the duplicate.
func AddDomainAlias(s *Store, from, to, note string) (AliasRepair, error) {
	from, to, err := aliasDomains(from, to)
	if err != nil {
		return AliasRepair{}, err
	}
	if _, err := s.db.Exec(`
		insert into domain_aliases (from_domain, to_domain, note, added_at) values (?,?,?,?)
		on conflict(from_domain) do update set to_domain=excluded.to_domain, note=excluded.note`,
		from, to, nullStr(note), time.Now().Unix()); err != nil {
		return AliasRepair{}, fmt.Errorf("recording alias %s -> %s: %w", from, to, err)
	}

	r := AliasRepair{From: from, To: to, Applied: true}
	merges, refusals, err := planDomainAlias(s, from, to)
	if err != nil {
		return r, err
	}
	r.Refused = refusals
	for _, m := range merges {
		// A previous iteration may already have folded one of these away.
		if err := mergeWithReason(s, m.keep, m.drop,
			"domain-alias:"+from+" ("+m.addr+")"); err != nil {
			if strings.Contains(err.Error(), "no person") {
				continue
			}
			return r, err
		}
		r.Merged++
	}
	return r, nil
}

// PreviewDomainAlias reports what AddDomainAlias would do without recording the
// alias or touching a single person.
//
// A preview exists because this command is the one place a rebrand and an
// identity edit happen in the same breath, and the identity edit cannot be taken
// back: person_merges names the two people and the reason, but not which
// identities, entries or participant rows moved, so nothing in the corpus can
// put a wrongly folded person back. Given that, the only safety available is
// looking first.
//
// It is a flag and not the default, unlike `corpus dedupe`. Dedupe surveys the
// whole corpus on rules the user did not state and can plan dozens of merges
// nobody asked for; an alias is a fact the user has just asserted about two
// domains they named, and refusing to act on it until asked twice would make the
// common case two commands. The cost of that choice is that a wrong -from lands
// before it is seen, which is what the refusal list and this flag are for.
func PreviewDomainAlias(s *Store, from, to string) (AliasRepair, error) {
	from, to, err := aliasDomains(from, to)
	if err != nil {
		return AliasRepair{}, err
	}
	r := AliasRepair{From: from, To: to}
	merges, refusals, err := planDomainAlias(s, from, to)
	if err != nil {
		return r, err
	}
	r.Merged, r.Refused = len(merges), refusals
	return r, nil
}

func aliasDomains(from, to string) (string, string, error) {
	from = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(from, "@")))
	to = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(to, "@")))
	if from == "" || to == "" {
		return "", "", errors.New("alias needs both a from-domain and a to-domain")
	}
	if from == to {
		return "", "", errors.New("an alias from a domain to itself does nothing")
	}
	return from, to, nil
}

type aliasMerge struct {
	keep, drop int64
	addr       string
}

// planDomainAlias decides which of the people an alias matches are one human.
//
// The match is a shared local part across the two domains, which is what the
// alias is claimed to explain and usually does — but a shared local part is not
// proof, and where it is wrong it is wrong in the worst available way: two
// people's correspondence fuses into somebody who reads as entirely ordinary
// afterwards, with no error and no way back. So the local part opens the
// question and the names settle it, on the same evidence `corpus dedupe` uses,
// so the two commands cannot come to opposite conclusions about one pair.
//
// The new domain's person survives. It is the domain still in use, so it is the
// person every later sighting and every Slack profile will go on matching.
//
// A role mailbox is the deliberate exception: it merges without consulting names
// at all. Under an alias the two domains are one organisation — that is the
// whole content of the assertion — and one organisation's support@ is one inbox,
// however many people have sent from it. Names cannot be asked here because a
// role mailbox carries whoever last sent from it, so `support@old` reading
// "Alyssa Salado" against `support@new` reading "Bo Nguyen" would be refused for
// a contradiction between two names that were never claims about the same thing.
// Refusing them instead would also strand them: CanonicalAddress already sends
// every later sighting of support@old to the surviving support@new person, so the
// unmerged half would keep its history and never gain another entry.
func planDomainAlias(s *Store, from, to string) ([]aliasMerge, []AliasRefusal, error) {
	rows, err := s.db.Query(`
		select old.person_id, oldp.display_name, new.person_id, newp.display_name, old.value
		  from identities old
		  join people oldp on oldp.id = old.person_id
		  join identities new
		    on new.kind='email'
		   and new.value = substr(old.value, 1, instr(old.value,'@')) || ?
		  join people newp on newp.id = new.person_id
		 where old.kind='email'
		   and old.value like '%@' || ?
		   and old.person_id <> new.person_id
		 order by old.value`, to, from)
	if err != nil {
		return nil, nil, fmt.Errorf("finding people split by %s: %w", from, err)
	}
	defer rows.Close()

	var merges []aliasMerge
	var refusals []AliasRefusal
	for rows.Next() {
		var m aliasMerge
		var dropName, keepName string
		if err := rows.Scan(&m.drop, &dropName, &m.keep, &keepName, &m.addr); err != nil {
			return nil, nil, err
		}
		local, _, _ := strings.Cut(m.addr, "@")
		if !genericLocalPart(local) {
			if why := namesContradict(keepName, dropName); why != "" {
				refusals = append(refusals, AliasRefusal{
					Address: m.addr, KeepID: m.keep, KeepName: keepName,
					DropID: m.drop, DropName: dropName, Reason: why})
				continue
			}
		}
		merges = append(merges, m)
	}
	return merges, refusals, rows.Err()
}

// namesContradict reports why two display names cannot belong to one human, or
// "" where nothing in them says they cannot.
//
// "Not contradicted" and not "confirmed", which is the only question worth
// asking here: the addresses have already agreed on a local part and the user has
// already asserted the domains are one organisation, so a name is being consulted
// for a veto, not for the case. A name that is absent, or is the address itself,
// vetoes nothing — those are people nobody ever gave a name to, and demanding a
// name from them would leave every bare-address sighting split forever on a
// corpus where names were never captured, which is the ordinary state of mail
// harvested from headers.
//
// The surname test is dedupe's, unchanged and shared. The first-name test is
// here because dedupe never needs it — it groups by first name, so its groups
// agree on one by construction — while an alias groups by local part, and
// `jsmith@old` held by "Jan Smith" against `jsmith@new` held by "Jo Smith" is
// two humans whose surnames agree.
func namesContradict(a, b string) string {
	ap, aok := personNameParts(a)
	bp, bok := personNameParts(b)
	if !aok || !bok {
		return ""
	}
	group := []orgPerson{ap, bp}
	if firsts := distinctFirstNames(group); len(firsts) > 1 {
		return "different first names: " + strings.Join(firsts, ", ")
	}
	if surnames := distinctSurnames(group); len(surnames) > 1 {
		return "different surnames: " + strings.Join(surnames, ", ")
	}
	return ""
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
//
// Two rules feed it. A shared local part across domains is the rebrand shape an
// alias would close. A shared display name is the placeholder shape: a
// participant seen only as a name is a person until someone says which person,
// and RepairTruncatedNames deliberately leaves here every pair its evidence does
// not settle, so a declined merge is visible rather than quietly broken.
func MergeCandidates(s *Store) ([]MergeCandidate, error) {
	var out []MergeCandidate
	at := map[[2]int64]int{}
	add := func(c MergeCandidate) {
		key := [2]int64{c.AID, c.BID}
		if i, seen := at[key]; seen {
			out[i].Reason += "; " + c.Reason
			return
		}
		at[key] = len(out)
		out = append(out, c)
	}

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
	for rows.Next() {
		var c MergeCandidate
		var aAddr, bAddr string
		if err := rows.Scan(&c.AID, &c.BID, &aAddr, &bAddr); err != nil {
			rows.Close()
			return nil, err
		}
		c.AAddresses = []string{aAddr}
		c.BAddresses = []string{bAddr}
		c.Reason = "same local part, different domain"
		_ = s.db.QueryRow(`select display_name from people where id=?`, c.AID).Scan(&c.AName)
		_ = s.db.QueryRow(`select display_name from people where id=?`, c.BID).Scan(&c.BName)
		add(c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	welded, err := WeldedNameCandidates(s)
	if err != nil {
		return nil, err
	}
	for _, c := range welded {
		add(c)
	}

	named, err := sameNamedPeople(s)
	if err != nil {
		return nil, err
	}
	for _, group := range named {
		for i := 0; i < len(group); i++ {
			for j := i + 1; j < len(group); j++ {
				c := MergeCandidate{
					AID: group[i].id, BID: group[j].id,
					AName: group[i].name, BName: group[j].name,
					Reason: "same display name",
				}
				if c.AAddresses, err = emailsOf(s, c.AID); err != nil {
					return nil, err
				}
				if c.BAddresses, err = emailsOf(s, c.BID); err != nil {
					return nil, err
				}
				add(c)
			}
		}
	}
	return out, nil
}

type namedPerson struct {
	id   int64
	name string
}

// sameNamedPeople groups people who share a name, by their display name or by a
// name-only identity, ordered by name and then by id. Names are compared after
// normalisation rather than in SQL so that the grouping matches what resolution
// would key on.
func sameNamedPeople(s *Store) ([][]namedPerson, error) {
	rows, err := s.db.Query(`
		select p.id, p.display_name, p.display_name from people p
		union
		select i.person_id, p.display_name, i.value from identities i
		  join people p on p.id = i.person_id
		 where i.kind = ?
		order by 1`, KindDisplayName)
	if err != nil {
		return nil, fmt.Errorf("finding people who share a name: %w", err)
	}
	defer rows.Close()
	groups := map[string][]namedPerson{}
	seen := map[string]bool{}
	for rows.Next() {
		var p namedPerson
		var value string
		if err := rows.Scan(&p.id, &p.name, &value); err != nil {
			return nil, err
		}
		// An address is nobody's name: a person still known only by the address
		// they were first seen as would otherwise group with every other sighting
		// of it, which the local-part rule already reports far better.
		if strings.Contains(value, "@") {
			continue
		}
		norm, err := NormaliseIdentity(KindDisplayName, value)
		if err != nil {
			continue // a person with no usable name groups with nobody
		}
		key := fmt.Sprintf("%s\x00%d", norm, p.id)
		if seen[key] {
			continue
		}
		seen[key] = true
		groups[norm] = append(groups[norm], p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(groups))
	for name, g := range groups {
		if len(g) > 1 {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	out := make([][]namedPerson, 0, len(names))
	for _, name := range names {
		out = append(out, groups[name])
	}
	return out, nil
}

func emailsOf(s *Store, person int64) ([]string, error) {
	rows, err := s.db.Query(
		`select value from identities where person_id=? and kind=? order by value`,
		person, KindEmail)
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
