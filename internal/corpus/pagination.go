package corpus

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"
)

// ChainCursor continues a browse in (whole-chain last DESC, root ext ID ASC) order.
// It is not an entry-time filter: older members must not resurrect a shown chain.
type ChainCursor struct {
	Last      time.Time `json:"last"`
	RootExtID string    `json:"root"`
}

func (c ChainCursor) String() string {
	b, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(b)
}

func ParseChainCursor(raw string) (*ChainCursor, error) {
	b, err := base64.RawURLEncoding.DecodeString(raw)
	var c ChainCursor
	if err != nil || json.Unmarshal(b, &c) != nil || c.Last.IsZero() || c.RootExtID == "" {
		return nil, fmt.Errorf("invalid chain cursor")
	}
	return &c, nil
}

// browseCandidates limits chains, not entries. An entry candidate cap can hide
// roots behind a chatty thread, and applying the cursor to entries repeats chains
// whose older members cross the boundary.
func (s *Store) browseCandidates(q Query) ([]candidate, error) {
	where, args := q.filtersForActivity(true)
	reactionScope := "1"
	var reactionArgs []any
	if q.AccountID != "" {
		reactionScope = "exists(select 1 from gmail_copies rc where rc.entry_id=ra.target_id and rc.account_id=?)"
		reactionArgs = []any{q.AccountID}
	}
	spanScope, spanArgs := accountEntrySQL("e", q.AccountID)
	continuation := "1"
	callArgs := []any{walkDepthCap, walkDepthCap}
	callArgs = append(callArgs, reactionArgs...)
	callArgs = append(callArgs, args...)
	callArgs = append(callArgs, walkDepthCap)
	callArgs = append(callArgs, spanArgs...)
	if q.After != nil {
		continuation = "(last < ? or (last = ? and ext_id > ?))"
		callArgs = append(callArgs, q.After.Last.Unix(), q.After.Last.Unix(), q.After.RootExtID)
	}
	callArgs = append(callArgs, q.Limit)
	rows, err := s.db.Query(`
 with recursive up(start, cur, depth) as (
   select id, id, 0 from entries
   union all
   select u.start, e.parent_id, u.depth+1 from up u join entries e on e.id=u.cur
   where e.parent_id is not null and u.depth < ?
 ), membership as (
   select u.start, u.cur as root from up u join entries e on e.id=u.cur
   where e.parent_id is null or u.depth >= ?
 ), matched as (
   select coalesce(ra.target_id,e.id) as id, max(e.ts) as ts, m.root from entries e
   left join reaction_active ra on ra.source_id=e.id and `+reactionScope+`
   join membership m on m.start=coalesce(ra.target_id,e.id)
   left join mail_detail md on md.entry_id=e.id
   left join slack_detail sd on sd.entry_id=e.id
   where `+where+` group by coalesce(ra.target_id,e.id),m.root
 ), down(root, id, depth) as (
   select distinct root, root, 0 from matched
   union all
   select d.root, e.id, d.depth+1 from down d join entries e on e.parent_id=d.id
   where d.depth < ?
 ), activity(root,id) as (
   select root,id from down union select d.root,ra.source_id from down d join reaction_active ra on ra.target_id=d.id
 ), spans as (
   select d.root, r.ext_id, max(e.ts) as last from activity d
   join entries e on e.id=d.id join entries r on r.id=d.root where `+spanScope+` group by d.root
 ), page as (
   select root from spans where `+continuation+` order by last desc, ext_id asc limit ?
 )
 select m.id, m.root from matched m join page p on p.root=m.root order by m.ts desc, m.id desc`, callArgs...)
	if err != nil {
		return nil, fmt.Errorf("browsing chains: %w", err)
	}
	defer rows.Close()
	var out []candidate
	for rows.Next() {
		var id, root int64
		if err := rows.Scan(&id, &root); err != nil {
			return nil, err
		}
		out = append(out, candidate{id: id, root: root, score: 1 / (rrfK + float64(len(out)+1))})
	}
	return out, rows.Err()
}
