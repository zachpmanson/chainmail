package corpus

import (
	"testing"
	"time"
)

// A ring in the reply graph is a walk of parent_id that returns to itself, and
// every test here guards or repairs one. The names, addresses and bodies are
// invented, in the house style of twins_test.go.

// setupRing writes a straight five-node ring child -> parent around a clock
// that runs in the same direction except for the seam, named by the caller so
// the repair's choice is checkable: c0 is the newest host, c4 the oldest
// quoted block, and the closing edge c4 -> c0 is the one the repair must cut.
func setupRing(t *testing.T, s *Store) (int64, int64) {
	t.Helper()
	off := 600
	base := time.Unix(1_700_000_000, 0)
	p := person(t, s, "ring@corpus.fed", "Ring Tester")
	mk := func(name string, at time.Time, parent int64) int64 {
		res, err := s.Put(Entry{
			Source: SourceMail, ExtID: "mail:<" + name + ">", TS: at, TZ: "AEST",
			TZOffset: &off, PersonID: p, Container: "ring",
			Subject:  "ring " + name,
			BodyText: "the ring body of " + name,
		}, &Mail{MessageID: "<" + name + ">"}, nil)
		if err != nil {
			t.Fatalf("putting %s: %v", name, err)
		}
		if parent != 0 {
			if _, err := s.db.Exec(
				`update entries set parent_id = ? where id = ?`, parent, res.ID); err != nil {
				t.Fatalf("wiring %s: %v", name, err)
			}
		}
		return res.ID
	}
	// c0 (host, newest) -> c1 -> c2 -> c3 -> c4 (oldest) -> c0: only the last
	// edge runs against the clock, so it is unambiguously the seam.
	c4 := mk("c4@ring", base, 0)
	c3 := mk("c3@ring", base.Add(1*time.Hour), c4)
	c2 := mk("c2@ring", base.Add(2*time.Hour), c3)
	c1 := mk("c1@ring", base.Add(3*time.Hour), c2)
	c0 := mk("c0@ring", base.Add(4*time.Hour), c1)
	// the seam: newest quotes the copy of the oldest
	if _, err := s.db.Exec(`update entries set parent_id = ? where id = ?`, c0, c4); err != nil {
		t.Fatalf("closing the ring: %v", err)
	}
	return c0, c4
}

func TestRepairGraphSevertsTheAnachronisticEdge(t *testing.T) {
	s := open(t)
	rep, err := s.RepairGraph()
	if err != nil {
		t.Fatalf("RepairGraph on an empty store: %v", err)
	}
	if rep.Edges != 0 {
		t.Fatalf("empty store: severed %d edges, want 0", rep.Edges)
	}

	_, seamChild := setupRing(t, s)
	rep, err = s.RepairGraph()
	if err != nil {
		t.Fatalf("RepairGraph: %v", err)
	}
	if rep.Edges != 1 {
		t.Fatalf("severed %d edges, want exactly the one seam\n%+v", rep.Edges, rep.Severed)
	}
	if rep.Severed[0].Child != seamChild {
		t.Fatalf("severed %d (%s), want the seam child %d",
			rep.Severed[0].Child, rep.Severed[0].ChildExt, seamChild)
	}
	var parent int64
	if err := s.db.QueryRow(`select coalesce(parent_id,0) from entries where id=?`, seamChild).
		Scan(&parent); err != nil {
		t.Fatal(err)
	}
	if parent != 0 {
		t.Fatalf("seam child still has a parent %d", parent)
	}
	// And the whole graph is acyclic now.
	rep, err = s.RepairGraph()
	if err != nil {
		t.Fatalf("second RepairGraph: %v", err)
	}
	if rep.Edges != 0 {
		t.Fatalf("second RepairGraph severed %d more edges\n%+v", rep.Edges, rep.Severed)
	}
}

// Every entry has one parent, so rings are disjoint: two independent rings
// need exactly two severs, each on its own seam.
func TestRepairGraphSevertsTwoDisjointRings(t *testing.T) {
	s := open(t)
	off := 600
	base := time.Unix(1_700_000_000, 0)
	p := person(t, s, "two@corpus.fed", "Two Ring Tester")
	// mk creates the entry with no parent; the caller wires edges afterwards,
	// because a ring's edges point at each other and cannot be built in
	// parent-first order.
	mk := func(name string, at time.Time) int64 {
		res, err := s.Put(Entry{
			Source: SourceMail, ExtID: "mail:<" + name + ">", TS: at, TZ: "AEST",
			TZOffset: &off, PersonID: p, Container: "two-rings",
			Subject: "two rings " + name, BodyText: "the two-ring body of " + name,
		}, &Mail{MessageID: "<" + name + ">"}, nil)
		if err != nil {
			t.Fatalf("putting %s: %v", name, err)
		}
		return res.ID
	}
	wire := func(child, parent int64, name string) {
		if _, err := s.db.Exec(
			`update entries set parent_id = ? where id = ?`, parent, child); err != nil {
			t.Fatalf("wiring %s: %v", name, err)
		}
	}
	// Ring one: a -> b -> c -> a. Newest is a (base+2h), oldest b (base), so
	// the seam the repair must cut is c -> a.
	a := mk("a@two", base.Add(2*time.Hour))
	c := mk("c@two", base.Add(45*time.Minute))
	b := mk("b@two", base)
	wire(a, b, "a")
	wire(b, c, "b")
	wire(c, a, "c") // the seam
	// Ring two, elsewhere: d -> e -> d, seam e -> d.
	e := mk("e@two", base.Add(3*time.Hour))
	d := mk("d@two", base.Add(4*time.Hour))
	wire(d, e, "d")
	wire(e, d, "e") // the seam

	rep, err := s.RepairGraph()
	if err != nil {
		t.Fatalf("RepairGraph: %v", err)
	}
	if rep.Edges != 2 {
		t.Fatalf("severed %d edges, want the two seams (c@two, e@two)",
			rep.Edges)
	}
	for _, s := range rep.Severed {
		if s.Child != c && s.Child != e {
			t.Fatalf("severed %d (%s), want only the two seam children", s.Child, s.ChildExt)
		}
	}
	if rep, err := s.RepairGraph(); err != nil {
		t.Fatalf("second RepairGraph: %v", err)
	} else if rep.Edges != 0 {
		t.Fatalf("rings not emptied: %d more edges\n%+v", rep.Edges, rep.Severed)
	}
}

func TestSetParentRefusesACycle(t *testing.T) {
	s := open(t)
	off := 600
	at := time.Unix(1_700_000_000, 0)
	p := person(t, s, "two@corpus.fed", "Two Tester")
	mk := func(name string) int64 {
		res, err := s.Put(Entry{
			Source: SourceMail, ExtID: "mail:<" + name + ">", TS: at, TZ: "AEST",
			TZOffset: &off, PersonID: p, BodyText: "body of " + name,
		}, &Mail{MessageID: "<" + name + ">"}, nil)
		if err != nil {
			t.Fatalf("putting %s: %v", name, err)
		}
		return res.ID
	}
	a, b := mk("a@two"), mk("b@two")

	if err := s.SetParent(b, a); err != nil {
		t.Fatalf("first edge: %v", err)
	}
	// a -> b would close b -> a; refused.
	if err := s.SetParent(a, b); err != nil {
		t.Fatalf("ring edge errored instead of declining: %v", err)
	}
	var parent int64
	if err := s.db.QueryRow(`select coalesce(parent_id,0) from entries where id=?`, a).
		Scan(&parent); err != nil {
		t.Fatal(err)
	}
	if parent != 0 {
		t.Fatalf("ring edge was written: a now points at %d", parent)
	}
}

// The bug the corpus actually hit: a reply quotes the original inside its body,
// so the copy's parent is the reply, and the reply's own edge points back at
// the original. Collapsing the copy into the original must not adopt the reply
// as the original's parent — that closes original -> reply -> original.
func TestAbsorbRefusesAdoptingTheRingParent(t *testing.T) {
	s := open(t)
	deniz := person(t, s, "deniz.aslan@quarry.fed", "Deniz Aslan")
	tui := person(t, s, "tui.walker@moana.fed", "Tui Walker")
	sent := twinAt(t, "2026-05-20 01:38:14")

	keep := mailbox(t, s, deniz, "ask@quarry.fed", askBody, sent)
	h := host(t, s, tui, "reply@moana.fed", sent.Add(3*time.Hour))
	if err := s.SetParent(h, keep); err != nil { // the reply edge
		t.Fatal(err)
	}
	drop := recovered(t, s, deniz, "abc", askQuoted, sent.Add(10*time.Hour), h)
	if err := s.SetParent(drop, h); err != nil { // quoted inside the reply
		t.Fatal(err)
	}

	if _, err := CollapseTwins(s, true); err != nil {
		t.Fatalf("CollapseTwins: %v", err)
	}
	// The survivor must stay a root: adopting the reply's parent would ring it.
	var parent, dropGone int64
	if err := s.db.QueryRow(`select coalesce(parent_id,0) from entries where id=?`, keep).
		Scan(&parent); err != nil {
		t.Fatal(err)
	}
	if parent != 0 {
		t.Fatalf("survivor adopted the ring parent: keep(%d) -> %d", keep, parent)
	}
	if err := s.db.QueryRow(`select count(*) from entries where id=?`, drop).
		Scan(&dropGone); err != nil {
		t.Fatal(err)
	}
	if dropGone != 0 {
		t.Fatalf("dropped copy %d still exists", drop)
	}
	// The reply still reads: reply -> keep -> root.
	if err := s.db.QueryRow(`select coalesce(parent_id,0) from entries where id=?`, h).
		Scan(&parent); err != nil {
		t.Fatal(err)
	}
	if parent != keep {
		t.Fatalf("reply edge lost: %d -> %d, want %d", h, parent, keep)
	}
}
