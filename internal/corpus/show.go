package corpus

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Shown is one entry, resolved for reading rather than for ranking.
//
// Search returns what matched; this returns what a thing actually says. The
// fields a reader needs to trust it — where it was seen, whether it is a real
// mailbox message or was recovered from someone's quoted history — are part of
// the entry here rather than something the caller has to join for.
type Shown struct {
	ID     int64
	ExtID  string
	Source string
	Quoted bool
	TS     time.Time
	TZ     string
	// TZOffset is minutes east of UTC as the source stated it, nil when it stated
	// none. Carried alongside TZ because a label does not determine an offset, so
	// a caller placing this entry's wall clock has nothing else to place it with.
	TZOffset  *int
	Author    string
	Subject   string
	Body      string
	Container string
	Permalink string
	Parent    string // parent's ext_id, empty at a chain root
	ParentRef string // what it names as its parent, resolved or not

	// Sightings is every place this entry was found. A message quoted in five
	// forwards has five, which is the evidence that it mattered.
	Sightings []Sighting
	// Participants in role order.
	Participants []Participant
}

// Sighting is one place an entry was seen.
type Sighting struct {
	Kind   string // direct | quoted | forwarded
	SeenIn string // ext_id of the message it was found inside, empty when direct
	Detail string
}

// ErrNotFound is returned when no entry carries the given ext_id.
var ErrNotFound = errors.New("no entry with that id")

// Show resolves one entry by ext_id.
//
// The id is the one search prints, so a reader can move from a result to the
// thing itself without translating between id spaces — which was impossible
// while the only lookup took a Gmail id that search never emits.
func (s *Store) Show(extID string) (Shown, error) {
	var e Shown
	var ts int64
	var off sql.NullInt64
	var tz, author, subject, body, container, permalink, parent, parentRef sql.NullString
	err := s.db.QueryRow(`
		select e.id, e.ext_id, e.source, e.quoted, e.ts, e.tz, e.tz_offset,
		       p.display_name, e.subject, e.body_text, e.container, e.permalink,
		       par.ext_id, e.parent_ref
		from entries e
		left join people p   on p.id = e.person_id
		left join entries par on par.id = e.parent_id
		where e.ext_id = ?`, extID).
		Scan(&e.ID, &e.ExtID, &e.Source, &e.Quoted, &ts, &tz, &off,
			&author, &subject, &body, &container, &permalink, &parent, &parentRef)
	if errors.Is(err, sql.ErrNoRows) {
		return e, fmt.Errorf("%q: %w", extID, ErrNotFound)
	}
	if err != nil {
		return e, err
	}
	e.TS = time.Unix(ts, 0)
	if off.Valid {
		m := int(off.Int64)
		e.TZOffset = &m
	}
	e.TZ, e.Author, e.Subject = tz.String, author.String, subject.String
	e.Body, e.Container, e.Permalink = body.String, container.String, permalink.String
	e.Parent, e.ParentRef = parent.String, parentRef.String

	rows, err := s.db.Query(`
		select s.kind, coalesce(h.ext_id, ''), coalesce(s.detail, '')
		from sightings s left join entries h on h.id = s.seen_in
		where s.entry_id = ? order by s.kind`, e.ID)
	if err != nil {
		return e, err
	}
	defer rows.Close()
	for rows.Next() {
		var g Sighting
		if err := rows.Scan(&g.Kind, &g.SeenIn, &g.Detail); err != nil {
			return e, err
		}
		e.Sightings = append(e.Sightings, g)
	}
	if err := rows.Err(); err != nil {
		return e, err
	}

	e.Participants, err = Participants(s, e.ID)
	return e, err
}

// Chain returns every entry reachable from the one named, in time order.
//
// Reachability is followed in BOTH directions — ancestors and descendants — so
// naming any message in a conversation returns the conversation. Naming only a
// root would be useless in practice, because search reports the entry that
// matched, not the root.
func (s *Store) Chain(extID string) ([]Shown, error) {
	var id int64
	if err := s.db.QueryRow(`select id from entries where ext_id = ?`, extID).Scan(&id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%q: %w", extID, ErrNotFound)
		}
		return nil, err
	}
	rows, err := s.db.Query(`
		with recursive up(id) as (
		  select ? union
		  select e.parent_id from entries e join up on e.id = up.id
		    where e.parent_id is not null
		),
		root(id) as (
		  select id from up where id not in (
		    select e.id from entries e join up on e.id = up.id where e.parent_id is not null)
		),
		down(id) as (
		  select id from root union
		  select e.id from entries e join down on e.parent_id = down.id
		)
		select e.ext_id from entries e join down on e.id = down.id order by e.ts`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var x string
		if err := rows.Scan(&x); err != nil {
			return nil, err
		}
		ids = append(ids, x)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]Shown, 0, len(ids))
	for _, x := range ids {
		sh, err := s.Show(x)
		if err != nil {
			return nil, err
		}
		out = append(out, sh)
	}
	return out, nil
}
