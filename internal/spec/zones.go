package spec

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Zone handling is the one place where a careless join would produce a wrong
// answer that looks right.
//
// The corpus orders by absolute UTC (entries.ts) and keeps entries.tz as the
// *label the source stated* — nothing more. The spec, by contrast, displays the
// sender's own clock, and the renderer marks a zone it inferred differently from
// one that was stated. So:
//
//   - date/time are rendered at the stated zone's offset, never converted to
//     local, UTC, or the reader's zone;
//   - tz is copied through verbatim when the store has one, and omitted when it
//     does not, rather than guessed. The renderer then infers a zone from the
//     sender's other messages and labels it as inferred, which is honest;
//   - a stated label we cannot turn into an offset (an ambiguous or unknown
//     abbreviation) is still emitted as stated, but the clock can only be
//     rendered in UTC — so those entries are listed in sourceNotes instead of
//     being passed off as the sender's local time.
//
// tzOffsets mirrors TZ_OFFSETS in src/lib/chronological.ts so that the offset a
// label implies here is the same one the renderer will use for ordering.
var tzOffsets = map[string]int{
	"AEST": 600, "AEDT": 660, "NZST": 720, "NZDT": 780, "AWST": 480,
	"ACST": 570, "ACDT": 630, "GMT": 0, "UTC": 0, "BST": 60, "CET": 60,
	"CEST": 120, "IST": 330, "PST": -480, "PDT": -420, "EST": -300, "EDT": -240,
}

var reNumericZone = regexp.MustCompile(`^([+-])(\d{2}):?(\d{2})$`)

// tzMinutes returns minutes east of UTC for a zone label, and whether the label
// was recognised at all.
func tzMinutes(tz string) (int, bool) {
	t := strings.TrimSpace(tz)
	if t == "" {
		return 0, false
	}
	if m := reNumericZone.FindStringSubmatch(t); m != nil {
		h, _ := strconv.Atoi(m[2])
		mm, _ := strconv.Atoi(m[3])
		v := h*60 + mm
		if m[1] == "-" {
			v = -v
		}
		return v, true
	}
	off, ok := tzOffsets[strings.ToUpper(t)]
	return off, ok
}

// stamp renders an instant for display in the zone the source stated. resolved
// reports whether the label could be turned into an offset; when it could not,
// the clock shown is UTC and the caller records that as a caveat.
func stamp(ts time.Time, tz string) (date, clock string, resolved bool) {
	off, ok := tzMinutes(tz)
	loc := time.UTC
	if ok {
		loc = time.FixedZone(zoneName(tz), off*60)
	}
	t := ts.In(loc)
	return t.Format("Mon 2 Jan 2006"), t.Format("15:04"), ok
}

// zoneName keeps the stated label on the synthetic location, so a formatted
// value can never silently acquire a different zone's name.
func zoneName(tz string) string { return strings.TrimSpace(tz) }

// spanOf renders the range a thread covers, e.g. "25 Nov 2025 – 20 Aug 2026".
// It works from the dates as displayed rather than from the underlying instants,
// so a span always agrees with the first and last stamps on the page — the two
// differ whenever an entry's stated zone puts it on another calendar day.
func spanOf(fromDate, toDate string) string {
	f, t := trimWeekday(fromDate), trimWeekday(toDate)
	if f == t {
		return f
	}
	return fmt.Sprintf("%s – %s", f, t)
}

// trimWeekday drops the leading day name from "Mon 2 Mar 2026".
func trimWeekday(date string) string {
	if m := reAnchorDate.FindString(date); m != "" {
		return m
	}
	return date
}
