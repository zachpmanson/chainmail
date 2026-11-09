package unnest

import (
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// DateParserVersion versions interpretation, not quote keys or stored records.
const DateParserVersion = 1

const (
	PrecisionUnknown     = "unknown"
	PrecisionMinute      = "minute"
	PrecisionSecond      = "second"
	DateParsed           = "parsed"
	DateMissing          = "missing_date"
	DateMissingClock     = "missing_clock"
	DateIncomplete       = "incomplete_date"
	DateAmbiguousNumeric = "ambiguous_numeric_date"
	DateUnsupported      = "unsupported_date_format"
	DateInvalid          = "invalid_date"
	DateInvalidClock     = "invalid_clock"
	DateInvalidZone      = "invalid_zone"
	DateContradictory    = "contradictory_date"
)

// DateResult describes a complete stated wall clock or why it is unavailable.
// TZ is the label as stated, never an inferred offset. Precision records whether
// seconds were stated; the zero seconds in a minute value are not extra evidence.
// Reasons contain no source text and are suitable for metadata-only inspection.
type DateResult struct {
	Sent      time.Time
	TZ        string
	Precision string
	Reason    string
}

func unknownDate(reason string) DateResult {
	return DateResult{Precision: PrecisionUnknown, Reason: reason}
}

var (
	// Capture the whole clock-looking token, not a valid substring of a bad one.
	reClockToken  = regexp.MustCompile(`[0-9]+:[^\s,]*`)
	reWholeClock  = regexp.MustCompile(`(?i)^([0-9]{1,2}):([0-9]{2})(?::([0-9]{2}))?(am|pm)?$`)
	reNumericDate = regexp.MustCompile(`^[0-9]{1,4}[/.-][0-9]{1,2}[/.-][0-9]{1,4}$`)
	reISODate     = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}$`)
	reOffset      = regexp.MustCompile(`^[+-]([0-9]{2}):?([0-9]{2})$`)
	reZoneLabel   = regexp.MustCompile(`^[A-Z]{2,5}$`)
)

// ParseDate parses a whole quoted Date/Sent value, not an RFC mailbox header.
// Trailing prose cannot silently turn a partial match into stated metadata.
func ParseDate(s string) DateResult {
	r, rest, _ := splitDate(s, true)
	if r.Reason == DateParsed && strings.Trim(rest, " ,") != "" {
		return unknownDate(DateUnsupported)
	}
	return r
}

// splitDate shares the clock pivot with attribution parsing. Its boolean only
// means that a clock-looking token was found, not that a date parsed.
func splitDate(s string, whole bool) (DateResult, string, bool) {
	s = foldSpaces(s)
	loc := reClockToken.FindStringIndex(s)
	if loc == nil {
		_, reason := parseCalendar(s)
		if reason == DateParsed {
			reason = DateMissingClock
		}
		return unknownDate(reason), s, false
	}
	rest := s[loc[1]:]
	clock := s[loc[0]:loc[1]]
	// A sign, letter, digit or punctuation glued before the clock is not a pivot.
	boundary := true
	if loc[0] > 0 {
		prev, _ := utf8.DecodeLastRuneInString(s[:loc[0]])
		boundary = unicode.IsSpace(prev) || prev == ','
	}
	m := reWholeClock.FindStringSubmatch(clock)
	if m == nil || !boundary || (len(rest) > 1 && rest[0] == ',' && rest[1] >= '0' && rest[1] <= '9') {
		return unknownDate(DateInvalidClock), rest, true
	}
	meridiem := strings.ToLower(m[4])
	if meridiem == "" {
		fields := strings.Fields(strings.TrimLeft(rest, " "))
		if len(fields) > 0 {
			candidate := strings.ToLower(strings.TrimRight(fields[0], ","))
			if candidate == "a.m." || candidate == "p.m." || candidate == "a.m" || candidate == "p.m" {
				return unknownDate(DateInvalidClock), rest[strings.Index(rest, fields[0])+len(fields[0]):], true
			}
			if candidate == "am" || candidate == "pm" {
				meridiem = candidate
				at := strings.Index(rest, fields[0])
				rest = rest[at+len(candidate):]
			}
		}
	}
	hour, _ := strconv.Atoi(m[1])
	minute, _ := strconv.Atoi(m[2])
	second, _ := strconv.Atoi(m[3])
	if minute > 59 || second > 59 || hour > 23 || (meridiem != "" && (hour < 1 || hour > 12)) {
		return unknownDate(DateInvalidClock), rest, true
	}
	if meridiem != "" {
		hour %= 12
		if meridiem == "pm" {
			hour += 12
		}
	}
	if fields := strings.Fields(rest); len(fields) > 0 && !strings.HasPrefix(rest, ",") {
		if token := strings.ToLower(strings.TrimRight(fields[0], ",")); token == "am" || token == "pm" || token == "a.m." || token == "p.m." {
			return unknownDate(DateInvalidClock), rest, true
		}
	}
	r := unknownDate(DateUnsupported)
	// After a comma, an attribution's token can be a person's name. A zone
	// there needs its own terminating comma; a whole header has no person.
	zoneRest := strings.TrimLeft(rest, " ,")
	if zoneRest != "" {
		token := strings.TrimRight(strings.Fields(zoneRest)[0], ",")
		tail := strings.TrimLeft(zoneRest[len(token):], " ")
		afterComma := strings.HasPrefix(strings.TrimLeft(rest, " "), ",")
		zonePosition := !strings.HasPrefix(tail, "<") && (!afterComma || whole || strings.HasPrefix(tail, ","))
		if zonePosition && (strings.HasPrefix(token, "+") || strings.HasPrefix(token, "-")) {
			z := reOffset.FindStringSubmatch(token)
			if z == nil {
				return unknownDate(DateInvalidZone), rest, true
			}
			zh, _ := strconv.Atoi(z[1])
			zm, _ := strconv.Atoi(z[2])
			if zh > 23 || zm > 59 {
				return unknownDate(DateInvalidZone), rest, true
			}
			r.TZ = token
		} else if zonePosition && reZoneLabel.MatchString(token) {
			r.TZ = token
		}
		if r.TZ != "" {
			rest = zoneRest[len(token):]
		}
	}
	d, reason := parseCalendar(s[:loc[0]])
	r.Reason = reason
	if reason == DateParsed {
		r.Sent = time.Date(d.Year(), d.Month(), d.Day(), hour, minute, second, 0, time.UTC)
		if r.Sent.IsZero() {
			return unknownDate(DateInvalid), rest, true
		}
		r.Precision = PrecisionMinute
		if m[3] != "" {
			r.Precision = PrecisionSecond
		}
	}
	return r, rest, true
}

// A grammar uses one lexicon at a time: translated words are never replaced
// throughout an attribution (which could rewrite a person's name).
type dateGrammar struct {
	name       string
	months     []string // January first; space-separated accepted spellings
	weekdays   []string // Sunday first
	joiners    []string
	filler     string
	monthFirst bool
}

var dateGrammars = []dateGrammar{
	{"en", []string{"jan january", "feb february", "mar march", "apr april", "may", "jun june", "jul july", "aug august", "sep sept september", "oct october", "nov november", "dec december"},
		[]string{"sun sunday", "mon monday", "tue tues tuesday", "wed wednesday", "thu thur thurs thursday", "fri friday", "sat saturday"}, []string{"at"}, "", true},
	{"it", []string{"gen gennaio", "feb febbraio", "mar marzo", "apr aprile", "mag maggio", "giu giugno", "lug luglio", "ago agosto", "set settembre", "ott ottobre", "nov novembre", "dic dicembre"},
		[]string{"dom domenica", "lun lunedì", "mar martedì", "mer mercoledì", "gio giovedì", "ven venerdì", "sab sabato"}, []string{"alle"}, "", false},
	{"fr", []string{"janv janvier", "févr février", "mars", "avr avril", "mai", "juin", "juil juillet", "août", "sept septembre", "oct octobre", "nov novembre", "déc décembre"},
		[]string{"dim dimanche", "lun lundi", "mar mardi", "mer mercredi", "jeu jeudi", "ven vendredi", "sam samedi"}, []string{"à"}, "", false},
	{"de", []string{"jan januar", "feb februar", "mär märz", "apr april", "mai", "jun juni", "jul juli", "aug august", "sep sept september", "okt oktober", "nov november", "dez dezember"},
		[]string{"so sonntag", "mo montag", "di dienstag", "mi mittwoch", "do donnerstag", "fr freitag", "sa samstag"}, []string{"um"}, "", false},
	{"es", []string{"ene enero", "feb febrero", "mar marzo", "abr abril", "may mayo", "jun junio", "jul julio", "ago agosto", "sep sept septiembre", "oct octubre", "nov noviembre", "dic diciembre"},
		[]string{"dom domingo", "lun lunes", "mar martes", "mié miércoles", "jue jueves", "vie viernes", "sáb sábado"}, []string{"a las"}, "de", false},
	{"pt", []string{"jan janeiro", "fev fevereiro", "mar março", "abr abril", "mai maio", "jun junho", "jul julho", "ago agosto", "set setembro", "out outubro", "nov novembro", "dez dezembro"},
		[]string{"dom domingo", "seg segunda-feira", "ter terça-feira", "qua quarta-feira", "qui quinta-feira", "sex sexta-feira", "sáb sábado"}, []string{"às", "as"}, "de", false},
	{"nl", []string{"jan januari", "feb februari", "mrt maart", "apr april", "mei", "jun juni", "jul juli", "aug augustus", "sep sept september", "okt oktober", "nov november", "dec december"},
		[]string{"zo zondag", "ma maandag", "di dinsdag", "wo woensdag", "do donderdag", "vr vrijdag", "za zaterdag"}, []string{"om"}, "", false},
	{"da", []string{"jan januar", "feb februar", "mar marts", "apr april", "maj", "jun juni", "jul juli", "aug august", "sep sept september", "okt oktober", "nov november", "dec december"},
		[]string{"søn søndag", "man mandag", "tir tirsdag", "ons onsdag", "tor torsdag", "fre fredag", "lør lørdag"}, []string{"kl"}, "", false},
	{"no", []string{"jan januar", "feb februar", "mar mars", "apr april", "mai", "jun juni", "jul juli", "aug august", "sep sept september", "okt oktober", "nov november", "des desember"},
		[]string{"søn søndag", "man mandag", "tir tirsdag", "ons onsdag", "tor torsdag", "fre fredag", "lør lørdag"}, []string{"kl"}, "", false},
	{"sv", []string{"jan januari", "feb februari", "mar mars", "apr april", "maj", "jun juni", "jul juli", "aug augusti", "sep sept september", "okt oktober", "nov november", "dec december"},
		[]string{"sön söndag", "mån måndag", "tis tisdag", "ons onsdag", "tor torsdag", "fre fredag", "lör lördag"}, []string{"kl"}, "", false},
	{"pl", []string{"sty stycznia", "lut lutego", "mar marca", "kwi kwietnia", "maj maja", "cze czerwca", "lip lipca", "sie sierpnia", "wrz września", "paź października", "lis listopada", "gru grudnia"},
		[]string{"niedz niedziela", "pon poniedziałek", "wt wtorek", "śr środa", "czw czwartek", "pt piątek", "sob sobota"}, []string{"o"}, "", false},
}

func tokenIndex(groups []string, token string) int {
	for i, group := range groups {
		for _, word := range strings.Fields(group) {
			if word == token {
				return i
			}
		}
	}
	return -1
}

func parseCalendar(s string) (time.Time, string) {
	s = strings.ToLower(strings.Trim(structuralText(s), " ,"))
	if s == "" {
		return time.Time{}, DateMissing
	}
	if reISODate.MatchString(s) {
		d, err := time.Parse("2006-01-02", s)
		if err != nil || d.Year() == 0 {
			return time.Time{}, DateInvalid
		}
		return d, DateParsed
	}
	if reNumericDate.MatchString(s) {
		return time.Time{}, DateAmbiguousNumeric
	}
	reason := DateUnsupported
	for _, g := range dateGrammars {
		tokens := strings.Fields(s)
		punctuationOK := true
		for i := range tokens {
			tokens[i] = strings.TrimSuffix(strings.TrimSuffix(tokens[i], ","), ".")
			if strings.HasSuffix(tokens[i], ".") || strings.HasSuffix(tokens[i], ",") {
				punctuationOK = false
			}
		}
		if !punctuationOK {
			continue
		}
		// Joiners can contain more than one word, but only at the clock seam.
		for _, joiner := range g.joiners {
			j := strings.Fields(joiner)
			if len(tokens) >= len(j) && strings.Join(tokens[len(tokens)-len(j):], " ") == joiner {
				tokens = tokens[:len(tokens)-len(j)]
				break
			}
		}
		weekday := -1
		if len(tokens) > 0 {
			weekday = tokenIndex(g.weekdays, tokens[0])
			if weekday >= 0 {
				tokens = tokens[1:]
			}
		}
		if len(tokens) == 1 && reNumericDate.MatchString(tokens[0]) && !reISODate.MatchString(tokens[0]) {
			return time.Time{}, DateAmbiguousNumeric
		}
		// Spanish/Portuguese explicit day de month de year grammar.
		if g.filler != "" && len(tokens) == 5 && tokens[1] == g.filler && tokens[3] == g.filler {
			tokens = []string{tokens[0], tokens[2], tokens[4]}
		}
		if len(tokens) == 0 {
			if weekday >= 0 {
				return time.Time{}, DateIncomplete
			}
			return time.Time{}, DateMissing
		}
		if len(tokens) < 2 {
			if weekday >= 0 || tokenIndex(g.months, tokens[0]) >= 0 {
				reason = DateIncomplete
			}
			continue
		}
		dayText, monthText := tokens[0], tokens[1]
		if g.monthFirst && tokenIndex(g.months, tokens[0]) >= 0 {
			monthText, dayText = tokens[0], tokens[1]
		}
		month := tokenIndex(g.months, monthText)
		if month < 0 {
			continue
		}
		if len(tokens) == 2 {
			reason = DateIncomplete
			continue
		}
		if len(tokens) != 3 {
			continue
		}
		day, e1 := strconv.Atoi(dayText)
		year, e2 := strconv.Atoi(tokens[2])
		if e1 != nil || e2 != nil {
			continue
		}
		if !decimalDigits(dayText) || !decimalDigits(tokens[2]) || len(dayText) > 2 {
			return time.Time{}, DateInvalid
		}
		if len(tokens[2]) != 4 {
			reason = DateIncomplete
			continue
		}
		d := time.Date(year, time.Month(month+1), day, 0, 0, 0, 0, time.UTC)
		if year < 1 || day < 1 || d.Month() != time.Month(month+1) || d.Day() != day {
			return time.Time{}, DateInvalid
		}
		if weekday >= 0 && int(d.Weekday()) != weekday {
			return time.Time{}, DateContradictory
		}
		return d, DateParsed
	}
	return time.Time{}, reason
}

func decimalDigits(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return s != ""
}
