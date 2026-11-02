package gmailclient

import (
	"errors"
	"strings"
	"testing"
)

func TestDraftReplacementDiagnosticsRemainSafeAndActionable(t *testing.T) {
	next, err := draftMIME(Draft{To: "new@example.test", Subject: "Changed", Body: "Changed body"})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, headers, reason, names string
	}{
		{"immutable headers", "From: private@example.test\r\nDate: Mon, 02 Nov 2026 10:00:00 +1100\r\nMessage-ID: <private-message@example.test>\r\nContent-Type: text/plain; charset=UTF-8\r\n", "header_missing_or_changed", "headers=Date,From,Message-Id"},
		{"duplicate editable header", "To: private@example.test\r\nTo: other-private@example.test\r\nContent-Type: text/plain\r\n", "duplicate_header", "headers=To"},
		{"html", "Content-Type: text/html; charset=UTF-8\r\n", "media_type", ""},
		{"charset", "Content-Type: text/plain; charset=iso-8859-1\r\n", "charset", ""},
		{"extra parameter", "Content-Type: text/plain; charset=UTF-8; format=flowed\r\n", "mime_parameter", ""},
		{"transfer encoding", "Content-Type: text/plain; charset=UTF-8\r\nContent-Transfer-Encoding: base64\r\n", "transfer_encoding", ""},
		{"malformed", "private malformed header\r\n", "malformed_message", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			current := []byte(tc.headers + "Subject: PRIVATE SUBJECT\r\n\r\nPRIVATE BODY")
			for i := 0; i < 10; i++ {
				err := checkDraftReplacement(current, next)
				if !errors.Is(err, ErrUnsupportedDraft) || !strings.Contains(err.Error(), "edit it in Gmail") || !strings.Contains(err.Error(), "reason="+tc.reason) {
					t.Fatalf("unexpected diagnostic: %v", err)
				}
				if tc.names != "" && !strings.Contains(err.Error(), tc.names) {
					t.Fatalf("missing stable sorted header names: %v", err)
				}
				for _, private := range []string{"private@example.test", "other-private@example.test", "private-message@example.test", "PRIVATE SUBJECT", "PRIVATE BODY", "private malformed header"} {
					if strings.Contains(err.Error(), private) {
						t.Fatalf("diagnostic exposed a private value: %v", err)
					}
				}
			}
		})
	}
}
