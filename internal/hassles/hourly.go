package hassles

import (
	"strconv"
	"strings"
	"time"

	"github.com/emersion/go-ical"
)

var hourlyIntervals = [...]float64{0.5, 1, 2, 3, 4, 6, 8, 12, 24}

func supportedHourlyInterval(interval float64) bool {
	for _, allowed := range hourlyIntervals {
		if interval == allowed {
			return true
		}
	}
	return false
}

func hourlyMarker(interval float64) string {
	return "[chainmail:hourly:v1 hours=" + strconv.FormatFloat(interval, 'f', -1, 64) + "]"
}

// DAILY slots are exact only for these divisors of the civil day. Half-hourly
// also divides each hour, so its minute slots never introduce extra occurrences.
func hourlySlots(s Schedule, start time.Time) (string, string) {
	var hours []string
	for hour := 0; hour < 24; hour++ {
		if s.WorkHours && (hour < 9 || hour >= 17) {
			continue
		}
		if s.Interval >= 1 && (hour-start.Hour()+24)%int(s.Interval) != 0 {
			continue
		}
		hours = append(hours, strconv.Itoa(hour))
	}
	minutes := strconv.Itoa(start.Minute())
	if s.Interval == 0.5 {
		minute := start.Minute() % 30
		minutes = strconv.Itoa(minute) + "," + strconv.Itoa(minute+30)
	}
	return strings.Join(hours, ","), minutes
}

// Metadata records UI intent, never ownership or recurrence authority. The rule
// still has to match the complete canonical slot/filter profile independently.
func readScheduleMetadata(e ical.Event, s *Schedule) bool {
	if len(e.Props["DESCRIPTION"]) > 1 {
		return false
	}
	desc := ""
	if p := e.Props.Get("DESCRIPTION"); p != nil {
		// Text() alone hides values after an unescaped comma.
		values, err := p.TextList()
		if err != nil || len(values) != 1 {
			return false
		}
		desc = values[0]
	}
	workCount := strings.Count(desc, "[chainmail:work-hours:")
	hourCount := strings.Count(desc, "[chainmail:hourly:")
	if workCount > 0 {
		if workCount != 1 || !strings.Contains(desc, workHoursMarker) {
			return false
		}
		s.WorkHours = true
	}
	if hourCount > 0 {
		if hourCount != 1 {
			return false
		}
		for _, interval := range hourlyIntervals {
			if strings.Contains(desc, hourlyMarker(interval)) {
				s.Cadence, s.Interval = "hourly", interval
				return true
			}
		}
		return false
	}
	return true
}

func applyHourlyMetadata(p *ical.Prop, s Schedule) {
	wanted := ""
	if s.Cadence == "hourly" {
		wanted = hourlyMarker(s.Interval)
	}
	for _, interval := range hourlyIntervals {
		marker := hourlyMarker(interval)
		if marker != wanted {
			// The marker is literal ASCII in iCalendar text; leave every original
			// escape, separator and property parameter untouched.
			p.Value = strings.Replace(p.Value, marker, "", 1)
		}
	}
	if wanted != "" && !strings.Contains(p.Value, wanted) {
		p.Value += `\n` + wanted
	}
}
