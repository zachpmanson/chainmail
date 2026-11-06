package gmailclient

import (
	"bytes"
	"fmt"
	stdhtml "html"
	"io"
	"mime"
	"mime/quotedprintable"
	"net/mail"
	"regexp"
	"strings"
	"unicode/utf8"

	"golang.org/x/net/html"
)

type draftSpan struct{ start, end int }
type rawDraftHeaders struct {
	blocks []string
	body   int
	header mail.Header
}

// Unlike net/mail, retain every byte and reject non-CRLF or ambiguous fields.
func rawHeaders(raw []byte) (rawDraftHeaders, error) {
	out := rawDraftHeaders{}
	end := bytes.Index(raw, []byte("\r\n\r\n"))
	if end < 0 {
		return out, draftRefusal("malformed_message")
	}
	h := string(raw[:end])
	if strings.ContainsAny(strings.ReplaceAll(h, "\r\n", ""), "\r\n") {
		return out, draftRefusal("malformed_message")
	}
	for _, line := range strings.Split(h, "\r\n") {
		for _, ch := range []byte(line) {
			if ch == 127 || ch < 32 && ch != '\t' || ch > 126 {
				return out, draftRefusal("malformed_message")
			}
		}
		if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
			if len(out.blocks) == 0 {
				return out, draftRefusal("malformed_message")
			}
			out.blocks[len(out.blocks)-1] += line + "\r\n"
			continue
		}
		name, _, ok := strings.Cut(line, ":")
		if !ok || name == "" {
			return out, draftRefusal("malformed_message")
		}
		for _, ch := range name {
			if ch < 33 || ch > 126 {
				return out, draftRefusal("malformed_message")
			}
		}
		out.blocks = append(out.blocks, line+"\r\n")
	}
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return out, draftRefusal("malformed_message")
	}
	out.header = msg.Header
	out.body = end + 4
	for name, values := range out.header {
		if len(values) != 1 && name != "Received" {
			return out, draftRefusal("duplicate_header", name)
		}
	}
	return out, nil
}

func blockName(block string) string {
	name, _, _ := strings.Cut(block, ":")
	return strings.ToLower(name)
}
func editorHeader(name string) bool { return name == "to" || name == "cc" || name == "subject" }
func isAlternative(raw []byte) bool {
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return false
	}
	mt, _, err := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	return err == nil && mt == "multipart/alternative"
}

type draftAlternative struct {
	headers             rawDraftHeaders
	plain, html         draftSpan
	text                string
	rootOpen, rootClose string
}

// Reject duplicate and RFC2231 extended/continued parameters rather than letting
// mime.ParseMediaType merge them into an apparently ordinary parameter map.
func singleDraftMIMEParameter(value, name string) bool {
	if strings.Count(value, ";") != 1 {
		return false
	}
	_, parameter, _ := strings.Cut(value, ";")
	key, _, ok := strings.Cut(parameter, "=")
	return ok && strings.EqualFold(strings.TrimSpace(key), name)
}

func parseAlternative(raw []byte) (draftAlternative, error) {
	var out draftAlternative
	fail := func() (draftAlternative, error) { return out, draftRefusal("alternative_shape") }
	if len(raw) > maxDraftMIMEBytes {
		return fail()
	}
	h, err := rawHeaders(raw)
	if err != nil {
		return out, err
	}
	out.headers = h
	mt, p, err := mime.ParseMediaType(h.header.Get("Content-Type"))
	if err != nil || mt != "multipart/alternative" || len(p) != 1 || p["boundary"] == "" || !singleDraftMIMEParameter(h.header.Get("Content-Type"), "boundary") {
		return fail()
	}
	boundary := p["boundary"]
	// Use the standard constructor's boundary grammar, without serializing any MIME.
	if len(boundary) > 70 || strings.ContainsAny(boundary, "\r\n") {
		return fail()
	}
	for _, c := range boundary {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("'()+_,-./:=?", c)) {
			return fail()
		}
	}
	for name := range h.header {
		switch strings.ToLower(name) {
		case "to", "cc", "subject", "date", "message-id", "received", "from", "mime-version", "content-type", "in-reply-to", "references":
		default:
			return out, draftRefusal("header_missing_or_changed", name)
		}
	}
	if v := h.header.Get("Mime-Version"); v != "" && v != "1.0" {
		return fail()
	}
	// Exact delimiter lines; any boundary-like line with suffixes is ambiguous.
	body := raw[h.body:]
	if bytes.Contains(bytes.ReplaceAll(body, []byte("\r\n"), nil), []byte("\n")) || bytes.Contains(bytes.ReplaceAll(body, []byte("\r\n"), nil), []byte("\r")) {
		return fail()
	}
	var marks []draftSpan
	var closing []bool
	for pos := h.body; pos < len(raw); {
		rel := bytes.Index(raw[pos:], []byte("\r\n"))
		end := len(raw)
		if rel >= 0 {
			end = pos + rel
		}
		line := string(raw[pos:end])
		if strings.HasPrefix(line, "--"+boundary) {
			if line != "--"+boundary && line != "--"+boundary+"--" {
				return fail()
			}
			marks = append(marks, draftSpan{pos, end + 2})
			closing = append(closing, line == "--"+boundary+"--")
		}
		if rel < 0 {
			break
		}
		pos = end + 2
	}
	if len(marks) != 3 || closing[0] || closing[1] || !closing[2] || marks[2].end > len(raw) {
		return fail()
	}
	for i := 0; i < 2; i++ {
		start, end := marks[i].end, marks[i+1].start-2
		if end <= start {
			return fail()
		}
		ph, err := rawHeaders(raw[start:end])
		if err != nil {
			return out, err
		}
		if len(ph.header) != 2 {
			return fail()
		}
		pt, pp, err := mime.ParseMediaType(ph.header.Get("Content-Type"))
		if err != nil || len(pp) != 1 || !strings.EqualFold(pp["charset"], "utf-8") || !singleDraftMIMEParameter(ph.header.Get("Content-Type"), "charset") {
			return fail()
		}
		span := draftSpan{start + ph.body, end}
		switch {
		case pt == "text/plain" && out.plain.end == 0 && strings.EqualFold(ph.header.Get("Content-Transfer-Encoding"), "quoted-printable"):
			out.plain = span
		case pt == "text/html" && out.html.end == 0 && strings.EqualFold(ph.header.Get("Content-Transfer-Encoding"), "7bit"):
			out.html = span
		default:
			return fail()
		}
	}
	if out.plain.end == 0 || out.html.end == 0 {
		return fail()
	}
	encoded := raw[out.plain.start:out.plain.end]
	for _, line := range bytes.Split(encoded, []byte("\r\n")) {
		if len(line) > 76 || bytes.HasSuffix(line, []byte(" ")) || bytes.HasSuffix(line, []byte("\t")) {
			return fail()
		}
	}
	for i := 0; i < len(encoded); i++ {
		if encoded[i] > 127 {
			return fail()
		}
		if encoded[i] == '=' {
			if i+2 >= len(encoded) || !(encoded[i+1] == '\r' && encoded[i+2] == '\n' || mimeHex(encoded[i+1]) && mimeHex(encoded[i+2])) {
				return fail()
			}
			i += 2
		}
	}
	decoded, err := io.ReadAll(quotedprintable.NewReader(bytes.NewReader(encoded)))
	if err != nil {
		return fail()
	}
	out.text = strings.ReplaceAll(string(decoded), "\r\n", "\n")
	if !safeDraftText(out.text) {
		return fail()
	}
	htmlRaw := raw[out.html.start:out.html.end]
	for _, line := range bytes.Split(htmlRaw, []byte("\r\n")) {
		if len(line) > 998 {
			return fail()
		}
	}
	for _, b := range htmlRaw {
		if b > 127 {
			return fail()
		}
	}
	projection, open, close, generated, err := projectDraftHTML(string(htmlRaw))
	if err != nil {
		return out, err
	}
	out.rootOpen, out.rootClose = open, close
	if projection != out.text {
		if !generated && !strings.ContainsRune(out.text, '\u00a0') {
			projection = strings.ReplaceAll(projection, "\u00a0", " ")
		}
		if projection != out.text && (generated || strings.HasSuffix(projection, "\n") || projection+"\n" != out.text) {
			return out, draftRefusal("alternative_text_mismatch")
		}
	}
	return out, nil
}

func safeDraftText(s string) bool {
	if !utf8.ValidString(s) || len(s) > maxDraftHTMLBytes {
		return false
	}
	for _, r := range s {
		if r < 32 && r != '\n' && r != '\t' || r == 127 || r == '\u2028' || r == '\u2029' {
			return false
		}
	}
	return true
}

var draftRoot = regexp.MustCompile(`^<div dir=(?:"ltr"|'ltr'|ltr)>`)

const draftPreWrap = `<span style="white-space: pre-wrap">`

// This is a recognizer, not an HTML sanitizer. No browser error recovery, hidden
// attributes, comments, foreign content, nested blocks or formatting are accepted.
func projectDraftHTML(source string) (text, open, close string, generated bool, err error) {
	fail := func() (string, string, string, bool, error) { return "", "", "", false, draftRefusal("html_grammar") }
	if len(source) > maxDraftHTMLBytes {
		return fail()
	}
	open = draftRoot.FindString(source)
	close = "</div>"
	if open == "" || !strings.HasSuffix(source, close) {
		return fail()
	}
	inner := source[len(open) : len(source)-len(close)]
	if strings.HasPrefix(inner, draftPreWrap) && strings.HasSuffix(inner, "</span>") {
		raw := inner[len(draftPreWrap) : len(inner)-len("</span>")]
		if strings.ContainsAny(raw, "<>") {
			return fail()
		}
		decoded, ok := decodeDraftHTMLText(raw)
		decoded = strings.ReplaceAll(decoded, "\r\n", "\n")
		if !ok || !safeDraftText(decoded) {
			return fail()
		}
		return decoded, open, close, true, nil
	}
	tokens := html.NewTokenizer(strings.NewReader(inner))
	var segments []string
	var inline strings.Builder
	block := ""
	seenBlock := false
	layoutText := func() string {
		lines := strings.Split(inline.String(), "\n")
		for i := range lines {
			lines[i] = strings.Trim(lines[i], " ")
		}
		return strings.Join(lines, "\n")
	}
	flush := func() {
		segments = append(segments, layoutText())
		inline.Reset()
	}
	for n := 0; ; n++ {
		if n > maxDraftHTMLTokens {
			return fail()
		}
		kind := tokens.Next()
		raw := string(tokens.Raw())
		switch kind {
		case html.ErrorToken:
			if tokens.Err() != io.EOF || raw != "" || block != "" {
				return fail()
			}
			if inline.Len() > 0 || len(segments) == 0 {
				if strings.HasPrefix(layoutText(), "\n") || strings.HasSuffix(layoutText(), "\n") {
					return fail()
				}
				flush()
			}
			return strings.Join(segments, "\n"), open, close, false, nil
		case html.TextToken:
			if seenBlock && block == "" {
				return fail()
			}
			value, ok := decodeDraftHTMLText(raw)
			value = strings.ReplaceAll(value, "\r\n", "\n")
			if !ok || !safeDraftText(value) {
				return fail()
			}
			// HTML collapses ASCII whitespace, but NBSP is retained until the bounded
			// equivalence comparison against the old plain representation.
			var collapsed strings.Builder
			space := false
			for _, r := range value {
				if strings.ContainsRune(" \t\r\n\f", r) {
					if !space {
						collapsed.WriteByte(' ')
					}
					space = true
				} else {
					collapsed.WriteRune(r)
					space = false
				}
			}
			inline.WriteString(collapsed.String())
		case html.StartTagToken, html.SelfClosingTagToken:
			switch raw {
			case "<br>", "<br/>", "<br />":
				if seenBlock && block == "" {
					return fail()
				}
				inline.WriteByte('\n')
			case "<div>", "<p>":
				if block != "" {
					return fail()
				}
				if inline.Len() > 0 {
					if layoutText() == "" || strings.HasPrefix(layoutText(), "\n") || strings.HasSuffix(layoutText(), "\n") {
						return fail()
					}
					flush()
				}
				block = raw
				seenBlock = true
			default:
				return fail()
			}
		case html.EndTagToken:
			if !(block == "<div>" && raw == "</div>" || block == "<p>" && raw == "</p>") {
				return fail()
			}
			if inline.String() == "\n" {
				inline.Reset()
			} else if layoutText() == "" || strings.HasPrefix(layoutText(), "\n") || strings.HasSuffix(layoutText(), "\n") {
				return fail()
			}
			flush()
			block = ""
		default:
			return fail()
		}
	}
}

func decodeDraftHTMLText(raw string) (string, bool) {
	for pos := 0; pos < len(raw); pos++ {
		if raw[pos] != '&' {
			continue
		}
		end := strings.IndexByte(raw[pos:], ';')
		if end < 0 {
			return "", false
		}
		entity := raw[pos : pos+end+1]
		if stdhtml.UnescapeString(entity) == entity {
			return "", false
		}
		pos += end
	}
	return stdhtml.UnescapeString(raw), true
}

func encodeDraftHTMLText(text string) string {
	var out strings.Builder
	for _, r := range text {
		switch r {
		case '&':
			out.WriteString("&amp;")
		case '<':
			out.WriteString("&lt;")
		case '>':
			out.WriteString("&gt;")
		default:
			if r > 126 {
				fmt.Fprintf(&out, "&#%d;", r)
			} else if r == '\n' {
				out.WriteString("\r\n")
			} else {
				out.WriteRune(r)
			}
		}
	}
	return out.String()
}

func spliceAlternative(raw []byte, a draftAlternative, text string) ([]byte, error) {
	if !safeDraftText(text) {
		return nil, draftRefusal("body_text")
	}
	var qp bytes.Buffer
	w := quotedprintable.NewWriter(&qp)
	_, err := io.WriteString(w, strings.ReplaceAll(text, "\n", "\r\n"))
	if err != nil {
		return nil, err
	}
	if err = w.Close(); err != nil {
		return nil, err
	}
	htmlBody := []byte(a.rootOpen + draftPreWrap + encodeDraftHTMLText(text) + "</span>" + a.rootClose)
	// Refuse even a boundary-looking line rather than relying on MIME reader recovery.
	_, params, _ := mime.ParseMediaType(a.headers.header.Get("Content-Type"))
	marker := []byte("--" + params["boundary"])
	for _, body := range [][]byte{qp.Bytes(), htmlBody} {
		if bytes.HasPrefix(body, marker) || bytes.Contains(body, append([]byte("\r\n"), marker...)) {
			return nil, draftRefusal("boundary_collision")
		}
	}
	spans := []draftSpan{a.plain, a.html}
	bodies := [][]byte{qp.Bytes(), htmlBody}
	if spans[0].start > spans[1].start {
		spans[0], spans[1] = spans[1], spans[0]
		bodies[0], bodies[1] = bodies[1], bodies[0]
	}
	var out []byte
	pos := 0
	for i, span := range spans {
		out = append(out, raw[pos:span.start]...)
		out = append(out, bodies[i]...)
		pos = span.end
	}
	out = append(out, raw[pos:]...)
	return out, nil
}

// The skeleton contains all non-editor bytes, including wrapper, headers,
// boundary framing, preamble and epilogue. Only the two proven body slots vary.
func alternativeSkeleton(raw []byte, a draftAlternative) []byte {
	spans := []draftSpan{a.plain, a.html}
	if spans[0].start > spans[1].start {
		spans[0], spans[1] = spans[1], spans[0]
	}
	var out []byte
	for _, block := range a.headers.blocks {
		if !editorHeader(blockName(block)) {
			out = append(out, block...)
		}
	}
	out = append(out, '\r', '\n')
	pos := a.headers.body
	for _, s := range spans {
		out = append(out, raw[pos:s.start]...)
		out = append(out, 0)
		pos = s.end
	}
	return append(out, raw[pos:]...)
}

func checkAlternativeReplacement(current, replacement []byte) error {
	old, err := parseAlternative(current)
	if err != nil {
		return err
	}
	next, err := parseAlternative(replacement)
	if err != nil {
		return err
	}
	if old.rootOpen != next.rootOpen || old.rootClose != next.rootClose || !bytes.Equal(alternativeSkeleton(current, old), alternativeSkeleton(replacement, next)) {
		return draftRefusal("immutable_mime_changed")
	}
	return nil
}
