package gmailclient

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// Entirely invented content, including the literal QP-looking text.
func defaultEncodingAlternative(plain, html string, reverse, folded bool) []byte {
	raw := flowedAlternativeFixture("charset=UTF-8", reverse)
	plainBody := "Hello =20 & world\r\n"
	if plain == "quoted-printable" {
		plainBody = "Hello =3D20 & world\r\n"
	}
	raw = bytes.Replace(raw, []byte("Hello & world\r\n"), []byte(plainBody), 1)
	raw = bytes.Replace(raw, []byte("Hello &amp; world"), []byte("Hello =20 &amp; world"), 1)
	for _, part := range []struct{ media, old, encoding string }{{"plain", "quoted-printable", plain}, {"html", "7bit", html}} {
		contentType := "Content-Type: text/" + part.media + "; charset=UTF-8\r\n"
		old := contentType + "Content-Transfer-Encoding: " + part.old + "\r\n"
		next := ""
		if part.encoding != "" {
			next = "Content-Transfer-Encoding: " + part.encoding + "\r\n"
			if folded {
				next = "cOnTeNt-TrAnSfEr-EnCoDiNg:\r\n\t" + strings.ToUpper(part.encoding) + " \t\r\n"
			}
		}
		raw = bytes.Replace(raw, []byte(old), []byte(contentType+next), 1)
	}
	if folded {
		raw = bytes.ReplaceAll(raw, []byte("; charset=UTF-8"), []byte(";\r\n\tcharset=UTF-8"))
	}
	return append([]byte("From: Synthetic Owner <owner@example.test>\r\n"), raw...)
}

func TestAlternativeDefaultEncodingPreservesAndEdits(t *testing.T) {
	for _, plain := range []string{"", "7bit", "quoted-printable"} {
		for _, html := range []string{"", "7bit"} {
			for _, reverse := range []bool{false, true} {
				for _, folded := range []bool{false, true} {
					t.Run(fmt.Sprintf("plain=%s/html=%s/reverse=%v/folded=%v", plain, html, reverse, folded), func(t *testing.T) {
						raw := defaultEncodingAlternative(plain, html, reverse, folded)
						a, err := parseAlternative(raw)
						if err != nil || a.text != "Hello =20 & world\n" {
							t.Fatalf("literal 7bit projection: %q %v", a.text, err)
						}
						d, err := parseDraft(raw)
						if err != nil || d.Body != a.text {
							t.Fatalf("reader: %q %v", d.Body, err)
						}
						originalFixture, client := newMetadataFixture(t, base64.RawURLEncoding.EncodeToString(raw))
						for _, headerOnly := range []bool{false, true} {
							editor := d
							if headerOnly {
								editor.To, editor.Cc, editor.Subject = "new@example.test", "copy@example.test", "New café"
							}
							replacement, _ := draftReplacementMIME(editor, raw)
							next, err := client.prepareDraftReplacement(raw, replacement)
							if err != nil {
								t.Fatal(err)
							}
							h, _ := rawHeaders(next)
							if !bytes.Equal(raw[a.headers.body:], next[h.body:]) || !headerOnly && !bytes.Equal(raw, next) {
								t.Fatal("no-op/header-only changed original MIME bytes")
							}
							editor.ID, editor.MessageID = "d1", "api-revision-1"
							if _, err := client.SaveDraft(editor, ""); err != nil {
								t.Fatal(err)
							}
							written, _ := decodeDraftRaw(originalFixture.update.Message.Raw)
							if !bytes.Equal(next, written) {
								t.Fatal("actual no-op/header-only save differed from preparation")
							}
						}
						for _, body := range []string{"Literal = and =20", "Unicode café 😀\r\n\r\nlast\r\n", "  spaces  \t\tNBSP\u00a0 \n", ""} {
							f, c := newMetadataFixture(t, base64.RawURLEncoding.EncodeToString(raw))
							editor, err := c.GetDraft("d1")
							if err != nil {
								t.Fatal(err)
							}
							editor.Body = body
							if _, err := c.SaveDraft(editor, ""); err != nil || f.writes != 1 {
								t.Fatalf("actual save: %v writes=%d", err, f.writes)
							}
							next, _ := decodeDraftRaw(f.update.Message.Raw)
							b, err := parseAlternative(next)
							if err != nil || b.text != strings.ReplaceAll(body, "\r\n", "\n") || b.plainEncoding != "quoted-printable" {
								t.Fatalf("synchronized body: %q %v", b.text, err)
							}
							if !bytes.Equal(restoreDefaultEncodingSlots(next, b, raw, a), raw) {
								t.Fatal("bytes outside exact encoding promotion/body slots changed")
							}
							// A second actual save must retain even the first save's encoding bytes.
							f.raw = []byte(f.update.Message.Raw)
							editor, _ = c.GetDraft("d1")
							if _, err := c.SaveDraft(editor, ""); err != nil || f.writes != 2 {
								t.Fatalf("repeat save: %v writes=%d", err, f.writes)
							}
							again, _ := decodeDraftRaw(f.update.Message.Raw)
							if !bytes.Equal(next, again) {
								t.Fatal("repeat changed bytes")
							}
						}
					})
				}
			}
		}
	}
}

// Independent inverse splice proves that even header whitespace and framing survive.
func restoreDefaultEncodingSlots(next []byte, a draftAlternative, old []byte, b draftAlternative) []byte {
	type slot struct{ from, to draftSpan }
	slots := []slot{{a.plain, b.plain}, {a.html, b.html}}
	if b.format != "" {
		slots = append(slots, slot{a.formatValue, b.formatValue})
	}
	if b.plainEncoding != "quoted-printable" {
		from := a.encodingValue
		if b.plainEncoding == "" {
			from.start -= len("Content-Transfer-Encoding: ")
			from.end += 2
		}
		slots = append(slots, slot{from, b.encodingValue})
	}
	for i := range slots {
		for j := i + 1; j < len(slots); j++ {
			if slots[j].from.start < slots[i].from.start {
				slots[i], slots[j] = slots[j], slots[i]
			}
		}
	}
	var out []byte
	pos := 0
	for _, s := range slots {
		out = append(out, next[pos:s.from.start]...)
		out = append(out, old[s.to.start:s.to.end]...)
		pos = s.from.end
	}
	return append(out, next[pos:]...)
}

func TestAlternativeDefaultEncodingContextChecksAndSaves(t *testing.T) {
	for _, plain := range []string{"", "7bit", "quoted-printable"} {
		for _, html := range []string{"", "7bit"} {
			for _, reply := range []bool{false, true} {
				raw := defaultEncodingAlternative(plain, html, true, true)
				parent, context := "", "compose"
				if reply {
					raw = append([]byte(checkReplyHeaders), raw...)
					parent, context = "parent", "reply"
				}
				f, c, d := checkContextFixture(t, raw)
				out, err := c.CheckDraftSaveWithContext(d, parent)
				assertContextCheck(t, out, err, "supported", context)
				if f.writes != 0 || f.reads["/gmail/v1/users/me/profile"] != 2 || reply && f.reads["/gmail/v1/users/me/messages/parent"] != 2 {
					t.Fatalf("GET-only check: reads=%v writes=%d", f.reads, f.writes)
				}
				f.forbidWrites = false
				d.Body = "Actual café =20\n"
				if _, err := c.SaveDraft(d, parent); err != nil || f.writes != 1 {
					t.Fatalf("save after check: %v writes=%d", err, f.writes)
				}
			}
		}
	}
}

func TestAlternativeDefaultEncodingGuard(t *testing.T) {
	for _, plain := range []string{"", "7bit"} {
		raw := defaultEncodingAlternative(plain, "", false, false)
		a, _ := parseAlternative(raw)
		next, err := spliceAlternative(raw, a, "Edited")
		if err != nil || checkDraftReplacement(raw, next) != nil {
			t.Fatalf("valid promotion: %v", err)
		}
		// Every candidate remains subject to independent parsing and immutable bytes.
		for _, candidate := range [][]byte{
			bytes.Replace(next, []byte("Encoding: quoted-printable"), []byte("Encoding: QUOTED-PRINTABLE"), 1),
			bytes.Replace(next, []byte("Encoding: quoted-printable"), []byte("Encoding: 7bit"), 1),
			bytes.Replace(next, []byte("Content-Transfer-Encoding: quoted-printable\r\n"), nil, 1),
			bytes.Replace(next, []byte("Encoding: quoted-printable"), []byte("Encoding: 8bit"), 1),
			bytes.Replace(next, []byte("Content-Transfer-Encoding: quoted-printable"), []byte("Content-Transfer-Encoding: 7bit\r\nContent-Transfer-Encoding: quoted-printable"), 1),
			bytes.Replace(next, []byte("text/html;"), []byte("text/html; format=fixed;"), 1),
			bytes.Replace(next, []byte("text/html;"), []byte("text/html;\r\n"), 1),
			bytes.Replace(next, []byte("text/html;"), []byte("text/html; charset=UTF-8;"), 1),
			bytes.Replace(next, []byte("Content-Type: text/html;"), []byte("Content-Transfer-Encoding: 7bit\r\nContent-Type: text/html;"), 1),
			bytes.Replace(next, []byte("preamble PRIVATE"), []byte("preamble changed"), 1),
			bytes.Replace(next, []byte("epilogue PRIVATE"), []byte("epilogue changed"), 1),
			bytes.Replace(next, []byte("Received: opaque PRIVATE"), []byte("Received: changed"), 1),
			bytes.Replace(next, []byte(`dir="auto"`), []byte(`dir="ltr"`), 1),
		} {
			if !errors.Is(checkDraftReplacement(raw, candidate), ErrUnsupportedDraft) {
				t.Fatal("accepted encoding/header/metadata forgery")
			}
		}
		if !errors.Is(checkDraftReplacement(next, raw), ErrUnsupportedDraft) {
			t.Fatal("accepted reverse transition on body edit")
		}
		// Promote a byte-identical decoded body: must not gain an encoding exception.
		promoted := bytes.Replace(raw, []byte("Hello =20 & world"), []byte("Hello =3D20 & world"), 1)
		if plain == "" {
			at := a.encodingValue.start
			promoted = append(append(append([]byte{}, promoted[:at]...), []byte("Content-Transfer-Encoding: quoted-printable\r\n")...), promoted[at:]...)
		} else {
			promoted = bytes.Replace(promoted, []byte("Encoding: 7bit"), []byte("Encoding: quoted-printable"), 1)
		}
		if !errors.Is(checkDraftReplacement(raw, promoted), ErrUnsupportedDraft) {
			t.Fatal("accepted encoding change without decoded body change")
		}
	}
}

func TestAlternativeDefaultEncodingMalformedZeroWrites(t *testing.T) {
	base := defaultEncodingAlternative("", "", false, false)
	var cases [][]byte
	for _, bad := range []string{"NUL\x00", "control\x01", "formfeed\f", "DEL\x7f", "high\x80", "UTF8 café", "bare\rreturn", "bare\nnewline", strings.Repeat("x", 999)} {
		cases = append(cases, bytes.Replace(base, []byte("Hello =20 & world"), []byte(bad), 1))
		cases = append(cases, bytes.Replace(base, []byte("Hello =20 &amp; world"), []byte(bad), 1))
	}
	for _, header := range []string{
		"Content-Transfer-Encoding: base64\r\n", "Content-Transfer-Encoding: 8bit\r\n", "Content-Transfer-Encoding: binary\r\n", "Content-Transfer-Encoding: \r\n",
		"Content-Transfer-Encoding: 7bit\r\nContent-Transfer-Encoding: 7bit\r\n", "Content-Disposition: attachment\r\n", "X-Unknown: synthetic\r\n",
	} {
		cases = append(cases, bytes.Replace(base, []byte("Content-Type: text/plain;"), []byte(header+"Content-Type: text/plain;"), 1))
		cases = append(cases, bytes.Replace(base, []byte("Content-Type: text/html;"), []byte(header+"Content-Type: text/html;"), 1))
	}
	for _, rich := range []string{"<b>synthetic</b>", `<a href="https://example.test">synthetic</a>`, `<img src="https://example.test/synthetic">`, `<div class="gmail_signature">synthetic</div>`, "<blockquote>synthetic</blockquote>", "<unknown>synthetic</unknown>"} {
		cases = append(cases, bytes.Replace(base, []byte("Hello =20 &amp; world"), []byte(rich), 1))
	}
	for i, raw := range cases {
		f, c := newMetadataFixture(t, base64.RawURLEncoding.EncodeToString(raw))
		for _, body := range []string{"Hello =20 & world\n", "Edited"} {
			_, err := c.SaveDraft(Draft{ID: "d1", MessageID: "api-revision-1", Body: body}, "")
			if !errors.Is(err, ErrUnsupportedDraft) || f.writes != 0 {
				t.Fatalf("case %d: err=%v writes=%d", i, err, f.writes)
			}
		}
		out, err := c.CheckDraftSaveWithContext(Draft{ID: "d1", MessageID: "api-revision-1"}, "")
		if err != nil || out.Status != "unsupported" || out.BodyEditCompatible || out.UnchangedCompatible || f.writes != 0 {
			t.Fatalf("case %d: check=%+v err=%v writes=%d", i, out, err, f.writes)
		}
	}
}

func TestAlternative7bitLiteralEqualsAndWhitespace(t *testing.T) {
	text := "  literal = =20 =XX=\t \nnext\t "
	for _, encoding := range []string{"", "7bit"} {
		raw := defaultEncodingAlternative(encoding, "", false, false)
		raw = bytes.Replace(raw, []byte("Hello =20 & world\r\n"), []byte(strings.ReplaceAll(text, "\n", "\r\n")), 1)
		raw = bytes.Replace(raw, []byte("Hello =20 &amp; world"), []byte(draftPreWrap+encodeDraftHTMLText(text)+"</span>"), 1)
		a, err := parseAlternative(raw)
		if err != nil || a.text != text {
			t.Fatalf("7bit literal equals/whitespace: %q %v", a.text, err)
		}
		_, c, d := checkContextFixture(t, raw)
		out, err := c.CheckDraftSaveWithContext(d, "")
		assertContextCheck(t, out, err, "supported", "compose")
		candidate := bytes.Replace(raw, []byte("literal"), []byte("litera&#108;"), 1)
		if !errors.Is(checkDraftReplacement(raw, candidate), ErrUnsupportedDraft) {
			t.Fatal("accepted plain-only mutation")
		}
		// Even equivalent HTML rewrites are forbidden without a decoded body edit.
		candidate = bytes.Replace(raw, []byte("white-space: pre-wrap\">  literal"), []byte("white-space: pre-wrap\">  litera&#108;"), 1)
		if bytes.Equal(candidate, raw) || !errors.Is(checkDraftReplacement(raw, candidate), ErrUnsupportedDraft) {
			t.Fatal("accepted equivalent HTML rewrite without a decoded body change")
		}
	}
}

func TestAlternativeDefaultEncodingLineLimitsAndFlowed(t *testing.T) {
	for _, encoding := range []string{"", "7bit"} {
		raw := defaultEncodingAlternative(encoding, "", false, false)
		text := strings.Repeat("x", 480) + " " + strings.Repeat("y", 513) + " =20" // 998 bytes
		raw = bytes.Replace(raw, []byte("Hello =20 & world"), []byte(text), 1)
		raw = bytes.Replace(raw, []byte("Hello =20 &amp; world"), []byte(strings.Replace(text, " ", "\r\n", 1)), 1)
		if _, err := parseAlternative(raw); err != nil {
			t.Fatalf("998-byte literal 7bit line: %v", err)
		}
		if _, err := parseAlternative(bytes.Replace(raw, []byte(text), []byte(text+"x"), 1)); !errors.Is(err, ErrUnsupportedDraft) {
			t.Fatal("accepted 999-byte line")
		}
		flowed := bytes.Replace(defaultEncodingAlternative(encoding, "", true, true), []byte("text/plain;"), []byte("text/plain; format=flowed;"), 1)
		a, err := parseAlternative(flowed)
		if err != nil {
			t.Fatal(err)
		}
		next, err := spliceAlternative(flowed, a, "  literal =20\t \n>quote\nNBSP\u00a0")
		if err != nil || checkDraftReplacement(flowed, next) != nil {
			t.Fatalf("combined encoding/flowed normalization: %v", err)
		}
		b, err := parseAlternative(next)
		if err != nil || b.format != "fixed" || b.plainEncoding != "quoted-printable" || !bytes.Equal(restoreDefaultEncodingSlots(next, b, flowed, a), flowed) {
			t.Fatalf("combined transition changed immutable bytes: %v", err)
		}
	}
}
