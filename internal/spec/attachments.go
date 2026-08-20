package spec

import (
	"fmt"
	"path/filepath"
	"strings"
)

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
