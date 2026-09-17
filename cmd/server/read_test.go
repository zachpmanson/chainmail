package main

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/zachpmanson/chainmail/internal/corpus"
	"github.com/zachpmanson/chainmail/internal/gmailclient"
	"github.com/zachpmanson/chainmail/internal/mailingest"
)

// fakeMailbox is the mailbox the write tests work through.
//
// It records what it was asked to change, because "the chain was marked read" is
// only true if every message in it was, and answers with the labels a real
// Modify comes back with — the message's own labels with UNREAD added or removed,
// which is what the handler stores. A fake that answered with its input would let
// a handler that ignored the answer pass.
type fakeMailbox struct {
	calls []string // "<gmail id>:<read|unread>", in the order the write asked
	base  map[string][]string
	fail  map[string]error

	// replies is every reply the fake was asked for, in order: what it was told to
	// answer, the body it was handed, and whether the call was the send or the
	// preview. Recorded as three facts because three different claims rest on them —
	// which message was answered, what the quoted body said, and that pressing the
	// preview button wrote nothing.
	replies []fakeReply
	// to, cc and subject are what the mailbox answers a reply with, per message
	// answered: its own From header, the rest of the message's audience, and its
	// own subject with one Re:. They are deliberately not derived from the request,
	// so a handler that echoed what the caller sent back as "the plan" fails rather
	// than passes.
	to      map[string]string
	cc      map[string]string
	subject map[string]string
	// sentID is the id the mailbox gives a message it has sent, and unanswered is
	// what a preview carries in its place.
	sentID string
	// filing is what Read answers for a sent message: the mailbox's own view of what
	// went out, which is what the handler files into the corpus.
	filing  mailingest.Message
	readErr error
}

type fakeReply struct {
	id, body string
	send     bool
}

func (f *fakeMailbox) SetUnread(id string, unread bool) ([]string, error) {
	verb := "read"
	if unread {
		verb = "unread"
	}
	f.calls = append(f.calls, id+":"+verb)
	if err := f.fail[id]; err != nil {
		return nil, err
	}
	return corpus.SetUnread(f.base[id], unread), nil
}

// Reply is the mailbox's half of POST /v1/send: the recipient and subject its own
// headers give, and — when asked to send — the id of the message that went out.
// The body is the caller's, because composing the quote is the server's and this
// package quotes nothing (see spec.ReplyBody).
func (f *fakeMailbox) Reply(id, body string, send bool) (gmailclient.ReplyPlan, error) {
	f.replies = append(f.replies, fakeReply{id: id, body: body, send: send})
	if err := f.fail[id]; err != nil {
		return gmailclient.ReplyPlan{}, err
	}
	plan := gmailclient.ReplyPlan{To: f.to[id], Cc: f.cc[id], Subject: f.subject[id], Body: body}
	if send {
		plan.GmailID = f.sentID
	}
	return plan, nil
}

// Read is what the handler files a sent message by: the same read the ingest does,
// so the corpus receives the mailbox's own copy of the message rather than a
// second rendering of it.
func (f *fakeMailbox) Read(id string) (mailingest.Message, error) {
	if f.readErr != nil {
		return mailingest.Message{}, f.readErr
	}
	msg := f.filing
	msg.ID = id
	return msg, nil
}

// SetLabels is the same fake for the mail actions: what it records is the two
// label sets it was handed, because "archived" is a claim about which labels came
// off and only a fake that saw them can prove it.
func (f *fakeMailbox) SetLabels(id string, add, remove []string) ([]string, error) {
	f.calls = append(f.calls, id+":+"+strings.Join(add, "+")+":-"+strings.Join(remove, "-"))
	if err := f.fail[id]; err != nil {
		return nil, err
	}
	labels := append([]string(nil), f.base[id]...)
	for _, drop := range remove {
		labels = slices.DeleteFunc(labels, func(l string) bool { return l == drop })
	}
	for _, name := range add {
		if !slices.Contains(labels, name) {
			labels = append(labels, name)
		}
	}
	return labels, nil
}

// readServer is the server over a corpus whose messages have mailbox copies: the
// shape every other fixture lacks, because no other test writes to a mailbox.
//
// The trail is a root with one reply, both unread, one read message in the same
// container, and one entry recovered from a quote — which is part of the chain
// and has no Gmail id, so it is the entry that proves a chain is not all
// markable.
func readServer(t *testing.T) (*harness, *fakeMailbox) {
	t.Helper()
	s, err := corpus.Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	ada := putPerson(t, s, "Ada Okoye", "ada@loomworks.example")
	putMail(t, s, mailFixture{
		ext: extAda1, ts: "2026-03-02T09:15:00+11:00", tz: "AEDT",
		person: ada, container: "T1", subject: "Solar install quote",
		messageID: "<c0ffee-1@loomworks.example>",
		from:      "Ada Okoye <ada@loomworks.example>",
		to:        "Bo Halvorsen <bo@fjordline.example>",
		labels:    []string{"INBOX", "UNREAD"}, gmail: "g-1",
		text: "Can you quote the solar install for the north shed?",
	})
	putMail(t, s, mailFixture{
		ext: extBo2, ts: "2026-03-02T23:40:00+01:00", tz: "+0100",
		person: ada, container: "T1", subject: "Solar install quote",
		messageID: "<c0ffee-2@fjordline.example>", inReplyTo: "<c0ffee-1@loomworks.example>",
		from:   "Bo Halvorsen <bo@fjordline.example>",
		to:     "Ada Okoye <ada@loomworks.example>",
		labels: []string{"INBOX", "UNREAD"}, gmail: "g-2",
		text: "Two days of roof access.",
	})
	putMail(t, s, mailFixture{
		ext: extAda3, ts: "2026-03-03T10:00:00+11:00", tz: "AEDT",
		person: ada, container: "T1", subject: "Solar install quote: dates",
		messageID: "<c0ffee-3@loomworks.example>", inReplyTo: "<c0ffee-2@fjordline.example>",
		from:   "Ada Okoye <ada@loomworks.example>",
		to:     "Bo Halvorsen <bo@fjordline.example>",
		labels: []string{"SENT"}, gmail: "g-3",
		text: "Roof access is fine from the 14th.",
	})
	// The unmarkable member: a reply recovered from a quote, which hangs off the
	// trail by its parent ref and has no mailbox copy of its own.
	ts, err := time.Parse(time.RFC3339, "2026-03-02T12:00:00+11:00")
	if err != nil {
		t.Fatalf("bad ts: %v", err)
	}
	if _, err := s.Put(corpus.Entry{
		Source: corpus.SourceMail, ExtID: extQuoted, TS: ts,
		ParentRef: "<c0ffee-1@loomworks.example>",
		Container: "T1", Subject: "Solar install quote",
		BodyText: "Quoting the original: two days of roof access.",
	}, nil, nil); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if _, err := s.ResolveParents(); err != nil {
		t.Fatalf("ResolveParents: %v", err)
	}

	h := harnessOver(t, s)
	h.markReadEnabled = true
	fake := &fakeMailbox{
		base: map[string][]string{
			"g-1": {"INBOX", "UNREAD"},
			"g-2": {"INBOX", "UNREAD"},
			"g-3": {"SENT"},
		},
	}
	h.openMailbox = func() (mailbox, error) { return fake, nil }
	return h, fake
}

// The whole write, end to end: every message in the chain that has a mailbox copy
// is marked, in the mailbox and in the corpus, and the one that has none is
// counted rather than failed. A handler that marked only the message it was
// pointed at would leave a thread half read; one that treated the recovered entry
// as an error would refuse to mark any chain that contains quoted text.
func TestReadingAChainMarksEveryMessageThatHasAMailboxCopy(t *testing.T) {
	h, fake := readServer(t)

	res := h.do(t, "POST", "/v1/read", []byte(`{"chain":"`+extAda1+`","unread":false}`))
	if res.status != 200 {
		t.Fatalf("status %d: %s", res.status, res.body)
	}
	var got markReadResponse
	if err := json.Unmarshal(res.body, &got); err != nil {
		t.Fatalf("decoding %s: %v", res.body, err)
	}
	if got.Marked != 3 || got.Skipped != 1 {
		t.Errorf("response = %+v, want 3 marked and the recovered entry skipped", got)
	}
	if want := []string{"g-1:read", "g-2:read", "g-3:read"}; !slices.Equal(fake.calls, want) {
		t.Errorf("mailbox saw %v, want %v", fake.calls, want)
	}
	// The local half: what the corpus now holds is what the mailbox answered
	// with, not what the caller asked for.
	for ext, want := range map[string][]string{
		extAda1: {"INBOX"},
		extBo2:  {"INBOX"},
		extAda3: {"SENT"},
	} {
		if got := chainLabels(t, h, ext); !slices.Equal(got, want) {
			t.Errorf("%s labels = %v, want %v", ext, got, want)
		}
	}
}

// And the other direction, because a reader who marked something read by accident
// has to be able to put it back — from the same button.
func TestAChainCanBeMarkedUnreadAgain(t *testing.T) {
	h, fake := readServer(t)
	res := h.do(t, "POST", "/v1/read", []byte(`{"chain":"`+extAda1+`","unread":true}`))
	if res.status != 200 {
		t.Fatalf("status %d: %s", res.status, res.body)
	}
	if want := []string{"g-1:unread", "g-2:unread", "g-3:unread"}; !slices.Equal(fake.calls, want) {
		t.Errorf("mailbox saw %v, want %v", fake.calls, want)
	}
	if got := chainLabels(t, h, extAda3); !slices.Contains(got, "UNREAD") {
		t.Errorf("the read message was not marked unread: %v", got)
	}
	// Idempotent: the message already unread does not collect a second UNREAD.
	if got := chainLabels(t, h, extAda1); strings.Count(strings.Join(got, ","), "UNREAD") != 1 {
		t.Errorf("labels = %v, want exactly one UNREAD", got)
	}
}

// The switch: a host that has not granted -mark-read answers 403 and never opens
// the mailbox, which is the difference between a refused button and a button that
// quietly does nothing.
func TestReadingIsRefusedWithoutTheGrant(t *testing.T) {
	h, fake := readServer(t)
	h.markReadEnabled = false
	opened := false
	h.openMailbox = func() (mailbox, error) {
		opened = true
		return fake, nil
	}

	res := h.do(t, "POST", "/v1/read", []byte(`{"chain":"`+extAda1+`","unread":false}`))
	if res.status != 403 {
		t.Fatalf("status %d, want 403", res.status)
	}
	if msg := res.errText(t); !strings.Contains(msg, "-mark-read") {
		t.Errorf("the refusal does not name the switch: %s", msg)
	}
	if opened || len(fake.calls) != 0 {
		t.Errorf("a refused request reached the mailbox: %v", fake.calls)
	}
}

// A chain of nothing but recovered text is answered, not refused: it is a real
// chain with a real count of what could not be marked, and a host with no
// mailbox grant can still be told so. The mailbox is not opened at all.
func TestAChainWithNoMailboxCopyNeedsNoMailbox(t *testing.T) {
	h, _ := readServer(t)
	h.openMailbox = func() (mailbox, error) {
		t.Error("a chain of recovered text opened the mailbox")
		return nil, errors.New("no mailbox")
	}
	res := h.do(t, "POST", "/v1/read", []byte(`{"chain":"`+extQuoted+`","unread":false}`))
	if res.status != 200 {
		t.Fatalf("status %d: %s", res.status, res.body)
	}
	var got markReadResponse
	if err := json.Unmarshal(res.body, &got); err != nil {
		t.Fatal(err)
	}
	if got.Marked != 0 || got.Skipped != 1 {
		t.Errorf("response = %+v, want 0 marked and 1 skipped", got)
	}
}

// A mailbox that refuses halfway says how far it got: a chain is more than one
// message, and the caller has to know whether it is half changed. The messages
// already marked stay marked — a rollback of the mailbox is not this server's to
// perform — and the ones after it are left alone.
func TestAMailboxFailureNamesWhatWasAlreadyChanged(t *testing.T) {
	h, fake := readServer(t)
	fake.fail = map[string]error{"g-2": errors.New("insufficient permission")}

	res := h.do(t, "POST", "/v1/read", []byte(`{"chain":"`+extAda1+`","unread":false}`))
	if res.status != 502 {
		t.Fatalf("status %d, want 502", res.status)
	}
	msg := res.errText(t)
	if !strings.Contains(msg, "1 of 3") {
		t.Errorf("the error does not say how much was changed: %s", msg)
	}
	if got := chainLabels(t, h, extAda1); slices.Contains(got, "UNREAD") {
		t.Errorf("the first message was not left marked: %v", got)
	}
	if got := chainLabels(t, h, extAda3); slices.Contains(got, "UNREAD") {
		t.Errorf("a message after the failure was touched: %v", got)
	}
}

// The shapes a caller can get wrong, refused before anything is written.
func TestReadRefusesWhatItCannotAnswer(t *testing.T) {
	h, fake := readServer(t)
	for _, tc := range []struct {
		name, body string
		want       int
		mentions   string
	}{
		{name: "no chain", body: `{"unread":false}`, want: 400, mentions: "needs a chain"},
		{name: "unknown chain", body: `{"chain":"` + extNone + `"}`, want: 404, mentions: "no chain at"},
		{name: "misspelled field", body: `{"chian":"` + extAda1 + `"}`, want: 400, mentions: "request body"},
		{name: "not json", body: `nonsense`, want: 400, mentions: "request body"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res := h.do(t, "POST", "/v1/read", []byte(tc.body))
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

// The badge's number reaches the list: the chain a reader picks out of a search
// says how much of it is unread, and a chain with none says 0 rather than
// omitting the field — a client that cannot see the key cannot tell "read" from
// "this server does not report read state".
func TestASearchResultCarriesTheChainsUnreadCount(t *testing.T) {
	h, _ := readServer(t)
	res := h.do(t, "GET", "/v1/search?q=roof", nil)
	if res.status != 200 {
		t.Fatalf("status %d: %s", res.status, res.body)
	}
	var got searchResponse
	if err := json.Unmarshal(res.body, &got); err != nil {
		t.Fatal(err)
	}
	if got.Chains == nil || len(*got.Chains) != 1 {
		t.Fatalf("chains = %v, want one", got.Chains)
	}
	if n := (*got.Chains)[0].Unread; n != 2 {
		t.Errorf("chain unread = %d, want 2", n)
	}
	if raw := res.body; !strings.Contains(string(raw), `"unread":2`) {
		t.Errorf("the count is not on the wire: %s", raw)
	}
}

// chainLabels reads one entry's stored labels out of the corpus the harness is
// serving, so a test asserts what the write left behind rather than what the
// handler said it did.
func chainLabels(t *testing.T, h *harness, ext string) []string {
	t.Helper()
	entries, err := h.store.ChainEntries(ext)
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
