package main

import (
	"strings"
	"testing"

	"github.com/zachpmanson/chainmail/internal/corpus"
)

func TestAccountForEntriesRequiresExplicitChoiceWhenCopiesSpanAccounts(t *testing.T) {
	trails := [][]corpus.ChainEntry{{
		{Copies: []corpus.GmailCopy{{AccountID: "work", GmailID: "same-id"}, {AccountID: "personal", GmailID: "same-id"}}},
	}}
	if _, err := accountForEntries(trails, ""); err == nil || !strings.Contains(err.Error(), "specify accountId") {
		t.Fatalf("ambiguous account selection error = %v, want explicit accountId refusal", err)
	}
	got, err := accountForEntries(trails, "personal")
	if err != nil || got != "personal" {
		t.Fatalf("explicit account selection = %q, %v; want personal", got, err)
	}
	if _, err := accountForEntries(trails, "missing"); err == nil || !strings.Contains(err.Error(), "no copy") {
		t.Fatalf("account without a selected-chain copy error = %v, want refusal", err)
	}
}

func TestAccountForEntriesInfersOnlyAnUnambiguousAccount(t *testing.T) {
	trails := [][]corpus.ChainEntry{{
		{Copies: []corpus.GmailCopy{{AccountID: "work", GmailID: "w-1"}}},
		{Copies: []corpus.GmailCopy{{AccountID: "work", GmailID: "w-2"}}},
	}}
	got, err := accountForEntries(trails, "")
	if err != nil || got != "work" {
		t.Fatalf("single-account selection = %q, %v; want work", got, err)
	}
}
