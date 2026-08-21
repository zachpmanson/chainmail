package corpus

import (
	"testing"
	"time"

	"github.com/zachpmanson/chainmail/internal/tzinfer"
)

// All names, addresses and domains here are invented.

// The two copies of one message, as the corpus stores them. The mailbox copy's
// clock is the true instant; the quoted copy's is that instant rendered by
// whoever quoted it, which is what the offsets in these tests add.
const (
	// A message long enough to identify, with an opening that is its own.
	askBody = `Hi Priya,

Can you confirm the meter number for the Rothwell depot before the tender pack
goes out this afternoon? The retailer wants it on the cover sheet and I do not
want to guess at it.

Thanks,
Deniz Aslan | Operations | Quarry Energy`
	// The same message as a client requotes it: rewrapped, and elided at the end.
	askQuoted = `Hi Priya, Can you confirm the meter number for the Rothwell depot before
the tender pack goes out this afternoon? The retailer wants it on the cover
sheet and I do not want to guess at it. Thanks, Deniz Aslan`
	// A different question from the same person, signed the same way. The
	// signature and disclaimer are most of the words, which is what defeats an
	// unordered comparison; the openings are what separate the two.
	otherBody = `Hi Priya,

Do we have the August network invoice yet?

Thanks,
Deniz Aslan | Operations | Quarry Energy
This email and any attachments are confidential and may be privileged. If you
are not the intended recipient you must not use, copy or disclose any of it;
please delete it and tell us at once. Quarry Energy accepts no liability for
any loss or damage arising from a virus transmitted with this message.`
	otherQuoted = `Hi Priya, Can you confirm the meter number for the Rothwell depot?
Thanks, Deniz Aslan | Operations | Quarry Energy
This email and any attachments are confidential and may be privileged. If you
are not the intended recipient you must not use, copy or disclose any of it;
please delete it and tell us at once. Quarry Energy accepts no liability for
any loss or damage arising from a virus transmitted with this message.`
)

func twinAt(t *testing.T, min string) time.Time {
	t.Helper()
	at, err := time.Parse("2006-01-02 15:04:05", min)
	if err != nil {
		t.Fatal(err)
	}
	return at
}

// mailbox stores a copy as the slurper does: the true instant, the zone the
// sender's client stated, markup, an attachment and a Message-ID.
func mailbox(t *testing.T, s *Store, person int64, ext, body string, at time.Time) int64 {
	t.Helper()
	off := 600
	e := Entry{
		Source: SourceMail, ExtID: "mail:<" + ext + ">", TS: at, TZ: "AEST",
		TZOffset: &off, PersonID: person, Container: "thread-1",
		Subject: "Rothwell tender pack", BodyText: body,
		BodyHTML: "<p>" + body + "</p>",
	}
	res, err := s.Put(e, &Mail{MessageID: "<" + ext + ">"},
		[]Attachment{{Name: "tender-pack.pdf", Mime: "application/pdf"}})
	if err != nil {
		t.Fatalf("storing the mailbox copy: %v", err)
	}
	if err := Participate(s, res.ID, person, RoleFrom); err != nil {
		t.Fatal(err)
	}
	return res.ID
}

// recovered stores a copy as extraction does: a stated wall clock with no zone,
// one block of text, and a sighting naming the host it was quoted in.
func recovered(t *testing.T, s *Store, person int64, ext, body string, wall time.Time, host int64) int64 {
	t.Helper()
	id, created, err := s.PutQuoted(Entry{
		Source: SourceMail, ExtID: "quote:" + ext, TS: wall, PersonID: person,
		Container: "thread-1", BodyText: body,
	})
	if err != nil {
		t.Fatalf("storing the recovered copy: %v", err)
	}
	if !created {
		t.Fatalf("recovered copy %s already existed", ext)
	}
	if err := s.Sight(id, host, "quoted", "depth 1"); err != nil {
		t.Fatal(err)
	}
	if err := Participate(s, id, person, RoleFrom); err != nil {
		t.Fatal(err)
	}
	return id
}

// host is a message that quoted something: what a sighting points at, and whose
// author's client rendered the clock a measurement comes from.
func host(t *testing.T, s *Store, person int64, ext string, at time.Time) int64 {
	t.Helper()
	off := 720
	res, err := s.Put(Entry{
		Source: SourceMail, ExtID: "mail:<" + ext + ">", TS: at, TZ: "NZST",
		TZOffset: &off, PersonID: person, Container: "thread-1",
		BodyText: "Confirming, see below.",
	}, &Mail{MessageID: "<" + ext + ">"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return res.ID
}

// The bug, at its simplest: one message in the mailbox and the same message
// recovered from a New Zealand reply, ten hours apart because that is where the
// clock was rendered. The mailbox copy survives whole.
func TestTwinCollapsesOntoTheMailboxCopy(t *testing.T) {
	s := open(t)
	deniz := person(t, s, "deniz.aslan@quarry.fed", "Deniz Aslan")
	tui := person(t, s, "tui.walker@moana.fed", "Tui Walker")
	sent := twinAt(t, "2026-05-20 01:38:14")

	keep := mailbox(t, s, deniz, "ask@quarry.fed", askBody, sent)
	h := host(t, s, tui, "reply@moana.fed", sent.Add(3*time.Hour))
	drop := recovered(t, s, deniz, "abc", askQuoted, sent.Add(10*time.Hour), h)

	plan, err := CollapseTwins(s, true)
	if err != nil {
		t.Fatalf("CollapseTwins: %v", err)
	}
	if len(plan.Collapse) != 1 || plan.Removed != 1 {
		t.Fatalf("plan = %d groups removing %d, want one group removing one\n%+v",
			len(plan.Collapse), plan.Removed, plan.Declined)
	}
	if plan.Collapse[0].Keep != keep {
		t.Fatalf("survivor is %d, want the mailbox copy %d", plan.Collapse[0].Keep, keep)
	}

	// The mailbox copy keeps everything the quoted one never had.
	var html, tz string
	var off, atts int
	if err := s.DB().QueryRow(`
		select coalesce(body_html,''), coalesce(tz,''), coalesce(tz_offset,0),
		       (select count(*) from attachments a where a.entry_id = e.id)
		from entries e where id=?`, keep).Scan(&html, &tz, &off, &atts); err != nil {
		t.Fatal(err)
	}
	if html == "" || tz != "AEST" || off != 600 || atts != 1 {
		t.Fatalf("survivor lost something: html %q tz %q off %d attachments %d",
			html, tz, off, atts)
	}
	var n int
	if err := s.DB().QueryRow(`select count(*) from entries where id=?`, drop).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatal("the quoted copy is still there")
	}

	// Its evidence is not lost: the sighting is now against the survivor, with
	// the host and the depth it was seen at.
	var seenIn int64
	var detail string
	if err := s.DB().QueryRow(
		`select seen_in, coalesce(detail,'') from sightings where entry_id=? and kind='quoted'`,
		keep).Scan(&seenIn, &detail); err != nil {
		t.Fatalf("the sighting did not move: %v", err)
	}
	if seenIn != h || detail != "depth 1" {
		t.Fatalf("sighting = host %d %q, want host %d \"depth 1\"", seenIn, detail, h)
	}

	// And the collapse measured what the quoter's client renders at.
	if plan.Measured != 1 {
		t.Fatalf("measured %d render offsets, want 1", plan.Measured)
	}
	var person64, offMins int64
	if err := s.DB().QueryRow(
		`select person_id, off from render_offsets`).Scan(&person64, &offMins); err != nil {
		t.Fatalf("no render offset recorded: %v", err)
	}
	if person64 != tui || offMins != 600 {
		t.Fatalf("offset = person %d at %d minutes, want person %d at 600",
			person64, offMins, tui)
	}
}

// A collapse must not sever a chain. The reply that quoted the twin points at
// the twin, and after the collapse it points at the survivor.
func TestCollapseRepointsTheChildrenOfTheDroppedCopy(t *testing.T) {
	s := open(t)
	deniz := person(t, s, "deniz.aslan@quarry.fed", "Deniz Aslan")
	tui := person(t, s, "tui.walker@moana.fed", "Tui Walker")
	sent := twinAt(t, "2026-05-20 01:38:14")

	keep := mailbox(t, s, deniz, "ask@quarry.fed", askBody, sent)
	h := host(t, s, tui, "reply@moana.fed", sent.Add(3*time.Hour))
	drop := recovered(t, s, deniz, "abc", askQuoted, sent.Add(10*time.Hour), h)
	// The reply replies to the recovered copy, as extraction's positional nesting
	// links it, and a deeper block sits under it.
	if err := s.SetParent(h, drop); err != nil {
		t.Fatal(err)
	}
	deeper := recovered(t, s, tui, "def", otherBody, sent.Add(-24*time.Hour), h)
	if err := s.SetParent(drop, deeper); err != nil {
		t.Fatal(err)
	}

	if _, err := CollapseTwins(s, true); err != nil {
		t.Fatalf("CollapseTwins: %v", err)
	}
	var parent int64
	if err := s.DB().QueryRow(`select parent_id from entries where id=?`, h).Scan(&parent); err != nil {
		t.Fatal(err)
	}
	if parent != keep {
		t.Fatalf("the reply now points at %d, want the survivor %d", parent, keep)
	}
	if err := s.DB().QueryRow(`select coalesce(parent_id,0) from entries where id=?`, keep).
		Scan(&parent); err != nil {
		t.Fatal(err)
	}
	if parent != deeper {
		t.Fatalf("the survivor's parent is %d, want the edge the dropped copy held, %d",
			parent, deeper)
	}
}

// The case an unordered comparison gets wrong: two questions a fortnight apart
// from one person whose signature and disclaimer are most of both. The gap
// between them is a plausible offset and the words mostly agree, so only the
// ordered opening separates them.
func TestTwoMessagesWithOneSignatureDoNotCollapse(t *testing.T) {
	s := open(t)
	deniz := person(t, s, "deniz.aslan@quarry.fed", "Deniz Aslan")
	tui := person(t, s, "tui.walker@moana.fed", "Tui Walker")
	sent := twinAt(t, "2026-05-20 01:38:14")

	mailbox(t, s, deniz, "other@quarry.fed", otherBody, sent)
	h := host(t, s, tui, "reply@moana.fed", sent.Add(3*time.Hour))
	drop := recovered(t, s, deniz, "abc", otherQuoted, sent.Add(10*time.Hour), h)

	plan, err := CollapseTwins(s, true)
	if err != nil {
		t.Fatalf("CollapseTwins: %v", err)
	}
	if len(plan.Collapse) != 0 {
		t.Fatalf("collapsed two different messages: %+v", plan.Collapse)
	}
	if len(plan.Declined) != 1 || plan.Declined[0].Entry != drop {
		t.Fatalf("declines = %+v, want the recovered copy left alone", plan.Declined)
	}
	var n int
	if err := s.DB().QueryRow(`select count(*) from entries`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("entries = %d, want all 3 still there", n)
	}
}

// A gap has to be an offset. Three hours and twenty minutes is nobody's clock,
// so however alike the words are the two are left alone.
func TestAnImplausibleGapDoesNotCollapse(t *testing.T) {
	s := open(t)
	deniz := person(t, s, "deniz.aslan@quarry.fed", "Deniz Aslan")
	tui := person(t, s, "tui.walker@moana.fed", "Tui Walker")
	sent := twinAt(t, "2026-05-20 01:38:14")

	mailbox(t, s, deniz, "ask@quarry.fed", askBody, sent)
	h := host(t, s, tui, "reply@moana.fed", sent.Add(6*time.Hour))
	recovered(t, s, deniz, "abc", askQuoted, sent.Add(3*time.Hour+20*time.Minute), h)

	plan, err := CollapseTwins(s, true)
	if err != nil {
		t.Fatalf("CollapseTwins: %v", err)
	}
	if len(plan.Collapse) != 0 {
		t.Fatalf("collapsed on a gap that is not an offset: %+v", plan.Collapse)
	}
	if len(plan.Declined) != 1 {
		t.Fatalf("declines = %+v, want one", plan.Declined)
	}
}

// Three copies of one message — the mailbox copy and two quoters an hour and a
// half apart — are one group, and the group is the same whichever order the
// copies were stored in.
func TestTriplicateConvergesWhateverTheOrder(t *testing.T) {
	build := func(t *testing.T, reverse bool) TwinPlan {
		s := open(t)
		deniz := person(t, s, "deniz.aslan@quarry.fed", "Deniz Aslan")
		tui := person(t, s, "tui.walker@moana.fed", "Tui Walker")
		sent := twinAt(t, "2026-05-20 01:38:14")
		keep := mailbox(t, s, deniz, "ask@quarry.fed", askBody, sent)
		h := host(t, s, tui, "reply@moana.fed", sent.Add(3*time.Hour))
		walls := []time.Duration{10 * time.Hour, 12*time.Hour + time.Minute}
		texts := []string{askQuoted, askQuoted + " | Operations | Quarry Energy"}
		if reverse {
			walls[0], walls[1] = walls[1], walls[0]
			texts[0], texts[1] = texts[1], texts[0]
		}
		recovered(t, s, deniz, "abc", texts[0], sent.Add(walls[0]), h)
		recovered(t, s, deniz, "def", texts[1], sent.Add(walls[1]), h)

		plan, err := CollapseTwins(s, true)
		if err != nil {
			t.Fatalf("CollapseTwins: %v", err)
		}
		if len(plan.Collapse) != 1 || plan.Removed != 2 {
			t.Fatalf("plan = %d groups removing %d, want one group removing two; declined %+v",
				len(plan.Collapse), plan.Removed, plan.Declined)
		}
		if plan.Collapse[0].Keep != keep {
			t.Fatalf("survivor is %d, want the mailbox copy %d", plan.Collapse[0].Keep, keep)
		}
		var n int
		if err := s.DB().QueryRow(`select count(*) from entries where quoted=1`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Fatalf("%d recovered copies survived the triplicate", n)
		}
		return plan
	}
	a, b := build(t, false), build(t, true)
	if a.Removed != b.Removed || len(a.Collapse) != len(b.Collapse) ||
		a.Collapse[0].Keep != b.Collapse[0].Keep || a.Measured != b.Measured {
		t.Fatalf("order changed the outcome:\n%+v\n%+v", a, b)
	}
}

// With no mailbox copy at all the fullest recovered copy survives, because
// quoting elides rather than invents — and the sighting of the copy that goes is
// kept, since two quoters saying the same thing is stronger provenance than one.
func TestQuotedOnlyTwinsKeepTheFullestCopyAndBothSightings(t *testing.T) {
	s := open(t)
	deniz := person(t, s, "deniz.aslan@quarry.fed", "Deniz Aslan")
	tui := person(t, s, "tui.walker@moana.fed", "Tui Walker")
	sent := twinAt(t, "2026-05-20 01:38:14")

	h1 := host(t, s, tui, "reply@moana.fed", sent.Add(20*time.Hour))
	h2 := host(t, s, deniz, "onward@quarry.fed", sent.Add(30*time.Hour))
	full := recovered(t, s, deniz, "abc", askBody, sent.Add(10*time.Hour), h1)
	recovered(t, s, deniz, "def", askQuoted, sent.Add(12*time.Hour), h2)

	plan, err := CollapseTwins(s, true)
	if err != nil {
		t.Fatalf("CollapseTwins: %v", err)
	}
	if len(plan.Collapse) != 1 || plan.Collapse[0].Keep != full {
		t.Fatalf("plan = %+v, want the fuller copy %d kept", plan.Collapse, full)
	}
	// No mailbox copy means no true instant, so nothing was measured.
	if plan.Measured != 0 {
		t.Fatalf("measured %d offsets with no mailbox copy to measure against", plan.Measured)
	}
	var hosts int
	if err := s.DB().QueryRow(
		`select count(*) from sightings where entry_id=? and kind='quoted'`, full).
		Scan(&hosts); err != nil {
		t.Fatal(err)
	}
	if hosts != 2 {
		t.Fatalf("the survivor is sighted in %d hosts, want both", hosts)
	}
}

// A run over an already-collapsed corpus finds nothing to do.
func TestCollapseIsIdempotent(t *testing.T) {
	s := open(t)
	deniz := person(t, s, "deniz.aslan@quarry.fed", "Deniz Aslan")
	tui := person(t, s, "tui.walker@moana.fed", "Tui Walker")
	sent := twinAt(t, "2026-05-20 01:38:14")
	mailbox(t, s, deniz, "ask@quarry.fed", askBody, sent)
	h := host(t, s, tui, "reply@moana.fed", sent.Add(3*time.Hour))
	recovered(t, s, deniz, "abc", askQuoted, sent.Add(10*time.Hour), h)

	if _, err := CollapseTwins(s, true); err != nil {
		t.Fatalf("first pass: %v", err)
	}
	before := entryCount(t, s)
	plan, err := CollapseTwins(s, true)
	if err != nil {
		t.Fatalf("second pass: %v", err)
	}
	if len(plan.Collapse) != 0 || plan.Removed != 0 {
		t.Fatalf("the second pass would collapse %+v", plan.Collapse)
	}
	if after := entryCount(t, s); after != before {
		t.Fatalf("entries went %d -> %d on a second pass", before, after)
	}
	// And the measurement is not duplicated by a second run.
	var n int
	if err := s.DB().QueryRow(`select count(*) from render_offsets`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("render offsets = %d, want the one the first pass measured", n)
	}
}

// The dry run writes nothing, and decides exactly what the apply would.
func TestPlanWithoutApplyWritesNothing(t *testing.T) {
	s := open(t)
	deniz := person(t, s, "deniz.aslan@quarry.fed", "Deniz Aslan")
	tui := person(t, s, "tui.walker@moana.fed", "Tui Walker")
	sent := twinAt(t, "2026-05-20 01:38:14")
	mailbox(t, s, deniz, "ask@quarry.fed", askBody, sent)
	h := host(t, s, tui, "reply@moana.fed", sent.Add(3*time.Hour))
	recovered(t, s, deniz, "abc", askQuoted, sent.Add(10*time.Hour), h)

	before := entryCount(t, s)
	dry, err := CollapseTwins(s, false)
	if err != nil {
		t.Fatalf("CollapseTwins: %v", err)
	}
	if dry.Applied || entryCount(t, s) != before {
		t.Fatalf("a dry run changed the corpus: %d -> %d", before, entryCount(t, s))
	}
	if dry.Removed != 1 || dry.Measured != 1 {
		t.Fatalf("dry run = %+v, want one removal and one measurement", dry)
	}
	wet, err := CollapseTwins(s, true)
	if err != nil {
		t.Fatal(err)
	}
	if wet.Removed != dry.Removed || wet.Measured != dry.Measured ||
		wet.Collapse[0].Keep != dry.Collapse[0].Keep {
		t.Fatalf("the apply decided something else:\n%+v\n%+v", dry, wet)
	}
}

// Two mailbox copies in one group would mean two messages that each carry their
// own Message-ID are the same message. Nothing here can say which gate is wrong
// about them, so all of it stays.
func TestGroupWithTwoMailboxCopiesIsRefused(t *testing.T) {
	s := open(t)
	deniz := person(t, s, "deniz.aslan@quarry.fed", "Deniz Aslan")
	tui := person(t, s, "tui.walker@moana.fed", "Tui Walker")
	sent := twinAt(t, "2026-05-20 01:38:14")

	// The same words sent twice, ten hours apart: a resend, and the recovered
	// copy sits at a plausible offset from each.
	mailbox(t, s, deniz, "ask@quarry.fed", askBody, sent)
	mailbox(t, s, deniz, "again@quarry.fed", askBody, sent.Add(10*time.Hour))
	h := host(t, s, tui, "reply@moana.fed", sent.Add(20*time.Hour))
	recovered(t, s, deniz, "abc", askQuoted, sent.Add(10*time.Hour), h)

	plan, err := CollapseTwins(s, true)
	if err != nil {
		t.Fatalf("CollapseTwins: %v", err)
	}
	if len(plan.Collapse) != 0 {
		t.Fatalf("collapsed a group holding two mailbox copies: %+v", plan.Collapse)
	}
	if len(plan.Declined) != 3 {
		t.Fatalf("declines = %+v, want all three copies named", plan.Declined)
	}
}

// A recovered copy short enough to match anything is left alone and said so.
func TestTooShortToIdentifyIsDeclined(t *testing.T) {
	s := open(t)
	deniz := person(t, s, "deniz.aslan@quarry.fed", "Deniz Aslan")
	tui := person(t, s, "tui.walker@moana.fed", "Tui Walker")
	sent := twinAt(t, "2026-05-20 01:38:14")

	mailbox(t, s, deniz, "ask@quarry.fed", "Thanks, will do.", sent)
	h := host(t, s, tui, "reply@moana.fed", sent.Add(3*time.Hour))
	drop := recovered(t, s, deniz, "abc", "Thanks, will do.", sent.Add(10*time.Hour), h)

	plan, err := CollapseTwins(s, false)
	if err != nil {
		t.Fatalf("CollapseTwins: %v", err)
	}
	if len(plan.Collapse) != 0 {
		t.Fatalf("collapsed on 3 words: %+v", plan.Collapse)
	}
	if len(plan.Declined) != 1 || plan.Declined[0].Entry != drop {
		t.Fatalf("declines = %+v, want the short copy named", plan.Declined)
	}
}

func entryCount(t *testing.T, s *Store) int {
	t.Helper()
	var n int
	if err := s.DB().QueryRow(`select count(*) from entries`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// What a collapse establishes beyond removing a duplicate: the quoter's own
// client rendered the clock, so the gap between the two copies IS that client's
// offset. This one has never stated anything but +0000 in a header — an Exchange
// account that does not know where it is — which placement alone can make
// nothing of, and the measurement places them.
func TestARenderOffsetPlacesAQuoterWhoseHeadersOnlySayUTC(t *testing.T) {
	s := open(t)
	deniz := person(t, s, "deniz.aslan@quarry.fed", "Deniz Aslan")
	tui := person(t, s, "tui.walker@moana.fed", "Tui Walker")
	sent := twinAt(t, "2026-05-20 01:38:14")

	mailbox(t, s, deniz, "ask@quarry.fed", askBody, sent)
	utc := 0
	res, err := s.Put(Entry{
		Source: SourceMail, ExtID: "mail:<reply@moana.fed>", TS: sent.Add(3 * time.Hour),
		TZ: "+0000", TZOffset: &utc, PersonID: tui, BodyText: "Confirming, see below.",
	}, &Mail{MessageID: "<reply@moana.fed>"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	recovered(t, s, deniz, "abc", askQuoted, sent.Add(12*time.Hour), res.ID)

	if before, err := s.Places(); err != nil {
		t.Fatal(err)
	} else if before[tui].Verdict != tzinfer.UTCOnly {
		t.Fatalf("the quoter starts %s, want utc-only", before[tui].Verdict)
	}
	if _, err := CollapseTwins(s, true); err != nil {
		t.Fatalf("CollapseTwins: %v", err)
	}
	after, err := s.Places()
	if err != nil {
		t.Fatal(err)
	}
	if after[tui].Verdict != tzinfer.Placed || after[tui].Seen[0] != 720 {
		t.Fatalf("the quoter is %s at %v, want placed at +1200",
			after[tui].Verdict, after[tui].Seen)
	}
}
