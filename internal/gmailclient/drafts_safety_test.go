package gmailclient

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"google.golang.org/api/gmail/v1"
	"google.golang.org/api/option"
)

func TestDraftResumeRefusesLossyReplacement(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  string
	}{
		{"bcc", "To: ada@example.test\nBcc: hidden@example.test\nSubject: Keep\nContent-Type: text/plain; charset=UTF-8\n\nWords"},
		{"attachment", "To: ada@example.test\nSubject: Keep\nContent-Type: multipart/mixed; boundary=x\n\n--x\nContent-Type: text/plain; charset=UTF-8\n\nWords\n--x\nContent-Type: application/octet-stream\nContent-Disposition: attachment; filename=important.txt\n\nfile\n--x--\n"},
		{"html", "To: ada@example.test\nContent-Type: text/html; charset=UTF-8\n\n<b>Words</b>"},
		{"alternative", "To: ada@example.test\nContent-Type: multipart/alternative; boundary=x\n\n--x\nContent-Type: text/plain\n\nWords\n--x\nContent-Type: text/html\n\n<b>Words</b>\n--x--\n"},
		{"base64", "To: ada@example.test\nContent-Type: text/plain; charset=UTF-8\nContent-Transfer-Encoding: base64\n\nV29yZHM="},
		{"legacy charset", "To: ada@example.test\nContent-Type: text/plain; charset=iso-8859-1\n\nWords"},
		{"sender alias", "To: ada@example.test\nFrom: Alias <alias@example.test>\nContent-Type: text/plain; charset=UTF-8\n\nWords"},
		{"reply-to", "To: ada@example.test\nReply-To: other@example.test\nContent-Type: text/plain; charset=UTF-8\n\nWords"},
		{"reply threading", "To: ada@example.test\nIn-Reply-To: <parent@example.test>\nReferences: <parent@example.test>\nContent-Type: text/plain; charset=UTF-8\n\nWords"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := strings.ReplaceAll(tc.raw, "\n", "\r\n")
			writes := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.Method != http.MethodGet {
					writes++
					t.Errorf("unsupported draft mutated: %s %s", r.Method, r.URL.Path)
				}
				_ = json.NewEncoder(w).Encode(&gmail.Draft{Id: "d1", Message: &gmail.Message{Id: "m1", Raw: base64.RawURLEncoding.EncodeToString([]byte(raw))}})
			}))
			defer srv.Close()
			svc, err := gmail.NewService(context.Background(), option.WithEndpoint(srv.URL+"/"), option.WithoutAuthentication())
			if err != nil {
				t.Fatal(err)
			}
			c := Client{ctx: context.Background(), svc: svc}
			d, err := c.GetDraft("d1")
			if err != nil {
				t.Fatal(err)
			}
			// Opening an unchanged draft and making an intentional edit must both
			// refuse before PUT. A revision check alone does not protect MIME.
			for _, edit := range []bool{false, true} {
				if edit {
					d.Body = "New words"
				}
				_, err := c.SaveDraft(d, "")
				if !errors.Is(err, ErrUnsupportedDraft) || !strings.Contains(err.Error(), "edit it in Gmail") {
					t.Fatalf("SaveDraft = %v, want actionable unsupported error", err)
				}
			}
			if writes != 0 {
				t.Fatalf("writes = %d", writes)
			}
		})
	}
}

func TestDraftSupportedReplacementStillChecksRevisionAndWrites(t *testing.T) {
	raw, err := draftMIME(Draft{To: "ada@example.test", Subject: "Hello", Body: "Old"})
	if err != nil {
		t.Fatal(err)
	}
	writes := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(&gmail.Draft{Id: "d1", Message: &gmail.Message{Id: "m1", Raw: base64.RawURLEncoding.EncodeToString(raw)}})
		case http.MethodPut:
			writes++
			var d gmail.Draft
			if err := json.NewDecoder(r.Body).Decode(&d); err != nil {
				t.Error(err)
			}
			body, err := base64.RawURLEncoding.DecodeString(d.Message.Raw)
			if err != nil || !strings.Contains(string(body), "New words") {
				t.Errorf("updated MIME = %q, %v", body, err)
			}
			_ = json.NewEncoder(w).Encode(&gmail.Draft{Id: "d1", Message: &gmail.Message{Id: "m2"}})
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()
	svc, err := gmail.NewService(context.Background(), option.WithEndpoint(srv.URL+"/"), option.WithoutAuthentication())
	if err != nil {
		t.Fatal(err)
	}
	c := Client{ctx: context.Background(), svc: svc}
	d, err := c.GetDraft("d1")
	if err != nil {
		t.Fatal(err)
	}
	d.Body = "New words"
	d.MessageID = "stale"
	if _, err := c.SaveDraft(d, ""); !errors.Is(err, ErrDraftConflict) {
		t.Fatalf("stale revision = %v", err)
	}
	d.MessageID = "m1"
	got, err := c.SaveDraft(d, "")
	if err != nil || got.MessageID != "m2" || writes != 1 {
		t.Fatalf("save = %+v, %v; writes = %d", got, err, writes)
	}
}

func TestReplyDraftDefaultsUseReplyToWithoutSending(t *testing.T) {
	f, svc := replyFixture(t)
	f.headers[0].Value = "Sender <from@example.test>"
	f.headers = append(f.headers, &gmail.MessagePartHeader{Name: "Reply-To", Value: "Reply Target <reply-target@example.test>"})
	plan, err := replyClient(svc).Reply("m1", ReplyBody{Text: "Draft words"}, ReplyOptions{})
	if err != nil || len(plan.ToRecipients) != 1 || plan.ToRecipients[0].Address != "reply-target@example.test" {
		t.Fatalf("canonical draft audience = %+v, %v", plan, err)
	}
	if len(f.sent) != 0 {
		t.Fatalf("planning a draft sent mail: %v", f.sent)
	}
}
