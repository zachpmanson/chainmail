package main

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
)

// mailServer is the read-state fixture with the mail grant on, plus one more
// chain: the read tests have exactly one (a root, a reply, a sent message and a
// recovered quote all hang off the same trail), and the one thing a plural call
// can get wrong is which row belongs to which chain.
//
// The extra chain is deliberately plain — one message, one mailbox copy, in the
// inbox — so "two chains, two rows" is asserted against a chain that has nothing
// odd about it.
func mailServer(t *testing.T) (*harness, *fakeMailbox) {
	t.Helper()
	h, fake := readServer(t)
	h.mailWriteEnabled = true
	ada := putPerson(t, h.store, "Ada Okoye", "ada@loomworks.example")
	putMail(t, h.store, mailFixture{
		ext: extOther, ts: "2026-03-05T08:00:00+11:00", tz: "AEDT",
		person: ada, container: "T9", subject: "Storage quote",
		messageID: "<c0ffee-9@loomworks.example>",
		from:      "Ada Okoye <ada@loomworks.example>",
		to:        "Bo Halvorsen <bo@fjordline.example>",
		labels:    []string{"INBOX", "UNREAD"}, gmail: "g-9",
		text: "The storage unit is free from April.",
	})
	fake.base["g-9"] = []string{"INBOX", "UNREAD"}
	return h, fake
}

// The whole write, end to end: every message of the chain that has a mailbox copy
// loses INBOX, in the mailbox and in the corpus, and the entry with no copy is
// counted rather than failed. A message with no INBOX in it is written to anyway
// — the mailbox is the source of truth and the mirror is refreshed from what it
// answers, so a label some other client removed an hour ago is picked up by this
// write rather than being preserved by a local guess that it was not there.
func TestArchivingAChainTakesEveryMessageOutOfTheInbox(t *testing.T) {
	h, fake := mailServer(t)

	res := h.do(t, "POST", "/v1/mail", []byte(`{"chains":["`+extAda1+`"],"action":"archive"}`))
	if res.status != 200 {
		t.Fatalf("status %d: %s", res.status, res.body)
	}
	var got mailActionResponse
	if err := json.Unmarshal(res.body, &got); err != nil {
		t.Fatalf("decoding %s: %v", res.body, err)
	}
	if got.Action != "archive" || got.Changed != 3 || got.Skipped != 1 {
		t.Errorf("response = %+v, want archive of 3 messages and the recovered entry skipped", got)
	}
	if len(got.Chains) != 1 || got.Chains[0].RootExtID != extAda1 ||
		got.Chains[0].Changed != 3 || got.Chains[0].Skipped != 1 {
		t.Errorf("per-chain = %+v, want the one chain named", got.Chains)
	}
	if want := []string{"g-1:+:-INBOX", "g-2:+:-INBOX", "g-3:+:-INBOX"}; !slices.Equal(fake.calls, want) {
		t.Errorf("mailbox saw %v, want %v", fake.calls, want)
	}
	// The local half: what the corpus holds is what the mailbox answered with.
	for ext, want := range map[string][]string{
		extAda1: {"UNREAD"},
		extBo2:  {"UNREAD"},
		extAda3: {"SENT"},
	} {
		if got := chainLabels(t, h, ext); !slices.Equal(got, want) {
			t.Errorf("%s labels = %v, want %v", ext, got, want)
		}
	}
}

// A move is a label added and the way out of the inbox, in one write: a folder
// the reader moves into is where the message now lives, which is why the label
// is not added somewhere else first. The label the caller named is echoed back,
// so a client that fired a move can say where the mail went.
func TestMovingAChainPutsEveryMessageInTheNamedFolder(t *testing.T) {
	h, fake := mailServer(t)

	res := h.do(t, "POST", "/v1/mail",
		[]byte(`{"chains":["`+extOther+`"],"action":"move","labels":["Work"]}`))
	if res.status != 200 {
		t.Fatalf("status %d: %s", res.status, res.body)
	}
	var got mailActionResponse
	if err := json.Unmarshal(res.body, &got); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got.Labels, []string{"Work"}) {
		t.Errorf("labels = %v, want the folder named back", got.Labels)
	}
	if got.Changed != 1 {
		t.Errorf("changed = %d, want 1", got.Changed)
	}
	if want := []string{"g-9:+Work:-INBOX"}; !slices.Equal(fake.calls, want) {
		t.Errorf("mailbox saw %v, want %v", fake.calls, want)
	}
	if labels := chainLabels(t, h, extOther); !slices.Contains(labels, "Work") || slices.Contains(labels, "INBOX") {
		t.Errorf("stored labels = %v, want Work and not INBOX", labels)
	}
}

// Trash is the server's word for delete, and it is two labels at once: the
// message carries Trash and stops carrying INBOX, because a message in the trash
// that is still in the inbox is in two places at once — which is what a reader
// who pressed Delete would see if this removed only INBOX.
func TestTrashingAChainTakesItOutOfTheInboxAndPutsItInTheTrash(t *testing.T) {
	h, fake := mailServer(t)

	res := h.do(t, "POST", "/v1/mail", []byte(`{"chains":["`+extOther+`"],"action":"trash"}`))
	if res.status != 200 {
		t.Fatalf("status %d: %s", res.status, res.body)
	}
	if want := []string{"g-9:+TRASH:-INBOX"}; !slices.Equal(fake.calls, want) {
		t.Errorf("mailbox saw %v, want %v", fake.calls, want)
	}
	labels := chainLabels(t, h, extOther)
	if !slices.Contains(labels, "TRASH") || slices.Contains(labels, "INBOX") {
		t.Errorf("stored labels = %v, want TRASH and not INBOX", labels)
	}
	// Gmail's own trash, not a folder this server invented: the reader can still
	// find it in the app on their phone, which is the point of writing through.
	// Stored exactly as the mailbox answered, UNREAD and all — this write is about
	// where the message is, and a read state it happened to carry is not this
	// call's business to drop.
	sorted := append([]string(nil), labels...)
	slices.Sort(sorted)
	if want := []string{"TRASH", "UNREAD"}; !slices.Equal(sorted, want) {
		t.Errorf("stored labels = %v, want %v", labels, want)
	}
}

// Two chains in one call, counted per chain: a total that says 4 cannot tell the
// reader which of the two threads stayed put, and the row order follows the order
// they named rather than whatever the store iterates in.
func TestAMailChangeCountsEachChainItWasGiven(t *testing.T) {
	h, _ := mailServer(t)

	res := h.do(t, "POST", "/v1/mail",
		[]byte(`{"chains":["`+extOther+`","`+extAda1+`"],"action":"archive"}`))
	if res.status != 200 {
		t.Fatalf("status %d: %s", res.status, res.body)
	}
	var got mailActionResponse
	if err := json.Unmarshal(res.body, &got); err != nil {
		t.Fatal(err)
	}
	if got.Changed != 4 || got.Skipped != 1 {
		t.Errorf("totals = %d changed / %d skipped, want 4 and 1", got.Changed, got.Skipped)
	}
	if len(got.Chains) != 2 {
		t.Fatalf("chains = %+v, want two rows", got.Chains)
	}
	if got.Chains[0].RootExtID != extOther || got.Chains[1].RootExtID != extAda1 {
		t.Errorf("rows = %+v, want them in the order they were named", got.Chains)
	}
	if got.Chains[0].Changed != 1 || got.Chains[1].Changed != 3 {
		t.Errorf("rows = %+v, want 1 then 3 changed", got.Chains)
	}
}

// All of them or none: an unknown id in the set is a caller working from a list
// that has moved on, and half-applying it would leave a mailbox matching neither
// the list they were looking at nor the one they will see next. Nothing is
// written, not even for the chain that does exist.
func TestAMailChangeWithOneUnknownChainChangesNothing(t *testing.T) {
	h, fake := mailServer(t)

	res := h.do(t, "POST", "/v1/mail",
		[]byte(`{"chains":["`+extAda1+`","`+extNone+`"],"action":"archive"}`))
	if res.status != 404 {
		t.Fatalf("status %d, want 404: %s", res.status, res.body)
	}
	if msg := res.errText(t); !strings.Contains(msg, "no chain at") {
		t.Errorf("error %q does not say which chain is unknown", msg)
	}
	if len(fake.calls) != 0 {
		t.Errorf("a refused call reached the mailbox: %v", fake.calls)
	}
	if labels := chainLabels(t, h, extAda1); !slices.Contains(labels, "INBOX") {
		t.Errorf("the chain that does exist was changed anyway: %v", labels)
	}
	if labels := chainLabels(t, h, extAda3); !slices.Equal(labels, []string{"SENT"}) {
		t.Errorf("stored labels = %v, want them untouched", labels)
	}
}

// The switch, and its own switch: a host that granted -mark-read has not thereby
// asked this server to be able to delete mail, so the refusal names -mail-write
// and the mailbox is never opened — which is the difference between a refused
// button and a button that quietly does nothing.
func TestMailIsRefusedWithoutItsOwnGrant(t *testing.T) {
	h, fake := mailServer(t)
	h.mailWriteEnabled = false
	h.markReadEnabled = true
	opened := false
	h.openUnreadMailbox = func() (mailbox, error) {
		opened = true
		return fake, nil
	}

	res := h.do(t, "POST", "/v1/mail", []byte(`{"chains":["`+extAda1+`"],"action":"archive"}`))
	if res.status != 403 {
		t.Fatalf("status %d, want 403: %s", res.status, res.body)
	}
	if msg := res.errText(t); !strings.Contains(msg, "-mail-write") {
		t.Errorf("the refusal does not name the switch: %s", msg)
	}
	if opened || len(fake.calls) != 0 {
		t.Errorf("a refused request reached the mailbox: %v", fake.calls)
	}
	// And the read circle still works on the same host, because the two grants
	// are separate: the reader who never wanted delete still wanted read.
	if res := h.do(t, "POST", "/v1/read", []byte(`{"chain":"`+extAda1+`","unread":false}`)); res.status != 200 {
		t.Errorf("the read grant stopped working with the mail one off: %d %s", res.status, res.body)
	}
}

// A chain of nothing but recovered text is answered, not refused: it is a real
// chain with a real count of what could not be changed, and a host with no
// mailbox grant can still be told so. The mailbox is not opened at all.
func TestAChainWithNoMailboxCopyNeedsNoMailboxForMailEither(t *testing.T) {
	h, _ := mailServer(t)
	h.openUnreadMailbox = func() (mailbox, error) {
		t.Error("a chain of recovered text opened the mailbox")
		return nil, errors.New("no mailbox")
	}
	res := h.do(t, "POST", "/v1/mail", []byte(`{"chains":["`+extQuoted+`"],"action":"archive"}`))
	if res.status != 200 {
		t.Fatalf("status %d: %s", res.status, res.body)
	}
	var got mailActionResponse
	if err := json.Unmarshal(res.body, &got); err != nil {
		t.Fatal(err)
	}
	if got.Changed != 0 || got.Skipped != 1 {
		t.Errorf("response = %+v, want 0 changed and 1 skipped", got)
	}
}

// A mailbox that refuses part way says how far it got, because a call may be part
// way through several chains: the count of messages already changed and of chains
// already finished is the first thing the caller needs, and the mailbox's own
// error says nothing about either. What was written stays written — undoing a
// mailbox change is not this server's to perform, and the caller can re-run the
// same call for the rest.
func TestAMailboxFailureOnAMailChangeNamesWhatWasAlreadyChanged(t *testing.T) {
	h, fake := mailServer(t)
	fake.fail = map[string]error{"g-2": errors.New("insufficient permission")}

	res := h.do(t, "POST", "/v1/mail", []byte(`{"chains":["`+extAda1+`"],"action":"archive"}`))
	if res.status != 502 {
		t.Fatalf("status %d, want 502: %s", res.status, res.body)
	}
	msg := res.errText(t)
	if !strings.Contains(msg, "1 of 3 messages changed") {
		t.Errorf("the error does not say how much was changed: %s", msg)
	}
	if got := chainLabels(t, h, extAda1); slices.Contains(got, "INBOX") {
		t.Errorf("the first message was not left changed: %v", got)
	}
	if got := chainLabels(t, h, extAda3); !slices.Equal(got, []string{"SENT"}) {
		t.Errorf("a message after the failure was touched: %v", got)
	}
}

// The shapes a caller can get wrong, refused before anything is written. The
// action is checked against the table rather than defaulted, because the default
// of a missing action would have to be some action, and guessing at a mailbox
// write is worse than asking again.
func TestMailRefusesWhatItCannotAnswer(t *testing.T) {
	h, fake := mailServer(t)
	for _, tc := range []struct {
		name, body string
		want       int
		mentions   string
	}{
		{name: "no chains", body: `{"action":"archive"}`, want: 400, mentions: "needs chains"},
		{name: "unknown action", body: `{"chains":["` + extAda1 + `"],"action":"burn"}`, want: 400,
			mentions: "unknown action"},
		{name: "move with no labels", body: `{"chains":["` + extAda1 + `"],"action":"move"}`, want: 400,
			mentions: "needs labels"},
		{name: "misspelled field", body: `{"chians":["` + extAda1 + `"],"action":"archive"}`, want: 400,
			mentions: "request body"},
		{name: "not json", body: `nonsense`, want: 400, mentions: "request body"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res := h.do(t, "POST", "/v1/mail", []byte(tc.body))
			if res.status != tc.want {
				t.Fatalf("status %d, want %d: %s", res.status, tc.want, res.body)
			}
			if msg := res.errText(t); !strings.Contains(msg, tc.mentions) {
				t.Errorf("error %q does not mention %q", msg, tc.mentions)
			}
		})
	}
	if len(fake.calls) != 0 {
		t.Errorf("a refused request reached the mailbox: %v", fake.calls)
	}
}

// A folder the mailbox does not have is the mailbox's to refuse, not this
// server's to create: the label list is Gmail's, and a typo that silently made a
// new folder would put a reader's mail somewhere they cannot find it. The refusal
// is carried back with the part that had already been written.
func TestAnUnnamedFolderIsRefusedByTheMailboxNotCreated(t *testing.T) {
	h, fake := mailServer(t)
	fake.fail = map[string]error{"g-9": errors.New("label does not exist: Wrok")}

	res := h.do(t, "POST", "/v1/mail",
		[]byte(`{"chains":["`+extOther+`"],"action":"move","labels":["Wrok"]}`))
	if res.status != 502 {
		t.Fatalf("status %d, want 502: %s", res.status, res.body)
	}
	if msg := res.errText(t); !strings.Contains(msg, "label does not exist") {
		t.Errorf("the mailbox's reason did not reach the caller: %s", msg)
	}
	if labels := chainLabels(t, h, extOther); slices.Contains(labels, "Wrok") {
		t.Errorf("the corpus stored a folder the mailbox does not have: %v", labels)
	}
	if labels := chainLabels(t, h, extOther); !slices.Contains(labels, "INBOX") {
		t.Errorf("a failed move left the message out of the inbox: %v", labels)
	}
}
