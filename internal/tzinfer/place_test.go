package tzinfer

import (
	"testing"
	"time"
)

func at(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

// One stated offset is enough to place someone, and the placement is a claim
// about where they are rather than a copy of the offset: the caller gets it back
// through a zone, which is what makes the next test possible.
func TestOneStatedOffsetPlacesAPerson(t *testing.T) {
	p := Fit([]Observation{{At: at("2026-07-01T02:00:00Z"), Off: 600}})
	if p.Verdict != Placed {
		t.Fatalf("verdict = %v, want Placed (%v)", p.Verdict, p.Why())
	}
	if p.Zones[0] != "Australia/Sydney" {
		t.Errorf("zone = %q, want Australia/Sydney — the corpus's own centre breaks a tie "+
			"nothing in the evidence can", p.Zones[0])
	}
	if got := p.Candidates(at("2026-07-01T00:00:00Z"), at("2026-07-01T06:00:00Z")); len(got) != 1 || got[0] != 600 {
		t.Errorf("candidates = %v, want [600]", got)
	}
}

// The test a single stored offset cannot pass.
//
// Both observations are the same person in the same city; the hour between them
// is daylight saving. Storing the observed offset and applying it six months
// later is wrong by that hour, and wrong invisibly, because the clock still
// reads like a clock. A zone evaluated at the target instant is right on both
// sides of the transition.
func TestDaylightSavingResolvesOnBothSidesOfTheBoundary(t *testing.T) {
	p := Fit([]Observation{
		{At: at("2026-07-01T02:00:00Z"), Off: 600}, // AEST, winter
		{At: at("2026-01-15T02:00:00Z"), Off: 660}, // AEDT, summer
	})
	if p.Verdict != Placed {
		t.Fatalf("verdict = %v, want Placed: one zone explains both (%v)", p.Verdict, p.Why())
	}
	winter := p.Candidates(at("2026-08-10T00:00:00Z"), at("2026-08-10T06:00:00Z"))
	summer := p.Candidates(at("2026-12-10T00:00:00Z"), at("2026-12-10T06:00:00Z"))
	if len(winter) != 1 || winter[0] != 600 {
		t.Errorf("August = %v, want [600]", winter)
	}
	if len(summer) != 1 || summer[0] != 660 {
		t.Errorf("December = %v, want [660]", summer)
	}
}

// Two offsets no single zone explains is a person who moved, not a person whose
// clock sprang forward. Nothing is chosen for them: both offsets stay as
// candidates for the ordering to rule on, and "most common" is not consulted,
// because the person who travels is exactly the person a frequency rule is
// wrong about.
func TestOffsetsNoZoneExplainsDoNotResolve(t *testing.T) {
	p := Fit([]Observation{
		{At: at("2026-04-08T05:30:00Z"), Off: 330},
		{At: at("2026-05-25T05:21:00Z"), Off: 600},
	})
	if p.Verdict != Moved {
		t.Fatalf("verdict = %v, want Moved: +0530 and +1000 are 4h30m apart and India keeps no "+
			"daylight saving, so no zone shows both", p.Verdict)
	}
	if got := p.Candidates(at("2026-04-01T00:00:00Z"), at("2026-04-02T00:00:00Z")); len(got) != 2 {
		t.Errorf("candidates = %v, want both observed offsets and no preference between them", got)
	}
}

// The long tail — the corpus's busiest travellers state up to seven offsets —
// behaves like the pair: no zone fits, every offset that says something about
// place stays a candidate, and none is favoured. The +0000 among them is dropped
// on the way in, so the candidate list is six.
func TestManyOffsetsStayCandidates(t *testing.T) {
	var obs []Observation
	for i, off := range []int{660, 600, 480, -420, 0, -240, -300} {
		obs = append(obs, Observation{At: at("2026-01-01T00:00:00Z").AddDate(0, i, 0), Off: off})
	}
	p := Fit(obs)
	if p.Verdict != Moved || len(p.Seen) != 6 {
		t.Fatalf("verdict = %v, offsets = %v", p.Verdict, p.Seen)
	}
}

// A person seen only at +0000 is not placed. UTC is what a client emits when it
// does not know where it is, and one Exchange account in this corpus does
// exactly that while rendering other people's mail at +1200. Reading it as
// evidence of place puts that person in London.
func TestUTCAloneIsNotEvidenceOfPlace(t *testing.T) {
	p := Fit([]Observation{
		{At: at("2026-04-14T21:08:00Z"), Off: 0},
		{At: at("2026-05-20T01:38:00Z"), Off: 0},
	})
	if p.Verdict != UTCOnly {
		t.Fatalf("verdict = %v, want UTCOnly", p.Verdict)
	}
	if got := p.Candidates(at("2026-04-01T00:00:00Z"), at("2026-04-02T00:00:00Z")); got != nil {
		t.Errorf("candidates = %v, want none", got)
	}
}

// A +0000 is dropped rather than weighed, whatever else the person has stated,
// and a genuine British account still comes out right: the summer offset alone
// identifies the zone.
func TestBareUTCIsDroppedAndTheRestStillPlaces(t *testing.T) {
	p := Fit([]Observation{
		{At: at("2026-01-15T12:00:00Z"), Off: 0},
		{At: at("2026-07-15T12:00:00Z"), Off: 60},
	})
	if p.Verdict != Placed || p.Zones[0] != "Europe/London" {
		t.Fatalf("verdict = %v zones = %v, want Placed in Europe/London", p.Verdict, p.Zones)
	}
	if len(p.Seen) != 1 {
		t.Errorf("observed offsets = %v, want the +0000 dropped", p.Seen)
	}
}

// The case the rule exists for: a Sydney sender whose client stamps UTC now and
// then. Left in the evidence the +0000 makes no zone fit, and the sender becomes
// a traveller who never travelled — which costs the inference every message they
// ever quoted.
func TestAStrayUTCDoesNotMakeATravellerOfASydneySender(t *testing.T) {
	p := Fit([]Observation{
		{At: at("2026-07-01T02:00:00Z"), Off: 600},
		{At: at("2026-01-15T02:00:00Z"), Off: 660},
		{At: at("2026-03-04T02:00:00Z"), Off: 0},
		{At: at("2026-08-11T02:00:00Z"), Off: 0},
	})
	if p.Verdict != Placed || p.Zones[0] != "Australia/Sydney" {
		t.Fatalf("verdict = %v zones = %v, want Placed in Australia/Sydney", p.Verdict, p.Zones)
	}
}

func TestNoObservationsPlacesNobody(t *testing.T) {
	p := Fit(nil)
	if p.Verdict != NoEvidence || p.Candidates(at("2026-01-01T00:00:00Z"), at("2026-01-02T00:00:00Z")) != nil {
		t.Fatalf("verdict = %v, candidates = %v", p.Verdict, p.Candidates(at("2026-01-01T00:00:00Z"), at("2026-01-02T00:00:00Z")))
	}
}
