package corpus

import (
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
