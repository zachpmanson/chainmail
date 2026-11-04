package hassles

import (
	"testing"
	"time"

	"github.com/emersion/go-ical"
)

func TestTimingRejectsUnboundedFiltersBeforeIteration(t *testing.T) {
	start := time.Date(2026, 1, 1, 8, 30, 0, 0, time.UTC)
	for _, rule := range []string{
		"FREQ=DAILY;BYMONTH=2;BYMONTHDAY=30;COUNT=1",
		"FREQ=DAILY;BYMONTH=2;BYMONTHDAY=30;UNTIL=20990101T000000Z",
		"FREQ=DAILY;INTERVAL=7;BYDAY=MO;COUNT=1",
		"FREQ=MONTHLY;INTERVAL=12;BYMONTHDAY=31;COUNT=1",
		"FREQ=YEARLY;BYMONTH=2;BYMONTHDAY=30;COUNT=1",
		"FREQ=MONTHLY;BYDAY=MO;BYSETPOS=5;COUNT=1",
		"FREQ=WEEKLY;BYDAY=1MO;COUNT=1",
	} {
		t.Run(rule, func(t *testing.T) {
			p := ical.NewProp("RRULE")
			p.Value = rule
			if r, ok := readRule(p, start); ok || r != nil {
				t.Fatal("unsafe filter reached the recurrence iterator")
			}
			object := timingObject(t, ":20260101T083000Z", "RRULE:"+rule+"\r\n", "")
			if got := inventory(t, object)[0].Timing; got.State != "unknown" || got.Note == "" {
				t.Fatalf("unsafe schedule must remain visible and undated: %+v", got)
			}
		})
	}
}

func TestTimingRetainsBoundedWeeklyWeekdays(t *testing.T) {
	object := timingObject(t, ";TZID=Australia/Melbourne:20261026T130000", "RRULE:FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR;COUNT=10\r\n", "")
	got := inventory(t, object)[0].Timing
	if got.State != "upcoming" || got.Next == nil || got.Next.Format(time.RFC3339) != "2026-11-04T02:00:00Z" {
		t.Fatalf("safe weekdays schedule rejected: %+v", got)
	}
}
