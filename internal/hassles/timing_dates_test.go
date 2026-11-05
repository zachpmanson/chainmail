package hassles

import (
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-ical"
	"github.com/emersion/go-webdav/caldav"
)

func TestDateTiming(t *testing.T) {
	for _, tc := range []struct{ name, start, rule, extra, at, state, day string }{
		{"monthly 28", "20261028", "FREQ=MONTHLY", "", "2026-11-04T01:00:00Z", "upcoming", "2026-11-28"},
		{"Google November", "20261028", "FREQ=MONTHLY;WKST=TU;BYMONTHDAY=28", "DTEND;VALUE=DATE:20261029\r\n", "2026-11-05T04:00:00Z", "upcoming", "2026-11-28"},
		{"Google December", "20261028", "FREQ=MONTHLY;WKST=TU;BYMONTHDAY=28", "DTEND;VALUE=DATE:20261029\r\n", "2026-11-29T01:00:00Z", "upcoming", "2026-12-28"},
		{"Google January", "20261028", "FREQ=MONTHLY;WKST=TU;BYMONTHDAY=28", "DTEND;VALUE=DATE:20261029\r\n", "2026-12-29T01:00:00Z", "upcoming", "2027-01-28"},
		{"Google inclusive until", "20261028", "FREQ=MONTHLY;WKST=TU;BYMONTHDAY=28;UNTIL=20261128", "DTEND;VALUE=DATE:20261029\r\n", "2026-11-29T01:00:00Z", "past", "2026-11-28"},
		{"Google month end skips February", "20260131", "FREQ=MONTHLY;BYMONTHDAY=31", "", "2026-02-01T01:00:00Z", "upcoming", "2026-03-31"},
		{"December 28", "20261028", "FREQ=MONTHLY", "", "2026-11-29T01:00:00Z", "upcoming", "2026-12-28"},
		{"January 28", "20261028", "FREQ=MONTHLY", "", "2026-12-29T01:00:00Z", "upcoming", "2027-01-28"},
		{"today", "20261104", "", "", "2026-11-04T12:59:59Z", "upcoming", "2026-11-04"},
		{"yesterday", "20261103", "", "", "2026-11-04T01:00:00Z", "past", "2026-11-03"},
		{"tomorrow", "20261105", "", "", "2026-11-04T01:00:00Z", "upcoming", "2026-11-05"},
		{"inclusive until today", "20261101", "FREQ=DAILY;UNTIL=20261104", "", "2026-11-04T12:59:59Z", "upcoming", "2026-11-04"},
		{"until ended", "20261101", "FREQ=DAILY;UNTIL=20261104", "", "2026-11-04T13:00:00Z", "past", "2026-11-04"},
		{"count ended", "20261101", "FREQ=DAILY;COUNT=2", "", "2026-11-04T01:00:00Z", "past", "2026-11-02"},
		{"month end skips February", "20260131", "FREQ=MONTHLY", "", "2026-02-01T01:00:00Z", "upcoming", "2026-03-31"},
		{"leap skips nonleap", "20240229", "FREQ=YEARLY;COUNT=2", "", "2026-11-04T01:00:00Z", "upcoming", "2028-02-29"},
		{"weekly days", "20261102", "FREQ=WEEKLY;BYDAY=MO,WE,FR", "", "2026-11-04T01:00:00Z", "upcoming", "2026-11-04"},
		{"date additions and exclusions", "20261101", "FREQ=DAILY;COUNT=2", "RDATE;VALUE=DATE:20261104,20261106\r\nEXDATE;VALUE=DATE:20261104\r\n", "2026-11-04T01:00:00Z", "upcoming", "2026-11-06"},
		{"explicit single day", "20261104", "", "DTEND;VALUE=DATE:20261105\r\n", "2026-11-04T01:00:00Z", "upcoming", "2026-11-04"},
		{"DST end whole day", "20260405", "", "DTEND;VALUE=DATE:20260406\r\n", "2026-04-05T13:59:59Z", "upcoming", "2026-04-05"},
		{"DST end next midnight", "20260405", "", "", "2026-04-05T14:00:00Z", "past", "2026-04-05"},
		{"DST start whole day", "20261004", "", "", "2026-10-04T12:59:59Z", "upcoming", "2026-10-04"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rule := ""
			if tc.rule != "" {
				rule = "RRULE:" + tc.rule + "\r\n"
			}
			o := timingObject(t, ";VALUE=DATE:"+tc.start, rule, tc.extra)
			at, _ := time.Parse(time.RFC3339, tc.at)
			tasks, err := Inventory([]caldav.CalendarObject{o}, at)
			if err != nil || len(tasks) != 1 {
				t.Fatalf("inventory: %+v %v", tasks, err)
			}
			task := tasks[0]
			got := task.Timing
			date := got.Next
			flag := got.NextAllDay
			if tc.state == "past" {
				date = got.Last
				flag = got.LastAllDay
			}
			loc, _ := time.LoadLocation(Zone)
			if got.State != tc.state || !flag || date == nil || date.In(loc).Format("2006-01-02") != tc.day {
				t.Fatalf("timing: %+v", got)
			}
			if !strings.Contains(task.Reminder, "All day") || strings.Contains(task.Reminder, "00:00") {
				t.Fatal(task.Reminder)
			}
			if !task.Writable || task.Schedule != nil || task.Instance != nil || task.InstanceRestriction == "" {
				t.Fatalf("mutation safety widened: %+v", task)
			}
		})
	}
}

func TestDateTimingRejectsUnsupported(t *testing.T) {
	for _, tc := range []struct{ name, start, rule, extra string }{
		{"untyped", ":20261104", "", ""},
		{"different monthly day", ";VALUE=DATE:20261104", "FREQ=MONTHLY;BYMONTHDAY=5", ""},
		{"duplicate monthly day", ";VALUE=DATE:20261104", "FREQ=MONTHLY;BYMONTHDAY=4;BYMONTHDAY=4", ""},
		{"monthly day list", ";VALUE=DATE:20261104", "FREQ=MONTHLY;BYMONTHDAY=4,5", ""},
		{"weekly monthly day", ";VALUE=DATE:20261104", "FREQ=WEEKLY;BYMONTHDAY=4", ""},
		{"yearly monthly day", ";VALUE=DATE:20261104", "FREQ=YEARLY;BYMONTHDAY=4", ""},
		{"negative monthly day", ";VALUE=DATE:20261104", "FREQ=MONTHLY;BYMONTHDAY=-4", ""},
		{"invalid day", ";VALUE=DATE:20260229", "", ""},
		{"lower bound", ";VALUE=DATE:19691231", "", ""},
		{"upper bound", ";VALUE=DATE:21010101", "", ""},
		{"timezone date", ";VALUE=DATE;TZID=Australia/Melbourne:20261104", "", ""},
		{"date with datetime value", ";VALUE=DATE:20261104T000000Z", "", ""},
		{"datetime with date value", ";VALUE=DATE-TIME:20261104", "", ""},
		{"mixed rdate", ";VALUE=DATE:20261104", "", "RDATE:20261105T000000Z\r\n"},
		{"mixed exdate", ";VALUE=DATE:20261104", "", "EXDATE:20261104T000000Z\r\n"},
		{"untyped rdate", ";VALUE=DATE:20261104", "", "RDATE:20261105\r\n"},
		{"period", ";VALUE=DATE:20261104", "", "RDATE;VALUE=PERIOD:20261105T000000Z/PT1H\r\n"},
		{"mixed until", ";VALUE=DATE:20261104", "FREQ=DAILY;UNTIL=20261105T000000Z", ""},
		{"invalid until", ";VALUE=DATE:20261104", "FREQ=DAILY;UNTIL=20260230", ""},
		{"count and until", ";VALUE=DATE:20261104", "FREQ=DAILY;COUNT=2;UNTIL=20261105", ""},
		{"unsupported rule", ";VALUE=DATE:20261104", "FREQ=MONTHLY;BYMONTHDAY=28", ""},
		{"budget", ";VALUE=DATE:19700101", "FREQ=DAILY", ""},
		{"multiday", ";VALUE=DATE:20261104", "", "DTEND;VALUE=DATE:20261106\r\n"},
		{"end equal start", ";VALUE=DATE:20261104", "", "DTEND;VALUE=DATE:20261104\r\n"},
		{"mixed end", ";VALUE=DATE:20261104", "", "DTEND:20261105T000000Z\r\n"},
		{"duration", ";VALUE=DATE:20261104", "", "DURATION:P1D\r\n"},
		{"duplicate start", ";VALUE=DATE:20261104", "", "DTSTART;VALUE=DATE:20261105\r\n"},
		{"duplicate end", ";VALUE=DATE:20261104", "", "DTEND;VALUE=DATE:20261105\r\nDTEND;VALUE=DATE:20261105\r\n"},
		{"duplicate value parameter", ";VALUE=DATE,DATE:20261104", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rule := ""
			if tc.rule != "" {
				rule = "RRULE:" + tc.rule + "\r\n"
			}
			got := resourceTiming(timingObject(t, tc.start, rule, tc.extra).Data.Events(), now)
			if got.State != "unknown" || got.Note == "" {
				t.Fatalf("guessed timing: %+v", got)
			}
		})
	}
	timed := timingObject(t, ";TZID=Australia/Melbourne:20261104T130000", "", "EXDATE;VALUE=DATE:20261104\r\n")
	if got := resourceTiming(timed.Data.Events(), now); got.State != "unknown" {
		t.Fatalf("mixed types: %+v", got)
	}
}

func TestDateTimingGroupsAndOwnership(t *testing.T) {
	date := timingObject(t, ";VALUE=DATE:20261104", "", "")
	timed := timingObject(t, ";TZID=Australia/Melbourne:20261103T130000", "", "")
	timed.Path = "/timed"
	task := inventory(t, date, timed)[0]
	if !task.Timing.NextAllDay || task.Timing.LastAllDay || task.Timing.State != "upcoming" || task.Instance != nil {
		t.Fatalf("mixed group: %+v", task)
	}
	unknown := timingObject(t, ":20261104T130000", "", "")
	unknown.Path = "/unknown"
	task = inventory(t, date, unknown)[0]
	if task.Timing.State != "upcoming" || task.Timing.Note == "" || !task.Timing.NextAllDay || task.Instance != nil {
		t.Fatalf("incomplete: %+v", task)
	}
	date.Data.Events()[0].Props.SetText("DESCRIPTION", "[hassle:v1 task="+testID+"]")
	task = inventory(t, date)[0]
	if task.Writable || task.Schedule != nil || task.Instance != nil || task.Timing.State != "upcoming" {
		t.Fatalf("ownership changed: %+v", task)
	}
	date.Data.Events()[0].Props.SetText("STATUS", "CANCELLED")
	if got := inventory(t, date); len(got) != 0 {
		t.Fatalf("cancelled included: %+v", got)
	}
	date.Data.Events()[0].Props.Del("STATUS")
	exception := ical.NewEvent()
	exception.Props.SetText("RECURRENCE-ID", "20261104")
	exception.Props.SetText("STATUS", "CANCELLED")
	date.Data.Children = append(date.Data.Children, exception.Component)
	if got := inventory(t, date)[0].Timing; got.State != "unknown" {
		t.Fatalf("detached exception: %+v", got)
	}
}

func TestOwnedDateWithoutTimezoneStillCannotEditOrComplete(t *testing.T) {
	o := timingObject(t, ";VALUE=DATE:20261028", "RRULE:FREQ=MONTHLY\r\n", "DTEND;VALUE=DATE:20261029\r\n")
	event := o.Data.Events()[0]
	o.Data = ical.NewCalendar()
	o.Data.Props.SetText("PRODID", "-//test//EN")
	o.Data.Props.SetText("VERSION", "2.0")
	event.Props.SetDateTime("DTSTAMP", now)
	o.Data.Children = append(o.Data.Children, event.Component)
	before, err := Encode(o.Data)
	if err != nil {
		t.Fatal(err)
	}
	task := inventory(t, o)[0]
	if !task.Writable || task.Schedule != nil || !strings.Contains(task.Restriction, "duration") || task.Instance != nil || !task.Timing.NextAllDay {
		t.Fatalf("unsafe DATE mutation eligibility: %+v", task)
	}
	after, err := Encode(o.Data)
	if err != nil || before != after {
		t.Fatal("read-only inventory modified event")
	}
}
