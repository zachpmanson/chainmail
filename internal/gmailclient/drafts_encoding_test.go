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

func TestDraftReadsAndUpdatesAcceptBase64URLPadding(t *testing.T) {
	for _, tc := range []struct {
		name     string
		encoding *base64.Encoding
		mod      int
	}{
		{"unpadded", base64.RawURLEncoding, 1},
		{"one padding character", base64.URLEncoding, 2},
		{"two padding characters", base64.URLEncoding, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := draftMIME(Draft{To: "ada@example.test", Subject: "Saved", Body: "Saved words"})
			if err != nil {
				t.Fatal(err)
			}
			for len(raw)%3 != tc.mod {
				raw = append(raw, '!')
			}
			encoded := tc.encoding.EncodeToString(raw)
			writes := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch {
				case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/drafts"):
					_ = json.NewEncoder(w).Encode(&gmail.ListDraftsResponse{Drafts: []*gmail.Draft{{Id: "d1"}}})
				case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/drafts/d1"):
					_ = json.NewEncoder(w).Encode(&gmail.Draft{Id: "d1", Message: &gmail.Message{Id: "m1", ThreadId: "thread-1", Raw: encoded}})
				case r.Method == http.MethodPut && strings.HasSuffix(r.URL.Path, "/drafts/d1"):
					writes++
					var update gmail.Draft
					if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
						t.Error(err)
					}
					decoded, err := base64.RawURLEncoding.DecodeString(update.Message.Raw)
					if err != nil || !strings.Contains(string(decoded), "Updated words") {
						t.Errorf("update MIME did not retain edited text: %v", err)
					}
					_ = json.NewEncoder(w).Encode(&gmail.Draft{Id: "d1", Message: &gmail.Message{Id: "m2", ThreadId: "thread-1"}})
				default:
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
					w.WriteHeader(http.StatusBadRequest)
				}
			}))
			defer srv.Close()
			svc, err := gmail.NewService(context.Background(), option.WithEndpoint(srv.URL+"/"), option.WithoutAuthentication())
			if err != nil {
				t.Fatal(err)
			}
			c := Client{ctx: context.Background(), svc: svc}
			drafts, err := c.ListDrafts()
			if err != nil || len(drafts) != 1 {
				t.Fatalf("ListDrafts = %d drafts, %v", len(drafts), err)
			}
			d := drafts[0]
			if !strings.HasPrefix(d.Body, "Saved words") || d.ThreadID != "thread-1" || d.MessageID != "m1" {
				t.Fatalf("draft metadata/content changed: %+v", d)
			}
			d.Body = "Updated words"
			saved, err := c.SaveDraft(d, "")
			if err != nil || saved.MessageID != "m2" || writes != 1 {
				t.Fatalf("SaveDraft = %+v, %v; writes=%d", saved, err, writes)
			}
		})
	}
}

func TestDraftRawRejectsMalformedEncoding(t *testing.T) {
	for _, encoded := range []string{"SGV$", "AA===", "_A=", "+/8="} {
		if _, err := decodeDraftRaw(encoded); err == nil {
			t.Errorf("accepted malformed base64url %q", encoded)
		}
	}
}

func TestPaddedDraftRetainsUnsupportedMIMEProtection(t *testing.T) {
	raw := []byte("To: ada@example.test\r\nBcc: hidden@example.test\r\nSubject: Keep\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\nWords")
	for len(raw)%3 == 0 {
		raw = append(raw, '!')
	}
	writes := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writes++
			t.Errorf("protected draft was mutated: %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(&gmail.Draft{Id: "d1", Message: &gmail.Message{Id: "m1", Raw: base64.URLEncoding.EncodeToString(raw)}})
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
	d.Body = "Changed"
	if _, err := c.SaveDraft(d, ""); !errors.Is(err, ErrUnsupportedDraft) || writes != 0 {
		t.Fatalf("replacement error=%v, writes=%d", err, writes)
	}
}
