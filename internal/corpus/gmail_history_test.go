package corpus

import (
	"bufio"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestGmailSyncLockAcrossHandlesAndAccounts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "corpus.db")
	a, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	alias := path + ".symlink"
	if err := os.Symlink(path, alias); err != nil {
		t.Fatal(err)
	}
	b, err := Open(alias)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	unlock, err := a.LockGmailSync("legacy")
	if err != nil {
		t.Fatal(err)
	}
	if release, err := b.LockGmailSync("legacy"); err == nil {
		release()
		t.Fatal("overlapping run allowed")
	}
	other, err := b.LockGmailSync("other")
	if err != nil {
		t.Fatal("account locks not isolated", err)
	}
	other()
	unlock()
	release, err := b.LockGmailSync("legacy")
	if err != nil {
		t.Fatal(err)
	}
	release()
}

func TestGmailSyncLockReleasedAfterProcessDeath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "corpus.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	child := exec.Command(os.Args[0], "-test.run=^TestGmailSyncLockChild$")
	child.Env = append(os.Environ(), "CHAINMAIL_TEST_LOCK_DB="+path)
	out, err := child.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	child.Stderr = os.Stderr
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	defer child.Process.Kill()
	scanner := bufio.NewScanner(out)
	if !scanner.Scan() || scanner.Text() != "locked" {
		t.Fatal("child did not lock")
	}
	if release, err := s.LockGmailSync("legacy"); err == nil {
		release()
		t.Fatal("process lock ignored")
	}
	if err := child.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = child.Wait()
	release, err := s.LockGmailSync("legacy")
	if err != nil {
		t.Fatal("crashed run wedged account", err)
	}
	release()
}

func TestGmailSyncLockChild(t *testing.T) {
	path := os.Getenv("CHAINMAIL_TEST_LOCK_DB")
	if path == "" {
		return
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.LockGmailSync("legacy"); err != nil {
		t.Fatal(err)
	}
	fmt.Println("locked")
	// Wait until the parent kills this process, without deferring Unlock.
	_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
	select {}
}

func TestUpgradeFrom22PreservesCopiesAndStartsWithoutHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "corpus.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`create table schema_version(v integer primary key)`); err != nil {
		t.Fatal(err)
	}
	for i, m := range migrations[:22] {
		if _, err := db.Exec(m); err != nil {
			t.Fatalf("migration %d: %v", i+1, err)
		}
		if _, err := db.Exec(`insert into schema_version values (?)`, i+1); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`insert into entries(id, source, ext_id, kind, ts, body_text, body_sha, ingested_at) values (1,'mail','mail:test','message',0,'body','sha',0);
		insert into gmail_copies(account_id, gmail_id, entry_id, labels) values ('legacy','test',1,'["INBOX"]')`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	copies, err := s.GmailCopies(1)
	if err != nil || len(copies) != 1 || len(copies[0].Labels) != 1 || copies[0].Labels[0] != "INBOX" {
		t.Fatalf("copies %+v %v", copies, err)
	}
	committed, bootstrap, err := s.GmailHistory("legacy")
	if err != nil || committed != "" || bootstrap != "" {
		t.Fatalf("history %q/%q %v", committed, bootstrap, err)
	}
	if err := s.BeginGmailBootstrap("legacy", "18446744073709551000"); err != nil {
		t.Fatal(err)
	}
	if err := s.CommitGmailHistory("legacy", "18446744073709551010"); err != nil {
		t.Fatal(err)
	}
	committed, bootstrap, err = s.GmailHistory("legacy")
	if err != nil || committed != "18446744073709551010" || bootstrap != "" {
		t.Fatalf("large checkpoint %q/%q %v", committed, bootstrap, err)
	}
}
