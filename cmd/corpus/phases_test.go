package main

import (
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/zachpmanson/chainmail/internal/corpus"
	"github.com/zachpmanson/chainmail/internal/mailingest"
)

type emptyMailboxWithLabels struct{}

func (emptyMailboxWithLabels) Search(string, int, string) ([]mailingest.Envelope, mailingest.Page, error) {
	return nil, mailingest.Page{}, nil
}

func (emptyMailboxWithLabels) Read(string) (mailingest.Message, error) {
	return mailingest.Message{}, errors.New("unexpected message read")
}

func (emptyMailboxWithLabels) LabelNames() []string { return []string{"INBOX"} }

func TestRunGmailIngestUsesCorpusPathAndSeparateAccountTokenPath(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateDir)
	corpusPath := filepath.Join(t.TempDir(), "corpus.db")
	accountID := "account-123"
	store, err := corpus.Open(corpusPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.PutGmailAccount(corpus.GmailAccount{ID: accountID, DisplayName: "Test account"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	var gotTokenPath string

	_, err = runGmailIngest(corpusPath, mailOpts{
		query:     "in:anywhere",
		accountID: accountID,
	}, func(tokenPath string) (mailingest.Mailbox, error) {
		gotTokenPath = tokenPath
		return emptyMailboxWithLabels{}, nil
	})
	if err != nil {
		t.Fatalf("run Gmail ingest: %v", err)
	}

	wantTokenPath := filepath.Join(stateDir, "docket", "accounts", accountID+".json")
	if gotTokenPath != wantTokenPath {
		t.Errorf("Gmail client token path = %q, want %q", gotTokenPath, wantTokenPath)
	}

	store, err = corpus.Open(corpusPath)
	if err != nil {
		t.Fatalf("the corpus should be a readable SQLite database: %v", err)
	}
	defer store.Close()
	labels, err := store.MailboxLabelsForAccount(accountID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(labels, []string{"INBOX"}) {
		t.Errorf("stored labels = %v, want [INBOX]", labels)
	}
}
