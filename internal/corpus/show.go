package corpus

import (
	"database/sql"
	"errors"
	"fmt"
	"sort"
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
		       par.ext_id, e.parent_ref, e.body_html is not null and e.body_html != ''
		from entries e
		left join people p   on p.id = e.person_id
		left join entries par on par.id = e.parent_id
		where e.ext_id = ?`, extID).
		Scan(&e.ID, &e.ExtID, &e.Source, &e.Quoted, &ts, &tz, &off,
			&author, &subject, &body, &container, &permalink, &parent, &parentRef,
			&e.HasOriginal)
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

// chainRow is one entry of a chain's reachable set, with just enough of it to be
// placed: who it answers, and the clock and id that order two entries the reply graph
// leaves unrelated.
type chainRow struct {
	id     int64
	extID  string
	parent int64  // 0 when the entry has no parent row
	ts     string // the stored timestamp text, as `order by e.ts` compared it
}

// Chain returns every entry reachable from the one named, in conversation order:
// an entry never before a parent that is also in the set, and in time order
// wherever the reply graph leaves a choice.
//
// Reachability is followed in BOTH directions — ancestors and descendants — so
// naming any message in a conversation returns the conversation. Naming only a
// root would be useless in practice, because search reports the entry that
// matched, not the root.
//
// Conversation order used to be `order by e.ts`, which is the same thing on every
// chain whose timestamps are instants and wrong on the ones where they are not. A
// message recovered from someone's quotation has no Date header of its own: its ts
// is the wall clock the quoter's client wrote for it, read as UTC (see
// unnest.Attribution.Sent), so it can be hours after the replies that came before
// it — and sorting on it drew a reply above the message it answers. The zone behind
// that clock is ambiguous by construction (tzinfer's job is to report that, not to
// choose), so nothing may lean on it to place an edge: the edge decides, and the
// clock only breaks ties. The page build (chronological.ts) and the tree view
// (tree.ts) keep the same promise on the client, which is why this end of it is
// theirs to trust.
//
// A chain is not ordered by the store's own ids either: an id says when we heard of
// a message, not when it was sent, and a recovered entry is inserted when its quoter
// is ingested.
func (s *Store) Chain(extID string) ([]Shown, error) {
	var id int64
	if err := s.db.QueryRow(`select id from entries where ext_id = ?`, extID).Scan(&id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%q: %w", extID, ErrNotFound)
		}
		return nil, err
	}
	// The reachable set, unordered: the SQL sort that used to be here cannot express
	// conversation order (a parent's clock may be the later of the two), so it is
	// done in Go where the graph is visible. The CTE carries each row's parent id
	// out for that reason alone.
	//
	// `down` is seeded with the ancestors as well as with the undescended roots, which
	// is the same set on a tree — every ancestor is under the root it was reached from
	// — and the only way a parent cycle comes back whole: a cycle has no root for
	// `root` to find, so without the seed the whole component would be dropped and the
	// query would answer a chain that exists with an empty list. `union` rather than
	// `union all` at every step is what terminates the walk inside a cycle.
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
		  select id from root union select id from up union
		  select e.id from entries e join down on e.parent_id = down.id
		)
		select e.id, e.ext_id, coalesce(e.parent_id, 0), e.ts
		  from entries e join down on e.id = down.id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var set []chainRow
	for rows.Next() {
		var r chainRow
		if err := rows.Scan(&r.id, &r.extID, &r.parent, &r.ts); err != nil {
			return nil, err
		}
		set = append(set, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	ids := chainOrder(set)
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

// chainOrder places a chain's reachable set: parents first, and time order between
// everything the graph does not relate, which is the order the reader meets the
// conversation in.
//
// Kahn's algorithm, with the eligible entries scanned in (ts, id) order: take the
// earliest entry whose parent is either already placed or not in the set at all.
// Parents come before children by construction, and the clock only ever decides
// between entries that are not ancestor and descendant — which is exactly as much
// as a clock with a recovered entry in it can be trusted with.
//
// A parent cycle is not forbidden by the schema. Its members are then never
// eligible, and a pass with nothing eligible takes the earliest entry outright
// rather than stopping: every reachable row is returned, the same as the plain sort
// returned every row, and a cycle costs the chain only the order within itself. The
// cost of the scan-the-whole-set loop is n² comparisons on a chain of n entries,
// which a thread is small enough to pay.
func chainOrder(set []chainRow) []string {
	byID := make(map[int64]int, len(set))
	for i, r := range set {
		byID[r.id] = i
	}
	ordered := make([]int, len(set))
	for i := range set {
		ordered[i] = i
	}
	sort.Slice(ordered, func(a, b int) bool {
		x, y := set[ordered[a]], set[ordered[b]]
		if x.ts != y.ts {
			return x.ts < y.ts
		}
		return x.id < y.id
	})
	placed := make(map[int64]bool, len(set))
	out := make([]string, 0, len(set))
	for len(out) < len(set) {
		take := -1
		first := -1
		for _, i := range ordered {
			r := set[i]
			if placed[r.id] {
				continue
			}
			if first < 0 {
				first = i
			}
			// Eligible unless a parent is still waiting to be placed. A parent that is
			// not in the set cannot block anything — the set is closed both ways, so
			// this is a guard rather than a case.
			if _, in := byID[r.parent]; r.parent != 0 && in && !placed[r.parent] {
				continue
			}
			take = i
			break
		}
		if take < 0 {
			// Nothing is eligible, so what is left is a cycle: the earliest of its
			// members is taken out of order, and the rest follow as their own parents
			// are placed. `first` is always set here — the loop runs only while
			// something is unplaced.
			take = first
		}
		placed[set[take].id] = true
		out = append(out, set[take].extID)
	}
	return out
}
