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
			return nil, fmt.Errorf("decoding labels for Gmail copy %q: %w", c.GmailID, err)
		}
		copies = append(copies, c)
	}
	return copies, rows.Err()
}
