package main

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/zachpmanson/chainmail/internal/corpus"
	"github.com/zachpmanson/chainmail/internal/gmailclient"
)

type draftFake struct {
	*fakeMailbox
	drafts       map[string]gmailclient.Draft
	deleted      []string
	saved        []gmailclient.Draft
	sent         []string
	saveErr      error
	replyParents []string
}

func newDraftFake() *draftFake {
	return &draftFake{fakeMailbox: &fakeMailbox{}, drafts: map[string]gmailclient.Draft{
		"d1": {ID: "d1", ThreadID: "thread-1", To: "ada@example.test", Subject: "Saved", Body: "Hello"},
	}}
}

func (f *draftFake) ListDrafts() ([]gmailclient.Draft, error) {
	out := make([]gmailclient.Draft, 0, len(f.drafts))
	for _, d := range f.drafts {
		out = append(out, d)
	}
	return out, nil
}
func (f *draftFake) GetDraft(id string) (gmailclient.Draft, error) {
	d, ok := f.drafts[id]
	if !ok {
		return gmailclient.Draft{}, errors.New("draft not found")
	}
	return d, nil
}
func (f *draftFake) SaveDraft(d gmailclient.Draft, replyTo string) (gmailclient.Draft, error) {
	f.replyParents = append(f.replyParents, replyTo)
	if f.saveErr != nil {
		return gmailclient.Draft{}, f.saveErr
	}
	if d.ID == "" {
		d.ID = "new-draft"
	}
	f.drafts[d.ID] = d
	f.saved = append(f.saved, d)
	return d, nil
}
func (f *draftFake) DeleteDraft(id string) error {
	if _, ok := f.drafts[id]; !ok {
		return errors.New("draft not found")
	}
	delete(f.drafts, id)
	f.deleted = append(f.deleted, id)
	return nil
}
func (f *draftFake) SendDraft(id string) (string, string, error) {
	if _, ok := f.drafts[id]; !ok {
		return "", "", errors.New("draft not found")
	}
	f.sent = append(f.sent, id)
	return "sent-message", "thread-1", nil
}

func draftsServer(t *testing.T) (*harness, *draftFake) {
	t.Helper()
	h := testServer(t)
	fake := newDraftFake()
	h.openMailbox = func() (mailbox, error) { return fake, nil }
	h.draftMailEnabled = true
	return h, fake
}

func TestDraftListAndGetReadGmailState(t *testing.T) {
	h, _ := draftsServer(t)
	list := h.do(t, "GET", "/v1/drafts", nil)
	if list.status != 200 {
		t.Fatalf("list status %d: %s", list.status, list.body)
	}
	got := decode[[]draftResponse](t, list)
	if len(got) != 1 || got[0].ID != "d1" || got[0].Draft.ThreadID != "thread-1" {
		t.Fatalf("list = %+v", got)
	}
	one := h.do(t, "GET", "/v1/drafts/d1", nil)
	if one.status != 200 || decode[draftResponse](t, one).Subject != "Saved" {
		t.Fatalf("get status %d: %s", one.status, one.body)
	}
}

func TestDraftCreateUpdateAndDeleteRequireDraftGrant(t *testing.T) {
	h, fake := draftsServer(t)
	create := h.do(t, "POST", "/v1/drafts", []byte(`{"to":["ada@example.test"],"subject":"New","body":"Hello"}`))
	if create.status != 200 || decode[draftResponse](t, create).ID != "new-draft" {
		t.Fatalf("create status %d: %s", create.status, create.body)
	}
	update := h.do(t, "PUT", "/v1/drafts/d1", []byte(`{"to":["bo@example.test"],"subject":"Changed","body":"Again","expectedMessageId":"message-1"}`))
	if update.status != 200 || decode[draftResponse](t, update).To != "bo@example.test" || fake.saved[len(fake.saved)-1].MessageID != "message-1" {
		t.Fatalf("update status %d: %s", update.status, update.body)
	}
	remove := h.do(t, "DELETE", "/v1/drafts/d1", nil)
	if remove.status != 200 || len(fake.deleted) != 1 || fake.deleted[0] != "d1" {
		t.Fatalf("delete status %d: %s; deleted=%v", remove.status, remove.body, fake.deleted)
	}
	h.draftMailEnabled = false
	refused := h.do(t, "POST", "/v1/drafts", []byte(`{"to":[],"subject":"","body":""}`))
	if refused.status != 403 || !strings.Contains(refused.errText(t), "-draft-mail") {
		t.Fatalf("status %d: %s", refused.status, refused.body)
	}
}

func TestReplyDraftResolvesParentMessageForSelectedAccount(t *testing.T) {
	h, fake := draftsServer(t)
	entries, err := h.store.ChainEntries(extAda1)
	if err != nil || len(entries) == 0 {
		t.Fatalf("chain entries = %v, %v", entries, err)
	}
	if err := h.store.PutGmailCopy(corpus.GmailCopy{AccountID: "legacy", GmailID: "gmail-parent", EntryID: entries[0].ID}); err != nil {
		t.Fatal(err)
	}
	res := h.do(t, "POST", "/v1/drafts", []byte(`{"to":["bo@example.test"],"subject":"Re: hello","body":"reply","replyToEntry":"`+extAda1+`"}`))
	if res.status != 200 {
		t.Fatalf("status %d: %s", res.status, res.body)
	}
	if len(fake.replyParents) != 1 || fake.replyParents[0] != "gmail-parent" {
		t.Fatalf("reply parents = %v", fake.replyParents)
	}
}

func TestDraftUpdateConflictIsSurfaced(t *testing.T) {
	h, fake := draftsServer(t)
	fake.saveErr = gmailclient.ErrDraftConflict
	res := h.do(t, "PUT", "/v1/drafts/d1", []byte(`{"to":[],"subject":"Changed","body":"Again","expectedMessageId":"message-1"}`))
	if res.status != 409 || !strings.Contains(res.errText(t), "changed outside Chainmail") {
		t.Fatalf("status %d: %s", res.status, res.body)
	}
}

func TestDraftSendRequiresSendGrantAndFilesMessage(t *testing.T) {
	h, fake := draftsServer(t)
	refused := h.do(t, "POST", "/v1/drafts/d1/send", []byte(`{}`))
	if refused.status != 403 || !strings.Contains(refused.errText(t), "-send-mail") {
		t.Fatalf("status %d: %s", refused.status, refused.body)
	}
	h.sendMailEnabled = true
	sent := h.do(t, "POST", "/v1/drafts/d1/send", []byte(`{}`))
	if sent.status != 200 {
		t.Fatalf("status %d: %s", sent.status, sent.body)
	}
	var got map[string]any
	if err := json.Unmarshal(sent.body, &got); err != nil {
		t.Fatal(err)
	}
	if got["gmailId"] != "sent-message" || got["threadId"] != "thread-1" || got["sent"] != true {
		t.Fatalf("response = %v", got)
	}
	if len(fake.sent) != 1 || fake.sent[0] != "d1" {
		t.Fatalf("sent = %v", fake.sent)
	}
}
