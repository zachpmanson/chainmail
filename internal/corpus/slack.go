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

// PutSlack stores one Slack message: the entry, its slack_detail row and its
// files, in one transaction.
//
// Slack messages are treated as immutable. That is a decision, not an
// oversight: the corpus is fed from a local slackdump archive, so re-ingest is
// free and never re-hits the API, and an entry whose body hash still matches is
// skipped outright rather than rewritten. The consequence is that an edited
// message keeps the text it was archived with until slackdump sees the edit —
// at which point the hash differs and this rewrites the row, which is why the
// slow path stays. What there is no code for is *reporting* an edit, since
// nothing downstream distinguishes a revised Slack message from a fresh one.
func (s *Store) PutSlack(e Entry, d Slack, atts []Attachment) (PutResult, error) {
	if e.ExtID == "" {
		return PutResult{}, errors.New("slack entry needs an ext_id")
	}
	if e.Source == "" {
		e.Source = SourceSlack
	}

	// The skip is keyed on the detail row existing as well as the hash matching.
	// Hash alone would let a run interrupted between the two writes stay broken
	// forever, since the entry it would need to repair is the very thing that
	// makes it look done.
	var id int64
	err := s.db.QueryRow(`
		select e.id from entries e
		join slack_detail sd on sd.entry_id = e.id
		where e.source=? and e.ext_id=? and e.body_sha=?`,
		e.Source, e.ExtID, BodySHA(e.BodyText, e.Subject)).Scan(&id)
	switch {
	case err == nil:
		return PutResult{ID: id}, nil
	case !errors.Is(err, sql.ErrNoRows):
		return PutResult{}, fmt.Errorf("looking up %s: %w", e.ExtID, err)
	}

	tx, err := s.db.Begin()
	if err != nil {
		return PutResult{}, err
	}
	defer tx.Rollback()
	res, err := s.put(tx, e, nil, &d, atts)
	if err != nil {
		return res, err
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
