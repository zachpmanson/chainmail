package main

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/zachpmanson/chainmail/internal/corpus"
	"github.com/zachpmanson/chainmail/internal/spec"
)

// attachment serves bytes the corpus holds: GET /v1/attachments/{sha}, where a
// chip's click lands once its file has been pulled.
//
// No -media gate, unlike POST /v1/media/pull. That switch is about *asking the
// mailbox* for something; this reads what is already on disk, and costs nothing
// a page does not already have — the preview embedded in the spec is the same
// bytes in the same process's hands. A host restarted without -media should not
// lose the ability to open the files it fetched while it had it.
//
// The disposition comes from spec.Disposition, the rule that decided the chip's
// `open` — so a chip promising a window over the page cannot be handed a
// download instead, and markup cannot go inline at all (spec.attachmentOpen
// checks that first): served from this server's own origin, a sender's HTML
// would be script running beside the app.
func (s *server) attachment(w http.ResponseWriter, r *http.Request) {
	sha := strings.TrimSpace(r.PathValue("sha"))
	if !isDigest(sha) {
		fail(w, http.StatusBadRequest, fmt.Errorf(
			"%q is not a blob digest: this path takes the sha256 that a spec carries in blobSha", sha))
		return
	}
	blob, err := s.store.Blob(sha)
	if errors.Is(err, corpus.ErrNoBlob) {
		fail(w, http.StatusNotFound, fmt.Errorf(
			"no bytes filed under %s: this attachment has not been pulled, or its file was pruned", sha))
		return
	}
	if err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	// The attachment row's MIME and name, not the blob's, because those are what
	// the chip was labelled and judged with. Falls back to what the blob recorded
	// when it was fetched, for bytes whose rows are gone.
	name, mime, _ := s.store.BlobName(sha)
	if mime == "" {
		mime = blob.Mime
	}
	if name == "" {
		name = sha
	}
	if mime == "" {
		mime = "application/octet-stream"
	}
	h := w.Header()
	h.Set("Content-Type", mime)
	h.Set("Content-Disposition", contentDisposition(spec.Disposition(mime, name), name))
	// The stated type is the only type a browser may use, so a part that arrived
	// mislabelled cannot become HTML in this origin on the strength of its bytes.
	h.Set("X-Content-Type-Options", "nosniff")
	// The digest is the content, so this URL cannot change what it means: one of
	// the few honest immutables. private, because it is somebody's mail.
	h.Set("ETag", `"`+sha+`"`)
	h.Set("Cache-Control", "private, max-age=31536000, immutable")
	// ServeContent rather than a write, for the parts that are easy to get wrong
	// by hand: Content-Length, Range (a stored video seeks, a re-opened PDF costs
	// nothing), and the conditional requests the headers above then promise.
	// Content-Type is already set, so its sniffing never runs.
	http.ServeContent(w, r, name, blob.FetchedAt, bytes.NewReader(blob.Bytes))
}

// isDigest reports whether s has the shape of a blob digest: 64 hex characters,
// which is sha256 in the lower case hex the store files under.
//
// Checked before the query rather than left to it, because the path is a caller's
// text and every other answer this surface gives a mistake is a named one.
func isDigest(s string) bool {
	if len(s) != sha256.Size*2 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// contentDisposition renders the header for a filed file, per RFC 6266.
//
// The filename is sender-controlled text going into a header value, and a header
// value is a line: a CR or LF in it is header injection, so those are dropped
// rather than escaped — there is no escape that means "the value ends here".
// Quotes and backslashes are escaped, because they would end the quoted-string
// early and hand the rest of the name to the parser as parameters.
//
// A name with anything outside ASCII in it also carries the RFC 5987 form, which
// every current browser prefers; the quoted form stays as the fallback an older
// client reads, and keeps the extension, which is the part a client acts on.
func contentDisposition(disp, name string) string {
	// quote escapes what would end the quoted-string early and hand the rest of the
	// name to the parser as parameters.
	quote := func(s string) string {
		return strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s)
	}
	clean := strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, name)
	if clean == "" {
		return disp
	}
	ascii := true
	for _, r := range clean {
		if r > 0x7f {
			ascii = false
			break
		}
	}
	if ascii {
		return fmt.Sprintf(`%s; filename="%s"`, disp, quote(clean))
	}
	fallback := strings.TrimSpace(strings.Map(func(r rune) rune {
		if r > 0x7f {
			return -1
		}
		return r
	}, clean))
	if fallback == "" {
		fallback = "file"
	}
	return fmt.Sprintf(`%s; filename="%s"; filename*=UTF-8''%s`,
		disp, quote(fallback), url.PathEscape(clean))
}
