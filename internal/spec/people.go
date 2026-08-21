package spec

import (
	"database/sql"
	"fmt"
	"net/mail"
	"strings"
	"unicode"
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

// freemail domains say nothing about who someone works for, so they yield no
// org rather than a colour slot named after a mail provider.
var freemail = map[string]bool{
	"gmail.com": true, "googlemail.com": true, "outlook.com": true,
	"hotmail.com": true, "live.com": true, "yahoo.com": true, "yahoo.com.au": true,
	"icloud.com": true, "me.com": true, "mac.com": true, "aol.com": true,
	"msn.com": true, "proton.me": true, "protonmail.com": true,
	"bigpond.com": true, "bigpond.net.au": true, "xtra.co.nz": true,
}

// orgOf names the organisation behind an address. `org` drives nothing but
// colour and the panel's grouping, and the mail domain is the only org evidence
// the corpus holds — people.org exists but nothing populates it — so it is
// derived from the domain: the label to the left of any public suffix,
// capitalised. Options.Orgs overrides it wherever that guess reads badly
// ("mail.acme-group.example" -> "Acme").
func orgOf(address string, overrides map[string]string) string {
	i := strings.LastIndex(address, "@")
	if i < 0 {
		return ""
	}
	domain := strings.ToLower(address[i+1:])
	if org, ok := overrides[domain]; ok {
		return org
	}
	if freemail[domain] {
		return ""
	}
	// Trim the public suffix — two labels for the .co.nz / .com.au forms, one
	// otherwise — and take the label immediately to its left as the org name.
	labels := strings.Split(domain, ".")
	if len(labels) < 2 {
		return ""
	}
	labels = labels[:len(labels)-1]
	if len(labels) > 1 && twoLevelSuffix[labels[len(labels)-1]] {
		labels = labels[:len(labels)-1]
	}
	return capitalise(labels[len(labels)-1])
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
	ph, args = placeholders(keys(people))
	addrs, err := db.Query(`
		select person_id, value from identities
		where kind = 'email' and person_id in (`+ph+`)
		order by person_id, value`, args...)
	if err != nil {
		return nil, nil, fmt.Errorf("loading the addresses of the cast: %w", err)
	}
	defer addrs.Close()
	byPerson := map[int64][]string{}
	for addrs.Next() {
		var id int64
		var v string
		if err := addrs.Scan(&id, &v); err != nil {
			return nil, nil, err
		}
		byPerson[id] = append(byPerson[id], strings.ToLower(v))
	}
	return byEntry, byPerson, addrs.Err()
}
