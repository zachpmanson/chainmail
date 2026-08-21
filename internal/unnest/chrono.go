package unnest

import (
	"sort"
	"time"

	"github.com/zachpmanson/chainmail/internal/zone"
)

// Stamp is what one block states about when it was sent.
//
// Wall is a wall clock and nothing more — a sentinel writes "3 Feb 2026 at
// 07:29" and usually not the zone that clock belongs to, so the instant behind
// it is only known to within the width of the world's civil offsets. Off is that
// zone when it is known, in minutes east of UTC; nil means unknown, which is the
// common case and not an error.
//
// A zero Wall means the sentinel stated no date at all. Such a block has no
// place in a chronology and Chrono says so rather than choosing one for it.
type Stamp struct {
	Wall time.Time
	Off  *int
}

// Chronology is the reading of a body's blocks by stated date.
type Chronology struct {
	// Order is the dated blocks by index, newest stated first — the same
	// direction Peel returns, so the two listings are comparable at a glance.
	Order []int
	// Undated is the blocks that stated no date, in nesting order. They are
	// carried rather than dropped: a recovered message exists nowhere else, and a
	// block that cannot be placed is still a block that was found.
	Undated []int
	// Moved counts the dated blocks whose position by date differs from their
	// position by nesting. Nonzero is the honest headline "these two orders
	// disagree", and says nothing about whether the disagreement is suspicious.
	Moved int
	// Impossible is the inversions no zone difference can account for, given that
	// a body reads newest-first. Empty is the normal outcome and the one worth
	// staying quiet about.
	Impossible []Inversion
}

// Inversion is a quoted block that cannot have been sent when it says it was.
//
// Outer quotes Inner — every block Peel returns after another was nested inside
// it — so Inner was sent first, always. Excess is how far past the outer block's
// latest possible instant the inner block's earliest possible instant falls: the
// amount by which the two stated dates contradict each other after every
// allowance for an unstated zone has already been made.
type Inversion struct {
	Outer, Inner int
	Excess       time.Duration
}

// Chrono orders blocks by the date they state and names the orderings that
// cannot be true.
//
// presumed is the offset to read an unlabelled wall clock at, in minutes east of
// UTC — in practice the offset of the message the blocks were peeled out of,
// which is the only real offset a body carries. It affects the ORDER only.
// Presuming a zone is fine for deciding what to print first and never fine for
// asserting that a date is wrong, so Impossible ignores it and works from the
// full [zone.Min, zone.Max] range instead. The alternative — presuming the same
// zone for both purposes — would report a Sydney reply to a London mail as
// impossible ten times a day.
func Chrono(stamps []Stamp, presumed int) Chronology {
	var c Chronology
	for i, s := range stamps {
		if s.Wall.IsZero() {
			c.Undated = append(c.Undated, i)
			continue
		}
		c.Order = append(c.Order, i)
	}

	// Stable, so blocks stating the same minute — a forward and the message it
	// forwards, quoted at one-minute resolution — keep their nesting order.
	sort.SliceStable(c.Order, func(a, b int) bool {
		return instant(stamps[c.Order[a]], presumed).After(instant(stamps[c.Order[b]], presumed))
	})
	c.Moved = countMoved(c.Order)
	c.Impossible = impossible(stamps)
	return c
}

// countMoved compares the date order against nesting order, which for the dated
// blocks is the same slice sorted ascending by index.
func countMoved(order []int) int {
	sorted := make([]int, len(order))
	copy(sorted, order)
	sort.Ints(sorted)
	n := 0
	for i := range order {
		if order[i] != sorted[i] {
			n++
		}
	}
	return n
}

// impossible finds, for each block, the strongest contradiction between its
// stated date and the date of a block that quotes it.
//
// Every earlier block quotes every later one: Peel emits a body outermost first,
// so block 3 was nested inside block 1 as surely as inside block 2. All such
// pairs are therefore candidates, and the one reported per block is the worst —
// one line per block that is wrong, rather than one per pair, which for a single
// misdated block deep in a long forward would be a page of the same finding.
//
// The newest-first premise is a property of quoted mail and not of every body
// that reaches here. Ticket systems append a conversation history oldest-first,
// and this corpus holds such bodies: one Zendesk trail runs strictly ascending
// across nine blocks. Those bodies invert by hours rather than days, so the zone
// window in span already swallows them, and nothing is claimed about them — which
// is why a finding names a misordered body as one of its possible causes rather
// than asserting a wrong clock.
func impossible(stamps []Stamp) []Inversion {
	var out []Inversion
	for j := 1; j < len(stamps); j++ {
		if stamps[j].Wall.IsZero() {
			continue
		}
		lo, _ := span(stamps[j])
		worst := Inversion{Outer: -1, Inner: j}
		for i := 0; i < j; i++ {
			if stamps[i].Wall.IsZero() {
				continue
			}
			_, hi := span(stamps[i])
			if excess := lo.Sub(hi); excess > 0 && excess > worst.Excess {
				worst.Outer, worst.Excess = i, excess
			}
		}
		if worst.Outer >= 0 {
			out = append(out, worst)
		}
	}
	return out
}

// span is the window of instants a stamp could refer to.
//
// Two widenings, both in the direction of claiming less:
//
//   - an unknown zone puts the instant anywhere in the 26 hours between
//     zone.Min and zone.Max, so an ordering difference smaller than that is
//     indistinguishable from a Sydney client quoting a Vancouver one and must
//     not be called an anomaly;
//   - a stamp landing exactly on midnight is either a real midnight send or a
//     date whose clock did not parse, and nothing here can tell those apart, so
//     it gets the whole day's width. A real midnight send loses some detection;
//     a misparsed clock would otherwise gain a false anomaly, and a feature that
//     cries wolf is worth less than one that misses a case.
func span(s Stamp) (lo, hi time.Time) {
	lo, hi = s.Wall, s.Wall
	if s.Off != nil {
		lo = lo.Add(-time.Duration(*s.Off) * time.Minute)
		hi = lo
	} else {
		lo = lo.Add(-time.Duration(zone.Max) * time.Minute)
		hi = hi.Add(-time.Duration(zone.Min) * time.Minute)
	}
	if s.Wall.Hour() == 0 && s.Wall.Minute() == 0 && s.Wall.Second() == 0 {
		hi = hi.Add(24 * time.Hour)
	}
	return lo, hi
}

// instant places a stamp for ordering, reading an unlabelled wall clock at the
// presumed offset. See Chrono on why this is not what Impossible uses.
func instant(s Stamp, presumed int) time.Time {
	off := presumed
	if s.Off != nil {
		off = *s.Off
	}
	return s.Wall.Add(-time.Duration(off) * time.Minute)
}
