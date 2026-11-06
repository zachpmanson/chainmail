package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/zachpmanson/chainmail/internal/gmailclient"
	"google.golang.org/api/googleapi"
)

type saveCheckFake struct {
	*draftFake
	checked []string
	err     error
}

func (f *saveCheckFake) CheckDraftSave(id string) (gmailclient.DraftSaveCheck, error) {
	f.checked = append(f.checked, id)
	return gmailclient.DraftSaveCheck{UnchangedCompatible: true, BodyEditCompatible: true, Context: "compose"}, f.err
}

func TestDraftSaveCheckAccountGETOnlyAndPrivacy(t *testing.T) {
	h, f, opened := mimeCheckServer(t)
	checker := &saveCheckFake{draftFake: f.draftFake}
	h.openMailboxForAccount = func(id string) (mailbox, error) { *opened = append(*opened, id); return checker, nil }
	for _, account := range []string{"work", "personal"} {
		res := h.do(t, "GET", "/v1/drafts/PRIVATE/save-check?accountId="+account, nil)
		if res.status != 200 {
			t.Fatalf("status %d: %s", res.status, res.body)
		}
		loadAPI(t).assert(t, "DraftSaveCheck", res.body)
		if strings.Contains(string(res.body), "PRIVATE") || strings.Contains(string(res.body), "accountId") || !strings.Contains(string(res.body), `"bodyEditCompatible":true`) {
			t.Fatalf("response: %s", res.body)
		}
	}
	if strings.Join(*opened, ",") != "work,personal" || len(checker.checked) != 2 {
		t.Fatalf("account routing: %v %v", *opened, checker.checked)
	}
	for _, method := range []string{"POST", "PUT", "DELETE", "PATCH", "HEAD", "OPTIONS"} {
		if res := h.do(t, method, "/v1/drafts/d1/save-check?accountId=personal", nil); res.status != 405 {
			t.Fatalf("%s status %d", method, res.status)
		}
	}
	for _, tc := range []struct {
		query  string
		status int
	}{{"", 400}, {"?accountId=", 400}, {"?accountId=%20", 400}, {"?accountId=PRIVATE", 409}} {
		res := h.do(t, "GET", "/v1/drafts/d1/save-check"+tc.query, nil)
		if res.status != tc.status {
			t.Fatalf("status %d: %s", res.status, res.body)
		}
		assertMIMECheckSafe(t, res.body)
	}
	if len(checker.checked) != 2 || len(*opened) != 2 || len(f.saved)+len(f.sent)+len(f.deleted)+len(f.replies)+len(f.replyParents) != 0 {
		t.Fatal("non-GET/invalid-account reached checker or probe mutated mailbox")
	}
}

func TestDraftSaveCheckSafeErrors(t *testing.T) {
	for _, tc := range []struct {
		err    error
		status int
	}{
		{errors.New("PRIVATE SECRET owner@example.test"), 502},
		{&googleapi.Error{Code: 404, Message: "PRIVATE", Body: "SECRET"}, 404},
	} {
		h, f, _ := mimeCheckServer(t)
		checker := &saveCheckFake{draftFake: f.draftFake, err: tc.err}
		h.openMailboxForAccount = func(string) (mailbox, error) { return checker, nil }
		res := h.do(t, "GET", "/v1/drafts/d1/save-check?accountId=personal", nil)
		if res.status != tc.status {
			t.Fatalf("status %d: %s", res.status, res.body)
		}
		assertMIMECheckSafe(t, res.body)
	}
	for _, unsupported := range []bool{false, true} {
		h, f, _ := mimeCheckServer(t)
		h.openMailboxForAccount = func(string) (mailbox, error) {
			if unsupported {
				return f.draftFake, nil
			}
			return nil, errors.New("PRIVATE SECRET")
		}
		res := h.do(t, "GET", "/v1/drafts/d1/save-check?accountId=personal", nil)
		if res.status != 502 {
			t.Fatalf("status %d: %s", res.status, res.body)
		}
		assertMIMECheckSafe(t, res.body)
	}
}
