package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zachpmanson/chainmail/internal/corpus"
	"github.com/zachpmanson/chainmail/internal/gmailclient"
	"google.golang.org/api/googleapi"
)

type mimeCheckFake struct {
	*draftFake
	checked []string
	err     error
}

func (f *mimeCheckFake) CheckDraftMIME(id string) (*gmailclient.DraftMIMEPart, error) {
	f.checked = append(f.checked, id)
	if f.err != nil {
		return nil, f.err
	}
	return &gmailclient.DraftMIMEPart{MIMEClass: "text/plain", Charset: "utf-8", TransferEncoding: "7bit", Disposition: "none", Parts: []gmailclient.DraftMIMEPart{}}, nil
}

func mimeCheckServer(t *testing.T) (*harness, *mimeCheckFake, *[]string) {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	h, f := draftsServer(t)
	checker := &mimeCheckFake{draftFake: f}
	for _, id := range []string{"work", "personal"} {
		if err := h.store.PutGmailAccount(corpus.GmailAccount{ID: id, DisplayName: id, Email: id + "@example.test"}); err != nil {
			t.Fatal(err)
		}
		path, err := gmailclient.AccountTokenPath(id)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		// Dummy existence marker only; no credentials are read by the fake opener.
		if err := os.WriteFile(path, []byte("synthetic"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	opened := []string{}
	h.openMailboxForAccount = func(id string) (mailbox, error) {
		opened = append(opened, id)
		return checker, nil
	}
	h.openMailbox = func() (mailbox, error) { t.Fatal("implicit mailbox selected"); return nil, nil }
	h.draftMailEnabled, h.sendMailEnabled = false, false
	return h, checker, &opened
}

func assertMIMECheckSafe(t *testing.T, body []byte) {
	t.Helper()
	for _, secret := range []string{"PRIVATE", "SECRET", "owner@example.test", "secret.invalid"} {
		if strings.Contains(string(body), secret) {
			t.Fatalf("leaked %q: %s", secret, body)
		}
	}
}

func TestDraftMIMECheckReadOnlySelectedAccount(t *testing.T) {
	h, f, opened := mimeCheckServer(t)
	for _, account := range []string{"personal", "work"} {
		res := h.do(t, "GET", "/v1/drafts/d1/mime-check?accountId="+account, nil)
		if res.status != 200 {
			t.Fatalf("status %d: %s", res.status, res.body)
		}
		loadAPI(t).assert(t, "DraftMIMEPart", res.body)
		var tree gmailclient.DraftMIMEPart
		if err := json.Unmarshal(res.body, &tree); err != nil {
			t.Fatal(err)
		}
		if tree.MIMEClass != "text/plain" || strings.Contains(string(res.body), "accountId") || strings.Contains(string(res.body), `"id"`) {
			t.Fatalf("response: %s", res.body)
		}
	}
	if strings.Join(*opened, ",") != "personal,work" || strings.Join(f.checked, ",") != "d1,d1" {
		t.Fatalf("opened %v, checked %v", *opened, f.checked)
	}
	for _, method := range []string{"POST", "PUT", "DELETE", "PATCH", "HEAD", "OPTIONS"} {
		if res := h.do(t, method, "/v1/drafts/d1/mime-check?accountId=personal", nil); res.status != 405 {
			t.Fatalf("%s: %d", method, res.status)
		}
	}
	if len(f.checked) != 2 || len(*opened) != 2 {
		t.Fatal("non-GET reached mailbox")
	}
	for _, path := range []string{"/v1/drafts?accountId=personal", "/v1/drafts/d1?accountId=personal"} {
		if res := h.do(t, "GET", path, nil); res.status != 200 {
			t.Fatalf("ordinary read: %d %s", res.status, res.body)
		}
	}
	if len(f.checked) != 2 {
		t.Fatal("ordinary list/read gained MIME probe")
	}
	if len(f.saved)+len(f.sent)+len(f.deleted)+len(f.replies)+len(f.replyParents) != 0 {
		t.Fatal("probe mutated mailbox or read reply parent")
	}
}

func TestDraftMIMECheckRequiresExplicitAccount(t *testing.T) {
	h, f, opened := mimeCheckServer(t)
	for _, tc := range []struct {
		query  string
		status int
	}{
		{"", 400}, {"?accountId=", 400}, {"?accountId=%20", 400}, {"?accountId=PRIVATE", 409},
	} {
		res := h.do(t, "GET", "/v1/drafts/d1/mime-check"+tc.query, nil)
		if res.status != tc.status {
			t.Fatalf("status %d: %s", res.status, res.body)
		}
		assertMIMECheckSafe(t, res.body)
	}
	if len(f.checked) != 0 || len(*opened) != 0 {
		t.Fatal("missing/unavailable account reached mailbox")
	}
}

func TestDraftMIMECheckSanitizesFailures(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		status int
		reason string
	}{
		{"provider", errors.New("PRIVATE SECRET owner@example.test"), 502, ""},
		{"deleted", &googleapi.Error{Code: 404, Message: "PRIVATE", Body: "SECRET"}, 404, ""},
		{"malformed", fmt.Errorf("PRIVATE: %w", gmailclient.ErrDraftMIMEMalformed), 409, "malformed_message"},
		{"limit", fmt.Errorf("PRIVATE: %w", gmailclient.ErrDraftMIMELimit), 409, "limit_exceeded"},
		{"encoding", fmt.Errorf("PRIVATE: %w", gmailclient.ErrDraftMIMEEncoding), 409, "unsupported_html_encoding"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, f, _ := mimeCheckServer(t)
			f.err = tc.err
			res := h.do(t, "GET", "/v1/drafts/d1/mime-check?accountId=personal", nil)
			if res.status != tc.status || !strings.Contains(string(res.body), tc.reason) {
				t.Fatalf("status %d: %s", res.status, res.body)
			}
			assertMIMECheckSafe(t, res.body)
			if len(f.saved)+len(f.sent)+len(f.deleted)+len(f.replies) != 0 {
				t.Fatal("failure mutated mailbox")
			}
		})
	}
	for _, unsupported := range []bool{false, true} {
		h, f, _ := mimeCheckServer(t)
		h.openMailboxForAccount = func(string) (mailbox, error) {
			if unsupported {
				return f.draftFake, nil
			}
			return nil, errors.New("PRIVATE SECRET owner@example.test")
		}
		res := h.do(t, "GET", "/v1/drafts/d1/mime-check?accountId=personal", nil)
		if res.status != 502 {
			t.Fatalf("status %d: %s", res.status, res.body)
		}
		assertMIMECheckSafe(t, res.body)
	}
}
