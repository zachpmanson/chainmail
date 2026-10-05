package corpus

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// GmailAccount is a connected mailbox. ID is an application-generated stable
// identifier; Email is display metadata and may be unknown for the migrated
// legacy account.
type GmailAccount struct {
	ID          string
	Email       string
	DisplayName string
	CreatedAt   time.Time
}

// GmailCopy identifies one account-local Gmail message corresponding to a
// logical corpus entry. Gmail IDs are only unique within an account, and labels
// belong to the copy rather than to the logical message.
type GmailCopy struct {
	AccountID string
	GmailID   string
	EntryID   int64
	Labels    []string
}

// GmailAccounts lists connected accounts in a stable order. The legacy
// account has no known email until its existing token is identified.
func (s *Store) GmailAccounts() ([]GmailAccount, error) {
	rows, err := s.db.Query(`select id, coalesce(email, ''), display_name, created_at
		from gmail_accounts order by created_at, id`)
	if err != nil {
		return nil, fmt.Errorf("listing Gmail accounts: %w", err)
	}
	defer rows.Close()
	var accounts []GmailAccount
	for rows.Next() {
		var a GmailAccount
		var created int64
		if err := rows.Scan(&a.ID, &a.Email, &a.DisplayName, &created); err != nil {
			return nil, err
		}
		a.CreatedAt = time.Unix(created, 0).UTC()
		accounts = append(accounts, a)
	}
	return accounts, rows.Err()
}

// PutGmailAccount inserts or updates display metadata for an account. The
// stable account ID is supplied by the caller; email is deliberately not the
// primary key because it is mutable profile metadata, not a durable handle.
func (s *Store) PutGmailAccount(a GmailAccount) error {
	a.ID = strings.TrimSpace(a.ID)
	a.Email = strings.TrimSpace(a.Email)
	a.DisplayName = strings.TrimSpace(a.DisplayName)
	if a.ID == "" || a.DisplayName == "" {
		return errors.New("Gmail account needs an id and display name")
	}
	var email any
	if a.Email != "" {
		email = a.Email
	}
	created := a.CreatedAt
	if created.IsZero() {
		created = time.Now().UTC()
	}
	_, err := s.db.Exec(`insert into gmail_accounts(id, email, display_name, created_at)
		values (?, ?, ?, ?)
		on conflict(id) do update set email=excluded.email, display_name=excluded.display_name`,
		a.ID, email, a.DisplayName, created.Unix())
	if err != nil {
		return fmt.Errorf("saving Gmail account %q: %w", a.ID, err)
	}
	return nil
}

// PutGmailCopy records or refreshes one account-local mailbox copy. An account
// cannot map two Gmail IDs to the same logical entry; duplicate logical copies
// within one mailbox must be settled by the existing twins pass first.
func (s *Store) PutGmailCopy(c GmailCopy) error {
	if strings.TrimSpace(c.AccountID) == "" || strings.TrimSpace(c.GmailID) == "" || c.EntryID == 0 {
		return errors.New("Gmail copy needs an account, Gmail id, and entry id")
	}
	labels, err := json.Marshal(c.Labels)
	if err != nil {
		return fmt.Errorf("encoding labels for Gmail copy %q: %w", c.GmailID, err)
	}
	_, err = s.db.Exec(`insert into gmail_copies(account_id, gmail_id, entry_id, labels)
		values (?, ?, ?, ?)
		on conflict(account_id, gmail_id) do update set entry_id=excluded.entry_id, labels=excluded.labels`,
		c.AccountID, c.GmailID, c.EntryID, string(labels))
	if err != nil {
		return fmt.Errorf("saving Gmail copy %q in account %q: %w", c.GmailID, c.AccountID, err)
	}
	return nil
}

// GmailCopies returns every mailbox copy of one logical entry, ordered by
// account ID. Labels are JSON arrays so label names containing commas survive.
func (s *Store) GmailCopies(entryID int64) ([]GmailCopy, error) {
	rows, err := s.db.Query(`select account_id, gmail_id, entry_id, labels
		from gmail_copies where entry_id=? order by account_id`, entryID)
	if err != nil {
		return nil, fmt.Errorf("listing Gmail copies for entry %d: %w", entryID, err)
	}
	defer rows.Close()
	var copies []GmailCopy
	for rows.Next() {
		var c GmailCopy
		var labels string
		if err := rows.Scan(&c.AccountID, &c.GmailID, &c.EntryID, &labels); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(labels), &c.Labels); err != nil {
			// The migration carries pre-existing comma-separated labels forward;
			// subsequent writes use JSON so commas in new label names are safe.
			c.Labels = splitLabels(labels)
		}
		copies = append(copies, c)
	}
	return copies, rows.Err()
}

// SetGmailCopyLabels stores the mailbox's latest label answer for one account
// copy. For the legacy account it also updates the pre-existing mail_detail
// mirror used by older readers.
func (s *Store) SetGmailCopyLabels(accountID, gmailID string, labels []string) error {
	encoded, err := json.Marshal(labels)
	if err != nil {
		return fmt.Errorf("encoding labels for Gmail copy %q: %w", gmailID, err)
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`update gmail_copies set labels=? where account_id=? and gmail_id=?`,
		string(encoded), accountID, gmailID)
	if err != nil {
		return fmt.Errorf("updating labels for Gmail copy %q: %w", gmailID, err)
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return fmt.Errorf("no Gmail copy %q in account %q", gmailID, accountID)
	}
	if accountID == "legacy" {
		if _, err := tx.Exec(`update mail_detail set labels=? where gmail_id=?`, joinLabels(labels), gmailID); err != nil {
			return fmt.Errorf("updating legacy labels for Gmail copy %q: %w", gmailID, err)
		}
	}
	return tx.Commit()
}

// ReconcileGmailUnread updates one account's copy labels from Gmail's complete
// unread set. The logical entry's unread bit is the union across its copies.
func (s *Store) ReconcileGmailUnread(accountID string, unreadGmailIDs []string) (UnreadReconcile, error) {
	want := make(map[string]bool, len(unreadGmailIDs))
	for _, id := range unreadGmailIDs {
		if id != "" {
			want[id] = true
		}
	}
	tx, err := s.db.Begin()
	if err != nil {
		return UnreadReconcile{}, err
	}
	defer tx.Rollback()
	rows, err := tx.Query(`select gmail_id, entry_id, labels from gmail_copies where account_id=?`, accountID)
	if err != nil {
		return UnreadReconcile{}, err
	}
	type copyRow struct {
		gmailID string
		entry   int64
		labels  []string
	}
	var copies []copyRow
	for rows.Next() {
		var r copyRow
		var raw string
		if err := rows.Scan(&r.gmailID, &r.entry, &raw); err != nil {
			rows.Close()
			return UnreadReconcile{}, err
		}
		if err := json.Unmarshal([]byte(raw), &r.labels); err != nil {
			r.labels = splitLabels(raw)
		}
		copies = append(copies, r)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return UnreadReconcile{}, err
	}
	rows.Close()
	updates := make(map[int64]struct{})
	res := UnreadReconcile{}
	for _, c := range copies {
		res.Checked++
		should := want[c.gmailID]
		if Unread(c.labels) != should {
			if should {
				res.Marked++
			} else {
				res.Cleared++
			}
			encoded, _ := json.Marshal(SetUnread(c.labels, should))
			if _, err := tx.Exec(`update gmail_copies set labels=? where account_id=? and gmail_id=?`,
				string(encoded), accountID, c.gmailID); err != nil {
				return res, fmt.Errorf("correcting Gmail copy %q: %w", c.gmailID, err)
			}
		}
		updates[c.entry] = struct{}{}
	}
	for entry := range updates {
		copyRows, err := tx.Query(`select labels from gmail_copies where entry_id=? and account_id=?`, entry, accountID)
		if err != nil {
			return res, err
		}
		unread := false
		for copyRows.Next() {
			var raw string
			var labels []string
			if err := copyRows.Scan(&raw); err != nil {
				copyRows.Close()
				return res, err
			}
			if json.Unmarshal([]byte(raw), &labels) != nil {
				labels = splitLabels(raw)
			}
			unread = unread || Unread(labels)
		}
		if err := copyRows.Err(); err != nil {
			copyRows.Close()
			return res, err
		}
		copyRows.Close()
		if accountID == "legacy" {
			var raw string
			if err := tx.QueryRow(`select coalesce(labels,'') from mail_detail where entry_id=?`, entry).Scan(&raw); err != nil {
				return res, err
			}
			if _, err := tx.Exec(`update mail_detail set labels=? where entry_id=?`,
				joinLabels(SetUnread(splitLabels(raw), unread)), entry); err != nil {
				return res, err
			}
		}
	}
	return res, tx.Commit()
}
