package gmailclient

import (
	"bytes"
	"mime"
	"net/mail"
	"strings"
)

// Normalize editor CRLF only on the supported alternative path. The existing
// top-level plain constructor is intentionally unchanged.
func draftReplacementMIME(d Draft, current []byte) ([]byte, error) {
	if isAlternative(current) {
		d.Body = strings.ReplaceAll(d.Body, "\r\n", "\n")
		if !safeDraftText(d.Body) {
			return nil, draftRefusal("body_text")
		}
	}
	return draftMIME(d)
}

// Save and GET-only save-check share construction and the final-byte guard.
func (c Client) prepareDraftReplacement(current, replacement []byte) ([]byte, error) {
	raw, err := c.preserveDraftMetadata(current, replacement)
	if err != nil {
		return nil, err
	}
	if isAlternative(current) {
		a, err := parseAlternative(current)
		if err != nil {
			return nil, err
		}
		next, err := rawHeaders(raw)
		if err != nil {
			return nil, err
		}
		for name, values := range a.headers.header {
			key := strings.ToLower(name)
			if editorHeader(key) || key == "content-type" || key == "mime-version" {
				continue
			}
			nextValues, present := next.header[name]
			if !present || strings.Join(values, "\n") != strings.Join(nextValues, "\n") {
				return nil, draftRefusal("header_missing_or_changed", name)
			}
		}
		for name := range next.header {
			key := strings.ToLower(name)
			if editorHeader(key) || key == "content-type" || key == "content-transfer-encoding" || key == "mime-version" {
				continue
			}
			if _, ok := a.headers.header[name]; !ok {
				return nil, draftRefusal("header_missing_or_changed", name)
			}
		}
		d, err := parseDraft(replacement)
		if err != nil {
			return nil, draftRefusal("replacement_body")
		}
		body := current
		if d.Body != a.text {
			body, err = spliceAlternative(current, a, d.Body)
			if err != nil {
				return nil, err
			}
		}
		var header bytes.Buffer
		seen := map[string]bool{}
		for _, block := range a.headers.blocks {
			key := blockName(block)
			if editorHeader(key) {
				seen[key] = true
				if !sameEditorHeader(key, a.headers.header.Get(key), next.header.Get(key)) {
					for _, b := range next.blocks {
						if blockName(b) == key {
							header.WriteString(b)
						}
					}
					continue
				}
			}
			header.WriteString(block)
		}
		for _, block := range next.blocks {
			key := blockName(block)
			if editorHeader(key) && !seen[key] {
				header.WriteString(block)
			}
		}
		header.WriteString("\r\n")
		header.Write(body[a.headers.body:])
		raw = header.Bytes()
	}
	if err := checkDraftReplacement(current, raw); err != nil {
		return nil, err
	}
	return raw, nil
}

func sameEditorHeader(name, a, b string) bool {
	if name == "subject" {
		decoder := new(mime.WordDecoder)
		x, e1 := decoder.DecodeHeader(a)
		y, e2 := decoder.DecodeHeader(b)
		return e1 == nil && e2 == nil && x == y
	}
	canonical := func(s string) string {
		if s == "" {
			return ""
		}
		list, err := mail.ParseAddressList(s)
		if err != nil {
			return "invalid:" + s
		}
		var parts []string
		for _, a := range list {
			parts = append(parts, a.String())
		}
		return strings.Join(parts, ", ")
	}
	return canonical(a) == canonical(b)
}
