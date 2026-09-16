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
	m := strings.ToLower(strings.TrimSpace(mime))
	if i := strings.Index(m, ";"); i >= 0 {
		m = strings.TrimSpace(m[:i])
	}
	// Markup is the exception, and it is checked FIRST because it is the one case
	// that has to win over the type's own family: it is a security rule rather
	// than a taste one — rendered from the app's own origin it is script, so it
	// never opens in a window over the page. Not for a text/html part, and not for
	// an SVG, which is the same problem wearing an image's MIME type.
	switch m {
	case "text/html", "application/xhtml+xml", "image/svg+xml":
		return OpenDownload
	}
	switch {
	case m == "", strings.HasPrefix(m, "image/"), strings.HasPrefix(m, "video/"),
		strings.HasPrefix(m, "audio/"), strings.HasPrefix(m, "text/"):
		return OpenPopup
	}
	switch m {
	// Text that does not say text/. A transcript of an ICS file, a CSV, a JSON
	// payload, a log: all of them read as text, and none of the senders who
	// produced them cared what the type said.
	case "application/json", "application/xml", "application/ics",
		"application/csv", "application/x-ndjson", "application/yaml":
		return OpenPopup
	}
	// An unlabelled file: fall back to the name, the only other evidence there is.
	// An image the sender did not type is still a picture.
	if m == "application/octet-stream" || m == "binary/octet-stream" {
		if ext := strings.ToLower(filepath.Ext(name)); ext != "" {
			switch ext {
			case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp", ".txt", ".log",
				".md", ".csv", ".json", ".xml", ".ics":
				return OpenPopup
			}
		}
	}
	return OpenDownload
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
	m := strings.ToLower(strings.TrimSpace(mime))
	if i := strings.Index(m, ";"); i >= 0 {
		m = strings.TrimSpace(m[:i])
	}
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
