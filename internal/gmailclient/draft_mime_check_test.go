package gmailclient

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"google.golang.org/api/gmail/v1"
	"google.golang.org/api/option"
)

func mimeLeaf(media, encoding, body string) string {
	h := "Content-Type: " + media + "\r\n"
	if encoding != "" {
		h += "Content-Transfer-Encoding: " + encoding + "\r\n"
	}
	return h + "\r\n" + body
}

func mimeMultipart(media, boundary string, children ...string) string {
	return "Content-Type: " + media + "; boundary=\"" + boundary + "\"\r\n\r\n--" + boundary + "\r\n" + strings.Join(children, "\r\n--"+boundary+"\r\n") + "\r\n--" + boundary + "--\r\n"
}

func assertMIMENoLeak(t *testing.T, tree *DraftMIMEPart, err error) {
	t.Helper()
	b, marshalErr := json.Marshal(tree)
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	text := string(b)
	if err != nil {
		text += err.Error()
		if tree != nil {
			t.Fatal("partial tree on failure")
		}
	}
	for _, secret := range []string{"PRIVATE", "owner@example.test", "secret.invalid", "SECRET", "private-boundary", "x-private", "cid:"} {
		if strings.Contains(text, secret) {
			t.Fatalf("diagnosis leaked %q", secret)
		}
	}
}

func TestDraftMIMEGmailAlternative(t *testing.T) {
	raw := "From: PRIVATE <owner@example.test>\r\nSubject: PRIVATE\r\nX-Private: SECRET\r\n" + mimeMultipart("multipart/alternative", "private-boundary",
		mimeLeaf("text/plain; charset=UTF-8", "quoted-printable", "PRIVATE words"),
		mimeLeaf("text/html; charset=UTF-8", "quoted-printable", "<div dir=3D\"ltr\">PRIVATE words<br>Second line</div>"))
	tree, err := inspectDraftMIME([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	assertMIMENoLeak(t, tree, err)
	want := &DraftMIMEPart{MIMEClass: "multipart/alternative", Charset: "none", TransferEncoding: "none", Disposition: "none", Parts: []DraftMIMEPart{
		{MIMEClass: "text/plain", Charset: "utf-8", TransferEncoding: "quoted-printable", Disposition: "none", Parts: []DraftMIMEPart{}},
		{MIMEClass: "text/html", Charset: "utf-8", TransferEncoding: "quoted-printable", Disposition: "none", Parts: []DraftMIMEPart{}, HTMLFeatures: &DraftHTMLFeatures{}},
	}}
	if !reflect.DeepEqual(tree, want) {
		t.Fatalf("got %+v; parts %+v", tree, tree.Parts)
	}
}

func TestDraftMIMENestedPrivateAttachments(t *testing.T) {
	attachment := "Content-Disposition: attachment; filename=\"PRIVATE.pdf\"\r\nContent-ID: <PRIVATE>\r\n" + mimeLeaf("application/x-private; private=SECRET", "base64", "PRIVATE OPAQUE BODY")
	raw := mimeMultipart("multipart/mixed", "private-boundary",
		mimeMultipart("multipart/related", "related", mimeLeaf("text/html", "", "<img src=\"cid:PRIVATE\"><a href=\"https://secret.invalid\">PRIVATE</a>"), attachment),
		mimeLeaf("image/x-private", "x-private", "PRIVATE"))
	tree, err := inspectDraftMIME([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	assertMIMENoLeak(t, tree, err)
	if tree.MIMEClass != "multipart/mixed" || tree.Parts[0].MIMEClass != "multipart/related" || tree.Parts[0].Parts[1].MIMEClass != "other" || tree.Parts[0].Parts[1].Disposition != "attachment" || tree.Parts[1].TransferEncoding != "other" {
		t.Fatalf("unexpected tree: %+v", tree)
	}
	features := tree.Parts[0].Parts[0].HTMLFeatures
	if !features.HasImages || !features.HasLinks || features.HasFormatting {
		t.Fatalf("features: %+v", features)
	}
}

func TestDraftMIMEHTMLTransferEncodings(t *testing.T) {
	const rich = `<div class="gmail_signature PRIVATE"><b>PRIVATE</b><blockquote><div class="gmail_quote"><a href="https://secret.invalid">link</a><img src="cid:PRIVATE"><x-private data-private="SECRET"></x-private></div></blockquote></div>`
	for _, tc := range []struct{ encoding, body string }{
		{"", rich}, {"7bit", rich}, {"8bit", rich}, {"binary", rich},
		{"base64", base64.StdEncoding.EncodeToString([]byte(rich))},
		{"quoted-printable", strings.ReplaceAll(rich, "=", "=3D")},
	} {
		t.Run(tc.encoding, func(t *testing.T) {
			tree, err := inspectDraftMIME([]byte(mimeMultipart("multipart/alternative", "b", mimeLeaf("text/html; charset=utf-8", tc.encoding, tc.body))))
			if err != nil {
				t.Fatal(err)
			}
			assertMIMENoLeak(t, tree, err)
			f := tree.Parts[0].HTMLFeatures
			if !f.HasLinks || !f.HasImages || !f.HasFormatting || !f.HasQuotes || !f.HasSignature || !f.HasUnknownElements {
				t.Fatalf("features: %+v", f)
			}
			want := tc.encoding
			if want == "" {
				want = "none"
			}
			if tree.Parts[0].TransferEncoding != want {
				t.Fatal("transfer encoding was implicitly stripped")
			}
		})
	}
	for _, content := range []string{`<span style="PRIVATE">words</span>`, `<div align="PRIVATE">words</div>`, `<ul><li>words</li></ul>`} {
		tree, err := inspectDraftMIME([]byte(mimeLeaf("text/html", "", content)))
		if err != nil || !tree.HTMLFeatures.HasFormatting {
			t.Fatalf("formatting: %+v, %v", tree, err)
		}
	}
}

func TestDraftMIMECategories(t *testing.T) {
	for _, media := range []string{"text/plain", "text/html", "multipart/alternative", "multipart/mixed", "multipart/related", "multipart/signed", "multipart/encrypted", "multipart/x-private"} {
		raw := mimeLeaf(media, "", "PRIVATE")
		if strings.HasPrefix(media, "multipart/") {
			raw = mimeMultipart(media, "b", mimeLeaf("text/plain", "", "PRIVATE"))
		}
		tree, err := inspectDraftMIME([]byte(raw))
		if err != nil {
			t.Fatal(err)
		}
		want := media
		if media == "multipart/x-private" {
			want = "other"
		}
		if tree.MIMEClass != want {
			t.Fatalf("class: %s", tree.MIMEClass)
		}
		assertMIMENoLeak(t, tree, err)
	}
	for _, charset := range []string{"utf-8", "us-ascii", "iso-8859-1", "windows-1252", "x-private"} {
		for _, disposition := range []string{"inline", "attachment", "x-private"} {
			tree, err := inspectDraftMIME([]byte("Content-Disposition: " + disposition + "; filename=PRIVATE\r\n" + mimeLeaf("text/plain; charset="+charset, "", "PRIVATE")))
			if err != nil {
				t.Fatal(err)
			}
			wantCharset, wantDisposition := charset, disposition
			if charset == "x-private" {
				wantCharset = "other"
			}
			if disposition == "x-private" {
				wantDisposition = "other"
			}
			if tree.Charset != wantCharset || tree.Disposition != wantDisposition {
				t.Fatalf("categories: %+v", tree)
			}
			assertMIMENoLeak(t, tree, err)
		}
	}
}

func TestDraftMIMEFailsClosed(t *testing.T) {
	for _, raw := range []string{
		"PRIVATE malformed headers\r\n\r\nSECRET", "Content-Type: PRIVATE \"\r\n\r\nSECRET",
		mimeLeaf("multipart/mixed", "", "PRIVATE"),
		"Content-Type: multipart/mixed; boundary=PRIVATE\r\n\r\n--PRIVATE\r\nPRIVATE invalid header\r\n\r\nSECRET",
		"Content-Type: text/plain\r\nContent-Type: text/html\r\n\r\nPRIVATE",
		"Content-Disposition: PRIVATE \"\r\n" + mimeLeaf("text/plain", "", "SECRET"),
		mimeLeaf("text/html", "base64", "PRIVATE !!!"), mimeLeaf("text/html", "quoted-printable", "PRIVATE =0Z"),
		mimeLeaf("text/html; charset=utf-8", "", "PRIVATE\xff"), mimeLeaf("text/html", "", "PRIVATE\x00"),
		mimeMultipart("multipart/alternative", "b", mimeLeaf("text/html", "base64", "PRIVATE !!!")),
		strings.TrimSuffix(mimeMultipart("multipart/mixed", "b", mimeLeaf("text/plain", "", "PRIVATE")), "--b--\r\n"),
	} {
		tree, err := inspectDraftMIME([]byte(raw))
		if !errors.Is(err, ErrDraftMIMEMalformed) {
			t.Fatalf("expected fixed malformed error, got %v for %q", err, raw)
		}
		assertMIMENoLeak(t, tree, err)
	}
	for _, raw := range []string{mimeLeaf("text/html; charset=x-private", "", "SECRET"), mimeLeaf("text/html", "x-private", "SECRET")} {
		tree, err := inspectDraftMIME([]byte(raw))
		if !errors.Is(err, ErrDraftMIMEEncoding) {
			t.Fatalf("encoding error: %v", err)
		}
		assertMIMENoLeak(t, tree, err)
	}
}

func TestDraftMIMELimits(t *testing.T) {
	leaf := mimeLeaf("text/plain", "", "PRIVATE")
	deep := leaf
	for i := 0; i < maxDraftMIMEDepth; i++ {
		deep = mimeMultipart("multipart/mixed", fmt.Sprintf("b%d", i), deep)
	}
	wide := make([]string, maxDraftMIMEParts)
	for i := range wide {
		wide[i] = leaf
	}
	for _, raw := range []string{
		leaf + strings.Repeat("x", maxDraftMIMEBytes), deep, mimeMultipart("multipart/mixed", "b", wide...),
		mimeLeaf("text/html", "", strings.Repeat("x", maxDraftHTMLBytes+1)),
		mimeLeaf("text/html", "base64", base64.StdEncoding.EncodeToString([]byte(strings.Repeat("x", maxDraftHTMLBytes+1)))),
		mimeLeaf("text/html", "quoted-printable", strings.Repeat(strings.Repeat("=41", 25)+"=\r\n", maxDraftHTMLBytes/25+1)),
		mimeLeaf("text/html", "", strings.Repeat("<br>", maxDraftHTMLTokens+1)),
	} {
		tree, err := inspectDraftMIME([]byte(raw))
		if !errors.Is(err, ErrDraftMIMELimit) {
			t.Fatalf("expected limit, got %v for %q (%d bytes)", err, raw[:min(100, len(raw))], len(raw))
		}
		assertMIMENoLeak(t, tree, err)
	}
	// Exactly-at-limit inputs are not silently truncated.
	for _, raw := range []string{leaf + strings.Repeat("x", maxDraftMIMEBytes-len(leaf)), mimeLeaf("text/html", "", strings.Repeat("x", maxDraftHTMLBytes)), mimeMultipart("multipart/mixed", "b", wide[:maxDraftMIMEParts-1]...)} {
		if _, err := inspectDraftMIME([]byte(raw)); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCheckDraftMIMEProviderReadOnlyAndSafe(t *testing.T) {
	for _, tc := range []struct {
		name         string
		status       int
		body         string
		transportErr bool
		want         error
	}{
		{name: "unpadded", body: `{"message":{"raw":"` + base64.RawURLEncoding.EncodeToString([]byte(mimeLeaf("text/html", "", "<div>PRIVATE</div>"))) + `"}}`},
		{name: "padded", body: `{"message":{"raw":"` + base64.URLEncoding.EncodeToString([]byte(mimeLeaf("text/html", "", "<div>PRIVATE</div>"))) + `"}}`},
		{name: "missing", body: `{}`, want: ErrDraftMIMEMalformed},
		{name: "bad raw", body: `{"message":{"raw":"PRIVATE !!!"}}`, want: ErrDraftMIMEMalformed},
		{name: "oversize raw", body: `{"message":{"raw":"` + strings.Repeat("A", base64.URLEncoding.EncodedLen(maxDraftMIMEBytes)+1) + `"}}`, want: ErrDraftMIMELimit},
		{name: "bad provider json", body: `PRIVATE broken JSON`},
		{name: "deleted", status: 404}, {name: "forbidden", status: 403}, {name: "provider failure", status: 500}, {name: "transport", transportErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			transport := fixtureTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Method != "GET" || r.URL.Path != "/gmail/v1/users/me/drafts/d1" || r.URL.Query().Get("format") != "raw" || r.URL.Query().Get("fields") != "message/raw" {
					t.Fatalf("unexpected provider request: %s %s", r.Method, r.URL)
				}
				if tc.transportErr {
					return nil, errors.New("PRIVATE owner@example.test transport SECRET")
				}
				status, body := tc.status, tc.body
				if status == 0 {
					status = 200
				}
				if status != 200 {
					body = fmt.Sprintf(`{"error":{"code":%d,"message":"PRIVATE owner@example.test SECRET"}}`, status)
				}
				return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			})
			svc, err := gmail.NewService(context.Background(), option.WithHTTPClient(&http.Client{Transport: transport}), option.WithEndpoint("https://fixture.invalid/"))
			if err != nil {
				t.Fatal(err)
			}
			c := Client{ctx: context.Background(), svc: svc}
			tree, err := c.CheckDraftMIME("d1")
			if calls != 1 {
				t.Fatalf("provider requests: %d", calls)
			}
			assertMIMENoLeak(t, tree, err)
			if tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatalf("want %v, got %v", tc.want, err)
			}
			if tc.status == 404 && !IsNotFound(err) {
				t.Fatalf("not found: %v", err)
			}
			if tc.name == "unpadded" || tc.name == "padded" {
				if err != nil || tree.HTMLFeatures == nil {
					t.Fatalf("tree=%+v err=%v", tree, err)
				}
			} else if err == nil {
				t.Fatal("expected failure")
			}
			if _, err := c.CheckDraftMIME(" "); !errors.Is(err, ErrDraftMIMEMalformed) || calls != 1 {
				t.Fatal("empty id reached provider")
			}
		})
	}
}
