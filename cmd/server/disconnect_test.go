package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/zachpmanson/chainmail/internal/corpus"
	"github.com/zachpmanson/chainmail/internal/gmailclient"
)

func TestDisconnectAccountRemovesTokenButKeepsAccountAndOtherCredentials(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	srv := testServer(t)
	if err := srv.store.PutGmailAccount(corpus.GmailAccount{
		ID: "personal", Email: "zach@example.test", DisplayName: "Personal",
	}); err != nil {
		t.Fatal(err)
	}
	paths := make(map[string]string)
	for _, id := range []string{"legacy", "personal"} {
		path, err := gmailclient.AccountTokenPath(id)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("token"), 0o600); err != nil {
			t.Fatal(err)
		}
		paths[id] = path
	}

	res := srv.do(t, "POST", "/auth/accounts/legacy/disconnect", nil)
	if res.status != 200 {
		t.Fatalf("disconnect status = %d: %s", res.status, res.body)
	}
	var got struct {
		AccountID    string `json:"accountId"`
		Disconnected bool   `json:"disconnected"`
	}
	if err := json.Unmarshal(res.body, &got); err != nil {
		t.Fatal(err)
	}
	if got.AccountID != "legacy" || !got.Disconnected {
		t.Fatalf("disconnect response = %+v", got)
	}
	if _, err := os.Stat(paths["legacy"]); !os.IsNotExist(err) {
		t.Fatalf("legacy token stat error = %v, want not-exist", err)
	}
	if _, err := os.Stat(paths["personal"]); err != nil {
		t.Fatalf("personal token was removed: %v", err)
	}

	accounts, err := srv.store.GmailAccounts()
	if err != nil {
		t.Fatal(err)
	}
	if len(accounts) != 2 {
		t.Fatalf("disconnect removed account metadata or copies: accounts = %+v", accounts)
	}
	status := srv.do(t, "GET", "/auth/status", nil)
	if status.status != 200 {
		t.Fatalf("auth status = %d: %s", status.status, status.body)
	}
	var auth authStatusResponse
	if err := json.Unmarshal(status.body, &auth); err != nil {
		t.Fatal(err)
	}
	signedIn := make(map[string]bool, len(auth.Accounts))
	for _, account := range auth.Accounts {
		signedIn[account.ID] = account.SignedIn
	}
	if !auth.SignedIn || len(auth.Accounts) != 2 || signedIn["legacy"] || !signedIn["personal"] {
		t.Fatalf("auth status after disconnect = %+v, want legacy signed out and personal still in", auth)
	}
}

func TestDisconnectUnknownAccountIsNotFound(t *testing.T) {
	srv := testServer(t)
	res := srv.do(t, "POST", "/auth/accounts/missing/disconnect", nil)
	if res.status != 404 {
		t.Fatalf("status = %d, want 404: %s", res.status, res.body)
	}
}
