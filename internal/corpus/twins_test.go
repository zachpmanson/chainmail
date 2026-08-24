package corpus

import (
	"fmt"
	"strings"
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

// The bodies the positional test turns on. Every one of them is the same message
// as askBody; what differs is where the extra words sit.
const (
	// The case the decline exists for: a recipient answered one of the questions
	// where it stood, inside the text they were quoting. Their answer is in no
	// other entry — the mailbox copy predates it and their own body_html is peeled
	// away — so a collapse would delete it.
	askAnnotated = `Hi Priya, Can you confirm the meter number for the Rothwell depot before
the tender pack goes out this afternoon? It is on the last invoice, I will send
it over. The retailer wants it on the cover sheet and I do not want to guess at
it. Thanks, Deniz Aslan`
	// What a client adds on its own account, and where: after the shared text,
	// never inside it. This must still collapse — refusing it would leave a
	// duplicate standing for a footer.
	askWithChrome = askQuoted + `
Sent from a device that says so. This message was scanned for viruses by an
appliance that appends a paragraph about it, and delivered by a helpdesk that
appends another.`
	// The same message with an inline image and a link rendered the way a second
	// client renders them: both land inside the shared text, and both are
	// markup rather than words anybody typed.
	askRerendered = `Hi Priya, Can you confirm the meter number for the Rothwell depot
<https://depot.example.test/rothwell/meters?ref=8841&utm_source=mail> before
the tender pack goes out this afternoon? <Screenshot 2026-05-19 at 9.14.02 am.png>
The retailer wants it on the cover sheet and I do not want to guess at it.
Thanks, Deniz Aslan`
)

// The collapse that would destroy the answer. Both entries stay, and the decline
// says which copy holds what.
func TestAnAnnotatedCopyIsKeptAndSaysWhy(t *testing.T) {
	s := open(t)
	deniz := person(t, s, "deniz.aslan@quarry.fed", "Deniz Aslan")
	tui := person(t, s, "tui.walker@moana.fed", "Tui Walker")
	sent := twinAt(t, "2026-05-20 01:38:14")

	mailbox(t, s, deniz, "ask@quarry.fed", askBody, sent)
	h := host(t, s, tui, "reply@moana.fed", sent.Add(3*time.Hour))
	drop := recovered(t, s, deniz, "abc", askAnnotated, sent.Add(10*time.Hour), h)

	before := entryCount(t, s)
	plan, err := CollapseTwins(s, true)
	if err != nil {
		t.Fatalf("CollapseTwins: %v", err)
	}
	if len(plan.Collapse) != 0 || plan.Removed != 0 {
		t.Fatalf("collapsed a copy holding an inline answer: %+v", plan.Collapse)
	}
	if entryCount(t, s) != before {
		t.Fatalf("entries went %d -> %d", before, entryCount(t, s))
	}
	if plan.Annotated != 1 || len(plan.Declined) != 1 {
		t.Fatalf("plan = %d annotated of %d declined, want one of one",
			plan.Annotated, len(plan.Declined))
	}
	d := plan.Declined[0]
	if d.Entry != drop || !d.Annotated {
		t.Fatalf("decline = %+v, want the annotated copy %d marked", d, drop)
	}
	if !strings.Contains(d.Reason, "answered inside the text they quoted") {
		t.Fatalf("reason does not name the annotation: %q", d.Reason)
	}
	// And the words themselves are still in the corpus, which is the whole point.
	var n int
	if err := s.DB().QueryRow(
		`select count(*) from entries_fts where entries_fts match ?`,
		`"it is on the last invoice"`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		t.Fatal("the inserted answer is not findable")
	}
}

// A footer, a virus-scanner paragraph and a "sent from" line are extra words the
// survivor lacks, and they are not an annotation: they sit after the shared text,
// which is where a client appends and a person does not answer.
func TestAppendedChromeStillCollapses(t *testing.T) {
	s := open(t)
	deniz := person(t, s, "deniz.aslan@quarry.fed", "Deniz Aslan")
	tui := person(t, s, "tui.walker@moana.fed", "Tui Walker")
	sent := twinAt(t, "2026-05-20 01:38:14")

	keep := mailbox(t, s, deniz, "ask@quarry.fed", askBody, sent)
	h := host(t, s, tui, "reply@moana.fed", sent.Add(3*time.Hour))
	recovered(t, s, deniz, "abc", askWithChrome, sent.Add(10*time.Hour), h)

	plan, err := CollapseTwins(s, true)
	if err != nil {
		t.Fatalf("CollapseTwins: %v", err)
	}
	if len(plan.Collapse) != 1 || plan.Removed != 1 || plan.Annotated != 0 {
		t.Fatalf("plan = %d groups removing %d, %d annotated; want one collapse and "+
			"no annotation\n%+v", len(plan.Collapse), plan.Removed, plan.Annotated, plan.Declined)
	}
	if plan.Collapse[0].Keep != keep {
		t.Fatalf("survivor is %d, want the mailbox copy %d", plan.Collapse[0].Keep, keep)
	}
}

// Links, inline images and attachment names are written differently by every
// client, and the difference lands inside the shared text rather than after it —
// the same position an inline answer occupies. Reading them as an answer is how
// the measurement inflates, so they collapse.
func TestRerenderedMarkupInsideTheQuoteStillCollapses(t *testing.T) {
	s := open(t)
	deniz := person(t, s, "deniz.aslan@quarry.fed", "Deniz Aslan")
	tui := person(t, s, "tui.walker@moana.fed", "Tui Walker")
	sent := twinAt(t, "2026-05-20 01:38:14")

	mailbox(t, s, deniz, "ask@quarry.fed", askBody, sent)
	h := host(t, s, tui, "reply@moana.fed", sent.Add(3*time.Hour))
	recovered(t, s, deniz, "abc", askRerendered, sent.Add(10*time.Hour), h)

	plan, err := CollapseTwins(s, true)
	if err != nil {
		t.Fatalf("CollapseTwins: %v", err)
	}
	if len(plan.Collapse) != 1 || plan.Removed != 1 || plan.Annotated != 0 {
		t.Fatalf("plan = %d groups removing %d, %d annotated; want one collapse and "+
			"no annotation\n%+v", len(plan.Collapse), plan.Removed, plan.Annotated, plan.Declined)
	}
}

// The other direction is elision, not annotation: the copy that survives holds
// words the dropped one lacks, because quoting drops words. Nothing is lost by
// dropping the shorter copy, so it goes.
func TestACopyMissingWordsTheSurvivorHasStillCollapses(t *testing.T) {
	s := open(t)
	deniz := person(t, s, "deniz.aslan@quarry.fed", "Deniz Aslan")
	tui := person(t, s, "tui.walker@moana.fed", "Tui Walker")
	sent := twinAt(t, "2026-05-20 01:38:14")

	// The mailbox copy carries the sentence; the requote elided it from the middle,
	// which is the same position an annotation would occupy in the other copy.
	keep := mailbox(t, s, deniz, "ask@quarry.fed", askAnnotated, sent)
	h := host(t, s, tui, "reply@moana.fed", sent.Add(3*time.Hour))
	recovered(t, s, deniz, "abc", askQuoted, sent.Add(10*time.Hour), h)

	plan, err := CollapseTwins(s, true)
	if err != nil {
		t.Fatalf("CollapseTwins: %v", err)
	}
	if len(plan.Collapse) != 1 || plan.Removed != 1 || plan.Annotated != 0 {
		t.Fatalf("plan = %d groups removing %d, %d annotated; want one collapse and "+
			"no annotation\n%+v", len(plan.Collapse), plan.Removed, plan.Annotated, plan.Declined)
	}
	if plan.Collapse[0].Keep != keep {
		t.Fatalf("survivor is %d, want the fuller mailbox copy %d", plan.Collapse[0].Keep, keep)
	}
}

// One quoter answered inline and another requoted the same message untouched.
// The clean copy is still a duplicate and still goes; refusing the whole group
// would leave it standing for the sake of a copy it has nothing to do with.
func TestAnAnnotatedCopyDoesNotSaveACleanOneFromCollapsing(t *testing.T) {
	s := open(t)
	deniz := person(t, s, "deniz.aslan@quarry.fed", "Deniz Aslan")
	tui := person(t, s, "tui.walker@moana.fed", "Tui Walker")
	sent := twinAt(t, "2026-05-20 01:38:14")

	keep := mailbox(t, s, deniz, "ask@quarry.fed", askBody, sent)
	h := host(t, s, tui, "reply@moana.fed", sent.Add(3*time.Hour))
	annotated := recovered(t, s, deniz, "abc", askAnnotated, sent.Add(10*time.Hour), h)
	clean := recovered(t, s, deniz, "def", askQuoted, sent.Add(12*time.Hour), h)

	plan, err := CollapseTwins(s, true)
	if err != nil {
		t.Fatalf("CollapseTwins: %v", err)
	}
	if len(plan.Collapse) != 1 || plan.Removed != 1 {
		t.Fatalf("plan = %d groups removing %d, want one group removing the clean copy "+
			"only\n%+v", len(plan.Collapse), plan.Removed, plan.Declined)
	}
	if plan.Collapse[0].Keep != keep || plan.Collapse[0].Drop[0] != clean {
		t.Fatalf("collapse = %+v, want %d dropped into %d", plan.Collapse[0], clean, keep)
	}
	if plan.Annotated != 1 || plan.Declined[0].Entry != annotated {
		t.Fatalf("declined %+v, want the annotated copy %d kept", plan.Declined, annotated)
	}
	var n int
	if err := s.DB().QueryRow(`select count(*) from entries where id=?`, annotated).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatal("the annotated copy did not survive")
	}
}

// A second pass over a corpus holding an annotated copy decides the same thing
// again, and a dry run writes nothing.
func TestAnnotationDeclineIsIdempotentAndDryRunSafe(t *testing.T) {
	s := open(t)
	deniz := person(t, s, "deniz.aslan@quarry.fed", "Deniz Aslan")
	tui := person(t, s, "tui.walker@moana.fed", "Tui Walker")
	sent := twinAt(t, "2026-05-20 01:38:14")

	mailbox(t, s, deniz, "ask@quarry.fed", askBody, sent)
	h := host(t, s, tui, "reply@moana.fed", sent.Add(3*time.Hour))
	recovered(t, s, deniz, "abc", askAnnotated, sent.Add(10*time.Hour), h)
	recovered(t, s, deniz, "def", askQuoted, sent.Add(12*time.Hour), h)

	before := entryCount(t, s)
	dry, err := CollapseTwins(s, false)
	if err != nil {
		t.Fatal(err)
	}
	if entryCount(t, s) != before {
		t.Fatalf("the dry run changed the corpus: %d -> %d", before, entryCount(t, s))
	}
	if _, err := CollapseTwins(s, true); err != nil {
		t.Fatal(err)
	}
	again, err := CollapseTwins(s, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Collapse) != 0 || again.Removed != 0 {
		t.Fatalf("the second pass would collapse %+v", again.Collapse)
	}
	if again.Annotated != dry.Annotated {
		t.Fatalf("annotated went %d -> %d across passes", dry.Annotated, again.Annotated)
	}
}

// The shape the real case has: several numbered questions, and the answer to one
// of them written where that question stands. Six words, which is what the
// threshold has to see — and the requoting client renumbered the list, which is
// residue arriving at the same position and must not be counted with them.
const (
	askedThree = `Hi Priya,

A few things before the tender pack goes out.

Is the Rothwell meter number the one on the last invoice?
Can we send the pack by email, or do you want it in the portal?
Do you still want the branded copies going out to the sites directly?

Thanks,
Deniz Aslan | Operations | Quarry Energy`
	answeredThree = `Hi Priya, A few things before the tender pack goes out.
1. Is the Rothwell meter number the one on the last invoice?
2. Can we send the pack by email, or do you want it in the portal?
3. Do you still want the branded copies going out to the sites directly? - Yes,
please keep sending those directly.
Thanks, Deniz Aslan | Operations | Quarry Energy`
)

func TestAnInlineAnswerOfSixWordsIsKept(t *testing.T) {
	s := open(t)
	deniz := person(t, s, "deniz.aslan@quarry.fed", "Deniz Aslan")
	tui := person(t, s, "tui.walker@moana.fed", "Tui Walker")
	sent := twinAt(t, "2026-05-20 01:38:14")

	mailbox(t, s, deniz, "ask@quarry.fed", askedThree, sent)
	h := host(t, s, tui, "reply@moana.fed", sent.Add(3*time.Hour))
	drop := recovered(t, s, deniz, "abc", answeredThree, sent.Add(10*time.Hour), h)

	plan, err := CollapseTwins(s, true)
	if err != nil {
		t.Fatalf("CollapseTwins: %v", err)
	}
	if len(plan.Collapse) != 0 || plan.Annotated != 1 || plan.Declined[0].Entry != drop {
		t.Fatalf("plan collapsed %+v and declined %+v, want the annotated copy kept",
			plan.Collapse, plan.Declined)
	}
}

// The same refusal at the other end. Converging at extraction stores nothing but
// a sighting, so an answer written inside a newly recovered block would never
// reach the corpus at all — a loss the repair pass could not undo, because there
// would be no second row for it to decline.
func TestFindTwinWillNotConvergeAnAnnotatedBlock(t *testing.T) {
	s := open(t)
	deniz := person(t, s, "deniz.aslan@quarry.fed", "Deniz Aslan")
	sent := twinAt(t, "2026-05-20 01:38:14")
	keep := mailbox(t, s, deniz, "ask@quarry.fed", askBody, sent)

	if id, ok, err := FindTwin(s, deniz, sent.Add(10*time.Hour), askQuoted, ""); err != nil {
		t.Fatal(err)
	} else if !ok || id != keep {
		t.Fatalf("a clean requote found twin %d (%v), want the mailbox copy %d", id, ok, keep)
	}
	if id, ok, err := FindTwin(s, deniz, sent.Add(10*time.Hour), askAnnotated, ""); err != nil {
		t.Fatal(err)
	} else if ok {
		t.Fatalf("an annotated block converged onto entry %d, losing the answer", id)
	}
	// FindDerived is how that refusal is not lost: the same block is recognisably
	// a MODIFIED copy of the base it answered inside, not an unrelated message.
	if d, ok, err := FindDerived(s, deniz, sent.Add(10*time.Hour), askAnnotated, ""); err != nil {
		t.Fatal(err)
	} else if !ok {
		t.Fatalf("FindDerived did not recognise the annotated block as a copy")
	} else if d.Base != keep {
		t.Fatalf("derived base = %d, want the mailbox copy %d", d.Base, keep)
	}
}

// A machine-generated report is far too long to align, and there is nothing to
// place: its requote holds no word the mailbox copy lacks, so wherever those
// words sit nothing is lost by dropping it. Declining on length alone would leave
// a duplicate of every such report standing.
func TestALongCopyHoldingNothingExtraStillCollapses(t *testing.T) {
	s := open(t)
	deniz := person(t, s, "deniz.aslan@quarry.fed", "Deniz Aslan")
	tui := person(t, s, "tui.walker@moana.fed", "Tui Walker")
	sent := twinAt(t, "2026-05-20 01:38:14")

	var b strings.Builder
	b.WriteString(askBody)
	for i := 0; i < 900; i++ {
		fmt.Fprintf(&b, "\nrow %d meter QB0838%04d reading %d kwh", i, i, i*7)
	}
	long := b.String()

	keep := mailbox(t, s, deniz, "ask@quarry.fed", long, sent)
	h := host(t, s, tui, "reply@moana.fed", sent.Add(3*time.Hour))
	recovered(t, s, deniz, "abc", long, sent.Add(10*time.Hour), h)

	plan, err := CollapseTwins(s, true)
	if err != nil {
		t.Fatalf("CollapseTwins: %v", err)
	}
	if len(plan.Collapse) != 1 || plan.Removed != 1 || plan.Annotated != 0 {
		t.Fatalf("plan = %d groups removing %d, %d annotated; want one collapse\n%+v",
			len(plan.Collapse), plan.Removed, plan.Annotated, plan.Declined)
	}
	if plan.Collapse[0].Keep != keep {
		t.Fatalf("survivor is %d, want the mailbox copy %d", plan.Collapse[0].Keep, keep)
	}
}

// The pair that motivated the subject tier, anonymized: a short "example file
// attached" mail whose quoted copy renders the sender's clock in the quoter's
// +1200 zone while the mailbox copy stores the true instant (14 Apr 06:59 UTC
// vs a sentinel wall clock of 18:59), and whose quoted copy carries the same
// thread subject the mailbox copy does. Both copies are under the 25-word floor
// — the subject is what lets a message this short be identified at all.
func TestShortMailbackCopyVouchedByItsThreadCollapses(t *testing.T) {
	s := open(t)
	aria := person(t, s, "aria.meyer@acme.test", "Aria Meyer")
	dev := person(t, s, "dev.patel@northland.test", "Dev Patel")
	sent := twinAt(t, "2026-04-14 06:59:05")

	// The mailbox copy: the true instant, AEST, a real attachment.
	body := `Hi Dev,

I have attached an example data file here. Let me know how it goes.

Regards,
Aria`
	off := 600
	att := Attachment{Name: "sample.csv", Mime: "text/csv", Size: 717}
	res, err := s.Put(Entry{
		Source: SourceMail, ExtID: "mail:<csv-handoff@acme.test>", TS: sent,
		TZ: "AEST", TZOffset: &off, PersonID: aria, Container: "thread-9",
		Subject: "Meadow & Northland - Data Sharing", BodyText: body,
	}, &Mail{MessageID: "<csv-handoff@acme.test>"}, []Attachment{att})
	if err != nil {
		t.Fatalf("storing the mailbox copy: %v", err)
	}
	if err := Participate(s, res.ID, aria, RoleFrom); err != nil {
		t.Fatal(err)
	}

	// At extraction the same short message arrives as a quoted block, its clock
	// rendered by the quoter's +1200 client. The subject vouches for it, so the
	// ingest-time gate converges instead of storing a second entry.
	quoted := `Hi Dev, I have attached an example data file here. Let me know how it goes. Regards, Aria`
	wall := sent.Add(12 * time.Hour)
	if id, ok, err := FindTwin(s, aria, wall, quoted,
		"Re: Meadow & Northland - Data Sharing"); err != nil {
		t.Fatal(err)
	} else if !ok || id != res.ID {
		t.Fatalf("a vouched short requote found twin %d (%v), want the mailbox copy %d", id, ok, res.ID)
	}

	// The repair pass collapses the pair the same way, even if the mailbox copy
	// arrived after the quote was stored.
	h := host(t, s, dev, "reply@northland.test", sent.Add(14*time.Hour))
	drop, created, err := s.PutQuoted(Entry{
		Source: SourceMail, ExtID: "quote:csv-handoff@acme.test", TS: wall,
		PersonID: aria, Container: "thread-9",
		Subject: "Re: Meadow & Northland - Data Sharing", BodyText: quoted,
	})
	if err != nil {
		t.Fatalf("storing the recovered copy: %v", err)
	}
	if !created {
		t.Fatalf("recovered copy already existed")
	}
	if err := s.Sight(drop, h, "quoted", "depth 1"); err != nil {
		t.Fatal(err)
	}
	if err := Participate(s, drop, aria, RoleFrom); err != nil {
		t.Fatal(err)
	}

	plan, err := CollapseTwins(s, true)
	if err != nil {
		t.Fatalf("CollapseTwins: %v", err)
	}
	if len(plan.Collapse) != 1 || plan.Removed != 1 || plan.Annotated != 0 {
		t.Fatalf("plan = %d groups removing %d, %d annotated; want one collapse\n%+v",
			len(plan.Collapse), plan.Removed, plan.Annotated, plan.Declined)
	}
	if plan.Collapse[0].Keep != res.ID {
		t.Fatalf("survivor is %d, want the mailbox copy %d", plan.Collapse[0].Keep, res.ID)
	}
	var n int
	if err := s.DB().QueryRow(`select count(*) from entries where id=?`, drop).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatal("the recovered copy is still there")
	}
	var seenIn int64
	if err := s.DB().QueryRow(
		`select seen_in from sightings where entry_id=? and kind='quoted'`,
		res.ID).Scan(&seenIn); err != nil {
		t.Fatalf("the sighting did not move to the survivor: %v", err)
	}
	if seenIn != h {
		t.Fatalf("sighting = host %d, want %d", seenIn, h)
	}
}

// The failure the floor still stops: two different short messages from one
// person on two different threads, same words, clocks a plausible offset apart.
// The subjects disagree, so neither the ingest gate nor the repair pass may
// claim they are one message — a wrong collapse would delete a real email.
func TestShortCopyFromAnotherThreadIsStillDeclined(t *testing.T) {
	s := open(t)
	aria := person(t, s, "aria.meyer@acme.test", "Aria Meyer")
	dev := person(t, s, "dev.patel@northland.test", "Dev Patel")
	sent := twinAt(t, "2026-04-14 06:59:05")

	// A real "Thanks, will do." on the data-sharing thread, in the mailbox.
	res, err := s.Put(Entry{
		Source: SourceMail, ExtID: "mail:<thanks@acme.test>", TS: sent,
		TZ: "AEST", TZOffset: &[]int{600}[0], PersonID: aria, Container: "thread-9",
		Subject: "Meadow & Northland - Data Sharing", BodyText: "Thanks, will do.",
	}, &Mail{MessageID: "<thanks@acme.test>"}, nil)
	if err != nil {
		t.Fatalf("storing the mailbox copy: %v", err)
	}
	if err := Participate(s, res.ID, aria, RoleFrom); err != nil {
		t.Fatal(err)
	}

	// A different thread, same words, same sender: the quoter's clock renders
	// it a plausible offset away, which is exactly the trap the floor exists for.
	quoted := "Thanks, will do."
	wall := sent.Add(12 * time.Hour)
	if id, ok, err := FindTwin(s, aria, wall, quoted, "Re: August invoice"); err != nil {
		t.Fatal(err)
	} else if ok {
		t.Fatalf("a short block from another thread converged onto entry %d", id)
	}

	h := host(t, s, dev, "reply@northland.test", sent.Add(14*time.Hour))
	drop, created, err := s.PutQuoted(Entry{
		Source: SourceMail, ExtID: "quote:thanks-august@acme.test", TS: wall,
		PersonID: aria, Container: "thread-11",
		Subject: "Re: August invoice", BodyText: quoted,
	})
	if err != nil {
		t.Fatalf("storing the recovered copy: %v", err)
	}
	if !created {
		t.Fatal("recovered copy already existed")
	}
	if err := s.Sight(drop, h, "quoted", "depth 1"); err != nil {
		t.Fatal(err)
	}
	if err := Participate(s, drop, aria, RoleFrom); err != nil {
		t.Fatal(err)
	}

	plan, err := CollapseTwins(s, false)
	if err != nil {
		t.Fatalf("CollapseTwins: %v", err)
	}
	if len(plan.Collapse) != 0 || plan.Removed != 0 {
		t.Fatalf("collapsed two different messages: %+v", plan.Collapse)
	}
	if len(plan.Declined) != 1 || plan.Declined[0].Entry != drop {
		t.Fatalf("declines = %+v, want the short copy %d named", plan.Declined, drop)
	}
	if !strings.Contains(plan.Declined[0].Reason, "25 words") {
		t.Fatalf("the decline does not name the floor: %q", plan.Declined[0].Reason)
	}
	var n int
	if err := s.DB().QueryRow(`select count(*) from entries where id=?`, drop).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("the second message did not survive (entries where id=%d is %d)", drop, n)
	}
}
