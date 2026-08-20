package unnest

import (
	"regexp"
	"strings"
	"time"
)

// HeaderBlock is the sender, time and routing recovered from a quoted header
// block — the Outlook forward form, and the block that follows a forward rule.
//
// Unlike an attribution, this form carries recipients. That matters: a recovered
// message whose To/Cc are known can credit the people who only ever appear as
// recipients, who are otherwise invisible in the corpus.
type HeaderBlock struct {
	Sender  string
	Address string
	Sent    time.Time
	TZ      string
	Subject string
	To      string
	Cc      string
	// MessageID if the quoting client wrote one. Almost never present in
	// practice, but when it is it is a far better dedup key than any hash.
	MessageID string
}

// Canonical key names, including the localisations seen in this corpus. The
// value is the field it fills; several source keys map to one field because
// Outlook writes "Sent:" where Gmail writes "Date:" for the same fact.
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
	"cc": "cc", "bcc": "cc",
	"message-id": "messageid",
}

var reKeyValue = regexp.MustCompile(`^\*?([A-Za-z\-éÄäÖöÜüÅåÆæØø]{2,12})\*?\s*:\s*(.*)$`)

// ParseHeaderBlock reads a quoted header block. Lines it does not recognise are
// ignored rather than guessed at, so a meeting-invite block that slipped through
// detection yields an empty result instead of a fabricated sender.
func ParseHeaderBlock(sentinel string) HeaderBlock {
	var h HeaderBlock
	// last names the field a folded continuation line belongs to.
	var last string
	for _, raw := range strings.Split(sentinel, "\n") {
		line := unbold(strings.TrimSpace(raw))
		m := reKeyValue.FindStringSubmatch(line)
		if m == nil {
			// A folded continuation of the previous key. Appending rather than
			// dropping it is what keeps a wrapped recipient list complete — the
			// people past the wrap are otherwise lost from the corpus entirely.
			appendTo(&h, last, line)
			continue
		}
		field, ok := headerKeyAlias[strings.ToLower(m[1])]
		if !ok {
			last = ""
			continue
		}
		last = field
		v := strings.TrimSpace(m[2])
		if v == "" {
			continue
		}
		switch field {
		case "from":
			// A From: value is "Name <addr>", the same shape an attribution ends
			// with, so it is parsed by the same code.
			h.Sender, h.Address = parsePerson(v)
		case "date":
			if t, tz, _, ok := SplitWhen(v); ok {
				h.Sent, h.TZ = t, tz
			}
		case "subject":
			h.Subject = v
		case "to":
			h.To = v
		case "cc":
			h.Cc = appendHeader(h.Cc, v)
		case "messageid":
			h.MessageID = strings.Trim(v, "<>")
		}
	}
	return h
}

// appendHeader joins repeated Cc/Bcc lines rather than letting the last win:
// Bcc maps onto cc, and a block carrying both would otherwise silently drop one.
func appendHeader(existing, v string) string {
	if existing == "" {
		return v
	}
	return existing + ", " + v
}

// appendTo continues a folded value. Only the fields that realistically wrap are
// handled: a Date: or a From: fits on one line, while recipient lists routinely
// do not.
func appendTo(h *HeaderBlock, field, text string) {
	if field == "" || text == "" {
		return
	}
	switch field {
	case "to":
		h.To = appendHeader(h.To, text)
	case "cc":
		h.Cc = appendHeader(h.Cc, text)
	case "subject":
		h.Subject = strings.TrimSpace(h.Subject + " " + text)
	}
}
