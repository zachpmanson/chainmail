package corpus

import (
	"strings"
	"testing"
	"time"
)

// The message one forward carries, and the shape of the forward: a header block
// whose LAST line is a wrapped recipient list, cut inside an address. Gmail
// re-renders it that way, with none of the leading whitespace RFC 5322 folding
// would have left (zpm/chainmail#146).
const (
	foldMessage = `Hiya Lane

Following on from our phone call this week I can confirm that you will receive
invoices for all of these accounts on the 20th, and that the statement date will
be the 20th of each month from September onward. Nothing else is needed from
you.

Thanks heaps,

Nia Coleridge | Energy Partner | Lodestar Energy`
	// The tail the old parser took as the message's opening.
	foldResidue = "sam@lodestar.example>, Zach Manson <zach@termina.example>\n\n"
)

func foldForwardBody() string {
	return "FYI\n\n---------- Forwarded message ---------\n" +
		"From: Nia Coleridge <nia@lodestar.example>\n" +
		"Date: Wed, 16 Sep 2026 at 8:14 PM\n" +
		"Subject: Fernbrook sites\n" +
		"To: Lena Whitfield <lane@termina.example>\n" +
		"Cc: nella@loomworks.example <nella@loomworks.example>, Bo Vantel <\n" +
		foldResidue + "\n" +
		foldMessage + "\n"
}

// foldStored is a corpus holding the three rows the ingest of one real trail
// produced: the mailbox copy of the message, the forward that quoted it, and the
// copy recovered out of the forward — with the body the OLD parser wrote, which
// opened on the header tail rather than on the message.
func foldStored(t *testing.T) (s *Store, mailboxID, hostID, quotedID int64, recoveredText string) {
	t.Helper()
	s = open(t)
	nia := person(t, s, "nia@lodestar.example", "Nia Coleridge")
	lane := person(t, s, "lane@termina.example", "Lena Whitfield")

	hostBody := foldForwardBody()
	q := RecoverQuotes(hostBody)
	if len(q.Distinct) != 1 {
		t.Fatalf("the forward recovered %d messages, want 1", len(q.Distinct))
	}
	rec := q.Distinct[0]
	recoveredText = rec.Block.Text
	if !strings.HasPrefix(recoveredText, "Hiya Lane") {
		t.Fatalf("extraction itself is wrong: %q", recoveredText)
	}

	off := 0
	mb, err := s.Put(Entry{
		Source: SourceMail, ExtID: "mail:<nia@lodestar.example>",
		TS: time.Date(2026, 9, 16, 8, 14, 46, 0, time.UTC), TZ: "+0000", TZOffset: &off,
		PersonID: nia, Container: "fernbrook", Subject: "Fernbrook sites",
		BodyText: foldMessage, BodyHTML: "<p>" + foldMessage + "</p>",
	}, &Mail{MessageID: "<nia@lodestar.example>"}, nil)
	if err != nil {
		t.Fatalf("storing the mailbox copy: %v", err)
	}
	host, err := s.Put(Entry{
		Source: SourceMail, ExtID: "mail:<lane@termina.example>",
		TS: time.Date(2026, 9, 16, 9, 4, 32, 0, time.UTC), TZ: "+1200",
		PersonID: lane, Container: "fernbrook", Subject: "Fernbrook sites",
		BodyText: hostBody,
	}, &Mail{MessageID: "<lane@termina.example>"}, nil)
	if err != nil {
		t.Fatalf("storing the forward: %v", err)
	}

	id, created, err := s.PutQuoted(Entry{
		Source: SourceMail, ExtID: rec.Key,
		TS: time.Date(2026, 9, 16, 20, 14, 0, 0, time.UTC),
		TZ: "", PersonID: nia, Container: "fernbrook", Subject: "Fernbrook sites",
		BodyText: foldResidue + recoveredText,
	})
	if err != nil || !created {
		t.Fatalf("storing the recovered copy: created=%v err=%v", created, err)
	}
	if err := s.Sight(id, host.ID, "quoted", "depth 0"); err != nil {
		t.Fatal(err)
	}
	return s, mb.ID, host.ID, id, recoveredText
}

func bodyOf(t *testing.T, s *Store, id int64) (string, string) {
	t.Helper()
	var body, sha string
	if err := s.DB().QueryRow(
		`select coalesce(body_text,''), body_sha from entries where id=?`, id).Scan(&body, &sha); err != nil {
		t.Fatalf("reading entry %d: %v", id, err)
	}
	return body, sha
}

// The repair itself: the row the old parser wrote is rewritten to what
// extraction says now, and the hash moves with it so the vector is re-embedded.
func TestRepairQuotedBodiesRewritesTheOldParsersText(t *testing.T) {
	s, _, _, quotedID, recovered := foldStored(t)

	rep, err := RepairQuotedBodies(s)
	if err != nil {
		t.Fatalf("RepairQuotedBodies: %v", err)
	}
	if rep.Fixed != 1 {
		t.Fatalf("fixed %d entries, want 1 (%v)", rep.Fixed, rep.Missing)
	}
	body, sha := bodyOf(t, s, quotedID)
	if !strings.HasPrefix(body, "Hiya Lane") {
		t.Errorf("body = %q, want it to open on the message", body)
	}
	// The hash is the embedder's staleness test, so it must be the new body's.
	if want := BodySHA("Fernbrook sites", recovered); sha != want {
		t.Errorf("body_sha = %s, want the re-derived body's %s", sha, want)
	}
	// The search index must follow the row, or the residue stays findable.
	var n int
	if err := s.DB().QueryRow(
		`select count(*) from entries_fts where rowid=? and entries_fts match 'termina'`,
		quotedID).Scan(&n); err != nil {
		t.Fatalf("querying the search index: %v", err)
	}
	if n != 0 {
		t.Error("the header tail is still indexed under the recovered entry")
	}

	// Idempotent: a second pass has nothing left to do, which is what makes it
	// safe to run on every page refresh.
	again, err := RepairQuotedBodies(s)
	if err != nil || again.Fixed != 0 {
		t.Fatalf("second pass fixed %d (%v), want 0", again.Fixed, err)
	}
}

// The reason the repair exists: the twin sweep can only judge the text it is
// given, so the duplicate the old parser's opening defeated collapses once the
// body is re-derived — and the quoter's render offset gets measured with it.
func TestTheRepairedCopyIsNoLongerDeclinedByTheTwinSweep(t *testing.T) {
	s, mailboxID, _, _, _ := foldStored(t)

	before, err := CollapseTwins(s, false)
	if err != nil {
		t.Fatalf("CollapseTwins: %v", err)
	}
	if len(before.Collapse) != 0 || len(before.Declined) != 1 {
		t.Fatalf("before the repair: %d collapsed, %d declined; want 0 and 1",
			len(before.Collapse), len(before.Declined))
	}
	if !strings.Contains(before.Declined[0].Reason, "not its opening") {
		t.Fatalf("declined for %q, want the opening test", before.Declined[0].Reason)
	}

	if _, err := RepairQuotedBodies(s); err != nil {
		t.Fatalf("RepairQuotedBodies: %v", err)
	}
	after, err := CollapseTwins(s, true)
	if err != nil {
		t.Fatalf("CollapseTwins after the repair: %v", err)
	}
	if len(after.Collapse) != 1 || after.Removed != 1 {
		t.Fatalf("after the repair: %d collapsed removing %d, want 1 removing 1\n%+v",
			len(after.Collapse), after.Removed, after.Declined)
	}
	if after.Collapse[0].Keep != mailboxID {
		t.Errorf("survivor is %d, want the mailbox copy %d", after.Collapse[0].Keep, mailboxID)
	}
	if after.Measured != 1 {
		t.Errorf("measured %d render offsets, want 1", after.Measured)
	}
}

// An entry its host no longer accounts for is reported, never resolved: the
// host's text may have been truncated, and deleting an entry on that evidence
// would lose a message nothing else holds.
func TestRepairQuotedBodiesLeavesAMissingEntryAlone(t *testing.T) {
	s, _, hostID, _, _ := foldStored(t)

	// A recovered entry whose host body contains no such block.
	lonely, created, err := s.PutQuoted(Entry{
		Source: SourceMail, ExtID: "quote:vanish", TS: time.Unix(1_700_000_000, 0),
		Container: "fernbrook", Subject: "Fernbrook sites", BodyText: "a message of its own",
	})
	if err != nil || !created {
		t.Fatalf("storing the stray entry: created=%v err=%v", created, err)
	}
	if err := s.Sight(lonely, hostID, "quoted", "depth 2"); err != nil {
		t.Fatal(err)
	}

	rep, err := RepairQuotedBodies(s)
	if err != nil {
		t.Fatalf("RepairQuotedBodies: %v", err)
	}
	if len(rep.Missing) != 1 || rep.Missing[0] != "quote:vanish" {
		t.Errorf("missing = %v, want the entry no host accounts for", rep.Missing)
	}
	if body, _ := bodyOf(t, s, lonely); body != "a message of its own" {
		t.Errorf("body = %q, want it untouched", body)
	}
}
