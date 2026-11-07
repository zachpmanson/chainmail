package gmailclient

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"google.golang.org/api/gmail/v1"
)

const checkReplyHeaders = "In-Reply-To: <parent@example.test>\r\nReferences: <ancestor@example.test> <parent@example.test>\r\n"

func checkContextFixture(t *testing.T, raw []byte) (*metadataFixture, Client, Draft) {
	t.Helper()
	f, c := newMetadataFixture(t, base64.RawURLEncoding.EncodeToString(raw))
	f.forbidWrites = true
	d, err := c.GetDraft("d1")
	if err != nil {
		t.Fatal(err)
	}
	return f, c, d
}

func assertContextCheck(t *testing.T, out DraftSaveCheck, err error, status, context string) {
	t.Helper()
	if err != nil || out.Status != status || out.Context != context || out.UnchangedCompatible != (status == "supported") || out.BodyEditCompatible != (status == "supported") {
		t.Fatalf("check=%+v err=%v want=%s/%s", out, err, status, context)
	}
	encoded, _ := json.Marshal(out)
	for _, secret := range []string{"PRIVATE", "owner@example.test", "api-revision", "parent@example.test", "thread-1", "d1"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("response leaked %q: %s", secret, encoded)
		}
	}
}

func TestDraftSaveCheckContextPlainAndAlternative(t *testing.T) {
	for _, alternative := range []bool{false, true} {
		for _, reply := range []bool{false, true} {
			raw := []byte(draftMetadata + "To: ada@example.test\r\nSubject: Saved\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\nOld")
			if alternative {
				raw = append([]byte("From: owner@example.test\r\n"), alternativeFixture()...)
			}
			parent, context := "", "compose"
			if reply {
				raw = append([]byte(checkReplyHeaders), raw...)
				parent, context = "parent", "reply"
			}
			f, c, d := checkContextFixture(t, raw)
			out, err := c.CheckDraftSaveWithContext(d, parent)
			assertContextCheck(t, out, err, "supported", context)
			if f.writes != 0 || f.reads["/gmail/v1/users/me/profile"] != 2 || f.reads["/gmail/v1/users/me/settings/sendAs"] != 2 {
				t.Fatalf("reads=%v writes=%d", f.reads, f.writes)
			}
			if reply && f.reads["/gmail/v1/users/me/messages/parent"] != 2 {
				t.Fatal("real parent not freshly read for both constructions")
			}
			// Snapshot evidence never substitutes for SaveDraft's own fresh gate.
			f.revision = "external-revision"
			if _, err := c.SaveDraft(d, parent); !errors.Is(err, ErrDraftConflict) || f.writes != 0 {
				t.Fatalf("save after probe=%v writes=%d", err, f.writes)
			}
		}
	}
}

func TestDraftSaveCheckContextFailsClosed(t *testing.T) {
	base := checkReplyHeaders + draftMetadata + "To: ada@example.test\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\nOld"
	for _, tc := range []struct {
		name, status string
		change       func(*metadataFixture, *Draft)
	}{
		{"wrong thread", "unsupported", func(f *metadataFixture, d *Draft) { f.thread = "other" }},
		{"wrong parent", "unsupported", func(f *metadataFixture, d *Draft) {
			f.parent = &gmail.Message{ThreadId: "thread-1", Payload: &gmail.MessagePart{Headers: []*gmail.MessagePartHeader{{Name: "Message-ID", Value: "<other@example.test>"}}}}
		}},
		{"missing parent ID", "indeterminate", func(f *metadataFixture, d *Draft) {
			f.parent = &gmail.Message{ThreadId: "thread-1", Payload: &gmail.MessagePart{}}
		}},
		{"changed references", "unsupported", func(f *metadataFixture, d *Draft) {
			f.parent = &gmail.Message{ThreadId: "thread-1", Payload: &gmail.MessagePart{Headers: []*gmail.MessagePartHeader{{Name: "Message-ID", Value: "<parent@example.test>"}, {Name: "References", Value: "<different@example.test>"}}}}
		}},
		{"parent missing", "indeterminate", func(f *metadataFixture, d *Draft) { f.parentStatus = 404 }},
		{"parent unavailable", "indeterminate", func(f *metadataFixture, d *Draft) { f.parentStatus = 503 }},
		{"profile unavailable", "indeterminate", func(f *metadataFixture, d *Draft) { f.profileStatus = 503 }},
		{"settings unavailable", "indeterminate", func(f *metadataFixture, d *Draft) { f.sendAsStatus = 403 }},
		{"identity unknown", "indeterminate", func(f *metadataFixture, d *Draft) { f.sendAs = nil }},
		{"wrong identity", "unsupported", func(f *metadataFixture, d *Draft) { f.profile.EmailAddress = "other@example.test" }},
		{"checker revision race", "revision_changed", func(f *metadataFixture, d *Draft) { f.revision = "changed" }},
		{"no witness", "indeterminate", func(f *metadataFixture, d *Draft) { d.MessageID = "" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, c, d := checkContextFixture(t, []byte(base))
			tc.change(f, &d)
			out, err := c.CheckDraftSaveWithContext(d, "parent")
			assertContextCheck(t, out, err, tc.status, "unsupported")
			if f.writes != 0 {
				t.Fatal("probe mutated")
			}
		})
	}
	for _, headers := range []string{"In-Reply-To: \r\n", "In-Reply-To: malformed\r\n", checkReplyHeaders + "In-Reply-To: <parent@example.test>\r\n", checkReplyHeaders + "References: <parent@example.test>\r\n", "References: <parent@example.test>\r\n"} {
		f, c, d := checkContextFixture(t, []byte(headers+draftMetadata+"Content-Type: text/plain\r\n\r\nOld"))
		out, err := c.CheckDraftSaveWithContext(d, "parent")
		assertContextCheck(t, out, err, "unsupported", "unsupported")
		if f.writes != 0 {
			t.Fatal("probe mutated")
		}
	}
}

func TestDraftSaveCheckContextLegacyReplyAndMissingDraft(t *testing.T) {
	f, c, d := checkContextFixture(t, []byte(checkReplyHeaders+"Content-Type: text/plain\r\n\r\nOld"))
	out, err := c.CheckDraftSave("d1")
	if err != nil || out.Context != "reply_context" || out.Status != "" || out.UnchangedCompatible || out.BodyEditCompatible {
		t.Fatalf("legacy=%+v %v", out, err)
	}
	f.deleted = true
	_, err = c.CheckDraftSaveWithContext(d, "parent")
	if !IsNotFound(err) {
		t.Fatalf("missing draft=%v", err)
	}
}

func TestDraftSaveCheckStatusUnknownEvidence(t *testing.T) {
	if got := draftSaveCheckStatus(draftRefusal("future_evidence_unavailable")); got != "indeterminate" {
		t.Fatalf("unknown evidence classified %q", got)
	}
}

func TestDraftSaveCheckContextLiveParentGuardsAndActualUpdates(t *testing.T) {
	for _, alternative := range []bool{false, true} {
		for _, tc := range []struct {
			name, status string
			ids, refs    []string
		}{
			{"original parent", "supported", []string{"<parent@example.test>"}, nil},
			{"reply parent", "supported", []string{"<parent@example.test>"}, []string{"<ancestor@example.test>"}},
			{"missing ID", "indeterminate", nil, nil},
			{"empty ID", "indeterminate", []string{""}, nil},
			{"duplicate ID", "unsupported", []string{"<parent@example.test>", "<parent@example.test>"}, nil},
			{"malformed ID", "unsupported", []string{"PRIVATE malformed"}, nil},
			{"duplicate references", "unsupported", []string{"<parent@example.test>"}, []string{"<ancestor@example.test>", "<ancestor@example.test>"}},
			{"malformed references", "unsupported", []string{"<parent@example.test>"}, []string{"PRIVATE malformed"}},
			{"empty references", "unsupported", []string{"<parent@example.test>"}, []string{""}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				raw := []byte(draftMetadata + "To: ada@example.test\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\nOld")
				if alternative {
					raw = alternativeFixture()
				}
				refs := "References: <parent@example.test>\r\n"
				if tc.name == "reply parent" {
					refs = "References: <ancestor@example.test> <parent@example.test>\r\n"
				}
				raw = append([]byte("In-Reply-To: <parent@example.test>\r\n"+refs), raw...)
				f, c, d := checkContextFixture(t, raw)
				f.parent = &gmail.Message{ThreadId: "thread-1", Payload: &gmail.MessagePart{}}
				for _, id := range tc.ids {
					f.parent.Payload.Headers = append(f.parent.Payload.Headers, &gmail.MessagePartHeader{Name: "Message-ID", Value: id})
				}
				for _, ref := range tc.refs {
					f.parent.Payload.Headers = append(f.parent.Payload.Headers, &gmail.MessagePartHeader{Name: "References", Value: ref})
				}
				out, err := c.CheckDraftSaveWithContext(d, "parent")
				context := "unsupported"
				if tc.status == "supported" {
					context = "reply"
				}
				assertContextCheck(t, out, err, tc.status, context)
				if f.writes != 0 {
					t.Fatal("preflight mutated")
				}
				f.forbidWrites = tc.status != "supported"
				d.Body = "actual edit"
				_, err = c.SaveDraft(d, "parent")
				if tc.status == "supported" {
					if err != nil || f.writes != 1 {
						t.Fatalf("valid update: %v writes=%d", err, f.writes)
					}
				} else if !errors.Is(err, ErrUnsupportedDraft) || f.writes != 0 {
					t.Fatalf("unsafe update: %v writes=%d", err, f.writes)
				}
			})
		}
	}
}

func TestDraftSaveCheckContextDoesNotWidenMIMEOrIdentity(t *testing.T) {
	base := draftMetadata + "To: ada@example.test\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\nOld"
	for _, raw := range []string{
		"Bcc: hidden@example.test\r\n" + base,
		"X-PRIVATE: SECRET\r\n" + base,
		"Content-Disposition: attachment; filename=PRIVATE\r\n" + base,
		strings.Replace(base, "owner@example.test", "alias@example.test", 1),
		strings.Replace(base, "text/plain; charset=UTF-8", "text/html; charset=UTF-8", 1),
		strings.Replace(string(alternativeFixture()), "Hello &amp; world", "<b>Hello &amp; world</b>", 1),
	} {
		f, c, d := checkContextFixture(t, []byte(raw))
		out, err := c.CheckDraftSaveWithContext(d, "")
		assertContextCheck(t, out, err, "unsupported", "unsupported")
		if f.writes != 0 {
			t.Fatal("unsafe probe mutated")
		}
	}
}

func TestDraftSaveCheckRealReadOnlyReplyPlanning(t *testing.T) {
	for _, alternative := range []bool{false, true} {
		raw := []byte(draftMetadata + "To: ada@example.test\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\nOld")
		if alternative {
			raw = alternativeFixture()
		}
		f, c, d := checkContextFixture(t, append([]byte(checkReplyHeaders), raw...))
		f.parent = &gmail.Message{ThreadId: "thread-1", Payload: &gmail.MessagePart{Headers: []*gmail.MessagePartHeader{
			{Name: "Message-ID", Value: "<parent@example.test>"}, {Name: "References", Value: "<ancestor@example.test>"},
			{Name: "From", Value: "Ada <ada@example.test>"}, {Name: "To", Value: "owner@example.test"},
			{Name: "Cc", Value: "default-copy@example.test"}, {Name: "Subject", Value: "Real parent"},
		}}}
		plan, err := c.Reply("parent", ReplyBody{Text: d.Body}, ReplyOptions{Send: false, All: true, To: []string{"ada@example.test", "bo@example.test"}, Cc: []string{}})
		if err != nil || plan.Subject != "Re: Real parent" || plan.Cc != "" || len(plan.ToRecipients) != 2 {
			t.Fatalf("plan=%+v err=%v", plan, err)
		}
		d.To, d.Cc, d.Subject = plan.To, plan.Cc, plan.Subject
		out, err := c.CheckDraftSaveWithContext(d, "parent")
		assertContextCheck(t, out, err, "supported", "reply")
		if f.writes != 0 || f.reads["/gmail/v1/users/me/messages/parent"] != 3 {
			t.Fatalf("reads=%v writes=%d", f.reads, f.writes)
		}
	}
}
