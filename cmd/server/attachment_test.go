package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zachpmanson/chainmail/internal/corpus"
)

// The serving half of the media design, at the handler: the file itself, and the
// headers that decide what a browser does with it. The fixture corpus holds one
// attachment (shed.csv, text/csv), so the plain case is a real read of real bytes.

func TestAttachmentServesTheStoredBytes(t *testing.T) {
	h := testServer(t)
	res := h.do(t, "GET", "/v1/attachments/"+shedSHA, nil)
	if res.status != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", res.status, res.body)
	}
	if string(res.body) != shedBytes {
		t.Errorf("body = %q, want the filed bytes", res.body)
	}
	if got := res.header.Get("Content-Type"); got != "text/csv" {
		t.Errorf("Content-Type = %q, want the attachment row's own type", got)
	}
	if got := res.header.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
	}
	if got := res.header.Get("ETag"); got != `"`+shedSHA+`"` {
		t.Errorf("ETag = %q, want the digest quoted", got)
	}
	if got := res.header.Get("Cache-Control"); !strings.Contains(got, "immutable") {
		t.Errorf("Cache-Control = %q, want an immutable response: the digest is the content", got)
	}
	if got := res.header.Get("Content-Length"); got == "" {
		t.Error("no Content-Length: ServeContent should have sized the response from the reader")
	}
}

func TestAttachmentDispositionFollowsTheType(t *testing.T) {
	// What a browser does with the file, per type. internal/spec asserts the part
	// that matters — that this is the same rule that put `open` on the chip — so
	// what is checked here is only that the mapping reaches the header.
	cases := []struct {
		name string
		mime string
		want string
	}{
		{"shed.csv", "text/csv", "inline"},
		{"readings.png", "image/png", "inline"},
		{"walkthrough.mp4", "video/mp4", "inline"},
		{"quote.pdf", "application/pdf", "attachment"},
		{"plan.docx", "application/vnd.openxmlformats-officedocument.wordprocessingml.document", "attachment"},
		// Markup is never inline, whatever it looks like: served from this origin,
		// a sender's HTML is script.
		{"page.html", "text/html", "attachment"},
		{"logo.svg", "image/svg+xml", "attachment"},
		{"ledger.xhtml", "application/xhtml+xml", "attachment"},
	}
	for _, c := range cases {
		t.Run(c.mime, func(t *testing.T) {
			h := testServer(t)
			sha := storeAs(t, h, c.name, c.mime)
			got := h.do(t, "GET", "/v1/attachments/"+sha, nil).header.Get("Content-Disposition")
			if !strings.HasPrefix(got, c.want) {
				t.Errorf("Content-Disposition = %q, want it to start %q", got, c.want)
			}
			if !strings.Contains(got, `filename="`+c.name+`"`) {
				t.Errorf("Content-Disposition = %q, want it to carry the sender's filename", got)
			}
		})
	}
}

func TestAttachmentRefusesWhatItCannotServe(t *testing.T) {
	h := testServer(t)
	// Not a digest: the shape is checked before the query, because the answer to a
	// mistake should name the mistake.
	for _, bad := range []string{"not-a-digest", strings.ToUpper(shedSHA), shedSHA[:63], shedSHA + "0"} {
		res := h.do(t, "GET", "/v1/attachments/"+bad, nil)
		if res.status != http.StatusBadRequest {
			t.Errorf("GET %q status = %d, want 400", bad, res.status)
			continue
		}
		if got := res.errText(t); !strings.Contains(got, "digest") {
			t.Errorf("GET %q error = %q, want it to say what shape was wanted", bad, got)
		}
	}
	// A digest filed under nothing: a file never pulled, or one that was pruned.
	missing := strings.Repeat("0", 64)
	res := h.do(t, "GET", "/v1/attachments/"+missing, nil)
	if res.status != http.StatusNotFound {
		t.Fatalf("unfiled digest status = %d, want 404", res.status)
	}
	if got := res.errText(t); !strings.Contains(got, missing) {
		t.Errorf("error = %q, want it to name the digest that has nothing under it", got)
	}
}

func TestAttachmentCanBeRangeRequested(t *testing.T) {
	// A stored video has to seek and a re-opened PDF should cost nothing: both are
	// Range requests, and both are why this is http.ServeContent rather than a
	// write of the bytes.
	h := testServer(t)
	req := h.do(t, "GET", "/v1/attachments/"+shedSHA, nil)
	if got := req.header.Get("Accept-Ranges"); got != "bytes" {
		t.Errorf("Accept-Ranges = %q, want bytes", got)
	}
	// The digest is the content, so a conditional request can be answered without
	// the bytes: the same URL can never mean anything else.
	etag := req.header.Get("ETag")
	asked := httptest.NewRequest("GET", "/v1/attachments/"+shedSHA, nil)
	asked.Header.Set("If-None-Match", etag)
	rec := httptest.NewRecorder()
	h.handler.ServeHTTP(rec, asked)
	if rec.Code != http.StatusNotModified {
		t.Errorf("If-None-Match %s status = %d, want 304", etag, rec.Code)
	}
}

func TestContentDispositionKeepsAFilenameInOnePiece(t *testing.T) {
	cases := []struct {
		name string
		want string
	}{
		// The plain case.
		{"quote.pdf", `attachment; filename="quote.pdf"`},
		// A newline in a header value is header injection, and a sender controls
		// this text: dropped, not escaped, because no escape means "value ends here".
		{"ev\r\nX-Injected: yes\n.pdf", `attachment; filename="evX-Injected: yes.pdf"`},
		// A quote or a backslash would end the quoted-string early and hand the rest
		// of the name over as parameters, so both are escaped rather than dropped —
		// the name is still the name.
		{`he said "hi".pdf`, `attachment; filename="he said \"hi\".pdf"`},
		{`C:\quotes\quote.pdf`, `attachment; filename="C:\\quotes\\quote.pdf"`},
		// Anything outside ASCII also carries the RFC 5987 form, which every current
		// browser prefers; the quoted form stays as the fallback, keeping the
		// extension, which is the part a client acts on.
		{"rechnung f\u00fcr m\u00e4rz.pdf", `attachment; filename="rechnung fr mrz.pdf"; filename*=UTF-8''rechnung%20f%C3%BCr%20m%C3%A4rz.pdf`},
		// Nothing left to name once the control characters are gone.
		{"\r\n", "attachment"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := contentDisposition("attachment", c.name)
			if got != c.want {
				t.Errorf("contentDisposition(%q) = %q, want %q", c.name, got, c.want)
			}
			if strings.ContainsAny(got, "\r\n") {
				t.Errorf("contentDisposition(%q) = %q, and a header value is one line", c.name, got)
			}
			// An odd quote count means the value ended early and the rest of the
			// name became parameters.
			if strings.Count(got, `"`)%2 != 0 {
				t.Errorf("contentDisposition(%q) = %q, which leaves a quoted-string open", c.name, got)
			}
		})
	}
}

// storeAs files bytes under a name and MIME the test chooses, and links them to a
// message: the name a digest is served under comes from an attachment row, so
// bytes with no row have no name to give.
func storeAs(t *testing.T, h *harness, name, mime string) string {
	t.Helper()
	const ref = "2" // the part id this test files under
	data := []byte("pretend this is " + name)
	sha := corpus.BlobSHA(data)
	ext := "mail:<" + strings.ReplaceAll(name, " ", "-") + "@loomworks.example>"
	putMail(t, h.server.store, mailFixture{
		ext: ext, ts: "2026-03-04T09:00:00+11:00", tz: "AEDT",
		person:    putPerson(t, h.server.store, "Ada Okoye", "ada@loomworks.example"),
		container: "T2", subject: "Drawings", messageID: "<" + name + "@loomworks.example>",
		from: "Ada Okoye <ada@loomworks.example>", to: "Bo Halvorsen <bo@fjordline.example>",
		text: "Attached.",
		atts: []corpus.Attachment{{Name: name, Mime: mime, Size: int64(len(data)), SourceRef: ref}},
	})
	if err := h.server.store.PutBlob(corpus.Blob{SHA: sha, Bytes: data, Mime: mime, Source: "mail"}); err != nil {
		t.Fatalf("PutBlob %s: %v", name, err)
	}
	if n, err := h.server.store.LinkBlob(ext, ref, sha); err != nil || n != 1 {
		t.Fatalf("LinkBlob %s: %d rows, %v", name, n, err)
	}
	return sha
}
