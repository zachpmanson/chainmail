// Package tzinfer works out what a stated wall clock means when the source did
// not say which zone it belongs to.
//
// A mailbox message carries a Date header, so its offset is known exactly. A
// message recovered from quoted text carries only what an attribution sentinel
// wrote — "On Tue, 14 Apr 2026 at 16:35, Tom wrote:" — and most sentinels omit
// the zone. Two thirds of the recovered entries in this corpus are in that
// state, and a page that shows their clocks with no zone at all is a page whose
// times cannot be compared with each other.
//
// The inference has two halves, in two files. This one places people: from the
// instants where someone's own client stated an offset, which zone were they
// keeping. resolve.go then uses those zones to read the unlabelled clocks, under
// the constraint that a quoted message was sent before the message quoting it.
//
// An IANA zone rather than an offset, because the 24 people here with more than
// one observed offset are mostly not travellers: AEDT is +1100 and AEST is
// +1000 and they are one person in one place, six months apart. Storing the
// offset that was observed and applying it to another month is wrong by an hour
// every time, and silently — the clock still looks like a clock. A zone
// evaluated at the target instant is right on both sides of the transition,
// which is what time/tzdata is embedded for.
package tzinfer

import (
	"fmt"
	"sort"
	"strings"
	"time"

	// Embedded so the answer is the same on a machine with no system zoneinfo,
	// and so the tests have one answer everywhere.
	_ "time/tzdata"
)

// Observation is one instant at which a person's own client stated its offset.
// At is the true instant, from a Date header; Off is minutes east of UTC.
type Observation struct {
	At  time.Time
	Off int
}

// Verdict is how far the evidence about one person goes.
type Verdict int

const (
	// NoEvidence: this person never stated an offset anywhere in the corpus.
	// Nothing can be inferred for them and nothing is.
	NoEvidence Verdict = iota
	// UTCOnly: this person has never stated anything but +0000, which is no
	// evidence of place at all — see withoutBareUTC. One Exchange sender in this
	// corpus stamps UTC on its own mail while rendering other people's at +1200.
	// A genuine UTC-zone sender loses their inference; the alternative labels a
	// New Zealander as British, and 42 of the 137 people with any evidence at all
	// are in this state.
	UTCOnly
	// Moved: the observations are real but no single zone explains them, so this
	// person changed place rather than merely crossing a daylight-saving
	// boundary. Their offsets stay as candidates for resolve.go to choose
	// between; none is preferred over the others, because "most common" is a
	// tiebreak dressed as evidence and is wrong for exactly the people who
	// travel most.
	Moved
	// Placed: at least one zone reproduces every observation. The offset at any
	// other instant follows from the zone.
	Placed
)

func (v Verdict) String() string {
	switch v {
	case UTCOnly:
		return "utc-only"
	case Moved:
		return "moved"
	case Placed:
		return "placed"
	}
	return "no-evidence"
}

// Place is the conclusion about one person's clock.
type Place struct {
	Verdict Verdict
	// Zones are the IANA names that reproduce every observation, in candidates
	// order. More than one is the normal case for a single observation —
	// Australia/Sydney and Australia/Brisbane are indistinguishable in July — and
	// they diverge only at a daylight-saving boundary.
	Zones []string
	// Seen are the distinct observed offsets, ascending. This is the candidate
	// set when Verdict is Moved.
	Seen []int
	// Obs is how many observations the verdict rests on, after the ones that say
	// nothing about place have been dropped.
	Obs int
	// Bare is how many observations were dropped for stating +0000 and nothing
	// else. It is the whole of the evidence when Verdict is UTCOnly, so the
	// caveat can say how much UTC was seen rather than how much was believed.
	Bare int
}

// Candidates returns the offsets this person's clock could have shown over the
// window [from, to], best first, and whether the answer is single-valued.
//
// A window rather than an instant because the caller holds a wall clock rather
// than an instant: an unlabelled clock places the instant only to within the
// width of the world's offsets, and a daylight-saving transition can fall inside
// that width. Both sides of the transition are returned so the ordering
// constraints can reject the wrong one, rather than this function picking.
func (p Place) Candidates(from, to time.Time) []int {
	switch p.Verdict {
	case Placed:
		var out []int
		for _, name := range p.Zones {
			loc, err := time.LoadLocation(name)
			if err != nil {
				continue
			}
			for _, t := range []time.Time{from, to} {
				_, secs := t.In(loc).Zone()
				out = appendUnique(out, secs/60)
			}
		}
		return out
	case Moved:
		return append([]int(nil), p.Seen...)
	}
	return nil
}

// Why says what the conclusion rests on, as a clause that follows "someone
// who". One phrasing for all four verdicts, because the same sentence carries an
// inference's evidence and an unknown's excuse, and a reader needs to weigh
// both the same way.
func (p Place) Why() string {
	switch p.Verdict {
	case Placed:
		return fmt.Sprintf("is placed in %s by %s", p.Zones[0], plural(p.Obs, "stated offset"))
	case Moved:
		return fmt.Sprintf("has stated %s, which no single zone explains", offsetList(p.Seen))
	case UTCOnly:
		return fmt.Sprintf("has only ever stated +0000, across %s, which is what a client "+
			"emits when it does not know its zone", plural(p.Bare, "message"))
	}
	return "has never stated an offset"
}

// candidates are the zones a person may be placed in, most prevalent in this
// corpus first.
//
// A curated list rather than the whole tz database, for two reasons. Most of the
// database is aliases and pre-1970 history that no mail client emits, and the
// order is a tiebreak that has to be defensible: a single July observation of
// +1000 fits both Sydney and Brisbane, and this corpus is centred on Sydney, so
// Sydney is the answer where nothing distinguishes them. That is a judgement
// about this mailbox, stated here rather than buried in a sort.
//
// The fit test does most of the work regardless — a +1000 observation in
// January cannot be Sydney, so Brisbane wins there without any tiebreak. An
// offset no listed zone reproduces yields no placement rather than the nearest
// listed one, which is why the list has to cover every offset the corpus
// actually contains.
var candidates = []string{
	"Australia/Sydney", "America/Los_Angeles", "Asia/Singapore", "Asia/Dhaka",
	"Europe/London", "Asia/Karachi", "Asia/Kolkata", "Pacific/Auckland",
	"Australia/Brisbane", "Australia/Perth", "Australia/Adelaide",
	"Australia/Darwin", "Asia/Kathmandu", "Asia/Colombo", "Asia/Manila",
	"Asia/Shanghai", "Asia/Tokyo", "Asia/Jakarta", "Asia/Bangkok", "Asia/Dubai",
	"Asia/Jerusalem", "Europe/Berlin", "Europe/Lisbon", "Europe/Moscow", "UTC",
	"Africa/Nairobi", "Africa/Johannesburg", "Africa/Lagos",
	"America/Denver", "America/Phoenix", "America/Chicago", "America/New_York",
	"America/Bogota", "America/Sao_Paulo", "America/Halifax", "America/Anchorage",
	"Pacific/Honolulu", "Pacific/Fiji", "Pacific/Chatham",
}

// Fit places one person from their observations.
func Fit(obs []Observation) Place {
	p := Place{Obs: len(obs)}
	if len(obs) == 0 {
		return p
	}
	kept := withoutBareUTC(obs)
	p.Obs, p.Bare = len(kept), len(obs)-len(kept)
	if len(kept) == 0 {
		p.Verdict = UTCOnly
		return p
	}
	obs = kept
	for _, o := range obs {
		p.Seen = appendUnique(p.Seen, o.Off)
	}
	sort.Ints(p.Seen)
	for _, name := range candidates {
		loc, err := time.LoadLocation(name)
		if err != nil {
			// A zone the embedded database does not know is a typo in the list
			// above, not a fact about the data, so it is skipped rather than
			// failing the whole placement.
			continue
		}
		if explains(loc, obs) {
			p.Zones = append(p.Zones, name)
		}
	}
	if len(p.Zones) > 0 {
		p.Verdict = Placed
	} else {
		p.Verdict = Moved
	}
	return p
}

// withoutBareUTC drops the +0000 observations of anyone who has stated anything
// else, and returns nothing for anyone who has not.
//
// +0000 is the value a client emits when it does not know where it is, and it
// arrives interleaved rather than in a block: one Sydney sender here stamps UTC
// on nine messages spread over fourteen months while stamping +1000 and +1100
// throughout the same period. Nobody commutes to London nine times without once
// producing a London summer offset, so those nine are a device and not a place —
// and left in the evidence they make the sender unplaceable, which costs the
// inference every message they ever quoted.
//
// Applied to everyone, not only to that sender. A genuine Europe/London account
// keeps its +0100 observations and is still placed by them; one that has only
// ever stated +0000 becomes UTCOnly, which is what it already was. What breaks
// under the alternative — trusting a bare +0000 as a place — is that 42 people
// here are put in Britain, and 5 more become travellers who never travelled.
func withoutBareUTC(obs []Observation) []Observation {
	out := make([]Observation, 0, len(obs))
	for _, o := range obs {
		if o.Off != 0 {
			out = append(out, o)
		}
	}
	return out
}

// explains reports whether loc shows the stated offset at every observed
// instant. Every observation must hold: one counterexample means this is not
// where the person was, and daylight saving is the whole reason a zone can
// satisfy two different offsets without any special case for it.
func explains(loc *time.Location, obs []Observation) bool {
	for _, o := range obs {
		_, secs := o.At.In(loc).Zone()
		if secs/60 != o.Off {
			return false
		}
	}
	return true
}

// FormatOffset writes minutes east of UTC as a Date-header zone, e.g. "+0545".
func FormatOffset(mins int) string {
	sign := "+"
	if mins < 0 {
		sign, mins = "-", -mins
	}
	return fmt.Sprintf("%s%02d%02d", sign, mins/60, mins%60)
}

func offsetList(mins []int) string {
	out := make([]string, len(mins))
	for i, m := range mins {
		out[i] = FormatOffset(m)
	}
	switch len(out) {
	case 1:
		return out[0]
	case 2:
		return out[0] + " and " + out[1]
	}
	return strings.Join(out[:len(out)-1], ", ") + " and " + out[len(out)-1]
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

func appendUnique(xs []int, x int) []int {
	for _, v := range xs {
		if v == x {
			return xs
		}
	}
	return append(xs, x)
}
