package corpus

import (
	"testing"
	"time"
)

// putMail puts one message with the headers a reply carries, and reports its id.
func putMail(t *testing.T, s *Store, ext, messageID, inReplyTo string, refs ...string) int64 {
	t.Helper()
	res, err := s.Put(Entry{
		Source: SourceMail, ExtID: ext, Kind: "message",
		TS: time.Unix(1_700_000_000, 0), BodyText: ext, ParentRef: inReplyTo,
	}, &Mail{MessageID: messageID, InReplyTo: inReplyTo, References: refs}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return res.ID
}

func parentOf(t *testing.T, s *Store, id int64) int64 {
	t.Helper()
	var p *int64
	if err := s.db.QueryRow(`select parent_id from entries where id = ?`, id).Scan(&p); err != nil {
		t.Fatal(err)
	}
	if p == nil {
		return 0
	}
	return *p
}

// A reply whose own parent was never fetched still names its ancestry, and the
// newest of those ids the corpus holds is where it belongs: linked to its
// grandparent, not left standing as a root of its own.
func TestResolveParentsFallsBackToReferences(t *testing.T) {
	s := open(t)
	grand := putMail(t, s, "grand", "<grand@x>", "")
	// The message in between was never ingested.
	reply := putMail(t, s, "reply", "<reply@x>", "<missing@x>", "<grand@x>", "<missing@x>")

	n, err := s.ResolveParents()
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("ResolveParents: got %d edges, want 1", n)
	}
	if got := parentOf(t, s, reply); got != grand {
		t.Errorf("parent: got %d, want the grandparent %d", got, grand)
	}
}

// In-Reply-To is the last word: when References names an older message the
// corpus also holds, the direct parent is the one the message addressed.
func TestResolveParentsPrefersInReplyToOverTheOlderReferences(t *testing.T) {
	s := open(t)
	older := putMail(t, s, "older", "<older@x>", "")
	direct := putMail(t, s, "direct", "<direct@x>", "")
	reply := putMail(t, s, "reply", "<reply@x>", "<direct@x>", "<older@x>", "<direct@x>")

	if _, err := s.ResolveParents(); err != nil {
		t.Fatal(err)
	}
	if got := parentOf(t, s, reply); got != direct {
		t.Errorf("parent: got %d, want the direct parent %d (not the older %d)", got, direct, older)
	}
}

// ResolveParents fills a NULL parent and leaves a drawn edge alone: it is the
// ingest's pass, and overwriting an edge the graph already has is not its job.
func TestResolveParentsLeavesAnEdgeAlreadyDrawn(t *testing.T) {
	s := open(t)
	guessed := putMail(t, s, "guessed", "<guessed@x>", "")
	// The message the header names is in the corpus, so the header resolves: the
	// pass below has something it could draw and must decline to draw it.
	putMail(t, s, "real", "<real@x>", "")
	reply := putMail(t, s, "reply", "<reply@x>", "<real@x>")
	if err := s.SetParent(reply, guessed); err != nil {
		t.Fatal(err)
	}

	n, err := s.ResolveParents()
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("ResolveParents: got %d edges, want none", n)
	}
	if got := parentOf(t, s, reply); got != guessed {
		t.Errorf("parent: got %d, want the edge already there (%d)", got, guessed)
	}
}

// The whole point of the reassert: an edge written by whoever asked first loses
// to the header its own message carries.
func TestReassertParentsRedrawsAnEdgeTheHeaderDisagreesWith(t *testing.T) {
	s := open(t)
	quoted := putMail(t, s, "quoted", "<quoted@x>", "")
	real := putMail(t, s, "real", "<real@x>", "")
	reply := putMail(t, s, "reply", "<reply@x>", "<real@x>")
	// What the quoted pass wrote before it learned to leave the host alone.
	if err := s.SetParent(reply, quoted); err != nil {
		t.Fatal(err)
	}

	n, err := s.ReassertParents()
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("ReassertParents: got %d edges, want 1", n)
	}
	if got := parentOf(t, s, reply); got != real {
		t.Errorf("parent: got %d, want the header's %d", got, real)
	}

	// Idempotent: the second run has nothing left to redraw.
	again, err := s.ReassertParents()
	if err != nil {
		t.Fatal(err)
	}
	if again != 0 {
		t.Errorf("second run: got %d edges, want none", again)
	}
}

// A header naming a message the corpus does not hold is no reason to cut the
// edge an entry has: the structural guess is the only thing placing it in a
// conversation, and half a trail beats none.
func TestReassertParentsLeavesAnUnresolvableHeaderAlone(t *testing.T) {
	s := open(t)
	guessed := putMail(t, s, "guessed", "<guessed@x>", "")
	reply := putMail(t, s, "reply", "<reply@x>", "<never-fetched@x>")
	if err := s.SetParent(reply, guessed); err != nil {
		t.Fatal(err)
	}

	n, err := s.ReassertParents()
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("ReassertParents: got %d edges, want none", n)
	}
	if got := parentOf(t, s, reply); got != guessed {
		t.Errorf("parent: got %d, want the edge already there (%d)", got, guessed)
	}
}

// A header can name a message that is already below this one — a thread quoted
// in full inside a reply names it both ways — and a walk reads a ring as no
// chain at all, so the edge is refused rather than drawn.
func TestReassertParentsRefusesAnEdgeThatWouldCloseARing(t *testing.T) {
	s := open(t)
	first := putMail(t, s, "first", "<first@x>", "")
	second := putMail(t, s, "second", "<second@x>", "<first@x>")
	// The first message's header names the second. That edge would ring.
	if _, err := s.db.Exec(`update entries set parent_ref = ? where id = ?`, "<second@x>", first); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ResolveParents(); err != nil {
		t.Fatal(err)
	}

	// Exactly one of the two edges, whichever the pass reached first: drawing
	// both would be the ring.
	pf, ps := parentOf(t, s, first), parentOf(t, s, second)
	switch {
	case pf != 0 && ps != 0:
		t.Errorf("both edges drawn (%d -> %d, %d -> %d): the graph rings", first, pf, second, ps)
	case pf == 0 && ps == 0:
		t.Error("neither edge drawn, want one")
	}
}

// A forward whose own header names a parent the corpus never received keeps that
// slot for nothing: nothing places it, and the trail recovered out of its body is
// stored but disconnected. The quoted sightings are the evidence that joins the
// two, and the edge drawn is the one the quoted pass would have drawn had the
// header not claimed the slot — the outermost block the body named first.
func TestRepairDanglingQuoteParentsLinksAForwardOntoItsQuotedTrail(t *testing.T) {
	s := open(t)
	at := func(min int) time.Time {
		return time.Date(2026, 8, 20, 9, min, 0, 0, time.UTC)
	}

	// The forward. Its In-Reply-To names a message the corpus does not hold.
	res, err := s.Put(Entry{
		Source: SourceMail, ExtID: "mail:<forward@x>", Kind: "message",
		TS: at(30), BodyText: "passing this on", Container: "thread-1",
		ParentRef: "<never-received@elsewhere.example>",
	}, &Mail{
		MessageID: "<forward@x>",
		InReplyTo: "<never-received@elsewhere.example>",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	host := res.ID

	// The two messages its body quotes, outermost (newest) first.
	outer, _, err := s.PutQuoted(Entry{
		Source: SourceMail, ExtID: "quote:outer", Kind: "message",
		TS: at(20), BodyText: "the newest quoted message", Container: "thread-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	inner, _, err := s.PutQuoted(Entry{
		Source: SourceMail, ExtID: "quote:inner", Kind: "message",
		TS: at(10), BodyText: "the oldest quoted message", Container: "thread-1",
	})
	if err != nil {
		t.Fatal(err)
	}

	// What extraction records: each block was seen inside the host, and the
	// nesting edge places the outer block above the inner one. The host is
	// deliberately absent — its header claimed the slot, so the quoted pass
	// skipped it.
	for _, b := range []struct {
		id     int64
		detail string
	}{{outer, "depth 0"}, {inner, "depth 1"}} {
		if err := s.Sight(b.id, host, "quoted", b.detail); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.SetParent(outer, inner); err != nil {
		t.Fatal(err)
	}

	// Neither existing pass can place the host: the header names nothing the
	// corpus holds, and there is no edge to keep.
	if n, err := s.ResolveParents(); err != nil {
		t.Fatal(err)
	} else if n != 0 {
		t.Errorf("ResolveParents: got %d edges, want none (the parent is dangling)", n)
	}
	if n, err := s.ReassertParents(); err != nil {
		t.Fatal(err)
	} else if n != 0 {
		t.Errorf("ReassertParents: got %d edges, want none (there is no edge to keep)", n)
	}
	if got := parentOf(t, s, host); got != 0 {
		t.Fatalf("host parent before the repair: got %d, want none", got)
	}

	n, err := s.RepairDanglingQuoteParents()
	if err != nil {
		t.Fatalf("RepairDanglingQuoteParents: %v", err)
	}
	if n != 1 {
		t.Fatalf("RepairDanglingQuoteParents: got %d edges, want 1", n)
	}
	if got := parentOf(t, s, host); got != outer {
		t.Errorf("host parent: got %d, want the outermost quoted block %d", got, outer)
	}

	// One chain of three, reachable from either end.
	for _, from := range []string{"mail:<forward@x>", "quote:outer", "quote:inner"} {
		chain, err := s.Chain(from)
		if err != nil {
			t.Fatalf("chain from %s: %v", from, err)
		}
		if len(chain) != 3 {
			t.Errorf("chain from %s: got %d entries, want 3", from, len(chain))
		}
	}

	// Re-running draws nothing: the edge is already there.
	if again, err := s.RepairDanglingQuoteParents(); err != nil {
		t.Fatal(err)
	} else if again != 0 {
		t.Errorf("second run: got %d edges, want none", again)
	}
}

// The repair is the fallback for a slot a resolving header owns: where the
// corpus holds the parent the header names, the header's edge stands and the
// quoted trail is not consulted.
func TestRepairDanglingQuoteParentsLeavesAResolvingHeaderAlone(t *testing.T) {
	s := open(t)
	at := func(min int) time.Time {
		return time.Date(2026, 8, 20, 9, min, 0, 0, time.UTC)
	}
	parent := putMail(t, s, "parent", "<parent@x>", "")
	res, err := s.Put(Entry{
		Source: SourceMail, ExtID: "mail:<reply@x>", Kind: "message",
		TS: at(30), BodyText: "the reply body", Container: "thread-1",
		ParentRef: "<parent@x>",
	}, &Mail{MessageID: "<reply@x>", InReplyTo: "<parent@x>"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	host := res.ID
	outer, _, err := s.PutQuoted(Entry{
		Source: SourceMail, ExtID: "quote:outer", Kind: "message",
		TS: at(20), BodyText: "quoted inside the reply", Container: "thread-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Sight(outer, host, "quoted", "depth 0"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ResolveParents(); err != nil {
		t.Fatal(err)
	}
	if got := parentOf(t, s, host); got != parent {
		t.Fatalf("host parent: got %d, want the header's %d", got, parent)
	}

	if n, err := s.RepairDanglingQuoteParents(); err != nil {
		t.Fatal(err)
	} else if n != 0 {
		t.Errorf("RepairDanglingQuoteParents: got %d edges, want none (the header resolves)", n)
	}
	if got := parentOf(t, s, host); got != parent {
		t.Errorf("host parent: got %d, want the header's %d (untouched)", got, parent)
	}
}
