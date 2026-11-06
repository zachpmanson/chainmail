package gmailclient

import (
	"bytes"
	"encoding/base64"
	"errors"
	"testing"
)

func TestAlternativeIncompleteHTMLTokensRefuseBeforeWrite(t *testing.T) {
	for _, suffix := range []string{`<img src="PRIVATE`, `<a href="PRIVATE`, `<span title="PRIVATE`, `</span PRIVATE`} {
		raw := bytes.Replace(alternativeFixture(), []byte("Hello &amp; world</div>"), []byte("Hello &amp; world"+suffix+"</div>"), 1)
		f, c := newMetadataFixture(t, base64.RawURLEncoding.EncodeToString(raw))
		if _, err := parseAlternative(raw); !errors.Is(err, ErrUnsupportedDraft) {
			t.Fatalf("incomplete token accepted: %v", err)
		}
		result, err := c.CheckDraftSave("d1")
		if err != nil || result.UnchangedCompatible || result.BodyEditCompatible || f.writes != 0 {
			t.Fatalf("incomplete token reported editable: %+v %v writes=%d", result, err, f.writes)
		}
		_, err = c.SaveDraft(Draft{ID: "d1", MessageID: "api-revision-1", Body: "Edited"}, "")
		if !errors.Is(err, ErrUnsupportedDraft) || f.writes != 0 {
			t.Fatalf("incomplete token saved: %v writes=%d", err, f.writes)
		}
	}
}
