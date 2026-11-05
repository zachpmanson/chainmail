package hassles

import (
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/emersion/go-ical"
	"github.com/teambition/rrule-go"
)

// Timing is read-only: recognizing an imported recurrence never grants edit safety.
type Timing struct {
	State      string     `json:"state"` // upcoming, past, or unknown (kept in Upcoming).
	Next       *time.Time `json:"next"`
	Last       *time.Time `json:"last"`
	Note       string     `json:"note"`
	NextAllDay bool       `json:"nextAllDay,omitempty"`
	LastAllDay bool       `json:"lastAllDay,omitempty"`
}

func unknownTiming() Timing {
	return Timing{State: "unknown", Note: "Next/last reminder unavailable: unsupported or incomplete imported schedule; view in calendar"}
}

func reminderTime(p *ical.Prop) (time.Time, error) {
	if p == nil || len(p.Params["VALUE"]) > 1 || len(p.Params["TZID"]) > 1 || (p.Params.Get("VALUE") != "" && p.Params.Get("VALUE") != "DATE-TIME") || (len(p.Value) != 15 && len(p.Value) != 16) || (len(p.Value) == 15 && p.Params.Get("TZID") == "") {
		return time.Time{}, errors.New("undated, all-day or floating reminder")
	}
	// IANA TZIDs are resolved independently of embedded VTIMEZONE edit restrictions.
	return p.DateTime(nil)
}

// DATE values are civil Melbourne days, not notification instants.
func reminderDate(p *ical.Prop) (time.Time, error) {
	if p == nil || len(p.Params) != 1 || len(p.Params["VALUE"]) != 1 || p.Params.Get("VALUE") != "DATE" {
		return time.Time{}, errors.New("ambiguous DATE property")
	}
	loc, _ := time.LoadLocation(Zone)
	t, err := time.ParseInLocation("20060102", p.Value, loc)
	if err != nil || t.Format("20060102") != p.Value || t.Year() < 1970 || t.Year() > 2100 {
		return time.Time{}, errors.New("invalid or out of bounds DATE")
	}
	return t, nil
}

func recurrenceDates(props []ical.Prop) ([]time.Time, error) {
	return typedRecurrenceDates(props, false)
}

func typedRecurrenceDates(props []ical.Prop, allDay bool) ([]time.Time, error) {
	var out []time.Time
	for _, p := range props {
		for _, value := range strings.Split(p.Value, ",") {
			copy := p
			copy.Value = value
			t, err := reminderTime(&copy)
			if allDay {
				t, err = reminderDate(&copy)
			}
			if err != nil || len(out) >= 10000 {
				return nil, errors.New("unsupported recurrence dates")
			}
			out = append(out, t)
		}
	}
	return out, nil
}

func readRule(p *ical.Prop, start time.Time) (*rrule.RRule, bool) {
	if len(p.Params) != 0 || len(p.Value) > 1024 {
		return nil, false
	}
	seen := map[string]bool{}
	for _, part := range strings.Split(p.Value, ";") {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 || seen[kv[0]] {
			return nil, false
		}
		seen[kv[0]] = true
		switch kv[0] {
		case "FREQ":
			if kv[1] != "DAILY" && kv[1] != "WEEKLY" && kv[1] != "MONTHLY" && kv[1] != "YEARLY" {
				return nil, false
			}
		case "INTERVAL", "COUNT", "UNTIL", "WKST", "BYDAY":
		default:
			// High-frequency expansions and exotic rules stay visible, but undated.
			return nil, false
		}
	}
	// Iterator's next() can scan to year 9999 before yielding for impossible
	// filters. Only weekly bare weekdays have a bounded nonempty filter here.
	if seen["BYDAY"] {
		if !strings.Contains(";"+p.Value+";", ";FREQ=WEEKLY;") {
			return nil, false
		}
		for _, part := range strings.Split(p.Value, ";") {
			if !strings.HasPrefix(part, "BYDAY=") {
				continue
			}
			days := strings.Split(strings.TrimPrefix(part, "BYDAY="), ",")
			if len(days) > 7 {
				return nil, false
			}
			for _, day := range days {
				switch day {
				case "MO", "TU", "WE", "TH", "FR", "SA", "SU":
				default:
					return nil, false
				}
			}
		}
	}
	opt, err := rrule.StrToROptionInLocation(p.Value, start.Location())
	if err != nil || (seen["INTERVAL"] && (opt.Interval < 1 || opt.Interval > 999)) || (seen["COUNT"] && opt.Count < 1) || (seen["COUNT"] && seen["UNTIL"]) {
		return nil, false
	}
	opt.Dtstart = start
	if opt.Count > 0 {
		// The library otherwise silently ends COUNT rules after about 292 years.
		opt.Until = time.Date(9999, 12, 31, 23, 59, 59, 0, time.UTC)
	}
	r, err := rrule.NewRRule(*opt)
	return r, err == nil
}

func readDateRule(p *ical.Prop, start time.Time) (*rrule.RRule, bool) {
	if len(p.Params) != 0 || len(p.Value) > 1024 {
		return nil, false
	}
	copy := *p
	parts := strings.Split(p.Value, ";")
	out := make([]string, 0, len(parts))
	monthDaySeen := false
	for _, part := range parts {
		if strings.HasPrefix(part, "BYMONTHDAY=") {
			// Google's explicit matching monthly day is equivalent to the
			// already-supported implicit DTSTART day. Do not broaden filters.
			if monthDaySeen || !strings.Contains(";"+p.Value+";", ";FREQ=MONTHLY;") || part != "BYMONTHDAY="+strconv.Itoa(start.Day()) {
				return nil, false
			}
			monthDaySeen = true
			continue
		}
		if strings.HasPrefix(part, "UNTIL=") {
			end, err := reminderDate(&ical.Prop{Params: ical.Params{"VALUE": {"DATE"}}, Value: strings.TrimPrefix(part, "UNTIL=")})
			if err != nil || end.Before(start) {
				return nil, false
			}
			// The library accepts local DATE-TIME UNTIL; midnight is inclusive.
			part = "UNTIL=" + end.Format("20060102T150405")
		}
		out = append(out, part)
	}
	copy.Value = strings.Join(out, ";")
	return readRule(&copy, start)
}

func resourceTiming(events []ical.Event, now time.Time) Timing {
	// Detached exceptions can replace/cancel future instances. Never infer an end
	// from the master alone, even when an exception itself is cancelled.
	if len(events) != 1 || len(events[0].Props["RECURRENCE-ID"]) != 0 {
		return unknownTiming()
	}
	e := events[0]
	if len(e.Props["DTSTART"]) != 1 || len(e.Props["RRULE"]) > 1 || len(e.Props["EXRULE"]) != 0 {
		return unknownTiming()
	}
	p := e.Props.Get("DTSTART")
	allDay := p.Params.Get("VALUE") == "DATE"
	start, err := reminderTime(p)
	if allDay {
		start, err = reminderDate(p)
		// Only the RFC single-day default or an explicit exclusive next-day end.
		if len(e.Props["DTEND"]) > 1 || len(e.Props["DURATION"]) != 0 {
			return unknownTiming()
		}
		if endProp := e.Props.Get("DTEND"); endProp != nil {
			end, endErr := reminderDate(endProp)
			if endErr != nil || !end.Equal(start.AddDate(0, 0, 1)) {
				return unknownTiming()
			}
		}
		local := now.In(start.Location())
		now = time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, start.Location())
	}
	if err != nil || start.Year() < 1970 || start.Year() > 2100 {
		return unknownTiming()
	}
	rdates, err := typedRecurrenceDates(e.Props["RDATE"], allDay)
	if err != nil {
		return unknownTiming()
	}
	exdates, err := typedRecurrenceDates(e.Props["EXDATE"], allDay)
	if err != nil {
		return unknownTiming()
	}
	excluded := map[int64]bool{}
	for _, date := range exdates {
		excluded[date.Unix()] = true
	}
	out := Timing{State: "past"}
	add := func(date time.Time) {
		if excluded[date.Unix()] {
			return
		}
		if date.Before(now) {
			if out.Last == nil || date.After(*out.Last) {
				d := date.UTC()
				out.Last = &d
				out.LastAllDay = allDay
			}
		} else if out.Next == nil || date.Before(*out.Next) {
			d := date.UTC()
			out.Next = &d
			out.NextAllDay = allDay
		}
	}
	add(start)
	for _, date := range rdates {
		add(date)
	}
	if p := e.Props.Get("RRULE"); p != nil {
		reader := readRule
		if allDay {
			reader = readDateRule
		}
		r, ok := reader(p, start)
		if !ok {
			return unknownTiming()
		}
		next := r.Iterator()
		finished := false
		for i := 0; i < 10000; i++ {
			date, ok := next()
			if !ok {
				// Reaching a library horizon is not proof that a series has ended.
				if r.OrigOptions.Count > 0 {
					finished = i >= r.OrigOptions.Count
				} else {
					finished = !r.OrigOptions.Until.IsZero()
				}
				break
			}
			add(date)
			if !date.Before(now) && !excluded[date.Unix()] {
				finished = true
				break
			}
		}
		if !finished {
			return unknownTiming()
		}
	}
	if out.Next != nil {
		out.State = "upcoming"
	}
	if out.Next == nil && out.Last == nil {
		return unknownTiming()
	}
	return out
}

func mergeTiming(a, b Timing) Timing {
	out := a
	if b.Next != nil && (out.Next == nil || b.Next.Before(*out.Next)) {
		out.Next = b.Next
		out.NextAllDay = b.NextAllDay
	}
	if b.Last != nil && (out.Last == nil || b.Last.After(*out.Last)) {
		out.Last = b.Last
		out.LastAllDay = b.LastAllDay
	}
	if b.Note != "" {
		out.Note = b.Note
	}
	switch {
	case out.Next != nil:
		out.State = "upcoming"
	case a.State == "unknown" || b.State == "unknown":
		out.State = "unknown"
	case out.Last != nil:
		out.State = "past"
	default:
		out.State = "unknown"
	}
	return out
}

func timingReminder(t Timing) string {
	loc, _ := time.LoadLocation(Zone)
	if t.Next != nil {
		prefix := "Next · "
		if t.Note != "" {
			prefix = "Known next · "
		}
		if t.NextAllDay {
			return prefix + t.Next.In(loc).Format("2 Jan 2006") + " · All day"
		}
		return prefix + t.Next.In(loc).Format("2 Jan 2006 15:04 MST")
	}
	if t.State == "past" && t.Last != nil {
		if t.LastAllDay {
			return "Last · " + t.Last.In(loc).Format("2 Jan 2006") + " · All day"
		}
		return "Last · " + t.Last.In(loc).Format("2 Jan 2006 15:04 MST")
	}
	return "Reminder date unavailable · view in calendar"
}
