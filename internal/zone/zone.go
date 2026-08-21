// Package zone answers one question: what offset does a stated zone imply, and
// when is there no honest answer.
//
// It exists so that the renderer's ordering, the spec's clocks and the
// quoted-date checks all read the same label the same way. A label resolved to
// +1000 in one place and left unresolved in another would put the same message on
// two different calendar days.
package zone

import (
	"regexp"
	"strconv"
	"strings"
)

// Min and Max bound the civil offsets in use anywhere on earth: Baker Island at
// -12:00 through Kiritimati at +14:00.
//
// They are the width of "the zone was not stated". Nothing narrower can be
// claimed about an unlabelled wall clock, so any assertion about the order of two
// such clocks has to survive the whole 26 hours between them.
const (
	Min = -12 * 60
	Max = 14 * 60
)

// offsets is the labels this corpus states, as minutes east of UTC.
//
// A label is not an offset: an abbreviation is chosen by someone's mail client,
// several are ambiguous between zones, and a stated one can simply be wrong
// about daylight saving. The table is therefore the labels we are willing to
// resolve, not every label in existence — an unlisted one yields nothing rather
// than a guess.
//
// Mirrors TZ_OFFSETS in src/lib/chronological.ts so that the offset a label
// implies here is the one the renderer will order by.
var offsets = map[string]int{
	"AEST": 600, "AEDT": 660, "NZST": 720, "NZDT": 780, "AWST": 480,
	"ACST": 570, "ACDT": 630, "GMT": 0, "UTC": 0, "BST": 60, "CET": 60,
	"CEST": 120, "IST": 330, "PST": -480, "PDT": -420, "EST": -300, "EDT": -240,
}

var reNumeric = regexp.MustCompile(`^([+-])(\d{2}):?(\d{2})$`)

// Minutes returns minutes east of UTC for a zone label, and whether the label
// was recognised at all. A numeric offset is read as itself; anything else must
// be in the table.
func Minutes(tz string) (int, bool) {
	t := strings.TrimSpace(tz)
	if t == "" {
		return 0, false
	}
	if m := reNumeric.FindStringSubmatch(t); m != nil {
		h, _ := strconv.Atoi(m[2])
		mm, _ := strconv.Atoi(m[3])
		v := h*60 + mm
		if m[1] == "-" {
			v = -v
		}
		return v, true
	}
	off, ok := offsets[strings.ToUpper(t)]
	return off, ok
}
