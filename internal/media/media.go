// Package media pulls attachment bytes into the corpus, on purpose.
//
// Nothing here runs as part of `slurp`. A mail part costs a Gmail round trip, and
// a corpus that swept up every attachment it was told about would spend its quota
// on files nobody opened — so pulling is explicit: one message, one thread, one
// page, chosen by whoever is reading it.
//
// Two sources, and they are not the same shape. Slack's downloader already wrote
// every upload to disk beside the archive, so importing one is a local read with
// no failure mode worth a retry. Mail has to ask, which is why it takes a
// transport and a size cap, and why the cap is checked against the size the
// metadata states before anything is fetched.
package media

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/zachpmanson/chainmail/internal/corpus"
	"github.com/zachpmanson/chainmail/internal/uploads"
)

// Failure classes a fetcher must be able to name, so the caller can record the
// right reason and — more importantly — know which of them are worth retrying.
// The distinctions are docket's, carried across the seam rather than flattened
// into a string: a part that no longer exists and a mailbox that timed out look
// the same to a naive caller and mean opposite things about the next run.
var (
	// ErrTooLarge: the part is bigger than the cap. Refused rather than truncated,
	// and terminal — a bigger cap is the only thing that changes the answer.
	ErrTooLarge = errors.New("attachment is larger than the cap")
	// ErrUnavailable: the part exists and carries no content.
	ErrUnavailable = errors.New("the part carries no attachment content")
	// ErrNoPart: the message no longer has that part id; the metadata is stale.
	ErrNoPart = errors.New("no such part in this message")
)

// Fetcher is the mail transport: one MIME part's decoded bytes, by message id and
// part id. Deliberately narrow — the corpus knows those two things and nothing
// else about how mail is reached, so the in-process Gmail library and a docket
// subprocess are both behind it.
type Fetcher interface {
	FetchPart(msgID, partID string, maxBytes int64) ([]byte, error)
}

// Deferred opens a transport the first time a part actually needs one.
//
// The point is the pull that turns out to be Slack-only: its bytes are on disk, no
// mailbox is involved, and a host with no mail login should still be able to fetch
// them. Opening eagerly would make logging into Gmail a precondition for reading
// an archive — and would fail a dry run, which is the one mode that must work
// everywhere. The error is remembered, so a mailbox that is genuinely absent is
// reported once per file rather than re-attempted.
func Deferred(open func() (Fetcher, error)) Fetcher { return &deferred{open: open} }

type deferred struct {
	open func() (Fetcher, error)
	once sync.Once
	f    Fetcher
	err  error
}

func (d *deferred) FetchPart(msgID, partID string, maxBytes int64) ([]byte, error) {
	d.once.Do(func() {
		d.f, d.err = d.open()
		if d.err == nil && d.f == nil {
			d.err = errors.New("the mail transport opened to nothing")
		}
	})
	if d.err != nil {
		return nil, d.err
	}
	return d.f.FetchPart(msgID, partID, maxBytes)
}

// DefaultMaxBytes caps a pull when the caller does not say otherwise. It is
// docket's own default, deliberately: two caps for one quantity would drift, and
// the number that matters is "above the screenshots and PDFs that make up nearly
// all of this, below the video that should be a decision".
const DefaultMaxBytes = 10 << 20

// Options is one pull's configuration.
type Options struct {
	Store *corpus.Store
	// Uploads is the archive upload root: where slackdump put the bytes it
	// downloaded. Empty disables Slack pulls rather than failing them, because a
	// host with no archive is a host with a mail corpus.
	Uploads string
	// Fetcher reaches mail. Nil disables mail pulls: `-why` on a machine that
	// cannot authenticate is the useful half of this command, and it must work
	// without a mailbox.
	Fetcher Fetcher
	// MaxBytes caps a single file. 0 means DefaultMaxBytes; negative means no cap.
	MaxBytes int64
	// OnlyImages limits the pull to parts whose type says image, which is the
	// narrow ask — "show me the pictures in this thread" — that is worth a flag.
	OnlyImages bool
	// DryRun reports what would happen and writes nothing at all: no bytes, no
	// skip records. A deliberate pull has to be inspectable before it costs
	// anything, and the size cap makes the answer non-obvious.
	DryRun bool
	// Logf receives one line per file as it is decided. Nil is silent.
	Logf func(format string, args ...any)
}

// Result is what a pull did, in the shape a caller prints and a test asserts on.
type Result struct {
	Wanted  int
	Pulled  int
	Skipped int
	Failed  int
	Bytes   int64
	Items   []Item
}

// Item is one file's outcome. Reason is empty for a file that was pulled, and
// otherwise the short word that was recorded — or would be, under DryRun.
type Item struct {
	ExtID   string
	Name    string
	Source  string
	Reason  string
	SHA     string
	Bytes   int64
	Err     error // set only for a failure worth retrying
	Subject string
}

// Pull walks the attachments a scope covers, fetches the ones it can, and
// records why the others were not.
//
// One file's failure never ends the walk: a pull is a bulk operation over old
// metadata, where a part that moved and a size cap are both normal, and stopping
// at the first would make a thread with one dead link unfetchable.
func Pull(ctx context.Context, opts Options, sc corpus.MediaScope) (Result, error) {
	var r Result
	if opts.Store == nil {
		return r, errors.New("media pull needs a corpus")
	}
	max := opts.MaxBytes
	if max == 0 {
		max = DefaultMaxBytes
	}
	if max < 0 {
		max = 0
	}

	wants, err := opts.Store.PendingMedia(sc)
	if err != nil {
		return r, err
	}
	r.Wanted = len(wants)
	for _, w := range wants {
		if err := ctx.Err(); err != nil {
			return r, err
		}
		if opts.OnlyImages && !isImage(w.Mime) {
			continue
		}
		it := pullOne(opts, w, max)
		switch {
		case it.Err != nil:
			r.Failed++
		case it.Reason != "":
			r.Skipped++
		default:
			r.Pulled++
			r.Bytes += it.Bytes
		}
		r.Items = append(r.Items, it)
		if opts.Logf != nil {
			logOne(opts.Logf, it)
		}
	}
	return r, nil
}

// pullOne decides and carries out exactly one attachment, and is the only place
// the two sources differ.
func pullOne(opts Options, w corpus.MediaWant, max int64) Item {
	it := Item{ExtID: w.ExtID, Name: w.Name, Source: w.Source}

	// Nothing to ask for. A quoted message recovered from someone's forward has
	// no part of its own, and the honest record of that is a reason, not a retry
	// on every pass.
	if w.SourceRef == "" {
		return skip(opts, w, it, corpus.MediaSkipNoSourceRef)
	}
	// The cap is checked against what the metadata claims BEFORE the file is
	// asked for. This is the difference between a deliberate pull and an
	// expensive one: a 2 GB video sitting in a thread costs nothing to decline.
	// A source that states no size is fetched and capped on the bytes instead.
	if max > 0 && w.Size > max {
		return skip(opts, w, it, corpus.MediaSkipTooLarge)
	}

	var data []byte
	switch w.Source {
	case corpus.SourceSlack:
		path, ok := uploads.Locate(opts.Uploads, w.SourceRef, w.Name)
		if !ok {
			return skip(opts, w, it, corpus.MediaSkipNoBytes)
		}
		// Measured on the file rather than trusted from the metadata: Slack states
		// a size, and the file on disk is the authority on whether it is there at
		// all. A dry run stops here — reading the bytes to find out what a decision
		// already says would make inspecting a pull as expensive as running one.
		fi, err := os.Stat(path)
		if err != nil {
			return skip(opts, w, it, corpus.MediaSkipNoBytes)
		}
		if max > 0 && fi.Size() > max {
			return skip(opts, w, it, corpus.MediaSkipTooLarge)
		}
		it.Bytes = fi.Size()
		if opts.DryRun {
			return it
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return skip(opts, w, it, corpus.MediaSkipNoBytes)
		}
		data = b
	case corpus.SourceMail:
		if w.GmailID == "" {
			return skip(opts, w, it, corpus.MediaSkipNoMessage)
		}
		if opts.Fetcher == nil {
			// Only reachable on a dry run: a real pull always carries a transport,
			// deferred or not. Reporting what the fetch would cost is the whole of
			// what -why can say without one.
			if opts.DryRun {
				it.Bytes = w.Size
				return it
			}
			it.Err = errors.New("no mail transport available (log in, or run with -why)")
			return it
		}
		b, err := opts.Fetcher.FetchPart(w.GmailID, w.SourceRef, max)
		if err != nil {
			return failed(opts, w, it, err)
		}
		data = b
	default:
		it.Err = fmt.Errorf("no media source for %q", w.Source)
		return it
	}

	if len(data) == 0 {
		return skip(opts, w, it, corpus.MediaSkipUnavailable)
	}
	if max > 0 && int64(len(data)) > max {
		return skip(opts, w, it, corpus.MediaSkipTooLarge)
	}

	it.SHA = corpus.BlobSHA(data)
	it.Bytes = int64(len(data))
	if opts.DryRun {
		return it
	}
	if err := opts.Store.PutBlob(corpus.Blob{
		SHA:       it.SHA,
		Bytes:     data,
		Mime:      w.Mime,
		Source:    w.Source,
		FetchedAt: time.Now(),
	}); err != nil {
		it.Err = err
		return it
	}
	// The link, not the bytes, is what the renderer reads — and it is keyed on the
	// source's own part reference so that a re-slurp's wholesale replacement of
	// these rows re-applies it rather than orphaning the blob.
	if _, err := opts.Store.LinkBlob(w.ExtID, w.SourceRef, it.SHA); err != nil {
		it.Err = err
		return it
	}
	return it
}

// skip records a terminal answer: this file will not be fetched, and the next
// pass should not ask again. Under DryRun it is reported and not written, so
// `-why` can be run against a corpus without changing what a later real run does.
func skip(opts Options, w corpus.MediaWant, it Item, reason string) Item {
	it.Reason = reason
	if opts.DryRun {
		return it
	}
	if err := opts.Store.MarkMediaSkip(w.ExtID, w.SourceRef, reason); err != nil {
		it.Err = err
	}
	return it
}

// failed maps a transport error onto the reason it deserves — and records it,
// because the classes that earn a reason are the ones that must stop the next
// pass asking. Only a transient failure is left as a failure, and deliberately
// unrecorded: writing "no bytes" for a mailbox that timed out would make a bad
// minute into a permanent gap in the corpus.
func failed(opts Options, w corpus.MediaWant, it Item, err error) Item {
	switch {
	case errors.Is(err, ErrTooLarge):
		return skip(opts, w, it, corpus.MediaSkipTooLarge)
	case errors.Is(err, ErrUnavailable):
		return skip(opts, w, it, corpus.MediaSkipUnavailable)
	case errors.Is(err, ErrNoPart):
		return skip(opts, w, it, corpus.MediaSkipNoPart)
	}
	it.Err = err
	return it
}

func isImage(mime string) bool {
	m := strings.ToLower(strings.TrimSpace(mime))
	if i := strings.Index(m, ";"); i >= 0 {
		m = strings.TrimSpace(m[:i])
	}
	return strings.HasPrefix(m, "image/")
}

// logOne prints one line per file. Names are truncated by the format rather than
// by a helper: a filename is sender-controlled and arbitrarily long, and a pull's
// output should stay a column an operator can read.
func logOne(logf func(string, ...any), it Item) {
	switch {
	case it.Err != nil:
		logf("failed   %-40.40s %s", it.Name, it.Err)
	case it.Reason != "":
		logf("skipped  %-40.40s %s", it.Name, it.Reason)
	default:
		logf("pulled   %-40.40s %s", it.Name, human(it.Bytes))
	}
}

func human(n int64) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%d B", n)
	case n < 1024*1024:
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	default:
		return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024))
	}
}
