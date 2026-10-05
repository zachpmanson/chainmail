package corpus

import (
	"reflect"
	"testing"
	"time"
)

func TestGmailAccountMigrationAndMailboxCopies(t *testing.T) {
	s := open(t)
	accounts, err := s.GmailAccounts()
	if err != nil {
		t.Fatal(err)
	}
	if len(accounts) != 1 || accounts[0].ID != "legacy" || accounts[0].Email != "" {
		t.Fatalf("migrated accounts = %+v, want the unidentified legacy account", accounts)
	}

	for _, account := range []GmailAccount{
		{ID: "work", Email: "work@example.test", DisplayName: "Work"},
		{ID: "personal", Email: "personal@example.test", DisplayName: "Personal"},
	} {
		if err := s.PutGmailAccount(account); err != nil {
			t.Fatal(err)
		}
	}
	entry, err := s.PutForAccount("work", Entry{
		Source: SourceMail, ExtID: "mail:<shared@example.test>", Kind: "message",
		TS: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC), BodyText: "shared mail",
	}, &Mail{GmailID: "same-local-id", MessageID: "<shared@example.test>",
		Labels: []string{"INBOX", "Project, Alpha"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.PutGmailCopy(GmailCopy{
		AccountID: "personal", GmailID: "same-local-id", EntryID: entry.ID, Labels: []string{"SENT"},
	}); err != nil {
		t.Fatal(err)
	}

	copies, err := s.GmailCopies(entry.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := []GmailCopy{
		{AccountID: "personal", GmailID: "same-local-id", EntryID: entry.ID, Labels: []string{"SENT"}},
		{AccountID: "work", GmailID: "same-local-id", EntryID: entry.ID, Labels: []string{"INBOX", "Project, Alpha"}},
	}
	if !reflect.DeepEqual(copies, want) {
		t.Fatalf("copies = %+v, want %+v", copies, want)
	}
}

func TestGmailAccountsRejectDuplicateEmailAndInvalidCopy(t *testing.T) {
	s := open(t)
	if err := s.PutGmailAccount(GmailAccount{ID: "one", Email: "same@example.test", DisplayName: "One"}); err != nil {
		t.Fatal(err)
	}
	if err := s.PutGmailAccount(GmailAccount{ID: "two", Email: "same@example.test", DisplayName: "Two"}); err == nil {
		t.Fatal("duplicate account email was accepted")
	}
	if err := s.PutGmailCopy(GmailCopy{AccountID: "missing", GmailID: "g1", EntryID: 1}); err == nil {
		t.Fatal("copy referencing an unknown account was accepted")
	}
	if err := s.PutGmailAccount(GmailAccount{ID: "", DisplayName: "No ID"}); err == nil {
		t.Fatal("account without an ID was accepted")
	}
}
