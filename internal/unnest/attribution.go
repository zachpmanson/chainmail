package unnest

import (
	"regexp"
	"strings"
	"time"
)

// Attribution is the sender and send-time recovered from an attribution
// sentinel, e.g. "On Tue, 3 Feb 2026 at 07:29, Alice <a@ex.com> wrote:".
//
// Every field is best-effort. A sentinel carries whatever the quoting client
// chose to write, so a missing address or an unparsed date is a normal outcome
// and not an error: the block is still real and still worth an entry. Callers
// must therefore check rather than assume — which is why Sent is a time.Time
// whose zero value means "not stated", not a string that lies by looking valid.
type Attribution struct {
	// Sender is the display name as written, "" when the sentinel gave only an
	// address.
	Sender string
	// Address is the email address, "" when the sentinel gave only a name.
	Address string
	// Sent is the stated send time, in UTC with no offset applied — the sentinel
	// states a wall clock and usually not the zone it belongs to. Zero when no
	// date could be parsed.
	Sent time.Time
	// TZ is the zone label as stated ("NZST"), "" when absent. Kept as text
	// because a label alone does not determine an offset, and guessing one would
	// silently move a message across a date boundary.
	TZ            string
	DatePrecision string
	DateReason    string
}

func (a Attribution) DateString() string {
	if a.Sent.IsZero() {
		return ""
	}
	return a.Sent.Format("2006-01-02")
}

func (a Attribution) ClockString() string {
	if a.Sent.IsZero() {
		return ""
	}
	return a.Sent.Format("15:04")
}

var reEmail = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)

// ParseAttribution recovers a sender independently of date success.
func ParseAttribution(sentinel string) Attribution {
	s := stripOpener(trimClosers(foldSpaces(sentinel)))
	d, rest, pivot := splitDate(s, false)
	a := Attribution{Sent: d.Sent, TZ: d.TZ, DatePrecision: d.Precision, DateReason: d.Reason}
	if !pivot {
		rest = s
	}
	a.Sender, a.Address = parsePerson(rest)
	return a
}

// SplitWhen retains the existing clock-pivot API. ok means a structural clock
// token was found, NOT that a complete date parsed. Use ParseDate for diagnostics
// or to validate a whole header value; Sent remains zero on every failed parse.
func SplitWhen(s string) (sent time.Time, tz, rest string, ok bool) {
	d, rest, ok := splitDate(s, false)
	return d.Sent, d.TZ, rest, ok
}

// parsePerson splits the trailing "Name <addr>" region.
//
// Outlook renders a hyperlinked address as `Name <addr <mailto:addr>>`, so the
// first address found is the real one and the mailto: repeat must not become the
// answer — taking the last bracketed span would capture "mailto:..." verbatim.
func parsePerson(s string) (name, addr string) {
	s = strings.TrimSpace(strings.Trim(strings.TrimSpace(s), ",:"))
	if m := reEmail.FindStringIndex(s); m != nil {
		addr = s[m[0]:m[1]]
		// The name is whatever precedes the address, minus the bracket that
		// introduced it.
		name = strings.TrimSpace(s[:m[0]])
		name = strings.TrimRight(name, " <([,")
		name = strings.TrimSpace(name)
		if strings.EqualFold(name, "mailto") || name == addr {
			name = ""
		}
	} else {
		name = s
	}
	name = strings.Trim(name, " \"'")
	// A bare address with no display name must not be echoed as the name.
	if name == addr {
		name = ""
	}
	return name, addr
}

func trimClosers(s string) string {
	l := strings.ToLower(s)
	best := -1
	for _, c := range closers {
		if i := strings.LastIndex(l, c); i > best {
			best = i
		}
	}
	if best >= 0 {
		return strings.TrimSpace(s[:best])
	}
	return s
}
