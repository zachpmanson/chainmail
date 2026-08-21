package tzinfer

import (
	"strings"
	"testing"
	"time"
)

// The people in these fixtures are invented. Sydney is the placed quoter,
// Wanderer is the traveller, Nowhere has never stated an offset.
const (
	sydney   = int64(1)
	wanderer = int64(2)
	nowhere  = int64(3)
)

func places() map[int64]Place {
	return map[int64]Place{
		sydney: Fit([]Observation{
			{At: at("2026-07-01T02:00:00Z"), Off: 600},
			{At: at("2026-01-15T02:00:00Z"), Off: 660},
		}),
		wanderer: Fit([]Observation{
			{At: at("2026-04-08T05:30:00Z"), Off: 330},
			{At: at("2026-05-25T05:21:00Z"), Off: 600},
		}),
		nowhere: Fit(nil),
	}
}

// quoted builds a node for a message recovered from quoted text: a wall clock
// and no offset.
func quoted(id, parent, person int64, wall string) Node {
	return Node{ID: id, Parent: parent, Person: person, Wall: at(wall)}
}

// mailbox builds a node for a real message: an instant and a stated offset.
func mailbox(id, parent, person int64, ts string, off int) Node {
	o := off
	return Node{ID: id, Parent: parent, Person: person, Wall: at(ts), Off: &o}
}

// The ordinary case: one quoting client, placed, so one candidate.
func TestQuotedClockTakesTheQuotingClientsZone(t *testing.T) {
	nodes := []Node{
		quoted(10, 0, nowhere, "2026-08-10T09:00:00Z"),
		mailbox(11, 10, sydney, "2026-08-10T00:30:00Z", 600),
	}
	res, st := Resolve(nodes, places())
	got := res[10]
	if got.State != Inferred || got.Off != 600 {
		t.Fatalf("entry 10 = %v %+d (%s)", got.State, got.Off, got.Evidence)
	}
	if st.Inferred != 1 || st.Stated != 1 {
		t.Errorf("stats = %+v", st)
	}
	if res[11].State != Stated {
		t.Errorf("the mailbox copy must stay stated, got %v", res[11].State)
	}
}

// An inference is a claim and says so. Nothing may present it as the source's
// own words, and the evidence for it has to travel with it.
func TestAnInferenceIsMarkedAndArgued(t *testing.T) {
	nodes := []Node{
		quoted(10, 0, nowhere, "2026-08-10T09:00:00Z"),
		mailbox(11, 10, sydney, "2026-08-10T00:30:00Z", 600),
	}
	res, _ := Resolve(nodes, places())
	if res[10].State == Stated {
		t.Fatal("an inferred zone must never be reported as stated")
	}
	for _, want := range []string{"+1000", "Australia/Sydney"} {
		if !strings.Contains(res[10].Evidence, want) {
			t.Errorf("evidence %q does not mention %q", res[10].Evidence, want)
		}
	}
}

// A stated offset is never reconsidered, whatever the inference machinery would
// have said about the same entry. The source's own Date header is the better
// evidence by construction.
func TestStatedBeatsInferred(t *testing.T) {
	// The quoting client is in Sydney, so an inference would say +1000; the
	// message itself states +0530.
	nodes := []Node{
		mailbox(10, 0, wanderer, "2026-04-08T05:30:00Z", 330),
		mailbox(11, 10, sydney, "2026-04-08T06:00:00Z", 600),
	}
	res, st := Resolve(nodes, places())
	if res[10].State != Stated || res[10].Off != 330 {
		t.Fatalf("entry 10 = %v %+d, want the stated +0530", res[10].State, res[10].Off)
	}
	if st.Inferred != 0 {
		t.Errorf("nothing to infer here, stats = %+v", st)
	}
}

// Nothing is invented for a quoting client we have never observed.
func TestNoQuoterEvidenceStaysUnknown(t *testing.T) {
	nodes := []Node{
		quoted(10, 0, sydney, "2026-08-10T09:00:00Z"),
		quoted(11, 10, nowhere, "2026-08-10T11:00:00Z"),
	}
	res, st := Resolve(nodes, places())
	for _, id := range []int64{10, 11} {
		if res[id].State != Unknown {
			t.Errorf("entry %d = %v %+d, want unknown", id, res[id].State, res[id].Off)
		}
		if res[id].Off != 0 || res[id].Evidence == "" {
			t.Errorf("entry %d fabricated %+d or said nothing about why", id, res[id].Off)
		}
	}
	if st.Unknown != 2 || st.Inferred != 0 {
		t.Errorf("stats = %+v", st)
	}
}

// A traveller's offsets are candidates, not answers. With neighbours too distant
// to separate them the entry is ambiguous, and ambiguous renders as no zone at
// all — the alternative is a clock four and a half hours out, presented as
// though someone had checked it.
func TestTravellerAmbiguousWhenNothingSeparates(t *testing.T) {
	nodes := []Node{
		quoted(9, 0, nowhere, "2026-04-01T09:00:00Z"),
		quoted(10, 9, nowhere, "2026-04-15T09:00:00Z"),
		quoted(11, 10, wanderer, "2026-04-30T09:00:00Z"),
	}
	res, st := Resolve(nodes, places())
	if res[10].State != Ambiguous {
		t.Fatalf("entry 10 = %v (%s)", res[10].State, res[10].Evidence)
	}
	if res[10].Off != 0 {
		t.Errorf("an ambiguous entry must carry no offset, got %+d", res[10].Off)
	}
	for _, want := range []string{"+0530", "+1000"} {
		if !strings.Contains(res[10].Evidence, want) {
			t.Errorf("evidence %q should name both candidates", res[10].Evidence)
		}
	}
	if st.Ambiguous != 1 {
		t.Errorf("stats = %+v", st)
	}
}

// The ordering earns its keep here: the same traveller, but the neighbours are
// close enough that only one of his two offsets puts the message between them.
// A frequency rule would have had to guess.
func TestOrderingChoosesBetweenATravellersOffsets(t *testing.T) {
	// Entry 10's wall clock is 14:00. Read at +0530 that is 08:30 UTC, which is
	// before its parent; read at +1000 it is 04:00 UTC, which is not.
	nodes := []Node{
		mailbox(9, 0, sydney, "2026-04-15T03:30:00Z", 600),
		quoted(10, 9, nowhere, "2026-04-15T14:00:00Z"),
		mailbox(11, 10, wanderer, "2026-04-15T05:00:00Z", 330),
	}
	res, st := Resolve(nodes, places())
	if res[10].State != Inferred || res[10].Off != 600 {
		t.Fatalf("entry 10 = %v %+d (%s)", res[10].State, res[10].Off, res[10].Evidence)
	}
	if st.Selected != 1 {
		t.Errorf("this is the ordering choosing, not placement: stats = %+v", st)
	}
}

// An inferred zone that puts a message outside the window its neighbours allow
// is evidence against the inference, not against the ordering. The candidate is
// dropped, the entry falls back to unknown, and the fallback is counted and
// explained rather than absorbed.
func TestAnImpossibleOrderingRejectsTheInference(t *testing.T) {
	// The parent is stated three days after the quoted clock could possibly be,
	// whichever way it is read.
	nodes := []Node{
		mailbox(9, 0, sydney, "2026-08-13T00:00:00Z", 600),
		quoted(10, 9, nowhere, "2026-08-10T09:00:00Z"),
		mailbox(11, 10, sydney, "2026-08-14T00:00:00Z", 600),
	}
	res, st := Resolve(nodes, places())
	if res[10].State != Unknown {
		t.Fatalf("entry 10 = %v %+d, want the inference dropped", res[10].State, res[10].Off)
	}
	if st.Rejected != 1 || st.Inferred != 0 {
		t.Fatalf("the rejection must be counted: stats = %+v", st)
	}
	if !strings.Contains(res[10].Evidence, "outside the window") {
		t.Errorf("the fallback must say why: %q", res[10].Evidence)
	}
}

// The 26-hour window, unloosened, in one test: a wall-clock inversion of a few
// hours between two unlabelled clocks is a zone artefact and must not be
// reported, while a three-day inversion is a real contradiction and must be.
//
// A check that fired on the first would have fired on 421 wall-clock inversions
// in this corpus, none of which exceeded the window, and every one of those
// findings would have been worthless.
func TestTheZoneWindowIsNotLoosened(t *testing.T) {
	// Artefact: the child's clock reads an hour EARLIER than its parent's, which
	// any pair of zones an ocean apart produces routinely.
	artefact := []Node{
		quoted(9, 0, nowhere, "2026-08-10T09:00:00Z"),
		quoted(10, 9, nowhere, "2026-08-10T08:00:00Z"),
		mailbox(11, 10, sydney, "2026-08-11T00:00:00Z", 600),
	}
	res, st := Resolve(artefact, places())
	if res[10].State != Inferred {
		t.Fatalf("a one-hour inversion between two unlabelled clocks is not a contradiction: %v (%s)",
			res[10].State, res[10].Evidence)
	}
	if st.Rejected != 0 {
		t.Fatalf("nothing to reject here: stats = %+v", st)
	}

	// Contradiction: the same shape, but the parent's clock is three days later,
	// which no zone assignment on earth can absorb.
	contradiction := []Node{
		quoted(9, 0, nowhere, "2026-08-13T09:00:00Z"),
		quoted(10, 9, nowhere, "2026-08-10T08:00:00Z"),
		mailbox(11, 10, sydney, "2026-08-14T00:00:00Z", 600),
	}
	res, st = Resolve(contradiction, places())
	if res[10].State != Unknown || st.Rejected != 1 {
		t.Fatalf("a three-day inversion is a contradiction: %v, stats = %+v", res[10].State, st)
	}
}

// A sentinel writes whole minutes, so a reply quoted as 10:00 whose parent's
// Date header says 10:00:45 is rounding, not a contradiction. Without the
// minute of width the rejection would look like evidence against the zone.
func TestAMinuteOfRoundingIsNotAContradiction(t *testing.T) {
	nodes := []Node{
		mailbox(9, 0, sydney, "2026-08-10T00:00:45Z", 600),
		quoted(10, 9, nowhere, "2026-08-10T10:00:00Z"),
		mailbox(11, 10, sydney, "2026-08-10T01:00:00Z", 600),
	}
	res, st := Resolve(nodes, places())
	if res[10].State != Inferred || res[10].Off != 600 {
		t.Fatalf("entry 10 = %v %+d (%s); stats = %+v", res[10].State, res[10].Off, res[10].Evidence, st)
	}
}

// A parent cycle is not forbidden by the store, and must not spin.
func TestAParentCycleTerminates(t *testing.T) {
	done := make(chan struct{})
	go func() {
		Resolve([]Node{
			quoted(10, 11, nowhere, "2026-08-10T09:00:00Z"),
			quoted(11, 10, sydney, "2026-08-10T10:00:00Z"),
		}, places())
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Resolve did not settle on a cyclic graph")
	}
}
