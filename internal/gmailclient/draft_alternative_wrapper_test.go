package gmailclient

import (
	"bytes"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

func TestAlternativeRetainsAutoDirectionAndOuterEnvelope(t *testing.T) {
	for _, root := range []string{`<div dir="auto">`, `<div dir='auto'>`, `<div dir=auto>`, `<div dir="ltr">`} {
		for _, envelope := range []bool{false, true} {
			open, close := root, "</div>"
			if envelope {
				open, close = "<html><body>"+open, close+"</body></html>"
			}
			raw := bytes.Replace(alternativeFixture(), []byte(`<div dir="ltr">Hello &amp; world</div>`), []byte(open+"Hello &amp; world"+close), 1)
			f, c := newMetadataFixture(t, base64.RawURLEncoding.EncodeToString(raw))
			check, err := c.CheckDraftSave("d1")
			if err != nil || !check.UnchangedCompatible || !check.BodyEditCompatible || f.writes != 0 {
				t.Fatalf("check failed for envelope=%t root=%s: %+v %v", envelope, root, check, err)
			}
			d, err := c.GetDraft("d1")
			if err != nil {
				t.Fatal(err)
			}
			next, err := draftReplacementMIME(d, raw)
			if err != nil {
				t.Fatal(err)
			}
			unchanged, err := c.prepareDraftReplacement(raw, next)
			if err != nil || !bytes.Equal(unchanged, raw) {
				t.Fatalf("unchanged altered: %v", err)
			}
			d.Body = "Edited café & < >\n\n  space\ttab\u00a0"
			_, err = c.SaveDraft(d, "")
			if err != nil || f.writes != 1 {
				t.Fatalf("save failed: %v writes=%d", err, f.writes)
			}
			updated, err := decodeDraftRaw(f.update.Message.Raw)
			if err != nil {
				t.Fatal(err)
			}
			a, err := parseAlternative(updated)
			if err != nil || a.rootOpen != open || a.rootClose != close || a.text != d.Body {
				t.Fatalf("wrapper/text lost: %v", err)
			}
			candidate, err := draftReplacementMIME(d, updated)
			if err != nil {
				t.Fatal(err)
			}
			repeat, err := c.prepareDraftReplacement(updated, candidate)
			if err != nil || !bytes.Equal(updated, repeat) {
				t.Fatalf("repeat changed: %v", err)
			}
			if strings.Contains(root, "auto") {
				tampered := bytes.Replace(updated, []byte("dir="+strings.TrimPrefix(root, "<div dir=")), []byte(`dir="ltr">`), 1)
				if !errors.Is(checkDraftReplacement(raw, tampered), ErrUnsupportedDraft) {
					t.Fatal("direction mutation accepted")
				}
			}
		}
	}
}

func TestAlternativeEnvelopeDoesNotBroadenHTMLGrammar(t *testing.T) {
	for _, html := range []string{
		`<html lang="en"><body><div dir="auto">Hello &amp; world</div></body></html>`,
		`<html><body hidden><div dir="auto">Hello &amp; world</div></body></html>`,
		`<html><body><div dir="auto" class="PRIVATE">Hello &amp; world</div></body></html>`,
		`<html><body><div dir="rtl">Hello &amp; world</div></body></html>`,
		`<html><body><div dir="auto">Hello &amp; world</div></html>`,
		`<html><body><div dir="auto">Hello &amp; world<img src="PRIVATE</div></body></html>`,
		`<html><body><div dir="auto">Hello &amp; world<!--PRIVATE--></div></body></html>`,
	} {
		raw := bytes.Replace(alternativeFixture(), []byte(`<div dir="ltr">Hello &amp; world</div>`), []byte(html), 1)
		f, c := newMetadataFixture(t, base64.RawURLEncoding.EncodeToString(raw))
		result, err := c.CheckDraftSave("d1")
		if err != nil || result.UnchangedCompatible || result.BodyEditCompatible || f.writes != 0 {
			t.Fatalf("unsafe envelope compatible: %+v %v", result, err)
		}
		_, err = c.SaveDraft(Draft{ID: "d1", MessageID: "api-revision-1", Body: "Edited"}, "")
		if !errors.Is(err, ErrUnsupportedDraft) || f.writes != 0 {
			t.Fatalf("unsafe envelope saved: %v writes=%d", err, f.writes)
		}
	}
}
