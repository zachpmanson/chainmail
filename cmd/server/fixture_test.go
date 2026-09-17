package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/zachpmanson/chainmail/internal/corpus"
	"github.com/zachpmanson/chainmail/internal/embed"
)

// Every name, address and id below is invented, and every domain is a reserved
// example one. Real correspondence is never committed; the only checks against
// a real corpus are run by hand against an untracked copy.

const (
	extAda1  = "mail:<c0ffee-1@loomworks.example>"
	extBo2   = "mail:<c0ffee-2@fjordline.example>"
	extAda3  = "mail:<c0ffee-3@loomworks.example>"
	extOther = "mail:<c0ffee-9@loomworks.example>"
	extNone  = "mail:<no-such-entry@loomworks.example>"

	// The recovered entry and the message that quoted it are their own fixture
	// (see quotedServer): a recovered entry is the one shape the corpus below
	// deliberately does not hold.
	extQuoted     = "quote:9f2c1ab4e77d"
	extQuotedHost = "mail:<c0ffee-5@loomworks.example>"

	// The entry carrying html of its own and its plain-text neighbour are their
	// own fixture too (see htmlServer). An html body changes what the transcript
	// renders, so putting one in the shared corpus above would be a change to
	// every assertion about what a message says.
	extHTML  = "mail:<c0ffee-6@loomworks.example>"
	extPlain = "mail:<c0ffee-7@loomworks.example>"
)

// shedBytes is the fixture attachment's content, filed as a blob so that the
// corpus holds one file: GET /v1/attachments/{sha} needs something real to serve,
// and the documented-path walk substitutes this digest — an unrouted path answers
// 404, while an unsubstituted placeholder could only ever be a 400.
const (
	shedBytes = "shed,readings\nnorth,42\n"
	shedPart  = "1" // the part id the fixture's attachment carries
)

var shedSHA = corpus.BlobSHA([]byte(shedBytes))

type harness struct {
	*server
	handler http.Handler
}

type response struct {
	status int
	header http.Header
	body   []byte
}

// errText is the one shape every non-2xx carries.
func (r *response) errText(t *testing.T) string {
	t.Helper()
	var e struct{ Error string }
	if err := json.Unmarshal(r.body, &e); err != nil {
		t.Fatalf("a %d response is not an error object: %s", r.status, r.body)
	}
	if e.Error == "" {
		t.Errorf("a %d response carries no message", r.status)
	}
	return e.Error
}

func (h *harness) do(t *testing.T, method, path string, body []byte) *response {
	t.Helper()
	return h.doWithContext(t, context.Background(), method, path, body)
}

// doWithContext is do with the request's context in the caller's hands, so a
// test can take the reader away mid-request — which is what a browser does to
// its own fetch when the page is reloaded or the tab is closed.
func (h *harness) doWithContext(t *testing.T, ctx context.Context, method, path string, body []byte) *response {
	t.Helper()
	var rdr *bytes.Reader
	if body == nil {
		rdr = bytes.NewReader(nil)
	} else {
		rdr = bytes.NewReader(body)
	}
	req := httptest.NewRequest(method, path, rdr).WithContext(ctx)
	rec := httptest.NewRecorder()
	h.handler.ServeHTTP(rec, req)
	res := rec.Result()
	return &response{status: res.StatusCode, header: res.Header, body: rec.Body.Bytes()}
}

// testServer is the server over a small invented corpus: one three-message
// thread about a solar quote, and one unrelated message so that a query can be
// wrong as well as right.
func testServer(t *testing.T) *harness {
	t.Helper()
	s, err := corpus.Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	ada := putPerson(t, s, "Ada Okoye", "ada@loomworks.example")
	bo := putPerson(t, s, "Bo Halvorsen", "bo@fjordline.example")

	a := putMail(t, s, mailFixture{
		ext: extAda1, ts: "2026-03-02T09:15:00+11:00", tz: "AEDT", offset: mins(660),
		person: ada, container: "T1", subject: "Solar install quote",
		messageID: "<c0ffee-1@loomworks.example>",
		from:      "Ada Okoye <ada@loomworks.example>",
		to:        "Bo Halvorsen <bo@fjordline.example>",
		text:      "Can you quote the solar install for the north shed?",
		labels:    []string{"INBOX", "IMPORTANT"},
		atts:      []corpus.Attachment{{Name: "shed.csv", Mime: "text/csv", Size: 512, SourceRef: shedPart}},
	})
	b := putMail(t, s, mailFixture{
		ext: extBo2, ts: "2026-03-02T23:40:00+01:00", tz: "+0100",
		person: bo, container: "T1", subject: "Solar install quote",
		messageID: "<c0ffee-2@fjordline.example>", inReplyTo: "<c0ffee-1@loomworks.example>",
		from:   "Bo Halvorsen <bo@fjordline.example>",
		to:     "Ada Okoye <ada@loomworks.example>",
		labels: []string{"INBOX"},
		text:   "Quote attached. The install needs two days of roof access.",
	})
	c := putMail(t, s, mailFixture{
		ext: extAda3, ts: "2026-03-03T10:00:00+11:00", tz: "AEDT",
		person: ada, container: "T1", subject: "Solar install quote: dates",
		messageID: "<c0ffee-3@loomworks.example>", inReplyTo: "<c0ffee-2@fjordline.example>",
		from:   "Ada Okoye <ada@loomworks.example>",
		to:     "Bo Halvorsen <bo@fjordline.example>",
		labels: []string{"SENT"},
		text:   "Roof access is fine from the 14th.",
	})
	putMail(t, s, mailFixture{
		ext: extOther, ts: "2026-04-01T08:00:00+11:00", tz: "AEDT",
		person: ada, container: "T9", subject: "Fence panels",
		messageID: "<c0ffee-9@loomworks.example>",
		from:      "Ada Okoye <ada@loomworks.example>",
		to:        "Bo Halvorsen <bo@fjordline.example>",
		text:      "Unrelated: the fence panels arrived.",
		labels:    []string{"INBOX", "CATEGORY_PROMOTIONS"},
	})
	for _, id := range []int64{a, b, c} {
		if err := s.Sight(id, 0, "direct", ""); err != nil {
			t.Fatalf("Sight: %v", err)
		}
	}
	if _, err := s.ResolveParents(); err != nil {
		t.Fatalf("ResolveParents: %v", err)
	}
	// The one file the fixture corpus holds, linked to the part it arrived as.
	if err := s.PutBlob(corpus.Blob{SHA: shedSHA, Bytes: []byte(shedBytes), Mime: "text/csv", Source: "mail"}); err != nil {
		t.Fatalf("PutBlob: %v", err)
	}
	if n, err := s.LinkBlob(extAda1, shedPart, shedSHA); err != nil || n != 1 {
		t.Fatalf("LinkBlob: %d rows, %v", n, err)
	}
	return harnessOver(t, s)
}

// quotedServer is the server over a corpus holding the one shape the fixture
// above has none of: a message recovered from quoted text, and the message it was
// found inside.
//
// It is a corpus of its own rather than two more entries in testServer's, because
// an extra entry changes what the shared fixture's counts mean for every test
// that is not about this one — a chain's length, the ops screen's people — and
// those assertions are the ones a fixture exists to keep honest.
//
// The recovered entry is a chain of its own, and that is the point of the shape:
// the host that quoted it is outside any trail it can be rendered in, so a name
// for that host can only come from a load the render goes and makes.
func quotedServer(t *testing.T) *harness {
	t.Helper()
	s, err := corpus.Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	ada := putPerson(t, s, "Ada Okoye", "ada@loomworks.example")
	dana := putPerson(t, s, "Dana Reyes", "dana@fjordline.example")
	host := putMail(t, s, mailFixture{
		ext: extQuotedHost, ts: "2026-03-04T09:00:00+11:00", tz: "AEDT", offset: mins(660),
		person: ada, container: "T1", subject: "Fence panels",
		messageID: "<c0ffee-5@loomworks.example>",
		from:      "Ada Okoye <ada@loomworks.example>",
		to:        "Dana Reyes <dana@fjordline.example>",
		text:      "Quoting the original below.",
	})
	ts, err := time.Parse(time.RFC3339, "2026-03-01T08:00:00+11:00")
	if err != nil {
		t.Fatalf("bad ts: %v", err)
	}
	id, _, err := s.PutQuoted(corpus.Entry{
		Source: corpus.SourceMail, ExtID: extQuoted, TS: ts, TZ: "AEDT",
		PersonID: dana, Container: "T1", Subject: "Fence panels",
		BodyText: "The gate hinge was ordered.",
	})
	if err != nil {
		t.Fatalf("PutQuoted: %v", err)
	}
	if err := s.Sight(id, host, "quoted", ""); err != nil {
		t.Fatalf("Sight: %v", err)
	}
	return harnessOver(t, s)
}

// htmlPart is the invented html an entry arrives with, shaped like the thing this
// route exists for: a booking confirmation, which is all presentation and layout
// and none of it a paragraph the transcript can keep.
//
// Every refusal the route makes is in here too — a script, an inline handler and a
// javascript: URL — because the point of a fixture is to be the input the route is
// not allowed to pass, and an input that only contains what we keep cannot fail.
const htmlPart = `<!doctype html>
<html><head><meta charset="utf-8"><title>Booking confirmed</title>
<link rel="stylesheet" href="https://assets.example.example/booking.css">
<style>
  body { background: #eef2f7; font-family: Georgia, serif; }
  .card { border: 1px solid #ccd6e0; padding: 12px; }
  @media (max-width: 600px) { .card { padding: 4px; } }
</style></head>
<body bgcolor="#eef2f7">
<div class="card" id="booking" onclick="steal()">
  <h1>Booking confirmed</h1>
  <p>When: <b>Tuesday 3 March, 10:00</b></p>
  <script>steal()</script>
  <a href="javascript:steal()">Reschedule</a>
  <a href="https://example.example/booking/1">Details</a>
</div>
</body></html>`

// htmlServer is the fixture for a reader's second look at a message: one entry that
// arrived with html of its own, and one plain-text neighbour so that "this entry
// has no original" is asked of an entry that is certainly there.
//
// Quoted is what the original must survive — a class, a page-level rule and a media
// query, which is the whole difference between this and the transcript's rendering —
// so the two fixtures are one sender writing twice, and the plain one is the same
// message the transcript would have drawn from the html alone.
func htmlServer(t *testing.T) *harness {
	t.Helper()
	s, err := corpus.Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	ada := putPerson(t, s, "Ada Okoye", "ada@loomworks.example")
	putMail(t, s, mailFixture{
		ext: extHTML, ts: "2026-03-01T09:00:00+11:00", tz: "AEDT", offset: mins(660),
		person: ada, container: "T2", subject: "Booking confirmed",
		messageID: "<c0ffee-6@loomworks.example>",
		from:      "Ada Okoye <ada@loomworks.example>",
		to:        "Bo Halvorsen <bo@fjordline.example>",
		text:      "Booking confirmed: Tuesday 3 March, 10:00.",
		html:      htmlPart,
	})
	putMail(t, s, mailFixture{
		ext: extPlain, ts: "2026-03-01T09:05:00+11:00", tz: "AEDT", offset: mins(660),
		person: ada, container: "T2", subject: "Booking confirmed",
		messageID: "<c0ffee-7@loomworks.example>",
		// A reply, so the two fixtures are one trail: the pane draws the toggle
		// from a chain read, and a fixture of two unrelated chains could not tell
		// a per-entry flag from a per-read one.
		inReplyTo: "<c0ffee-6@loomworks.example>",
		from:      "Ada Okoye <ada@loomworks.example>",
		to:        "Bo Halvorsen <bo@fjordline.example>",
		text:      "Booking confirmed: Tuesday 3 March, 10:00.",
	})
	// The reply edge is resolved after both rows exist, not at insert: a parent is
	// named by Message-ID and the message it names may not have been ingested yet.
	if _, err := s.ResolveParents(); err != nil {
		t.Fatalf("ResolveParents: %v", err)
	}
	return harnessOver(t, s)
}

// harnessOver is the server the fixtures run on, over a corpus they built.
//
// Its switches are the same on every corpus on purpose: a test that added a
// recovered entry should be exercising this wire, not a differently configured
// server — and the two fixtures that need different ones set them on the harness
// the way the slurp tests do.
func harnessOver(t *testing.T, s *corpus.Store) *harness {
	t.Helper()
	// Seeded so the documented-path walk can GET /v1/specs/{name} without a
	// prior save; the round-trip test writes its own.
	specsDir := t.TempDir()
	serve := &server{
		store: s,
		specs: specsDir,
		// Seeded with the saved pages, so a test that writes a snapshot can
		// point the server at it without a separate temp path.
		statusPath: filepath.Join(specsDir, "status.json"),
		specSlots:  make(chan struct{}, specConcurrency),
		// Shortened from the real wait so the 429 path is a fast test.
		slotWait:  10 * time.Millisecond,
		embedWait: 2 * time.Second,
		// The auth flows host the Google redirect on the bound port.
		loginPort: "9876",
		// A deploy stamp, so /v1/version has something to serve: a test that
		// asserted an empty answer would pass just as happily against a handler
		// that never filled the field in. startedAt is a fixed instant, not now(),
		// so the test says nothing about when it ran.
		rev:       "c0ffee1234567890abcdef1234567890abcdef12",
		startedAt: time.Date(2026, 9, 16, 13, 21, 23, 0, time.UTC),
		// Pointed at a port nothing listens on, so mode=semantic exercises the
		// daemon-down path without needing ollama absent from the machine.
		embedder: func() *embed.Ollama {
			return &embed.Ollama{BaseURL: "http://127.0.0.1:1", Name: embed.DefaultModel,
				Dimension: embed.DefaultDim, Client: &http.Client{Timeout: 2 * time.Second}}
		},
	}
	if err := os.WriteFile(filepath.Join(serve.specs, "demo.json"),
		[]byte(`{"title":"demo","messages":[]}`), 0o644); err != nil {
		t.Fatalf("seeding the saved page: %v", err)
	}
	return &harness{server: serve, handler: serve.routes()}
}

func specBody(chains ...string) []byte {
	blob, _ := json.Marshal(specRequest{
		Chains: chains, Title: "Solar install quote", Me: []string{"ada@loomworks.example"},
	})
	return blob
}

func mins(n int) *int { return &n }

type mailFixture struct {
	ext       string
	ts        string // RFC3339, with the offset the sender stated
	tz        string
	offset    *int
	person    int64
	container string
	subject   string
	messageID string
	inReplyTo string
	from      string
	to        string
	text      string
	html      string   // the entry's own text/html part, when it arrived with one
	labels    []string // the mailbox's own labels, as Gmail states them
	gmail     string   // the Gmail message id, when this entry has a mailbox copy
	atts      []corpus.Attachment
}

func putPerson(t *testing.T, s *corpus.Store, name, addr string) int64 {
	t.Helper()
	id, err := corpus.Resolve(s, "email", addr, name)
	if err != nil {
		t.Fatalf("resolving %s: %v", addr, err)
	}
	return id
}

// personOf is the person the fixture's address belongs to, for a test that has to
// name the same human the corpus resolved those messages to — the reader is
// stored as a person id, and an id picked out of the air would be the wrong one.
func personOf(t *testing.T, h *harness, addr string) int64 {
	t.Helper()
	id, err := corpus.PersonByIdentity(h.store, corpus.KindEmail, addr)
	if err != nil {
		t.Fatalf("PersonByIdentity(%s): %v", addr, err)
	}
	return id
}

func putMail(t *testing.T, s *corpus.Store, m mailFixture) int64 {
	t.Helper()
	ts, err := time.Parse(time.RFC3339, m.ts)
	if err != nil {
		t.Fatalf("bad ts %q: %v", m.ts, err)
	}
	res, err := s.Put(corpus.Entry{
		Source: corpus.SourceMail, ExtID: m.ext, TS: ts, TZ: m.tz, TZOffset: m.offset,
		PersonID: m.person, Container: m.container, Subject: m.subject,
		ParentRef: m.inReplyTo, BodyText: m.text, BodyHTML: m.html,
	}, &corpus.Mail{
		GmailID: m.gmail, MessageID: m.messageID, InReplyTo: m.inReplyTo,
		From: m.from, To: m.to, Labels: m.labels,
	}, m.atts)
	if err != nil {
		t.Fatalf("Put %s: %v", m.ext, err)
	}
	for role, header := range map[string]string{"from": m.from, "to": m.to} {
		if header == "" {
			continue
		}
		if _, err := corpus.RecordHeader(s, res.ID, role, header); err != nil {
			t.Fatalf("recording %s of %s: %v", role, m.ext, err)
		}
	}
	return res.ID
}
