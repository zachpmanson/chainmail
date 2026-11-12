package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/zachpmanson/chainmail/internal/corpus"
	"github.com/zachpmanson/chainmail/internal/spec"
)

func TestReplyDraftRestoresAndResavesTheQuoteExactlyOnce(t *testing.T) {
	h, fake := draftsServer(t)
	h.sendMailEnabled = true
	target, err := h.store.ReplyTarget(extAda1)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.store.PutGmailCopy(corpus.GmailCopy{AccountID: "legacy", GmailID: "gmail-parent", EntryID: target.ID}); err != nil {
		t.Fatal(err)
	}
	want := spec.ComposeReply("**My answer.**\n\n> My own quotation.", target)
	input := draftRequest{Body: "**My answer.**\n\n> My own quotation.", ReplyToEntry: extAda1}
	path, method := "/v1/drafts", "POST"
	for i := 0; i < 3; i++ {
		payload, err := json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		saved := h.do(t, method, path, payload)
		if saved.status != 200 {
			t.Fatalf("save %d: status %d: %s", i, saved.status, saved.body)
		}
		d := decode[draftResponse](t, saved)
		if d.Body != want.Text || fake.saved[i].Body != want.Text {
			t.Fatalf("save %d lost or duplicated quote: response=%q, mailbox=%q, want=%q", i, d.Body, fake.saved[i].Body, want.Text)
		}
		path, method = "/v1/drafts/"+d.ID, "PUT"
		read := h.do(t, "GET", path, nil)
		if read.status != 200 {
			t.Fatalf("restore: status %d: %s", read.status, read.body)
		}
		restored := decode[draftResponse](t, read)
		if restored.Body != want.Text {
			t.Fatalf("restore lost quote: %q", restored.Body)
		}
		list := h.do(t, "GET", "/v1/drafts", nil)
		if list.status != 200 {
			t.Fatalf("list: status %d: %s", list.status, list.body)
		}
		found := false
		for _, listed := range decode[[]draftResponse](t, list) {
			if listed.ID == d.ID {
				found = listed.Body == want.Text
			}
		}
		if !found {
			t.Fatal("saved quote missing from draft list")
		}
		input.Body, input.ExpectedMessageID = restored.Body, restored.MessageID
	}
	// Editing authored text in a restored draft still leaves just one parent quote.
	input.Body = strings.Replace(input.Body, "My answer.", "My edited answer.", 1)
	payload, _ := json.Marshal(input)
	updated := h.do(t, "PUT", path, payload)
	if updated.status != 200 {
		t.Fatalf("edited save: %d: %s", updated.status, updated.body)
	}
	want = spec.ComposeReply("**My edited answer.**\n\n> My own quotation.", target)
	if decode[draftResponse](t, updated).Body != want.Text {
		t.Fatalf("edited save changed quote: %s", updated.body)
	}
	// The browser submits the hydrated body to /send, not /drafts/{id}/send.
	previewInput, _ := json.Marshal(map[string]any{"entry": extAda1, "body": want.Text})
	preview := h.do(t, "POST", "/v1/send", previewInput)
	if preview.status != 200 {
		t.Fatalf("preview: %d: %s", preview.status, preview.body)
	}
	plan := decode[sendResponse](t, preview)
	if plan.Body != want.Text || plan.HTML != want.HTML || plan.Sent {
		t.Fatalf("restored preview changed reply: %+v, want %+v", plan, want)
	}
	for _, call := range fake.replies {
		if call.send {
			t.Fatal("draft save/preview sent mail")
		}
	}
}
