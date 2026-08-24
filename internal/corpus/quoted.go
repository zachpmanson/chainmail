package corpus

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// PutQuoted stores an entry recovered from quoted text.
//
// Unlike Put this is insert-or-leave-alone, and the difference is load-bearing.
// A recovered block that carries a Message-ID gets the same ext_id as the real
// mailbox message, so the two converge on one row — but the quoted copy has been
// rewrapped and elided by every client that forwarded it, so overwriting the
// mailbox copy with it would degrade the corpus. The existing row wins; the new
// sighting is the only thing recorded.
//
// Returns the entry id and whether this call created it.
func (s *Store) PutQuoted(e Entry) (int64, bool, error) {
	if e.ExtID == "" {
		return 0, false, errors.New("quoted entry needs an ext_id")
	}
	if e.TS.IsZero() {
		return 0, false, fmt.Errorf("quoted entry %s has no timestamp", e.ExtID)
	}
	var id int64
	err := s.db.QueryRow(`select id from entries where source=? and ext_id=?`,
		e.Source, e.ExtID).Scan(&id)
	if err == nil {
		return id, false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, false, err
	}

	tx, err := s.db.Begin()
	if err != nil {
		return 0, false, err
	}
	defer tx.Rollback()

	res, err := tx.Exec(`
		insert into entries (source, ext_id, kind, ts, tz, tz_offset, person_id,
		                     container, parent_ref, subject, body_text, permalink,
		                     body_sha, quoted, derived, ingested_at)
		values (?,?,?,?,?,?,?,?,?,?,?,?,?,1,?,?)`,
		e.Source, e.ExtID, nz(e.Kind, "message"), e.TS.Unix(), nullStr(e.TZ), e.TZOffset,
		nullInt64(e.PersonID), nullStr(e.Container), nullStr(e.ParentRef),
		nullStr(e.Subject), nullStr(e.BodyText), nullStr(e.Permalink),
		BodySHA(e.Subject, e.BodyText), boolInt(e.Derived), time.Now().Unix())
	if err != nil {
		return 0, false, err
	}
	id, err = res.LastInsertId()
	if err != nil {
		return 0, false, err
	}
	// created=true, so no retraction is attempted: a double retraction against a
	// fresh rowid corrupts an external-content index.
	if err := s.reindex(tx, id, true, "", ""); err != nil {
		return 0, false, err
	}
	return id, true, tx.Commit()
}

// SetParent records a reply edge directly.
//
// Quoted entries cannot go through ResolveParents: that matches parent_ref
// against mail_detail.message_id, and a recovered block has no mail_detail row
// and usually no Message-ID at all. Its parent is known structurally instead —
// the block it was quoted beneath — so it is linked at extraction time.
//
// Refuses to make an entry its own parent, which positional nesting can produce
// when two blocks collapse to one under dedup.
func (s *Store) SetParent(child, parent int64) error {
	if child == 0 || parent == 0 || child == parent {
		return nil
	}
	_, err := s.db.Exec(
		`update entries set parent_id=? where id=? and parent_id is null`,
		parent, child)
	return err
}

// QuotedCount reports how many entries came only from quoted text.
func (s *Store) QuotedCount() (real, quoted int, err error) {
	err = s.db.QueryRow(
		`select coalesce(sum(quoted=0),0), coalesce(sum(quoted=1),0) from entries`).
		Scan(&real, &quoted)
	return real, quoted, err
}

func nz(s, dflt string) string {
	if s == "" {
		return dflt
	}
	return s
}

// nullInt64 keeps an unknown author NULL rather than 0, which would violate the
// foreign key on people(id).
func nullInt64(v int64) any {
	if v == 0 {
		return nil
	}
	return v
}

// EnrichQuoted fills gaps on an already-stored quoted entry from a later
// sighting of the same message.
//
// Each client quotes a different subset: one writes a full header block with
// Subject and recipients, the next re-quotes it as a bare "On ... wrote:" with
// neither. Whichever arrives first should not decide what is known, so a later
// sighting may fill what is missing — but never overwrite what is present, and
// never touch a real mailbox message, whose own headers are authoritative.
//
// Body text is replaced only when the new copy is longer, on the reasoning that
// quoting elides rather than invents.
func (s *Store) EnrichQuoted(id int64, e Entry) error {
	var quoted int
	if err := s.db.QueryRow(`select quoted from entries where id=?`, id).Scan(&quoted); err != nil {
		return err
	}
	if quoted == 0 {
		// A real message. Its own headers beat anything a forward said about it.
		return nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := s.enrich(tx, id, e); err != nil {
		return err
	}
	return tx.Commit()
}

// enrich is EnrichQuoted's write, inside a transaction the caller owns. Twin
// collapse needs the same fill-the-gaps update in the middle of a transaction
// that is also moving rows, and one connection cannot hold two write
// transactions at once.
func (s *Store) enrich(tx *sql.Tx, id int64, e Entry) error {
	// The FTS retraction needs the values the index currently holds, so they are
	// read BEFORE the update. Retracting with the new text would leave the old
	// terms in the index permanently, matching text the entry no longer contains.
	var oldSubject, oldBody string
	if err := tx.QueryRow(
		`select coalesce(subject,''), coalesce(body_text,'') from entries where id=?`,
		id).Scan(&oldSubject, &oldBody); err != nil {
		return err
	}
	if _, err := tx.Exec(`
		update entries set
		  subject   = coalesce(nullif(subject,''), ?),
		  tz        = coalesce(nullif(tz,''), ?),
		  person_id = coalesce(person_id, ?),
		  body_text = case when length(?) > length(coalesce(body_text,''))
		                   then ? else body_text end
		where id = ?`,
		nullStr(e.Subject), nullStr(e.TZ), nullInt64(e.PersonID),
		e.BodyText, e.BodyText, id); err != nil {
		return err
	}
	return s.reindex(tx, id, false, oldSubject, oldBody)
}
