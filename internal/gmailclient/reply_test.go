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
// That is also where the hard limit is enforced rather than merely stated: an
// address the answered message did not carry has to be REFUSED by the mailbox, not
// dropped by the caller, or the page behind a loopback bind with no authentication
// could be turned into an outbound sender by a client that asked nicely.

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

// The hard limit, through this package rather than only in docket's own tests: an
// address the answered message did not carry is refused, and refused BEFORE
// anything is handed over — a reply that failed is one that sent nothing.
func TestAnAddressTheMessageDidNotCarryIsRefusedAndNothingIsSent(t *testing.T) {
	f, svc := replyFixture(t)
	c := replyClient(svc)

	_, err := c.Reply("m1", ReplyBody{Text: "noted"}, ReplyOptions{
		All:  true,
		Send: true,
		To:   []string{"dana@example.com", "stranger@example.com"},
	})
	if err == nil {
		t.Fatal("an address the message did not carry was accepted")
	}
	if !strings.Contains(err.Error(), "carried") {
		t.Errorf("the refusal does not say why: %v", err)
	}
	if !strings.Contains(err.Error(), ErrUnsent.Error()) {
		t.Errorf("the refusal does not say nothing was sent: %v", err)
	}
	if len(f.sent) != 0 {
		t.Errorf("a refused reply was handed to the mailbox anyway: %s", f.sent[0])
	}
	// And the mailbox's own address cannot be put back on: it was resolved off the
	// message when the plan was assembled, so it is outside the set a send may name
	// — which is what keeps a reply from cc'ing its own reader.
	if _, err := c.Reply("m1", ReplyBody{Text: "noted"}, ReplyOptions{
		All: true, Send: true, To: []string{"reader@example.com"},
	}); err == nil {
		t.Error("an address belonging to the mailbox was accepted")
	}
	if len(f.sent) != 0 {
		t.Errorf("a refused reply was handed to the mailbox anyway: %s", f.sent[0])
	}
}

// The plan a preview answers with is the set the send may name, and it is the same
// set the message ends up carrying: a client that showed the reader one audience
// and sent another would be the one thing the two-step exists to rule out.
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
	// mailbox, as addresses — which is what the chips a reader narrows are drawn
	// from and the whole of what a request may name.
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
