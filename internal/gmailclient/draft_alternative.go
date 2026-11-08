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
	"sort"
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
	plainEncoding       string
	encodingValue       draftSpan
	formatValue         draftSpan
	format              string
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
	failureCode := "alternative_size"
	fail := func() (draftAlternative, error) { return out, draftRefusal(failureCode) }
	if len(raw) > maxDraftMIMEBytes {
		return fail()
	}
	h, err := rawHeaders(raw)
	if err != nil {
		return out, err
	}
	out.headers = h
	failureCode = "alternative_root_type"
	mt, p, err := mime.ParseMediaType(h.header.Get("Content-Type"))
	if err != nil || mt != "multipart/alternative" || len(p) != 1 || p["boundary"] == "" || !singleDraftMIMEParameter(h.header.Get("Content-Type"), "boundary") {
		return fail()
	}
	failureCode = "alternative_boundary"
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
	failureCode = "alternative_version"
	if v := h.header.Get("Mime-Version"); v != "" && v != "1.0" {
		return fail()
	}
	// Exact delimiter lines; any boundary-like line with suffixes is ambiguous.
	failureCode = "alternative_framing"
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
			markEnd := end
			if rel >= 0 {
				markEnd += 2
			} else if line != "--"+boundary+"--" {
				return fail()
			}
			marks = append(marks, draftSpan{pos, markEnd})
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
		failureCode = "alternative_framing"
		start, end := marks[i].end, marks[i+1].start-2
		if end <= start {
			return fail()
		}
		ph, err := rawHeaders(raw[start:end])
		if err != nil {
			return out, err
		}
		failureCode = "alternative_part_headers"
		for name := range ph.header {
			if name != "Content-Type" && name != "Content-Transfer-Encoding" {
				return fail()
			}
		}
		encoding := strings.ToLower(ph.header.Get("Content-Transfer-Encoding"))
		if _, present := ph.header["Content-Transfer-Encoding"]; present && encoding == "" {
			return fail()
		}
		failureCode = "alternative_part_type"
		pt, pp, err := mime.ParseMediaType(ph.header.Get("Content-Type"))
		plainFormat, formatValue, formatOK := plainDraftFormat(ph, pp)
		ordinary := len(pp) == 1 && strings.EqualFold(pp["charset"], "utf-8") && singleDraftMIMEParameter(ph.header.Get("Content-Type"), "charset")
		if err != nil || !(ordinary || pt == "text/plain" && formatOK) {
			if err == nil && pt == "text/plain" {
				switch strings.ToLower(pp["format"]) {
				case "flowed":
					failureCode = "alternative_plain_flowed"
					if delsp, present := pp["delsp"]; present {
						switch strings.ToLower(delsp) {
						case "yes":
							failureCode = "alternative_plain_flowed_delsp_yes"
						case "no":
							failureCode = "alternative_plain_flowed_delsp_no"
						default:
							failureCode = "alternative_plain_flowed_delsp_other"
						}
					}
					for name := range pp {
						if name != "charset" && name != "format" && name != "delsp" {
							failureCode = "alternative_plain_flowed_extra"
						}
					}
				case "fixed":
					failureCode = "alternative_plain_fixed"
				default:
					failureCode = "alternative_plain_parameters"
				}
			} else if err == nil && pt == "text/html" {
				failureCode = "alternative_html_parameters"
			}
			return fail()
		}
		span := draftSpan{start + ph.body, end}
		switch {
		case pt == "text/plain" && out.plain.end == 0 && (encoding == "" || encoding == "7bit" || encoding == "quoted-printable"):
			out.plain = span
			out.plainEncoding = encoding
			// Missing CTE is inserted immediately before the header/body separator.
			out.encodingValue = draftSpan{start + ph.body - 2, start + ph.body - 2}
			offset := start
			for _, block := range ph.blocks {
				if blockName(block) == "content-transfer-encoding" {
					colon := strings.IndexByte(block, ':') + 1
					value := strings.Trim(block[colon:], " \t\r\n")
					begin := colon + strings.Index(block[colon:], value)
					out.encodingValue = draftSpan{offset + begin, offset + begin + len(value)}
				}
				offset += len(block)
			}
			if formatOK {
				out.format = plainFormat
				out.formatValue = draftSpan{start + formatValue.start, start + formatValue.end}
			}
		case pt == "text/html" && out.html.end == 0 && (encoding == "" || encoding == "7bit"):
			out.html = span
		default:
			return fail()
		}
	}
	failureCode = "alternative_part_pair"
	if out.plain.end == 0 || out.html.end == 0 {
		return fail()
	}
	failureCode = "alternative_plain_lines"
	encoded := raw[out.plain.start:out.plain.end]
	decoded := encoded
	if out.plainEncoding == "quoted-printable" {
		for _, line := range bytes.Split(encoded, []byte("\r\n")) {
			if len(line) > 76 || bytes.HasSuffix(line, []byte(" ")) || bytes.HasSuffix(line, []byte("\t")) {
				return fail()
			}
		}
		failureCode = "alternative_plain_encoding"
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
		decoded, err = io.ReadAll(quotedprintable.NewReader(bytes.NewReader(encoded)))
		if err != nil {
			return fail()
		}
	} else {
		for _, line := range bytes.Split(encoded, []byte("\r\n")) {
			if len(line) > 998 {
				return fail()
			}
		}
		failureCode = "alternative_plain_encoding"
		if !safeDraft7bit(encoded) {
			return fail()
		}
	}
	failureCode = "alternative_plain_text"
	out.text = strings.ReplaceAll(string(decoded), "\r\n", "\n")
	if !safeDraftText(out.text) {
		return fail()
	}
	if out.format == "flowed" && !inertDraftFlowed(out.text) {
		return out, draftRefusal("alternative_plain_flowed")
	}
	failureCode = "alternative_html_lines"
	htmlRaw := raw[out.html.start:out.html.end]
	for _, line := range bytes.Split(htmlRaw, []byte("\r\n")) {
		if len(line) > 998 {
			return fail()
		}
	}
	failureCode = "alternative_html_encoding"
	if !safeDraft7bit(htmlRaw) {
		return fail()
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

// CRLF framing is checked before this; neither default 7bit body is QP-decoded.
func safeDraft7bit(raw []byte) bool {
	for _, b := range raw {
		if b > 126 || b < 32 && b != '\t' && b != '\r' && b != '\n' {
			return false
		}
	}
	return true
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

var draftRoot = regexp.MustCompile(`^<div dir=(?:"(?:ltr|auto)"|'(?:ltr|auto)'|(?:ltr|auto))>`)

const draftPreWrap = `<span style="white-space: pre-wrap">`

// This is a recognizer, not an HTML sanitizer. No browser error recovery, hidden
// attributes, comments, foreign content, nested blocks or formatting are accepted.
func projectDraftHTML(source string) (text, open, close string, generated bool, err error) {
	fail := func() (string, string, string, bool, error) { return "", "", "", false, draftRefusal("html_grammar") }
	if len(source) > maxDraftHTMLBytes {
		return fail()
	}
	// One transport CRLF may follow the complete root/envelope, not arbitrary
	// HTML whitespace. Retain it as immutable closing-wrapper bytes on edits.
	trailing := ""
	if strings.HasSuffix(source, "\r\n") {
		source = source[:len(source)-2]
		trailing = "\r\n"
	}
	const envelopeOpen, envelopeClose = "<html><body>", "</body></html>"
	enveloped := strings.HasPrefix(source, envelopeOpen)
	if enveloped {
		if !strings.HasSuffix(source, envelopeClose) {
			return fail()
		}
		source = source[len(envelopeOpen) : len(source)-len(envelopeClose)]
	}
	open = draftRoot.FindString(source)
	close = "</div>"
	if open == "" || !strings.HasSuffix(source, close) {
		return fail()
	}
	inner := source[len(open) : len(source)-len(close)]
	if enveloped {
		open, close = envelopeOpen+open, close+envelopeClose
	}
	close += trailing
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
	if text == a.text {
		return raw, nil
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
	type edit struct {
		span draftSpan
		body []byte
	}
	edits := []edit{{a.plain, qp.Bytes()}, {a.html, htmlBody}}
	if a.plainEncoding != "quoted-printable" {
		edits = append(edits, edit{a.encodingValue, alternativeEncodingPromotion(a)})
	}
	if a.format == "flowed" {
		edits = append(edits, edit{a.formatValue, []byte("fixed")})
	}
	sort.Slice(edits, func(i, j int) bool { return edits[i].span.start < edits[j].span.start })
	var out []byte
	pos := 0
	for _, e := range edits {
		out = append(out, raw[pos:e.span.start]...)
		out = append(out, e.body...)
		pos = e.span.end
	}
	out = append(out, raw[pos:]...)
	return out, nil
}

func alternativeEncodingPromotion(a draftAlternative) []byte {
	if a.plainEncoding == "" {
		return []byte("Content-Transfer-Encoding: quoted-printable\r\n")
	}
	return []byte("quoted-printable")
}

// All part headers remain exact. Only the positively located old encoding token
// (or missing-header insertion point) can be normalized to the constructor's
// exact forward promotion, after the independent decoded-text transition proof.
func alternativeSkeleton(raw []byte, a draftAlternative) []byte {
	return alternativeEncodingSkeleton(raw, a, false)
}

func alternativeEncodingSkeleton(raw []byte, a draftAlternative, promote bool) []byte {
	type slot struct {
		span  draftSpan
		value []byte
	}
	slots := []slot{{a.plain, []byte{0}}, {a.html, []byte{0}}}
	if a.format != "" {
		slots = append(slots, slot{a.formatValue, []byte{0}})
	}
	if promote {
		slots = append(slots, slot{a.encodingValue, alternativeEncodingPromotion(a)})
	}
	sort.Slice(slots, func(i, j int) bool { return slots[i].span.start < slots[j].span.start })
	var out []byte
	for _, block := range a.headers.blocks {
		if !editorHeader(blockName(block)) {
			out = append(out, block...)
		}
	}
	out = append(out, '\r', '\n')
	pos := a.headers.body
	for _, s := range slots {
		out = append(out, raw[pos:s.span.start]...)
		out = append(out, s.value...)
		pos = s.span.end
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
	bodyChanged := old.text != next.text
	promoting := bodyChanged && old.plainEncoding != "quoted-printable"
	if promoting && next.plainEncoding != "quoted-printable" {
		return draftRefusal("immutable_mime_changed")
	}
	if !bodyChanged && (!bytes.Equal(current[old.plain.start:old.plain.end], replacement[next.plain.start:next.plain.end]) || !bytes.Equal(current[old.html.start:old.html.end], replacement[next.html.start:next.html.end])) {
		return draftRefusal("immutable_mime_changed")
	}
	normalizing := old.format == "flowed" && next.format == "fixed" && bodyChanged
	sameFormat := old.format == next.format && bytes.Equal(current[old.formatValue.start:old.formatValue.end], replacement[next.formatValue.start:next.formatValue.end])
	if !normalizing && (!sameFormat || old.format == "flowed" && bodyChanged) {
		return draftRefusal("immutable_mime_changed")
	}
	if old.rootOpen != next.rootOpen || old.rootClose != next.rootClose || !bytes.Equal(alternativeEncodingSkeleton(current, old, promoting), alternativeSkeleton(replacement, next)) {
		return draftRefusal("immutable_mime_changed")
	}
	return nil
}
