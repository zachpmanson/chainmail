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
	if err := s.SetGmailCopyLabels("work", "same-local-id", []string{"INBOX", "UNREAD"}); err != nil {
		t.Fatal(err)
	}
	copies, err = s.GmailCopies(entry.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(copies[0].Labels, []string{"SENT"}) || !reflect.DeepEqual(copies[1].Labels, []string{"INBOX", "UNREAD"}) {
		t.Fatalf("same-local-id label update crossed accounts: %+v", copies)
	}
}

func TestMailboxLabelsAndCountsAreScopedByAccount(t *testing.T) {
	s := open(t)
	for _, account := range []GmailAccount{{ID: "work", DisplayName: "Work"}, {ID: "personal", DisplayName: "Personal"}} {
		if err := s.PutGmailAccount(account); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct{ account, ext, id, label string }{
		{"work", "mail:<w@example.test>", "same-id", "Work"},
		{"personal", "mail:<p@example.test>", "same-id", "Personal"},
	} {
		_, err := s.PutForAccount(tc.account, Entry{Source: SourceMail, ExtID: tc.ext, Kind: "message", TS: time.Now(), BodyText: tc.ext},
			&Mail{GmailID: tc.id, MessageID: tc.ext, Labels: []string{tc.label}}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.PutMailboxLabelsForAccount(tc.account, []string{"INBOX", tc.label}); err != nil {
			t.Fatal(err)
		}
	}
	work, err := s.LabelsForAccount("work")
	if err != nil {
		t.Fatal(err)
	}
	workCounts := map[string]int{}
	for _, label := range work {
		workCounts[label.Name] = label.Messages
	}
	if workCounts["Work"] != 1 || workCounts["INBOX"] != 0 || len(workCounts) != 2 {
		t.Fatalf("work labels = %+v, want Work=1 and account-local INBOX=0", work)
	}
	if _, ok := workCounts["Personal"]; ok {
		t.Fatalf("personal label leaked into work account: %+v", work)
	}
	personal, err := s.LabelsForAccount("personal")
	if err != nil {
		t.Fatal(err)
	}
	for _, label := range personal {
		if label.Name == "Work" {
			t.Fatalf("work label leaked into personal account: %+v", personal)
		}
	}
}

func TestUnreadStateIsReconciledPerCopyAndAggregatedAcrossAccounts(t *testing.T) {
	s := open(t)
	for _, account := range []GmailAccount{
		{ID: "work", DisplayName: "Work"}, {ID: "personal", DisplayName: "Personal"},
	} {
		if err := s.PutGmailAccount(account); err != nil {
			t.Fatal(err)
		}
	}
	entry, err := s.PutForAccount("work", Entry{
		Source: SourceMail, ExtID: "mail:<unread@example.test>", Kind: "message",
		TS: time.Now(), BodyText: "unread",
	}, &Mail{GmailID: "work-id", MessageID: "<unread@example.test>", Labels: []string{"UNREAD"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.PutGmailCopy(GmailCopy{
		AccountID: "personal", GmailID: "personal-id", EntryID: entry.ID, Labels: []string{"UNREAD"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReconcileGmailUnread("work", nil); err != nil {
		t.Fatal(err)
	}
	labels, err := s.MailLabels()
	if err != nil {
		t.Fatal(err)
	}
	if len(labels) != 1 || !Unread(labels[0].Labels) {
		t.Fatalf("logical unread labels after one account was read: %+v", labels)
	}
	if _, err := s.ReconcileGmailUnread("personal", nil); err != nil {
		t.Fatal(err)
	}
	labels, err = s.MailLabels()
	if err != nil {
		t.Fatal(err)
	}
	if len(labels) != 1 || Unread(labels[0].Labels) {
		t.Fatalf("logical unread labels after both copies were read: %+v", labels)
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
