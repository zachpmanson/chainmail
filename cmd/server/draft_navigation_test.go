package main

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/zachpmanson/chainmail/internal/corpus"
	"github.com/zachpmanson/chainmail/internal/gmailclient"
)

// Simulate provider-owned final evidence independently of request intent.
type navigationMailbox struct {
	*draftFake
	evidence gmailclient.ReplyHeaders
	after    func()
}

func (f *navigationMailbox) SaveDraft(d gmailclient.Draft, parent string) (gmailclient.Draft, error) {
	d, err := f.draftFake.SaveDraft(d, parent)
	d.ReplyHeaders = f.evidence
	if f.after != nil {
		f.after()
	}
	return d, err
}
func (f *navigationMailbox) ListDrafts() ([]gmailclient.Draft, error) {
	d, e := f.draftFake.ListDrafts()
	if f.after != nil {
		f.after()
	}
	return d, e
}
func (f *navigationMailbox) GetDraft(id string) (gmailclient.Draft, error) {
	d, e := f.draftFake.GetDraft(id)
	if f.after != nil {
		f.after()
	}
	return d, e
}

func assertNavigationSchema(t *testing.T, body []byte) {
	t.Helper()
	doc := loadAPI(t)
	response := doc.schemas["DraftResponse"].(map[string]any)
	merged := map[string]any{"type": "object", "properties": map[string]any{}}
	var required []any
	for _, part := range response["allOf"].([]any) {
		schema := part.(map[string]any)
		if ref, ok := schema["$ref"].(string); ok {
			if ref != "#/components/schemas/Draft" {
				t.Fatal(ref)
			}
			schema = doc.schemas["Draft"].(map[string]any)
		}
		for k, v := range schema["properties"].(map[string]any) {
			merged["properties"].(map[string]any)[k] = v
		}
		required = append(required, schema["required"].([]any)...)
	}
	merged["required"] = required
	var value map[string]any
	if err := json.Unmarshal(body, &value); err != nil {
		t.Fatal(err)
	}
	if problems := doc.violations(merged, value, "DraftResponse"); len(problems) > 0 {
		t.Fatal(problems)
	}
	for _, field := range []string{"kind", "resolution"} {
		copy := map[string]any{}
		for k, v := range value {
			if k != field {
				copy[k] = v
			}
		}
		if len(doc.violations(merged, copy, "DraftResponse")) == 0 {
			t.Fatalf("schema does not require %s", field)
		}
	}
}

func TestDraftNavigationListDetailSaveParity(t *testing.T) {
	for _, tc := range []struct {
		name         string
		headers      gmailclient.ReplyHeaders
		kind, status string
		duplicate    bool
	}{
		{"compose container", gmailclient.ReplyHeaders{}, "compose", "compose", false},
		{"exact nonroot", gmailclient.ReplyHeaders{InReplyTo: []string{"<parent@e.test>"}}, "reply", "resolved", false},
		{"missing", gmailclient.ReplyHeaders{InReplyTo: []string{"<missing@e.test>"}}, "unresolved", "missing-parent", false},
		{"malformed", gmailclient.ReplyHeaders{InReplyTo: []string{"garbage"}}, "unresolved", "malformed-parent", false},
		{"references only", gmailclient.ReplyHeaders{References: []string{"<parent@e.test>"}}, "unresolved", "missing-explicit-parent", false},
		{"ambiguous", gmailclient.ReplyHeaders{InReplyTo: []string{"<parent@e.test>"}}, "unresolved", "ambiguous-parent", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, f := draftsServer(t)
			for _, entry := range []struct{ ext, parent, id string }{{"mail:nav-root", "", "<root@e.test>"}, {"mail:nav-parent", "<root@e.test>", "<parent@e.test>"}} {
				_, err := h.store.PutForAccount("legacy", corpus.Entry{Source: corpus.SourceMail, ExtID: entry.ext, Kind: "message", ParentRef: entry.parent, TS: time.Unix(1, 0), BodyText: entry.ext}, &corpus.Mail{GmailID: entry.ext, MessageID: entry.id}, nil)
				if err != nil {
					t.Fatal(err)
				}
			}
			if _, err := h.store.ResolveParents(); err != nil {
				t.Fatal(err)
			}
			if tc.duplicate {
				_, err := h.store.PutForAccount("legacy", corpus.Entry{Source: corpus.SourceMail, ExtID: "mail:nav-duplicate", Kind: "message", TS: time.Unix(1, 0), BodyText: "different"}, &corpus.Mail{GmailID: "duplicate", MessageID: "<parent@e.test>"}, nil)
				if err != nil {
					t.Fatal(err)
				}
			}
			d := f.drafts["d1"]
			d.ReplyHeaders = tc.headers
			f.drafts["d1"] = d
			mb := &navigationMailbox{draftFake: f, evidence: tc.headers}
			h.openMailbox = func() (mailbox, error) { return mb, nil }
			list := h.do(t, "GET", "/v1/drafts", nil)
			if list.status != 200 {
				t.Fatal(string(list.body))
			}
			listed := decode[[]draftResponse](t, list)
			if len(listed) != 1 {
				t.Fatal("draft hidden")
			}
			want := listed[0]
			if want.Kind != tc.kind || want.Resolution != tc.status {
				t.Fatalf("navigation %+v", want)
			}
			if tc.kind == "reply" {
				if want.ReplyToEntry != "mail:nav-parent" || want.RootID != "mail:nav-root" {
					t.Fatalf("handles %+v", want)
				}
			} else if want.ReplyToEntry != "" || want.RootID != "" {
				t.Fatal("unresolved handles")
			}
			for _, method := range []string{"GET", "POST", "PUT"} {
				path := "/v1/drafts/d1"
				var body []byte
				if method != "GET" {
					body = []byte(`{"to":[],"subject":"Saved","body":"Hello"}`)
				}
				if method == "POST" {
					path = "/v1/drafts"
				}
				res := h.do(t, method, path, body)
				if res.status != 200 {
					t.Fatalf("%s %d %s", method, res.status, res.body)
				}
				assertNavigationSchema(t, res.body)
				got := decode[draftResponse](t, res)
				if got.Kind != want.Kind || got.Resolution != want.Resolution || got.ReplyToEntry != want.ReplyToEntry || got.RootID != want.RootID {
					t.Fatalf("parity %s %+v vs %+v", method, got, want)
				}
			}
		})
	}
}

func TestDraftNavigationLookupFailureRetainsProviderSuccess(t *testing.T) {
	for _, method := range []string{"GET-list", "GET", "POST", "PUT"} {
		t.Run(method, func(t *testing.T) {
			h, f := draftsServer(t)
			evidence := gmailclient.ReplyHeaders{InReplyTo: []string{"<private-parent@e.test>"}}
			d := f.drafts["d1"]
			d.ReplyHeaders = evidence
			f.drafts["d1"] = d
			mb := &navigationMailbox{draftFake: f, evidence: evidence, after: func() { _ = h.store.Close() }}
			h.openMailbox = func() (mailbox, error) { return mb, nil }
			path := "/v1/drafts/d1"
			verb := method
			var body []byte
			if method == "POST" {
				path = "/v1/drafts"
			}
			if method == "POST" || method == "PUT" {
				body = []byte(`{"to":[],"subject":"Saved","body":"Hello"}`)
			}
			if method == "GET-list" {
				path = "/v1/drafts"
				verb = "GET"
			}
			res := h.do(t, verb, path, body)
			if res.status != 200 {
				t.Fatalf("provider success lost: %d %s", res.status, res.body)
			}
			var got draftResponse
			if method == "GET-list" {
				list := decode[[]draftResponse](t, res)
				if len(list) != 1 {
					t.Fatal("hidden")
				}
				got = list[0]
			} else {
				got = decode[draftResponse](t, res)
				assertNavigationSchema(t, res.body)
			}
			if got.Kind != "unresolved" || got.Resolution != "lookup-unavailable" || got.RootID != "" || got.ReplyToEntry != "" {
				t.Fatalf("got %+v", got)
			}
			if !reflect.DeepEqual(got.ReplyHeaders, gmailclient.ReplyHeaders{}) {
				t.Fatal("private evidence leaked")
			}
			if method == "POST" || method == "PUT" {
				if len(f.saved) != 1 {
					t.Fatalf("writes=%d", len(f.saved))
				}
			}
		})
	}
}

func TestDraftNavigationFieldsRemainResponseOnly(t *testing.T) {
	for _, field := range []string{`"kind":"reply"`, `"resolution":"resolved"`, `"rootId":"mail:root"`} {
		h, f := draftsServer(t)
		res := h.do(t, "POST", "/v1/drafts", []byte(`{"to":[],"subject":"","body":"",`+field+`}`))
		if res.status != 400 || len(f.saved) != 0 {
			t.Fatalf("response field accepted %s: %d", field, res.status)
		}
	}
}
