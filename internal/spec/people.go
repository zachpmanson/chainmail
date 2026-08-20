package spec

import (
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
// colour, and the mail domain is the only org evidence the corpus holds — there
// is no org column on people — so it is derived from the domain: the label to
// the left of any public suffix, capitalised. Options.Orgs overrides it wherever
// that guess reads badly ("mail.acme-group.example" -> "Acme").
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
