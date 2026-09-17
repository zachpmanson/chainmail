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
