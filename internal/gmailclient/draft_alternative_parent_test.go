package gmailclient

import (
	"encoding/base64"
	"errors"
	"testing"
)

func TestAlternativeEmptyParentHeadersStillRequireEvidence(t *testing.T) {
	for _, header := range []string{"In-Reply-To", "References"} {
		t.Run(header, func(t *testing.T) {
			raw := append([]byte(header+":\r\n"), alternativeFixture()...)
			for _, body := range []string{"Hello & world\n", "Edited"} {
				f, c := newMetadataFixture(t, base64.RawURLEncoding.EncodeToString(raw))
				_, err := c.SaveDraft(Draft{ID: "d1", MessageID: "api-revision-1", To: "ada@example.test", Subject: "Saved", Body: body}, "")
				if !errors.Is(err, ErrUnsupportedDraft) || f.writes != 0 {
					t.Fatalf("empty parent header bypassed guard: err=%v writes=%d", err, f.writes)
				}
			}
		})
	}
}
