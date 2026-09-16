package corpus

import (
	"reflect"
	"testing"
)

// Everything here is invented, like the rest of this package's fixtures.

// labels reads one message's stored labels back out, so a test asserts what the
// store holds rather than what a caller passed in.
func labels(t *testing.T, s *Store, ext string) []string {
	t.Helper()
	entries, err := s.ChainEntries(ext)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.ExtID == ext {
			return e.Labels
		}
	}
	t.Fatalf("no entry %q in the corpus", ext)
	return nil
}

// The set a chain's read-state write touches is every entry reachable from the
// root, and the mailbox copy is what makes an entry touchable at all: a message
// recovered from somebody's quote is part of the chain and has no Gmail id, so it
// comes back as a row with nothing to change. A walk that returned only the
// mailbox copies would report a chain as fully marked when a third of it cannot
// be, and a walk that returned only the root would mark one message of twelve.
func TestChainEntriesWalkTheGraphAndNameWhatHasNoMailboxCopy(t *testing.T) {
	s := open(t)
	put(t, s, msg{id: "a@example.com", gmail: "g-a", subject: "invoice"})
	put(t, s, msg{id: "b@example.com", parent: "a@example.com", gmail: "g-b"})
	// A recovered entry hangs off the trail by parent_ref, with no mail_detail
	// row of its own: exactly what the unnest pass stores for quoted text.
	if _, err := s.Put(Entry{
		Source: SourceMail, ExtID: "quote:deadbeef", TS: july,
		ParentRef: "b@example.com", BodyText: "the quoted reply",
	}, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ResolveParents(); err != nil {
		t.Fatal(err)
	}

	entries, err := s.ChainEntries("mail:a@example.com")
	if err != nil {
		t.Fatal(err)
	}
	var ext []string
	byExt := map[string]ChainEntry{}
	for _, e := range entries {
		ext = append(ext, e.ExtID)
		byExt[e.ExtID] = e
	}
	want := []string{"mail:a@example.com", "mail:b@example.com", "quote:deadbeef"}
	if !reflect.DeepEqual(ext, want) {
		t.Fatalf("chain = %v, want %v (oldest first, root included)", ext, want)
	}
	if got := byExt["quote:deadbeef"].GmailID; got != "" {
		t.Errorf("the recovered entry claims a mailbox copy: %q", got)
	}
	if got := byExt["mail:b@example.com"].GmailID; got != "g-b" {
		t.Errorf("gmail id = %q, want g-b", got)
	}
}

// An unknown root is an empty answer rather than an error: the handler has to
// tell "no such chain" from "a chain with nothing markable in it", and both are
// answered from the same call.
func TestAnUnknownChainIsEmptyNotAnError(t *testing.T) {
	s := open(t)
	entries, err := s.ChainEntries("mail:nobody@example.com")
	if err != nil {
		t.Fatalf("ChainEntries: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("chain = %v, want nothing", entries)
	}
}

// Both directions, because the mirror can be wrong in both: a message read since
// it was ingested carries a UNREAD the mailbox has dropped, and a message marked
// unread somewhere else carries no UNREAD at all. Agreeing rows are left exactly
// as they are — not rewritten to the same bytes — because a labels rewrite is how
// the order of a message's other labels would be churned for nothing.
func TestReconcileUnreadCorrectsBothDirections(t *testing.T) {
	s := open(t)
	put(t, s, msg{id: "stale@example.com", gmail: "g-stale",
		labels: []string{"INBOX", "UNREAD", "IMPORTANT"}})
	put(t, s, msg{id: "fresh@example.com", gmail: "g-fresh",
		labels: []string{"INBOX"}})
	put(t, s, msg{id: "agreed@example.com", gmail: "g-agreed",
		labels: []string{"INBOX", "UNREAD"}})
	// A message with no mailbox copy: the mailbox's unread set cannot mention it,
	// and clearing a label it does not have would be inventing an answer about a
	// message Gmail has never heard of.
	if _, err := s.Put(Entry{Source: SourceMail, ExtID: "quote:cafe", TS: july}, nil, nil); err != nil {
		t.Fatal(err)
	}

	r, err := s.ReconcileUnread([]string{"g-fresh", "g-agreed"})
	if err != nil {
		t.Fatal(err)
	}
	if r.Checked != 3 || r.Marked != 1 || r.Cleared != 1 {
		t.Errorf("reconcile = %+v, want 3 checked, 1 marked, 1 cleared", r)
	}
	if got, want := labels(t, s, "mail:stale@example.com"), []string{"INBOX", "IMPORTANT"}; !reflect.DeepEqual(got, want) {
		t.Errorf("stale labels = %v, want %v", got, want)
	}
	if got, want := labels(t, s, "mail:fresh@example.com"), []string{"INBOX", "UNREAD"}; !reflect.DeepEqual(got, want) {
		t.Errorf("fresh labels = %v, want %v", got, want)
	}
	if got, want := labels(t, s, "mail:agreed@example.com"), []string{"INBOX", "UNREAD"}; !reflect.DeepEqual(got, want) {
		t.Errorf("agreed labels = %v, want %v", got, want)
	}
	if got := labels(t, s, "quote:cafe"); len(got) != 0 {
		t.Errorf("a recovered entry was labelled: %v", got)
	}
}

// SetUnread is the local half of the write, and it is the mailbox's list that is
// being edited: every other label survives, in the order it arrived, and UNREAD
// goes on the end rather than into a sort order nobody asked for.
func TestSetUnreadKeepsTheOtherLabels(t *testing.T) {
	labels := []string{"INBOX", "CATEGORY_PERSONAL"}
	if got, want := SetUnread(labels, true), []string{"INBOX", "CATEGORY_PERSONAL", "UNREAD"}; !reflect.DeepEqual(got, want) {
		t.Errorf("marking unread = %v, want %v", got, want)
	}
	back := SetUnread(SetUnread(labels, true), false)
	if !reflect.DeepEqual(back, labels) {
		t.Errorf("unread then read = %v, want %v back", back, labels)
	}
	if Unread(labels) || !Unread(SetUnread(labels, true)) {
		t.Error("Unread disagrees with SetUnread")
	}
	// Idempotent in both directions: marking an unread message unread must not
	// stack a second UNREAD onto it.
	twice := SetUnread(SetUnread(labels, true), true)
	if !reflect.DeepEqual(twice, SetUnread(labels, true)) {
		t.Errorf("marking unread twice = %v", twice)
	}
}

// A row that has no mailbox copy cannot be given labels: mail_detail rows are the
// ingest's to create, and a labels write that could create one would be a way to
// invent a mailbox copy of a message that has none.
func TestLabelsCannotBeSetOnAMessageWithNoMailboxCopy(t *testing.T) {
	s := open(t)
	if _, err := s.Put(Entry{Source: SourceMail, ExtID: "quote:feed", TS: july}, nil, nil); err != nil {
		t.Fatal(err)
	}
	entries, err := s.ChainEntries("quote:feed")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetLabels(entries[0].ID, []string{"UNREAD"}); err == nil {
		t.Error("SetLabels invented a mailbox copy")
	}
}

// The badge's number is the chain's, not the message's: a reader picking threads
// out of a list needs "this conversation has unread mail in it", and a count of
// one means four messages went unmentioned.
func TestAChainReportsHowMuchOfItIsUnread(t *testing.T) {
	s := open(t)
	put(t, s, msg{id: "root@example.com", gmail: "g1", subject: "levy", labels: []string{"INBOX", "UNREAD"}})
	put(t, s, msg{id: "two@example.com", parent: "root@example.com", gmail: "g2",
		subject: "levy", labels: []string{"INBOX", "UNREAD"}})
	put(t, s, msg{id: "three@example.com", parent: "root@example.com", gmail: "g3",
		subject: "levy", labels: []string{"SENT"}})
	if _, err := s.ResolveParents(); err != nil {
		t.Fatal(err)
	}

	chains, err := s.SearchChains(Query{Text: "levy"})
	if err != nil {
		t.Fatal(err)
	}
	if len(chains) != 1 {
		t.Fatalf("chains = %d, want one", len(chains))
	}
	if chains[0].Unread != 2 {
		t.Errorf("chain unread = %d, want 2", chains[0].Unread)
	}
}
