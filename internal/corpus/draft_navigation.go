package corpus

import "database/sql"

// DraftNavigation names corpus ext IDs, never Gmail IDs. Handles exist only
// when the account-qualified logical parent is unique and its walk reaches a
// current stored root. Recovered ancestors need not have mailbox copies.
type DraftNavigation struct {
	Resolution   string
	ReplyToEntry string
	RootID       string
}

// ResolveDraftParent is a read-only, ancestor-only lookup in one SQLite snapshot.
// Retain at most two distinct candidates: even siblings under one root remain
// ambiguous. The depth bound also terminates cycles; exhaustion is not a root.
func (s *Store) ResolveDraftParent(accountID, messageID string) (DraftNavigation, error) {
	var count int
	var parent, root sql.NullString
	err := s.db.QueryRow(`with recursive
 candidates as materialized (
  select distinct e.id, e.ext_id from mail_detail d
  join entries e on e.id=d.entry_id
  join gmail_copies gc on gc.entry_id=e.id
  where gc.account_id=? and d.message_id=? and e.source='mail' limit 2
 ),
 up(id,ext_id,parent_id,depth) as (
  select e.id,e.ext_id,e.parent_id,0 from entries e
  join candidates c on c.id=e.id where (select count(*) from candidates)=1
  union all
  select e.id,e.ext_id,e.parent_id,up.depth+1 from entries e
  join up on e.id=up.parent_id where up.depth<?
 )
 select (select count(*) from candidates),
 (select ext_id from candidates where (select count(*) from candidates)=1),
 (select ext_id from up where parent_id is null)`, accountID, messageID, walkDepthCap-1).Scan(&count, &parent, &root)
	if err != nil {
		return DraftNavigation{}, err
	}
	switch {
	case count == 0:
		return DraftNavigation{Resolution: "missing-parent"}, nil
	case count > 1:
		return DraftNavigation{Resolution: "ambiguous-parent"}, nil
	case !parent.Valid || parent.String == "" || !root.Valid || root.String == "":
		return DraftNavigation{Resolution: "unresolved-root"}, nil
	default:
		// parent_ref may still name an absent external ancestor. parent_id NULL is
		// nevertheless the actual current corpus root, matching chain semantics.
		return DraftNavigation{Resolution: "resolved", ReplyToEntry: parent.String, RootID: root.String}, nil
	}
}
