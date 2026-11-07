package main

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/zachpmanson/chainmail/internal/corpus"

	"github.com/zachpmanson/chainmail/internal/gmailclient"
	"google.golang.org/api/googleapi"
)

type saveCheckFake struct {
	*draftFake
	checked []string
	err     error
	planned []gmailclient.Draft
	parents []string
	result  *gmailclient.DraftSaveCheck
	getErr  error
}

func (f *saveCheckFake) CheckDraftSave(id string) (gmailclient.DraftSaveCheck, error) {
	f.checked = append(f.checked, id)
	return gmailclient.DraftSaveCheck{UnchangedCompatible: true, BodyEditCompatible: true, Context: "compose"}, f.err
}

func (f *saveCheckFake) GetDraft(id string) (gmailclient.Draft, error) {
	if f.getErr != nil {
		return gmailclient.Draft{}, f.getErr
	}
	return f.draftFake.GetDraft(id)
}

func (f *saveCheckFake) CheckDraftSaveWithContext(d gmailclient.Draft, parent string) (gmailclient.DraftSaveCheck, error) {
	f.planned = append(f.planned, d)
	f.parents = append(f.parents, parent)
	if f.result != nil {
		return *f.result, f.err
	}
	context := "compose"
	if parent != "" {
		context = "reply"
	}
	return gmailclient.DraftSaveCheck{UnchangedCompatible: true, BodyEditCompatible: true, Context: context, Status: "supported"}, f.err
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
		if res.header.Get("Cache-Control") != "no-store" {
			t.Fatalf("preflight response may be cached: %q", res.header.Get("Cache-Control"))
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

func TestDraftSaveCheckWitnessComposeAndRaces(t *testing.T) {
	h, f, _ := mimeCheckServer(t)
	checker := &saveCheckFake{draftFake: f.draftFake}
	h.openMailboxForAccount = func(string) (mailbox, error) { return checker, nil }
	d := checker.drafts["d1"]
	d.MessageID = "revision-A"
	checker.drafts["d1"] = d
	for _, tc := range []struct {
		query, status     string
		httpStatus, calls int
	}{
		{"expectedMessageId=revision-A", "supported", 200, 1},
		{"expectedMessageId=revision-B", "revision_changed", 200, 1},
		{"expectedMessageId=", "", 400, 1},
		{"expectedMessageId=%20", "", 400, 1},
		{"expectedMessageId=revision-A&expectedMessageId=revision-A", "", 400, 1},
	} {
		res := h.do(t, "GET", "/v1/drafts/d1/save-check?accountId=personal&"+tc.query, nil)
		if res.status != tc.httpStatus || len(checker.planned) != tc.calls {
			t.Fatalf("%s: %d %s calls=%d", tc.query, res.status, res.body, len(checker.planned))
		}
		assertMIMECheckSafe(t, res.body)
		if res.status == 200 {
			loadAPI(t).assert(t, "DraftSaveCheck", res.body)
			out := decode[gmailclient.DraftSaveCheck](t, res)
			if out.Status != tc.status || (out.Status != "supported" && (out.BodyEditCompatible || out.UnchangedCompatible)) {
				t.Fatalf("%+v", out)
			}
		}
	}
	checker.result = &gmailclient.DraftSaveCheck{Context: "unsupported", Status: "revision_changed"}
	res := h.do(t, "GET", "/v1/drafts/d1/save-check?accountId=personal&expectedMessageId=revision-A", nil)
	if res.status != 200 || decode[gmailclient.DraftSaveCheck](t, res).Status != "revision_changed" {
		t.Fatalf("checker race: %d %s", res.status, res.body)
	}
	if len(checker.saved)+len(checker.sent)+len(checker.deleted) != 0 || len(checker.replies) != 0 {
		t.Fatal("compose preflight mutated or planned reply")
	}
}

func TestDraftSaveCheckWitnessReplyPlanningAndResolution(t *testing.T) {
	for _, tc := range []struct {
		name, account, headers, status string
		duplicate                      bool
	}{
		{"resolved", "personal", "<parent@e.test>", "supported", false},
		{"wrong account no copy", "work", "<parent@e.test>", "indeterminate", false},
		{"missing", "personal", "<missing@e.test>", "indeterminate", false},
		{"ambiguous", "personal", "<parent@e.test>", "indeterminate", true},
		{"malformed", "personal", "PRIVATE", "unsupported", false},
		{"empty", "personal", "", "unsupported", false},
		{"parent unavailable", "personal", "<parent@e.test>", "indeterminate", false},
		{"duplicate header", "personal", "<parent@e.test>", "unsupported", false},
		{"empty references", "personal", "<parent@e.test>", "unsupported", false},
		{"references only", "personal", "<parent@e.test>", "unsupported", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, f, _ := mimeCheckServer(t)
			_, err := h.store.PutForAccount("personal", corpus.Entry{Source: corpus.SourceMail, ExtID: "mail:parent", Kind: "message", TS: time.Unix(1, 0), BodyText: "synthetic"}, &corpus.Mail{GmailID: "parent-copy", MessageID: "<parent@e.test>"}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if tc.duplicate {
				_, err = h.store.PutForAccount("personal", corpus.Entry{Source: corpus.SourceMail, ExtID: "mail:duplicate", Kind: "message", TS: time.Unix(2, 0), BodyText: "different"}, &corpus.Mail{GmailID: "duplicate-copy", MessageID: "<parent@e.test>"}, nil)
				if err != nil {
					t.Fatal(err)
				}
			}
			checker := &saveCheckFake{draftFake: f.draftFake}
			h.openMailboxForAccount = func(string) (mailbox, error) { return checker, nil }
			checker.subject = map[string]string{"parent-copy": "Re: Actual mailbox subject"}
			d := checker.drafts["d1"]
			d.MessageID, d.To, d.Cc = "revision-A", "Ada <ada@example.test>, bo@example.test", ""
			d.ReplyHeaders = gmailclient.ReplyHeaders{InReplyTo: []string{tc.headers}}
			switch tc.name {
			case "parent unavailable":
				checker.fail = map[string]error{"parent-copy": errors.New("PRIVATE SECRET")}
			case "duplicate header":
				d.ReplyHeaders.InReplyTo = append(d.ReplyHeaders.InReplyTo, tc.headers)
			case "empty references":
				d.ReplyHeaders.References = []string{""}
			case "references only":
				d.ReplyHeaders.InReplyTo = nil
				d.ReplyHeaders.References = []string{tc.headers}
			}
			checker.drafts["d1"] = d
			res := h.do(t, "GET", "/v1/drafts/d1/save-check?accountId="+tc.account+"&expectedMessageId=revision-A", nil)
			if res.status != 200 {
				t.Fatalf("%d %s", res.status, res.body)
			}
			loadAPI(t).assert(t, "DraftSaveCheck", res.body)
			out := decode[gmailclient.DraftSaveCheck](t, res)
			if out.Status != tc.status {
				t.Fatalf("%+v", out)
			}
			assertMIMECheckSafe(t, res.body)
			if tc.status == "supported" {
				if out.Context != "reply" || len(checker.planned) != 1 || checker.parents[0] != "parent-copy" || checker.planned[0].Subject != "Re: Actual mailbox subject" || checker.planned[0].Cc != "" {
					t.Fatalf("planning=%+v parents=%v", checker.planned, checker.parents)
				}
				if len(checker.replies) != 1 || checker.replies[0].send || checker.replies[0].cc == nil || len(checker.replies[0].cc) != 0 || len(checker.replies[0].to) != 2 {
					t.Fatalf("reply=%+v", checker.replies)
				}
			} else if out.UnchangedCompatible || out.BodyEditCompatible || len(checker.planned) != 0 {
				t.Fatal("unresolved reply reached checker")
			}
			if len(checker.saved)+len(checker.sent)+len(checker.deleted) != 0 {
				t.Fatal("preflight mutated")
			}
		})
	}
}

func TestDraftSaveCheckWitnessSafeReadErrors(t *testing.T) {
	for _, tc := range []struct {
		err    error
		status int
	}{{errors.New("PRIVATE SECRET"), 502}, {&googleapi.Error{Code: 404, Message: "PRIVATE SECRET"}, 404}} {
		h, f, _ := mimeCheckServer(t)
		checker := &saveCheckFake{draftFake: f.draftFake, getErr: tc.err}
		h.openMailboxForAccount = func(string) (mailbox, error) { return checker, nil }
		res := h.do(t, "GET", "/v1/drafts/d1/save-check?accountId=personal&expectedMessageId=revision-A", nil)
		if res.status != tc.status {
			t.Fatalf("%d %s", res.status, res.body)
		}
		assertMIMECheckSafe(t, res.body)
	}
}

func TestDraftSaveCheckEmptyWitnessRejectedBeforeMailbox(t *testing.T) {
	h, _, _ := mimeCheckServer(t)
	h.openMailboxForAccount = func(string) (mailbox, error) {
		t.Fatal("invalid witness opened mailbox")
		return nil, errors.New("PRIVATE")
	}
	res := h.do(t, "GET", "/v1/drafts/d1/save-check?accountId=personal&expectedMessageId=", nil)
	if res.status != 400 {
		t.Fatalf("%d %s", res.status, res.body)
	}
}
