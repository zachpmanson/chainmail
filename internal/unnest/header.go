package unnest

import (
	"strings"
	"time"
)

// HeaderBlock is the sender, time and routing recovered from a quoted header
// block. Unlike an attribution, it can carry recipients and a subject.
type HeaderBlock struct {
	Sender        string
	Address       string
	Sent          time.Time
	TZ            string
	DatePrecision string
	DateReason    string
	Subject       string
	To            string
	Cc            string
	MessageID     string
}

// ParseHeaderBlock reads logical values before parsing dates, so a folded date
// is validated as a whole. Detection bounds the folds; unknown labels never fill
// a field. Conflicting Date/Sent evidence fails closed independently of sender.
func ParseHeaderBlock(sentinel string) HeaderBlock {
	h := HeaderBlock{DatePrecision: PrecisionUnknown, DateReason: DateMissing}
	var field, value string
	var dates []DateResult
	var dateValues []string
	flush := func() {
		v := strings.TrimSpace(value)
		switch field {
		case "from":
			h.Sender, h.Address = parsePerson(v)
		case "date":
			dates = append(dates, ParseDate(v))
			dateValues = append(dateValues, structuralText(v))
		case "subject":
			h.Subject = v
		case "to":
			h.To = appendHeader(h.To, v)
		case "cc":
			h.Cc = appendHeader(h.Cc, v)
		case "messageid":
			h.MessageID = strings.Trim(v, "<>")
		}
	}
	for _, raw := range strings.Split(sentinel, "\n") {
		line := strings.TrimSpace(raw)
		if f, v, ok := parseHeaderLine(line); ok {
			flush()
			field, value = f, v
			continue
		}
		if _, _, label := splitHeaderLine(line); label || line == "" {
			flush()
			field, value = "", ""
			continue
		}
		if field != "" && line != "" {
			// Sentinel continuations have already passed the detector's fold guards.
			// A comma is appropriate for recipient lists, a space for other values.
			if field == "to" || field == "cc" {
				value = appendHeader(value, line)
			} else {
				value = strings.TrimSpace(value + " " + line)
			}
		}
	}
	flush()
	if len(dates) > 0 {
		d := dates[0]
		for i, other := range dates[1:] {
			if !d.Sent.Equal(other.Sent) || d.TZ != other.TZ || d.Reason != other.Reason ||
				(d.Reason != DateParsed && dateValues[0] != dateValues[i+1]) {
				d = unknownDate(DateContradictory)
				break
			}
			if other.Precision == PrecisionSecond {
				d.Precision = PrecisionSecond
			}
		}
		h.Sent, h.TZ, h.DatePrecision, h.DateReason = d.Sent, d.TZ, d.Precision, d.Reason
	}
	return h
}

// Repeated recipients, including Bcc (mapped to cc), form a union.
func appendHeader(existing, v string) string {
	if existing == "" {
		return v
	}
	if v == "" {
		return existing
	}
	return existing + ", " + v
}
