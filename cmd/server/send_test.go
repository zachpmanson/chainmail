package main

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/zachpmanson/chainmail/internal/corpus"
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
	fake.cc = map[string]string{"g-3": "Cy Okafor <cy@loomworks.example>"}
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
	// The rest of the audience is the mailbox's answer too — this is a reply to
	// everyone the message was addressed to, and who that is comes from the
	// message's own headers rather than from anything the caller sent. It is the
	// default for the same reason it is the mailbox's answer: a request that says
	// nothing about the audience gets the whole of it.
	if got.Cc != "Cy Okafor <cy@loomworks.example>" {
		t.Errorf("cc = %q, want the mailbox's other recipients", got.Cc)
	}
	if len(fake.replies) != 1 || !fake.replies[0].all {
		t.Errorf("the mailbox saw %+v, want a reply to everyone", fake.replies)
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

// The audience goes back to the client as addresses, not only as the headers they
// will be written into: the two say the same thing — the strings are these
// rendered — and it is the addresses a client offers a reader to narrow the reply
// with and names back on the send (see SendRequest's to/cc). What is checked here
// is that they are the answered message's own, with the names it gave them, in its
// order, and that they are the whole of the set a send may choose from.
func TestThePlanCarriesTheAudienceItMaySendToAsAddresses(t *testing.T) {
	h, _ := sendServer(t)

	res := h.do(t, "POST", "/v1/send",
		[]byte(`{"entry":"`+extAda3+`","body":"The 14th works."}`))
	if res.status != 200 {
		t.Fatalf("status %d: %s", res.status, res.body)
	}
	loadAPI(t).assert(t, "SendResponse", res.body)

	got := decode[sendResponse](t, res)
	wantTo := []recipient{{Name: "Bo Halvorsen", Address: "bo@fjordline.example"}}
	if !slices.Equal(got.ToRecipients, wantTo) {
		t.Errorf("toRecipients = %+v, want %+v", got.ToRecipients, wantTo)
	}
	wantCc := []recipient{{Name: "Cy Okafor", Address: "cy@loomworks.example"}}
	if !slices.Equal(got.CcRecipients, wantCc) {
		t.Errorf("ccRecipients = %+v, want %+v", got.CcRecipients, wantCc)
	}
	// The header and the addresses agree, which is what makes a chip and the line
	// above it the same claim rather than two.
	if got.To != "Bo Halvorsen <bo@fjordline.example>" {
		t.Errorf("to = %q, want it rendered from the address above", got.To)
	}
}

// A caller may take an address off the reply, and may move one between To and Cc:
// the chosen set is what the mailbox is asked for and what the plan it answers with
// carries, so the chips a reader arranged are the message that leaves. And it may
// not add anyone — an address the answered message did not carry is refused where
// the message's own audience is known (see docket's mail.WithRecipients, which the
// mailbox enforces), so the fake here reproduces the half a handler can get wrong:
// it selects from the assembled audience and cannot invent a member of it.
func TestAChosenAudienceNarrowsTheReplyToTheMessagesOwnAddresses(t *testing.T) {
	h, fake := sendServer(t)

	// Cy was on the message and is moved from cc into to; Carl was not on it at all.
	res := h.do(t, "POST", "/v1/send",
		[]byte(`{"entry":"`+extAda3+`","body":"The 14th works.",`+
			`"to":["bo@fjordline.example","cy@loomworks.example"],`+
			`"cc":["carl@example.net"]}`))
	if res.status != 200 {
		t.Fatalf("status %d: %s", res.status, res.body)
	}
	loadAPI(t).assert(t, "SendResponse", res.body)

	got := decode[sendResponse](t, res)
	if got.To != "Bo Halvorsen <bo@fjordline.example>, Cy Okafor <cy@loomworks.example>" {
		t.Errorf("to = %q, want the two the caller chose", got.To)
	}
	if got.Cc != "" {
		t.Errorf("cc = %q, want nobody: carl was not on the message, whatever was asked for", got.Cc)
	}
	// The handler handed the choice down rather than deciding it, which is the only
	// way the mailbox can be the thing that refuses a widened set.
	if len(fake.replies) != 1 {
		t.Fatalf("the mailbox saw %+v, want one call", fake.replies)
	}
	if !slices.Equal(fake.replies[0].to, []string{"bo@fjordline.example", "cy@loomworks.example"}) {
		t.Errorf("the mailbox was asked for to %v", fake.replies[0].to)
	}
	if !slices.Equal(fake.replies[0].cc, []string{"carl@example.net"}) {
		t.Errorf("the mailbox was asked for cc %v", fake.replies[0].cc)
	}
}

// The empty list a caller can send and an absent one are different facts: absent
// leaves the list as the mailbox assembled it, while an empty cc takes everybody
// off it. That is the one thing a client cannot say any other way, and it is what
// makes "nobody else on this reply" reachable without dropping the whole audience.
func TestAChosenAudienceCanEmptyTheCcAndTheSendCarriesIt(t *testing.T) {
	h, fake := sendServer(t)

	res := h.do(t, "POST", "/v1/send",
		[]byte(`{"entry":"`+extAda3+`","body":"The 14th works.","confirm":true,`+
			`"to":["bo@fjordline.example"],"cc":[]}`))
	if res.status != 200 {
		t.Fatalf("status %d: %s", res.status, res.body)
	}
	loadAPI(t).assert(t, "SendResponse", res.body)

	got := decode[sendResponse](t, res)
	if !got.Sent {
		t.Error("a confirmed send did not send")
	}
	if got.Cc != "" || strings.Contains(string(res.body), `"cc"`) {
		t.Errorf("a reply with cc emptied still carries a cc: %s", res.body)
	}
	if len(fake.replies) != 1 {
		t.Fatalf("the mailbox saw %+v, want one send", fake.replies)
	}
	if fake.replies[0].cc == nil || len(fake.replies[0].cc) != 0 {
		t.Errorf("the mailbox was asked for cc %v, want an empty list rather than none", fake.replies[0].cc)
	}
	if !slices.Equal(fake.replies[0].to, []string{"bo@fjordline.example"}) {
		t.Errorf("the mailbox was asked for to %v", fake.replies[0].to)
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

// The one thing the caller decides about a reply's audience is whether the rest of
// it is on the reply, and the two settings are told apart by the mailbox: what it is
// asked for is what it answers, so the plan and the send are the same message either
// way. The flag narrows to the person who wrote — there is no setting that reaches an
// address the answered message did not carry.
func TestTheReplyAllTickCanOnlyNarrowTheReplyToItsSender(t *testing.T) {
	h, fake := sendServer(t)

	res := h.do(t, "POST", "/v1/send",
		[]byte(`{"entry":"`+extAda3+`","body":"The 14th works.","all":false}`))
	if res.status != 200 {
		t.Fatalf("status %d: %s", res.status, res.body)
	}
	loadAPI(t).assert(t, "SendResponse", res.body)

	got := decode[sendResponse](t, res)
	if got.To != "Bo Halvorsen <bo@fjordline.example>" {
		t.Errorf("to = %q, want the sender even so — only its audience is dropped", got.To)
	}
	if got.Cc != "" {
		t.Errorf("cc = %q, want nobody else", got.Cc)
	}
	if strings.Contains(string(res.body), `"cc"`) {
		t.Errorf("a sender-only reply carries a cc: %s", res.body)
	}
	if len(fake.replies) != 1 || fake.replies[0].all {
		t.Errorf("the mailbox saw %+v, want a reply to the sender alone", fake.replies)
	}
	// And it is still the same message otherwise: the reader's words with the
	// answered message quoted under them, threaded against the message answered.
	if fake.replies[0].id != "g-3" {
		t.Errorf("the reply was threaded against %q, want the answered message's own id",
			fake.replies[0].id)
	}
	for _, want := range []string{"The 14th works.", "> Roof access is fine from the 14th."} {
		if !strings.Contains(got.Body, want) {
			t.Errorf("the plan's body is missing %q:\n%s", want, got.Body)
		}
	}

	// And the send carries the same setting as the preview: a two-step that
	// previewed one audience and sent another would be the one thing the preview
	// exists to rule out.
	res = h.do(t, "POST", "/v1/send",
		[]byte(`{"entry":"`+extAda3+`","body":"The 14th works.","all":false,"confirm":true}`))
	if res.status != 200 {
		t.Fatalf("status %d: %s", res.status, res.body)
	}
	if sent := decode[sendResponse](t, res); !sent.Sent || sent.Cc != "" {
		t.Errorf("the send answered %+v, want it sent with nobody else on it", sent)
	}
	if len(fake.replies) != 2 || !fake.replies[1].send || fake.replies[1].all {
		t.Errorf("the mailbox saw %+v, want a send to the sender alone", fake.replies)
	}
}

// A reply is one message in two forms, and the mailbox is handed both: the plain
// text that says what it says, and the same words marked up for a client that
// renders them. The handler composes them in one call (spec.ComposeReply), so what
// travels here is the pair rather than two renderings that were assembled
// separately and hope to agree.
func TestTheReplyGoesOutInBothForms(t *testing.T) {
	h, fake := sendServer(t)

	res := h.do(t, "POST", "/v1/send",
		[]byte(`{"entry":"`+extAda3+`","body":"The 14th works."}`))
	if res.status != 200 {
		t.Fatalf("status %d: %s", res.status, res.body)
	}
	if len(fake.replies) != 1 {
		t.Fatalf("the mailbox saw %+v, want one call", fake.replies)
	}
	body := fake.replies[0].body

	// The text part is the reply as the plan shows it — the reader's words with the
	// answered message quoted under them — and it is what every client can read.
	for _, want := range []string{
		"The 14th works.",
		"On Tue 3 Mar 2026 10:00 AEDT, Ada Okoye <ada@loomworks.example> wrote:",
		"> Roof access is fine from the 14th.",
	} {
		if !strings.Contains(body.Text, want) {
			t.Errorf("the text part is missing %q:\n%s", want, body.Text)
		}
	}
	// The HTML part is the same message: the words as paragraphs, and the answered
	// message inside a blockquote — the markup a mail client folds a quote by, so the
	// quote is not read as part of the answer.
	for _, want := range []string{
		"<p>The 14th works.</p>",
		"Ada Okoye &lt;ada@loomworks.example&gt; wrote:",
		"<blockquote class=\"gmail_quote\">",
		"<p>Roof access is fine from the 14th.</p>",
	} {
		if !strings.Contains(body.HTML, want) {
			t.Errorf("the HTML part is missing %q:\n%s", want, body.HTML)
		}
	}
	// And the plan answers the HTML half back, because by default that is the form
	// the reply goes out in and the pane draws its preview from it: a reader shown
	// the text part while the HTML part travels would be checking a rendering their
	// correspondent never receives — the quote a `blockquote` on the way out and
	// `> ` lines on screen.
	if got := decode[sendResponse](t, res); got.HTML != body.HTML {
		t.Errorf("the plan does not carry the HTML part that was handed to the mailbox")
	}
	// And the response is the plan of the text part: it is the form the pane shows,
	// and the contract carries one body rather than a pair of them.
	if got := decode[sendResponse](t, res); got.Body != body.Text {
		t.Errorf("the plan's body is not the text part that was sent")
	}
}

// The reader can send the words alone. The second rendering is dropped rather than
// the message being different: the text part is the one the preview showed, byte for
// byte, and the HTML never was the source of it. Absent means it goes, so a client
// that has not been rebuilt sends the message it always sent.
func TestTheReplyGoesOutAsTextAloneWhenAsked(t *testing.T) {
	h, fake := sendServer(t)

	res := h.do(t, "POST", "/v1/send",
		[]byte(`{"entry":"`+extAda3+`","body":"The 14th works.","html":false}`))
	if res.status != 200 {
		t.Fatalf("status %d: %s", res.status, res.body)
	}
	if len(fake.replies) != 1 {
		t.Fatalf("the mailbox saw %+v, want one call", fake.replies)
	}
	body := fake.replies[0].body

	if body.HTML != "" {
		t.Errorf("the mailbox was handed an HTML part anyway:\n%s", body.HTML)
	}
	// The text part is what it would have been either way — including the quote
	// and its heading, which are the server's and not the form's.
	for _, want := range []string{
		"The 14th works.",
		"Ada Okoye <ada@loomworks.example> wrote:",
		"> Roof access is fine from the 14th.",
	} {
		if !strings.Contains(body.Text, want) {
			t.Errorf("the text part is missing %q:\n%s", want, body.Text)
		}
	}
	// And the plan says so, by not carrying an HTML half at all: the field's own
	// presence is the answer to which rendering travels, so a preview draws the text
	// part and no client has to agree about a flag it sent.
	if got := decode[sendResponse](t, res); got.HTML != "" {
		t.Errorf("a text-alone plan carries an HTML part:\n%s", got.HTML)
	}
	if strings.Contains(string(res.body), `"html"`) {
		t.Errorf("a text-alone plan carries an html field: %s", res.body)
	}
	if got := decode[sendResponse](t, res); got.Body != body.Text {
		t.Errorf("the plan's body is not the text part that was sent")
	}
}

func TestARepliesCcIsAbsentWhenThereIsNobodyElse(t *testing.T) {
	h, fake := sendServer(t)
	// The mailbox answers a reply to a message the reader was the only recipient
	// of: the field is left out of the contract rather than sent as an empty
	// string, because "nobody else" and "a field nobody filled in" would
	// otherwise be the same bytes to a client drawing them.
	fake.cc = map[string]string{}

	res := h.do(t, "POST", "/v1/send",
		[]byte(`{"entry":"`+extAda3+`","body":"The 14th works."}`))
	if res.status != 200 {
		t.Fatalf("status %d: %s", res.status, res.body)
	}
	loadAPI(t).assert(t, "SendResponse", res.body)
	if strings.Contains(string(res.body), `"cc"`) {
		t.Errorf("a reply with no other recipients carries a cc: %s", res.body)
	}
	if got := decode[sendResponse](t, res); got.Cc != "" {
		t.Errorf("cc = %q, want empty", got.Cc)
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

// htmlSendServer is the send surface over a message that arrived with markup of its
// own: one entry, one mailbox copy, and a mailbox that answers with its own recipient
// and subject. rThe fixture's html is the one the reading route is tested against
// (htmlPart) — a booking confirmation full of layout, and full of the things the
// allowlist refuses — because the quote of it is the same pass over the same bytes.
func htmlSendServer(t *testing.T) (*harness, *fakeMailbox) {
	t.Helper()
	s, err := corpus.Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	ada := putPerson(t, s, "Ada Okoye", "ada@loomworks.example")
	putMail(t, s, mailFixture{
		ext: extHTML, ts: "2026-03-01T09:00:00+11:00", tz: "AEDT", offset: mins(660),
		person: ada, container: "T2", subject: "Booking confirmed",
		messageID: "<c0ffee-6@loomworks.example>",
		from:      "Ada Okoye <ada@loomworks.example>",
		to:        "Bo Halvorsen <bo@fjordline.example>",
		labels:    []string{"INBOX"}, gmail: "g-6",
		text: "Booking confirmed: Tuesday 3 March, 10:00.",
		html: htmlPart,
	})
	if _, err := s.ResolveParents(); err != nil {
		t.Fatalf("ResolveParents: %v", err)
	}

	h := harnessOver(t, s)
	h.sendMailEnabled = true
	fake := &fakeMailbox{
		base:    map[string][]string{"g-6": {"INBOX"}},
		to:      map[string]string{"g-6": "Bo Halvorsen <bo@fjordline.example>"},
		subject: map[string]string{"g-6": "Re: Booking confirmed"},
	}
	h.openMailbox = func() (mailbox, error) { return fake, nil }
	return h, fake
}

// The quote's own source, end to end: the message being answered arrived with markup,
// so the HTML part of the answer carries that markup rather than the transcript the
// text part beside it is. Until this, answering an HTML mail sent its text under a
// blockquote — a table as the cells' lines, a link as its label — which reads as a
// transcript of a mail rather than as the mail.
func TestASentReplyQuotesTheAnsweredMessagesMarkup(t *testing.T) {
	h, fake := htmlSendServer(t)

	res := h.do(t, "POST", "/v1/send",
		[]byte(`{"entry":"`+extHTML+`","body":"Both dates work."}`))
	if res.status != 200 {
		t.Fatalf("status %d: %s", res.status, res.body)
	}
	got := decode[sendResponse](t, res)
	if len(fake.replies) != 1 {
		t.Fatalf("the mailbox saw %+v, want one call", fake.replies)
	}
	html := fake.replies[0].body.HTML

	// The message's own markup, marked up as its sender marked it up.
	for _, want := range []string{
		"<h1>Booking confirmed</h1>",
		"<b>Tuesday 3 March, 10:00</b>",
		`<a href="https://example.example/booking/1">Details</a>`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("the reply's HTML is missing the answered message's own %q:\n%s", want, html)
		}
	}
	// And none of what the same fixture's sender put in there to be refused: the quote
	// passes the allowlist every body in the reading pane passes, so a reply can relay
	// to its recipients nothing the page would refuse to render.
	for _, bad := range []string{"<script", "onclick", "javascript:", "<style", "assets.example.example", "<title>"} {
		if strings.Contains(html, bad) {
			t.Errorf("the reply's HTML carries %q, which the allowlist refuses:\n%s", bad, html)
		}
	}
	// The text part is the message's own text, one level in, because a text part has
	// nowhere to put markup: the two forms are each the message's own reading of one
	// message, and neither is a conversion of the other.
	if !strings.Contains(fake.replies[0].body.Text, "\n> Booking confirmed: Tuesday 3 March, 10:00.") {
		t.Errorf("the reply's text lost the answered message:\n%s", fake.replies[0].body.Text)
	}
	// What the reader was shown is what the mailbox was handed: the plan is the
	// message rather than a draft of one.
	if got.Body != fake.replies[0].body.Text {
		t.Errorf("the plan previews %q, the mailbox was handed %q", got.Body, fake.replies[0].body.Text)
	}
}
