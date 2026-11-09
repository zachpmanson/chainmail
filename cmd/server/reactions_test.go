package main

import (
	"encoding/json"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/zachpmanson/chainmail/internal/corpus"
	"github.com/zachpmanson/chainmail/internal/mailingest"
)

func serverReaction(t *testing.T) mailingest.Message {
	t.Helper()
	raw, err := os.ReadFile("../../testdata/gmail-reaction-synthetic.json")
	if err != nil {
		t.Fatal(err)
	}
	var m mailingest.Message
	if err = json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	return m
}
func TestReactionAPISharedChipsSourceInspectionAndAccountBoundary(t *testing.T) {
	h := testServer(t)
	m := serverReaction(t)
	target := mailingest.Message{Envelope: mailingest.Envelope{ID: "synthetic-target", MessageID: "<synthetic-target@example.test>", ThreadID: "synthetic-thread", From: "Alex Vale <alex@example.test>", Date: "Fri, 06 Nov 2026 21:27:00 +0000", Subject: "Private synthetic target"}, Body: "private-target-prose-only"}
	if err := h.store.PutGmailAccount(corpus.GmailAccount{ID: "other", DisplayName: "Other"}); err != nil {
		t.Fatal(err)
	}
	if _, err := mailingest.PutForAccount(h.store, "other", target); err != nil {
		t.Fatal(err)
	}
	if _, err := mailingest.PutForAccount(h.store, "legacy", m); err != nil {
		t.Fatal(err)
	}
	if _, err := h.store.ResolveParents(); err != nil {
		t.Fatal(err)
	}
	sourcePath := "/v1/entries/" + url.PathEscape("mail:"+m.MessageID)
	res := h.do(t, "GET", sourcePath+"?accountId=legacy", nil)
	if res.status != 200 {
		t.Fatalf("source lookup %d %s", res.status, res.body)
	}
	var source corpusEntry
	if err := json.Unmarshal(res.body, &source); err != nil {
		t.Fatal(err)
	}
	if len(source.ReactionEvidence) != 1 || source.Body != m.Body || source.ReactionStatus != "unresolved-missing-ambiguous-inaccessible-or-self" {
		t.Fatalf("source inspection lost: %+v", source)
	}
	if res = h.do(t, "GET", "/v1/chains/"+url.PathEscape("mail:"+m.MessageID)+"?accountId=legacy", nil); res.status != 200 || strings.Contains(string(res.body), "private-target-prose-only") || strings.Contains(string(res.body), "Private synthetic target") {
		t.Fatalf("inaccessible target exposed: %d %s", res.status, res.body)
	}
	if res = h.do(t, "GET", "/v1/entries/"+url.PathEscape("mail:"+target.MessageID)+"?accountId=legacy", nil); res.status != 404 {
		t.Fatalf("target direct access crossed account: %d %s", res.status, res.body)
	}
	// Same source copy in the target mailbox enables exactly one logical event.
	m.ID = "other-source"
	if _, err := mailingest.PutForAccount(h.store, "other", m); err != nil {
		t.Fatal(err)
	}
	res = h.do(t, "GET", "/v1/chains/"+url.PathEscape("mail:"+target.MessageID)+"?accountId=other", nil)
	var chain chainResponse
	if err := json.Unmarshal(res.body, &chain); err != nil {
		t.Fatal(err)
	}
	if res.status != 200 || len(chain.Entries) != 1 || len(chain.Entries[0].Reactions) != 1 || chain.Entries[0].Reactions[0].Emoji != "❤️" || len(chain.Entries[0].Reactions[0].Events) != 1 {
		t.Fatalf("resolved chain: %d %s", res.status, res.body)
	}
	event := chain.Entries[0].Reactions[0].Events[0]
	if event.SourceExtID != "mail:"+m.MessageID || event.ClaimedFrom != m.From || event.Body != m.Body || event.Date != m.Date {
		t.Fatalf("provenance changed: %+v", event)
	}
	res = h.do(t, "GET", sourcePath+"?accountId=other", nil)
	if res.status != 200 || !strings.Contains(string(res.body), `"reactionStatus":"resolved"`) {
		t.Fatalf("suppressed source no longer inspectable: %d %s", res.status, res.body)
	}
	res = h.do(t, "GET", "/v1/chains/"+url.PathEscape("mail:"+target.MessageID)+"?accountId=legacy", nil)
	if strings.Contains(string(res.body), `"reactions"`) || strings.Contains(string(res.body), "private-target-prose-only") {
		t.Fatalf("other account's reaction/target leaked: %d %s", res.status, res.body)
	}
}

func TestAccountProjectionMixedHostsAndEnrichedQuote(t *testing.T) {
	h := testServer(t)
	for _, id := range []string{"A", "B"} {
		if err := h.store.PutGmailAccount(corpus.GmailAccount{ID: id, DisplayName: id}); err != nil {
			t.Fatal(err)
		}
	}
	safe := "A quoted words describe the launch timetable and the required review of the synthetic project before the final handover."
	a := mailingest.Message{Envelope: mailingest.Envelope{ID: "a-host", MessageID: "<a-host@example.test>", From: "A Host <a-host@example.test>", Date: "Sat, 07 Nov 2026 10:00:00 +0000", Subject: "A host subject"}, Body: "A host words\n\nOn Fri, Nov 6, 2026 at 9:27 PM Quoted <quoted@example.test> wrote:\n> " + safe, BodyHTML: "<p>A host words</p><blockquote><p><b>" + safe + "</b></p></blockquote>"}
	b := mailingest.Message{Envelope: mailingest.Envelope{ID: "b-host", MessageID: "<b-private-host@example.test>", From: "B Host <b-private@example.test>", Date: a.Date, Subject: "B host subject"}, Body: "B host words\n\nFrom: B Enriched Name <quoted@example.test>\nDate: Fri, 6 Nov 2026 21:27:00 +0000\nTo: B Recipient <b-recipient@example.test>\nSubject: B PRIVATE SUBJECT\n\n" + safe + " B PRIVATE DETAIL", BodyHTML: "<p>B host words</p><blockquote><p>" + safe + " B PRIVATE DETAIL</p></blockquote>"}
	for account, m := range map[string]mailingest.Message{"A": a, "B": b} {
		if _, err := mailingest.PutForAccount(h.store, account, m); err != nil {
			t.Fatal(err)
		}
	}
	ext := corpus.RecoverQuotes(a.Body).Distinct[0].Key
	for _, route := range []string{"/v1/entries/", "/v1/chains/"} {
		res := h.do(t, "GET", route+url.PathEscape(ext)+"?accountId=A", nil)
		if res.status != 200 {
			t.Fatalf("%s: %d %s", route, res.status, res.body)
		}
		body := string(res.body)
		for _, secret := range []string{"B PRIVATE", "b-private", "b-recipient", "B Enriched", "b-host"} {
			if strings.Contains(body, secret) {
				t.Fatalf("A leaked %s: %s", secret, body)
			}
		}
		if !strings.Contains(body, safe) || !strings.Contains(body, "a-host") {
			t.Fatalf("A quote/provenance missing: %s", body)
		}
	}
	// A host sighting alone admits the card, not a global body with no matching block.
	id, _, err := h.store.PutQuoted(corpus.Entry{Source: "mail", ExtID: "quote:unprovable", TS: time.Unix(1, 0), BodyText: "B PRIVATE DETAIL", Subject: "B PRIVATE SUBJECT"})
	if err != nil {
		t.Fatal(err)
	}
	host, err := h.store.Show("mail:" + a.MessageID)
	if err != nil {
		t.Fatal(err)
	}
	if err = h.store.Sight(id, host.ID, "quoted", "A evidence without a matching block"); err != nil {
		t.Fatal(err)
	}
	res := h.do(t, "GET", "/v1/entries/quote:unprovable?accountId=A", nil)
	var fallback corpusEntry
	if err = json.Unmarshal(res.body, &fallback); err != nil {
		t.Fatal(err)
	}
	chainResult := h.do(t, "GET", "/v1/chains/quote:unprovable?accountId=A", nil)
	var unavailableChain chainResponse
	if err = json.Unmarshal(chainResult.body, &unavailableChain); err != nil {
		t.Fatal(err)
	}
	if unavailableChain.Summary.First != stamp(host.TS) || unavailableChain.Summary.Last != stamp(host.TS) {
		t.Fatalf("global date leaked in summary: %s", chainResult.body)
	}
	stored, err := h.store.Show("quote:unprovable")
	if err != nil {
		t.Fatal(err)
	}
	if fallback.TS != stamp(host.TS) || stored.TS.Unix() != 1 || fallback.TZ != "" {
		t.Fatalf("unsafe or persisted placement: %+v stored %+v", fallback, stored)
	}
	if res.status != 200 || strings.Contains(string(res.body), "B PRIVATE") || !strings.Contains(string(res.body), "Original date unavailable; placement uses an accessible host") {
		t.Fatalf("unsafe fallback: %d %s", res.status, res.body)
	}
}

func TestAccountProjectionUsesSelectedMailboxPermalink(t *testing.T) {
	h := testServer(t)
	if err := h.store.PutGmailAccount(corpus.GmailAccount{ID: "A", DisplayName: "A", Email: "selected+mail@example.test"}); err != nil {
		t.Fatal(err)
	}
	m := mailingest.Message{Envelope: mailingest.Envelope{ID: "copy-legacy", ThreadID: "legacy-thread-not-proven-for-A", MessageID: "<shared-copy@example.test>", From: "Sender <sender@example.test>", Date: "Sat, 07 Nov 2026 10:00:00 +0000"}, Body: "shared content"}
	r, err := mailingest.PutForAccount(h.store, "legacy", m)
	if err != nil {
		t.Fatal(err)
	}
	if err = h.store.PutGmailCopy(corpus.GmailCopy{AccountID: "A", GmailID: "copy-A", EntryID: r.ID}); err != nil {
		t.Fatal(err)
	}
	res := h.do(t, "GET", "/v1/entries/"+url.PathEscape("mail:"+m.MessageID)+"?accountId=A", nil)
	if res.status != 200 || strings.Contains(string(res.body), "copy-legacy") || strings.Contains(string(res.body), "legacy-thread-not-proven-for-A") || (!strings.Contains(string(res.body), "#all/copy-A") || !strings.Contains(string(res.body), "authuser=selected%2Bmail%40example.test")) {
		t.Fatalf("wrong copy metadata: %d %s", res.status, res.body)
	}
}
