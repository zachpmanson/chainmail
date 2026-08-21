package spec

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/zachpmanson/chainmail/internal/corpus"
)

// entryRow is one corpus entry with everything the spec needs, gathered in as
// few queries as the shape of the store allows.
type entryRow struct {
	ID        int64
	ParentID  int64 // 0 when unset or unresolved
	ParentRef string
	Kind      string
	TS        time.Time
	TZ        string
	TZOffset  *int // minutes east of UTC; nil when the source stated none
	Person    string
	Container string
	Subject   string
	ExtID     string
	GmailID   string
	From      string
	To        string
	Cc        string
	Direct    bool    // seen in the mailbox itself, not only inside a quote
	SeenIn    []int64 // entries this one was found quoted or forwarded inside
	Atts      []attRow
}

type attRow struct {
	Name string
	Mime string
	Size int64
}

// seeds resolves Options into the set of entry ids to start from, before the
// reply-graph closure is taken. Selection proper — searching, ranking — belongs
// to the caller; this only looks up what it was handed.
func seeds(db *sql.DB, opts Options) ([]int64, error) {
	set := map[int64]bool{}
	for _, id := range opts.EntryIDs {
		set[id] = true
	}
	add := func(query string, args []any) error {
		rows, err := db.Query(query, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				return err
			}
			set[id] = true
		}
		return rows.Err()
	}
	if len(opts.Containers) > 0 {
		ph, args := placeholders(opts.Containers)
		if err := add(`select id from entries where container in (`+ph+`)`, args); err != nil {
			return nil, fmt.Errorf("entries for containers: %w", err)
		}
	}
	if len(opts.ExtIDs) > 0 {
		ph, args := placeholders(opts.ExtIDs)
		if err := add(`select id from entries where ext_id in (`+ph+`)`, args); err != nil {
			return nil, fmt.Errorf("entries for ext ids: %w", err)
		}
	}
	return keys(set), nil
}

// closure expands a seed set to every entry connected to it by reply edges:
// ancestors first, then all their descendants. Rendering half a reply chain
// would show answers with no questions, and the reply graph is materialised in
// the store precisely so this is one query.
//
// Parents are single, so up-then-down reaches the whole connected component: the
// walk up ends at each seed's chain root, and the walk down from those roots
// covers every branch under them.
func closure(db *sql.DB, ids []int64) ([]int64, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	ph, args := placeholders(ids)
	rows, err := db.Query(`
		with recursive
		  seed(id) as (select id from entries where id in (`+ph+`)),
		  up(id) as (
		    select id from seed
		    union
		    select e.parent_id from entries e join up on e.id = up.id
		    where e.parent_id is not null
		  ),
		  down(id) as (
		    select id from up
		    union
		    select e.id from entries e join down on e.parent_id = down.id
		  )
		select id from down`, args...)
	if err != nil {
		return nil, fmt.Errorf("reply-graph closure: %w", err)
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// load reads the selected entries, in chronological (absolute UTC) order.
func load(store *corpus.Store, ids []int64) ([]*entryRow, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	db := store.DB()
	ph, args := placeholders(ids)
	rows, err := db.Query(`
		select e.id, coalesce(e.parent_id, 0), coalesce(e.parent_ref, ''), e.kind, e.ts,
		       coalesce(e.tz, ''), e.tz_offset, coalesce(p.display_name, ''),
		       coalesce(e.container, ''),
		       coalesce(e.subject, ''), e.ext_id, coalesce(d.gmail_id, ''),
		       coalesce(d.from_addr, ''), coalesce(d.to_addr, ''), coalesce(d.cc_addr, '')
		from entries e
		left join people p      on p.id = e.person_id
		left join mail_detail d on d.entry_id = e.id
		where e.id in (`+ph+`)
		order by e.ts, e.id`, args...)
	if err != nil {
		return nil, fmt.Errorf("loading entries: %w", err)
	}
	defer rows.Close()

	var out []*entryRow
	byID := map[int64]*entryRow{}
	for rows.Next() {
		var r entryRow
		var ts int64
		if err := rows.Scan(&r.ID, &r.ParentID, &r.ParentRef, &r.Kind, &ts, &r.TZ,
			&r.TZOffset, &r.Person, &r.Container, &r.Subject, &r.ExtID, &r.GmailID, &r.From, &r.To, &r.Cc); err != nil {
			return nil, err
		}
		r.TS = time.Unix(ts, 0).UTC()
		out = append(out, &r)
		byID[r.ID] = &r
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if err := loadSightings(db, ph, args, byID); err != nil {
		return nil, err
	}
	if err := loadAttachments(db, ph, args, byID); err != nil {
		return nil, err
	}
	return out, nil
}

func loadSightings(db *sql.DB, ph string, args []any, byID map[int64]*entryRow) error {
	rows, err := db.Query(`
		select entry_id, kind, coalesce(seen_in, 0) from sightings
		where entry_id in (`+ph+`)`, args...)
	if err != nil {
		return fmt.Errorf("loading sightings: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id, seenIn int64
		var kind string
		if err := rows.Scan(&id, &kind, &seenIn); err != nil {
			return err
		}
		r, ok := byID[id]
		if !ok {
			continue
		}
		if kind == "direct" {
			r.Direct = true
		} else if seenIn != 0 {
			r.SeenIn = append(r.SeenIn, seenIn)
		}
	}
	return rows.Err()
}

func loadAttachments(db *sql.DB, ph string, args []any, byID map[int64]*entryRow) error {
	rows, err := db.Query(`
		select entry_id, name, coalesce(mime, ''), coalesce(size, 0) from attachments
		where entry_id in (`+ph+`) order by rowid`, args...)
	if err != nil {
		return fmt.Errorf("loading attachments: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var a attRow
		if err := rows.Scan(&id, &a.Name, &a.Mime, &a.Size); err != nil {
			return err
		}
		if r, ok := byID[id]; ok {
			r.Atts = append(r.Atts, a)
		}
	}
	return rows.Err()
}

// placeholders builds an "?,?,?" list and the matching args for an in-clause.
func placeholders[T any](vs []T) (string, []any) {
	args := make([]any, len(vs))
	for i, v := range vs {
		args[i] = v
	}
	return strings.TrimSuffix(strings.Repeat("?,", len(vs)), ","), args
}

func keys(set map[int64]bool) []int64 {
	out := make([]int64, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	return out
}
