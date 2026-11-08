package gmailclient

import (
	"bytes"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

func transportAlternativeFixture(envelope, trailing, eof bool) []byte {
	raw := defaultEncodingAlternative("", "", false, false)
	if !envelope {
		raw = bytes.Replace(raw, []byte("<html><body>"), nil, 1)
		raw = bytes.Replace(raw, []byte("</body></html>"), nil, 1)
	}
	if trailing {
		end := "</div>"
		if envelope {
			end += "</body></html>"
		}
		raw = bytes.Replace(raw, []byte(end), []byte(end+"\r\n"), 1)
	}
	if eof {
		raw = bytes.Replace(raw, []byte("--abc123--\r\nepilogue PRIVATE\r\n"), []byte("--abc123--"), 1)
	}
	return raw
}

func TestAlternativeTransportFramingPreserved(t *testing.T) {
	for _, envelope := range []bool{false, true} {
		for _, trailing := range []bool{false, true} {
			for _, eof := range []bool{false, true} {
				for _, reply := range []bool{false, true} {
					raw := transportAlternativeFixture(envelope, trailing, eof)
					parent, context := "", "compose"
					if reply {
						raw = append([]byte(checkReplyHeaders), raw...)
						parent, context = "parent", "reply"
					}
					a, err := parseAlternative(raw)
					if err != nil || a.text != "Hello =20 & world\n" || strings.HasSuffix(a.rootClose, "\r\n") != trailing {
						t.Fatalf("transport projection: %+v %v", a, err)
					}
					f, c, d := checkContextFixture(t, raw)
					out, err := c.CheckDraftSaveWithContext(d, parent)
					assertContextCheck(t, out, err, "supported", context)
					if f.writes != 0 {
						t.Fatal("check wrote")
					}
					f.forbidWrites = false
					// Actual no-op and header-only saves must preserve exact leaf/framing bytes.
					for _, subject := range []string{d.Subject, "Header edit"} {
						d.Subject = subject
						if _, err := c.SaveDraft(d, parent); err != nil {
							t.Fatal(err)
						}
						next, _ := decodeDraftRaw(f.update.Message.Raw)
						h, _ := rawHeaders(next)
						if !bytes.Equal(raw[a.headers.body:], next[h.body:]) || subject != "Header edit" && !bytes.Equal(raw, next) {
							t.Fatal("no-op/header edit changed transport bytes")
						}
					}
					d.Subject = "Saved"
					d.Body = "Edited café =20\r\n  tabs\tNBSP\u00a0\r\n"
					if _, err := c.SaveDraft(d, parent); err != nil {
						t.Fatal(err)
					}
					next, _ := decodeDraftRaw(f.update.Message.Raw)
					b, err := parseAlternative(next)
					if err != nil || b.rootClose != a.rootClose || b.text != strings.ReplaceAll(d.Body, "\r\n", "\n") || !bytes.Equal(restoreDefaultEncodingSlots(next, b, raw, a), raw) {
						t.Fatalf("transport body edit: %v", err)
					}
					if eof && !bytes.HasSuffix(next, []byte("--abc123--")) {
						t.Fatal("EOF delimiter gained CRLF")
					}
					if trailing {
						candidate := bytes.Replace(next, []byte(b.rootClose), []byte(strings.TrimSuffix(b.rootClose, "\r\n")), 1)
						if !errors.Is(checkDraftReplacement(raw, candidate), ErrUnsupportedDraft) {
							t.Fatal("guard accepted changed terminal HTML transport CRLF")
						}
					}
				}
			}
		}
	}
}

func TestAlternativeTransportWhitespaceAndBoundaryRefusals(t *testing.T) {
	base := transportAlternativeFixture(false, false, true)
	var cases [][]byte
	for _, suffix := range []string{"\r\n\r\n", " ", "\t", "\n", "\r", "junk", "<!--synthetic-->", "\r\n ", " \r\n"} {
		cases = append(cases, bytes.Replace(base, []byte("</div>"), []byte("</div>"+suffix), 1))
	}
	for _, ending := range []string{"--abc123", "--abc123-", "--abc123--junk", "--abc123--\r", "--abc123--\n", "--abc123--\r\n--abc123--", ""} {
		cases = append(cases, bytes.Replace(base, []byte("--abc123--"), []byte(ending), 1))
	}
	for i, raw := range cases {
		f, c := newMetadataFixture(t, base64.RawURLEncoding.EncodeToString(raw))
		_, err := c.SaveDraft(Draft{ID: "d1", MessageID: "api-revision-1", Body: "Edited"}, "")
		if !errors.Is(err, ErrUnsupportedDraft) || f.writes != 0 {
			t.Fatalf("case %d: err=%v writes=%d", i, err, f.writes)
		}
		out, err := c.CheckDraftSaveWithContext(Draft{ID: "d1", MessageID: "api-revision-1"}, "")
		if err != nil || out.Status != "unsupported" || f.writes != 0 {
			t.Fatalf("case %d: check=%+v err=%v writes=%d", i, out, err, f.writes)
		}
	}
}

func TestAlternativeDefaultEncodingSnapshotSenderParentGuards(t *testing.T) {
	base := transportAlternativeFixture(false, true, true)
	for _, tc := range []struct {
		name, parent string
		mutate       func(*metadataFixture, *Draft)
	}{
		{name: "stale revision", mutate: func(f *metadataFixture, d *Draft) { d.MessageID = "stale" }},
		{name: "missing revision", mutate: func(f *metadataFixture, d *Draft) { d.MessageID = "" }},
		{name: "sender unavailable", mutate: func(f *metadataFixture, d *Draft) { f.failProfile = true }},
		{name: "sender mismatch", mutate: func(f *metadataFixture, d *Draft) { f.profile.EmailAddress = "other@example.test" }},
		{name: "reply without parent", mutate: func(f *metadataFixture, d *Draft) {
			f.raw = []byte(base64.RawURLEncoding.EncodeToString(append([]byte(checkReplyHeaders), base...)))
		}},
		{name: "wrong parent thread", parent: "parent", mutate: func(f *metadataFixture, d *Draft) { f.thread = "other-thread" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, c, d := checkContextFixture(t, base)
			tc.mutate(f, &d)
			out, err := c.CheckDraftSaveWithContext(d, tc.parent)
			if err != nil || out.Status == "supported" || out.UnchangedCompatible || out.BodyEditCompatible || f.writes != 0 {
				t.Fatalf("check guard: %+v %v writes=%d", out, err, f.writes)
			}
			d.Body = "Edited"
			if _, err := c.SaveDraft(d, tc.parent); err == nil || f.writes != 0 {
				t.Fatalf("save guard: %v writes=%d", err, f.writes)
			}
		})
	}
}
