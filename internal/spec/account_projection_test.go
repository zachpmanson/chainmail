package spec

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/zachpmanson/chainmail/internal/corpus"
	"github.com/zachpmanson/chainmail/internal/mailingest"
)

func TestGenerateAccountProjectsMixedHostQuoteAndHTML(t *testing.T) {
	s := open(t)
	for _, id := range []string{"A", "B"} {
		if err := s.PutGmailAccount(corpus.GmailAccount{ID: id, DisplayName: id}); err != nil {
			t.Fatal(err)
		}
	}
	safe := "The selected account has only these quoted words about the synthetic timetable and the detailed review before final handover with the complete checklist for the team and the agreed demonstration of the final synthetic deliverable."
	a := mailingest.Message{Envelope: mailingest.Envelope{ID: "A-host", MessageID: "<a-host@example.test>", From: "A Host <a-host@example.test>", Date: "Sat, 07 Nov 2026 10:00:00 +0000", Subject: "A subject"}, Body: "A host\n\nOn Fri, Nov 6, 2026 at 9:27 PM Quoted <quoted@example.test> wrote:\n> " + safe, BodyHTML: "<p>A host</p><blockquote class=\"gmail_quote\"><p><b>" + safe + "</b></p></blockquote>"}
	b := mailingest.Message{Envelope: mailingest.Envelope{ID: "B-private-host", MessageID: "<b-private@example.test>", From: "B Host <b-private@example.test>", Date: a.Date, Subject: "B PRIVATE SUBJECT"}, Body: "B host\n\nFrom: B Enriched <quoted@example.test>\nDate: Fri, 6 Nov 2026 21:27:00 +0000\nTo: B Recipient <b-recipient@example.test>\nSubject: B PRIVATE SUBJECT\n\n" + safe + " B PRIVATE DETAIL", BodyHTML: "<p>B host</p><blockquote class=\"gmail_quote\"><p>" + safe + " B PRIVATE DETAIL</p></blockquote>"}
	for _, v := range []struct {
		account string
		m       mailingest.Message
	}{{"A", a}, {"B", b}} {
		if _, err := mailingest.PutForAccount(s, v.account, v.m); err != nil {
			t.Fatal(err)
		}
	}
	ext := corpus.RecoverQuotes(a.Body).Distinct[0].Key
	sp, err := Generate(s, Options{Title: "Synthetic selection", AccountID: "A", ExtIDs: []string{ext}})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(sp)
	for _, secret := range []string{"B PRIVATE", "b-private", "B-private", "b-recipient", "B Enriched"} {
		if strings.Contains(string(raw), secret) {
			t.Fatalf("export leaked %s: %s", secret, raw)
		}
	}
	var quote *Entry
	for n := range sp.Messages {
		if sp.Messages[n].ExtID == ext {
			quote = &sp.Messages[n]
		}
	}
	if quote == nil || !strings.Contains(quote.Body, safe) || quote.Sender != "Quoted" || quote.To != "" || quote.GmailID != "" || !strings.Contains(quote.Source, "msg A-host") {
		t.Fatalf("selected quote/provenance lost: %+v", quote)
	}
	if !strings.Contains(quote.Body, "<b>") {
		t.Fatalf("selected host HTML not recovered: %s", quote.Body)
	}
	global, err := Generate(s, Options{ExtIDs: []string{ext}})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(global)
	if !strings.Contains(string(raw), "B PRIVATE DETAIL") {
		t.Fatal("unscoped owner aggregate changed")
	}
}

func TestGenerateAccountSelectsCopyAndConservativeUnavailable(t *testing.T) {
	s := open(t)
	if err := s.PutGmailAccount(corpus.GmailAccount{ID: "A", DisplayName: "A", Email: "selected@example.test"}); err != nil {
		t.Fatal(err)
	}
	id := put(t, s, msg{ext: "mail:shared", ts: "2026-11-07T10:00:00Z", gmail: "copy-legacy", container: "legacy-thread-not-proven-for-A", from: "Sender <sender@example.test>", text: "shared body"})
	if err := s.PutGmailCopy(corpus.GmailCopy{AccountID: "A", GmailID: "copy-A", EntryID: id}); err != nil {
		t.Fatal(err)
	}
	q, _, err := s.PutQuoted(corpus.Entry{Source: "mail", ExtID: "quote:unprovable", TS: time.Unix(1, 0), Subject: "B PRIVATE SUBJECT", BodyText: "B PRIVATE DETAIL"})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Sight(q, id, "quoted", "selected A host"); err != nil {
		t.Fatal(err)
	}
	sp, err := Generate(s, Options{Title: "Synthetic selection", AccountID: "A", ExtIDs: []string{"mail:shared", "quote:unprovable"}})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(sp)
	if strings.Contains(string(raw), "copy-legacy") || strings.Contains(string(raw), "legacy-thread-not-proven-for-A") || strings.Contains(string(raw), "B PRIVATE") {
		t.Fatalf("unsafe projection %s", raw)
	}
	for _, e := range sp.Messages {
		if e.ExtID == "mail:shared" && (e.GmailID != "copy-A" || e.Source != "msg copy-A" || e.GmailAccountEmail != "selected@example.test") {
			t.Fatalf("wrong copy: %+v", e)
		}
		if e.ExtID == "quote:unprovable" && !strings.Contains(e.Body, "Content unavailable") {
			t.Fatalf("unsafe fallback %+v", e)
		}
	}
}

func TestScopedLocalSeedExpansionRefusesOverBudget(t *testing.T) {
	s := open(t)
	a := put(t, s, msg{ext: "mail:<root@example.test>", messageID: "<root@example.test>", ts: "2026-11-07T10:00:00Z", gmail: "root", text: "root"})
	put(t, s, msg{ext: "mail:<child@example.test>", messageID: "<child@example.test>", inReplyTo: "<root@example.test>", ts: "2026-11-07T11:00:00Z", gmail: "child", text: "child"})
	if _, err := s.ResolveParents(); err != nil {
		t.Fatal(err)
	}
	if _, err := Generate(s, Options{AccountID: "legacy", EntryIDs: []int64{a}, Title: "Bounded", EntryLimit: 1}); err == nil || !strings.Contains(err.Error(), "exceeds 1 entries") {
		t.Fatalf("unbounded closure: %v", err)
	}
}
