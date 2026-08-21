package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
)

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
	var rdr *bytes.Reader
	if body == nil {
		rdr = bytes.NewReader(nil)
	} else {
		rdr = bytes.NewReader(body)
	}
	req := httptest.NewRequest(method, path, rdr)
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
		atts:      []corpus.Attachment{{Name: "shed.csv", Mime: "text/csv", Size: 512}},
	})
	b := putMail(t, s, mailFixture{
		ext: extBo2, ts: "2026-03-02T23:40:00+01:00", tz: "+0100",
		person: bo, container: "T1", subject: "Solar install quote",
		messageID: "<c0ffee-2@fjordline.example>", inReplyTo: "<c0ffee-1@loomworks.example>",
		from: "Bo Halvorsen <bo@fjordline.example>",
		to:   "Ada Okoye <ada@loomworks.example>",
		text: "Quote attached. The install needs two days of roof access.",
	})
	c := putMail(t, s, mailFixture{
		ext: extAda3, ts: "2026-03-03T10:00:00+11:00", tz: "AEDT",
		person: ada, container: "T1", subject: "Solar install quote: dates",
		messageID: "<c0ffee-3@loomworks.example>", inReplyTo: "<c0ffee-2@fjordline.example>",
		from: "Ada Okoye <ada@loomworks.example>",
		to:   "Bo Halvorsen <bo@fjordline.example>",
		text: "Roof access is fine from the 14th.",
	})
	putMail(t, s, mailFixture{
		ext: extOther, ts: "2026-04-01T08:00:00+11:00", tz: "AEDT",
		person: ada, container: "T9", subject: "Fence panels",
		messageID: "<c0ffee-9@loomworks.example>",
		from:      "Ada Okoye <ada@loomworks.example>",
		to:        "Bo Halvorsen <bo@fjordline.example>",
		text:      "Unrelated: the fence panels arrived.",
	})
	for _, id := range []int64{a, b, c} {
		if err := s.Sight(id, 0, "direct", ""); err != nil {
			t.Fatalf("Sight: %v", err)
		}
	}
	if _, err := s.ResolveParents(); err != nil {
		t.Fatalf("ResolveParents: %v", err)
	}

	srv := &server{
		store:     s,
		specSlots: make(chan struct{}, specConcurrency),
		// Shortened from the real wait so the 429 path is a fast test.
		slotWait:  10 * time.Millisecond,
		embedWait: 2 * time.Second,
		// Pointed at a port nothing listens on, so mode=semantic exercises the
		// daemon-down path without needing ollama absent from the machine.
		embedder: func() *embed.Ollama {
			return &embed.Ollama{BaseURL: "http://127.0.0.1:1", Name: embed.DefaultModel,
				Dimension: embed.DefaultDim, Client: &http.Client{Timeout: 2 * time.Second}}
		},
	}
	return &harness{server: srv, handler: srv.routes()}
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

func putMail(t *testing.T, s *corpus.Store, m mailFixture) int64 {
	t.Helper()
	ts, err := time.Parse(time.RFC3339, m.ts)
	if err != nil {
		t.Fatalf("bad ts %q: %v", m.ts, err)
	}
	res, err := s.Put(corpus.Entry{
		Source: corpus.SourceMail, ExtID: m.ext, TS: ts, TZ: m.tz, TZOffset: m.offset,
		PersonID: m.person, Container: m.container, Subject: m.subject,
		ParentRef: m.inReplyTo, BodyText: m.text,
	}, &corpus.Mail{
		MessageID: m.messageID, InReplyTo: m.inReplyTo, From: m.from, To: m.to,
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
