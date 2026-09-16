package corpus

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

// Blob is one attachment's bytes, filed under the digest of the bytes
// themselves.
//
// The digest is the identity, not a database id: bytes fetched twice — the same
// screenshot forwarded five times, a re-pull after a re-slurp — are one row, and
// a caller holding a digest can ask whether it already has the file without
// knowing which message it came from.
type Blob struct {
	SHA       string // hex sha256 of Bytes
	Bytes     []byte
	Mime      string
	Size      int64
	Source    string // mail | slack — where the bytes came from, for the audit trail
	FetchedAt time.Time
}

// ErrNoBlob reports that nothing is filed under a digest. A normal answer: it is
// what "this attachment has not been pulled" looks like.
var ErrNoBlob = errors.New("no such blob")

// Why an attachment has no bytes. Every value is a decision or a dead end that
// must stop a later pass re-asking for the same file:
//
//   - MediaSkipTooLarge — the part is bigger than the pull's cap. docket refuses
//     rather than truncating, and so do we: half a PNG is not a smaller PNG.
//   - MediaSkipUnavailable — the part exists in the message and carries no
//     content. Terminal for that part; the message around it is fine.
//   - MediaSkipNoPart — the message no longer has that part id. The metadata is
//     stale and re-reading the message is the fix, not a retry.
//   - MediaSkipNoSourceRef — an attachment recovered from quoted text, or one the
//     source never gave a handle for: there is nothing to ask for.
//   - MediaSkipNoMessage — no Gmail message id, so not even a fetchable host.
//   - MediaSkipNoBytes — the archive the bytes were expected in does not have
//     them (a Slack upload the downloader skipped, or a pruned archive).
const (
	MediaSkipTooLarge    = "too_large"
	MediaSkipUnavailable = "unavailable"
	MediaSkipNoPart      = "part_not_found"
	MediaSkipNoSourceRef = "no_source_ref"
	MediaSkipNoMessage   = "no_message_id"
	MediaSkipNoBytes     = "no_bytes"
)

// BlobSHA is the digest a blob is filed under. Exported because a caller that
// holds bytes — the fetcher, a test — has to be able to say what they are
// without a round trip through the store.
func BlobSHA(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// PutBlob files bytes under their digest. Re-filing identical bytes is a no-op
// rather than an update: the key IS the content, so two rows under one digest
// would be the same bytes with two provenance claims, and there is no rule that
// picks the better one.
//
// The digest is re-derived from the bytes and checked rather than trusted. The
// cost is one hash of data already in memory, and it is the difference between a
// mis-filed blob (undetectable later) and an error at the moment it happened.
func (s *Store) PutBlob(b Blob) error {
	if len(b.Bytes) == 0 {
		return errors.New("blob has no bytes")
	}
	got := BlobSHA(b.Bytes)
	if b.SHA != "" && b.SHA != got {
		return fmt.Errorf("blob digest %s does not match its bytes (%s)", b.SHA, got)
	}
	b.SHA = got
	if b.Size == 0 {
		b.Size = int64(len(b.Bytes))
	}
	at := b.FetchedAt
	if at.IsZero() {
		at = time.Now()
	}
	if _, err := s.db.Exec(`
		insert into blobs (sha256, bytes, mime, size, source, fetched_at)
		values (?,?,?,?,?,?)
		on conflict(sha256) do nothing`,
		b.SHA, b.Bytes, nullStr(b.Mime), b.Size, b.Source, at.Unix()); err != nil {
		return fmt.Errorf("filing blob %s: %w", b.SHA, err)
	}
	return nil
}

// Blob reads one filed blob back. ErrNoBlob when nothing is filed under it.
func (s *Store) Blob(sha string) (Blob, error) {
	var b Blob
	var at int64
	err := s.db.QueryRow(`
		select sha256, bytes, coalesce(mime,''), size, source, fetched_at
		from blobs where sha256=?`, sha).
		Scan(&b.SHA, &b.Bytes, &b.Mime, &b.Size, &b.Source, &at)
	if err == sql.ErrNoRows {
		return Blob{}, ErrNoBlob
	}
	if err != nil {
		return Blob{}, fmt.Errorf("reading blob %s: %w", sha, err)
	}
	b.FetchedAt = time.Unix(at, 0)
	return b, nil
}

// mediaLink is one attachment row's association with bytes: what a re-slurp has
// to re-apply after it replaces the row.
type mediaLink struct {
	ref  string
	sha  string
	skip string
}

// mediaLinks reads the blob links an entry's attachment rows carry, so put can
// re-apply them after it replaces those rows. Only rows with a source_ref are
// captured: a link without one could not have been made in the first place, and
// a skip without one is re-derived by the next pass for free.
func mediaLinks(tx *sql.Tx, entryID int64) ([]mediaLink, error) {
	rows, err := tx.Query(`
		select source_ref, coalesce(blob_sha,''), coalesce(media_skip,'') from attachments
		where entry_id=? and coalesce(source_ref,'') <> ''
		  and (blob_sha is not null or media_skip is not null)`, entryID)
	if err != nil {
		return nil, fmt.Errorf("reading attachment links: %w", err)
	}
	defer rows.Close()
	var out []mediaLink
	for rows.Next() {
		var l mediaLink
		if err := rows.Scan(&l.ref, &l.sha, &l.skip); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// restoreMediaLinks re-applies the links mediaLinks read, matching on the
// source's own reference. A part id that has moved — the message was re-read and
// Gmail numbered it differently — matches nothing, which leaves the bytes filed
// but unreferenced rather than attached to the wrong file; `media prune` is what
// collects those.
func restoreMediaLinks(tx *sql.Tx, entryID int64, links []mediaLink) error {
	for _, l := range links {
		if _, err := tx.Exec(`
			update attachments set blob_sha=?, media_skip=?
			where entry_id=? and source_ref=?`,
			nullStr(l.sha), nullStr(l.skip), entryID, l.ref); err != nil {
			return fmt.Errorf("restoring attachment link %s: %w", l.ref, err)
		}
	}
	return nil
}

// BlobBytes is the read the renderer makes, where a missing blob is an ordinary
// "no preview" rather than an error worth a failure path. An empty digest is
// "this attachment was never pulled", which is the common case.
func (s *Store) BlobBytes(sha string) ([]byte, bool) {
	if sha == "" {
		return nil, false
	}
	var data []byte
	if err := s.db.QueryRow(`select bytes from blobs where sha256=?`, sha).Scan(&data); err != nil {
		return nil, false
	}
	return data, true
}

// LinkBlob points an entry's attachment rows at bytes that are already filed.
//
// Keyed on (ext_id, source_ref) rather than on a row, because attachments have
// no primary key and `Put` replaces them wholesale on every walk — a link made
// by rowid would be gone the next hour. source_ref is the source's own handle on
// the part (a Gmail part id, a Slack file id), which is stable across a re-read
// of the same message.
func (s *Store) LinkBlob(extID, sourceRef, sha string) (int64, error) {
	if extID == "" || sourceRef == "" {
		return 0, errors.New("linking a blob needs an entry and the source's own reference")
	}
	res, err := s.db.Exec(`
		update attachments set blob_sha=?, media_skip=null
		where source_ref=? and entry_id=(select id from entries where ext_id=?)`,
		sha, sourceRef, extID)
	if err != nil {
		return 0, fmt.Errorf("linking blob %s to %s: %w", sha, sourceRef, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	return n, nil
}

// MarkMediaSkip records why an attachment has no bytes, so the next pass does
// not ask again. It never overwrites a link: an attachment with bytes has
// nothing to skip.
//
// A source_ref may be empty — an attachment recovered from someone's quoted text
// has no handle to fetch by, and that is exactly the case worth recording, since
// otherwise every pass re-reports it. Those rows are matched as a group: they are
// all ref-less, and the reason they carry is the same one.
func (s *Store) MarkMediaSkip(extID, sourceRef, reason string) error {
	if extID == "" {
		return errors.New("marking a skip needs an entry")
	}
	_, err := s.db.Exec(`
		update attachments set media_skip=?
		where coalesce(source_ref,'')=? and blob_sha is null
		  and entry_id=(select id from entries where ext_id=?)`,
		reason, sourceRef, extID)
	if err != nil {
		return fmt.Errorf("recording media skip %s for %s: %w", reason, sourceRef, err)
	}
	return nil
}

// MediaWant is one attachment whose bytes have not been asked for: everything a
// pull needs to fetch it, and nothing that requires a second query.
type MediaWant struct {
	EntryID   int64
	ExtID     string
	Source    string // mail | slack
	Name      string
	Mime      string
	Size      int64
	SourceRef string
	// GmailID is the message the part belongs to — mail only, and empty when the
	// entry has no mail_detail row (a quoted message, a Slack post).
	GmailID string
	// Container is the mail thread id or Slack channel id, so a pull can report
	// what it is working through.
	Container string
}

// MediaScope narrows a pull. An empty scope is every attachment in the corpus
// that has not been pulled — a whole-corpus sweep, which is the one thing a
// deliberate pull exists to avoid, so callers set at least one field.
type MediaScope struct {
	// Entry is one entry's ext_id: the message whose chips the reader clicked.
	Entry string
	// Container is a mail thread id or Slack channel id: every message in it.
	Container string
	// Source is "mail" or "slack"; empty for both.
	Source string
	// Limit bounds how many attachments are returned; 0 is no bound.
	Limit int
}

// PendingMedia lists the attachments a scope covers that have neither bytes nor
// a recorded reason to stop asking.
//
// Rows without a source_ref are included on purpose: for mail they are the parts
// recovered from quoted text, and marking them (rather than filtering them here)
// is what turns "why is this chip dead" into a row an operator can read.
func (s *Store) PendingMedia(sc MediaScope) ([]MediaWant, error) {
	q := `
		select e.id, e.ext_id, e.source, a.name, coalesce(a.mime,''), coalesce(a.size,0),
		       coalesce(a.source_ref,''), coalesce(m.gmail_id,''), coalesce(e.container,'')
		from attachments a
		join entries e on e.id = a.entry_id
		left join mail_detail m on m.entry_id = e.id
		where a.blob_sha is null and a.media_skip is null`
	args := []any{}
	if sc.Entry != "" {
		q += ` and e.ext_id=?`
		args = append(args, sc.Entry)
	}
	if sc.Container != "" {
		q += ` and e.container=?`
		args = append(args, sc.Container)
	}
	if sc.Source != "" {
		q += ` and e.source=?`
		args = append(args, sc.Source)
	}
	// Newest first: a deliberate pull is almost always about recent mail, and a
	// bounded sweep should spend its limit where the reader is looking.
	q += ` order by e.ts desc, a.rowid`
	if sc.Limit > 0 {
		q += ` limit ?`
		args = append(args, sc.Limit)
	}

	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("listing media: %w", err)
	}
	defer rows.Close()
	var out []MediaWant
	for rows.Next() {
		var w MediaWant
		if err := rows.Scan(&w.EntryID, &w.ExtID, &w.Source, &w.Name, &w.Mime, &w.Size,
			&w.SourceRef, &w.GmailID, &w.Container); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// MediaTally is what the corpus holds of the bytes it has been asked for.
type MediaTally struct {
	Blobs       int
	Bytes       int64
	PulledRows  int            // attachment rows pointing at a blob
	SkippedRows int            // rows with a reason instead of bytes
	Skips       map[string]int // reason -> rows
}

// MediaStats answers "what is in there", which is the question a deliberate pull
// makes people ask — and the one that decides whether a prune is worth running.
func (s *Store) MediaStats() (MediaTally, error) {
	t := MediaTally{Skips: map[string]int{}}
	if err := s.db.QueryRow(
		`select count(*), coalesce(sum(size),0) from blobs`).Scan(&t.Blobs, &t.Bytes); err != nil {
		return t, fmt.Errorf("counting blobs: %w", err)
	}
	if err := s.db.QueryRow(
		`select count(*) from attachments where blob_sha is not null`).Scan(&t.PulledRows); err != nil {
		return t, fmt.Errorf("counting linked attachments: %w", err)
	}
	rows, err := s.db.Query(`
		select media_skip, count(*) from attachments
		where media_skip is not null group by media_skip order by count(*) desc, media_skip`)
	if err != nil {
		return t, fmt.Errorf("counting skips: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var reason string
		var n int
		if err := rows.Scan(&reason, &n); err != nil {
			return t, err
		}
		t.Skips[reason] = n
		t.SkippedRows += n
	}
	return t, rows.Err()
}

// PruneMedia deletes blobs nothing points at, and reports what went. A blob
// whose only referencing attachment row was replaced by a re-slurp — the part id
// moved, the message was rewritten, the entry was collapsed as a twin — is
// unreachable, and bytes are the one thing in this corpus big enough for that to
// matter.
//
// Deliberately not automatic: it is the only operation here that destroys
// something a reader might still have wanted.
func (s *Store) PruneMedia() (count int, bytes int64, err error) {
	err = s.db.QueryRow(`
		select count(*), coalesce(sum(size),0) from blobs
		where sha256 not in (
		  select blob_sha from attachments where blob_sha is not null
		)`).Scan(&count, &bytes)
	if err != nil {
		return 0, 0, fmt.Errorf("sizing the prune: %w", err)
	}
	if count == 0 {
		return 0, 0, nil
	}
	if _, err := s.db.Exec(`
		delete from blobs where sha256 not in (
		  select blob_sha from attachments where blob_sha is not null
		)`); err != nil {
		return 0, 0, fmt.Errorf("pruning blobs: %w", err)
	}
	return count, bytes, nil
}
