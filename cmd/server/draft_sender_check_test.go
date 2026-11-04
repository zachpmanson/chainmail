package main

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/zachpmanson/chainmail/internal/gmailclient"
	"google.golang.org/api/googleapi"
)

type senderCheckFake struct {
	*draftFake
	checked []string
	err     error
}

func (f *senderCheckFake) CheckDraftSender(id string) error {
	f.checked = append(f.checked, id)
	return f.err
}

func TestDraftSenderCheckIsReadOnly(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		status int
	}{
		{name: "supported", status: 200},
		{name: "refusal", err: fmt.Errorf("%w [reason=sender_display_name_unconfigured; headers=From]", gmailclient.ErrUnsupportedDraft), status: 409},
		{name: "deleted", err: &googleapi.Error{Code: 404}, status: 404},
		{name: "provider error", err: errors.New("synthetic failure"), status: 502},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, f := draftsServer(t)
			checker := &senderCheckFake{draftFake: f, err: tc.err}
			h.openMailbox = func() (mailbox, error) { return checker, nil }
			h.draftMailEnabled, h.sendMailEnabled = false, false
			res := h.do(t, "GET", "/v1/drafts/d1/sender-check", nil)
			if res.status != tc.status {
				t.Fatalf("status %d: %s", res.status, res.body)
			}
			if len(checker.checked) != 1 || checker.checked[0] != "d1" {
				t.Fatalf("checks: %v", checker.checked)
			}
			if len(f.saved)+len(f.sent)+len(f.deleted) != 0 || len(f.replies) != 0 {
				t.Fatal("sender check mutated mailbox or prepared reply")
			}
			if tc.err == nil && !strings.Contains(string(res.body), `"senderCompatible":true`) {
				t.Fatalf("response: %s", res.body)
			}
			if tc.name == "refusal" && !strings.Contains(res.errText(t), "sender_display_name_unconfigured") {
				t.Fatalf("response: %s", res.body)
			}
			for _, method := range []string{"POST", "PUT", "DELETE"} {
				if got := h.do(t, method, "/v1/drafts/d1/sender-check", nil); got.status != 405 {
					t.Fatalf("%s: %d", method, got.status)
				}
			}
			if len(checker.checked) != 1 {
				t.Fatal("non-GET request reached mailbox")
			}
		})
	}
}

func TestDraftSenderCheckRejectsUnavailableAccount(t *testing.T) {
	h, f := draftsServer(t)
	checker := &senderCheckFake{draftFake: f}
	h.openMailbox = func() (mailbox, error) { return checker, nil }
	res := h.do(t, "GET", "/v1/drafts/d1/sender-check?accountId=missing", nil)
	if res.status != 409 || len(checker.checked) != 0 {
		t.Fatalf("status %d: %s; checks %v", res.status, res.body, checker.checked)
	}
}
