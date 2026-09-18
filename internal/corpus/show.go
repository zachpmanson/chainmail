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
	// PersonID is the person the corpus resolved this entry's sender to, and 0
	// where it resolved nobody. It is the handle a client needs for anything the
	// reader decides about that person rather than about this message — the
	// sender's reading style below is the first of those — and it is served as
	// itself because the name and the address on the entry are what the corpus
	// found, not who it found them to be.
	PersonID int64
	// PreferOriginal is whether the reader reads this entry's sender as that sender
	// wrote them, which is a choice about the person above (see people's
	// prefer_original). Carried on the entry rather than looked up by the client
	// because every bubble draws the control and every bubble would otherwise need
	// a second read to know what to draw; it is one boolean off a join this query
	// already makes. False where there is no person to hold the answer, which is
	// also what an entry whose sender the corpus cannot name gets.
	PreferOriginal bool
	// TZOffset is minutes east of UTC as the source stated it, nil when it stated
	// none. Carried alongside TZ because a label does not determine an offset, so
	// a caller placing this entry's wall clock has nothing else to place it with.
	TZOffset *int
	Author   string
	Subject  string
	Body     string
	// HasOriginal is whether this entry has a text/html part of its own filed in
	// the corpus — the sender's own markup, before any of the renderer's rules get
	// to it. It is a boolean and not the part because the two reads want opposite
	// things from it: every entry in a chain read has to say whether the reader may
	// ask to see the original, and answering that by carrying the markup would put
	// a chain's worth of it on every read. A reader who asks gets the one part they
	// asked for, from OriginalHTML.
	HasOriginal bool
	Container   string
	Permalink   string
	Parent      string // parent's ext_id, empty at a chain root
	ParentRef   string // what it names as its parent, resolved or not

	// Sightings is every place this entry was found. A message quoted in five
	// forwards has five, which is the evidence that it mattered.
	Sightings []Sighting
	// Participants in role order.
	Participants []Participant
	// Attachments are the files this entry carries, in the order its client put
	// them in. The rows are the corpus's own record of what the source stated —
	// name, type, size, the place it can be fetched from — plus whatever the media
	// phase has since made of it (the digest of bytes we hold, or why we never
	// will). Nothing here says how a renderer should draw one; see
	// ShownAttachment.
	Attachments []ShownAttachment
}

// ShownAttachment is one file an entry carries, as the corpus holds it.
//
// It is deliberately the raw material of an attachment chip rather than the chip:
// the wording ("PDF · 93 KB"), the decision of what a click does with bytes we
// hold, and whether the file is shown in a window are all made from this in one
// place (see spec.AttachmentOf), so a page build and the corpus's own read cannot
// describe one file two ways. What is NOT here is who the file belongs to: the
// entry's permalink is, and a caller turns that into wherever the chip should go.
type ShownAttachment struct {
	Name string
	// Mime is what the source called it, empty when it called it nothing: a mail
	// part with no Content-Type is a file, not an unknown one.
	Mime      string
	Size      int64
	Permalink string
	// SourceRef is how to ask the source for the bytes — a Gmail part id, in
	// practice. Empty when there is nothing to fetch by.
	SourceRef string
	// BlobSHA is set once the bytes are in the corpus, and is what a client asks
	// for instead of sending the reader back to the source.
	BlobSHA string
	// Skip is why the corpus will never hold these bytes, once a pull has decided.
	Skip string
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
		       par.ext_id, e.parent_ref, e.body_html is not null and e.body_html != '',
		       coalesce(e.person_id, 0), coalesce(p.prefer_original, 0)
		from entries e
		left join people p   on p.id = e.person_id
		left join entries par on par.id = e.parent_id
		where e.ext_id = ?`, extID).
		Scan(&e.ID, &e.ExtID, &e.Source, &e.Quoted, &ts, &tz, &off,
			&author, &subject, &body, &container, &permalink, &parent, &parentRef,
			&e.HasOriginal, &e.PersonID, &e.PreferOriginal)
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

	// The files, in the order they were stated: a message's attachment list is the
	// sender's, and rowid is the order the source gave them in (the rows are
	// replaced wholesale on every ingest, so rowid means the newest statement of
	// that order). Ordered explicitly rather than left to the planner.
	arows, err := s.db.Query(`
		select name, coalesce(mime, ''), size, coalesce(permalink, ''),
		       coalesce(source_ref, ''), coalesce(blob_sha, ''), coalesce(media_skip, '')
		from attachments where entry_id = ? order by rowid`, e.ID)
	if err != nil {
		return e, err
	}
	defer arows.Close()
	for arows.Next() {
		var a ShownAttachment
		if err := arows.Scan(&a.Name, &a.Mime, &a.Size, &a.Permalink, &a.SourceRef,
			&a.BlobSHA, &a.Skip); err != nil {
			return e, err
		}
		e.Attachments = append(e.Attachments, a)
	}
	if err := arows.Err(); err != nil {
		return e, err
	}

	e.Participants, err = Participants(s, e.ID)
	return e, err
}

// OriginalHTML returns the entry's own text/html part exactly as the sender's
// client wrote it: the markup a reader sees when they ask for the original rather
// than for this renderer's reading of it.
//
// It is kept apart from Show deliberately, because it is not part of reading an
// entry — it is the point of *not* reading it this way, and it is big (tens of
// kilobytes for a mail client's output). The caller is the server route that hands
// it to a shadow root, and everything that makes it presentable is spec's:
// OriginalBody decides what may go in, this only fetches bytes.
//
// An entry with no html part returns "" and no error: the corpus has the message,
// it simply has nothing the sender wrote to show for it. A reader asking for an
// original that does not exist is the caller's 404 to make, not an internal error.
func (s *Store) OriginalHTML(extID string) (string, error) {
	var part sql.NullString
	err := s.db.QueryRow(`select body_html from entries where ext_id = ?`, extID).Scan(&part)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("%q: %w", extID, ErrNotFound)
	}
	if err != nil {
		return "", err
	}
	return part.String, nil
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
