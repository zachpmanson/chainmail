// Package hassles keeps reminder tasks in CalDAV, never in the corpus.
package hassles

import (
	"errors"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata"

	"github.com/emersion/go-ical"
)

const Zone = "Australia/Melbourne"

type Schedule struct {
	Title    string `json:"title"`
	Start    string `json:"start"` // Melbourne wall time, not browser-local time.
	TimeZone string `json:"timeZone"`
	Cadence  string `json:"cadence"`
	Interval int    `json:"interval"`
	Until    string `json:"until"`
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
	if s.Interval < 1 || s.Interval > 999 {
		return start, "", errors.New("interval must be 1–999")
	}
	freq := map[string]string{"daily": "DAILY", "weekdays": "WEEKLY", "weekly": "WEEKLY", "monthly": "MONTHLY", "yearly": "YEARLY"}[s.Cadence]
	if s.Cadence == "once" {
		if s.Until != "" || s.Interval != 1 {
			return start, "", errors.New("one-off reminders cannot have a repeat end or interval")
		}
		return start, "", nil
	}
	if freq == "" {
		return start, "", errors.New("unsupported cadence; hourly is unavailable pending Google compatibility testing")
	}
	if s.Cadence == "weekdays" && s.Interval != 1 {
		return start, "", errors.New("weekdays requires interval 1")
	}
	if s.Cadence == "weekdays" && (start.Weekday() == time.Saturday || start.Weekday() == time.Sunday) {
		return start, "", errors.New("choose a weekday for the first weekdays reminder")
	}
	rule := "FREQ=" + freq + ";INTERVAL=" + strconv.Itoa(s.Interval)
	if s.Cadence == "weekdays" {
		rule += ";BYDAY=MO,TU,WE,TH,FR"
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
	start, err := p.DateTime(nil)
	if err != nil || start.Second() != 0 {
		return nil, "Invalid imported start time or unsupported seconds"
	}
	title, _ := e.Props.Text("SUMMARY")
	s := &Schedule{Title: title, Start: start.Format("2006-01-02T15:04"), TimeZone: Zone, Cadence: "once", Interval: 1}
	if p := e.Props.Get("RRULE"); p != nil {
		if len(p.Params) > 0 {
			return nil, "Imported recurrence parameters: schedule editing unavailable"
		}
		seen := map[string]bool{}
		for _, part := range strings.Split(p.Value, ";") {
			kv := strings.SplitN(part, "=", 2)
			if len(kv) != 2 || seen[kv[0]] {
				return nil, "Complex imported recurrence: schedule editing unavailable"
			}
			seen[kv[0]] = true
			switch kv[0] {
			case "FREQ":
				s.Cadence = map[string]string{"DAILY": "daily", "WEEKLY": "weekly", "MONTHLY": "monthly", "YEARLY": "yearly"}[kv[1]]
			case "INTERVAL":
				s.Interval, _ = strconv.Atoi(kv[1])
			case "UNTIL":
				end, err := time.Parse("20060102T150405Z", kv[1])
				if err != nil || end.Second() != 0 {
					return nil, "Imported recurrence end is unsupported"
				}
				s.Until = end.In(start.Location()).Format("2006-01-02T15:04")
			case "BYDAY":
				if kv[1] != "MO,TU,WE,TH,FR" {
					return nil, "Complex imported recurrence: schedule editing unavailable"
				}
			default:
				return nil, "Complex imported recurrence: schedule editing unavailable"
			}
		}
		if !seen["FREQ"] || s.Cadence == "" || s.Cadence == "once" {
			return nil, "Unsupported imported recurrence"
		}
		if seen["BYDAY"] {
			if s.Cadence != "weekly" {
				return nil, "Complex imported recurrence: schedule editing unavailable"
			}
			s.Cadence = "weekdays"
		}
	}
	if _, _, err := s.Validate(); err != nil {
		return nil, "Unsupported imported schedule: " + err.Error()
	}
	return s, ""
}
