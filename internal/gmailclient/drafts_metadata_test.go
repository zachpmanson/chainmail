package gmailclient

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"google.golang.org/api/gmail/v1"
	"google.golang.org/api/option"
)

// Synthetic Gmail-style headers, not captured mailbox data.
const draftMetadata = "Received: from synthetic.invalid (synthetic.invalid. [192.0.2.1])\r\n\tby synthetic.invalid with ESMTPS id synthetic;\r\n\tMon, 02 Nov 2026 10:00:00 +1100\r\n" +
	"Received: by 2001:db8::1 with SMTP id synthetic;\r\n        Mon, 02 Nov 2026 10:00:00 +1100\r\n" +
	"Date: Mon, 02 Nov 2026 10:00:00 +1100\r\n" +
	"Message-ID: <synthetic-draft@example.test>\r\n" +
	"From: =?UTF-8?Q?Synthetic_Owner?=\r\n\t<owner@example.test>\r\n"

type metadataFixture struct {
	raw                              []byte
	revision, thread                 string
	profile                          gmail.Profile
	sendAs                           []*gmail.SendAs
	failProfile, failSendAs, deleted bool
	profileStatus, sendAsStatus      int
	reads                            map[string]int
	writes                           int
	update                           *gmail.Draft
	parent                           *gmail.Message
	parentStatus                     int
	forbidWrites                     bool
}

func newMetadataFixture(t *testing.T, raw string) (*metadataFixture, Client) {
	t.Helper()
	f := &metadataFixture{
		raw: []byte(raw), revision: "api-revision-1", thread: "thread-1",
		profile: gmail.Profile{EmailAddress: "owner@example.test"},
		sendAs:  []*gmail.SendAs{{SendAsEmail: "owner@example.test", DisplayName: "Synthetic Owner", IsPrimary: true, IsDefault: true}},
		reads:   map[string]int{},
	}
	transport := fixtureTransport(func(r *http.Request) (*http.Response, error) {
		status := http.StatusOK
		var body any
		if r.Method == http.MethodGet {
			f.reads[r.URL.Path]++
			switch r.URL.Path {
			case "/gmail/v1/users/me/drafts":
				body = &gmail.ListDraftsResponse{Drafts: []*gmail.Draft{{Id: "d1"}}}
			case "/gmail/v1/users/me/drafts/d1":
				body = &gmail.Draft{Id: "d1", Message: &gmail.Message{Id: f.revision, ThreadId: f.thread, Raw: string(f.raw)}}
				if f.deleted {
					status = http.StatusNotFound
				}
			case "/gmail/v1/users/me/profile":
				body = &f.profile
				if f.failProfile {
					status = http.StatusForbidden
				}
				if f.profileStatus != 0 {
					status = f.profileStatus
				}
			case "/gmail/v1/users/me/settings/sendAs":
				body = &gmail.ListSendAsResponse{SendAs: f.sendAs}
				if f.failSendAs {
					status = http.StatusForbidden
				}
				if f.sendAsStatus != 0 {
					status = f.sendAsStatus
				}
			case "/gmail/v1/users/me/messages/parent":
				body = &gmail.Message{ThreadId: "thread-1", Payload: &gmail.MessagePart{Headers: []*gmail.MessagePartHeader{
					{Name: "Message-ID", Value: "<parent@example.test>"}, {Name: "References", Value: "<ancestor@example.test>"},
				}}}
				if f.parent != nil {
					body = f.parent
				}
				if f.parentStatus != 0 {
					status = f.parentStatus
				}
			default:
				return nil, fmt.Errorf("unexpected fixture GET path")
			}
		} else {
			if f.forbidWrites {
				t.Fatalf("read-only probe attempted %s", r.Method)
			}
			f.writes++
			if r.Method != http.MethodPut || r.URL.Path != "/gmail/v1/users/me/drafts/d1" {
				t.Errorf("unexpected mutation: %s %s", r.Method, r.URL.Path)
			}
			var update gmail.Draft
			if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
				t.Error(err)
			}
			f.update = &update
			body = &gmail.Draft{Id: "d1", Message: &gmail.Message{Id: "api-revision-2", ThreadId: "thread-1"}}
		}
		var encoded []byte
		if status != http.StatusOK {
			encoded = []byte(fmt.Sprintf(`{"error":{"code":%d,"message":"PRIVATE PROVIDER ERROR owner@example.test"}}`, status))
		} else {
			var err error
			encoded, err = json.Marshal(body)
			if err != nil {
				return nil, err
			}
		}
		return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(string(encoded))), Request: r}, nil
	})
	svc, err := gmail.NewService(context.Background(), option.WithHTTPClient(&http.Client{Transport: transport}), option.WithEndpoint("https://fixture.invalid/"))
	if err != nil {
		t.Fatal(err)
	}
	return f, Client{ctx: context.Background(), svc: svc}
}

func TestGmailStylePlainDraftPreservesMetadata(t *testing.T) {
	for _, encoding := range []*base64.Encoding{base64.RawURLEncoding, base64.URLEncoding} {
		t.Run(fmt.Sprintf("padding=%v", encoding == base64.URLEncoding), func(t *testing.T) {
			raw := draftMetadata + "To: old@example.test\r\nSubject: Saved\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\nSaved words"
			for len(raw)%3 != 1 {
				raw += "!"
			}
			f, c := newMetadataFixture(t, encoding.EncodeToString([]byte(raw)))
			drafts, err := c.ListDrafts()
			if err != nil || len(drafts) != 1 {
				t.Fatalf("list: %v, count=%d", err, len(drafts))
			}
			d := drafts[0]
			if d.ID != "d1" || d.MessageID != "api-revision-1" || d.ThreadID != "thread-1" || !strings.HasPrefix(d.Body, "Saved words") {
				t.Fatalf("draft fields: %+v", d)
			}
			d.To, d.Cc, d.Subject, d.Body = "new@example.test", "copy@example.test", "Edited café", "Edited body\nNext line"
			saved, err := c.SaveDraft(d, "")
			if err != nil {
				t.Fatal(err)
			}
			if saved.MessageID != "api-revision-2" || f.writes != 1 {
				t.Fatalf("save: %+v; writes=%d", saved, f.writes)
			}
			got, err := base64.RawURLEncoding.DecodeString(f.update.Message.Raw)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(string(got), draftMetadata) {
				t.Fatal("immutable raw header blocks changed")
			}
			parsed, err := parseDraft(got)
			if err != nil || parsed.To != "<new@example.test>" || parsed.Cc != "<copy@example.test>" || parsed.Subject != d.Subject || parsed.Body != d.Body {
				t.Fatalf("edited fields: %+v, %v", parsed, err)
			}
			if f.reads["/gmail/v1/users/me/profile"] != 1 || f.reads["/gmail/v1/users/me/settings/sendAs"] != 1 {
				t.Fatalf("identity reads: %v", f.reads)
			}
		})
	}
}

func TestInterleavedDraftMetadataPreservesRawOrder(t *testing.T) {
	const firstTrace = "Received: from first.synthetic.invalid\r\n\tby synthetic.invalid with HTTPREST; Mon, 02 Nov 2026 10:00:00 +1100\r\n"
	const secondTrace = "Received: from second.synthetic.invalid\r\n        by synthetic.invalid with SMTP; Mon, 02 Nov 2026 10:00:00 +1100\r\n"
	const date = "Date: Mon, 02 Nov 2026 10:00:00 +1100\r\n"
	const from = "From: =?UTF-8?Q?Synthetic_Owner?=\r\n\t<owner@example.test>\r\n"
	const messageID = "Message-ID: <interleaved@example.test>\r\n"
	const raw = firstTrace + "To: old@example.test\r\n" + date + "Subject: Saved\r\n" + secondTrace + "MIME-Version: 1.0\r\n" + from + "Content-Type: text/plain; charset=UTF-8\r\n" + messageID + "Content-Transfer-Encoding: quoted-printable\r\n\r\nOld words"
	const retained = firstTrace + date + secondTrace + from + messageID
	for _, encoding := range []*base64.Encoding{base64.RawURLEncoding, base64.URLEncoding} {
		f, c := newMetadataFixture(t, encoding.EncodeToString([]byte(raw)))
		draft, err := c.GetDraft("d1")
		if err != nil {
			t.Fatal(err)
		}
		draft.To, draft.Body = "new@example.test", "Changed words"
		if _, err := c.SaveDraft(draft, ""); err != nil {
			t.Fatal(err)
		}
		if f.writes != 1 || f.update == nil {
			t.Fatalf("mutations=%d", f.writes)
		}
		got, err := base64.RawURLEncoding.DecodeString(f.update.Message.Raw)
		if err != nil || !strings.HasPrefix(string(got), retained) {
			t.Fatal("interleaved immutable blocks or Received ordering changed")
		}
		parsed, err := parseDraft(got)
		if err != nil || parsed.To != "<new@example.test>" || parsed.Body != "Changed words" {
			t.Fatalf("edited fields: %+v, %v", parsed, err)
		}
	}
}

func TestDraftSenderSnapshotGate(t *testing.T) {
	const plain = "From: Synthetic Owner <owner@example.test>\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\nOld"
	for _, tc := range []struct {
		name, reason string
		change       func(*metadataFixture)
		from         string
		accepted     bool
	}{
		{name: "primary default name match", accepted: true},
		{name: "encoded folded match", from: "From: =?UTF-8?Q?Synthetic_Owner?=\r\n\t<owner@example.test>", accepted: true},
		{name: "case insensitive address match", from: "From: Synthetic Owner <OWNER@EXAMPLE.TEST>", accepted: true},
		{name: "empty name agreement", from: "From: owner@example.test", change: func(f *metadataFixture) { f.sendAs[0].DisplayName = "" }, accepted: true},
		{name: "alias From", from: "From: Synthetic Owner <alias@example.test>"},
		{name: "other account", change: func(f *metadataFixture) { f.profile.EmailAddress = "other@example.test" }},
		{name: "default alias", change: func(f *metadataFixture) {
			f.sendAs[0].IsPrimary = false
			f.sendAs[0].SendAsEmail = "alias@example.test"
		}},
		{name: "primary not default", change: func(f *metadataFixture) { f.sendAs[0].IsDefault = false }},
		{name: "default not primary", change: func(f *metadataFixture) { f.sendAs[0].IsPrimary = false }},
		{name: "historical presentation preserved", from: "From: Different Presentation <owner@example.test>", accepted: true},
		{name: "missing name preserved", from: "From: owner@example.test", accepted: true},
		{name: "empty configured name with named draft", change: func(f *metadataFixture) { f.sendAs[0].DisplayName = "" }, accepted: true},
		{name: "renamed sender settings", change: func(f *metadataFixture) { f.sendAs[0].DisplayName = "New Name" }, accepted: true},
		{name: "encoded Unicode historical presentation", from: "From: =?UTF-8?Q?Historical_=C3=96wner?=\r\n\t<owner@example.test>", accepted: true},
		{name: "extra nondefault alias", accepted: true, change: func(f *metadataFixture) {
			f.sendAs = append(f.sendAs, &gmail.SendAs{SendAsEmail: "alias@example.test", VerificationStatus: "accepted"})
		}},
		{name: "pending and unverified aliases before primary", accepted: true, from: "From: =?UTF-8?Q?Historical_=C3=96wner?=\r\n\t<OWNER@EXAMPLE.TEST>", change: func(f *metadataFixture) {
			f.sendAs = append([]*gmail.SendAs{
				{SendAsEmail: "pending@example.test", VerificationStatus: "pending"},
				{SendAsEmail: "alias@example.test"},
			}, f.sendAs...)
		}},
		{name: "case insensitive settings match with alias", accepted: true, change: func(f *metadataFixture) {
			f.sendAs[0].SendAsEmail = "OWNER@EXAMPLE.TEST"
			f.sendAs = append(f.sendAs, &gmail.SendAs{SendAsEmail: "alias@example.test"})
		}},
		{name: "duplicate address case insensitive", reason: "sender_identities_multiple", change: func(f *metadataFixture) {
			f.sendAs = append(f.sendAs, &gmail.SendAs{SendAsEmail: "OWNER@EXAMPLE.TEST"})
		}},
		{name: "duplicate aliases case insensitive", reason: "sender_identities_multiple", change: func(f *metadataFixture) {
			f.sendAs = append(f.sendAs, &gmail.SendAs{SendAsEmail: "alias@example.test"}, &gmail.SendAs{SendAsEmail: "ALIAS@EXAMPLE.TEST"})
		}},
		{name: "nil extra record", reason: "sender_settings_record_missing", change: func(f *metadataFixture) { f.sendAs = append(f.sendAs, nil) }},
		{name: "malformed extra address", reason: "sender_settings_address_invalid", change: func(f *metadataFixture) {
			f.sendAs = append(f.sendAs, &gmail.SendAs{SendAsEmail: "PRIVATE invalid"})
		}},
		{name: "named extra address", reason: "sender_settings_address_invalid", change: func(f *metadataFixture) {
			f.sendAs = append(f.sendAs, &gmail.SendAs{SendAsEmail: "Alias <alias@example.test>"})
		}},
		{name: "empty extra address", reason: "sender_settings_address_invalid", change: func(f *metadataFixture) { f.sendAs = append(f.sendAs, &gmail.SendAs{}) }},
		{name: "two primary records", reason: "sender_identities_multiple", change: func(f *metadataFixture) {
			f.sendAs = append(f.sendAs, &gmail.SendAs{SendAsEmail: "alias@example.test", IsPrimary: true})
		}},
		{name: "two default records", reason: "sender_identities_multiple", change: func(f *metadataFixture) {
			f.sendAs = append(f.sendAs, &gmail.SendAs{SendAsEmail: "alias@example.test", IsDefault: true})
		}},
		{name: "missing primary with alias", reason: "sender_not_primary", change: func(f *metadataFixture) {
			f.sendAs[0].IsPrimary = false
			f.sendAs = append(f.sendAs, &gmail.SendAs{SendAsEmail: "alias@example.test"})
		}},
		{name: "missing default with alias", reason: "sender_not_default", change: func(f *metadataFixture) {
			f.sendAs[0].IsDefault = false
			f.sendAs = append(f.sendAs, &gmail.SendAs{SendAsEmail: "alias@example.test"})
		}},
		{name: "primary default split", reason: "sender_not_default", change: func(f *metadataFixture) {
			f.sendAs[0].IsDefault = false
			f.sendAs = append(f.sendAs, &gmail.SendAs{SendAsEmail: "alias@example.test", IsDefault: true})
		}},
		{name: "configured alias From", from: "From: Alias <alias@example.test>", reason: "sender_address_mismatch", change: func(f *metadataFixture) {
			f.sendAs = append(f.sendAs, &gmail.SendAs{SendAsEmail: "alias@example.test"})
		}},
		{name: "profile mismatch with aliases", reason: "sender_profile_mismatch", change: func(f *metadataFixture) {
			f.profile.EmailAddress = "alias@example.test"
			f.sendAs = append(f.sendAs, &gmail.SendAs{SendAsEmail: "alias@example.test"})
		}},
		{name: "two primary default records", reason: "sender_identities_multiple", change: func(f *metadataFixture) { f.sendAs = append(f.sendAs, f.sendAs[0]) }},
		{name: "absent settings", change: func(f *metadataFixture) { f.sendAs = nil }},
		{name: "nil settings record", change: func(f *metadataFixture) { f.sendAs = []*gmail.SendAs{nil} }},
		{name: "invalid settings address", change: func(f *metadataFixture) { f.sendAs[0].SendAsEmail = "Synthetic Owner <owner@example.test>" }},
		{name: "invalid profile address", change: func(f *metadataFixture) { f.profile.EmailAddress = "owner@example.test,other@example.test" }},
		{name: "empty profile", change: func(f *metadataFixture) { f.profile.EmailAddress = "" }},
		{name: "profile failure", change: func(f *metadataFixture) { f.failProfile = true }},
		{name: "settings failure", change: func(f *metadataFixture) { f.failSendAs = true }},
		{name: "invalid From", from: "From: PRIVATE MALFORMED FROM"},
		{name: "two addresses", from: "From: Synthetic Owner <owner@example.test>, alias@example.test"},
		{name: "two From fields", from: "From: Synthetic Owner <owner@example.test>\r\nFrom: alias@example.test"},
		{name: "empty From", from: "From:"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := plain
			if tc.from != "" {
				raw = strings.Replace(raw, "From: Synthetic Owner <owner@example.test>", tc.from, 1)
			}
			f, c := newMetadataFixture(t, base64.RawURLEncoding.EncodeToString([]byte(raw)))
			if tc.change != nil {
				tc.change(f)
			}
			assertRefusal := func(err error) {
				t.Helper()
				assertMetadataRefusal(t, err, f)
				if tc.reason != "" && !strings.Contains(err.Error(), "reason="+tc.reason+";") {
					t.Fatalf("unexpected category: %v", err)
				}
			}
			err := c.CheckDraftSender("d1")
			if tc.accepted {
				if err != nil {
					t.Fatal(err)
				}
				if f.reads["/gmail/v1/users/me/profile"] != 1 || f.reads["/gmail/v1/users/me/settings/sendAs"] != 1 {
					t.Fatalf("missing identity reads: %v", f.reads)
				}
			} else {
				assertRefusal(err)
			}
			if f.writes != 0 || f.reads["/gmail/v1/users/me/drafts/d1"] != 1 {
				t.Fatalf("probe: writes=%d, reads=%v", f.writes, f.reads)
			}
			for path, count := range f.reads {
				if count != 1 || (path != "/gmail/v1/users/me/drafts/d1" && path != "/gmail/v1/users/me/profile" && path != "/gmail/v1/users/me/settings/sendAs") {
					t.Fatalf("unexpected probe read: %s count=%d", path, count)
				}
			}
			f.reads = map[string]int{}
			_, err = c.SaveDraft(Draft{ID: "d1", MessageID: "api-revision-1", To: "new@example.test", Body: "Edited"}, "")
			if tc.accepted {
				if err != nil || f.writes != 1 {
					t.Fatalf("save: %v, writes=%d", err, f.writes)
				}
				updated, err := decodeDraftRaw(f.update.Message.Raw)
				if err != nil {
					t.Fatal(err)
				}
				originalFrom, err := draftMetadataBlocks([]byte(raw))
				if err != nil {
					t.Fatal(err)
				}
				updatedFrom, err := draftMetadataBlocks(updated)
				if err != nil || !bytes.Equal(originalFrom, updatedFrom) {
					t.Fatal("saved From metadata differs from the original raw block")
				}
			} else {
				assertRefusal(err)
			}
		})
	}
}

func assertMetadataRefusal(t *testing.T, err error, f *metadataFixture) {
	t.Helper()
	if !errors.Is(err, ErrUnsupportedDraft) || !strings.Contains(err.Error(), "edit it in Gmail") || f.writes != 0 {
		t.Fatalf("refusal: %v; mutations=%d", err, f.writes)
	}
	for _, private := range []string{"owner@example.test", "alias@example.test", "PRIVATE", "Synthetic Owner", "Different Presentation", "private@example.test", "X-Private-Token", "private-token", "SECRET BODY"} {
		if strings.Contains(err.Error(), private) {
			t.Fatalf("private diagnostic: %v", err)
		}
	}
}

func TestMetadataStillRefusesProtectedContent(t *testing.T) {
	for _, tc := range []struct{ name, headers, mime string }{
		{name: "Bcc", headers: "Bcc: private@example.test\r\n"},
		{name: "empty Bcc", headers: "Bcc:\r\n"},
		{name: "Reply-To", headers: "Reply-To: private@example.test\r\n"},
		{name: "Sender", headers: "Sender: private@example.test\r\n"},
		{name: "custom unknown", headers: "X-Private-Token: private-token\r\n"},
		{name: "empty unknown", headers: "X-Private-Token:\r\n"},
		{name: "malicious header name", headers: "private@example.test: private-token\r\n"},
		{name: "malformed secret header", headers: "Authorization Bearer private-token: secret\r\n"},
		{name: "duplicate To", headers: "To: private@example.test\r\nTo: another@example.test\r\n"},
		{name: "duplicate MIME version", headers: "MIME-Version: 1.0\r\nMIME-Version: 1.0\r\n"},
		{name: "duplicate content type", headers: "Content-Type: text/plain\r\n"},
		{name: "HTML", mime: "text/html; charset=UTF-8"},
		{name: "multipart", mime: "multipart/mixed; boundary=x"},
		{name: "alternative", mime: "multipart/alternative; boundary=x"},
		{name: "legacy charset", mime: "text/plain; charset=iso-8859-1"},
		{name: "extra parameter", mime: "text/plain; charset=UTF-8; format=flowed"},
		{name: "base64 MIME", headers: "Content-Transfer-Encoding: base64\r\n"},
		{name: "attachment disposition", headers: "Content-Disposition: attachment; filename=private.txt\r\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			contentType := tc.mime
			if contentType == "" {
				contentType = "text/plain; charset=UTF-8"
			}
			raw := draftMetadata + tc.headers + "Content-Type: " + contentType + "\r\n\r\nSECRET BODY"
			f, c := newMetadataFixture(t, base64.URLEncoding.EncodeToString([]byte(raw)))
			_, err := c.SaveDraft(Draft{ID: "d1", MessageID: "api-revision-1", Body: "Edited"}, "")
			assertMetadataRefusal(t, err, f)
		})
	}
}

func TestImmutableDraftMetadataBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, headers string
		accepted      bool
	}{
		{"Date only", "Date: Mon, 02 Nov 2026 10:00:00 +1100\r\n", true},
		{"Message-ID only", "Message-ID: <synthetic@example.test>\r\n", true},
		{"Received only", "Received: synthetic trace\r\n\tcontinued\r\nReceived: another trace\r\n", true},
		{"folded lowercase", "date: Mon, 02 Nov 2026\r\n\t10:00:00 +1100\r\nmessage-id:\r\n <synthetic@example.test>\r\n", true},
		{"duplicate Date", "Date: Mon, 02 Nov 2026 10:00:00 +1100\r\nDate: Mon, 02 Nov 2026 10:00:00 +1100\r\n", false},
		{"duplicate ID case insensitive", "Message-ID: <synthetic@example.test>\r\nmessage-id: <other@example.test>\r\n", false},
		{"invalid Date", "Date: PRIVATE DATE\r\n", false},
		{"invalid ID", "Message-ID: PRIVATE ID\r\n", false},
		{"two IDs", "Message-ID: <a@example.test> <b@example.test>\r\n", false},
		{"unusual quoted ID", "Message-ID: <\"a\"@example.test>\r\n", false},
		{"empty ID", "Message-ID:\r\n", false},
		{"empty trace occurrence", "Received: synthetic trace\r\nReceived:\r\n", false},
		{"LF metadata", "Date: Mon, 02 Nov 2026 10:00:00 +1100\n", false},
		{"bare CR metadata", "Date: Mon, 02 Nov 2026\r 10:00:00 +1100\r\n", false},
		{"initial continuation", "\tPRIVATE CONTINUATION\r\n", false},
		{"invalid block", "PRIVATE MALFORMED HEADER\r\n", false},
		{"control byte", "Received: trace\x00PRIVATE\r\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := tc.headers + "Content-Type: text/plain; charset=UTF-8\r\n\r\nOld"
			f, c := newMetadataFixture(t, base64.RawURLEncoding.EncodeToString([]byte(raw)))
			_, err := c.SaveDraft(Draft{ID: "d1", MessageID: "api-revision-1", Body: "Edited"}, "")
			if tc.accepted {
				if err != nil || f.writes != 1 {
					t.Fatalf("save: %v; writes=%d", err, f.writes)
				}
				got, err := decodeDraftRaw(f.update.Message.Raw)
				if err != nil || !strings.HasPrefix(string(got), tc.headers) {
					t.Fatal("raw blocks changed")
				}
			} else {
				assertMetadataRefusal(t, err, f)
			}
			if f.reads["/gmail/v1/users/me/profile"] != 0 || f.reads["/gmail/v1/users/me/settings/sendAs"] != 0 {
				t.Fatalf("unnecessary identity reads: %v", f.reads)
			}
		})
	}
}

func TestMetadataRevisionDeletionAndExplicitParent(t *testing.T) {
	const refs = "In-Reply-To: <parent@example.test>\r\nReferences: <ancestor@example.test>\r\n\t<parent@example.test>\r\n"
	for _, tc := range []struct {
		name, parent string
		change       func(*metadataFixture)
		want         error
		accepted     bool
	}{
		{name: "same explicit parent folded refs", parent: "parent", accepted: true},
		{name: "wrong new composer no parent", want: ErrUnsupportedDraft},
		{name: "different explicit parent", parent: "parent", change: func(f *metadataFixture) {
			decoded, _ := decodeDraftRaw(string(f.raw))
			f.raw = []byte(base64.RawURLEncoding.EncodeToString([]byte(strings.Replace(string(decoded), "In-Reply-To: <parent@example.test>", "In-Reply-To: <other@example.test>", 1))))
		}, want: ErrUnsupportedDraft},
		{name: "thread mismatch", parent: "parent", change: func(f *metadataFixture) { f.thread = "other-thread" }, want: ErrUnsupportedDraft},
		{name: "stale revision", parent: "parent", change: func(f *metadataFixture) { f.revision = "external-revision" }, want: ErrDraftConflict},
		{name: "deleted", parent: "parent", change: func(f *metadataFixture) { f.deleted = true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := draftMetadata + refs + "Content-Type: text/plain; charset=UTF-8\r\n\r\nOld"
			f, c := newMetadataFixture(t, base64.RawURLEncoding.EncodeToString([]byte(raw)))
			if tc.change != nil {
				tc.change(f)
			}
			_, err := c.SaveDraft(Draft{ID: "d1", MessageID: "api-revision-1", Body: "Edited"}, tc.parent)
			if tc.accepted {
				if err != nil || f.writes != 1 || f.update.Message.ThreadId != "thread-1" {
					t.Fatalf("save: %v, writes=%d", err, f.writes)
				}
				got, _ := decodeDraftRaw(f.update.Message.Raw)
				if !strings.HasPrefix(string(got), draftMetadata) {
					t.Fatal("metadata changed")
				}
			} else {
				if err == nil || f.writes != 0 {
					t.Fatalf("save: %v, writes=%d", err, f.writes)
				}
				if tc.want != nil && !errors.Is(err, tc.want) {
					t.Fatalf("error=%v, want %v", err, tc.want)
				}
				if tc.name == "deleted" && !IsNotFound(err) {
					t.Fatalf("deleted error=%v", err)
				}
				if tc.name == "stale revision" || tc.name == "deleted" {
					if len(f.reads) != 1 {
						t.Fatalf("read after stale/deleted: %v", f.reads)
					}
				}
			}
		})
	}
}

func TestMetadataDoesNotExpandFirstCreate(t *testing.T) {
	writes := 0
	svc, err := gmail.NewService(context.Background(), option.WithHTTPClient(&http.Client{Transport: fixtureTransport(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/drafts") {
			t.Fatalf("unexpected first-create request: %s %s", r.Method, r.URL.Path)
		}
		writes++
		var d gmail.Draft
		if err := json.NewDecoder(r.Body).Decode(&d); err != nil {
			t.Fatal(err)
		}
		raw, err := decodeDraftRaw(d.Message.Raw)
		if err != nil {
			t.Fatal(err)
		}
		for _, header := range []string{"From:", "Date:", "Message-ID:", "Received:"} {
			if strings.Contains(string(raw), header) {
				t.Fatalf("first-create metadata: %s", header)
			}
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"id":"d1","message":{"id":"m1"}}`)), Request: r}, nil
	})}))
	if err != nil {
		t.Fatal(err)
	}
	c := Client{ctx: context.Background(), svc: svc}
	if _, err := c.SaveDraft(Draft{To: "new@example.test", Body: "First"}, ""); err != nil || writes != 1 {
		t.Fatalf("create: %v; writes=%d", err, writes)
	}
}
