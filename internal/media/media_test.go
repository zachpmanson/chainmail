package media

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zachpmanson/chainmail/internal/corpus"
)

// fakeFetcher stands in for the mailbox. What it returns matters more than how
// often it is called: each error class has to land on a different recorded
// reason, and the one class that must NOT be recorded is a transient failure.
type fakeFetcher struct {
	// byRef maps a part id to the bytes it "has", or to the error it fails with.
	data map[string][]byte
	err  map[string]error
	// calls counts fetches, so a test can prove the cap stopped one before it was
	// ever asked for.
	calls int
}

func (f *fakeFetcher) FetchPart(msgID, partID string, maxBytes int64) ([]byte, error) {
	f.calls++
	if err := f.err[partID]; err != nil {
		return nil, err
	}
	data, ok := f.data[partID]
	if !ok {
		return nil, fmt.Errorf("%s: %w", partID, ErrNoPart)
	}
	if maxBytes > 0 && int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("%s: %w", partID, ErrTooLarge)
	}
	return data, nil
}

type fixture struct {
	store   *corpus.Store
	uploads string
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	s, err := corpus.Open(":memory:")
	if err != nil {
		t.Fatalf("opening a corpus: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return fixture{store: s, uploads: t.TempDir()}
}

// part is one attachment as a test states it: what the source called the file,
// the handle it gave for the bytes, and the type it claimed.
type part struct{ name, ref, mime string }

// mailEntry stores one message with the given parts, each stating a small size.
func (f fixture) mailEntry(t *testing.T, ext, gmailID string, parts ...part) {
	t.Helper()
	atts := make([]corpus.Attachment, len(parts))
	for i, p := range parts {
		atts[i] = corpus.Attachment{Name: p.name, Mime: p.mime, SourceRef: p.ref, Size: 4}
	}
	e := corpus.Entry{
		Source: corpus.SourceMail, ExtID: ext, TS: time.Unix(1_700_000_000, 0),
		BodyText: "body of " + ext,
	}
	if _, err := f.store.Put(e, &corpus.Mail{GmailID: gmailID}, atts); err != nil {
		t.Fatalf("storing %s: %v", ext, err)
	}
}

// slackEntry stores a Slack post with one file, and puts its bytes in the archive
// the way slackdump does: a directory named by file id, the upload inside it.
func (f fixture) slackEntry(t *testing.T, ext, fileID, name string, data []byte, mime string) {
	t.Helper()
	dir := filepath.Join(f.uploads, fileID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("making the archive dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
		t.Fatalf("writing the archived upload: %v", err)
	}
	e := corpus.Entry{
		Source: corpus.SourceSlack, ExtID: ext, TS: time.Unix(1_700_000_000, 0),
		BodyText: "body of " + ext,
	}
	if _, err := f.store.PutSlack(e, corpus.Slack{}, []corpus.Attachment{
		{Name: name, Mime: mime, Size: int64(len(data)), SourceRef: fileID},
	}); err != nil {
		t.Fatalf("storing %s: %v", ext, err)
	}
}

func opts(f fixture) Options {
	return Options{Store: f.store, Uploads: f.uploads}
}

func TestPullImportsASlackUploadWithNoMailboxInvolved(t *testing.T) {
	f := newFixture(t)
	data := []byte("a picture slack already downloaded")
	f.slackEntry(t, "slack:C1:1.2", "F001", "shot.png", data, "image/png")

	r, err := Pull(context.Background(), opts(f), corpus.MediaScope{})
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}
	if r.Pulled != 1 || r.Skipped != 0 || r.Failed != 0 {
		t.Fatalf("result: %+v", r)
	}
	if r.Bytes != int64(len(data)) {
		t.Errorf("bytes: got %d, want %d", r.Bytes, len(data))
	}

	// The bytes are in the corpus, and the row points at them — which is the state
	// the renderer reads.
	got, err := f.store.Blob(corpus.BlobSHA(data))
	if err != nil {
		t.Fatalf("reading the blob back: %v", err)
	}
	if string(got.Bytes) != string(data) {
		t.Errorf("blob contents: %q", got.Bytes)
	}
	if got.Source != corpus.SourceSlack {
		t.Errorf("blob source: %q", got.Source)
	}
	wants, err := f.store.PendingMedia(corpus.MediaScope{})
	if err != nil {
		t.Fatalf("PendingMedia: %v", err)
	}
	if len(wants) != 0 {
		t.Errorf("pulled attachment is still pending: %+v", wants)
	}
}

func TestPullIsIdempotent(t *testing.T) {
	f := newFixture(t)
	f.slackEntry(t, "slack:C1:1.2", "F001", "shot.png", []byte("bytes"), "image/png")

	for i := 0; i < 2; i++ {
		r, err := Pull(context.Background(), opts(f), corpus.MediaScope{})
		if err != nil {
			t.Fatalf("Pull %d: %v", i, err)
		}
		want := 1
		if i == 1 {
			want = 0
		}
		if r.Pulled != want {
			t.Fatalf("run %d pulled %d, want %d", i, r.Pulled, want)
		}
	}
}

func TestPullRecordsWhyItWillNotAskAgain(t *testing.T) {
	f := newFixture(t)
	// An upload the archive does not have on disk, and one with no handle at all.
	f.slackEntry(t, "slack:C1:1.2", "F001", "missing.png", []byte("x"), "image/png")
	if err := os.RemoveAll(filepath.Join(f.uploads, "F001")); err != nil {
		t.Fatalf("removing the archived upload: %v", err)
	}
	f.slackEntry(t, "slack:C1:1.3", "F002", "here.png", []byte("x"), "image/png")
	if _, err := f.store.PutSlack(
		corpus.Entry{Source: corpus.SourceSlack, ExtID: "slack:C1:1.4",
			TS: time.Unix(1_700_000_000, 0), BodyText: "recovered from a quote"},
		corpus.Slack{}, []corpus.Attachment{{Name: "quoted.png", Mime: "image/png"}},
	); err != nil {
		t.Fatalf("storing the recovered attachment: %v", err)
	}

	r, err := Pull(context.Background(), opts(f), corpus.MediaScope{})
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}
	if r.Failed != 0 {
		t.Fatalf("a missing file is not a failure: %+v", r.Items)
	}
	reasons := map[string]int{}
	for _, it := range r.Items {
		reasons[it.Reason]++
	}
	if reasons[corpus.MediaSkipNoBytes] != 1 || reasons[corpus.MediaSkipNoSourceRef] != 1 {
		t.Fatalf("reasons: %+v", reasons)
	}

	// Recorded, so a second pass has nothing to do — the whole point of a reason.
	r2, err := Pull(context.Background(), opts(f), corpus.MediaScope{})
	if err != nil {
		t.Fatalf("second Pull: %v", err)
	}
	if r2.Wanted != 0 {
		t.Errorf("reasons were not remembered: %d wanted again", r2.Wanted)
	}
}

func TestPullSkipsAnOversizePartWithoutAskingForIt(t *testing.T) {
	f := newFixture(t)
	// The metadata says 40 MB. The cap is 1 MB, so the fetch must never happen:
	// that is the difference between a deliberate pull and an expensive one.
	if _, err := f.store.Put(
		corpus.Entry{Source: corpus.SourceMail, ExtID: "mail:<big@x>",
			TS: time.Unix(1_700_000_000, 0), BodyText: "holiday video"},
		&corpus.Mail{GmailID: "g-big"},
		[]corpus.Attachment{{Name: "holiday.mov", Mime: "video/quicktime",
			SourceRef: "part-1", Size: 40 << 20}},
	); err != nil {
		t.Fatalf("storing: %v", err)
	}
	ff := &fakeFetcher{data: map[string][]byte{"part-1": []byte("video")}}
	o := opts(f)
	o.Fetcher = ff
	o.MaxBytes = 1 << 20

	r, err := Pull(context.Background(), o, corpus.MediaScope{})
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}
	if r.Skipped != 1 || r.Items[0].Reason != corpus.MediaSkipTooLarge {
		t.Fatalf("result: %+v", r.Items)
	}
	if ff.calls != 0 {
		t.Errorf("the cap was checked after the fetch: %d calls", ff.calls)
	}
}

func TestPullMapsTransportErrorsOntoRecordedReasons(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		reason string
		fails  bool
	}{
		{"too large", fmt.Errorf("part-1: %w", ErrTooLarge), corpus.MediaSkipTooLarge, false},
		{"no content", fmt.Errorf("part-1: %w", ErrUnavailable), corpus.MediaSkipUnavailable, false},
		// A part that moved is stale metadata, so it is recorded; the message has
		// to be re-read, and re-asking would never work.
		{"no such part", fmt.Errorf("part-1: %w", ErrNoPart), corpus.MediaSkipNoPart, false},
		// A timeout is not recorded: writing a reason for it would turn a bad
		// minute into a permanent gap in the corpus.
		{"network", errors.New("dial tcp: connection refused"), "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture(t)
			f.mailEntry(t, "mail:<a@x>", "g-1", part{"f.png", "part-1", "image/png"})
			ff := &fakeFetcher{err: map[string]error{"part-1": c.err}}
			o := opts(f)
			o.Fetcher = ff

			r, err := Pull(context.Background(), o, corpus.MediaScope{})
			if err != nil {
				t.Fatalf("Pull: %v", err)
			}
			it := r.Items[0]
			if c.fails {
				if it.Err == nil || r.Failed != 1 {
					t.Fatalf("want a retryable failure, got %+v", it)
				}
			} else if it.Reason != c.reason {
				t.Fatalf("reason: got %q, want %q", it.Reason, c.reason)
			}
			// The row's state has to agree with the report.
			wants, err := f.store.PendingMedia(corpus.MediaScope{})
			if err != nil {
				t.Fatalf("PendingMedia: %v", err)
			}
			if c.fails && len(wants) != 1 {
				t.Errorf("a transient failure was recorded as final: %+v", wants)
			}
			if !c.fails && len(wants) != 0 {
				t.Errorf("a recorded reason did not stop the next pass: %+v", wants)
			}
		})
	}
}

func TestPullOnAMailboxLessHostStillReportsWhatItWouldDo(t *testing.T) {
	f := newFixture(t)
	f.mailEntry(t, "mail:<a@x>", "g-1", part{"f.png", "part-1", "image/png"})

	// Dry run, no transport: this is the mode that has to work on a host nobody
	// has logged into, and it must not open a connection to answer.
	o := opts(f)
	o.DryRun = true
	r, err := Pull(context.Background(), o, corpus.MediaScope{})
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}
	if r.Pulled != 1 || r.Failed != 0 {
		t.Fatalf("dry run: %+v", r)
	}
	if r.Items[0].SHA != "" {
		t.Error("a dry run computed a digest, which means it fetched")
	}
	if tally, err := f.store.MediaStats(); err != nil || tally.Blobs != 0 {
		t.Fatalf("dry run wrote %d blobs (err %v)", tally.Blobs, err)
	}
	wants, err := f.store.PendingMedia(corpus.MediaScope{})
	if err != nil {
		t.Fatalf("PendingMedia: %v", err)
	}
	if len(wants) != 1 {
		t.Error("a dry run recorded state: the same file would no longer be pulled")
	}
}

func TestDeferredFetcherOpensOnlyWhenAPartNeedsIt(t *testing.T) {
	f := newFixture(t)
	f.slackEntry(t, "slack:C1:1.2", "F001", "shot.png", []byte("bytes"), "image/png")
	f.mailEntry(t, "mail:<a@x>", "g-1", part{"f.png", "part-1", "image/png"})

	opened := 0
	ff := &fakeFetcher{data: map[string][]byte{"part-1": []byte("mail bytes")}}
	o := opts(f)
	o.Fetcher = Deferred(func() (Fetcher, error) { opened++; return ff, nil })

	// Slack only: the mailbox is never opened, so a host that is not logged in
	// can still import the archive.
	if _, err := Pull(context.Background(), o, corpus.MediaScope{Source: corpus.SourceSlack}); err != nil {
		t.Fatalf("Pull slack: %v", err)
	}
	if opened != 0 {
		t.Fatalf("the mail transport was opened for a slack-only pull (%d times)", opened)
	}

	// Mail: now it opens, and the bytes land.
	if _, err := Pull(context.Background(), o, corpus.MediaScope{Source: corpus.SourceMail}); err != nil {
		t.Fatalf("Pull mail: %v", err)
	}
	if opened != 1 {
		t.Fatalf("transport opened %d times, want once", opened)
	}
}

func TestDeferredFetcherRemembersAFailureToOpen(t *testing.T) {
	d := Deferred(func() (Fetcher, error) { return nil, errors.New("not logged in") })
	_, err := d.FetchPart("g-1", "part-1", 0)
	if err == nil || !strings.Contains(err.Error(), "not logged in") {
		t.Fatalf("open failure: %v", err)
	}
	// Reported, not swallowed as a skip: the pull must not record "no bytes" for
	// a mailbox it simply could not reach.
	if _, err := d.FetchPart("g-1", "part-1", 0); err == nil {
		t.Fatal("second call reported success")
	}
}

func TestOnlyImagesNarrowsThePull(t *testing.T) {
	f := newFixture(t)
	f.slackEntry(t, "slack:C1:1.2", "F001", "shot.png", []byte("png"), "image/png")
	f.slackEntry(t, "slack:C1:1.3", "F002", "deck.pdf", []byte("pdf"), "application/pdf")

	o := opts(f)
	o.OnlyImages = true
	r, err := Pull(context.Background(), o, corpus.MediaScope{})
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}
	if r.Pulled != 1 {
		t.Fatalf("pulled %d, want just the image", r.Pulled)
	}
	// The PDF is not refused, only not asked for: -images narrows a run, it does
	// not decide anything about the files it skips.
	wants, err := f.store.PendingMedia(corpus.MediaScope{})
	if err != nil {
		t.Fatalf("PendingMedia: %v", err)
	}
	if len(wants) != 1 || wants[0].Name != "deck.pdf" {
		t.Fatalf("narrowed pull changed what is pending: %+v", wants)
	}
}

func TestPullStopsWhenTheContextIsCancelled(t *testing.T) {
	f := newFixture(t)
	f.slackEntry(t, "slack:C1:1.2", "F001", "a.png", []byte("a"), "image/png")
	f.slackEntry(t, "slack:C1:1.3", "F002", "b.png", []byte("b"), "image/png")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Pull(ctx, opts(f), corpus.MediaScope{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled pull: %v", err)
	}
}
