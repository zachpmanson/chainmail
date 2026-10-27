package corpus

import (
	"crypto/sha256"
	"database/sql"
	"fmt"
	"path/filepath"
	"sync"

	"github.com/gofrs/flock"
)

// LockGmailSync excludes overlapping ingest runs for an account, including
// separate processes. OS locks release on a crash; no stale lease can wedge a
// mailbox or expire while a long bootstrap is still running.
func (s *Store) LockGmailSync(accountID string) (func(), error) {
	var seq int
	var name, path string
	if err := s.db.QueryRow(`pragma database_list`).Scan(&seq, &name, &path); err != nil {
		return nil, err
	}
	if path == "" {
		v, _ := s.gmailSync.LoadOrStore(accountID, new(sync.Mutex))
		mu := v.(*sync.Mutex)
		if !mu.TryLock() {
			return nil, fmt.Errorf("Gmail account %q is already syncing", accountID)
		}
		return mu.Unlock, nil
	}
	path, err := filepath.EvalSymlinks(path)
	if err != nil {
		return nil, err
	}
	lock := flock.New(fmt.Sprintf("%s.gmail-%x.lock", path, sha256.Sum256([]byte(accountID))))
	ok, err := lock.TryLock()
	if err != nil {
		return nil, fmt.Errorf("locking Gmail sync: %w", err)
	}
	if !ok {
		return nil, fmt.Errorf("Gmail account %q is already syncing", accountID)
	}
	return func() { _ = lock.Unlock() }, nil
}

func (s *Store) GmailHistory(accountID string) (committed, bootstrap string, err error) {
	err = s.db.QueryRow(`select history_id, bootstrap_id from gmail_history where account_id=?`, accountID).
		Scan(&committed, &bootstrap)
	if err == sql.ErrNoRows {
		err = nil
	}
	return
}

// BeginGmailBootstrap atomically starts a fresh full scan with no old frontier
// or page token. The previous committed history ID is retained until success.
func (s *Store) BeginGmailBootstrap(accountID, historyID string) error {
	if historyID == "" {
		return fmt.Errorf("empty Gmail bootstrap history ID")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`insert into gmail_history(account_id, bootstrap_id) values (?, ?)
		on conflict(account_id) do update set bootstrap_id=excluded.bootstrap_id`, accountID, historyID); err != nil {
		return err
	}
	if _, err := tx.Exec(`delete from gmail_cursors where account_id=? and query='in:anywhere'`, accountID); err != nil {
		return err
	}
	// Clear generations even when Google returns the same ID for a restarted
	// bootstrap (an unchanged mailbox is allowed to do that).
	if _, err := tx.Exec(`update gmail_copies set seen_history_id='' where account_id=?`, accountID); err != nil {
		return err
	}
	return tx.Commit()
}

// CancelGmailBootstrap invalidates the scan generation when another canonical
// query walk may advance its shared page cursor without marking copies seen.
func (s *Store) CancelGmailBootstrap(accountID string) error {
	_, err := s.db.Exec(`update gmail_history set bootstrap_id='' where account_id=?`, accountID)
	return err
}

func (s *Store) CommitGmailHistory(accountID, historyID string) error {
	if historyID == "" {
		return fmt.Errorf("empty Gmail history checkpoint")
	}
	_, err := s.db.Exec(`update gmail_history set history_id=?, bootstrap_id='' where account_id=?`, historyID, accountID)
	return err
}

func (s *Store) MarkGmailScanSeen(accountID, gmailID, historyID string) error {
	_, err := s.db.Exec(`update gmail_copies set seen_history_id=? where account_id=? and gmail_id=?`, historyID, accountID, gmailID)
	return err
}

// ForgetGmailCopyLabels retains archived evidence but no longer advertises a
// deleted message in any remote folder. Unknown IDs (e.g. deleted drafts) are
// harmless. The legacy mirror is updated in the same transaction.
func (s *Store) ForgetGmailCopyLabels(accountID, gmailID string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`update gmail_copies set labels='[]' where account_id=? and gmail_id=?`, accountID, gmailID); err != nil {
		return err
	}
	if accountID == "legacy" {
		if _, err := tx.Exec(`update mail_detail set labels='' where gmail_id=?`, gmailID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ReconcileMissingGmailCopies runs only after a complete bootstrap. Unlike a
// date-frontier top-up it can prove which stored copies have left the mailbox.
func (s *Store) ReconcileMissingGmailCopies(accountID, historyID string) error {
	rows, err := s.db.Query(`select gmail_id from gmail_copies where account_id=? and seen_history_id!=?`, accountID, historyID)
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err := s.ForgetGmailCopyLabels(accountID, id); err != nil {
			return err
		}
	}
	return nil
}
