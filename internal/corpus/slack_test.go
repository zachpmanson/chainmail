package corpus

import (
	"strings"
	"testing"
	"time"
)

func slackEntry(channel, ts, parentRef, body string) (Entry, Slack) {
	e := Entry{
		Source: SourceSlack, ExtID: "slack:" + channel + ":" + ts,
		TS: time.Unix(1_700_000_000, 0), Container: channel,
		ParentRef: parentRef, BodyText: body,
	}
	return e, Slack{ChannelID: channel, TS: ts, ThreadTS: parentRef}
}

func TestPutSlackWritesDetailAndThenSkips(t *testing.T) {
	s := open(t)
	e, d := slackEntry("C1", "1.1", "", "hello")

	first, err := s.PutSlack(e, d, nil)
	if err != nil {
		t.Fatalf("PutSlack: %v", err)
	}
	if !first.Created {
		t.Fatalf("first put: %+v, want created", first)
	}

	second, err := s.PutSlack(e, d, nil)
	if err != nil {
		t.Fatalf("second PutSlack: %v", err)
	}
	if second.Created || second.Changed || second.ID != first.ID {
		t.Fatalf("re-put: %+v, want the same entry untouched", second)
	}

	var details int
	if err := s.DB().QueryRow(
		`select count(*) from slack_detail where entry_id=?`, first.ID).Scan(&details); err != nil {
		t.Fatal(err)
	}
	if details != 1 {
		t.Fatalf("%d slack_detail rows for one entry", details)
	}
}

func TestPutSlackParticipationFollowsPersistedAuthor(t *testing.T) {
	s := open(t)
	first, err := Resolve(s, KindSlackUID, "U100", "Ada")
	if err != nil {
		t.Fatal(err)
	}
	second, err := Resolve(s, KindEmail, "ada@example.test", "Ada")
	if err != nil {
		t.Fatal(err)
	}
	e, d := slackEntry("C1", "1.1", "", "unchanged text")
	for _, tc := range []struct {
		name           string
		incoming, want int64
	}{
		{"unauthored", 0, 0},
		{"author found", first, first},
		{"author updated", second, second},
		{"author omitted", 0, second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e.PersonID = tc.incoming
			res, err := s.PutSlack(e, d, nil)
			if err != nil {
				t.Fatal(err)
			}
			if res.Changed {
				t.Fatalf("author-only update reported a body edit: %+v", res)
			}
			var author, participant int64
			var n int
			if err := s.DB().QueryRow(`select coalesce(person_id,0) from entries where id=?`, res.ID).Scan(&author); err != nil {
				t.Fatal(err)
			}
			if err := s.DB().QueryRow(`select count(*), coalesce(min(person_id),0) from participants
				where entry_id=? and role='from'`, res.ID).Scan(&n, &participant); err != nil {
				t.Fatal(err)
			}
			wantCount := 1
			if tc.want == 0 {
				wantCount = 0
			}
			if author != tc.want || participant != tc.want || n != wantCount {
				t.Fatalf("author=%d participant=%d count=%d, want person=%d count=%d", author, participant, n, tc.want, wantCount)
			}
		})
	}
}

func TestPutSlackParticipationFailureRollsBackMetadata(t *testing.T) {
	s := open(t)
	first, err := Resolve(s, KindSlackUID, "U100", "Ada")
	if err != nil {
		t.Fatal(err)
	}
	second, err := Resolve(s, KindEmail, "ada@example.test", "Ada")
	if err != nil {
		t.Fatal(err)
	}
	e, d := slackEntry("C1", "1.1", "", "unchanged text")
	e.PersonID = first
	res, err := s.PutSlack(e, d, []Attachment{{Name: "original.txt", SourceRef: "F1"}})
	if err != nil {
		t.Fatal(err)
	}
	// Also models a row created before participation moved into PutSlack.
	if err := Participate(s, res.ID, first, RoleFrom); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB().Exec(`create trigger reject_slack_participant before insert on participants
		when new.role='from' begin select raise(abort, 'injected participant failure'); end`); err != nil {
		t.Fatal(err)
	}
	e.PersonID = second
	d.ReplyCount = 2
	if _, err := s.PutSlack(e, d, []Attachment{{Name: "replacement.txt", SourceRef: "F2"}}); err == nil || !strings.Contains(err.Error(), "injected participant failure") {
		t.Fatalf("want participation failure, got %v", err)
	}
	var author, participant int64
	var replies int
	var name string
	if err := s.DB().QueryRow(`select e.person_id, sd.reply_count, a.name, p.person_id
		from entries e join slack_detail sd on sd.entry_id=e.id
		join attachments a on a.entry_id=e.id join participants p on p.entry_id=e.id
		where e.id=? and p.role='from'`, res.ID).Scan(&author, &replies, &name, &participant); err != nil {
		t.Fatal(err)
	}
	if author != first || participant != first || replies != 0 || name != "original.txt" {
		t.Fatalf("partial commit: author=%d participant=%d replies=%d attachment=%q", author, participant, replies, name)
	}
}

// The detail row and its entry are written in one transaction. A missing detail
// row is not self-healing on a later run — the entry already exists — so the
// only defence is that it cannot be committed apart from the entry.
func TestPutSlackDetailIsNeverMissing(t *testing.T) {
	s := open(t)
	e, d := slackEntry("C1", "1.1", "", "hello")
	if _, err := s.PutSlack(e, d, nil); err != nil {
		t.Fatal(err)
	}
	var orphans int
	if err := s.DB().QueryRow(`
		select count(*) from entries e
		where e.source='slack'
		  and not exists (select 1 from slack_detail sd where sd.entry_id = e.id)`).
		Scan(&orphans); err != nil {
		t.Fatal(err)
	}
	if orphans != 0 {
		t.Fatalf("%d slack entries with no detail row", orphans)
	}
}

// Two resolvers, one per source, and neither may touch the other's edges: a ts
// is not a Message-ID, and matching across the two would be a guess dressed as
// a fact.
func TestSlackAndMailParentResolutionStayApart(t *testing.T) {
	s := open(t)

	parent, d := slackEntry("C1", "1.1", "", "the question")
	if _, err := s.PutSlack(parent, d, nil); err != nil {
		t.Fatal(err)
	}
	child, cd := slackEntry("C1", "2.2", "1.1", "the answer")
	childRes, err := s.PutSlack(child, cd, nil)
	if err != nil {
		t.Fatal(err)
	}

	// A mail reply naming a Message-ID that is not in the corpus: a known hole,
	// and it must stay one.
	mailChild := Entry{
		Source: SourceMail, ExtID: "mail:<b@x>", TS: time.Unix(1_700_000_000, 0),
		ParentRef: "<a@x>", BodyText: "mail reply",
	}
	if _, err := s.Put(mailChild, &Mail{MessageID: "<b@x>"}, nil); err != nil {
		t.Fatal(err)
	}

	n, err := s.ResolveParents()
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("the mail resolver linked %d edges; it has none to link", n)
	}

	n, err = s.ResolveSlackParents()
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("slack resolver linked %d edges, want 1", n)
	}
	var parentID int64
	if err := s.DB().QueryRow(`select parent_id from entries where id=?`, childRes.ID).
		Scan(&parentID); err != nil {
		t.Fatal(err)
	}
	if parentID == 0 {
		t.Fatal("the slack reply is still a root")
	}

	// Re-running resolves nothing further, so a top-up costs one statement.
	if n, err := s.ResolveSlackParents(); err != nil || n != 0 {
		t.Fatalf("second resolve: %d edges, err %v", n, err)
	}
}

// A ts is unique within a channel only. An unscoped match would hang this reply
// off the identically-stamped message in the other channel.
func TestSlackParentResolutionIsScopedToTheChannel(t *testing.T) {
	s := open(t)
	elsewhere, ed := slackEntry("C2", "1.1", "", "same ts, other channel")
	if _, err := s.PutSlack(elsewhere, ed, nil); err != nil {
		t.Fatal(err)
	}
	child, cd := slackEntry("C1", "2.2", "1.1", "reply in C1")
	res, err := s.PutSlack(child, cd, nil)
	if err != nil {
		t.Fatal(err)
	}

	n, err := s.ResolveSlackParents()
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("linked %d edges across channels", n)
	}
	var parent *int64
	if err := s.DB().QueryRow(`select parent_id from entries where id=?`, res.ID).
		Scan(&parent); err != nil {
		t.Fatal(err)
	}
	if parent != nil {
		t.Fatalf("reply in C1 was given a parent in C2 (%d)", *parent)
	}
	// The parent_ref survives unresolved, which is how a known hole stays visible.
	st, err := s.Stats()
	if err != nil {
		t.Fatal(err)
	}
	if st.Unresolved != 1 {
		t.Fatalf("unresolved %d, want 1", st.Unresolved)
	}
}

// parent_ref means a Message-ID in mail and a thread_ts in Slack. Each resolver
// must stay inside its own source, or a coincidence between the two id spaces
// welds two unrelated conversations into one chain.
func TestParentResolutionStaysWithinItsSource(t *testing.T) {
	s := open(t)
	shared := "1780013450.965519" // a ts, planted as a mail Message-ID too

	mailEntry := Entry{
		Source: SourceMail, ExtID: "mail:<host@x>", Kind: "message",
		TS: time.Unix(1780013400, 0), BodyText: "the mail one",
	}
	if _, err := s.Put(mailEntry, &Mail{MessageID: shared}, nil); err != nil {
		t.Fatal(err)
	}
	// A Slack message whose thread_ts collides with that Message-ID.
	if _, err := s.PutSlack(Entry{
		Source: SourceSlack, ExtID: "slack:C1:1780013460.000100", Kind: "message",
		TS: time.Unix(1780013460, 0), ParentRef: shared, BodyText: "the slack one",
	}, Slack{ChannelID: "C1", TS: "1780013460.000100", ThreadTS: shared}, nil); err != nil {
		t.Fatal(err)
	}

	if _, err := s.ResolveParents(); err != nil {
		t.Fatal(err)
	}
	var parent *int64
	if err := s.DB().QueryRow(
		`select parent_id from entries where ext_id='slack:C1:1780013460.000100'`).
		Scan(&parent); err != nil {
		t.Fatal(err)
	}
	if parent != nil {
		t.Errorf("the mail resolver gave a Slack entry a mail parent (%d)", *parent)
	}
}
