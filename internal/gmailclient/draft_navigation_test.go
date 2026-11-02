package gmailclient

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"google.golang.org/api/gmail/v1"
	"google.golang.org/api/option"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestDraftReplyEvidence(t *testing.T) {
	for _, tc := range []struct{ name, headers, parent, status string }{
		{"absent", "", "", "compose"},
		{"folded", "In-Reply-To:\r\n\t<Parent@Example.test>\r\n", "<Parent@Example.test>", "candidate"},
		{"empty", "In-Reply-To: \r\n", "", "malformed-parent"},
		{"repeated", "In-Reply-To: <p@e.test>\r\nIn-Reply-To: <p@e.test>\r\n", "", "malformed-parent"},
		{"multiple", "In-Reply-To: <p@e.test> <q@e.test>\r\n", "", "malformed-parent"},
		{"garbage", "In-Reply-To: garbage <p@e.test>\r\n", "", "malformed-parent"},
		{"trailing", "In-Reply-To: <p@e.test> garbage\r\n", "", "malformed-parent"},
		{"quoted", "In-Reply-To: <\"p\"@e.test>\r\n", "", "malformed-parent"},
		{"comment", "In-Reply-To: (p) <p@e.test>\r\n", "", "malformed-parent"},
		{"references only", "References: <p@e.test>\r\n", "", "missing-explicit-parent"},
		{"empty references", "References:\r\n", "", "missing-explicit-parent"},
		{"empty references with explicit parent", "In-Reply-To: <p@e.test>\r\nReferences:\r\n", "", "malformed-parent"},
		{"explicit authoritative", "In-Reply-To: <p@e.test>\r\nReferences: <other@e.test> <q@e.test>\r\n", "<p@e.test>", "candidate"},
		{"invalid references", "In-Reply-To: <p@e.test>\r\nReferences: garbage\r\n", "", "malformed-parent"},
		{"repeated references", "In-Reply-To: <p@e.test>\r\nReferences: <p@e.test>\r\nReferences: <p@e.test>\r\n", "", "malformed-parent"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, err := parseDraft([]byte(tc.headers + "Content-Type: text/plain\r\n\r\nBody"))
			if err != nil {
				t.Fatal(err)
			}
			d.ThreadID = "gmail-container-not-a-root"
			parent, status := d.ReplyHeaders.Parent()
			if parent != tc.parent || status != tc.status {
				t.Fatalf("got %q %q", parent, status)
			}
			encoded, _ := json.Marshal(d)
			var fields map[string]any
			_ = json.Unmarshal(encoded, &fields)
			if _, ok := fields["ReplyHeaders"]; ok {
				t.Fatal("private evidence leaked")
			}
		})
	}
}

func TestSavedReplyEvidenceUsesFinalMIME(t *testing.T) {
	raw := "Content-Type: text/plain; charset=UTF-8\r\nSubject: Old\r\n\r\nOld body"
	f, c := newMetadataFixture(t, base64.RawURLEncoding.EncodeToString([]byte(raw)))
	d := Draft{ID: "d1", MessageID: "api-revision-1", Subject: "Edited", Body: "Edited", ReplyHeaders: ReplyHeaders{InReplyTo: []string{"<forged@e.test>"}}}
	saved, err := c.SaveDraft(d, "parent")
	if err != nil {
		t.Fatal(err)
	}
	parent, status := saved.ReplyHeaders.Parent()
	if parent != "<parent@example.test>" || status != "candidate" {
		t.Fatalf("saved evidence %q %q", parent, status)
	}
	finalRaw, err := decodeDraftRaw(f.update.Message.Raw)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := parseDraft(finalRaw)
	if err != nil {
		t.Fatal(err)
	}
	p, s := parsed.ReplyHeaders.Parent()
	if p != parent || s != status {
		t.Fatal("save/read evidence differs")
	}
	if f.writes != 1 || f.reads["/gmail/v1/users/me/drafts/d1"] != 1 || f.reads["/gmail/v1/users/me/messages/parent"] != 1 {
		t.Fatalf("unexpected calls: %v writes=%d", f.reads, f.writes)
	}
}

func TestCreatedDraftEvidenceIgnoresCallerAndNeedsNoRead(t *testing.T) {
	for _, reply := range []bool{false, true} {
		t.Run(fmt.Sprint(reply), func(t *testing.T) {
			reads, writes := 0, 0
			transport := fixtureTransport(func(r *http.Request) (*http.Response, error) {
				var response any
				if r.Method == http.MethodGet {
					reads++
					if !reply || writes != 0 || !strings.HasSuffix(r.URL.Path, "/messages/parent") {
						t.Fatalf("unexpected GET %s after %d writes", r.URL.Path, writes)
					}
					response = &gmail.Message{ThreadId: "gmail-thread", Payload: &gmail.MessagePart{Headers: []*gmail.MessagePartHeader{{Name: "Message-ID", Value: "<actual@e.test>"}}}}
				} else {
					writes++
					if r.Method != http.MethodPost {
						t.Fatalf("unexpected mutation %s", r.Method)
					}
					var request gmail.Draft
					if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
						t.Fatal(err)
					}
					raw, err := decodeDraftRaw(request.Message.Raw)
					if err != nil {
						t.Fatal(err)
					}
					parsed, err := parseDraft(raw)
					if err != nil {
						t.Fatal(err)
					}
					parent, status := parsed.ReplyHeaders.Parent()
					if reply {
						if parent != "<actual@e.test>" || status != "candidate" {
							t.Fatal(parent, status)
						}
					} else if status != "compose" {
						t.Fatal(status)
					}
					response = &gmail.Draft{Id: "created", Message: &gmail.Message{Id: "revision", ThreadId: "gmail-thread"}}
				}
				encoded, _ := json.Marshal(response)
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(bytes.NewReader(encoded)), Request: r}, nil
			})
			svc, err := gmail.NewService(context.Background(), option.WithHTTPClient(&http.Client{Transport: transport}), option.WithEndpoint("https://fixture.invalid/"))
			if err != nil {
				t.Fatal(err)
			}
			c := Client{ctx: context.Background(), svc: svc}
			parent := ""
			if reply {
				parent = "parent"
			}
			saved, err := c.SaveDraft(Draft{Subject: "Compose", ReplyHeaders: ReplyHeaders{InReplyTo: []string{"<forged@e.test>"}}}, parent)
			if err != nil {
				t.Fatal(err)
			}
			p, s := saved.ReplyHeaders.Parent()
			if reply {
				if p != "<actual@e.test>" || s != "candidate" {
					t.Fatal(p, s)
				}
			} else if s != "compose" {
				t.Fatal(s)
			}
			wantReads := 0
			if reply {
				wantReads = 1
			}
			if reads != wantReads || writes != 1 {
				t.Fatal(reads, writes)
			}
		})
	}
}
