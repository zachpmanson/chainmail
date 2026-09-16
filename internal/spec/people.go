package spec

import (
	"database/sql"
	"fmt"
	"net/mail"
	"strings"
	"unicode"

	"github.com/zachpmanson/chainmail/internal/corpus"
)

// addr is one parsed address from a From/To/Cc header.
type addr struct {
	Name    string // display name as it appeared; empty when the header had none
	Address string // lowercased; empty when the header had no parseable address
}

// Who is the name to show: the display name where there was one, else the
// address, which is all we honestly have.
func (a addr) Who() string {
	if a.Name != "" {
		return a.Name
	}
	return a.Address
}

// key identifies a person for de-duplication. The address is the identity where
// there is one — display names vary ("Tom", "Bo Vantel", "tom") while the
// address does not.
func (a addr) key() string {
	if a.Address != "" {
		return "a:" + a.Address
	}
	return "n:" + strings.ToLower(a.Name)
}

// parseAddrList splits an address header. Headers in the wild are not always
// RFC-clean, so a failed parse falls back to splitting on commas outside angle
// brackets and quotes rather than discarding the recipients.
func parseAddrList(header string) []addr {
	header = strings.TrimSpace(header)
	if header == "" {
		return nil
	}
	if list, err := mail.ParseAddressList(header); err == nil {
		out := make([]addr, 0, len(list))
		for _, a := range list {
			out = append(out, addr{Name: strings.TrimSpace(a.Name), Address: strings.ToLower(a.Address)})
		}
		return out
	}
	var out []addr
	for _, part := range splitList(header) {
		if a := parseAddr(part); a != (addr{}) {
			out = append(out, a)
		}
	}
	return out
}

// parseAddr parses a single address, tolerating the unparseable.
func parseAddr(s string) addr {
	s = strings.TrimSpace(s)
	if s == "" {
		return addr{}
	}
	if a, err := mail.ParseAddress(s); err == nil {
		return addr{Name: strings.TrimSpace(a.Name), Address: strings.ToLower(a.Address)}
	}
	if i, j := strings.Index(s, "<"), strings.LastIndex(s, ">"); i >= 0 && j > i {
		return addr{
			Name:    strings.Trim(strings.TrimSpace(s[:i]), `"`),
			Address: strings.ToLower(strings.TrimSpace(s[i+1 : j])),
		}
	}
	if strings.Contains(s, "@") {
		return addr{Address: strings.ToLower(s)}
	}
	return addr{Name: s}
}

// splitList splits on commas that are not inside quotes or angle brackets.
func splitList(s string) []string {
	var out []string
	var cur strings.Builder
	inQuote, depth := false, 0
	for _, r := range s {
		switch {
		case r == '"':
			inQuote = !inQuote
		case r == '<':
			depth++
		case r == '>' && depth > 0:
			depth--
		case r == ',' && !inQuote && depth == 0:
			out = append(out, cur.String())
			cur.Reset()
			continue
		}
		cur.WriteRune(r)
	}
	if strings.TrimSpace(cur.String()) != "" {
		out = append(out, cur.String())
	}
	return out
}

// recipientLine renders To/Cc as the free text the schema asks for, e.g.
// "Ro Laren, Zora Miller, cc Beverly Wells-Rhys". Names appear as the headers
// had them; the cc marker is the renderer's convention.
func recipientLine(to, cc []addr) string {
	names := func(as []addr) []string {
		seen := map[string]bool{}
		var out []string
		for _, a := range as {
			if seen[a.key()] || a.Who() == "" {
				continue
			}
			seen[a.key()] = true
			out = append(out, a.Who())
		}
		return out
	}
	line := strings.Join(names(to), ", ")
	if ccs := names(cc); len(ccs) > 0 {
		if line != "" {
			line += ", "
		}
		line += "cc " + strings.Join(ccs, ", ")
	}
	return line
}

// recipientsOf is the "to …" line for one entry: what the message itself said,
// or — where it has no headers of its own — what the corpus recovered of them.
//
// A recovered entry was found as text inside someone else's message: mail_detail
// has no row for it, so its To and Cc columns are empty and a line read from them
// alone would say "to —" for most of a thread. Its recipients are in the
// participants table, put there by the ingest reading the header block the
// quoting client wrote and unioning them across forwards (AddHeader argues why
// union rather than replacement: each forward shows a different subset). That is
// the message's own statement as it survived, not a guess about it, which is why
// this may fill the line and nothing may invent one.
//
// A recovered entry whose quoters wrote only an attribution has no recipients
// rows either, and still reads "to —". That is the honest answer: nobody said.
func recipientsOf(r *entryRow, part []partRow) string {
	if line := recipientLine(parseAddrList(r.To), parseAddrList(r.Cc)); line != "" {
		return line
	}
	var to, cc []addr
	for _, p := range part {
		switch p.Role {
		case corpus.RoleTo:
			to = append(to, addr{Name: p.Name})
		case corpus.RoleCc:
			cc = append(cc, addr{Name: p.Name})
		}
	}
	return recipientLine(to, cc)
}

// freemail domains say nothing about who someone works for, so they yield no
// org rather than a colour slot named after a mail provider.
var freemail = map[string]bool{
	"gmail.com": true, "googlemail.com": true, "outlook.com": true,
	"hotmail.com": true, "live.com": true, "yahoo.com": true, "yahoo.com.au": true,
	"icloud.com": true, "me.com": true, "mac.com": true, "aol.com": true,
	"msn.com": true, "proton.me": true, "protonmail.com": true,
	"bigpond.com": true, "bigpond.net.au": true, "xtra.co.nz": true,
}

// OrgForDomain names the organisation a mail domain belongs to, and says
// whether that is a decision.
//
// Three answers, and the third is not the second: a label; an empty label
// because the domain is not an organisation (a stored rule saying so); or no
// answer at all. The first two are decisions and stop the search — that is what
// makes "bigpond.com is not an organisation" mean something rather than being
// walked past on the way to the person's other address. The third, no answer,
// lets the caller look elsewhere.
//
// A freemail domain is no answer rather than a decision. Gmail is not an
// organisation and nothing should be coloured "Gmail"; but a person who also
// mails from a work address is still at work, so freemail must not stop the
// search. Only an explicit stored rule can.
//
// The derivation, for a domain nobody has ruled on: trim the public suffix —
// two labels for the .co.nz / .com.au forms, one otherwise — and take the label
// to its left, capitalised. A stored rule overrides it wherever that guess reads
// badly ("mail.acme-group.example" -> "Acme").
//
// rules is the set of decisions in force, not necessarily the stored one: a
// build passes what the reader has saved, and the Ops preview passes that set
// with one proposed change in it, so the consequence of a rule is computed by
// the same resolver that will apply it.
func OrgForDomain(domain string, rules map[string]string) (org string, decided bool) {
	domain = strings.ToLower(strings.TrimSpace(domain))
	if domain == "" {
		return "", false
	}
	if org, ok := rules[domain]; ok {
		return org, true
	}
	if freemail[domain] {
		return "", false
	}
	labels := strings.Split(domain, ".")
	if len(labels) < 2 {
		return "", false
	}
	labels = labels[:len(labels)-1]
	if len(labels) > 1 && twoLevelSuffix[labels[len(labels)-1]] {
		labels = labels[:len(labels)-1]
	}
	return capitalise(labels[len(labels)-1]), true
}

// orgResolver decides a person's organisation, once, for every surface that
// colours a message.
//
// It exists because there are two such surfaces — a built page and the home
// pane's chain read — and the rule has more than one step: this appearance's own
// address, then the first org the person's own mail established, then any other
// address the corpus holds for them. Written twice, the two would eventually
// disagree about one sender, and the colour would mean nothing rather than
// something.
//
// The recording half is what carries an org across a person's entries: their
// direct mail says which organisation they are at, and their quote-recovered
// entries have no address to say it again. It is first-appearance order, so
// someone who changed employer mid-trail is shown at whichever of the two their
// mail resolved to first rather than switching colour partway down — an
// ordering artefact, and the price of being able to read one panel as the key to
// the transcript.
type orgResolver struct {
	rules map[string]string
	// addrs is every address the corpus holds for a person, not just the ones in
	// this selection, so a person's colour does not move when the selection does.
	addrs    map[int64][]string
	byPerson map[int64]string
}

func newOrgResolver(rules map[string]string, addrs map[int64][]string) *orgResolver {
	return &orgResolver{rules: rules, addrs: addrs, byPerson: map[int64]string{}}
}

// note resolves one appearance and records the first org this person's own mail
// establishes. Call it in the order the trail is read in.
func (r *orgResolver) note(person int64, address string) string {
	if _, known := r.byPerson[person]; !known {
		if org, decided := OrgForDomain(corpus.MailDomain(address), r.rules); decided && org != "" {
			r.byPerson[person] = org
		}
	}
	return r.org(person, address)
}

// org resolves one appearance without recording anything.
func (r *orgResolver) org(person int64, address string) string {
	if org, decided := OrgForDomain(corpus.MailDomain(address), r.rules); decided {
		return org
	}
	if org := r.byPerson[person]; org != "" {
		return org
	}
	for _, a := range r.addrs[person] {
		// A rule saying one of the person's addresses is not an organisation is a
		// rule about that address, so the loop keeps looking rather than stopping.
		if org, decided := OrgForDomain(corpus.MailDomain(a), r.rules); decided && org != "" {
			return org
		}
	}
	return ""
}

// twoLevelSuffix lists the generic labels that only ever appear as the middle of
// a two-level public suffix (acme.co.nz, acme.com.au), so they are never taken
// for an organisation name.
var twoLevelSuffix = map[string]bool{
	"co": true, "com": true, "net": true, "org": true, "gov": true,
	"edu": true, "ac": true, "govt": true, "school": true, "asn": true, "id": true,
}

func capitalise(s string) string {
	if s == "" {
		return ""
	}
	r := []rune(s)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

// The participants panel answers "who is in this trail", and a reader checks it
// against the names beside the messages — the name is the only key they have.
// So a row must carry the same name the transcript shows, and every sender must
// have a row: a name visible in the page but absent from the panel reads as
// someone the tool lost, which is the opposite of what a tool for recovering
// people from quoted history is for.
//
// Two facts about the corpus make the header alone an insufficient source:
//
//   - A Slack message has no From header. Its author is only ever a person id.
//   - An entry recovered from quoted text has no mail_detail row at all, so it
//     has neither a From header nor a To/Cc one. Its author is a person id and
//     its recipients exist only as participants rows.
//
// Hence the cast is assembled from the corpus's own participation record and the
// headers together, keyed so that one human is one row however they arrived.

// castRef is the set of identities one appearance is known by, most durable
// first. A corpus person id is the strongest: it survives an address change, a
// rebrand and a display name that varies from one header to the next. An address
// is next. A bare display name is last and weakest — two people called "Sam"
// collapse into one row — and is used only where the trail carried nothing else,
// because listing them under a shared name is still better than omitting one of
// them.
type castRef struct {
	person  int64
	address string
	name    string
	// others are the person's remaining addresses. They are bound to the row so
	// that a header carrying any of them lands on it: on the corpus this was
	// measured against 39 of 363 people hold more than one address, and a row per
	// mailbox is one human listed twice.
	others []string
}

func (r castRef) aliases() []string {
	var out []string
	if r.person != 0 {
		out = append(out, fmt.Sprintf("p:%d", r.person))
	}
	if r.address != "" {
		out = append(out, "a:"+r.address)
	}
	if len(out) == 0 && r.name != "" {
		out = append(out, "n:"+strings.ToLower(r.name))
	}
	return out
}

// cast collects the people a timeline shows, in first-appearance order.
type cast struct {
	order  []string
	rows   map[string]*Participant
	key    map[string]string // alias -> the row key it resolved to
	direct map[string]bool   // row key -> seen at least once outside a quote
}

func newCast() *cast {
	return &cast{rows: map[string]*Participant{}, key: map[string]string{}, direct: map[string]bool{}}
}

// sender records the author of an entry. Their rendered name is authoritative:
// it is what the page shows beside their messages, so a row under any other
// spelling would read as a second person.
func (c *cast) sender(ref castRef, org string, direct bool) {
	c.see(ref, org, true, direct)
}

// recipient records someone addressed on an entry. Their name is whatever
// addressed them — they may have sent nothing here, so there is no rendered
// sender line to prefer.
func (c *cast) recipient(ref castRef, org string, direct bool) {
	c.see(ref, org, false, direct)
}

func (c *cast) see(ref castRef, org string, named, direct bool) {
	if ref.name == "" && ref.address == "" {
		// Neither a name nor an address is not a person, it is an empty header.
		return
	}
	aliases := ref.aliases()
	k := c.resolve(aliases)
	if k == "" {
		return
	}
	row, ok := c.rows[k]
	if !ok {
		row = &Participant{}
		c.rows[k] = row
		c.order = append(c.order, k)
	}
	for _, a := range aliases {
		c.key[a] = k
	}
	for _, a := range ref.others {
		c.bind("a:"+a, k)
	}
	if named || row.Name == "" {
		row.Name = firstNonEmpty(ref.name, ref.address)
	}
	// An address is only ever added, never overwritten: a later sighting knowing
	// less than an earlier one must not take knowledge away.
	if row.Email == "" {
		row.Email = ref.address
	}
	// A sender's org outranks the same person's org as a recipient, and only a
	// sender's: it is the value their bubbles are painted with, and a row coloured
	// otherwise would make the panel disagree with the transcript it is a key to.
	// An empty org never displaces a known one, whoever offers it.
	if org != "" && (named || row.Org == "") {
		row.Org = org
	}
	if direct {
		c.direct[k] = true
	}
}

// bind links an alias to whichever row it belongs to, so that a later appearance
// naming only that alias lands on the same row. It is how a person's other
// addresses are folded in: the corpus knows them all, one header carries one.
func (c *cast) bind(alias, to string) {
	if _, taken := c.key[alias]; !taken {
		if _, ok := c.rows[to]; ok {
			c.key[alias] = to
		}
	}
}

func (c *cast) resolve(aliases []string) string {
	for _, a := range aliases {
		if k, ok := c.key[a]; ok {
			return k
		}
	}
	if len(aliases) > 0 {
		return aliases[0]
	}
	return ""
}

// people returns the cast in first-appearance order.
//
// Someone every sighting of whom came out of quoted text is listed and said to
// be: they are a real participant, and recovering them is much of the point, but
// the evidence for them is one line in someone else's forward rather than a
// message in the mailbox. Omitting them is the defect; listing them unmarked
// would put that weaker evidence on the same footing as a delivered message.
func (c *cast) people() []Participant {
	out := make([]Participant, 0, len(c.order))
	for _, k := range c.order {
		p := *c.rows[k]
		if !c.direct[k] {
			p.Note = "seen only in quoted text"
		}
		out = append(out, p)
	}
	return out
}

// partRow is one participants row, joined to the person it names.
type partRow struct {
	Person int64
	Role   string
	Name   string
}

// loadParticipation reads the corpus's record of who took part in the selected
// entries, plus every address each of those people is known by.
//
// This is the only source for two populations the headers cannot supply: Slack
// authors, who have no From header, and everyone on a quoted entry, which has no
// header row of any kind. Roles are ordered from, to, cc so that first-appearance
// order still reads as the page does.
func loadParticipation(db *sql.DB, ids []int64) (map[int64][]partRow, map[int64][]string, error) {
	ph, args := placeholders(ids)
	rows, err := db.Query(`
		select pa.entry_id, pa.person_id, pa.role, coalesce(pe.display_name, '')
		from participants pa join people pe on pe.id = pa.person_id
		where pa.entry_id in (`+ph+`)
		order by pa.entry_id,
		         case pa.role when 'from' then 0 when 'to' then 1 else 2 end,
		         pe.display_name`, args...)
	if err != nil {
		return nil, nil, fmt.Errorf("loading participation: %w", err)
	}
	defer rows.Close()
	byEntry := map[int64][]partRow{}
	people := map[int64]bool{}
	for rows.Next() {
		var entry int64
		var p partRow
		if err := rows.Scan(&entry, &p.Person, &p.Role, &p.Name); err != nil {
			return nil, nil, err
		}
		byEntry[entry] = append(byEntry[entry], p)
		people[p.Person] = true
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	if len(people) == 0 {
		return byEntry, map[int64][]string{}, nil
	}
	byPerson, err := loadAddresses(db, people)
	if err != nil {
		return nil, nil, err
	}
	return byEntry, byPerson, nil
}

// loadAddresses reads every email address the corpus holds for the given people,
// in a stable order. Values are lowercased: an address here is compared, not
// shown.
//
// A nil people map reads every person the corpus knows, which is what a
// corpus-wide question needs (OrgShiftFor). It must be the corpus's whole record
// rather than the addresses in some selection: this is the fallback a person's
// colour is resolved from, and someone whose only address on one page is a
// personal one is still at work if the corpus holds a work address of theirs.
func loadAddresses(db *sql.DB, people map[int64]bool) (map[int64][]string, error) {
	query := `select person_id, value from identities where kind = 'email'`
	args := []any{}
	if people != nil {
		ph, vals := placeholders(keys(people))
		query += ` and person_id in (` + ph + `)`
		args = vals
	}
	query += ` order by person_id, value`
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("loading addresses: %w", err)
	}
	defer rows.Close()
	byPerson := map[int64][]string{}
	for rows.Next() {
		var id int64
		var v string
		if err := rows.Scan(&id, &v); err != nil {
			return nil, err
		}
		byPerson[id] = append(byPerson[id], strings.ToLower(v))
	}
	return byPerson, rows.Err()
}
