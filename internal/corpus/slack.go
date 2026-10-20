package corpus

import (
	"database/sql"
	"errors"
	"fmt"
)

// Slack holds the fields that only make sense for a Slack entry.
//
// TS is the raw float-seconds string, kept verbatim rather than re-derived from
// entries.ts: it is the message's identity to the Slack API, it is what a
// permalink is built from, and a reply names its parent by it. Reformatting it
// from an epoch would lose the trailing digits that make it unique within a
// channel.
type Slack struct {
	ChannelID   string
	ChannelName string
	TS          string
	ThreadTS    string
	ReplyCount  int
	Subtype     string
	IsBot       bool
	IsDM        bool
}

// PutSlack stores one Slack message: the entry, its slack_detail row, files and
// author participation, in one transaction.
//
// Re-ingest refreshes metadata and files even when the body is unchanged:
// replies and attachments can change independently of the text. Changed still
// reports only body edits; put preserves stored media links by source_ref.
func (s *Store) PutSlack(e Entry, d Slack, atts []Attachment) (PutResult, error) {
	if e.ExtID == "" {
		return PutResult{}, errors.New("slack entry needs an ext_id")
	}
	if e.Source == "" {
		e.Source = SourceSlack
	}

	tx, err := s.db.Begin()
	if err != nil {
		return PutResult{}, err
	}
	defer tx.Rollback()
	res, err := s.put(tx, e, nil, &d, atts, "")
	if err != nil {
		return res, err
	}
	// Slack participation is the author, not the channel membership. Read the
	// persisted author: put retains it when this archive omits the identity.
	if _, err := tx.Exec(`delete from participants where entry_id=? and role=?`, res.ID, RoleFrom); err != nil {
		return res, fmt.Errorf("clearing Slack author participation for %s: %w", e.ExtID, err)
	}
	if _, err := tx.Exec(`insert into participants (entry_id, person_id, role)
		select id, person_id, ? from entries where id=? and person_id is not null`, RoleFrom, res.ID); err != nil {
		return res, fmt.Errorf("recording Slack author participation for %s: %w", e.ExtID, err)
	}
	return res, tx.Commit()
}

func putSlackDetail(tx *sql.Tx, entryID int64, d Slack) error {
	_, err := tx.Exec(`
		insert into slack_detail (entry_id, channel_id, channel_name, ts, thread_ts,
		                          reply_count, subtype, is_bot, is_dm)
		values (?,?,?,?,?,?,?,?,?)
		on conflict(entry_id) do update set
		  channel_id=excluded.channel_id, channel_name=excluded.channel_name,
		  ts=excluded.ts, thread_ts=excluded.thread_ts,
		  reply_count=excluded.reply_count, subtype=excluded.subtype,
		  is_bot=excluded.is_bot, is_dm=excluded.is_dm`,
		entryID, nullStr(d.ChannelID), nullStr(d.ChannelName), nullStr(d.TS),
		nullStr(d.ThreadTS), d.ReplyCount, nullStr(d.Subtype),
		boolInt(d.IsBot), boolInt(d.IsDM))
	return err
}

// ResolveSlackParents links Slack replies to the message they hang off, matching
// parent_ref against slack_detail.ts.
//
// Separate from ResolveParents, which matches mail_detail.message_id: a Slack ts
// is not a Message-ID and would never match there, so without this every reply
// in the corpus stays a root. The match is scoped to the same channel because a
// bare ts is only unique within one — two channels can and do carry the same ts,
// and an unscoped match would silently hang a reply off a stranger's message in
// another channel.
//
// Re-runnable, and returns how many edges it resolved.
func (s *Store) ResolveSlackParents() (int64, error) {
	r, err := s.db.Exec(`
		update entries set parent_id = (
		  select p.id from slack_detail d join entries p on p.id = d.entry_id
		  where d.ts = entries.parent_ref
		    and d.channel_id = entries.container
		    and p.id <> entries.id
		)
		where source = ? and parent_id is null and parent_ref is not null
		  and exists (
		    select 1 from slack_detail d join entries p on p.id = d.entry_id
		    where d.ts = entries.parent_ref
		      and d.channel_id = entries.container
		      and p.id <> entries.id
		  )`, SourceSlack)
	if err != nil {
		return 0, fmt.Errorf("resolving slack parents: %w", err)
	}
	return r.RowsAffected()
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
