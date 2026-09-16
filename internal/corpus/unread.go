package corpus

import (
	"fmt"
	"strings"
)

// UnreadLabel is the mailbox's own word for "nobody has opened this yet", and
// the only label chainmail ever writes.
//
// It is a system label, so it is also its own id in the Gmail API — which is
// why resolving it needs no label lookup, and why the name is spelled here once
// rather than at each call site.
const UnreadLabel = "UNREAD"

// Unread reports whether a mailbox label list carries UNREAD.
//
// Case-sensitive deliberately: this compares the mailbox's own names, which are
// spelled UNREAD and nothing else. A case-folding comparison would be friendlier
// to a caller that is not the mailbox, and every such caller should be reading
// these same bytes back out of mail_detail rather than inventing them.
func Unread(labels []string) bool {
	for _, l := range labels {
		if l == UnreadLabel {
			return true
		}
	}
	return false
}

// SetUnread returns the label list with UNREAD added or removed, keeping the
// other labels in the order the mailbox gave them.
//
// The mailbox's order is the mailbox's, not ours: labels are stored as one
// comma-joined string and read back in that order, so a rewrite that sorted
// them would make an unrelated write look like a change to every label on the
// message. UNREAD is appended rather than inserted for the same reason — the
// position of a label nobody displays in order does not matter, and appending
// is the shape that is trivially stable across a write and its reverse.
func SetUnread(labels []string, unread bool) []string {
	out := make([]string, 0, len(labels)+1)
	for _, l := range labels {
		if l != UnreadLabel {
			out = append(out, l)
		}
	}
	if unread {
		out = append(out, UnreadLabel)
	}
	return out
}

// splitLabels reads the comma-joined labels column back into a list. The same
// shape Labels() counts, so a label with a stray space is read the same way in
// both places rather than being one thing in the sidebar and another here.
func splitLabels(raw string) []string {
	out := []string{}
	for _, l := range strings.Split(raw, ",") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}

// joinLabels is the inverse: what goes into the labels column. An empty list is
// stored as NULL rather than an empty string, matching what Put writes for a
// message with no labels — a message in no folder and a message whose folder
// list nobody has heard of should not be two different kinds of row.
func joinLabels(labels []string) any {
	if len(labels) == 0 {
		return nil
	}
	return strings.Join(labels, ",")
}

// ChainEntry is one entry of a chain, carrying the two handles a read-state
// write needs: the row to rewrite locally, and the mailbox id to rewrite
// remotely. A chain also holds entries with no mailbox copy at all — a message
// recovered from quoted text, a Slack post — and those come back with an empty
// GmailID, which is how a caller counts what it could not mark instead of
// treating it as a failure.
type ChainEntry struct {
	ID      int64
	ExtID   string
	GmailID string
	Labels  []string
}

// ChainEntries returns the chain rooted at rootExtID, oldest first: every entry
// reachable by walking parent_id down from the root, in the same direction
// chainMeta summarises and SearchChains groups by.
//
// The walk is the graph, not the container, for the reason SearchChains gives:
// a message recovered from a quote and an original forwarded across sources
// share no Gmail thread, and the reply headers are what actually connect them.
//
// An unknown root is an empty result, not an error — the caller is a handler
// that has to tell "no such chain" from "a chain with nothing markable in it",
// and it can only do that if this reports the difference (no rows) rather than
// a failure it would have to pattern-match on.
func (s *Store) ChainEntries(rootExtID string) ([]ChainEntry, error) {
	rows, err := s.db.Query(`
		with recursive down(id, depth) as (
		  select id, 0 from entries where ext_id = ?
		  union all
		  select e.id, d.depth + 1
		    from down d join entries e on e.parent_id = d.id
		   where d.depth < ?
		)
		select d.id, e.ext_id, coalesce(md.gmail_id, ''), coalesce(md.labels, '')
		  from down d join entries e on e.id = d.id
		  left join mail_detail md on md.entry_id = d.id
		 order by e.ts, e.id`, rootExtID, walkDepthCap)
	if err != nil {
		return nil, fmt.Errorf("walking the chain at %q: %w", rootExtID, err)
	}
	defer rows.Close()

	var out []ChainEntry
	for rows.Next() {
		var e ChainEntry
		var labels string
		if err := rows.Scan(&e.ID, &e.ExtID, &e.GmailID, &labels); err != nil {
			return nil, err
		}
		e.Labels = splitLabels(labels)
		out = append(out, e)
	}
	return out, rows.Err()
}

// SetLabels rewrites one message's stored mailbox labels — the local half of a
// read-state write, and the only label write there is.
//
// It updates, never inserts: mail_detail rows are created by the ingest, and a
// labels-only write that could create a row would be a way to invent a mailbox
// copy of a message that has none.
func (s *Store) SetLabels(entryID int64, labels []string) error {
	res, err := s.db.Exec(
		`update mail_detail set labels = ? where entry_id = ?`,
		joinLabels(labels), entryID)
	if err != nil {
		return fmt.Errorf("setting labels on entry %d: %w", entryID, err)
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return fmt.Errorf("entry %d has no mailbox copy, so its labels cannot be set", entryID)
	}
	return nil
}

// MailLabels is one corpus message that has a mailbox copy: the row to correct
// and the labels it currently claims.
type MailLabels struct {
	ID      int64
	GmailID string
	Labels  []string
}

// MailLabels lists every corpus message the mailbox could have labelled: the
// mail rows with a Gmail id. Quote-recovered mail has no mailbox copy and no
// labels to be wrong about, so it is excluded rather than reported as agreeing.
func (s *Store) MailLabels() ([]MailLabels, error) {
	rows, err := s.db.Query(`
		select entry_id, gmail_id, coalesce(labels, '') from mail_detail
		 where gmail_id is not null and gmail_id <> ''`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []MailLabels
	for rows.Next() {
		var m MailLabels
		var labels string
		if err := rows.Scan(&m.ID, &m.GmailID, &labels); err != nil {
			return nil, err
		}
		m.Labels = splitLabels(labels)
		out = append(out, m)
	}
	return out, rows.Err()
}

// UnreadReconcile is what one pass corrected: how many mailbox copies were
// compared, and the two directions a label can be wrong in.
//
// Two counters and not one, because the directions mean different things to a
// reader of the phase report. Marked is mail that arrived since the last run
// (or was unread when it was ingested and still is); a Cleared count in the
// hundreds is the mirror being corrected for the first time, which is a fact
// about the corpus's history rather than about today's mail.
type UnreadReconcile struct {
	Checked int
	Marked  int
	Cleared int
}

// ReconcileUnread corrects the stored UNREAD label against the mailbox's own
// unread set, in both directions.
//
// This exists because the ingest skips a Gmail id it already holds — a mail
// message is read once, and its labels with it, on the grounds that "a Gmail
// message id names immutable bytes" (refresh.go). That is true of the body and
// false of the labels: UNREAD is written by whoever opens the message, in
// Gmail, on a phone, anywhere at all, and a corpus that never looks again
// reports "unread when it was ingested" while claiming to report "unread".
//
// unreadGmailIDs must be the mailbox's complete unread set — the caller pages
// is:unread to exhaustion and fails rather than passing a prefix, because a
// truncated set does not read as truncated here: every id missing from it looks
// exactly like a message that has been read, and this would clear good labels
// wholesale. The cost is one page over the unread half of the mailbox, which is
// why the mail phase's own reads stay known-id-skipped.
func (s *Store) ReconcileUnread(unreadGmailIDs []string) (UnreadReconcile, error) {
	want := make(map[string]bool, len(unreadGmailIDs))
	for _, id := range unreadGmailIDs {
		if id != "" {
			want[id] = true
		}
	}
	rows, err := s.MailLabels()
	if err != nil {
		return UnreadReconcile{}, err
	}

	var res UnreadReconcile
	tx, err := s.db.Begin()
	if err != nil {
		return res, err
	}
	defer tx.Rollback()
	stmt, err := tx.Prepare(`update mail_detail set labels = ? where entry_id = ?`)
	if err != nil {
		return res, err
	}
	defer stmt.Close()

	for _, r := range rows {
		res.Checked++
		should := want[r.GmailID]
		if now := Unread(r.Labels); now == should {
			continue
		}
		if should {
			res.Marked++
		} else {
			res.Cleared++
		}
		if _, err := stmt.Exec(joinLabels(SetUnread(r.Labels, should)), r.ID); err != nil {
			return res, fmt.Errorf("correcting entry %d: %w", r.ID, err)
		}
	}
	return res, tx.Commit()
}
