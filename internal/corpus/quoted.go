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

	res, err := s.db.Exec(`
		insert into entries (source, ext_id, kind, ts, tz, tz_offset, person_id,
		                     container, parent_ref, subject, body_text, permalink,
		                     body_sha, quoted, ingested_at)
		values (?,?,?,?,?,?,?,?,?,?,?,?,?,1,?)`,
		e.Source, e.ExtID, nz(e.Kind, "message"), e.TS.Unix(), nullStr(e.TZ), e.TZOffset,
		nullInt64(e.PersonID), nullStr(e.Container), nullStr(e.ParentRef),
		nullStr(e.Subject), nullStr(e.BodyText), nullStr(e.Permalink),
		BodySHA(e.Subject, e.BodyText), time.Now().Unix())
	if err != nil {
		return 0, false, err
	}
	id, err = res.LastInsertId()
	if err != nil {
		return 0, false, err
	}
	// The FTS indexes are external-content, so they need an explicit insert.
	if _, err := s.db.Exec(
		`insert into entries_fts(rowid, subject, body_text) values (?,?,?)`,
		id, e.Subject, e.BodyText); err != nil {
		return 0, false, err
	}
	if _, err := s.db.Exec(
		`insert into entries_ident(rowid, body_text) values (?,?)`,
		id, e.BodyText); err != nil {
		return 0, false, err
	}
	return id, true, nil
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
