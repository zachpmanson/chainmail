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
	TZ string
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

var (
	// The time is the pivot of the whole parse: in every observed dialect the date
	// precedes it and the person follows it. Splitting there means the date
	// formats and the name formats never have to be disambiguated against each
	// other — which is what makes a US "Aug 12, 2026" and a UK "12 Aug 2026"
	// separable at all, since "Mar 14, 2025, 6:12" has a comma where en-GB has
	// the word "at".
	reClock = regexp.MustCompile(`(?i)\b(\d{1,2}):(\d{2})(?::(\d{2}))?\s*(am|pm)?`)
	// A zone label immediately after the clock. Deliberately 2-5 upper-case
	// letters or a numeric offset: lower-case words here are prose ("at", "by").
	reZone  = regexp.MustCompile(`^[\s,]*([A-Z]{2,5}|[+-]\d{4})\b`)
	reEmail = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)
	// Trailing closers, and the leading opener word, are stripped before parsing.
	reOpenerHead = regexp.MustCompile(`(?i)^\s*(on|le|el|il|em|am|op|den|på|pe|w dniu|dnia|vào|在)\b[\s,]*`)

	// Date layouts, tried in order. Both day-first and month-first appear in this
	// corpus, so an ambiguous "03/02" would be unresolvable — no numeric-only
	// layout is listed, and a sentinel written that way yields no date rather
	// than a wrong one.
	dateLayouts = []string{
		"Mon, 2 Jan 2006", "Mon, 2 January 2006",
		"Monday, 2 January 2006", "Monday, 2 Jan 2006",
		"2 Jan 2006", "2 January 2006",
		"Mon, Jan 2, 2006", "Mon, January 2, 2006",
		"Monday, January 2, 2006", "Monday, Jan 2, 2006",
		"Jan 2, 2006", "January 2, 2006",
		"Mon 2 Jan 2006", "Mon 2 January 2006",
	}
	reDateJoiner = regexp.MustCompile(`(?i)[\s,]+(at|kl|à|um|alle|as)$`)
	clockLayouts = []string{"15:04:05", "15:04", "3:04:05 PM", "3:04 PM", "3:04PM"}
)

// ParseAttribution recovers the sender and send-time from an attribution
// sentinel. It never fails: an unrecognised sentinel yields a zero Attribution.
func ParseAttribution(sentinel string) Attribution {
	s := strings.Join(strings.Fields(sentinel), " ")
	s = trimClosers(s)
	s = reOpenerHead.ReplaceAllString(s, "")

	sent, tz, rest, ok := SplitWhen(s)
	a := Attribution{TZ: tz, Sent: sent}
	if !ok {
		// No clock, so the pivot is gone. The person may still be recoverable.
		a.Sender, a.Address = parsePerson(s)
		return a
	}
	a.Sender, a.Address = parsePerson(rest)
	return a
}

// SplitWhen finds the date and time at the head of s, returning them plus
// whatever followed. Shared with header-block parsing, where a Date: value is
// the same text in the same dialects with no person after it.
//
// The clock is the pivot: the date precedes it and anything else follows.
func SplitWhen(s string) (sent time.Time, tz, rest string, ok bool) {
	loc := reClock.FindStringIndex(s)
	if loc == nil {
		return time.Time{}, "", s, false
	}
	datePart := strings.TrimRight(strings.TrimSpace(s[:loc[0]]), " ,")
	// "at" / "kl" / "à" sit between the date and the clock in most dialects.
	datePart = reDateJoiner.ReplaceAllString(datePart, "")
	datePart = strings.TrimRight(strings.TrimSpace(datePart), " ,")

	rest = s[loc[1]:]
	if z := reZone.FindStringSubmatch(rest); z != nil {
		tz = z[1]
		rest = rest[len(z[0]):]
	}
	return combine(datePart, s[loc[0]:loc[1]]), tz, rest, true
}

// combine parses the date and clock halves and merges them. The result carries
// no offset: see Attribution.Sent.
func combine(datePart, clockPart string) time.Time {
	datePart = normaliseDate(datePart)
	var d time.Time
	for _, l := range dateLayouts {
		if t, err := time.Parse(l, datePart); err == nil {
			d = t
			break
		}
	}
	if d.IsZero() {
		return time.Time{}
	}
	clockPart = strings.ToUpper(strings.Join(strings.Fields(clockPart), " "))
	for _, l := range clockLayouts {
		if t, err := time.Parse(l, clockPart); err == nil {
			return time.Date(d.Year(), d.Month(), d.Day(),
				t.Hour(), t.Minute(), t.Second(), 0, time.UTC)
		}
	}
	// A date with an unreadable clock is still a date worth keeping.
	return d
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

// normaliseDate repairs spellings Go's time package will not accept.
//
// "Sept" is the single largest cause of unparsed dates in this corpus — 340 of
// 385 — because Gmail's en-GB locale writes the four-letter abbreviation while
// the reference layout "Jan" accepts only three. A trailing dot after the day
// ("1. Oct 2025") is the Nordic and German form.
func normaliseDate(s string) string {
	s = reSept.ReplaceAllString(s, "Sep")

	s = reDayDot.ReplaceAllString(s, "$1$2")
	return s
}

var (
	reSept = regexp.MustCompile(`(?i)\bSept\b`)
	// No lookahead: RE2 has none, so the following space is captured and put back.
	reDayDot = regexp.MustCompile(`\b(\d{1,2})\.(\s)`)
)
