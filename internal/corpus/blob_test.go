package corpus

import (
	"bytes"
	"errors"
	"testing"
)

func atts(names ...string) []Attachment {
	out := make([]Attachment, len(names))
	for i, n := range names {
		out[i] = Attachment{Name: n, Mime: "image/png", Size: 12, SourceRef: "part-" + n}
	}
	return out
}

func TestPutBlobFilesUnderItsOwnDigestAndRefusesAMismatch(t *testing.T) {
	s := open(t)
	data := []byte("a small png that is not really a png")

	if err := s.PutBlob(Blob{Bytes: data, Mime: "image/png", Source: SourceSlack}); err != nil {
		t.Fatalf("PutBlob: %v", err)
	}
	sha := BlobSHA(data)
	got, err := s.Blob(sha)
	if err != nil {
		t.Fatalf("Blob: %v", err)
	}
	if !bytes.Equal(got.Bytes, data) || got.Size != int64(len(data)) {
		t.Fatalf("read back %d bytes (size %d), want %d", len(got.Bytes), got.Size, len(data))
	}
	if got.Source != SourceSlack {
		t.Errorf("source: got %q, want %q", got.Source, SourceSlack)
	}

	// The digest is verified against the bytes rather than trusted: a blob filed
	// under the wrong key is undetectable afterwards, and this is the last moment
	// anything knows better.
	err = s.PutBlob(Blob{SHA: "not-the-digest", Bytes: data, Source: SourceMail})
	if err == nil {
		t.Fatal("a digest that does not match the bytes was accepted")
	}

	// Re-filing identical bytes is a no-op, not a second row with a second
	// provenance claim.
	if err := s.PutBlob(Blob{Bytes: data, Mime: "image/png", Source: SourceMail}); err != nil {
		t.Fatalf("re-filing: %v", err)
	}
	if _, err := s.Blob(sha); err != nil {
		t.Fatalf("blob gone after re-filing: %v", err)
	}

	if _, err := s.Blob("nothing-under-this"); !errors.Is(err, ErrNoBlob) {
		t.Fatalf("missing blob: got %v, want ErrNoBlob", err)
	}
}

func TestBlobBytesIsQuietAboutAMissingBlob(t *testing.T) {
	s := open(t)
	if _, ok := s.BlobBytes(""); ok {
		t.Error("the empty digest is 'never pulled', not a hit")
	}
	if _, ok := s.BlobBytes("nothing-under-this"); ok {
		t.Error("a missing blob must not read as a hit")
	}
}

// The point of keying a link on (ext_id, source_ref) rather than on a row: `Put`
// replaces an entry's attachment rows on every walk, and bytes a reader asked for
// must not vanish with them. This is the whole reason blobs are a separate table.
func TestPulledBytesSurviveAReSlurp(t *testing.T) {
	s := open(t)
	data := []byte("screenshot bytes")
	if err := s.PutBlob(Blob{Bytes: data, Mime: "image/png", Source: SourceMail}); err != nil {
		t.Fatalf("PutBlob: %v", err)
	}

	if _, err := s.Put(entry("mail:<a@x>", "with a picture"), nil, atts("shot.png")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	sha := BlobSHA(data)
	if n, err := s.LinkBlob("mail:<a@x>", "part-shot.png", sha); err != nil || n != 1 {
		t.Fatalf("LinkBlob: n=%d err=%v, want one row linked", n, err)
	}

	// A second walk of the same message: same body, same part id, new rows.
	if _, err := s.Put(entry("mail:<a@x>", "with a picture"), nil, atts("shot.png")); err != nil {
		t.Fatalf("re-Put: %v", err)
	}

	wants, err := s.PendingMedia(MediaScope{Entry: "mail:<a@x>"})
	if err != nil {
		t.Fatalf("PendingMedia: %v", err)
	}
	if len(wants) != 0 {
		t.Fatalf("re-slurp orphaned the link: %d attachments want bytes again", len(wants))
	}
}

func TestASkipReasonAlsoSurvivesAReSlurp(t *testing.T) {
	s := open(t)
	if _, err := s.Put(entry("mail:<b@x>", "with a huge video"), nil,
		atts("holiday.mov")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := s.MarkMediaSkip("mail:<b@x>", "part-holiday.mov", MediaSkipTooLarge); err != nil {
		t.Fatalf("MarkMediaSkip: %v", err)
	}
	if _, err := s.Put(entry("mail:<b@x>", "with a huge video"), nil,
		atts("holiday.mov")); err != nil {
		t.Fatalf("re-Put: %v", err)
	}
	wants, err := s.PendingMedia(MediaScope{Entry: "mail:<b@x>"})
	if err != nil {
		t.Fatalf("PendingMedia: %v", err)
	}
	if len(wants) != 0 {
		t.Fatalf("a refused file is being asked for again on every walk: %d", len(wants))
	}
}

func TestMarkMediaSkipNeverOverwritesBytes(t *testing.T) {
	s := open(t)
	data := []byte("bytes we have")
	if err := s.PutBlob(Blob{Bytes: data, Source: SourceMail}); err != nil {
		t.Fatalf("PutBlob: %v", err)
	}
	if _, err := s.Put(entry("mail:<c@x>", "both"), nil, atts("a.png")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if _, err := s.LinkBlob("mail:<c@x>", "part-a.png", BlobSHA(data)); err != nil {
		t.Fatalf("LinkBlob: %v", err)
	}
	if err := s.MarkMediaSkip("mail:<c@x>", "part-a.png", MediaSkipNoBytes); err != nil {
		t.Fatalf("MarkMediaSkip: %v", err)
	}

	wants, err := s.PendingMedia(MediaScope{Entry: "mail:<c@x>"})
	if err != nil {
		t.Fatalf("PendingMedia: %v", err)
	}
	if len(wants) != 0 {
		t.Fatalf("an attachment with bytes is still being asked for: %d wants", len(wants))
	}
	// A row with bytes must not also be a row with a reason: the two states are
	// exclusive, and the renderer reads the link.
	var sha, skip string
	if err := s.DB().QueryRow(
		`select coalesce(a.blob_sha,''), coalesce(a.media_skip,'') from attachments a
		 join entries e on e.id=a.entry_id
		 where e.ext_id='mail:<c@x>' and a.source_ref='part-a.png'`).Scan(&sha, &skip); err != nil {
		t.Fatalf("reading the row: %v", err)
	}
	if skip != "" {
		t.Errorf("media_skip=%q on a row that has bytes", skip)
	}
	if sha == "" {
		t.Error("MarkMediaSkip cleared the link to bytes we hold")
	}
}

func TestPendingMediaScopesAndOrdering(t *testing.T) {
	s := open(t)
	newest := entry("mail:<new@x>", "newest")
	newest.TS = newest.TS.Add(48 * 3600)
	newest.Container = "thread-1"
	older := entry("mail:<old@x>", "older")
	older.Container = "thread-1"
	slack := entry("slack:C1:1.2", "slack")
	slack.Source = SourceSlack

	for _, e := range []Entry{newest, older, slack} {
		if _, err := s.Put(e, nil, atts("f-"+e.ExtID+".png")); err != nil {
			t.Fatalf("Put %s: %v", e.ExtID, err)
		}
	}

	all, err := s.PendingMedia(MediaScope{})
	if err != nil {
		t.Fatalf("PendingMedia: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("unscoped: got %d, want 3", len(all))
	}
	// Newest first: a bounded pull should spend its limit where the reader is
	// looking, and a deliberate pull is almost always about recent mail.
	if all[0].ExtID != "mail:<new@x>" {
		t.Errorf("first is %q, want the newest", all[0].ExtID)
	}

	byThread, err := s.PendingMedia(MediaScope{Container: "thread-1"})
	if err != nil {
		t.Fatalf("PendingMedia by container: %v", err)
	}
	if len(byThread) != 2 {
		t.Fatalf("thread scope: got %d, want 2", len(byThread))
	}

	bySource, err := s.PendingMedia(MediaScope{Source: SourceSlack})
	if err != nil {
		t.Fatalf("PendingMedia by source: %v", err)
	}
	if len(bySource) != 1 || bySource[0].Source != SourceSlack {
		t.Fatalf("source scope: got %+v", bySource)
	}

	limited, err := s.PendingMedia(MediaScope{Limit: 1})
	if err != nil {
		t.Fatalf("PendingMedia limited: %v", err)
	}
	if len(limited) != 1 || limited[0].ExtID != "mail:<new@x>" {
		t.Fatalf("limit: got %+v", limited)
	}
}

func TestPruneMediaKeepsReferencedBlobs(t *testing.T) {
	s := open(t)
	kept := []byte("referenced")
	orphan := []byte("nothing points at this")
	for _, d := range [][]byte{kept, orphan} {
		if err := s.PutBlob(Blob{Bytes: d, Source: SourceMail}); err != nil {
			t.Fatalf("PutBlob: %v", err)
		}
	}
	if _, err := s.Put(entry("mail:<d@x>", "keeps one"), nil, atts("k.png")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if _, err := s.LinkBlob("mail:<d@x>", "part-k.png", BlobSHA(kept)); err != nil {
		t.Fatalf("LinkBlob: %v", err)
	}

	n, bytes, err := s.PruneMedia()
	if err != nil {
		t.Fatalf("PruneMedia: %v", err)
	}
	if n != 1 || bytes != int64(len(orphan)) {
		t.Fatalf("pruned %d blobs (%d bytes), want 1 (%d)", n, bytes, len(orphan))
	}
	if _, err := s.Blob(BlobSHA(kept)); err != nil {
		t.Errorf("pruned a blob that is still referenced: %v", err)
	}
	if _, err := s.Blob(BlobSHA(orphan)); !errors.Is(err, ErrNoBlob) {
		t.Errorf("orphan survived the prune: %v", err)
	}
	// Pruning again with nothing left to do is not an error.
	n2, _, err := s.PruneMedia()
	if err != nil || n2 != 0 {
		t.Errorf("second prune: n=%d err=%v, want a no-op", n2, err)
	}
}

func TestMediaStatsCountsWhatIsFiledAndWhyNot(t *testing.T) {
	s := open(t)
	data := []byte("one picture")
	if err := s.PutBlob(Blob{Bytes: data, Source: SourceMail}); err != nil {
		t.Fatalf("PutBlob: %v", err)
	}
	if _, err := s.Put(entry("mail:<e@x>", "two files"), nil,
		atts("a.png", "b.pdf")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if _, err := s.LinkBlob("mail:<e@x>", "part-a.png", BlobSHA(data)); err != nil {
		t.Fatalf("LinkBlob: %v", err)
	}
	if err := s.MarkMediaSkip("mail:<e@x>", "part-b.pdf", MediaSkipTooLarge); err != nil {
		t.Fatalf("MarkMediaSkip: %v", err)
	}

	tally, err := s.MediaStats()
	if err != nil {
		t.Fatalf("MediaStats: %v", err)
	}
	if tally.Blobs != 1 || tally.Bytes != int64(len(data)) || tally.PulledRows != 1 {
		t.Errorf("tally: %+v", tally)
	}
	if tally.SkippedRows != 1 || tally.Skips[MediaSkipTooLarge] != 1 {
		t.Errorf("skips: %+v", tally.Skips)
	}
}
