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

// Invented actual-style transport shape; no mailbox content or identifiers.
func flowedAlternativeFixture(parameter string, reverse bool) []byte {
	raw := bytes.Replace(alternativeFixture(), []byte("text/plain; charset=UTF-8"), []byte("text/plain; "+parameter), 1)
	raw = bytes.Replace(raw, []byte(`<div dir="ltr">Hello &amp; world</div>`), []byte(`<html><body><div dir="auto">Hello &amp; world</div></body></html>`), 1)
	if reverse {
		first := bytes.Index(raw, []byte("--abc123\r\n"))
		second := first + 1 + bytes.Index(raw[first+1:], []byte("--abc123\r\n"))
		last := bytes.Index(raw, []byte("--abc123--"))
		reversed := append([]byte{}, raw[:first]...)
		reversed = append(reversed, raw[second:last]...)
		reversed = append(reversed, raw[first:second]...)
		raw = append(reversed, raw[last:]...)
	}
	return raw
}

func TestAlternativeFlowedNormalization(t *testing.T) {
	for _, parameter := range []string{
		"charset=UTF-8; format=flowed",
		`charset="UTF-8"; format="flowed"`,
		"FORMAT=FLOWED; CHARSET=utf-8",
		"charset = \"UTF-8\";\r\n\tFoRmAt = \"FlOwEd\"",
	} {
		for _, reverse := range []bool{false, true} {
			t.Run(parameter+map[bool]string{true: "/reversed", false: "/normal"}[reverse], func(t *testing.T) {
				raw := append([]byte("From: Synthetic Owner <owner@example.test>\r\n"), flowedAlternativeFixture(parameter, reverse)...)
				f, c := newMetadataFixture(t, base64.RawURLEncoding.EncodeToString(raw))
				d, err := c.GetDraft("d1")
				if err != nil {
					t.Fatal(err)
				}
				replacement, err := draftReplacementMIME(d, raw)
				if err != nil {
					t.Fatal(err)
				}
				unchanged, err := c.prepareDraftReplacement(raw, replacement)
				if err != nil || !bytes.Equal(raw, unchanged) {
					t.Fatalf("no-op not byte identical: %v", err)
				}
				d.Subject = "Changed subject"
				replacement, _ = draftReplacementMIME(d, raw)
				headerOnly, err := c.prepareDraftReplacement(raw, replacement)
				if err != nil {
					t.Fatal(err)
				}
				h, _ := rawHeaders(headerOnly)
				oldh, _ := rawHeaders(raw)
				if !bytes.Equal(raw[oldh.body:], headerOnly[h.body:]) {
					t.Fatal("header-only edit normalized body/header")
				}
				check, err := c.CheckDraftSave("d1")
				if err != nil || !check.UnchangedCompatible || !check.BodyEditCompatible || f.writes != 0 {
					t.Fatalf("GET-only check: %+v %v writes=%d", check, err, f.writes)
				}
				if f.reads["/gmail/v1/users/me/profile"] != 4 || f.reads["/gmail/v1/users/me/settings/sendAs"] != 4 {
					t.Fatalf("sender GET checks: %v", f.reads)
				}
				d.Subject = "Saved"
				d.Body = "  leading  repeated\t\ttabs \n>literal quote\n-- \nUnicode café 😀 NBSP\u00a0\nlast \n\n"
				_, err = c.SaveDraft(d, "")
				if err != nil || f.writes != 1 {
					t.Fatalf("save: %v writes=%d", err, f.writes)
				}
				next, err := decodeDraftRaw(f.update.Message.Raw)
				if err != nil {
					t.Fatal(err)
				}
				old, err := parseAlternative(raw)
				if err != nil {
					t.Fatal(err)
				}
				a, err := parseAlternative(next)
				if err != nil || a.text != d.Body || a.format != "fixed" {
					t.Fatalf("fixed body: %q %v", a.text, err)
				}
				// Restore only the permitted format value and body slots: all other bytes exact.
				restored := restoreAlternativeSlots(next, a, raw, old)
				if !bytes.Equal(restored, raw) {
					t.Fatal("changed bytes outside approved token and bodies")
				}
				if err := checkDraftReplacement(raw, next); err != nil {
					t.Fatal(err)
				}
				parsed, err := parseDraft(next)
				if err != nil {
					t.Fatal(err)
				}
				replacement, _ = draftReplacementMIME(parsed, next)
				again, err := c.prepareDraftReplacement(next, replacement)
				if err != nil || !bytes.Equal(next, again) {
					t.Fatalf("repeat: %v", err)
				}
				parsed.Body = " >another\t fixed body \n"
				replacement, _ = draftReplacementMIME(parsed, next)
				again, err = c.prepareDraftReplacement(next, replacement)
				if err != nil {
					t.Fatal(err)
				}
				a, err = parseAlternative(again)
				if err != nil || a.text != parsed.Body || a.format != "fixed" {
					t.Fatalf("fixed re-edit: %v", err)
				}
			})
		}
	}
}

func restoreAlternativeSlots(next []byte, a draftAlternative, old []byte, b draftAlternative) []byte {
	type slot struct{ from, to draftSpan }
	slots := []slot{{a.plain, b.plain}, {a.html, b.html}, {a.formatValue, b.formatValue}}
	// Independent test splice, ordered by actual raw offsets.
	for i := 0; i < len(slots); i++ {
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

func TestAlternativeFlowedRefusalsZeroWritesAndPrivacy(t *testing.T) {
	base := flowedAlternativeFixture("charset=UTF-8; format=flowed", false)
	var cases [][]byte
	for _, text := range []string{" leading", ">quote", "trailing ", "-- ", "soft \nwrap", "ok\n next", "ok\n>next", "ok\nnext ", "bare\rreturn"} {
		var qp bytes.Buffer
		w := quotedprintable.NewWriter(&qp)
		_, _ = w.Write([]byte(strings.ReplaceAll(text, "\n", "\r\n")))
		_ = w.Close()
		raw := bytes.Replace(base, []byte("Hello & world\r\n"), qp.Bytes(), 1)
		// Exact generated projection isolates flowed proof from legacy HTML whitespace rules.
		raw = bytes.Replace(raw, []byte("Hello &amp; world"), []byte(draftPreWrap+encodeDraftHTMLText(text)+"</span>"), 1)
		cases = append(cases, raw)
	}
	for _, parameter := range []string{
		"charset=UTF-8; format=flowed; delsp=no", "charset=UTF-8; format=flowed; delsp=yes",
		"charset=UTF-8; format=flowed; delsp=PRIVATE", "charset=UTF-8; format=flowed; PRIVATE=value",
		"charset=UTF-8; format=flowed; format=flowed", "charset=UTF-8; charset=UTF-8; format=flowed",
		"charset=UTF-8; format*=utf-8''flowed", "charset=UTF-8; format*0=flow; format*1=ed",
		"charset*=utf-8''UTF-8; format=flowed", "charset=UTF-8; format=fixed; delsp=no",
		"charset=UTF-8; format=PRIVATE", "format=flowed", "charset=iso-8859-1; format=flowed",
		"format=\"fl\\owed\"; charset=UTF-8",
	} {
		cases = append(cases, flowedAlternativeFixture(parameter, false))
	}
	cases = append(cases, bytes.Replace(base, []byte("Hello &amp; world"), []byte("PRIVATE mismatch"), 1))
	for i, raw := range cases {
		f, c := newMetadataFixture(t, base64.RawURLEncoding.EncodeToString(raw))
		original, _ := parseDraft(raw)
		for _, body := range []string{original.Body, "edited"} {
			_, err := c.SaveDraft(Draft{ID: "d1", MessageID: "api-revision-1", Body: body}, "")
			if !errors.Is(err, ErrUnsupportedDraft) || f.writes != 0 {
				t.Fatalf("case %d: %v writes=%d", i, err, f.writes)
			}
			if strings.Contains(err.Error(), "PRIVATE") {
				t.Fatal("privacy leak")
			}
		}
		check, err := c.CheckDraftSave("d1")
		encoded, _ := json.Marshal(check)
		if err != nil || check.UnchangedCompatible || check.BodyEditCompatible || f.writes != 0 || strings.Contains(string(encoded), "PRIVATE") {
			t.Fatalf("case %d: check=%s err=%v writes=%d", i, encoded, err, f.writes)
		}
	}
}

func TestAlternativeFormatTransitionGuard(t *testing.T) {
	raw := flowedAlternativeFixture("charset=UTF-8; format=flowed", false)
	a, err := parseAlternative(raw)
	if err != nil {
		t.Fatal(err)
	}
	next, err := spliceAlternative(raw, a, "Edited")
	if err != nil {
		t.Fatal(err)
	}
	// Case-insensitive fixed semantics are permitted only within the value slot
	// during normalization; fixed-to-fixed token changes remain forbidden.
	candidate := bytes.Replace(next, []byte("format=fixed"), []byte("format=FIXED"), 1)
	if err := checkDraftReplacement(raw, candidate); err != nil {
		t.Fatal(err)
	}
	unnormalized := bytes.Replace(next, []byte("format=fixed"), []byte("format=flowed"), 1)
	if !errors.Is(checkDraftReplacement(raw, unnormalized), ErrUnsupportedDraft) {
		t.Fatal("flowed body edit without normalization accepted")
	}
	noop, err := spliceAlternative(raw, a, a.text)
	if err != nil || !bytes.Equal(raw, noop) {
		t.Fatalf("constructor normalized without a body edit: %v", err)
	}
	for _, candidate := range [][]byte{
		bytes.Replace(raw, []byte("format=flowed"), []byte("format=fixed"), 1),  // no body change
		bytes.Replace(next, []byte("format=fixed"), []byte("format=flowed"), 1), // fixed -> flowed below
		bytes.Replace(next, []byte("format=fixed"), []byte("FORMAT=fixed"), 1),
		bytes.Replace(next, []byte("charset=UTF-8"), []byte("charset=utf-8"), 1),
		bytes.Replace(next, []byte("Encoding: quoted-printable"), []byte("Encoding: QUOTED-PRINTABLE"), 1),
		bytes.Replace(next, []byte("<html><body>"), []byte(""), 1),
		bytes.Replace(next, []byte("Received: opaque PRIVATE"), []byte("Received: changed"), 1),
		append([]byte("From: attacker@example.test\r\n"), next...),
	} {
		current := raw
		if bytes.Contains(candidate, []byte("format=flowed")) {
			current = next
		}
		if !errors.Is(checkDraftReplacement(current, candidate), ErrUnsupportedDraft) {
			t.Fatal("guard accepted forbidden mutation")
		}
	}
	fixed := bytes.Replace(raw, []byte("format=flowed"), []byte("format=fixed"), 1)
	absent := bytes.Replace(raw, []byte("; format=flowed"), nil, 1)
	for _, pair := range [][2][]byte{{fixed, raw}, {absent, fixed}, {raw, absent}, {fixed, absent}, {fixed, bytes.Replace(fixed, []byte("fixed"), []byte("FIXED"), 1)}} {
		if !errors.Is(checkDraftReplacement(pair[0], pair[1]), ErrUnsupportedDraft) {
			t.Fatal("guard accepted format/token mutation")
		}
	}
}

func TestAlternativeExistingFixedLiteralText(t *testing.T) {
	text := "  indented  text\t\t \n>literal\n-- \nUnicode é 😀\u00a0\n"
	for _, reverse := range []bool{false, true} {
		raw := flowedAlternativeFixture(`charset="UTF-8"; FoRmAt="FiXeD"`, reverse)
		var qp bytes.Buffer
		w := quotedprintable.NewWriter(&qp)
		_, _ = w.Write([]byte(strings.ReplaceAll(text, "\n", "\r\n")))
		_ = w.Close()
		raw = bytes.Replace(raw, []byte("Hello & world\r\n"), qp.Bytes(), 1)
		raw = bytes.Replace(raw, []byte("Hello &amp; world"), []byte(draftPreWrap+encodeDraftHTMLText(text)+"</span>"), 1)
		a, err := parseAlternative(raw)
		if err != nil || a.text != text || a.format != "fixed" {
			t.Fatalf("existing fixed: %v", err)
		}
		d, err := parseDraft(raw)
		if err != nil {
			t.Fatal(err)
		}
		replacement, _ := draftReplacementMIME(d, raw)
		next, err := (Client{}).prepareDraftReplacement(raw, replacement)
		if err != nil || !bytes.Equal(raw, next) {
			t.Fatalf("fixed no-op: %v", err)
		}
		d.Body = " > edited\t\t \n"
		replacement, _ = draftReplacementMIME(d, raw)
		next, err = (Client{}).prepareDraftReplacement(raw, replacement)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(next, []byte(`FoRmAt="FiXeD"`)) {
			t.Fatal("fixed token changed")
		}
		a, err = parseAlternative(next)
		if err != nil || a.text != d.Body {
			t.Fatalf("fixed edit: %v", err)
		}
	}
}

func TestAlternativeFlowedRetainsSnapshotAndReplyGuards(t *testing.T) {
	base := append([]byte("From: Synthetic Owner <owner@example.test>\r\n"), flowedAlternativeFixture("charset=UTF-8; format=flowed", false)...)
	for _, tc := range []struct {
		name   string
		mutate func(*metadataFixture, *Draft)
		parent string
	}{
		{name: "stale revision", mutate: func(f *metadataFixture, d *Draft) { d.MessageID = "stale" }},
		{name: "missing revision", mutate: func(f *metadataFixture, d *Draft) { d.MessageID = "" }},
		{name: "deleted", mutate: func(f *metadataFixture, d *Draft) { f.deleted = true }},
		{name: "sender unavailable", mutate: func(f *metadataFixture, d *Draft) { f.failProfile = true }},
		{name: "sender alias", mutate: func(f *metadataFixture, d *Draft) { f.profile.EmailAddress = "other@example.test" }},
		{name: "reply without parent", mutate: func(f *metadataFixture, d *Draft) {
			f.raw = []byte(base64.RawURLEncoding.EncodeToString(append([]byte("In-Reply-To: <PRIVATE@example.test>\r\n"), base...)))
		}},
		{name: "thread mismatch", parent: "parent", mutate: func(f *metadataFixture, d *Draft) { f.thread = "other-thread" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, body := range []string{"Hello & world\n", "edited"} {
				f, c := newMetadataFixture(t, base64.RawURLEncoding.EncodeToString(base))
				d := Draft{ID: "d1", MessageID: "api-revision-1", To: "ada@example.test", Subject: "Saved", Body: body}
				tc.mutate(f, &d)
				_, err := c.SaveDraft(d, tc.parent)
				if err == nil || f.writes != 0 {
					t.Fatalf("guard bypass: %v writes=%d", err, f.writes)
				}
				// Sender/parent preparation refusals use closed categories; provider deletion
				// remains the pre-existing public SaveDraft error contract.
				if tc.name != "deleted" && strings.Contains(err.Error(), "PRIVATE") {
					t.Fatal("preparation privacy leak")
				}
			}
		})
	}
}
