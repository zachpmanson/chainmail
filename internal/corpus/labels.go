package corpus

import (
	"encoding/json"
	"sort"
	"strings"
)

// LabelCount is one folder: a label the mailbox defines, and how many stored
// messages carry it. A folder with nothing filed under it yet is a real entry
// with a count of zero.
//
// Messages rather than chains, deliberately. A chain count for a label is a walk
// over the reply graph — the same pass that makes the list, run again for the
// sidebar — and a mail client's own sidebar counts messages. The number here is
// the one a reader already expects next to INBOX.
type LabelCount struct {
	Name     string
	Messages int
}

// Labels lists the mailbox's folders, busiest first.
//
// The names are the mailbox's own — the Gmail system labels (INBOX, SENT,
// UNREAD, STARRED, IMPORTANT, the CATEGORY_* buckets) and whatever Zach filed
// things under — because they are the only honest answer to "what folders are
// there": a folder list invented here would be a fiction about his mail, while
// this is what he already sees in the mailbox itself.
//
// Two sources, joined by name. The counts are the corpus's (labelCounts, one
// scan of one small column counted in Go: mail_detail.labels is a comma-joined
// string, so SQL can count rows but not the labels inside them). The names are
// the mailbox's whole list as the last ingest recorded it (MailboxLabels), so a
// folder created a minute ago with nothing filed under it appears at zero
// instead of being invisible until mail lands in it. A host whose ingest never
// learned the list serves the corpus-derived folders, exactly as it did before —
// an absent list is not an empty one.
func (s *Store) Labels() ([]LabelCount, error) {
	return s.labelsWith(s.labelCounts, s.MailboxLabels)
}

// LabelsForAccount returns counts and the last mailbox label list for one
// Gmail account. Mail in other accounts is deliberately excluded.
func (s *Store) LabelsForAccount(accountID string) ([]LabelCount, error) {
	if accountID == "" || accountID == "legacy" {
		return s.Labels()
	}
	return s.labelsWith(func() (map[string]int, error) {
		rows, err := s.db.Query(`select labels from gmail_copies where account_id=?`, accountID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		counts := map[string]int{}
		for rows.Next() {
			var raw string
			if err := rows.Scan(&raw); err != nil {
				return nil, err
			}
			var labels []string
			if err := json.Unmarshal([]byte(raw), &labels); err != nil {
				labels = splitLabels(raw)
			}
			for _, label := range labels {
				if label != "" {
					counts[label]++
				}
			}
		}
		return counts, rows.Err()
	}, func() ([]string, error) { return s.MailboxLabelsForAccount(accountID) })
}

func (s *Store) labelsWith(countFn func() (map[string]int, error), mailboxFn func() ([]string, error)) ([]LabelCount, error) {
	counts, err := countFn()
	if err != nil {
		return nil, err
	}

	// The mailbox's own labels are folded in with no messages of their own. A
	// label the ingest has seen on mail keeps its corpus count; a folder that
	// exists in the mailbox with nothing filed under it yet still appears, at
	// zero. That is the difference between "what folders are there" and "what
	// mail did we happen to ingest" — this list answers the first.
	mailbox, err := mailboxFn()
	if err != nil {
		return nil, err
	}
	for _, name := range mailbox {
		if _, ok := counts[name]; !ok {
			counts[name] = 0
		}
	}

	out := make([]LabelCount, 0, len(counts))
	for name, n := range counts {
		out = append(out, LabelCount{Name: name, Messages: n})
	}
	// A fixed order, so two reads of an unchanged corpus agree: busiest first,
	// and the name breaks a tie. Nothing here is a judgement about the labels —
	// only about how to print them.
	sort.Slice(out, func(i, j int) bool {
		if out[i].Messages != out[j].Messages {
			return out[i].Messages > out[j].Messages
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

// labelCounts counts the mailbox labels seen on stored messages. It is the half
// of the folder list the corpus can answer on its own, and the half a host with
// no mailbox keeps serving.
func (s *Store) labelCounts() (map[string]int, error) {
	rows, err := s.db.Query(
		`select labels from mail_detail where labels is not null and labels <> ''`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	counts := map[string]int{}
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		for _, l := range strings.Split(raw, ",") {
			if l = strings.TrimSpace(l); l != "" {
				counts[l]++
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return counts, nil
}

// MailboxLabels is the mailbox's own label list, as the last ingest saw it.
//
// Distinct from Labels: this is what the mailbox defines, not what the corpus
// holds, and it is stored rather than fetched so the request path keeps its
// no-network property (see the schema comment on mailbox_labels). An empty
// slice means the ingest has never recorded the list, which callers read the
// same way as "no mailbox available" rather than as "no folders".
func (s *Store) MailboxLabels() ([]string, error) {
	return s.mailboxLabelsQuery(`select name from mailbox_labels order by name`)
}

func (s *Store) MailboxLabelsForAccount(accountID string) ([]string, error) {
	if accountID == "legacy" || accountID == "" {
		return s.MailboxLabels()
	}
	return s.mailboxLabelsQuery(`select name from gmail_account_labels where account_id=? order by name`, accountID)
}

func (s *Store) mailboxLabelsQuery(query string, args ...any) ([]string, error) {
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out = append(out, name)
	}
	return out, rows.Err()
}

// PutMailboxLabels replaces the stored mailbox label list with names.
//
// A replace, not a merge: the list is the mailbox's whole answer to "what
// folders are there", so anything no longer in it was renamed or deleted and
// must go, or the folder list would grow permanent ghosts. Done in one
// transaction so a reader never sees a half-written list, and idempotent so a
// re-ingest of the same mailbox writes the same rows.
func (s *Store) PutMailboxLabels(names []string) error {
	return s.replaceMailboxLabels("", names)
}

func (s *Store) PutMailboxLabelsForAccount(accountID string, names []string) error {
	if accountID == "" || accountID == "legacy" {
		return s.PutMailboxLabels(names)
	}
	return s.replaceMailboxLabels(accountID, names)
}

func (s *Store) replaceMailboxLabels(accountID string, names []string) error {
	sorted := append([]string(nil), names...)
	sort.Strings(sorted)

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if accountID == "" {
		if _, err := tx.Exec(`delete from mailbox_labels`); err != nil {
			return err
		}
	} else {
		if _, err := tx.Exec(`delete from gmail_account_labels where account_id=?`, accountID); err != nil {
			return err
		}
	}
	seen := map[string]bool{}
	for _, name := range sorted {
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		if accountID == "" {
			if _, err := tx.Exec(`insert into mailbox_labels (name) values (?)`, name); err != nil {
				return err
			}
		} else if _, err := tx.Exec(`insert into gmail_account_labels (account_id, name) values (?, ?)`, accountID, name); err != nil {
			return err
		}
	}
	return tx.Commit()
}
