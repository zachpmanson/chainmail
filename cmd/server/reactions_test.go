package main

import (
	"encoding/json"
	"net/url"
	"os"
	"strings"
	"testing"

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
