package main

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/zachpmanson/chainmail/internal/gmailclient"
	"github.com/zachpmanson/chainmail/internal/mailingest"
)

// sendServer is the server that may answer mail: the read fixture's corpus — whose
// messages have mailbox copies and whose trail holds one entry that has none —
// with the reply half of the fake wired up.
//
// The mailbox's answers are set here rather than derived from the request, which is
// the point of the fixture: the recipient and the subject come from the answered
// message's own headers, so a handler that echoed the caller's request back as "the
// plan" fails instead of passing. The filing is the mailbox's own view of what went
// out, which is what the corpus is handed.
func sendServer(t *testing.T) (*harness, *fakeMailbox) {
	t.Helper()
	h, fake := readServer(t)
	h.sendMailEnabled = true
	fake.to = map[string]string{"g-3": "Bo Halvorsen <bo@fjordline.example>"}
	fake.subject = map[string]string{"g-3": "Re: Solar install quote: dates"}
	fake.sentID = "g-4"
	fake.filing = mailingest.Message{
		Envelope: mailingest.Envelope{
			ID:        "g-4",
			ThreadID:  "T1",
			From:      "Ada Okoye <ada@loomworks.example>",
			To:        "Bo Halvorsen <bo@fjordline.example>",
			Subject:   "Re: Solar install quote: dates",
			Date:      "Tue, 3 Mar 2026 11:00:00 +1100",
			MessageID: "<sent-4@loomworks.example>",
			InReplyTo: "<c0ffee-3@loomworks.example>",
			Labels:    []string{"SENT"},
		},
		Body: "The 14th works. I'll confirm the roof access with the fitters.\n\n" +
			"On Tue 3 Mar 2026 10:00 AEDT, Ada Okoye <ada@loomworks.example> wrote:\n" +
			"> Roof access is fine from the 14th.\n",
	}
	return h, fake
}

// The switch, and its own switch: -send-mail is a third grant rather than a piece
// of either write beside it. A host that let the read circle write has not thereby
// asked this server to be able to send mail on its behalf, and the refusal names
// the switch while never opening the mailbox.
func TestSendingIsRefusedWithoutTheGrant(t *testing.T) {
	h, fake := sendServer(t)
	h.sendMailEnabled = false
	h.markReadEnabled = true
	h.mailWriteEnabled = true
	opened := false
	h.openMailbox = func() (mailbox, error) {
		opened = true
		return fake, nil
	}

	res := h.do(t, "POST", "/v1/send",
		[]byte(`{"entry":"`+extAda3+`","body":"The 14th works.","confirm":true}`))
	if res.status != 403 {
		t.Fatalf("status %d, want 403: %s", res.status, res.body)
	}
	if msg := res.errText(t); !strings.Contains(msg, "-send-mail") {
		t.Errorf("the refusal does not name the switch: %s", msg)
	}
	if opened || len(fake.replies) != 0 {
		t.Errorf("a refused request reached the mailbox: %+v", fake.replies)
	}
}

// The preview: no confirm, so nothing is sent — the mailbox is asked what the reply
// would be, which is a read of the message being answered and no write at all.
//
// What comes back is the plan rather than an echo of the request: the recipient and
// the subject are the mailbox's, and the body is the reader's words with the quote
// this server writes under them. A client that showed the reader a preview can
// therefore be checked against what the send did, rather than trusting that the two
// were the same.
func TestThePreviewSendsNothingAndAnswersTheMailboxsOwnPlan(t *testing.T) {
	h, fake := sendServer(t)

	res := h.do(t, "POST", "/v1/send",
		[]byte(`{"entry":"`+extAda3+`","body":"The 14th works."}`))
	if res.status != 200 {
		t.Fatalf("status %d: %s", res.status, res.body)
	}
	api := loadAPI(t)
	api.assert(t, "SendResponse", res.body)

	got := decode[sendResponse](t, res)
	if got.Sent {
		t.Error("a preview claims to have sent")
	}
	if got.GmailID != "" {
		t.Errorf("a preview carries a sent id: %q", got.GmailID)
	}
	if got.To != "Bo Halvorsen <bo@fjordline.example>" {
		t.Errorf("to = %q, want the recipe the mailbox holds rather than one from the request", got.To)
	}
	if got.Subject != "Re: Solar install quote: dates" {
		t.Errorf("subject = %q, want the mailbox's", got.Subject)
	}
	if got.Entry != extAda3 {
		t.Errorf("entry = %q, want the entry named", got.Entry)
	}
	// The reader's words, then the message being answered, attributed the way its
	// own bubble names it — the whole of what a reply says that nobody typed.
	for _, want := range []string{
		"The 14th works.\n\nOn Tue 3 Mar 2026 10:00 AEDT, Ada Okoye <ada@loomworks.example> wrote:",
		"> Roof access is fine from the 14th.",
	} {
		if !strings.Contains(got.Body, want) {
			t.Errorf("the plan's body is missing %q:\n%s", want, got.Body)
		}
	}
	// The mailbox saw one call and it was not a send, and the corpus is untouched:
	// a preview that filed the message it did not send would put mail in the trail
	// that nobody was sent.
	if len(fake.replies) != 1 || fake.replies[0].send {
		t.Fatalf("the mailbox saw %+v, want one call that was not a send", fake.replies)
	}
	if fake.replies[0].id != "g-3" {
		t.Errorf("the reply was threaded against %q, want the answered message's own id", fake.replies[0].id)
	}
	if entries := chainExtIDs(t, h, extAda1); contains(entries, "mail:<sent-4@loomworks.example>") {
		t.Errorf("a preview filed a message: %v", entries)
	}
}

// The send, end to end: the message is answered, and the answer is filed into the
// corpus in the same request so the thread the reader is looking at shows it rather
// than waiting for the next slurp.
func TestSendingAnswersTheMessageAndFilesTheAnswerIntoTheCorpus(t *testing.T) {
	h, fake := sendServer(t)

	res := h.do(t, "POST", "/v1/send",
		[]byte(`{"entry":"`+extAda3+`","body":"The 14th works.","confirm":true}`))
	if res.status != 200 {
		t.Fatalf("status %d: %s", res.status, res.body)
	}
	loadAPI(t).assert(t, "SendResponse", res.body)

	got := decode[sendResponse](t, res)
	if !got.Sent || got.GmailID != "g-4" {
		t.Fatalf("response = %+v, want it sent and the id the mailbox gave", got)
	}
	if len(fake.replies) != 1 || !fake.replies[0].send {
		t.Errorf("the mailbox saw %+v, want one send", fake.replies)
	}
	// The corpus holds what the mailbox holds, through the same read-and-store path
	// the ingest uses — so the answer is one entry in the trail, joined to the
	// message it answers by the reply headers the mailbox built for it, rather than
	// a chain of its own.
	if !contains(chainExtIDs(t, h, extAda1), "mail:<sent-4@loomworks.example>") {
		t.Fatalf("the sent reply is not in the chain it answers: %v",
			chainExtIDs(t, h, extAda1))
	}
	if parent := chainTrail(t, h, extAda1)["mail:<sent-4@loomworks.example>"]; parent != extAda3 {
		t.Errorf("the reply's parent = %q, want the message it answers (%s)", parent, extAda3)
	}
}

// An entry with no mailbox copy is refused rather than skipped — the opposite of
// /v1/read and /v1/mail, and deliberately: those act on the part of a set that can
// be acted on, while a send cannot be half-performed. There is no mailbox message
// to thread an answer against, so the mailbox is never opened.
func TestSendingRefusesAnEntryTheMailboxDoesNotHold(t *testing.T) {
	h, fake := sendServer(t)
	h.openMailbox = func() (mailbox, error) {
		t.Error("an entry with no mailbox copy opened the mailbox")
		return fake, nil
	}

	res := h.do(t, "POST", "/v1/send",
		[]byte(`{"entry":"`+extQuoted+`","body":"The 14th works.","confirm":true}`))
	if res.status != 400 {
		t.Fatalf("status %d, want 400: %s", res.status, res.body)
	}
	if msg := res.errText(t); !strings.Contains(msg, extQuoted) {
		t.Errorf("the refusal does not name the entry: %s", msg)
	}
}

// The shapes a caller can get wrong, refused before the mailbox is opened: a send
// with no message to answer and one with nothing to say are both a message that
// would go out with the wrong content rather than a smaller version of the right
// call.
func TestSendRefusesWhatItCannotAnswer(t *testing.T) {
	h, fake := sendServer(t)
	h.openMailbox = func() (mailbox, error) {
		t.Error("a refused call opened the mailbox")
		return fake, nil
	}
	for _, tc := range []struct {
		name, body string
		want       int
		mentions   string
	}{
		{name: "no entry", body: `{"body":"hello"}`, want: 400, mentions: "answers one message"},
		{name: "blank entry", body: `{"entry":"  ","body":"hello"}`, want: 400, mentions: "answers one message"},
		{name: "no body", body: `{"entry":"` + extAda3 + `"}`, want: 400, mentions: "something to say"},
		{name: "blank body", body: `{"entry":"` + extAda3 + `","body":"\n  \n"}`, want: 400, mentions: "something to say"},
		{name: "unknown entry", body: `{"entry":"` + extNone + `","body":"hello"}`, want: 404, mentions: "no entry"},
		// The field that decides whether a real mailbox is written is the one a
		// typo must not silently skip.
		{name: "misspelled field", body: `{"entry":"` + extAda3 + `","body":"hi","confrim":true}`, want: 400, mentions: "request body"},
		{name: "not json", body: `nonsense`, want: 400, mentions: "request body"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res := h.do(t, "POST", "/v1/send", []byte(tc.body))
			if res.status != tc.want {
				t.Fatalf("status %d, want %d: %s", res.status, tc.want, res.body)
			}
			if msg := res.errText(t); !strings.Contains(msg, tc.mentions) {
				t.Errorf("error %q does not mention %q", msg, tc.mentions)
			}
		})
	}
	if len(fake.replies) != 0 {
		t.Errorf("a refused call reached the mailbox: %+v", fake.replies)
	}
}

// The two ways a send can fail, and the difference between them is the whole of
// what a reader can do next: a mailbox that would not prepare the reply sent
// nothing, so pressing send again is safe; a mailbox that did not answer the send
// may have sent it, so only Gmail can say — and a message that has gone out cannot
// be recalled.
func TestASendThatFailedSaysWhetherAnythingWentOut(t *testing.T) {
	for _, tc := range []struct {
		name          string
		err           error
		want, notWant string
	}{
		{
			name: "nothing was handed over",
			err:  fmt.Errorf("%w: fetching original message: 404", gmailclient.ErrUnsent),
			want: "nothing was sent",
		},
		{
			name: "the send did not answer",
			err:  fmt.Errorf("%w: sending message: context deadline exceeded", gmailclient.ErrSendUnknown),
			want: "not known from here",
			// It must not promise that a retry is safe, which is the one wrong thing
			// this handler could say about an unanswerable send.
			notWant: "nothing was sent",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, fake := sendServer(t)
			fake.fail = map[string]error{"g-3": tc.err}

			res := h.do(t, "POST", "/v1/send",
				[]byte(`{"entry":"`+extAda3+`","body":"The 14th works.","confirm":true}`))
			if res.status != 502 {
				t.Fatalf("status %d, want 502: %s", res.status, res.body)
			}
			msg := res.errText(t)
			if !strings.Contains(msg, tc.want) {
				t.Errorf("error %q does not say %q", msg, tc.want)
			}
			if tc.notWant != "" && strings.Contains(msg, tc.notWant) {
				t.Errorf("error %q claims %q, which is not known", msg, tc.notWant)
			}
		})
	}
}

// The reply has gone out by the time the corpus is asked to hold it, so a filing
// that fails must not become a failure of the send: answering 502 here would tell
// the reader their reply did not go out when it did, and they would send it again.
// The mailbox is the source of truth and the reply is in it, so the gap is the
// corpus's until the next ingest — which is idempotent by body hash — and the
// handler's own record of it is the log line.
func TestAFilingFailureDoesNotClaimTheReplyFailed(t *testing.T) {
	h, fake := sendServer(t)
	fake.readErr = errors.New("gmail: the request took too long")

	res := h.do(t, "POST", "/v1/send",
		[]byte(`{"entry":"`+extAda3+`","body":"The 14th works.","confirm":true}`))
	if res.status != 200 {
		t.Fatalf("status %d, want 200 — the message was sent: %s", res.status, res.body)
	}
	got := decode[sendResponse](t, res)
	if !got.Sent || got.GmailID != "g-4" {
		t.Errorf("response = %+v, want it sent and truthful about the id", got)
	}
	if contains(chainExtIDs(t, h, extAda1), "mail:<sent-4@loomworks.example>") {
		t.Error("the corpus holds the reply even though the read of it failed")
	}
}

// chainTrail is a chain as the store walks it: every entry by ext id, with the
// entry it answers. A send has two things to prove and this answers both — the
// reply arrived in the trail, and the trail joined it to the message it answers
// rather than giving it a chain of its own.
func chainTrail(t *testing.T, h *harness, ext string) map[string]string {
	t.Helper()
	shown, err := h.store.Chain(ext)
	if err != nil {
		t.Fatalf("Chain(%s): %v", ext, err)
	}
	trail := make(map[string]string, len(shown))
	for _, e := range shown {
		trail[e.ExtID] = e.Parent
	}
	return trail
}

// chainExtIDs is every entry of a chain by ext id, for a test that has to see
// whether a message arrived in the trail rather than only that the call succeeded.
func chainExtIDs(t *testing.T, h *harness, ext string) []string {
	t.Helper()
	trail := chainTrail(t, h, ext)
	out := make([]string, 0, len(trail))
	for id := range trail {
		out = append(out, id)
	}
	return out
}
