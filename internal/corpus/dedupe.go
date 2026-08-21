package corpus

import (
	"fmt"
	"sort"
	"strings"
)

// Rules a planned merge can come from. The value is carried into
// person_merges.reason, so `corpus dedupe` output and the audit trail name the
// same rule.
const (
	RuleSameName     = "dedupe:same-display-name"
	RuleFirstNameOrg = "dedupe:first-name-and-org"
)

// PlannedMerge is one merge Dedupe would make, with everything a human needs to
// approve it: both people, both sets of identities, and the evidence.
type PlannedMerge struct {
	Rule     string
	KeepID   int64
	KeepName string
	KeepIDs  []string
	DropID   int64
	DropName string
	DropIDs  []string
	Evidence string
}

// Refusal is a group Dedupe would not decide. It is not an error: the whole
// point is that an unresolved duplicate stays two people and stays visible,
// where a wrong merge would look like an ordinary person forever.
type Refusal struct {
	Rule    string
	Subject string // the name, or first-name@organisation, that grouped them
	Reason  string
	People  []int64
}

// DedupePlan is what Dedupe decided, and whether it carried it out.
type DedupePlan struct {
	Merges   []PlannedMerge
	Refusals []Refusal
	Applied  bool
	Before   int // people before
	After    int // people after, projected when Applied is false
}

// Dedupe folds together people two rules can prove are one human, and reports
// every group it could not prove either way.
//
// The plan is computed in full before anything is written, so a dry run and an
// apply of the same corpus produce the same plan by construction: neither rule
// reads state an earlier merge in the same pass would have changed. That is the
// property that makes reviewing dry-run output worth anything.
//
// The two rules are deliberately asymmetric about what counts as proof, because
// the two shapes of duplicate are not alike:
//
//   - A name-only person is corruption. Someone cc'd as "Johan" with no address
//     is a placeholder for a human the corpus already knows by address, so the
//     question is only which one, and RepairTruncatedNames already answers it.
//   - Two people who both hold addresses are two confirmed humans. A shared name
//     is not proof they are one — two colleagues can share a name, and this
//     corpus holds two people whose addresses differ only by the domain of one
//     organisation and who are nonetheless different humans. So the second rule
//     asks for an organisation to be the same, not a name.
func Dedupe(s *Store, apply bool) (DedupePlan, error) {
	var plan DedupePlan
	if err := s.db.QueryRow(`select count(*) from people`).Scan(&plan.Before); err != nil {
		return plan, err
	}

	nameMerges, nameRefusals, err := planPlaceholders(s)
	if err != nil {
		return plan, err
	}
	orgMerges, orgRefusals, err := planFirstNameAndOrg(s)
	if err != nil {
		return plan, err
	}
	plan.Merges = append(nameMerges, orgMerges...)
	plan.Refusals = append(nameRefusals, orgRefusals...)
	plan.After = plan.Before - len(plan.Merges)
	if !apply {
		return plan, nil
	}

	for _, m := range plan.Merges {
		reason := m.Rule
		if m.Evidence != "" {
			reason += " (" + m.Evidence + ")"
		}
		if err := mergeWithReason(s, m.KeepID, m.DropID, reason); err != nil {
			return plan, fmt.Errorf("merging %d into %d: %w", m.DropID, m.KeepID, err)
		}
	}
	plan.Applied = true
	if err := s.db.QueryRow(`select count(*) from people`).Scan(&plan.After); err != nil {
		return plan, err
	}
	return plan, nil
}

// planPlaceholders is RepairTruncatedNames' decision generalised from the values
// a folded header cut off to every person the corpus knows by name alone.
//
// The gates are that repair's, unchanged, and for its reasons: the target must
// hold a machine-derived identity, so a placeholder is folded into a confirmed
// human rather than into another placeholder; and the target must appear on every
// entry the placeholder appears on, which is the fingerprint of a name that lost
// its address — another sighting of the same conversation carried the address.
// Two same-named colleagues do not track each other through a whole corpus.
//
// Nothing here corroborates the target's address against the name, and that is
// on purpose: a header reading `Ainslee Portlock <manager.easterncreek@…>` names
// a shared mailbox that really is hers, and demanding the address look like the
// name would refuse exactly the merges this rule exists for.
func planPlaceholders(s *Store) ([]PlannedMerge, []Refusal, error) {
	holders, err := placeholderPeople(s)
	if err != nil {
		return nil, nil, err
	}
	var merges []PlannedMerge
	var refusals []Refusal
	for _, h := range holders {
		cands, err := peopleNamed(s, h.name, h.id)
		if err != nil {
			return nil, nil, err
		}
		strong, reason, err := strongestTarget(s, h.id, cands)
		if err != nil {
			return nil, nil, err
		}
		if strong == 0 {
			// Nobody else of that name is not a refusal: there is no duplicate to
			// decide about, only a participant the corpus knows by name. Reporting
			// it would bury the groups that do need a decision.
			if len(cands) > 0 {
				refusals = append(refusals, Refusal{
					Rule: RuleSameName, Subject: h.name, Reason: reason,
					People: append([]int64{h.id}, cands...)})
			}
			continue
		}
		m := PlannedMerge{
			Rule: RuleSameName, KeepID: strong, DropID: h.id,
			Evidence: "name-only person, and the kept person is on every entry they are",
		}
		if err := describe(s, &m); err != nil {
			return nil, nil, err
		}
		merges = append(merges, m)
	}
	return merges, refusals, nil
}

type nameOnlyPerson struct {
	id   int64
	name string // normalised
}

// placeholderPeople lists everyone the corpus knows by name and nothing else.
// A person whose name is an address is skipped rather than treated as a name:
// they are a real sighting of a mailbox nobody attached a name to, and grouping
// them by that string would group them with every other sighting of it.
func placeholderPeople(s *Store) ([]nameOnlyPerson, error) {
	rows, err := s.db.Query(`
		select p.id, p.display_name from people p
		 where not exists (select 1 from identities i
		                    where i.person_id = p.id and i.kind in (?,?))
		 order by p.id`, KindEmail, KindSlackUID)
	if err != nil {
		return nil, fmt.Errorf("finding name-only people: %w", err)
	}
	defer rows.Close()
	var out []nameOnlyPerson
	for rows.Next() {
		var p nameOnlyPerson
		var name string
		if err := rows.Scan(&p.id, &name); err != nil {
			return nil, err
		}
		if strings.Contains(name, "@") {
			continue
		}
		norm, err := NormaliseIdentity(KindDisplayName, name)
		if err != nil {
			continue // no usable name groups with nobody
		}
		p.name = norm
		out = append(out, p)
	}
	return out, rows.Err()
}

// planFirstNameAndOrg folds together people with one first name at one
// organisation: the rebrand-and-rename shape, where camille@old and
// camille.cruz@new are one human whose addresses share no local part and whose
// domains share no name.
//
// "Organisation" is the canonical domain — the domain after domain_aliases has
// been applied. Nothing else in the corpus states it: people.org exists but is
// written by nothing, so a domain is all there is, and two domains are one
// organisation only where somebody has said so. Which means this rule cannot see
// a rebrand until `corpus alias` records it, and says so in its refusals rather
// than inferring one. The alternative — treating two domains as one because the
// same first names appear on both — reads a shared payroll into a shared mailing
// list, and the corpus has an "Alyssa" at each of two domains of one
// organisation who are two different people.
//
// Three further gates, because a first name at one organisation is weak on its
// own — two Michaels at one company is unremarkable:
//
//   - the local part must not be a role mailbox (see genericLocalPart), which is
//     what makes info@a and info@b stay two things.
//   - the local part must be consistent with the display name (see
//     addressNamesPerson). Without it, ellen@ carrying "Cy Marrow" in a
//     From header folds an assistant's mailbox into her employer, and sales@
//     carrying the name of whoever last sent from it folds a team into a person.
//   - no two members may carry different surnames. This is the third gate, added
//     rather than assumed: the user asked for first name plus company, and first
//     name plus company alone merges the two Alyssas.
func planFirstNameAndOrg(s *Store) ([]PlannedMerge, []Refusal, error) {
	people, err := orgPeople(s)
	if err != nil {
		return nil, nil, err
	}

	byOrgFirst := map[string][]orgPerson{}
	byFirst := map[string][]orgPerson{}
	for _, p := range people {
		byOrgFirst[p.first+"@"+p.org] = append(byOrgFirst[p.first+"@"+p.org], p)
		byFirst[p.first] = append(byFirst[p.first], p)
	}

	var merges []PlannedMerge
	var refusals []Refusal
	for _, key := range sortedKeys(byOrgFirst) {
		group := byOrgFirst[key]
		if len(group) < 2 {
			continue
		}
		if surnames := distinctSurnames(group); len(surnames) > 1 {
			refusals = append(refusals, Refusal{
				Rule: RuleFirstNameOrg, Subject: key,
				Reason: fmt.Sprintf("%d different surnames share this first name here: %s",
					len(surnames), strings.Join(surnames, ", ")),
				People: idsOf(group)})
			continue
		}
		keep := survivorOf(group)
		for _, p := range group {
			if p.id == keep.id {
				continue
			}
			m := PlannedMerge{
				Rule: RuleFirstNameOrg, KeepID: keep.id, DropID: p.id,
				Evidence: "first name " + p.first + " at " + p.org,
			}
			if err := describe(s, &m); err != nil {
				return nil, nil, err
			}
			merges = append(merges, m)
		}
	}

	// A first name that matches across two organisations is the case this rule
	// was asked for and the one it cannot settle, so it is reported with the
	// command that would settle it. Reported and not merged: nothing here says
	// the two domains belong to one employer, and this is exactly where guessing
	// fuses two people's correspondence.
	for _, first := range sortedKeys(byFirst) {
		group := byFirst[first]
		orgs := distinctOrgs(group)
		if len(orgs) < 2 || len(distinctSurnames(group)) > 1 {
			continue
		}
		reason := "one first name at " + strings.Join(orgs, " and ") +
			"; if those are one organisation, say so with `corpus alias` and this rule folds them"
		if len(orgs) == 2 {
			reason = fmt.Sprintf("one first name at %s and %s; if those are one "+
				"organisation, `corpus alias -from %s -to %s` says so and this rule folds them",
				orgs[0], orgs[1], orgs[0], orgs[1])
		}
		refusals = append(refusals, Refusal{
			Rule: RuleFirstNameOrg, Subject: first, Reason: reason, People: idsOf(group)})
	}
	return merges, refusals, nil
}

// orgPerson is a person reduced to what this rule keys on. A person who does not
// reduce cleanly — no name, no work address, or addresses at two organisations —
// is left out entirely rather than guessed at.
type orgPerson struct {
	id      int64
	name    string // normalised display name
	first   string
	surname string // the rest of the name, normalised; empty when there is only one token
	org     string
	weight  int // participation count, for choosing a survivor
}

func orgPeople(s *Store) ([]orgPerson, error) {
	rows, err := s.db.Query(`
		select p.id, p.display_name,
		       (select count(*) from participants x where x.person_id = p.id)
		  from people p order by p.id`)
	if err != nil {
		return nil, fmt.Errorf("reading people: %w", err)
	}
	type row struct {
		id     int64
		name   string
		weight int
	}
	var all []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.name, &r.weight); err != nil {
			rows.Close()
			return nil, err
		}
		all = append(all, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	var out []orgPerson
	for _, r := range all {
		p, named := personNameParts(r.name)
		if !named {
			continue
		}
		emails, err := emailsOf(s, r.id)
		if err != nil {
			return nil, err
		}
		org := ""
		ok := false
		for _, addr := range emails {
			canon, _, err := CanonicalAddress(s, addr)
			if err != nil {
				return nil, err
			}
			at := strings.LastIndex(canon, "@")
			if at < 0 {
				continue
			}
			local, domain := canon[:at], canon[at+1:]
			if freeMailDomain(domain) || noReplyDomain(domain) {
				continue // neither a webmail host nor a sending host is an employer
			}
			if genericLocalPart(local) || !addressNamesPerson(local, r.name) {
				continue
			}
			if org != "" && org != domain {
				ok = false // two organisations; which one is theirs is unanswerable
				break
			}
			org, ok = domain, true
		}
		if !ok {
			continue
		}
		p.id, p.org, p.weight = r.id, org, r.weight
		out = append(out, p)
	}
	return out, nil
}

// personNameParts splits a display name into the first name and surname the
// rules below compare, reporting false where there is no name to split.
//
// A person named by their own address has no name to take a first name from, and
// tokenising the address yields words that are not anybody's: a plus-addressed
// forwarder splits into a first name and a "surname" assembled out of a domain.
func personNameParts(name string) (orgPerson, bool) {
	if strings.Contains(name, "@") {
		return orgPerson{}, false
	}
	toks := nameTokens(name)
	if len(toks) == 0 {
		return orgPerson{}, false
	}
	p := orgPerson{name: strings.Join(toks, " "), first: toks[0]}
	if len(toks) > 1 {
		p.surname = strings.Join(toks[1:], " ")
	}
	return p, true
}

// distinctFirstNames lists the first names present in a group, sorted. Every
// member has one — personNameParts admits nobody without — so unlike surnames
// there is no silent member here, and two entries are always a contradiction.
func distinctFirstNames(group []orgPerson) []string {
	seen := map[string]bool{}
	var out []string
	for _, p := range group {
		if seen[p.first] {
			continue
		}
		seen[p.first] = true
		out = append(out, p.first)
	}
	sort.Strings(out)
	return out
}

// distinctSurnames lists the surnames present in a group, sorted. Members with
// only a first name contribute none: "Camille" does not contradict "Camille
// Cruz", where "Camille Nguyen" does.
func distinctSurnames(group []orgPerson) []string {
	seen := map[string]bool{}
	var out []string
	for _, p := range group {
		if p.surname == "" || seen[p.surname] {
			continue
		}
		seen[p.surname] = true
		out = append(out, p.surname)
	}
	sort.Strings(out)
	return out
}

func distinctOrgs(group []orgPerson) []string {
	seen := map[string]bool{}
	var out []string
	for _, p := range group {
		if seen[p.org] {
			continue
		}
		seen[p.org] = true
		out = append(out, p.org)
	}
	sort.Strings(out)
	return out
}

// survivorOf picks the most-involved member, then the lowest id. Involvement
// rather than id because the survivor is the person every later sighting will go
// on matching, and the busiest account is the one still in use.
func survivorOf(group []orgPerson) orgPerson {
	best := group[0]
	for _, p := range group[1:] {
		if p.weight > best.weight || (p.weight == best.weight && p.id < best.id) {
			best = p
		}
	}
	return best
}

func idsOf(group []orgPerson) []int64 {
	out := make([]int64, 0, len(group))
	for _, p := range group {
		out = append(out, p.id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// describe fills in the names and identities of both halves of a merge, which is
// the whole of what a reviewer has to go on.
func describe(s *Store, m *PlannedMerge) error {
	if err := s.db.QueryRow(`select display_name from people where id=?`, m.KeepID).
		Scan(&m.KeepName); err != nil {
		return err
	}
	if err := s.db.QueryRow(`select display_name from people where id=?`, m.DropID).
		Scan(&m.DropName); err != nil {
		return err
	}
	var err error
	if m.KeepIDs, err = identitiesOf(s, m.KeepID); err != nil {
		return err
	}
	m.DropIDs, err = identitiesOf(s, m.DropID)
	return err
}

// genericLocalParts is the denylist of role mailboxes. Every entry is a mailbox
// an organisation owns rather than a person, so two of them at two domains are
// two mailboxes and never one human.
//
// A denylist and not a heuristic. The tempting alternative — asking whether a
// local part looks like a person's name — is wrong in the direction that hurts:
// it has to accept Grace, Frank, Art and Sky, which are words as well as names,
// and it accepts "sales-team" and "customer.care" as readily as "bo.vantel"
// because they are the same shape. A missed merge leaves two people who are
// visibly two people and reachable from `corpus candidates`; a wrong merge fuses
// two correspondences into an entity that looks entirely ordinary. So the list is
// written down, reviewable in a diff, and grows when somebody names what it let
// through.
var genericLocalParts = map[string]bool{
	"abuse": true, "accounting": true, "accounts": true, "admin": true,
	"administration": true, "alerts": true, "all": true, "automation": true,
	"automations": true, "billing": true, "bookings": true, "bot": true,
	"bounce": true, "bounces": true, "care": true, "careers": true,
	"contact": true, "contactus": true, "customerservice": true, "dev": true,
	"devnull": true, "donotreply": true, "do-not-reply": true, "enquiries": true,
	"enquiry": true, "everyone": true, "feedback": true, "finance": true,
	"help": true, "helpdesk": true, "hello": true, "hi": true, "hr": true,
	"info": true, "inquiries": true, "invoice": true, "invoices": true,
	"jobs": true, "legal": true, "mail": true, "mailer": true, "mailer-daemon": true,
	"manager": true, "marketing": true, "news": true, "newsletter": true,
	"no-reply": true, "noreply": true, "notification": true, "notifications": true,
	"office": true, "orders": true, "payments": true, "payroll": true,
	"postmaster": true, "privacy": true, "reception": true, "recruitment": true,
	"general": true, "inbox": true, "mailbox": true, "procurement": true,
	"pricing": true, "quotes": true, "reply": true, "rfp": true, "root": true,
	"sales": true, "security": true, "service": true, "staff": true,
	"subscriptions": true, "support": true, "system": true, "team": true,
	"tender": true, "tenders": true, "webmaster": true,
}

// genericLocalPart reports whether a local part names a role rather than a
// person. The whole local part is checked, and then each of its separated words
// — so `sales-team`, `corporatesales.support` and `noreply+123` all land — but a
// word is only matched at three letters or more, so the two-letter entries
// (hr, hi) cannot fire from inside a name like `l.mccorley`.
func genericLocalPart(local string) bool {
	local = strings.ToLower(strings.TrimSpace(local))
	if i := strings.Index(local, "+"); i > 0 {
		local = local[:i]
	}
	if genericLocalParts[local] {
		return true
	}
	for _, w := range strings.FieldsFunc(local, func(r rune) bool {
		return r == '.' || r == '-' || r == '_'
	}) {
		if len(w) >= 3 && genericLocalParts[w] {
			return true
		}
	}
	return false
}

// freeMailDomains are webmail and consumer-ISP hosts. They are not employers, so
// two people sharing a first name on one of them share nothing: keying an
// organisation on gmail.com would make every Michael with a Gmail account one
// Michael.
var freeMailDomains = map[string]bool{
	"aol.com": true, "bigpond.com": true, "bigpond.net.au": true, "gmail.com": true,
	"googlemail.com": true, "gmx.com": true, "gmx.net": true, "hotmail.co.nz": true,
	"hotmail.co.uk": true, "hotmail.com": true, "hotmail.com.au": true,
	"icloud.com": true, "iinet.net.au": true, "internode.on.net": true,
	"live.com": true, "live.com.au": true, "mac.com": true, "me.com": true,
	"msn.com": true, "optusnet.com.au": true, "outlook.co.nz": true,
	"outlook.com": true, "outlook.com.au": true, "proton.me": true,
	"protonmail.com": true, "tpg.com.au": true, "xtra.co.nz": true,
	"yahoo.co.nz": true, "yahoo.com": true, "yahoo.com.au": true, "ymail.com": true,
}

func freeMailDomain(domain string) bool {
	return freeMailDomains[strings.ToLower(strings.TrimSpace(domain))]
}

// noReplyLabels are the subdomains transactional mail is sent from. A domain
// under one is a sending host, not an employer: `ghsa-…@noreply.github.com`
// carries a project name where a display name goes, and attributing that name to
// an organisation would offer to alias github into whoever it notified.
var noReplyLabels = map[string]bool{
	"bounce": true, "bounces": true, "donotreply": true, "do-not-reply": true,
	"no-reply": true, "noreply": true,
}

func noReplyDomain(domain string) bool {
	label, _, found := strings.Cut(strings.ToLower(strings.TrimSpace(domain)), ".")
	return found && noReplyLabels[label]
}

// nameTokens reduces a display name to its name words, lowercased.
//
// The name is cut at a parenthesis or a pipe first, because what follows one is
// an annotation and not part of anybody's name — "Paul Stevens (Customer
// Support)" and "Klara Belk | Arvida". Keeping the annotation would let
// support@ corroborate the first and arvida@ the second, which is the wrong
// direction for a gate whose job is to disqualify.
func nameTokens(name string) []string {
	if i := strings.IndexAny(name, "(|"); i >= 0 {
		name = name[:i]
	}
	var out []string
	for _, w := range strings.FieldsFunc(strings.ToLower(name), func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= 'A' && r <= 'Z') &&
			!(r >= '0' && r <= '9') && r != '\''
	}) {
		w = strings.ReplaceAll(w, "'", "")
		if len(w) >= 2 {
			out = append(out, w)
		}
	}
	return out
}

// addressNamesPerson reports whether a local part is consistent with a display
// name. It is the gate that keeps a person's name on somebody else's mailbox
// from being read as that person's second address: a From header saying
// `Cy Marrow <ellen@…>` is a real thing mail does, and the name in it is
// no evidence at all about whose mailbox that is.
//
// Two forms count. A name word of three letters or more appearing anywhere in the
// local part covers tom, rob.harrington and zachpmanson. An initial followed by
// the start of another name word covers aking and tdempst, with four letters of
// the word required so that a two-letter remainder cannot match half the corpus.
//
// It answers "not contradicted", not "belongs to": clin@ for "Clin" passes on the
// same evidence a stranger's clinic@ would. That is why it is one gate of three
// and never the only one.
func addressNamesPerson(local, name string) bool {
	squashed := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			return r
		}
		if r >= 'A' && r <= 'Z' {
			return r + ('a' - 'A')
		}
		return -1
	}, local)
	if squashed == "" {
		return false
	}
	toks := nameTokens(name)
	for _, t := range toks {
		if len(t) >= 3 && strings.Contains(squashed, t) {
			return true
		}
	}
	for _, initial := range toks {
		if squashed[0] != initial[0] {
			continue
		}
		rest := squashed[1:]
		if len(rest) < 4 {
			continue
		}
		for _, t := range toks {
			if t != initial && strings.HasPrefix(t, rest) {
				return true
			}
		}
	}
	return false
}
