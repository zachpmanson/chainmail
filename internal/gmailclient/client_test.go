package gmailclient

import (
	"strings"
	"testing"

	"github.com/zachpmanson/docket/gmail/mail"
)

// The converter functions are the parity-critical glue between the two
// transports: the subprocess protocol carries these same fields as JSON, so
// every field mapped here must land on the same chainmail-side value a docket
// subprocess would have produced.

// TestConvertEnvelopeMapsEveryField walks the whole envelope shape once. A
// field dropped here disappears from the corpus without any type error — the
// mapped type would simply carry an empty string — so the test asserts all
// twelve, not a sample.
func TestConvertEnvelopeMapsEveryField(t *testing.T) {
	e := mail.Envelope{
		ID:         "m1",
		ThreadID:   "t9",
		From:       "Zora Miller <zora@example.com>",
		To:         "me@example.com",
		Cc:         "cc@example.com",
		Subject:    "the subject",
		Date:       "Tue, 1 Sep 2026 09:00:00 +1000",
		MessageID:  "<abc@example.com>",
		InReplyTo:  "<prev@example.com>",
		References: []string{"<prev@example.com>", "<older@example.com>"},
		Labels:     []string{"INBOX", "Label_5"},
		Snippet:    "a snippet",
	}
	got := convertEnvelope(e)
	checks := []struct {
		name string
		a, b string
	}{{
		name: "id", a: got.ID, b: e.ID,
	}, {
		name: "thread_id", a: got.ThreadID, b: e.ThreadID,
	}, {
		name: "from", a: got.From, b: e.From,
	}, {
		name: "to", a: got.To, b: e.To,
	}, {
		name: "cc", a: got.Cc, b: e.Cc,
	}, {
		name: "subject", a: got.Subject, b: e.Subject,
	}, {
		name: "date", a: got.Date, b: e.Date,
	}, {
		name: "message_id", a: got.MessageID, b: e.MessageID,
	}, {
		name: "in_reply_to", a: got.InReplyTo, b: e.InReplyTo,
	}, {
		name: "snippet", a: got.Snippet, b: e.Snippet,
	}}
	for _, ch := range checks {
		if ch.a != ch.b {
			t.Errorf("%s: got %q, want %q", ch.name, ch.a, ch.b)
		}
	}
	if len(got.References) != len(e.References) {
		t.Errorf("references: got %d, want 2", len(got.References))
	}
	for i := 0; i < min(len(got.References), len(e.References)); i++ {
		if got.References[i] != e.References[i] {
			t.Errorf("references[%d]: got %q, want %q", i, got.References[i], e.References[i])
		}
	}
	if len(got.Labels) != len(e.Labels) {
		t.Errorf("labels: got %d, want 2", len(got.Labels))
	}
	for i := 0; i < min(len(got.Labels), len(e.Labels)); i++ {
		if got.Labels[i] != e.Labels[i] {
			t.Errorf("labels[%d]: got %q, want %q", i, got.Labels[i], e.Labels[i])
		}
	}
	if strings.Join(got.Labels, ",") != "INBOX,Label_5" {
		t.Errorf("labels order changed: %q", strings.Join(got.Labels, ","))
	}
}

// TestPageOfWithMore mirrors docket's own pageOf contract: a page with a next
// token reports has_more with the token, and the token alone is the signal.
func TestPageOfWithMore(t *testing.T) {
	envs, page := pageOf(&mail.ListResult{
		Envelopes:     []mail.Envelope{{ID: "m1"}, {ID: "m2"}},
		NextPageToken: "tok-7",
	}, 500)
	if !page.HasMore || page.NextPageToken != "tok-7" {
		t.Errorf("have more: got has_more=%v token=%q, want true/tok-7",
			page.HasMore, page.NextPageToken)
	}
	if len(envs) != 2 {
		t.Errorf("returned: got %d, want 2", len(envs))
	}
	if page.Returned != 2 || page.Limit != 500 {
		t.Errorf("page: got returned=%d limit=%d, want 2/500", page.Returned, page.Limit)
	}
}

// TestPageOfLastPageHasNoToken: the last page carries has_more=false and an
// empty token — the same signal ingest relies on to stop the walk.
func TestPageOfLastPageHasNoToken(t *testing.T) {
	envs, page := pageOf(&mail.ListResult{
		Envelopes: []mail.Envelope{{ID: "m1"}},
	}, 25)
	if page.HasMore || page.NextPageToken != "" {
		t.Errorf("last page: got has_more=%v token=%q, want false/empty", page.HasMore, page.NextPageToken)
	}
	if len(envs) != 1 {
		t.Errorf("returned: got %d, want 1", len(envs))
	}
}

// TestConvertMessageMapsBodyAndAttachments checks the read path fields: body,
// the truncation flags, and the attachment rows chainmail records.
func TestConvertMessageMapsBodyAndAttachments(t *testing.T) {
	m := &mail.Message{
		Envelope: mail.Envelope{
			ID: "m3", ThreadID: "t9", From: "a@example.com",
			To: "b@example.com", Subject: "s", Date: "d", MessageID: "<m3@x>",
		},
		Body:          "the body",
		Truncated:     false,
		HTMLStatus:    "present",
		BodyHTML:      "<p>the body</p>",
		HTMLTruncated: false,
		Attachments: []mail.Attachment{{
			Filename: "a.txt", MimeType: "text/plain", Size: 12, PartID: "1.2",
		}},
	}
	got := convertMessage(m)
	if got.ID != "m3" {
		t.Errorf("id: got %q, want m3", got.ID)
	}
	if got.Body != "the body" || got.BodyHTML != "<p>the body</p>" {
		t.Errorf("body/html mapping changed")
	}
	if got.Truncated || got.HTMLTruncated {
		t.Errorf("truncation flags should pass through false")
	}
	if len(got.Attachments) != 1 {
		t.Fatalf("attachments: got %d, want 1", len(got.Attachments))
	}
	a := got.Attachments[0]
	if a.Filename != "a.txt" || a.MimeType != "text/plain" || a.Size != 12 || a.PartID != "1.2" {
		t.Errorf("attachment row: got %q/%q/%d/%q, want a.txt/text/plain/12/1.2",
			a.Filename, a.MimeType, a.Size, a.PartID)
	}
}
