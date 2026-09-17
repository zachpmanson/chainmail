// Package corpus is the local store: every message from every source, with the
// reply graph materialised rather than re-derived on each query.
package corpus

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// Source values for Entry.Source.
const (
	SourceMail  = "mail"
	SourceSlack = "slack"
)

// Entry is one atomic thing someone said, whatever the medium.
type Entry struct {
	ID     int64
	Source string
	ExtID  string
	Kind   string // message | note
	TS     time.Time
	TZ     string // as stated by the source; empty when it stated none
	// TZOffset is minutes east of UTC, as the source stated it. Kept alongside
	// the label because a label alone cannot render the sender's own clock, and
	// an unrecognised label would otherwise force a UTC fallback.
	TZOffset  *int
	PersonID  int64
	Container string
	ParentRef string // raw Message-ID / thread_ts; resolved separately
	Subject   string
	BodyHTML  string
	BodyText  string
	Permalink string
	// Derived marks an entry that is a MODIFIED copy of a quoted message (its
	// parent points at the base it edited). Set at ingest from the graded overlap
	// decision; the renderer reads it to hoist the copy into its host as an
	// inline edit rather than rendering a floating duplicate.
	Derived bool
}

// Mail holds the fields that only make sense for a mail entry.
type Mail struct {
	GmailID    string
	MessageID  string
	InReplyTo  string
	References []string
	From       string
	To         string
	Cc         string
	Labels     []string
}

// Attachment is the metadata a source states about a file on a message. The
// bytes, when they have been pulled, live in `blobs` and are reached through the
// attachment row's blob_sha — see blob.go. Ingest never carries bytes: a mail
// part costs a Gmail round trip, and `slurp` does not spend those.
type Attachment struct {
	Name      string
	Mime      string
	Size      int64
	Permalink string
	SourceRef string
}

// Store is a corpus database.
type Store struct {
	db *sql.DB

	// bodies holds each mail body's reduction, keyed by entry id and guarded by
	// bodiesMu. See reduced for why the cache key is the body itself.
	bodiesMu sync.Mutex
	bodies   map[int64]reduction
}

// Open opens (creating if absent) a corpus at path and applies any pending
// migrations. Pass ":memory:" for a throwaway.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", path, err)
	}
	// WAL suits the access pattern: one writer (the slurper), many readers.
	// It is a no-op for :memory:, which is why the error is tolerated.
	if _, err := db.Exec(`pragma journal_mode=wal`); err != nil && path != ":memory:" {
		return nil, fmt.Errorf("enabling wal: %w", err)
	}
	if _, err := db.Exec(`pragma foreign_keys=on`); err != nil {
		return nil, fmt.Errorf("enabling foreign keys: %w", err)
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

// DB exposes the handle for queries that do not warrant a method.
func (s *Store) DB() *sql.DB { return s.db }

func (s *Store) migrate() error {
	if _, err := s.db.Exec(`create table if not exists schema_version(v integer primary key)`); err != nil {
		return fmt.Errorf("creating schema_version: %w", err)
	}
	var at int
	if err := s.db.QueryRow(`select coalesce(max(v),0) from schema_version`).Scan(&at); err != nil {
		return fmt.Errorf("reading schema_version: %w", err)
	}
	for i, m := range migrations {
		v := i + 1
		if v <= at {
			continue
		}
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(m); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %d: %w", v, err)
		}
		if _, err := tx.Exec(`insert into schema_version(v) values (?)`, v); err != nil {
			tx.Rollback()
			return fmt.Errorf("recording migration %d: %w", v, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("committing migration %d: %w", v, err)
		}
	}
	return nil
}

// BodySHA is the content hash used for idempotent re-ingest. Whitespace is
// collapsed first so a reflowed body is not mistaken for an edited one.
func BodySHA(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte(strings.Join(strings.Fields(p), " ")))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// PutResult reports what an upsert did, which is what makes a re-slurp
// meaningful rather than merely harmless.
type PutResult struct {
	ID      int64
	Created bool
	Changed bool // existed, but the body hash differed
	// Skipped: the caller declined to store this message. Not an error —
	// a Gmail draft is real evidence someone composed, but it was never
	// sent and must not enter the timeline as first-class mail.
	Skipped bool
}

// Put inserts or updates an entry, keyed on (source, ext_id). Re-ingesting
// identical content is a no-op; a differing body is recorded and reported, which
// is what the renderer surfaces as "revised".
func (s *Store) Put(e Entry, m *Mail, atts []Attachment) (PutResult, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return PutResult{}, err
	}
	defer tx.Rollback()
	res, err := s.put(tx, e, m, nil, atts)
	if err != nil {
		return res, err
	}
	return res, tx.Commit()
}

// put is the whole write for one entry — the row, the per-source detail, the
// attachments and both FTS indexes — inside a transaction the caller owns. It
// exists so a source detail row cannot be committed apart from its entry: a
// slack_detail row without its entry, or an entry whose detail never landed, is
// a hole no re-ingest would notice, since the entry already exists.
func (s *Store) put(tx *sql.Tx, e Entry, m *Mail, sd *Slack, atts []Attachment) (PutResult, error) {
	if e.Source == "" || e.ExtID == "" {
		return PutResult{}, errors.New("entry needs a source and an ext_id")
	}
	if e.Kind == "" {
		e.Kind = "message"
	}
	sha := BodySHA(e.BodyText, e.Subject)

	var res PutResult
	var err error

	// The old subject/body are needed to delete this row's terms from the FTS
	// indexes: external-content tables do not store the text, so FTS5 cannot work
	// out which terms to remove from the rowid alone.
	var oldID int64
	var oldSHA, oldSubject, oldBody string
	err = tx.QueryRow(`
		select id, body_sha, coalesce(subject,''), coalesce(body_text,'')
		from entries where source=? and ext_id=?`,
		e.Source, e.ExtID).Scan(&oldID, &oldSHA, &oldSubject, &oldBody)
	switch {
	case err == sql.ErrNoRows:
		res.Created = true
	case err != nil:
		return res, fmt.Errorf("looking up %s: %w", e.ExtID, err)
	default:
		res.ID = oldID
		res.Changed = oldSHA != sha
	}

	var personID any
	if e.PersonID != 0 {
		personID = e.PersonID
	}
	row := tx.QueryRow(`
		insert into entries (source, ext_id, kind, ts, tz, tz_offset, person_id,
		                     container, parent_ref, subject, body_html, body_text,
		                     permalink, body_sha, derived, ingested_at)
		values (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		on conflict(source, ext_id) do update set
		  kind=excluded.kind, ts=excluded.ts, tz=excluded.tz,
		  tz_offset=excluded.tz_offset,
		  person_id=coalesce(excluded.person_id, entries.person_id),
		  container=excluded.container, parent_ref=excluded.parent_ref,
		  subject=excluded.subject, body_html=excluded.body_html,
		  body_text=excluded.body_text, permalink=excluded.permalink,
		  derived=coalesce(entries.derived, excluded.derived),
		  body_sha=excluded.body_sha, ingested_at=excluded.ingested_at
		returning id`,
		e.Source, e.ExtID, e.Kind, e.TS.UTC().Unix(), nullStr(e.TZ), nullInt(e.TZOffset),
		personID, nullStr(e.Container), nullStr(e.ParentRef), nullStr(e.Subject),
		nullStr(e.BodyHTML), e.BodyText, nullStr(e.Permalink), sha, boolInt(e.Derived), time.Now().Unix())
	if err := row.Scan(&res.ID); err != nil {
		return res, fmt.Errorf("upserting %s: %w", e.ExtID, err)
	}

	if m != nil {
		if _, err := tx.Exec(`
			insert into mail_detail (entry_id, gmail_id, message_id, in_reply_to, refs,
			                         from_addr, to_addr, cc_addr, labels)
			values (?,?,?,?,?,?,?,?,?)
			on conflict(entry_id) do update set
			  gmail_id=excluded.gmail_id, message_id=excluded.message_id,
			  in_reply_to=excluded.in_reply_to, refs=excluded.refs,
			  from_addr=excluded.from_addr, to_addr=excluded.to_addr,
			  cc_addr=excluded.cc_addr, labels=excluded.labels`,
			res.ID, nullStr(m.GmailID), nullStr(m.MessageID), nullStr(m.InReplyTo),
			nullStr(strings.Join(m.References, " ")), nullStr(m.From), nullStr(m.To),
			nullStr(m.Cc), nullStr(strings.Join(m.Labels, ","))); err != nil {
			return res, fmt.Errorf("mail detail for %s: %w", e.ExtID, err)
		}
	}

	if sd != nil {
		if err := putSlackDetail(tx, res.ID, *sd); err != nil {
			return res, fmt.Errorf("slack detail for %s: %w", e.ExtID, err)
		}
	}

	// Attachments are replaced wholesale: the source is authoritative, and a
	// message's attachment list does not change independently of its body.
	//
	// Bytes pulled earlier must survive that replacement, so the links are read
	// out first and re-applied by (source_ref) afterwards. A blob is not owned by
	// the row that points at it: a re-slurp deletes the row, and losing the only
	// reference to bytes nobody asked for twice would be a silent regression in
	// exactly the case this was built for — the hourly walk.
	links, err := mediaLinks(tx, res.ID)
	if err != nil {
		return res, err
	}
	if _, err := tx.Exec(`delete from attachments where entry_id=?`, res.ID); err != nil {
		return res, err
	}
	for _, a := range atts {
		if _, err := tx.Exec(`
			insert into attachments (entry_id, name, mime, size, permalink, source_ref)
			values (?,?,?,?,?,?)`,
			res.ID, a.Name, nullStr(a.Mime), a.Size, nullStr(a.Permalink),
			nullStr(a.SourceRef)); err != nil {
			return res, fmt.Errorf("attachment %q: %w", a.Name, err)
		}
	}
	if err := restoreMediaLinks(tx, res.ID, links); err != nil {
		return res, err
	}

	if err := s.reindex(tx, res.ID, res.Created, oldSubject, oldBody); err != nil {
		return res, err
	}
	return res, nil
}

// reindex refreshes both FTS tables for one entry. External-content tables are
// not updated automatically, so this must follow every write — and a delete must
// pass the OLD column values, since FTS5 has no copy of the text to look up.
func (s *Store) reindex(tx *sql.Tx, id int64, created bool, oldSubject, oldBody string) error {
	if !created {
		if _, err := tx.Exec(
			`insert into entries_fts(entries_fts, rowid, subject, body_text) values ('delete', ?, ?, ?)`,
			id, oldSubject, oldBody); err != nil {
			return fmt.Errorf("un-indexing %d: %w", id, err)
		}
		if _, err := tx.Exec(
			`insert into entries_ident(entries_ident, rowid, body_text) values ('delete', ?, ?)`,
			id, oldBody); err != nil {
			return fmt.Errorf("un-indexing %d for identifiers: %w", id, err)
		}
	}
	if _, err := tx.Exec(`
		insert into entries_fts(rowid, subject, body_text)
		select id, coalesce(subject,''), coalesce(body_text,'') from entries where id=?`, id); err != nil {
		return fmt.Errorf("indexing %d: %w", id, err)
	}
	if _, err := tx.Exec(`
		insert into entries_ident(rowid, body_text)
		select id, coalesce(body_text,'') from entries where id=?`, id); err != nil {
		return fmt.Errorf("indexing %d for identifiers: %w", id, err)
	}
	return nil
}

// ReindexFTS rebuilds both search indexes from scratch. The shadow tables are
// external-content fts5: they cannot be cleared with a plain DELETE (that
// corrupts the segment list — count/match still work while snippet() throws
// malformed), so a rebuild goes through the virtual table's own commands,
// which is also how a wipe+re-ingest leaves them consistent.
func (s *Store) ReindexFTS() error {
	for _, tbl := range []string{"entries_fts", "entries_ident"} {
		if _, err := s.db.Exec("insert into " + tbl + "(" + tbl + ") values('delete-all')"); err != nil {
			return fmt.Errorf("clearing %s: %w", tbl, err)
		}
		if _, err := s.db.Exec("insert into " + tbl + "(" + tbl + ", rank) values('rebuild', 0)"); err != nil {
			return fmt.Errorf("rebuilding %s: %w", tbl, err)
		}
	}
	return nil
}

// Sight records that an entry was seen somewhere: directly in the mailbox, or
// quoted inside another entry.
func (s *Store) Sight(entryID, seenIn int64, kind, detail string) error {
	var in any
	if seenIn != 0 {
		in = seenIn
	}
	_, err := s.db.Exec(`
		insert into sightings (entry_id, seen_in, kind, detail) values (?,?,?,?)
		on conflict(entry_id, seen_in, kind) do update set detail=excluded.detail`,
		entryID, in, kind, nullStr(detail))
	return err
}

// ResolveParents links entries whose headers name a parent that the corpus
// holds. Re-runnable: a parent dangling today may arrive next week once a
// forward containing it is extracted. Returns how many edges it resolved.
//
// A message names its ancestry twice over — References, oldest first, and then
// In-Reply-To, which is the direct parent — and the newest of those ids that the
// corpus holds is the parent here. Reading the two as one list is what makes a
// reply whose own parent was never fetched land under its grandparent instead of
// standing as a root of its own.
//
// Scoped to mail. A parent_ref means a different thing in each source — a
// Message-ID here, a thread_ts in Slack — and without the scope a Slack entry
// whose thread_ts happened to equal some Message-ID would be given a mail
// parent, silently welding two conversations together. No real ts collides with
// a real Message-ID today, so this is a guard rather than a repair.
func (s *Store) ResolveParents() (int64, error) { return s.linkParents(false) }

// ReassertParents is ResolveParents over edges that are already drawn: where a
// message's own headers name a parent the corpus holds and the edge it has is to
// something else, the header's parent wins.
//
// An edge is written by whoever asks first. Ingest resolves parents once at the
// end of its walk, while the quoted pass links what a body nests *during* the
// walk, and the guard both writers use — fill a NULL parent and nothing else —
// means the first writer is the last word. That is the wrong precedence for a
// header: nesting is a reading of the text, and In-Reply-To is a statement about
// it, so the reading should not be able to keep the header's slot. Doing it here,
// after the fact, also repairs the edges that were decided that way before the
// quoted pass learned to leave a header alone — which is the whole existing
// corpus.
//
// Only where the header resolves. A message whose parent is not in the corpus
// keeps the edge it has, because that edge is the only thing placing it in a
// conversation at all — half a trail beats none. Returns how many edges changed.
func (s *Store) ReassertParents() (int64, error) { return s.linkParents(true) }

// linkParents is both of the above: one reading of the same rows, differing only
// in whether an edge that is already drawn may be replaced. Candidates are read
// in id order — a pass that can refuse an edge has to visit the same rows in the
// same order twice to be worth running twice.
func (s *Store) linkParents(overwrite bool) (int64, error) {
	type candidate struct {
		id, parent int64
		parentRef  string
		refs       string
	}

	// Read everything before writing anything: the same connection cannot walk a
	// cursor and write through it at once, and a half-applied pass would be a
	// graph with no record of where it stopped.
	rows, err := s.db.Query(`
		select e.id, coalesce(e.parent_id, 0), coalesce(e.parent_ref, ''), coalesce(d.refs, '')
		from entries e left join mail_detail d on d.entry_id = e.id
		where e.source = 'mail' and (e.parent_id is null or ?)
		order by e.id`, overwrite)
	if err != nil {
		return 0, fmt.Errorf("reading parents: %w", err)
	}
	var cands []candidate
	for rows.Next() {
		var c candidate
		if err := rows.Scan(&c.id, &c.parent, &c.parentRef, &c.refs); err != nil {
			rows.Close()
			return 0, err
		}
		if c.parentRef != "" || strings.TrimSpace(c.refs) != "" {
			cands = append(cands, c)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	rows.Close()

	// Every Message-ID the corpus can answer for. `min` because a twin collapse
	// can leave two rows claiming one Message-ID, and the answer has to be the
	// same one every run.
	held := map[string]int64{}
	ids, err := s.db.Query(`
		select message_id, min(entry_id) from mail_detail
		where message_id is not null and message_id <> '' group by message_id`)
	if err != nil {
		return 0, fmt.Errorf("reading message ids: %w", err)
	}
	for ids.Next() {
		var mid string
		var eid int64
		if err := ids.Scan(&mid, &eid); err != nil {
			ids.Close()
			return 0, err
		}
		held[mid] = eid
	}
	if err := ids.Err(); err != nil {
		ids.Close()
		return 0, err
	}
	ids.Close()

	var changed int64
	for _, c := range cands {
		// Newest last, so the walk below finds In-Reply-To before References:
		// the header that names one message is a stronger claim than the list
		// that names an ancestry.
		named := strings.Fields(c.refs)
		if c.parentRef != "" {
			named = append(named, c.parentRef)
		}
		var parent int64
		for i := len(named) - 1; i >= 0; i-- {
			if id, ok := held[named[i]]; ok {
				parent = id
				break
			}
		}
		if parent == 0 || parent == c.id || parent == c.parent {
			continue
		}
		// A header can name a message that is already below this one — a thread
		// quoted in full inside a reply names it both ways — and the walk reads a
		// ring as no chain at all, so the edge is refused rather than drawn.
		if bad, err := closesCycle(s.db, c.id, parent); err != nil {
			return changed, err
		} else if bad {
			continue
		}
		q := `update entries set parent_id = ? where id = ? and parent_id is null`
		if overwrite {
			q = `update entries set parent_id = ? where id = ? and parent_id is not ?`
		}
		args := []any{parent, c.id}
		if overwrite {
			args = append(args, parent)
		}
		r, err := s.db.Exec(q, args...)
		if err != nil {
			return changed, fmt.Errorf("linking parent of %d: %w", c.id, err)
		}
		n, err := r.RowsAffected()
		if err != nil {
			return changed, err
		}
		changed += n
	}
	return changed, nil
}

// Stats is a summary of what is in the corpus, and of what is missing.
type Stats struct {
	Entries    int64
	BySource   map[string]int64
	Unresolved int64 // parent_ref set but no matching entry: a known hole
	Roots      int64
	People     int64
}

func (s *Store) Stats() (Stats, error) {
	st := Stats{BySource: map[string]int64{}}
	if err := s.db.QueryRow(`select count(*) from entries`).Scan(&st.Entries); err != nil {
		return st, err
	}
	rows, err := s.db.Query(`select source, count(*) from entries group by source`)
	if err != nil {
		return st, err
	}
	defer rows.Close()
	for rows.Next() {
		var k string
		var n int64
		if err := rows.Scan(&k, &n); err != nil {
			return st, err
		}
		st.BySource[k] = n
	}
	if err := s.db.QueryRow(
		`select count(*) from entries where parent_ref is not null and parent_id is null`,
	).Scan(&st.Unresolved); err != nil {
		return st, err
	}
	if err := s.db.QueryRow(
		`select count(*) from entries where parent_id is null and parent_ref is null`,
	).Scan(&st.Roots); err != nil {
		return st, err
	}
	if err := s.db.QueryRow(`select count(*) from people`).Scan(&st.People); err != nil {
		return st, err
	}
	return st, nil
}

func nullInt(i *int) any {
	if i == nil {
		return nil
	}
	return *i
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}
