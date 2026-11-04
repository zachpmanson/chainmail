package hassles

import (
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-ical"
	"github.com/emersion/go-webdav/caldav"
)

func timingObject(t *testing.T, start, rule, extra string) caldav.CalendarObject {
	t.Helper()
	c, err := ical.NewDecoder(strings.NewReader("BEGIN:VCALENDAR\r\nVERSION:2.0\r\nBEGIN:VTIMEZONE\r\nTZID:Australia/Melbourne\r\nEND:VTIMEZONE\r\nBEGIN:VEVENT\r\nUID:imported\r\nDESCRIPTION:[docket] [hassle:v1 task=" + testID + "]\r\nSUMMARY:Imported reminder\r\nDTSTART" + start + "\r\n" + rule + extra + "END:VEVENT\r\nEND:VCALENDAR\r\n")).Decode()
	if err != nil {
		t.Fatal(err)
	}
	return caldav.CalendarObject{Path: "/timing.ics", ETag: `"1"`, Data: c}
}

func TestReadOnlyRecurrenceTiming(t *testing.T) {
	for _, tc := range []struct {
		name, start, rule, extra, state, next, last string
	}{
		{"old daily with VTIMEZONE", ";TZID=Australia/Melbourne:20261026T130000", "RRULE:FREQ=DAILY\r\n", "", "upcoming", "2026-11-04T02:00:00Z", "2026-11-03T02:00:00Z"},
		{"expired until", ";TZID=Australia/Melbourne:20261026T130000", "RRULE:FREQ=DAILY;UNTIL=20261102T020000Z\r\n", "", "past", "", "2026-11-02T02:00:00Z"},
		{"sparse count beyond library default horizon", ";TZID=Australia/Melbourne:19700101T130000", "RRULE:FREQ=YEARLY;INTERVAL=999;COUNT=2\r\n", "", "upcoming", "2969-01-01T02:00:00Z", "1970-01-01T03:00:00Z"},
		{"expired count", ";TZID=Australia/Melbourne:20261026T130000", "RRULE:FREQ=DAILY;COUNT=3\r\n", "", "past", "", "2026-10-28T02:00:00Z"},
		{"future once", ";TZID=Australia/Melbourne:20261105T083000", "", "", "upcoming", "2026-11-04T21:30:00Z", ""},
		{"past once", ";TZID=Australia/Melbourne:20261103T083000", "", "", "past", "", "2026-11-02T21:30:00Z"},
		{"exclude next", ";TZID=Australia/Melbourne:20261026T130000", "RRULE:FREQ=DAILY\r\n", "EXDATE;TZID=Australia/Melbourne:20261104T130000\r\n", "upcoming", "2026-11-05T02:00:00Z", "2026-11-03T02:00:00Z"},
		{"exclude last", ";TZID=Australia/Melbourne:20261101T130000", "RRULE:FREQ=DAILY;COUNT=2\r\n", "EXDATE:20261102T020000Z\r\n", "past", "", "2026-11-01T02:00:00Z"},
		{"rdate extends ended rule", ";TZID=Australia/Melbourne:20261026T130000", "RRULE:FREQ=DAILY;COUNT=2\r\n", "RDATE;TZID=Australia/Melbourne:20261106T130000,20261107T130000\r\n", "upcoming", "2026-11-06T02:00:00Z", "2026-10-27T02:00:00Z"},
		{"floating", ":20261105T083000", "", "", "unknown", "", ""},
		{"unknown timezone", ";TZID=Custom/Unknown:20261105T083000", "", "", "unknown", "", ""},
		{"unsupported expansion", ";TZID=Australia/Melbourne:20261026T130000", "RRULE:FREQ=MINUTELY\r\n", "", "unknown", "", ""},
		{"bounded old daily", ";TZID=Australia/Melbourne:19700101T130000", "RRULE:FREQ=DAILY\r\n", "", "unknown", "", ""},
		{"all excluded", ";TZID=Australia/Melbourne:20261101T130000", "", "EXDATE:20261101T020000Z\r\n", "unknown", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := timingObject(t, tc.start, tc.rule, tc.extra)
			task := inventory(t, o)[0]
			got := task.Timing
			format := func(value *time.Time) string {
				if value == nil {
					return ""
				}
				return value.Format(time.RFC3339)
			}
			if got.State != tc.state || format(got.Next) != tc.next || format(got.Last) != tc.last {
				t.Fatalf("timing %+v, next %s last %s", got, format(got.Next), format(got.Last))
			}
			if !task.Writable || task.Schedule != nil || !strings.Contains(task.Restriction, "timezone definition") {
				t.Fatalf("read classification changed mutation safety: %+v", task)
			}
			if tc.state == "unknown" && got.Note == "" {
				t.Fatal("unknown timing needs explanation")
			}
		})
	}
}

func TestTimingUsesMelbourneDSTAndDateBoundaries(t *testing.T) {
	o := timingObject(t, ";TZID=Australia/Melbourne:20261003T083000", "RRULE:FREQ=DAILY\r\n", "")
	for _, tc := range []struct{ at, next string }{
		{"2026-10-02T23:00:00Z", "2026-10-03T21:30:00Z"},
		{"2026-10-03T22:00:00Z", "2026-10-04T21:30:00Z"},
		{"2026-10-04T21:30:00Z", "2026-10-04T21:30:00Z"},
	} {
		at, _ := time.Parse(time.RFC3339, tc.at)
		tasks, err := Inventory([]caldav.CalendarObject{o}, at)
		if err != nil || tasks[0].Timing.Next == nil || tasks[0].Timing.Next.Format(time.RFC3339) != tc.next {
			t.Fatalf("at %s: %+v %v", tc.at, tasks, err)
		}
	}
}

func TestGroupTimingAndDetachedExceptions(t *testing.T) {
	old := timingObject(t, ";TZID=Australia/Melbourne:20261101T130000", "", "")
	future := timingObject(t, ";TZID=Australia/Melbourne:20261105T130000", "", "")
	future.Path = "/future.ics"
	task := inventory(t, old, future)[0]
	if task.Timing.State != "upcoming" || task.Timing.Next.Format(time.RFC3339) != "2026-11-05T02:00:00Z" || task.Timing.Last == nil {
		t.Fatalf("old + future: %+v", task.Timing)
	}
	earlier := timingObject(t, ";TZID=Australia/Melbourne:20261104T130000", "", "")
	earlier.Path = "/earlier.ics"
	if got := inventory(t, old, future, earlier)[0].Timing; got.Next.Format(time.RFC3339) != "2026-11-04T02:00:00Z" {
		t.Fatalf("not earliest group occurrence: %+v", got)
	}
	future.Data.Events()[0].Props.Get("DTSTART").Value = "20261102T130000"
	task = inventory(t, old, future)[0]
	if task.Timing.State != "past" || task.Timing.Last.Format(time.RFC3339) != "2026-11-02T02:00:00Z" {
		t.Fatalf("ended group: %+v", task.Timing)
	}
	exception := ical.NewEvent()
	exception.Props.SetDateTime("RECURRENCE-ID", now)
	exception.Props.SetDateTime("DTSTART", now.Add(24*time.Hour))
	exception.Props.SetText("UID", "imported")
	exception.Props.SetText("STATUS", "CANCELLED")
	future.Data.Children = append(future.Data.Children, exception.Component)
	task = inventory(t, old, future)[0]
	if task.Timing.State != "unknown" || task.Timing.Note == "" {
		t.Fatalf("exception guessed as ended: %+v", task.Timing)
	}
	future.Data.Events()[0].Props.Get("DTSTART").Value = "20261105T130000"
	task = inventory(t, old, future)[0]
	if task.Timing.State != "unknown" {
		t.Fatal("exception guessed as upcoming date")
	}
	task = inventory(t, old, future, earlier)[0]
	if task.Timing.State != "upcoming" || task.Timing.Note == "" || !strings.HasPrefix(task.Reminder, "Known next") {
		t.Fatalf("incomplete group not labelled: %+v", task)
	}
}
