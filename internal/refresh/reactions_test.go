package refresh

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/zachpmanson/chainmail/internal/corpus"
	"github.com/zachpmanson/chainmail/internal/mailingest"
	"github.com/zachpmanson/chainmail/internal/spec"
)

type noReactionRefreshMailbox struct{}

func (noReactionRefreshMailbox) Search(string, int, string) ([]mailingest.Envelope, mailingest.Page, error) {
	panic("scoped refresh searched provider")
}
func (noReactionRefreshMailbox) Read(string) (mailingest.Message, error) {
	panic("scoped refresh read provider")
}
func (noReactionRefreshMailbox) Thread(string) (mailingest.ThreadResult, error) {
	panic("scoped refresh read provider thread")
}

func TestSavedReactionPagePreservesAccountScopeAndQuotedVisibility(t *testing.T) {
	s, err := corpus.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.PutGmailAccount(corpus.GmailAccount{ID: "other", DisplayName: "Other"}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile("../../testdata/gmail-reaction-synthetic.json")
	if err != nil {
		t.Fatal(err)
	}
	var m mailingest.Message
	if err = json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	target := mailingest.Message{Envelope: mailingest.Envelope{ID: "synthetic-target", MessageID: "<synthetic-target@example.test>", ThreadID: "synthetic-thread", From: "Alex Vale <alex@example.test>", Date: "Fri, 06 Nov 2026 21:27:00 +0000", Subject: "Private synthetic target"}, Body: "other-only-private-target-secret"}
	targetResult, err := mailingest.PutForAccount(s, "other", target)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = mailingest.PutForAccount(s, "legacy", m); err != nil {
		t.Fatal(err)
	}
	m.ID = "other-source"
	if _, err = mailingest.PutForAccount(s, "other", m); err != nil {
		t.Fatal(err)
	}
	quote, _, err := s.PutQuoted(corpus.Entry{Source: corpus.SourceMail, ExtID: "quote:account-scoped", TS: time.Unix(1793990000, 0), Container: "synthetic-thread", Subject: "Synthetic quoted history", BodyText: "other-only-quoted-content"})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Sight(quote, targetResult.ID, "quoted", "independently sighted in selected-account target"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ResolveParents(); err != nil {
		t.Fatal(err)
	}
	for _, account := range []string{"legacy", "other"} {
		t.Run(account, func(t *testing.T) {
			prev, err := spec.Generate(s, spec.Options{AccountID: account, Containers: []string{"synthetic-thread"}, Title: "Scoped synthetic reactions"})
			if err != nil {
				t.Fatal(err)
			}
			if prev.RunParams == nil || prev.RunParams.AccountID != account {
				t.Fatal("scope not saved")
			}
			rep, next, err := Run(s, noReactionRefreshMailbox{}, prev, Options{})
			if err != nil {
				t.Fatal(err)
			}
			if next.RunParams == nil || next.RunParams.AccountID != account || rep.EntriesAfter != len(next.Messages) {
				t.Fatal("scope not preserved on refresh")
			}
			encoded, _ := json.Marshal(next)
			if account == "legacy" && (strings.Contains(string(encoded), "other-only-private-target-secret") || strings.Contains(string(encoded), "other-only-quoted-content") || len(next.Messages) != 1) {
				t.Fatalf("refresh leaked account content: %s", encoded)
			}
			if account == "other" {
				foundQuote, foundTarget := false, false
				for _, entry := range next.Messages {
					foundQuote = foundQuote || entry.ExtID == "quote:account-scoped"
					if entry.ExtID == "mail:<synthetic-target@example.test>" {
						foundTarget = len(entry.Reactions) == 1
					}
				}
				if !foundQuote || !foundTarget || len(next.Messages) != 2 {
					t.Fatalf("accessible quote/reaction lost: %s", encoded)
				}
			}
			if _, _, err = Run(s, noReactionRefreshMailbox{}, prev, Options{Fetch: true}); err == nil {
				t.Fatal("scoped provider fetch did not refuse")
			}
		})
	}
}
