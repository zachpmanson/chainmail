package corpus

import "testing"

func TestGmailMessageURLKnownAccountAndLegacyFallback(t *testing.T) {
	if got := GmailMessageURL("copy-A", "selected+mail@example.test"); got != "https://mail.google.com/mail/?authuser=selected%2Bmail%40example.test#all/copy-A" {
		t.Fatal(got)
	}
	if got := GmailMessageURL("copy-legacy", ""); got != "https://mail.google.com/mail/#all/copy-legacy" {
		t.Fatal(got)
	}
	s := open(t)
	if err := s.PutGmailAccount(GmailAccount{ID: "legacy", Email: "selected+mail@example.test", DisplayName: "Selected"}); err != nil {
		t.Fatal(err)
	}
	putReactionTarget(t, s, "mail:<synthetic-target@example.test>", "target", "<synthetic-target@example.test>")
	e, m := corpusReactionFixture(t)
	if _, err := s.Put(e, m, nil); err != nil {
		t.Fatal(err)
	}
	groups, _, err := s.ReactionsFor("legacy", []string{"mail:<synthetic-target@example.test>"})
	if err != nil {
		t.Fatal(err)
	}
	if link := groups["mail:<synthetic-target@example.test>"][0].Events[0].SourceLink; link != GmailMessageURL(m.GmailID, "selected+mail@example.test") {
		t.Fatal(link)
	}
}
