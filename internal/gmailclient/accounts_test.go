package gmailclient

import (
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
