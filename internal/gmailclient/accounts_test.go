package gmailclient

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAccountTokenPathKeepsLegacyAndSeparatesAccounts(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	legacy, err := AccountTokenPath("legacy")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(legacy) != "token.json" {
		t.Fatalf("legacy token path = %q, want docket token.json", legacy)
	}
	work, err := AccountTokenPath("account-123")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(work) != "account-123.json" || filepath.Base(filepath.Dir(work)) != "accounts" {
		t.Fatalf("account token path = %q, want account-specific path", work)
	}
}

func TestAccountTokenPathRejectsPathTraversal(t *testing.T) {
	for _, id := range []string{"", "../token", `..\\token`, ".", ".."} {
		if path, err := AccountTokenPath(id); err == nil {
			t.Errorf("AccountTokenPath(%q) = %q, want error", id, path)
		}
	}
}

func TestDisconnectAccountRemovesOnlyTheSelectedToken(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	paths := make(map[string]string)
	for _, id := range []string{"legacy", "personal"} {
		path, err := AccountTokenPath(id)
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

	if err := DisconnectAccount("legacy"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(paths["legacy"]); !os.IsNotExist(err) {
		t.Fatalf("legacy token stat error = %v, want not-exist", err)
	}
	if _, err := os.Stat(paths["personal"]); err != nil {
		t.Fatalf("disconnect removed another account's token: %v", err)
	}
	if err := DisconnectAccount("legacy"); err != nil {
		t.Fatalf("disconnecting an already absent token: %v", err)
	}
}
