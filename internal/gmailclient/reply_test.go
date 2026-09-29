package gmailclient

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/mail"
	"strings"
	"testing"

	"google.golang.org/api/gmail/v1"
	"google.golang.org/api/option"

	dmail "github.com/zachpmanson/docket/gmail/mail"
)

// Replying, end to end through the real docket client: the message that would be
// handed to Gmail is built by docket from this package's own request, so what is
// asserted here is the last thing before a send — the bytes, and the addresses in
// their headers.
//
// The fake mailbox in cmd/server cannot show this. It answers the plan a mailbox
// would answer and records what it was asked for; the message itself is only
// assembled on the other side of the client, by the library that will send it. So
// the five endpoints Gmail's REST surface is asked for are stood up here instead
// (the answered message, the profile, the send-as aliases, the send, and the read
// back of what went out), and the assertion is made on the raw message Gmail would
// have received.
//
// That is also where the audience itself is checked: an address the reader typed
// is the one thing this surface could not reach before, so the test below asserts
// it arrives in the headers and in the bytes — the difference between a client
// that drew a chip and a message that goes where the chip said. What is refused is
// the shape of a list rather than its membership (an entry that is not an address,
// an address named twice), and it is still refused before anything is handed over.

// fakeGmail is the mailbox those endpoints describe: one message to answer, one
// address that is this account's, and every message handed to the send endpoint,
// undecoded.
type fakeGmail struct {
	// t is the test the fake answers into: a request it does not know is a fact
	// about this package's client, not about the fake, so it is reported rather
	// than answered with a 404.
	t *testing.T
	// headers are the answered message's, as a metadata read answers them.
	headers []*gmail.MessagePartHeader
	// me is the address the profile answers with, and the send-as alias beside it.
	me string
	// sent is every message the send endpoint received, decoded from the base64url
	// the API carries it in.
	sent [][]byte
}

// replyFixture is a fake Gmail and a service pointed at it, with the answered
// message the tests below arrange: a sender in To, a colleague in To, and a third
// party in Cc who is the one a reader would take off a reply.
func replyFixture(t *testing.T) (*fakeGmail, *gmail.Service) {
	t.Helper()
	f := &fakeGmail{
		t:  t,
		me: "reader@example.com",
		headers: []*gmail.MessagePartHeader{
			{Name: "From", Value: "Dana Okafor <dana@example.com>"},
			{Name: "To", Value: "reader@example.com, carl@example.net"},
			{Name: "Cc", Value: "ops@example.org"},
			{Name: "Subject", Value: "quarterly widget audit"},
			{Name: "Message-Id", Value: "<m1@example.com>"},
		},
	}
	srv := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(srv.Close)
	svc, err := gmail.NewService(context.Background(),
		option.WithEndpoint(srv.URL+"/"), option.WithoutAuthentication())
	if err != nil {
		t.Fatalf("building the fake gmail service: %v", err)
	}
	return f, svc
}

// client is this package's own Client, pointed at the fake mailbox. Built here
// rather than through New because New opens the real auth store and the real
// mailbox: what is under test is the reply path, and the one seam it needs is the
// service. The label cache is empty on purpose — the reply path only reads from it
// to name the labels of the message it read back, and an unknown id passes through
// as itself.
func replyClient(svc *gmail.Service) Client {
	return Client{ctx: context.Background(), svc: svc, labels: &dmail.LabelCache{}}
}

func (f *fakeGmail) serve(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/gmail/v1/users/me")
	write := func(v any) {
		w.Header().Set("content-type", "application/json")
		if err := json.NewEncoder(w).Encode(v); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
	switch {
	case path == "/profile":
		write(map[string]string{"emailAddress": f.me})
	case path == "/settings/sendAs":
		write(map[string]any{"sendAs": []map[string]string{{"sendAsEmail": f.me}}})
	case path == "/messages/send" && r.Method == http.MethodPost:
		var req gmail.Message
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		raw, err := base64.RawURLEncoding.DecodeString(req.Raw)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		f.sent = append(f.sent, raw)
		write(map[string]string{"id": "sent-1", "threadId": "T1"})
	case strings.HasPrefix(path, "/messages/"):
		// Two reads come here: the message being answered, and the message that just
		// went out. They are told apart by the id, and the second one exists because
		// the client files what it sent by reading it back.
		id := strings.TrimPrefix(path, "/messages/")
		headers := f.headers
		if id == "sent-1" {
			headers = []*gmail.MessagePartHeader{
				{Name: "From", Value: "reader@example.com"},
				{Name: "To", Value: "Dana Okafor <dana@example.com>"},
				{Name: "Subject", Value: "Re: quarterly widget audit"},
				{Name: "Message-Id", Value: "<sent-1@example.com>"},
			}
		}
		write(&gmail.Message{Id: id, ThreadId: "T1", Payload: &gmail.MessagePart{Headers: headers}})
	default:
		f.t.Errorf("the client asked for %s %s, which this fake does not answer", r.Method, r.URL.Path)
		http.NotFound(w, r)
	}
}

// sentMessage is the one message the fake was handed, parsed: the headers are the
// assertion, so they are read back rather than searched for in the bytes.
func (f *fakeGmail) sentMessage(t *testing.T) *mail.Message {
	t.Helper()
	if len(f.sent) != 1 {
		t.Fatalf("the mailbox was handed %d messages, want exactly one", len(f.sent))
	}
	msg, err := mail.ReadMessage(strings.NewReader(string(f.sent[0])))
	if err != nil {
		t.Fatalf("the message that was handed over does not parse: %v\n%s", err, f.sent[0])
	}
	return msg
}

// The whole point of the change, at the last place it can be checked: a reply
// narrowed before it is sent goes out carrying exactly the addresses the preview
// showed — the dropped one is not in the headers and not in the bytes — and the
// plan answers with the same set the reader chose.
func TestAChosenAudienceIsWhatTheMailboxIsHanded(t *testing.T) {
	f, svc := replyFixture(t)
	c := replyClient(svc)

	// The reply-all plan would carry Dana in To and both Carl and ops in Cc; ops is
	// taken off and Carl is moved into To.
	plan, err := c.Reply("m1", ReplyBody{Text: "noted"}, ReplyOptions{
		All:  true,
		Send: true,
		To:   []string{"dana@example.com", "carl@example.net"},
		Cc:   []string{},
	})
	if err != nil {
		t.Fatalf("sending the reply: %v", err)
	}
	if plan.To != "Dana Okafor <dana@example.com>, carl@example.net" {
		t.Errorf("plan.To = %q, want the two the caller chose", plan.To)
	}
	if plan.Cc != "" {
		t.Errorf("plan.Cc = %q, want nobody: the third party was taken off", plan.Cc)
	}
	if len(plan.CcRecipients) != 0 {
		t.Errorf("plan.CcRecipients = %+v, want none", plan.CcRecipients)
	}

	sent := f.sentMessage(t)
	if got := sent.Header.Get("To"); got != "Dana Okafor <dana@example.com>, carl@example.net" {
		t.Errorf("the message's To = %q", got)
	}
	if got := sent.Header.Get("Cc"); got != "" {
		t.Errorf("the message's Cc = %q, want no Cc header at all", got)
	}
	if strings.Contains(string(f.sent[0]), "ops@example.org") {
		t.Errorf("the dropped address is still in the message:\n%s", f.sent[0])
	}
	// The reply is threaded onto the message it answers, so the narrowed audience
	// did not cost the message its place in the conversation.
	if got := sent.Header.Get("In-Reply-To"); got != "<m1@example.com>" {
		t.Errorf("In-Reply-To = %q, want the answered message's own id", got)
	}
}

// What the reader typed, through this package rather than only in docket's own
// tests: an address the answered message never carried is now one the reply goes
// to, in the header and in the bytes. This is the widening the reply box's address
// field needs, and it is asserted here because the client assembling the message
// is the last thing between a chip on a screen and a send.
func TestAnAddressTheReaderTypedReachesTheMessage(t *testing.T) {
	f, svc := replyFixture(t)
	c := replyClient(svc)

	plan, err := c.Reply("m1", ReplyBody{Text: "noted"}, ReplyOptions{
		All:  true,
		Send: true,
		// One address typed by hand, and one a client knows a person's name for.
		To: []string{"dana@example.com", "stranger@example.com"},
		Cc: []string{"Ada Okoye <ada@example.org>"},
	})
	if err != nil {
		t.Fatalf("sending the reply: %v", err)
	}
	if plan.To != "Dana Okafor <dana@example.com>, stranger@example.com" {
		t.Errorf("plan.To = %q, want the sender and the typed address", plan.To)
	}
	if plan.Cc != "Ada Okoye <ada@example.org>" {
		t.Errorf("plan.Cc = %q, want the named address", plan.Cc)
	}

	sent := f.sentMessage(t)
	if got := sent.Header.Get("To"); got != "Dana Okafor <dana@example.com>, stranger@example.com" {
		t.Errorf("the message's To = %q", got)
	}
	if got := sent.Header.Get("Cc"); got != "Ada Okoye <ada@example.org>" {
		t.Errorf("the message's Cc = %q", got)
	}
	// The address the message did not carry is the message's to reach — and the
	// one it did still keeps the name the answered message gave it.
	if len(plan.ToRecipients) != 2 ||
		plan.ToRecipients[0] != (Recipient{Name: "Dana Okafor", Address: "dana@example.com"}) ||
		plan.ToRecipients[1] != (Recipient{Address: "stranger@example.com"}) {
		t.Errorf("ToRecipients = %+v", plan.ToRecipients)
	}
	if len(plan.CcRecipients) != 1 ||
		plan.CcRecipients[0] != (Recipient{Name: "Ada Okoye", Address: "ada@example.org"}) {
		t.Errorf("CcRecipients = %+v", plan.CcRecipients)
	}
	// Threading survives the rebuild: widening the audience makes a reply to more
	// people, not a new thread.
	if got := sent.Header.Get("In-Reply-To"); got != "<m1@example.com>" {
		t.Errorf("In-Reply-To = %q, want the answered message's own id", got)
	}
}

// What is still refused, through this package rather than only in docket's own
// tests: an audience that is not a list of addresses. A malformed entry and one
// address named twice are both refused BEFORE anything is handed over — a reply
// that failed is one that sent nothing.
func TestAnAudienceThatIsNotAddressesIsRefusedAndNothingIsSent(t *testing.T) {
	f, svc := replyFixture(t)
	c := replyClient(svc)

	_, err := c.Reply("m1", ReplyBody{Text: "noted"}, ReplyOptions{
		All: true, Send: true, To: []string{"not an address"},
	})
	if err == nil {
		t.Fatal("a string that is not an address was accepted")
	}
	if !strings.Contains(err.Error(), ErrUnsent.Error()) {
		t.Errorf("the refusal does not say nothing was sent: %v", err)
	}
	// One address in both lists is one recipient said twice — the caller chooses a
	// list for an address, and being in two of them is not a choice.
	if _, err := c.Reply("m1", ReplyBody{Text: "noted"}, ReplyOptions{
		All: true, Send: true,
		To: []string{"stranger@example.com"},
		Cc: []string{"Stranger@example.com"},
	}); err == nil {
		t.Error("an address named in both lists was accepted")
	}
	if len(f.sent) != 0 {
		t.Errorf("a refused reply was handed to the mailbox anyway: %s", f.sent[0])
	}
}

// The plan a preview answers with is the set a send starts from, and naming
// nobody sends exactly it: a client that showed the reader one audience and sent
// another would be the one thing the two-step exists to rule out.
func TestThePreviewNamesTheSameAudienceTheSendUses(t *testing.T) {
	f, svc := replyFixture(t)
	c := replyClient(svc)

	preview, err := c.Reply("m1", ReplyBody{Text: "noted"}, ReplyOptions{All: true})
	if err != nil {
		t.Fatalf("preparing the reply: %v", err)
	}
	if len(f.sent) != 0 {
		t.Fatalf("a preview sent something: %s", f.sent[0])
	}
	// The plan's recipients are the answered message's own audience, minus this
	// mailbox, as addresses — which is what the address field a reader edits is
	// seeded with, and what a request that names no `to`/`cc` sends.
	wantTo := []Recipient{{Name: "Dana Okafor", Address: "dana@example.com"}}
	if len(preview.ToRecipients) != 1 || preview.ToRecipients[0] != wantTo[0] {
		t.Errorf("ToRecipients = %+v, want %+v", preview.ToRecipients, wantTo)
	}
	if len(preview.CcRecipients) != 2 ||
		preview.CcRecipients[0].Address != "carl@example.net" ||
		preview.CcRecipients[1].Address != "ops@example.org" {
		t.Errorf("CcRecipients = %+v, want carl and ops", preview.CcRecipients)
	}

	// Now the send names the preview's own set, spelled the way the preview spelled
	// it, and the message carries exactly that.
	chosen, err := c.Reply("m1", ReplyBody{Text: "noted"}, ReplyOptions{
		All:  true,
		Send: true,
		To:   []string{preview.ToRecipients[0].Address},
		Cc:   []string{preview.CcRecipients[1].Address},
	})
	if err != nil {
		t.Fatalf("sending the reply: %v", err)
	}
	sent := f.sentMessage(t)
	if got := sent.Header.Get("To"); got != "Dana Okafor <dana@example.com>" {
		t.Errorf("the message's To = %q, want the preview's own recipient", got)
	}
	if got := sent.Header.Get("Cc"); got != "ops@example.org" {
		t.Errorf("the message's Cc = %q, want the one the preview named", got)
	}
	if chosen.GmailID == "" {
		t.Error("a confirmed send answers with no id for the message that went out")
	}
}
