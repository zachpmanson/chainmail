package unnest

import (
	"testing"
	"time"
)

func at(y int, m time.Month, d, h, min int) time.Time {
	return time.Date(y, m, d, h, min, 0, 0, time.UTC)
}

func off(mins int) *int { return &mins }

// Nesting says the second block is older than the first, always. The stated
// dates are the only evidence about whether that is what actually happened, so
// they, not the positions, decide the order.
func TestDatedBlocksOrderByStatedDateNotByPosition(t *testing.T) {
	stamps := []Stamp{
		{Wall: at(2026, 8, 3, 10, 4), Off: off(600)},
		{Wall: at(2026, 8, 3, 11, 30)},
		{Wall: at(2026, 8, 1, 7, 0)},
	}
	c := Chrono(stamps, 600)
	want := []int{1, 0, 2}
	if len(c.Order) != 3 || c.Order[0] != want[0] || c.Order[1] != want[1] || c.Order[2] != want[2] {
		t.Errorf("order = %v, want %v", c.Order, want)
	}
	if c.Moved != 2 {
		t.Errorf("moved = %d, want the two blocks that swapped", c.Moved)
	}
	// 86 minutes apart, both plausibly in different zones: an ordering difference
	// this small is what a zone difference looks like, so it is not a finding.
	if len(c.Impossible) != 0 {
		t.Errorf("a difference a zone could explain was reported: %+v", c.Impossible)
	}
}

// Blocks stating the same minute are the common case in a forward that quotes a
// forward, and their nesting is the better evidence of which came first.
func TestEqualStatedDatesKeepNestingOrder(t *testing.T) {
	same := at(2026, 8, 3, 9, 0)
	c := Chrono([]Stamp{{Wall: same}, {Wall: same}, {Wall: same}}, 0)
	for i, idx := range c.Order {
		if idx != i {
			t.Fatalf("order = %v, want nesting order preserved", c.Order)
		}
	}
	if c.Moved != 0 {
		t.Errorf("moved = %d, want 0 when nothing changed place", c.Moved)
	}
}

// An unstated zone puts an instant anywhere in a 26-hour window, so a
// wall-clock inversion smaller than that window is indistinguishable from one
// correspondent being in Auckland and the other in Vancouver. Reporting it would
// make the anomaly list worthless.
func TestInversionWithinTheZoneWindowIsNotReported(t *testing.T) {
	stamps := []Stamp{
		{Wall: at(2026, 8, 3, 10, 0)},
		{Wall: at(2026, 8, 4, 6, 0)}, // 20h later by wall clock
	}
	if got := Chrono(stamps, 0).Impossible; len(got) != 0 {
		t.Errorf("a 20h inversion between two unplaced clocks was reported: %+v", got)
	}
}

// The same inversion with both zones stated is a fact rather than a
// possibility: there is no offset left to absorb it.
func TestTheSameInversionIsReportedOnceBothZonesAreKnown(t *testing.T) {
	stamps := []Stamp{
		{Wall: at(2026, 8, 3, 10, 0), Off: off(600)},
		{Wall: at(2026, 8, 4, 6, 0), Off: off(600)},
	}
	got := Chrono(stamps, 600).Impossible
	if len(got) != 1 {
		t.Fatalf("impossible = %+v, want the one inversion", got)
	}
	if got[0].Outer != 0 || got[0].Inner != 1 {
		t.Errorf("inversion = %+v, want block 1 quoted inside block 0", got[0])
	}
	if got[0].Excess != 20*time.Hour {
		t.Errorf("excess = %s, want 20h", got[0].Excess)
	}
}

// A stamp on midnight is either a real midnight send or a date whose clock did
// not parse. Since the two are indistinguishable here, the day's width is
// allowed for, and an inversion that fits inside the day stays unreported.
func TestAMidnightStampGetsTheDaysWidthOfDoubt(t *testing.T) {
	stamps := []Stamp{
		{Wall: at(2026, 8, 4, 0, 0), Off: off(600)},
		{Wall: at(2026, 8, 4, 9, 0), Off: off(600)},
	}
	if got := Chrono(stamps, 600).Impossible; len(got) != 0 {
		t.Errorf("an inversion inside the outer block's unread day was reported: %+v", got)
	}
	// Past the day it is a finding again.
	stamps[1].Wall = at(2026, 8, 5, 9, 0)
	if got := Chrono(stamps, 600).Impossible; len(got) != 1 {
		t.Errorf("impossible = %+v, want the inversion that outruns the day", got)
	}
}

// A block whose sentinel gave no date is still a block that was found. It is
// carried out separately so that a caller cannot mistake its position for a
// claim about when it was sent.
func TestUndatedBlocksAreCarriedSeparatelyInNestingOrder(t *testing.T) {
	stamps := []Stamp{
		{Wall: at(2026, 8, 3, 10, 0), Off: off(600)},
		{},
		{Wall: at(2026, 8, 1, 7, 0)},
		{},
	}
	c := Chrono(stamps, 600)
	if len(c.Order) != 2 || c.Order[0] != 0 || c.Order[1] != 2 {
		t.Errorf("order = %v, want only the dated blocks", c.Order)
	}
	if len(c.Undated) != 2 || c.Undated[0] != 1 || c.Undated[1] != 3 {
		t.Errorf("undated = %v, want blocks 1 and 3 in nesting order", c.Undated)
	}
	// An undated block is not evidence of a disagreement, and must not be
	// compared against anything.
	if len(c.Impossible) != 0 {
		t.Errorf("an undated block produced a finding: %+v", c.Impossible)
	}
}

// Every earlier block quotes every later one, so a single misdated block
// contradicts several. One line per block, naming the outer block it contradicts
// hardest, keeps the report readable without dropping the finding.
func TestOneFindingPerBlockNamingItsWorstContradiction(t *testing.T) {
	stamps := []Stamp{
		{Wall: at(2026, 8, 10, 9, 0), Off: off(600)},
		{Wall: at(2026, 8, 5, 9, 0), Off: off(600)},
		{Wall: at(2026, 8, 20, 9, 0), Off: off(600)},
	}
	got := Chrono(stamps, 600).Impossible
	if len(got) != 1 {
		t.Fatalf("impossible = %+v, want one finding for the one misdated block", got)
	}
	if got[0].Inner != 2 || got[0].Outer != 1 {
		t.Errorf("inversion = %+v, want block 2 against block 1, the oldest block quoting it",
			got[0])
	}
}

// The presumed zone decides what prints first and must never decide that a date
// is wrong: a Sydney reply to a London mail inverts by nine hours every time,
// and asserting an anomaly there would flag ordinary correspondence.
func TestThePresumedZoneOrdersButNeverAccuses(t *testing.T) {
	stamps := []Stamp{
		{Wall: at(2026, 8, 3, 18, 0), Off: off(600)}, // 08:00 UTC
		{Wall: at(2026, 8, 3, 20, 0)},                // unplaced wall clock
	}
	c := Chrono(stamps, 600)
	if len(c.Order) != 2 || c.Order[0] != 1 {
		t.Errorf("order = %v, want the later stated clock first", c.Order)
	}
	if len(c.Impossible) != 0 {
		t.Errorf("the presumed zone was used to accuse: %+v", c.Impossible)
	}
}
