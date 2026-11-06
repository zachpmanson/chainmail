package gmailclient

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"mime/quotedprintable"
	"strings"
	"testing"
)

func alternativeFixture() []byte {
	return []byte("Date: Tue, 01 Jan 2030 12:00:00 +0000\r\nMessage-ID: <draft@example.test>\r\nReceived: opaque PRIVATE\r\nTo: ada@example.test\r\nSubject: Saved\r\nMIME-Version: 1.0\r\nContent-Type: multipart/alternative; boundary=abc123\r\n\r\npreamble PRIVATE\r\n--abc123\r\nContent-Type: text/plain; charset=UTF-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\nHello & world\r\n\r\n--abc123\r\nContent-Type: text/html; charset=UTF-8\r\nContent-Transfer-Encoding: 7bit\r\n\r\n<div dir=\"ltr\">Hello &amp; world</div>\r\n--abc123--\r\nepilogue PRIVATE\r\n")
}

func TestAlternativePreservesAndSynchronizes(t *testing.T) {
	raw := alternativeFixture()
	d, err := parseDraft(raw)
	if err != nil {
		t.Fatal(err)
	}
	unchanged, _ := draftMIME(d)
	next, err := (Client{}).prepareDraftReplacement(raw, unchanged)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw, next) {
		t.Fatal("unchanged not byte-identical")
	}
	for _, body := range []string{"Edited & < > \" café 😀\n\nlast\n", " spaces  and\ttabs\u00a0literal\n", "CRLF\r\nnext\r\n", ""} {
		d.Body = body
		replacement, _ := draftReplacementMIME(d, raw)
		next, err = (Client{}).prepareDraftReplacement(raw, replacement)
		if err != nil {
			t.Fatalf("body %q: %v", body, err)
		}
		if err := checkDraftReplacement(raw, next); err != nil {
			t.Fatal(err)
		}
		parsed, err := parseDraft(next)
		if err != nil {
			t.Fatal(err)
		}
		want := strings.ReplaceAll(body, "\r\n", "\n")
		if parsed.Body != want {
			t.Fatalf("body = %q want %q", parsed.Body, want)
		}
		again, _ := draftMIME(parsed)
		repeat, err := (Client{}).prepareDraftReplacement(next, again)
		if err != nil || !bytes.Equal(next, repeat) {
			t.Fatalf("repeat save: %v", err)
		}
		for _, opaque := range []string{"preamble PRIVATE\r\n", "epilogue PRIVATE\r\n", "Received: opaque PRIVATE\r\n", "<div dir=\"ltr\">", "Content-Transfer-Encoding: 7bit\r\n"} {
			if !bytes.Contains(next, []byte(opaque)) {
				t.Fatalf("lost %q", opaque)
			}
		}
	}
}

func TestAlternativeRejectsBeforePreparation(t *testing.T) {
	raw := string(alternativeFixture())
	for _, tc := range []struct{ name, old, new string }{
		{"rich", "Hello &amp; world", "<b>Hello &amp; world</b>"},
		{"hidden", "dir=\"ltr\"", "dir=\"ltr\" hidden"},
		{"style", "dir=\"ltr\"", "dir=\"ltr\" style=\"color:red\""},
		{"signature", "dir=\"ltr\"", "dir=\"ltr\" class=\"gmail_signature\""},
		{"comment", "Hello &amp; world", "Hello &amp; world<!--PRIVATE-->"},
		{"inequivalent", "Hello &amp; world", "other PRIVATE"},
		{"malformed", "</div>", ""},
		{"extra", "--abc123--", "--abc123\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\nPRIVATE\r\n--abc123--"},
		{"attachment", "Content-Type: text/html;", "Content-Disposition: attachment\r\nContent-Type: text/html;"},
		{"charset", "charset=UTF-8", "charset=iso-8859-1"},
		{"encoding", "Encoding: 7bit", "Encoding: base64"},
		{"parameter", "text/html; charset=UTF-8", "text/html; charset=UTF-8; name=PRIVATE"},
		{"duplicate", "Content-Type: text/html;", "Content-Type: text/html\r\nContent-Type: text/html;"},
		{"truncated", "--abc123--\r\n", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			current := []byte(strings.Replace(raw, tc.old, tc.new, 1))
			for _, body := range []string{"Hello & world\n", "edited"} {
				replacement, _ := draftMIME(Draft{To: "ada@example.test", Subject: "Saved", Body: body})
				_, err := (Client{}).prepareDraftReplacement(current, replacement)
				if !errors.Is(err, ErrUnsupportedDraft) {
					t.Fatalf("accepted: %v", err)
				}
				if strings.Contains(err.Error(), "PRIVATE") {
					t.Fatal("privacy leak")
				}
				f, c := newMetadataFixture(t, base64.RawURLEncoding.EncodeToString(current))
				_, err = c.SaveDraft(Draft{ID: "d1", MessageID: "api-revision-1", To: "ada@example.test", Subject: "Saved", Body: body}, "")
				if !errors.Is(err, ErrUnsupportedDraft) || f.writes != 0 {
					t.Fatalf("save err=%v writes=%d", err, f.writes)
				}
				result, err := c.CheckDraftSave("d1")
				if err != nil || result.UnchangedCompatible || result.BodyEditCompatible || f.writes != 0 {
					t.Fatalf("check=%+v err=%v writes=%d", result, err, f.writes)
				}
			}
		})
	}
}

func TestAlternativeRealSaveAndReadOnlyFullCheck(t *testing.T) {
	raw := append([]byte("From: =?UTF-8?Q?Synthetic_Owner?=\r\n\t<owner@example.test>\r\n"), alternativeFixture()...)
	f, c := newMetadataFixture(t, base64.RawURLEncoding.EncodeToString(raw))
	d, err := c.GetDraft("d1")
	if err != nil {
		t.Fatal(err)
	}
	check, err := c.CheckDraftSave("d1")
	if err != nil || !check.UnchangedCompatible || !check.BodyEditCompatible || check.Context != "compose" || f.writes != 0 {
		t.Fatalf("check %+v %v writes=%d", check, err, f.writes)
	}
	if f.reads["/gmail/v1/users/me/profile"] != 2 || f.reads["/gmail/v1/users/me/settings/sendAs"] != 2 {
		t.Fatalf("full sender check not exercised: %v", f.reads)
	}
	d.To = "new@example.test"
	d.Cc = "copy@example.test"
	d.Subject = "Changed café"
	d.Body = "Edited & < > 😀\r\n\r\n  spaces\ttab\u00a0\r\n"
	saved, err := c.SaveDraft(d, "")
	if err != nil || f.writes != 1 || saved.MessageID != "api-revision-2" {
		t.Fatalf("save %+v %v writes=%d", saved, err, f.writes)
	}
	next, err := decodeDraftRaw(f.update.Message.Raw)
	if err != nil {
		t.Fatal(err)
	}
	a, err := parseAlternative(next)
	if err != nil {
		t.Fatal(err)
	}
	if a.text != strings.ReplaceAll(d.Body, "\r\n", "\n") {
		t.Fatalf("text %q", a.text)
	}
	old, _ := parseAlternative(raw)
	if !bytes.Equal(alternativeSkeleton(raw, old), alternativeSkeleton(next, a)) {
		t.Fatal("raw MIME/metadata changed")
	}
	if err := checkDraftReplacement(raw, next); err != nil {
		t.Fatal(err)
	}
	// The final guard is on actual output, not a pre-splice approximation.
	for _, tampered := range []string{
		strings.Replace(string(next), "Received: opaque PRIVATE", "Received: lost", 1),
		strings.Replace(string(next), "Content-Transfer-Encoding: 7bit", "Content-Transfer-Encoding: 8bit", 1),
		strings.Replace(string(next), "epilogue PRIVATE", "epilogue changed", 1),
		strings.Replace(string(next), "dir=\"ltr\"", "dir='ltr'", 1),
		strings.Replace(string(next), "Edited &amp;", "stale &amp;", 1),
	} {
		if err := checkDraftReplacement(raw, []byte(tampered)); !errors.Is(err, ErrUnsupportedDraft) {
			t.Fatalf("guard accepted mutation: %v", err)
		}
	}
}

func TestAlternativeGrammarAndNormalization(t *testing.T) {
	for _, tc := range []struct {
		html, plain string
		ok          bool
	}{
		{`<div dir="ltr">Hello &amp; world</div>`, "Hello & world", true},
		{`<div dir=ltr>Hello<div>second</div><div><br></div><p>last</p></div>`, "Hello\nsecond\n\nlast\n", true},
		{`<div dir='ltr'>a&nbsp;b</div>`, "a b\n", true},
		{`<div dir="ltr">a&nbsp;b</div>`, "a\u00a0b", true},
		{`<div dir="ltr">a&nbsp;b&nbsp;c</div>`, "a\u00a0b c", false},
		{`<div dir="ltr">Hello</div>`, "Hello\n\n", false},
		{`<div dir="ltr"> a </div>`, " a ", false},
		{`<div dir="ltr"> <div>a</div></div>`, "\na", false},
		{`<div dir="ltr"><div>a</div><div> </div><div>b</div></div>`, "a\n\nb", false},
		{`<div dir="ltr"><div> <br>a</div></div>`, "\na", false},
		{`<div dir="ltr"><div><div>a</div></div></div>`, "a", false},
		{`<div dir="ltr"><a href="PRIVATE">a</a></div>`, "a", false},
		{`<div dir="ltr"><img src="PRIVATE"></div>`, "", false},
		{`<div dir="ltr"><blockquote>a</blockquote></div>`, "a", false},
		{`<div dir="ltr"><script>PRIVATE</script>a</div>`, "a", false},
		{`<div dir="ltr"><div title="PRIVATE">a</div></div>`, "a", false},
		{`<div dir="ltr"><span style="white-space: pre-wrap; color:red">a</span></div>`, "a", false},
		{`<div dir="ltr"><p>a</div></p></div>`, "a", false},
		{`<div dir="ltr">a&unknown;</div>`, "a&unknown;", false},
	} {
		t.Run(tc.html+tc.plain, func(t *testing.T) {
			raw := alternativeFixture()
			var qp bytes.Buffer
			w := quotedprintable.NewWriter(&qp)
			_, _ = w.Write([]byte(strings.ReplaceAll(tc.plain, "\n", "\r\n")))
			_ = w.Close()
			raw = bytes.Replace(raw, []byte("Hello & world\r\n"), qp.Bytes(), 1)
			raw = bytes.Replace(raw, []byte(`<div dir="ltr">Hello &amp; world</div>`), []byte(tc.html), 1)
			_, err := parseAlternative(raw)
			if (err == nil) != tc.ok {
				t.Fatalf("ok=%t err=%v", tc.ok, err)
			}
		})
	}
}

func TestAlternativeLimitsCollisionsAndUnknownShapes(t *testing.T) {
	raw := alternativeFixture()
	for _, current := range [][]byte{
		append(raw, bytes.Repeat([]byte("x"), maxDraftMIMEBytes)...),
		bytes.Replace(raw, []byte("Hello &amp; world"), bytes.Repeat([]byte("x"), maxDraftHTMLBytes+1), 1),
		bytes.Replace(raw, []byte("Hello &amp; world"), []byte(strings.Repeat("<br>", maxDraftHTMLTokens+1)), 1),
		bytes.Replace(raw, []byte("charset=UTF-8"), []byte("charset=UTF-8; charset=UTF-8"), 1),
		bytes.Replace(raw, []byte("charset=UTF-8"), []byte("charset*=utf-8''UTF-8"), 1),
		bytes.Replace(raw, []byte("boundary=abc123"), []byte("boundary*=utf-8''abc123"), 1),
		bytes.Replace(raw, []byte("boundary=abc123"), []byte("boundary=abc123; boundary=abc123"), 1),
		bytes.Replace(raw, []byte("text/html; charset=UTF-8"), []byte("multipart/related; boundary=nested"), 1),
		bytes.Replace(raw, []byte("To: ada@example.test"), []byte("X-PRIVATE: secret\r\nTo: ada@example.test"), 1),
		bytes.Replace(raw, []byte("To: ada@example.test"), []byte("Bcc: hidden@example.test\r\nTo: ada@example.test"), 1),
		bytes.Replace(raw, []byte("Hello & world"), []byte("Hello =XX world"), 1),
		bytes.Replace(raw, []byte("--abc123--"), []byte("--abc123--suffix"), 1),
	} {
		f, c := newMetadataFixture(t, base64.RawURLEncoding.EncodeToString(current))
		_, err := c.SaveDraft(Draft{ID: "d1", MessageID: "api-revision-1", Body: "Edited"}, "")
		if !errors.Is(err, ErrUnsupportedDraft) || f.writes != 0 {
			t.Fatalf("err=%v writes=%d", err, f.writes)
		}
	}
	for _, text := range []string{"--abc123", "x\n--abc123", "x\n--abc123--", strings.Repeat("x", maxDraftHTMLBytes+1), "bad\x00"} {
		replacement, _ := draftReplacementMIME(Draft{Body: text}, raw)
		if _, err := (Client{}).prepareDraftReplacement(raw, replacement); !errors.Is(err, ErrUnsupportedDraft) {
			t.Fatalf("accepted collision/limit: %v", err)
		}
	}
}

func TestFullSaveCheckRefusalsAndPrivacy(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*metadataFixture)
	}{
		{"reply", func(f *metadataFixture) {
			raw := append([]byte("In-Reply-To: <PRIVATE@example.test>\r\n"), alternativeFixture()...)
			f.raw = []byte(base64.RawURLEncoding.EncodeToString(raw))
		}},
		{"sender", func(f *metadataFixture) {
			f.raw = []byte(base64.RawURLEncoding.EncodeToString(append([]byte("From: owner@example.test\r\n"), alternativeFixture()...)))
			f.failProfile = true
		}},
		{"missing revision", func(f *metadataFixture) { f.revision = "" }},
		{"bad base64", func(f *metadataFixture) { f.raw = []byte("PRIVATE!!!") }},
		{"deleted", func(f *metadataFixture) { f.deleted = true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, c := newMetadataFixture(t, base64.RawURLEncoding.EncodeToString(alternativeFixture()))
			tc.mutate(f)
			result, err := c.CheckDraftSave("d1")
			encoded, _ := json.Marshal(result)
			if result.UnchangedCompatible || result.BodyEditCompatible || f.writes != 0 || strings.Contains(string(encoded), "PRIVATE") {
				t.Fatalf("result=%s writes=%d", encoded, f.writes)
			}
			if err != nil && strings.Contains(err.Error(), "PRIVATE") {
				t.Fatal("leaked provider error")
			}
			if tc.name == "reply" {
				if result.Context != "reply_context" || len(f.reads) != 1 {
					t.Fatalf("reply context was checked: %+v %v", result, f.reads)
				}
			}
		})
	}
}

func TestAlternativeFoldedHeadersAndReversedParts(t *testing.T) {
	raw := alternativeFixture()
	raw = bytes.Replace(raw, []byte("Content-Type: multipart/alternative; boundary=abc123"), []byte("Content-Type: multipart/alternative;\r\n\tboundary=abc123"), 1)
	raw = bytes.ReplaceAll(raw, []byte("Content-Type: text/plain; charset=UTF-8"), []byte("content-type: text/plain;\r\n charset=UTF-8"))
	if _, err := parseAlternative(raw); err != nil {
		t.Fatal(err)
	}
	// Reverse the complete part blocks, preserving delimiter framing.
	first := bytes.Index(raw, []byte("--abc123\r\n"))
	secondRel := bytes.Index(raw[first+1:], []byte("--abc123\r\n"))
	second := first + 1 + secondRel
	last := bytes.Index(raw, []byte("--abc123--"))
	reversed := append([]byte{}, raw[:first]...)
	reversed = append(reversed, raw[second:last]...)
	reversed = append(reversed, raw[first:second]...)
	reversed = append(reversed, raw[last:]...)
	if _, err := parseAlternative(reversed); err != nil {
		t.Fatal(err)
	}
	for _, current := range [][]byte{raw, reversed} {
		d, _ := parseDraft(current)
		d.Body = "new  é\t\nline"
		replacement, err := draftReplacementMIME(d, current)
		if err != nil {
			t.Fatal(err)
		}
		next, err := (Client{}).prepareDraftReplacement(current, replacement)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(next, []byte("content-type: text/plain;\r\n charset=UTF-8")) {
			t.Fatal("part header folding lost")
		}
		parsed, _ := parseDraft(next)
		parsed.Body = "again\u00a0next\n"
		replacement, _ = draftReplacementMIME(parsed, next)
		edited, err := (Client{}).prepareDraftReplacement(next, replacement)
		if err != nil {
			t.Fatal(err)
		}
		got, err := parseAlternative(edited)
		if err != nil || got.text != parsed.Body {
			t.Fatalf("re-edit: %q %v", got.text, err)
		}
	}
}

func TestAlternativeBoundaryAndEncodingLimitsZeroWrites(t *testing.T) {
	raw := alternativeFixture()
	for _, text := range []string{"--abc123", "before\n--abc123--", strings.Repeat("é", 1000), strings.Repeat("x", maxDraftHTMLBytes+1), "bare\rreturn", "NUL\x00"} {
		f, c := newMetadataFixture(t, base64.RawURLEncoding.EncodeToString(raw))
		_, err := c.SaveDraft(Draft{ID: "d1", MessageID: "api-revision-1", Body: text}, "")
		if !errors.Is(err, ErrUnsupportedDraft) || f.writes != 0 {
			t.Fatalf("err=%v writes=%d", err, f.writes)
		}
	}
}
