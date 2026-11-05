// Package hassles keeps reminder tasks in CalDAV, never in the corpus.
package hassles

import (
	"errors"
	"math"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata"

	"github.com/emersion/go-ical"
)

const Zone = "Australia/Melbourne"

type Schedule struct {
	Title     string  `json:"title"`
	Start     string  `json:"start"` // Melbourne wall time, not browser-local time.
	TimeZone  string  `json:"timeZone"`
	Cadence   string  `json:"cadence"`
	Interval  float64 `json:"interval"`
	Until     string  `json:"until"`
	WorkHours bool    `json:"workHours"`
}

func WallTime(value string) (time.Time, error) {
	loc, _ := time.LoadLocation(Zone)
	t, err := time.ParseInLocation("2006-01-02T15:04", value, loc)
	if err != nil || t.Format("2006-01-02T15:04") != value {
		return time.Time{}, errors.New("invalid Melbourne wall time (including a daylight-saving gap)")
	}
	// A repeated clock time is ambiguous without an offset: refuse rather than guess.
	for _, delta := range []time.Duration{-time.Hour, time.Hour} {
		if t.Add(delta).Format("2006-01-02T15:04") == value {
			return time.Time{}, errors.New("ambiguous daylight-saving wall time; choose another time")
		}
	}
	if t.Year() < 1970 || t.Year() > 2100 {
		return time.Time{}, errors.New("date must be between 1970 and 2100")
	}
	return t, nil
}

// Check the raw wall clock before DateTime's DST normalization can hide a gap.
func melbourneStart(p *ical.Prop) (time.Time, error) {
	parsed, err := reminderTime(p)
	if err != nil || p.Params.Get("TZID") != Zone || len(p.Value) != 15 {
		return time.Time{}, errors.New("invalid Melbourne start property")
	}
	raw, err := time.Parse("20060102T150405", p.Value)
	if err != nil {
		return time.Time{}, err
	}
	start, err := WallTime(raw.Format("2006-01-02T15:04"))
	if err != nil {
		return time.Time{}, err
	}
	start = start.Add(time.Duration(raw.Second()) * time.Second)
	if !start.Equal(parsed) || start.Format("20060102T150405") != p.Value {
		return time.Time{}, errors.New("Melbourne start does not round-trip")
	}
	return start, nil
}

func (s Schedule) Validate() (time.Time, string, error) {
	start, err := WallTime(s.Start)
	if err != nil {
		return start, "", err
	}
	if s.TimeZone != Zone {
		return start, "", errors.New("timezone must be Australia/Melbourne")
	}
	if strings.TrimSpace(s.Title) == "" || len(s.Title) > 500 || strings.ContainsAny(s.Title, "\r\n") {
		return start, "", errors.New("title is required (maximum 500 bytes, one line)")
	}
	if s.Cadence == "hourly" {
		if !supportedHourlyInterval(s.Interval) {
			return start, "", errors.New("hourly interval must be 0.5, 1, 2, 3, 4, 6, 8, 12 or 24 hours")
		}
	} else if math.IsNaN(s.Interval) || math.IsInf(s.Interval, 0) || s.Interval < 1 || s.Interval > 999 || math.Trunc(s.Interval) != s.Interval {
		return start, "", errors.New("interval must be a whole number from 1–999")
	}
	if s.WorkHours && !withinWorkHours(start) {
		return start, "", errors.New("choose a first reminder on a weekday, 09:00–17:00 Melbourne (before 17:00)")
	}
	freq := map[string]string{"hourly": "DAILY", "daily": "DAILY", "weekdays": "WEEKLY", "weekly": "WEEKLY", "monthly": "MONTHLY", "yearly": "YEARLY"}[s.Cadence]
	if s.Cadence == "once" {
		if s.Until != "" || s.Interval != 1 {
			return start, "", errors.New("one-off reminders cannot have a repeat end or interval")
		}
		return start, "", nil
	}
	if freq == "" {
		return start, "", errors.New("unsupported cadence")
	}
	if s.Cadence == "weekdays" && s.Interval != 1 {
		return start, "", errors.New("weekdays requires interval 1")
	}
	if s.Cadence == "weekdays" && (start.Weekday() == time.Saturday || start.Weekday() == time.Sunday) {
		return start, "", errors.New("choose a weekday for the first weekdays reminder")
	}
	ruleInterval := int(s.Interval)
	if s.Cadence == "hourly" {
		ruleInterval = 1 // DAILY INTERVAL is days, never hours.
	}
	rule := "FREQ=" + freq + ";INTERVAL=" + strconv.Itoa(ruleInterval)
	if s.Cadence == "hourly" {
		hours, minutes := hourlySlots(s, start)
		rule += ";BYHOUR=" + hours + ";BYMINUTE=" + minutes
	}
	if s.Cadence == "weekdays" || (s.WorkHours && s.Cadence != "weekly") {
		rule += ";BYDAY=" + workDays
	}
	if s.WorkHours {
		switch s.Cadence {
		case "monthly":
			rule += ";BYMONTHDAY=" + strconv.Itoa(start.Day())
		case "yearly":
			rule += ";BYMONTH=" + strconv.Itoa(int(start.Month())) + ";BYMONTHDAY=" + strconv.Itoa(start.Day())
		}
	}
	if s.Until != "" {
		end, e := WallTime(s.Until)
		if e != nil {
			return start, "", e
		}
		if !end.After(start) {
			return start, "", errors.New("repeat end must be after the first reminder")
		}
		rule += ";UNTIL=" + end.UTC().Format("20060102T150405Z")
	}
	return start, rule, nil
}

func readSchedule(e ical.Event) (*Schedule, string) {
	if len(e.Props["EXDATE"]) > 0 && validCompletionDates(e) {
		// Validate the base without changing the original exclusion identities.
		e = ical.Event{Component: cloneComponent(e.Component)}
		e.Props.Del("EXDATE")
	}

	if len(e.Props["SUMMARY"]) > 1 || len(e.Props["DTEND"]) > 1 || len(e.Props["DURATION"]) > 1 || (len(e.Props["DTEND"])+len(e.Props["DURATION"]) > 1) || len(e.Props["DTSTART"]) != 1 || len(e.Props["RRULE"]) > 1 || len(e.Props["EXDATE"])+len(e.Props["RDATE"])+len(e.Props["RECURRENCE-ID"]) > 0 {
		return nil, "Imported recurrence/exception data: schedule editing unavailable"
	}
	end, endErr := e.DateTimeEnd(nil)
	begin, beginErr := e.DateTimeStart(nil)
	if endErr != nil || beginErr != nil || end.Sub(begin) != 5*time.Minute {
		return nil, "Imported event duration: schedule editing unavailable"
	}
	p := e.Props.Get("DTSTART")
	if p.Params.Get("TZID") != Zone || len(p.Value) != 15 {
		return nil, "Imported date/time: schedule editing unavailable"
	}
	start, err := melbourneStart(p)
	if err != nil || start.Second() != 0 {
		return nil, "Invalid imported start time or unsupported seconds"
	}
	title, _ := e.Props.Text("SUMMARY")
	s := &Schedule{Title: title, Start: start.Format("2006-01-02T15:04"), TimeZone: Zone, Cadence: "once", Interval: 1}
	if !readScheduleMetadata(e, s) {
		return nil, "Ambiguous reminder metadata: schedule editing unavailable"
	}
	if p := e.Props.Get("RRULE"); p != nil {
		return readScheduleRule(s, p)
	}
	if s.Cadence == "hourly" {
		return nil, "Hourly metadata requires a matching recurrence"
	}
	if _, _, err := s.Validate(); err != nil {
		return nil, "Unsupported imported schedule: " + err.Error()
	}
	return s, ""
}

func readScheduleRule(s *Schedule, p *ical.Prop) (*Schedule, string) {
	if len(p.Params) > 0 || len(p.Value) > 1024 {
		return nil, "Imported recurrence parameters: schedule editing unavailable"
	}
	hourly := s.Cadence == "hourly"
	seen := map[string]bool{}
	for _, part := range strings.Split(p.Value, ";") {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 || seen[kv[0]] {
			return nil, "Complex imported recurrence: schedule editing unavailable"
		}
		seen[kv[0]] = true
		switch kv[0] {
		case "FREQ":
			if hourly {
				if kv[1] != "DAILY" {
					return nil, "Hourly metadata requires DAILY slots"
				}
			} else {
				s.Cadence = map[string]string{"DAILY": "daily", "WEEKLY": "weekly", "MONTHLY": "monthly", "YEARLY": "yearly"}[kv[1]]
			}
		case "INTERVAL":
			if hourly {
				if kv[1] != "1" {
					return nil, "Hourly DAILY interval must be 1"
				}
			} else {
				interval, err := strconv.Atoi(kv[1])
				if err != nil {
					return nil, "Invalid recurrence interval"
				}
				s.Interval = float64(interval)
			}
		case "UNTIL":
			end, err := time.Parse("20060102T150405Z", kv[1])
			if err != nil || end.Second() != 0 {
				return nil, "Imported recurrence end is unsupported"
			}
			loc, _ := time.LoadLocation(Zone)
			s.Until = end.In(loc).Format("2006-01-02T15:04")
		case "BYDAY":
			if kv[1] != workDays {
				return nil, "Complex imported recurrence: schedule editing unavailable"
			}
		case "BYHOUR", "BYMINUTE":
			if !hourly {
				return nil, "Hourly slots require supported metadata"
			}
		case "BYMONTHDAY":
			start, err := WallTime(s.Start)
			if err != nil || kv[1] != strconv.Itoa(start.Day()) {
				return nil, "Unsupported month-day filter"
			}
		case "BYMONTH":
			start, err := WallTime(s.Start)
			if err != nil || kv[1] != strconv.Itoa(int(start.Month())) {
				return nil, "Unsupported month filter"
			}
		default:
			return nil, "Complex imported recurrence: schedule editing unavailable"
		}
	}
	if !seen["FREQ"] || s.Cadence == "" || s.Cadence == "once" {
		return nil, "Unsupported imported recurrence"
	}
	if seen["BYDAY"] && s.Cadence == "weekly" {
		s.Cadence = "weekdays"
	} else if seen["BYDAY"] {
		s.WorkHours = true
	}
	_, expected, err := s.Validate()
	if err != nil {
		return nil, "Unsupported imported schedule: " + err.Error()
	}
	// Compare fields, not their order. No extra filters may alter the profile.
	want := map[string]string{}
	for _, part := range strings.Split(expected, ";") {
		kv := strings.SplitN(part, "=", 2)
		want[kv[0]] = kv[1]
	}
	for _, part := range strings.Split(p.Value, ";") {
		kv := strings.SplitN(part, "=", 2)
		if want[kv[0]] != kv[1] {
			return nil, "Conflicting imported recurrence filters"
		}
		delete(want, kv[0])
	}
	if !seen["INTERVAL"] {
		delete(want, "INTERVAL")
	}
	if len(want) != 0 {
		return nil, "Incomplete imported work-hours recurrence"
	}
	return s, ""
}
