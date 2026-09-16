package spec

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Click outcomes for an attachment whose bytes the corpus holds.
const (
	// OpenPopup is a file that reads in a window over the page: a picture, a
	// video, a recording, a text file.
	OpenPopup = "popup"
	// OpenDownload is everything else — a PDF, a document, an archive — where a
	// reader wants the file rather than a look at it.
	OpenDownload = "download"
)

// How a file is shown in the window over the page. Each one is an element the
// page has to build: an image, a block of text, a framed document.
const (
	ViewImage = "image"
	ViewText  = "text"
	ViewPDF   = "pdf"
)

// baseMime is the type on its own: without parameters, without case, and empty
// when the source had nothing to say. `text/plain; charset=utf-8` and
// `TEXT/PLAIN` are one type, and every question about a file's type starts here.
func baseMime(mime string) string {
	m := strings.ToLower(strings.TrimSpace(mime))
	if i := strings.Index(m, ";"); i >= 0 {
		m = strings.TrimSpace(m[:i])
	}
	return m
}

// isMarkup is true of the types that must never be rendered from this origin. It
// is asked first wherever a type is judged, because this is a security rule
// rather than a taste one: served or framed from the app's own origin, a sender's
// HTML is script. Not a text/html part, and not an SVG, which is the same problem
// wearing an image's MIME type.
func isMarkup(m string) bool {
	switch m {
	case "text/html", "application/xhtml+xml", "image/svg+xml":
		return true
	}
	return false
}

// readsAsText is true of the types that read as text without saying text/: a
// transcript of an ICS file, a CSV, a JSON payload, a log. None of the senders
// who produced them cared what type they put on them.
func readsAsText(m string) bool {
	if strings.HasPrefix(m, "text/") {
		return true
	}
	switch m {
	case "application/json", "application/xml", "application/ics",
		"application/csv", "application/x-ndjson", "application/yaml":
		return true
	}
	return false
}

// isOpaque is a type that claims nothing about the bytes: the spellings of an
// untyped stream, which is what a sender's mail client leaves on anything it does
// not recognise. The name is then the only evidence there is.
func isOpaque(m string) bool {
	return m == "" || m == "application/octet-stream" || m == "binary/octet-stream"
}

// What a name can say about a file whose type says nothing.
var (
	textExts = map[string]bool{
		".txt": true, ".log": true, ".md": true, ".csv": true, ".json": true,
		".xml": true, ".ics": true,
	}
	imageExts = map[string]bool{
		".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true,
		".bmp": true,
	}
)

// attachmentOpen says what clicking an attachment with local bytes should do,
// and is deliberately the server's call rather than the client's.
//
// The rule is the same one that decides Content-Disposition when the file is
// served, and the two must not be able to disagree: a chip promising a popup for
// something the server hands over as an attachment is a broken window. So the
// decision is made once, here, from the stored MIME, and travels in the spec.
//
// Type, not size, is what picks the branch. A 40 MB PDF stays a download
// whatever it weighs, and a small one is still not something to read in a modal.
//
// `stored` is whether the bytes are actually in the corpus: with nothing local
// there is nothing to open, and the answer is empty so that the chip keeps the
// source link it already had.
func attachmentOpen(mime, name string, stored bool) string {
	if !stored {
		return ""
	}
	m := baseMime(mime)
	if isMarkup(m) {
		return OpenDownload
	}
	switch {
	case m == "", strings.HasPrefix(m, "image/"), strings.HasPrefix(m, "video/"),
		strings.HasPrefix(m, "audio/"), readsAsText(m):
		return OpenPopup
	}
	// An unlabelled file: fall back to the name, the only other evidence there is.
	// An image the sender did not type is still a picture.
	if isOpaque(m) {
		ext := strings.ToLower(filepath.Ext(name))
		if textExts[ext] || imageExts[ext] {
			return OpenPopup
		}
	}
	return OpenDownload
}

// attachmentView says how the bytes are shown in the window over the page, or ""
// when they cannot be shown at all and are only worth taking as a file.
//
// This answers a different question from attachmentOpen, and the PDF is where the
// two deliberately differ: a click on a PDF takes it as a file — its
// Content-Disposition is `attachment`, and `open` says so — while the page can
// still *show* it, in the browser's own PDF viewer. So `open` is about the link and
// this is about what the window over the transcript contains; neither is derived
// from the other, because they are not the same decision.
//
// Video and audio are left out on purpose: they come with their own controls and a
// tab to play in, and the window is for what cannot be read anywhere else — a
// picture, a block of text, a document.
//
// Markup is excluded for the reason attachmentOpen excludes it, and one more: a
// frame is a document, so an HTML or SVG part framed here would be the app's own
// origin executing the sender's script.
func attachmentView(mime, name string, stored bool) string {
	if !stored {
		return ""
	}
	m := baseMime(mime)
	if isMarkup(m) {
		return ""
	}
	switch {
	case m == "application/pdf":
		return ViewPDF
	case strings.HasPrefix(m, "image/"):
		return ViewImage
	case readsAsText(m):
		return ViewText
	}
	if isOpaque(m) {
		switch ext := strings.ToLower(filepath.Ext(name)); {
		case ext == ".pdf":
			return ViewPDF
		case textExts[ext]:
			return ViewText
		case imageExts[ext]:
			return ViewImage
		}
	}
	return ""
}

// Disposition is the Content-Disposition for bytes the corpus holds, decided by
// the same rule that put `open` on the chip.
//
// One rule, asked twice, rather than two rules that agree: the server that serves
// the file and the page that promises what a click does must not be able to
// disagree, and the way to guarantee that is for the header to be derived from
// this function and nothing else. `inline` is refused for markup because
// attachmentOpen refuses it first, which is the part that matters — served from
// the app's own origin, a sender's HTML is script.
func Disposition(mime, name string) string {
	if attachmentOpen(mime, name, true) == OpenDownload {
		return "attachment"
	}
	return "inline"
}

// attachmentKind is the short human label shown on the chip. Derived from the
// MIME type, falling back to the file extension, because a MIME type of
// application/octet-stream is common and says nothing.
func attachmentKind(mime, name string) string {
	m := baseMime(mime)
	if k, ok := mimeKinds[m]; ok {
		return k
	}
	if strings.HasPrefix(m, "image/") {
		return "image"
	}
	if ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(name)), "."); ext != "" {
		return strings.ToUpper(ext)
	}
	return ""
}

var mimeKinds = map[string]string{
	"text/csv":                 "CSV",
	"application/csv":          "CSV",
	"text/plain":               "text",
	"text/html":                "HTML",
	"text/calendar":            "calendar",
	"application/ics":          "calendar",
	"application/pdf":          "PDF",
	"application/zip":          "ZIP",
	"application/json":         "JSON",
	"application/msword":       "DOC",
	"application/vnd.ms-excel": "XLS",
	"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":         "XLSX",
	"application/vnd.openxmlformats-officedocument.wordprocessingml.document":   "DOCX",
	"application/vnd.openxmlformats-officedocument.presentationml.presentation": "PPTX",
}

// humanSize renders a byte count the way a mail client does. Sizes are for
// orientation, so one decimal place below ten units is as much precision as is
// useful.
func humanSize(n int64) string {
	if n <= 0 {
		return ""
	}
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	units := []string{"KB", "MB", "GB", "TB"}
	v := float64(n) / 1024
	for i, u := range units {
		if v < 1024 || i == len(units)-1 {
			if v < 10 {
				return fmt.Sprintf("%.1f %s", v, u)
			}
			return fmt.Sprintf("%.0f %s", v, u)
		}
		v /= 1024
	}
	return ""
}
