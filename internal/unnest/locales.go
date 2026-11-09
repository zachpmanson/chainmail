package unnest

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// These are structural tokens, not a claim that every language's dates parse.
// See docs/quoted-locales.md for the fixture-backed format matrix.
var headerKeyAlias = map[string]string{
	"from": "from", "von": "from", "de": "from", "da": "from", "van": "from",
	"fra": "from", "reply-to": "replyto",
	"sent": "date", "date": "date", "gesendet": "date", "enviado": "date",
	"envoyé": "date", "verzonden": "date", "inviato": "date", "sendt": "date",
	"skickat": "date",
	"to":      "to", "an": "to", "para": "to", "aan": "to", "till": "to", "a": "to",
	"subject": "subject", "betreff": "subject", "asunto": "subject",
	"objet": "subject", "oggetto": "subject", "ämne": "subject",
	"assunto": "subject", "onderwerp": "subject",
	"cc": "cc", "bcc": "cc", "message-id": "messageid",
}

var openers = []string{
	"on", "le", "el", "il", "em", "am", "op", "den", "på", "pe",
	"w dniu", "dnia", "vào", "在",
}

var closers = []string{
	"wrote:", "sent:", "a écrit :", "a écrit:", "écrit:", "escribió:",
	"ha scritto:", "scritto:", "escreveu:", "schrieb:", "schreef:",
	"napisał:", "napisała:", "pisze:", "skrev:", "kirjoitti:",
	"đã viết:", "写道：", "작성:",
}

func foldSpaces(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func structuralText(s string) string {
	return foldSpaces(norm.NFC.String(s))
}

// parseHeaderLine is the single label lookup for detection and extraction.
// Only the label is case/Unicode-normalised; values keep their spelling.
func parseHeaderLine(s string) (field, value string, ok bool) {
	label, value, found := splitHeaderLine(s)
	if !found {
		return "", "", false
	}
	field, ok = headerKeyAlias[strings.ToLower(structuralText(label))]
	return field, value, ok
}

// Unknown labels end a logical value rather than becoming recipient prose.
// Digits are not labels: a folded clock must remain a date continuation.
func splitHeaderLine(s string) (label, value string, ok bool) {
	label, value, found := strings.Cut(strings.TrimSpace(s), ":")
	if !found {
		return "", "", false
	}
	label = strings.TrimSpace(strings.Trim(strings.TrimSpace(label), "*"))
	if label == "" {
		return "", "", false
	}
	for _, r := range label {
		if !unicode.IsLetter(r) && !unicode.IsMark(r) && r != '-' {
			return "", "", false
		}
	}
	value = strings.TrimSpace(value)
	if strings.HasPrefix(strings.TrimSpace(s), "*") {
		value = strings.TrimSpace(strings.TrimPrefix(value, "*"))
	}
	return label, value, true
}

func stripOpener(s string) string {
	lower := strings.ToLower(s)
	for _, opener := range openers {
		if strings.HasPrefix(lower, opener+" ") {
			return strings.TrimLeft(s[len(opener):], " ,")
		}
	}
	return s
}
