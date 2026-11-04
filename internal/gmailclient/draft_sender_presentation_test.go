package gmailclient

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestPreservedPresentationDoesNotBypassDraftGuards(t *testing.T) {
	for _, tc := range []struct{ name, headers string }{
		{"Bcc", "Bcc: private@example.test\r\nContent-Type: text/plain\r\n"},
		{"empty Bcc", "Bcc:\r\nContent-Type: text/plain\r\n"},
		{"Reply-To", "Reply-To: private@example.test\r\nContent-Type: text/plain\r\n"},
		{"Sender", "Sender: private@example.test\r\nContent-Type: text/plain\r\n"},
		{"unknown", "X-Private-Token: private-token\r\nContent-Type: text/plain\r\n"},
		{"HTML", "Content-Type: text/html; charset=UTF-8\r\n"},
		{"multipart attachment", "Content-Type: multipart/mixed; boundary=x\r\n"},
		{"legacy charset", "Content-Type: text/plain; charset=iso-8859-1\r\n"},
		{"extra MIME parameter", "Content-Type: text/plain; charset=UTF-8; format=flowed\r\n"},
		{"base64", "Content-Type: text/plain\r\nContent-Transfer-Encoding: base64\r\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := "From: Historical Owner <owner@example.test>\r\n" + tc.headers + "\r\nSECRET BODY"
			f, c := newMetadataFixture(t, base64.RawURLEncoding.EncodeToString([]byte(raw)))
			f.sendAs[0].DisplayName = ""
			_, err := c.SaveDraft(Draft{ID: "d1", MessageID: "api-revision-1", Body: "Edited"}, "")
			assertMetadataRefusal(t, err, f)
			if strings.Contains(err.Error(), "sender_") {
				t.Fatalf("presentation blocked before the protected-content guard: %v", err)
			}
		})
	}
}
