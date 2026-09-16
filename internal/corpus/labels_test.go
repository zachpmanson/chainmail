package corpus

import (
	"strings"
	"testing"
)

// A folder is a label, and a chain is in it when any of its messages is. The
// reply that went out under SENT does not take the thread out of the inbox its
// first message landed in — that is the reading a mail client has, and the one
// a reader expects when they click INBOX.
func TestALabelSelectsTheChainsWhoseMessagesCarryIt(t *testing.T) {
	s := open(t)
	// One conversation: a message that arrived, and its reply, which was sent.
	put(t, s, msg{id: "<root@example.com>", subject: "the rebate statement",
		body: "the rebate statement", labels: []string{"INBOX", "IMPORTANT"}})
	put(t, s, msg{id: "<reply@example.com>", parent: "<root@example.com>",
		subject: "Re: the rebate statement", body: "the rebate statement",
		labels: []string{"SENT"}})
	// A second conversation that is only ever outbound.
	put(t, s, msg{id: "<sent@example.com>", subject: "the rebate statement",
		body: "the rebate statement", labels: []string{"SENT"}})

	for _, tc := range []struct {
		label string
		want  int // chains
	}{
		// The reply carries SENT too, so the first chain is in this folder as
		// well; a chain is returned once however many of its messages match.
		{"INBOX", 1},
		{"SENT", 2},
		{"IMPORTANT", 1},
		// A label nobody used is an empty folder, not an error and not
		// everything.
		{"CATEGORY_PROMOTIONS", 0},
	} {
		hits, err := s.SearchChains(Query{Text: "rebate", Labels: []string{tc.label}})
		if err != nil {
			t.Fatalf("%s: %v", tc.label, err)
		}
		if len(hits) != tc.want {
			t.Errorf("label %s: %d chains, want %d", tc.label, len(hits), tc.want)
		}
	}
}

// The label is the mailbox's own text, so it may contain the characters LIKE
// treats as wildcards. A label is matched literally or the filter lies: "50% off"
// would otherwise select every message whose labels merely contain "50", and a
// bare "%" would select the whole corpus.
func TestALabelIsMatchedLiterallyNotAsAPattern(t *testing.T) {
	s := open(t)
	put(t, s, msg{id: "<sale@example.com>", body: "the rebate statement",
		labels: []string{"50% off"}})
	put(t, s, msg{id: "<unrelated@example.com>", body: "the rebate statement",
		labels: []string{"5031 route"}})

	hits, err := s.SearchChains(Query{Text: "rebate", Labels: []string{"50% off"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 {
		t.Fatalf("the label 50%% off selected %d chains, want 1", len(hits))
	}
	hits, err = s.SearchChains(Query{Text: "rebate", Labels: []string{"%"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		t.Fatalf("a label of %% selected %d chains, want none: it is not a pattern", len(hits))
	}
}

// The folder list is the mailbox's, with its own counts: a label is counted on
// the messages that carry it, and an entry with no labels — every Slack message,
// and any mail the mailbox never held a copy of — is in no folder.
func TestLabelsListsTheMailboxsOwnFolders(t *testing.T) {
	s := open(t)
	slackMsg(t, s, "slack:C1:1", "the rebate statement", june, false, "")
	put(t, s, msg{id: "<a@example.com>", body: "one",
		labels: []string{"INBOX", "IMPORTANT"}})
	put(t, s, msg{id: "<b@example.com>", body: "two",
		labels: []string{"INBOX"}})
	put(t, s, msg{id: "<c@example.com>", body: "three"})

	got, err := s.Labels()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int{"INBOX": 2, "IMPORTANT": 1}
	if len(got) != len(want) {
		t.Fatalf("labels = %v, want %v", got, want)
	}
	for _, l := range got {
		if want[l.Name] != l.Messages {
			t.Errorf("%s: %d messages, want %d", l.Name, l.Messages, want[l.Name])
		}
	}
	// Busiest first, so the list a reader opens leads with the folder they use.
	if got[0].Name != "INBOX" {
		t.Errorf("the list leads with %s, want the busiest label INBOX", got[0].Name)
	}
}

// Two reads of an unchanged corpus print the same list, tie or no tie: a
// sidebar that reordered itself between reloads would be a list to re-read
// rather than to use.
func TestTheFolderListIsOrderedTheSameWayEveryTime(t *testing.T) {
	s := open(t)
	put(t, s, msg{id: "<a@example.com>", body: "one",
		labels: []string{"STARRED", "INBOX", "IMPORTANT"}})

	first, err := s.Labels()
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.Labels()
	if err != nil {
		t.Fatal(err)
	}
	var a, b []string
	for _, l := range first {
		a = append(a, l.Name)
	}
	for _, l := range second {
		b = append(b, l.Name)
	}
	if strings.Join(a, ",") != strings.Join(b, ",") {
		t.Errorf("two reads disagree: %v then %v — all three labels hold one message", a, b)
	}
	if strings.Join(a, ",") != "IMPORTANT,INBOX,STARRED" {
		t.Errorf("equal counts should be broken by name, got %v", a)
	}
}
