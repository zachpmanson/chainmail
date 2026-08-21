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

// Attachment is metadata only; bytes are never stored.
type Attachment struct {
	Name      string
	Mime      string
	Size      int64
	Permalink string
	SourceRef string
}

// Store is a corpus database.
type Store struct{ db *sql.DB }

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
		                     permalink, body_sha, ingested_at)
		values (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		on conflict(source, ext_id) do update set
		  kind=excluded.kind, ts=excluded.ts, tz=excluded.tz,
		  tz_offset=excluded.tz_offset,
		  person_id=coalesce(excluded.person_id, entries.person_id),
		  container=excluded.container, parent_ref=excluded.parent_ref,
		  subject=excluded.subject, body_html=excluded.body_html,
		  body_text=excluded.body_text, permalink=excluded.permalink,
		  body_sha=excluded.body_sha, ingested_at=excluded.ingested_at
		returning id`,
		e.Source, e.ExtID, e.Kind, e.TS.UTC().Unix(), nullStr(e.TZ), nullInt(e.TZOffset),
		personID, nullStr(e.Container), nullStr(e.ParentRef), nullStr(e.Subject),
		nullStr(e.BodyHTML), e.BodyText, nullStr(e.Permalink), sha, time.Now().Unix())
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

// ResolveParents links entries whose parent_ref now matches an entry that is
// present. Re-runnable: a parent dangling today may arrive next week once a
// forward containing it is extracted. Returns how many edges it resolved.
func (s *Store) ResolveParents() (int64, error) {
	r, err := s.db.Exec(`
		update entries set parent_id = (
		  select p.id from mail_detail d join entries p on p.id = d.entry_id
		  where d.message_id = entries.parent_ref
		)
		where parent_id is null and parent_ref is not null
		  and exists (select 1 from mail_detail d where d.message_id = entries.parent_ref)`)
	if err != nil {
		return 0, fmt.Errorf("resolving parents: %w", err)
	}
	return r.RowsAffected()
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
