package corpus

import (
	"database/sql"
	"fmt"
	"time"
)

// Cursor is how far a walk of one container got.
//
// The pair that matters is Position and Frontier. Position resumes a walk that
// was interrupted; Frontier is the floor a finished walk established, below
// which a later run need not look. A cursor holding a Frontier and no Position
// describes a container that is fully covered as of SucceededAt.
type Cursor struct {
	Source    string
	Container string
	Position  string // source-defined; empty once the walk completed
	// Frontier is the newest entry timestamp a completed walk covered. Zero
	// means no walk of this container has ever finished, which is not the same
	// as an empty container — see Walked.
	Frontier    time.Time
	Complete    bool
	Walked      int // entries walked since the last completion
	UpdatedAt   time.Time
	SucceededAt time.Time
	Exists      bool
}

// LoadCursor reads a cursor. A container never walked returns a zero Cursor with
// Exists false rather than an error: "never started" is a normal state and every
// caller would otherwise have to special-case sql.ErrNoRows.
func LoadCursor(s *Store, source, container string) (Cursor, error) {
	c := Cursor{Source: source, Container: container}
	var pos sql.NullString
	var frontier, succeeded sql.NullInt64
	var updated int64
	err := s.db.QueryRow(`
		select position, frontier, complete, walked, updated_at, succeeded_at
		  from cursors where source=? and container=?`, source, container).
		Scan(&pos, &frontier, &c.Complete, &c.Walked, &updated, &succeeded)
	switch {
	case err == sql.ErrNoRows:
		return c, nil
	case err != nil:
		return c, fmt.Errorf("loading %s cursor for %q: %w", source, container, err)
	}
	c.Exists = true
	c.Position = pos.String
	if frontier.Valid {
		c.Frontier = time.Unix(frontier.Int64, 0).UTC()
	}
	if succeeded.Valid {
		c.SucceededAt = time.Unix(succeeded.Int64, 0).UTC()
	}
	c.UpdatedAt = time.Unix(updated, 0).UTC()
	return c, nil
}

// SaveProgress records an unfinished walk. Called between pages, so a kill
// leaves a cursor that resumes at the next page rather than the first: the point
// of the table is that the work already paid for is not paid for twice.
//
// Frontier is deliberately untouched. Advancing it here would let a walk killed
// on its second page certify coverage of everything newer than that page, when
// the pages below it were never read.
func SaveProgress(s *Store, source, container, position string, walked int) error {
	now := time.Now().Unix()
	_, err := s.db.Exec(`
		insert into cursors(source, container, position, complete, walked, updated_at)
		values (?, ?, ?, 0, ?, ?)
		on conflict(source, container) do update set
		  position=excluded.position, complete=0,
		  walked=excluded.walked, updated_at=excluded.updated_at`,
		source, container, position, walked, now)
	if err != nil {
		return fmt.Errorf("saving %s cursor for %q: %w", source, container, err)
	}
	return nil
}

// SaveComplete records a walk that reached the end of its container. frontier is
// the newest entry timestamp the walk saw; a zero frontier leaves the stored one
// alone, since a top-up that legitimately found nothing new must not erase the
// floor an earlier walk established.
func SaveComplete(s *Store, source, container string, frontier time.Time, walked int) error {
	now := time.Now().Unix()
	var f interface{}
	if !frontier.IsZero() {
		f = frontier.Unix()
	}
	_, err := s.db.Exec(`
		insert into cursors(source, container, position, frontier, complete, walked,
		                    updated_at, succeeded_at)
		values (?, ?, null, ?, 1, ?, ?, ?)
		on conflict(source, container) do update set
		  position=null, complete=1, walked=excluded.walked,
		  updated_at=excluded.updated_at, succeeded_at=excluded.succeeded_at,
		  -- max(), not excluded: a bounded top-up of an older window must not
		  -- drag the floor backwards and re-open ground already covered.
		  frontier=max(coalesce(excluded.frontier, 0), coalesce(cursors.frontier, 0))`,
		source, container, f, walked, now, now)
	if err != nil {
		return fmt.Errorf("completing %s cursor for %q: %w", source, container, err)
	}
	return nil
}

// Cursors lists every cursor, newest activity first, for `corpus stats` to show
// which containers are covered and which were left mid-walk.
func Cursors(s *Store) ([]Cursor, error) {
	rows, err := s.db.Query(`
		select source, container, position, frontier, complete, walked,
		       updated_at, succeeded_at
		  from cursors order by updated_at desc`)
	if err != nil {
		return nil, fmt.Errorf("listing cursors: %w", err)
	}
	defer rows.Close()
	var out []Cursor
	for rows.Next() {
		c := Cursor{Exists: true}
		var pos sql.NullString
		var frontier, succeeded sql.NullInt64
		var updated int64
		if err := rows.Scan(&c.Source, &c.Container, &pos, &frontier, &c.Complete,
			&c.Walked, &updated, &succeeded); err != nil {
			return nil, err
		}
		c.Position = pos.String
		if frontier.Valid {
			c.Frontier = time.Unix(frontier.Int64, 0).UTC()
		}
		if succeeded.Valid {
			c.SucceededAt = time.Unix(succeeded.Int64, 0).UTC()
		}
		c.UpdatedAt = time.Unix(updated, 0).UTC()
		out = append(out, c)
	}
	return out, rows.Err()
}
