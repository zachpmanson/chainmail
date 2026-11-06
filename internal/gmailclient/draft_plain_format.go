package gmailclient

import "strings"

// Locate only positively recognized plain parameters in the raw header. MIME's
// decoded map alone cannot distinguish duplicate or RFC2231 parameter spellings.
// The returned span excludes quotes, so normalization retains their raw bytes.
func plainDraftFormat(h rawDraftHeaders, params map[string]string) (format string, value draftSpan, ok bool) {
	format = strings.ToLower(params["format"])
	if len(params) != 2 || !strings.EqualFold(params["charset"], "utf-8") || format != "flowed" && format != "fixed" {
		return "", value, false
	}
	offset := 0
	for _, block := range h.blocks {
		if blockName(block) != "content-type" {
			offset += len(block)
			continue
		}
		pos := strings.IndexByte(block, ';')
		if pos < 0 {
			return "", value, false
		}
		seen := map[string]bool{}
		skipSpace := func() {
			for pos < len(block) && strings.ContainsRune(" \t\r\n", rune(block[pos])) {
				pos++
			}
		}
		for pos < len(block) {
			if block[pos] != ';' {
				return "", value, false
			}
			pos++
			skipSpace()
			start := pos
			for pos < len(block) && draftMIMETokenByte(block[pos]) {
				pos++
			}
			key := strings.ToLower(block[start:pos])
			// Do not search inside unknown/extended/continued parameter values.
			if key != "charset" && key != "format" || seen[key] {
				return "", value, false
			}
			seen[key] = true
			skipSpace()
			if pos >= len(block) || block[pos] != '=' {
				return "", value, false
			}
			pos++
			skipSpace()
			if pos >= len(block) {
				return "", value, false
			}
			var span draftSpan
			if block[pos] == '"' {
				pos++
				span.start = pos
				for pos < len(block) && block[pos] != '"' {
					if block[pos] == '\\' {
						pos++
					}
					if pos >= len(block) || block[pos] == '\r' || block[pos] == '\n' {
						return "", value, false
					}
					pos++
				}
				if pos >= len(block) {
					return "", value, false
				}
				span.end = pos
				pos++
			} else {
				span.start = pos
				for pos < len(block) && draftMIMETokenByte(block[pos]) {
					pos++
				}
				span.end = pos
			}
			if span.start == span.end {
				return "", value, false
			}
			if key == "format" {
				value = draftSpan{offset + span.start, offset + span.end}
			}
			skipSpace()
		}
		return format, value, seen["charset"] && seen["format"]
	}
	return "", value, false
}

func draftMIMETokenByte(b byte) bool {
	return b > 32 && b < 127 && !strings.ContainsRune("()<>@,;:\\\"/[]?=", rune(b))
}

// Without space-stuffing, quoting, flowed soft breaks or signature separators,
// default-DelSp flowed semantics are exactly the stored decoded text.
func inertDraftFlowed(text string) bool {
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, " ") || strings.HasPrefix(line, ">") || strings.HasSuffix(line, " ") {
			return false
		}
	}
	return true
}
