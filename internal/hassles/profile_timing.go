package hassles

import (
	"time"

	"github.com/emersion/go-ical"
)

const workDays = "MO,TU,WE,TH,FR"
const workHourList = "9,10,11,12,13,14,15,16"
const workHoursMarker = "[chainmail:work-hours:v1]"

func withinWorkHours(t time.Time) bool {
	return t.Weekday() != time.Saturday && t.Weekday() != time.Sunday && t.Hour() >= 9 && t.Hour() < 17
}

// Only the narrow, uncounted profiles we emit use this evaluator. In particular,
// never pass hourly BYHOUR filters to rrule-go: unreachable residues can hang
// inside a single iterator call. Generic imports retain their existing guards.
func timingProfile(e ical.Event, p *ical.Prop, start time.Time) *Schedule {
	checked, err := melbourneStart(e.Props.Get("DTSTART"))
	if err != nil || !checked.Equal(start) || start.Location().String() != Zone || start.Second() != 0 {
		return nil
	}
	s := &Schedule{Title: "Reminder", Start: start.Format("2006-01-02T15:04"), TimeZone: Zone, Cadence: "once", Interval: 1}
	if !readScheduleMetadata(e, s) {
		return nil
	}
	s, _ = readScheduleRule(s, p)
	if s == nil || (s.Cadence != "hourly" && !s.WorkHours) {
		return nil
	}
	return s
}

func civil(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), 0, time.UTC)
}

// profileCandidate uses a UTC civil clock for phase arithmetic, not elapsed time.
// Invalid month dates and DST gaps are skipped, without shifting later phases.
func profileCandidate(s Schedule, start time.Time, n int64) (time.Time, time.Time, bool) {
	base := civil(start)
	var c time.Time
	switch s.Cadence {
	case "hourly":
		c = time.Unix(base.Unix()+n*int64(s.Interval*60)*60, 0).UTC()
	case "daily", "weekdays", "weekly":
		days := int64(s.Interval)
		if s.Cadence == "weekly" {
			days *= 7
		}
		c = time.Unix(base.Unix()+n*days*86400, 0).UTC()
	case "monthly":
		c = time.Date(start.Year(), start.Month()+time.Month(n*int64(s.Interval)), start.Day(), start.Hour(), start.Minute(), 0, 0, time.UTC)
	case "yearly":
		c = time.Date(start.Year()+int(n)*int(s.Interval), start.Month(), start.Day(), start.Hour(), start.Minute(), 0, 0, time.UTC)
	}
	t := time.Date(c.Year(), c.Month(), c.Day(), c.Hour(), c.Minute(), c.Second(), 0, start.Location())
	// RFC 5545 local date-times use the first occurrence of a repeated clock.
	if earlier := t.Add(-time.Hour); civil(earlier).Equal(c) {
		t = earlier
	}
	valid := civil(t).Equal(c)
	if s.Cadence == "monthly" || s.Cadence == "yearly" {
		valid = valid && c.Day() == start.Day()
	}
	if s.Cadence == "yearly" {
		valid = valid && c.Month() == start.Month()
	}
	if s.Cadence == "weekdays" {
		valid = valid && t.Weekday() != time.Saturday && t.Weekday() != time.Sunday
	}
	if s.WorkHours {
		valid = valid && withinWorkHours(t)
	}
	return t, c, valid
}

func profileIndex(s Schedule, start, now time.Time) int64 {
	delta := civil(now).Unix() - civil(start).Unix()
	var n int64
	switch s.Cadence {
	case "hourly":
		n = delta / (int64(s.Interval*60) * 60)
	case "daily", "weekdays":
		n = delta / (int64(s.Interval) * 86400)
	case "weekly":
		n = delta / (int64(s.Interval) * 7 * 86400)
	case "monthly":
		n = int64((now.Year()-start.Year())*12+int(now.Month()-start.Month())) / int64(s.Interval)
	case "yearly":
		n = int64(now.Year()-start.Year()) / int64(s.Interval)
	}
	if n < 0 {
		return 0
	}
	return n
}

// Each direction has a finite candidate budget, including filtered/invalid dates.
// Seeking near now avoids turning old hourly schedules into unknown after 10k
// historical hours, while retaining the original DTSTART interval residue.
func profileTiming(s Schedule, start, now time.Time, excluded map[int64]bool) (Timing, bool) {
	var until time.Time
	if s.Until != "" {
		until, _ = WallTime(s.Until)
	}
	anchor := now.In(start.Location())
	if !until.IsZero() && until.Before(anchor) {
		anchor = until
	}
	index := profileIndex(s, start, anchor)
	out := Timing{State: "past"}
	// Start a little beyond the estimate for month-end normalization and DST.
	backwardDone := false
	for n, steps := index+2, 0; steps < 10000; n, steps = n-1, steps+1 {
		if n < 0 {
			backwardDone = true
			break
		}
		t, _, valid := profileCandidate(s, start, n)
		if valid && t.Before(now) && (until.IsZero() || !t.After(until)) && !excluded[t.Unix()] {
			value := t.UTC()
			out.Last = &value
			backwardDone = true
			break
		}
	}
	if !backwardDone {
		return Timing{}, false
	}
	forwardDone := false
	first := index - 2
	if first < 0 {
		first = 0
	}
	for n, steps := first, 0; steps < 10000; n, steps = n+1, steps+1 {
		t, c, valid := profileCandidate(s, start, n)
		if !until.IsZero() && c.After(civil(until)) {
			forwardDone = true
			break
		}
		if c.Year() > 9999 {
			break
		}
		if valid && !t.Before(now) && !excluded[t.Unix()] {
			value := t.UTC()
			out.Next = &value
			out.State = "upcoming"
			forwardDone = true
			break
		}
	}
	return out, forwardDone
}
