package corpus

import "strings"

// MailDomain is the mail domain of an address, taken as it appears: a bare
// address, a header form (`Ada Okoye <ada@okoye.example>`), or a quoted one.
//
// It is not `canonicalDomain`. That one applies the configured domain aliases,
// because it decides whether two notices came from one organisation and a
// rebrand halves the evidence for it. This one answers "what does the address
// say", which is what the reader is looking at when they group domains into an
// organisation: an alias is about merging people, and a colour is not a merger.
//
// Lowercased and trimmed, and empty for anything it cannot read an address out
// of — including a display name with no address, which resolves to a person but
// belongs to no domain.
func MailDomain(from string) string {
	a, ok := ParseAddress(from)
	if !ok || a.Addr == "" {
		return ""
	}
	i := strings.LastIndex(a.Addr, "@")
	if i < 0 {
		return ""
	}
	return strings.ToLower(strings.Trim(a.Addr[i+1:], "<> \t"))
}
