package spec

import (
	"testing"

	"github.com/zachpmanson/chainmail/internal/corpus"
)

// What a click does is the server's decision, made once from the stored MIME,
// because the same rule has to set Content-Disposition when the file is served
// later. These cases are the rule.
func TestAttachmentOpenSplitsPopupFromDownload(t *testing.T) {
	cases := []struct {
		name, mime string
		stored     bool
		want       string
		why        string
	}{
		{"a screenshot", "image/png", true, OpenPopup, "pictures read in a window"},
		{"a recording", "audio/mpeg", true, OpenPopup, "so do recordings"},
		{"a video", "video/mp4", true, OpenPopup, "and video"},
		{"a log", "text/plain", true, OpenPopup, "text reads in a window"},
		{"a transcript", "application/json", true, OpenPopup, "JSON without the text/ prefix"},
		{"a calendar reply", "application/ics", true, OpenPopup, "an ICS is text"},
		{"a CSV", "application/csv", true, OpenPopup, "a CSV is text"},
		{"a typed mime with parameters", "text/plain; charset=utf-8", true, OpenPopup,
			"parameters are not part of the type"},
		{"a spreadsheet", "application/vnd.ms-excel", true, OpenDownload, "a document is a file"},
		{"a PDF", "application/pdf", true, OpenDownload, "a PDF is a file"},
		{"a zip", "application/zip", true, OpenDownload, "so is an archive"},
		{"an unnamed binary", "application/octet-stream", true, OpenDownload,
			"nothing about it says it reads in a window"},

		// Markup is the security case, not a taste one: rendered from the app's
		// own origin it is script, so it never opens in a window over the page.
		{"an html body", "text/html", true, OpenDownload, "markup is script in our origin"},
		{"an xhtml part", "application/xhtml+xml", true, OpenDownload, "the same problem, another label"},
		{"an SVG", "image/svg+xml", true, OpenDownload, "an SVG is markup wearing an image's type"},

		// With nothing local there is nothing to open, and the chip keeps the
		// source link it already had.
		{"a picture we never pulled", "image/png", false, "",
			"nothing local to open, so the Gmail link stays"},
	}

	for _, c := range cases {
		if got := attachmentOpen(c.mime, c.name, c.stored); got != c.want {
			t.Errorf("%s (%s): got %q, want %q — %s", c.name, c.mime, got, c.want, c.why)
		}
	}
}

// An unlabelled file: the name is the only other evidence there is, and an image
// a sender did not type is still a picture.
func TestAttachmentOpenFallsBackToTheName(t *testing.T) {
	cases := []struct {
		name, want string
	}{
		{"screenshot.png", OpenPopup},
		{"shot.JPEG", OpenPopup},
		{"notes.txt", OpenPopup},
		{"data.csv", OpenPopup},
		{"report.pdf", OpenDownload},
		{"holiday.mov", OpenDownload},
		{"archive.tgz", OpenDownload},
		{"noextension", OpenDownload},
	}
	for _, c := range cases {
		if got := attachmentOpen("application/octet-stream", c.name, true); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

// A type that is a claim about a file we hold beats a name that contradicts it:
// the MIME is what the server will set Content-Disposition from.
func TestAttachmentOpenTrustsATypedMimeOverTheName(t *testing.T) {
	if got := attachmentOpen("application/pdf", "report.png", true); got != OpenDownload {
		t.Errorf("a pdf named .png opened as %q", got)
	}
}

// The chip and the served file are one decision asked twice, and this is the test
// that says so: whatever `open` a chip carries, the header the same file is
// served under must agree with it. A chip promising a window over the page, handed
// a download instead (or worse, markup handed back inline), is a broken window —
// and it is the kind of drift no single handler test can catch, because each side
// looks right on its own.
func TestDispositionIsTheSameDecisionAsOpen(t *testing.T) {
	names := []string{"shot.png", "walkthrough.mp4", "quote.pdf", "plan.docx", "page.html",
		"logo.svg", "notes.txt", "data.csv", "archive.zip", "unlabelled"}
	mimes := []string{"image/png", "video/mp4", "audio/mpeg", "text/plain", "text/csv",
		"text/html", "application/xhtml+xml", "image/svg+xml", "application/pdf",
		"application/zip", "application/json", "application/octet-stream", ""}
	for _, mime := range mimes {
		for _, name := range names {
			open := attachmentOpen(mime, name, true)
			disp := Disposition(mime, name)
			want := "inline"
			if open == OpenDownload {
				want = "attachment"
			}
			if disp != want {
				t.Errorf("%s (%s): open=%q but Content-Disposition=%q, want %q",
					name, mime, open, disp, want)
			}
			// The one case that would be a security bug rather than a broken
			// window: a sender's markup answered inline from the app's origin.
			if disp == "inline" {
				switch mime {
				case "text/html", "application/xhtml+xml", "image/svg+xml":
					t.Errorf("%s (%s): markup served inline from the app's origin is script", name, mime)
				}
			}
		}
	}
}

// The chip has to be able to say why a file is not here. The words are the
// page's; the fact is the spec's.
func TestASkippedAttachmentCarriesWhyAndNotABlob(t *testing.T) {
	s := trail(t)
	// No source_ref on the fixture's attachment, which is the case the reason
	// exists for: nothing to fetch it by, and re-deciding that every pass would
	// re-report it forever.
	if err := s.MarkMediaSkip("mail:<a@loomworks>", "", corpus.MediaSkipTooLarge); err != nil {
		t.Fatalf("MarkMediaSkip: %v", err)
	}
	sp := generate(t, s, Options{Containers: []string{"T1"}})
	att := sp.Messages[0].Attachments[0]
	if att.Skip != corpus.MediaSkipTooLarge {
		t.Errorf("skip = %q, want the corpus's own word for it", att.Skip)
	}
	// A reason and bytes are exclusive states: nothing local to open, and no
	// digest for a client to fetch.
	if att.BlobSHA != "" || att.Open != "" {
		t.Errorf("a skipped attachment carries blobSha=%q open=%q", att.BlobSHA, att.Open)
	}
}

// And the other side of it: once the bytes are here, the digest and the click
// outcome travel, and the reason is gone.
func TestAPulledAttachmentCarriesItsDigestAndOutcome(t *testing.T) {
	s := trail(t)
	if _, err := s.DB().Exec(
		`update attachments set source_ref='part-1' where name='plan.csv'`); err != nil {
		t.Fatalf("giving the fixture a part id: %v", err)
	}
	data := []byte("shed,readings\n")
	if err := s.PutBlob(corpus.Blob{Bytes: data, Mime: "text/csv", Source: corpus.SourceMail}); err != nil {
		t.Fatalf("PutBlob: %v", err)
	}
	sha := corpus.BlobSHA(data)
	if n, err := s.LinkBlob("mail:<a@loomworks>", "part-1", sha); err != nil || n != 1 {
		t.Fatalf("LinkBlob: n=%d err=%v", n, err)
	}
	sp := generate(t, s, Options{Containers: []string{"T1"}})
	att := sp.Messages[0].Attachments[0]
	if att.BlobSHA != sha {
		t.Errorf("blobSha = %q, want %q", att.BlobSHA, sha)
	}
	if att.Open != OpenPopup {
		t.Errorf("open = %q, want %q for a CSV", att.Open, OpenPopup)
	}
	if att.Skip != "" {
		t.Errorf("skip = %q on an attachment we hold the bytes of", att.Skip)
	}
}
