package gmailclient

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/zachpmanson/docket/gmail/mail"

	"github.com/zachpmanson/chainmail/internal/media"
)

// FetchPart reads one MIME part's bytes, for the media pull.
//
// The library writes an attachment to a file rather than returning it — a
// deliberate choice on docket's side, because its stdout carries a JSON envelope
// and a caller hunting for that envelope inside a stream of PNG bytes cannot read
// a failure at all. So the file is the interface, and this adapter gives it a
// temporary one and reads it back. The scratch directory is removed with the
// bytes still in memory, so a pulled attachment exists on disk only for as long
// as it takes to hand it over.
//
// The failure classes docket distinguishes are translated rather than passed
// through: the pull has to record why it will not try again, and "the part is
// gone" and "the mailbox timed out" demand opposite answers. The distinction is
// worth more than the one seam it costs — a corpus that records a timeout as a
// permanent gap has lost the mail silently.
func (c Client) FetchPart(msgID, partID string, maxBytes int64) ([]byte, error) {
	dir, err := os.MkdirTemp("", "chainmail-media-")
	if err != nil {
		return nil, fmt.Errorf("making an attachment scratch dir: %w", err)
	}
	defer os.RemoveAll(dir)

	// MaxBytes follows docket's convention: 0 is unlimited, so a negative has no
	// reading left that is not a guess — the pull clamps before it gets here.
	limit := 0
	if maxBytes > 0 {
		limit = int(maxBytes)
	}
	got, err := mail.FetchAttachment(c.ctx, c.svc, msgID, mail.FetchOptions{
		PartID:   partID,
		MaxBytes: limit,
		OutPath:  filepath.Join(dir, "part"),
	})
	if err != nil {
		switch {
		case errors.Is(err, mail.ErrAttachmentTooLarge):
			return nil, fmt.Errorf("%s: %w", partID, media.ErrTooLarge)
		case errors.Is(err, mail.ErrAttachmentUnavailable):
			return nil, fmt.Errorf("%s: %w", partID, media.ErrUnavailable)
		case errors.Is(err, mail.ErrPartNotFound):
			return nil, fmt.Errorf("%s: %w", partID, media.ErrNoPart)
		}
		return nil, err
	}
	return os.ReadFile(got.Path)
}
