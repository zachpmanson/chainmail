package main

import (
	"errors"
	"strings"
	"testing"
)

func TestComposeRequiresSendGrant(t *testing.T) {
	h, fake := readServer(t)
	opened := false
	h.openMailbox = func() (mailbox, error) {
		opened = true
		return fake, nil
	}
	res := h.do(t, "POST", "/v1/compose", []byte(`{"to":"ada@example.test","subject":"Hello","body":"Hi"}`))
	if res.status != 403 || !strings.Contains(res.errText(t), "-send-mail") {
		t.Fatalf("status %d: %s", res.status, res.body)
	}
	if opened {
		t.Fatal("refused compose opened the mailbox")
	}
}

func TestComposePreviewReturnsExactTextWithoutSending(t *testing.T) {
	h, _ := sendServer(t)
	res := h.do(t, "POST", "/v1/compose", []byte(`{"to":"ada@example.test","subject":"Hello","body":"Hi\nthere"}`))
	if res.status != 200 {
		t.Fatalf("status %d: %s", res.status, res.body)
	}
	got := decode[composeResponse](t, res)
	if got.To != "ada@example.test" || got.Subject != "Hello" || got.Body != "Hi\nthere" || got.Sent || got.GmailID != "" || got.Filed != nil {
		t.Fatalf("unexpected preview: %+v", got)
	}
}

func TestComposeConfirmedSendAndFilingResult(t *testing.T) {
	h, fake := sendServer(t)
	res := h.do(t, "POST", "/v1/compose", []byte(`{"to":"ada@example.test","subject":"Hello","body":"Hi\nthere","confirm":true}`))
	if res.status != 200 {
		t.Fatalf("status %d: %s", res.status, res.body)
	}
	got := decode[composeResponse](t, res)
	if !got.Sent || got.GmailID != "sent-compose-id" || got.Filed == nil || !*got.Filed {
		t.Fatalf("unexpected sent result: %+v", got)
	}
	if len(fake.composes) != 1 || !fake.composes[0].send || fake.composes[0].to != "ada@example.test" || fake.composes[0].subject != "Hello" || fake.composes[0].body != "Hi\nthere" {
		t.Fatalf("unexpected mailbox compose calls: %+v", fake.composes)
	}
}

func TestComposeUncertainSendDoesNotClaimSuccess(t *testing.T) {
	h, fake := sendServer(t)
	fake.composeErr = errors.New("transport failed")
	res := h.do(t, "POST", "/v1/compose", []byte(`{"to":"ada@example.test","subject":"Hello","body":"Hi","confirm":true}`))
	if res.status != 502 || !strings.Contains(res.errText(t), "whether it went out is unknown") {
		t.Fatalf("status %d: %s", res.status, res.body)
	}
	if len(fake.composes) != 1 || !fake.composes[0].send {
		t.Fatalf("unexpected mailbox compose calls: %+v", fake.composes)
	}
}

func TestComposeReportsFilingFailureAfterSuccessfulSend(t *testing.T) {
	h, fake := sendServer(t)
	fake.readErr = errors.New("read back failed")
	res := h.do(t, "POST", "/v1/compose", []byte(`{"to":"ada@example.test","subject":"Hello","body":"Hi","confirm":true}`))
	if res.status != 200 {
		t.Fatalf("status %d: %s", res.status, res.body)
	}
	got := decode[composeResponse](t, res)
	if !got.Sent || got.Filed == nil || *got.Filed {
		t.Fatalf("unexpected sent/filing result: %+v", got)
	}
}

func TestComposeRejectsMissingFieldsBeforeOpeningMailbox(t *testing.T) {
	h, fake := sendServer(t)
	opened := false
	h.openMailbox = func() (mailbox, error) {
		opened = true
		return fake, nil
	}
	for _, body := range []string{
		`{"subject":"Hello","body":"Hi"}`,
		`{"to":"ada@example.test","body":"Hi"}`,
		`{"to":"ada@example.test","subject":"Hello"}`,
		`{"to":"not an address","subject":"Hello","body":"Hi"}`,
	} {
		res := h.do(t, "POST", "/v1/compose", []byte(body))
		if res.status != 400 {
			t.Errorf("request %s: status %d: %s", body, res.status, res.body)
		}
	}
	if opened {
		t.Fatal("invalid request opened the mailbox")
	}
}
