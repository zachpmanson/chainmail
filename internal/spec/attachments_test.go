package spec

import "testing"

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
