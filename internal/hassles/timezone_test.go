package hassles

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-ical"
	"github.com/emersion/go-webdav/caldav"
)

func googleTimezoneFixture(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile("../../testdata/hassles/google-melbourne.ics")
	if err != nil {
		t.Fatal(err)
	}
	return strings.ReplaceAll(string(raw), "\r\n", "\n")
}

func withTimezone(t *testing.T, raw string) caldav.CalendarObject {
	t.Helper()
	c, err := ical.NewDecoder(strings.NewReader(raw)).Decode()
	if err != nil {
		t.Fatal(err)
	}
	o := object(t, testCollection+"provider-returned-name.ics")
	o.Data.Children = append(c.Children, o.Data.Children...)
	return o
}

func TestGoogleMelbourneTimezoneProfile(t *testing.T) {
	raw := googleTimezoneFixture(t)
	for name, definition := range map[string]string{
		"actual provider definition": raw,
		"without cosmetic labels":    strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(raw, "X-LIC-LOCATION:Australia/Melbourne\n", ""), "TZNAME:GMT+10\n", ""), "TZNAME:GMT+11\n", ""),
		"reordered rule parts":       strings.ReplaceAll(raw, "FREQ=YEARLY;BYMONTH=4;BYDAY=1SU", "BYDAY=1SU;BYMONTH=4;FREQ=YEARLY"),
	} {
		t.Run(name, func(t *testing.T) {
			o := withTimezone(t, definition)
			task := inventory(t, o)[0]
			if !task.Writable || task.Schedule == nil || *task.Schedule != sample() || task.Restriction != "" {
				t.Fatalf("compatible provider schedule rejected: %+v", task)
			}
			a, _ := json.Marshal(inventory(t, object(t, "/native"))[0].Timing)
			b, _ := json.Marshal(task.Timing)
			if string(a) != string(b) {
				t.Fatalf("timezone editing changed independent timing: %s != %s", a, b)
			}
		})
	}
}

func TestGoogleMelbourneTimezoneRejectsUnrecognizedDefinitions(t *testing.T) {
	raw := googleTimezoneFixture(t)
	start := strings.Index(raw, "BEGIN:STANDARD")
	end := strings.Index(raw, "END:STANDARD") + len("END:STANDARD\n")
	standard := raw[start:end]
	for _, tc := range []struct{ name, from, to string }{
		{"wrong offset", "TZOFFSETTO:+1000", "TZOFFSETTO:+0900"},
		{"wrong prior offset", "TZOFFSETFROM:+1000", "TZOFFSETFROM:+0900"},
		{"wrong month", "BYMONTH=4", "BYMONTH=3"},
		{"wrong weekday", "BYDAY=1SU", "BYDAY=-1SU"},
		{"wrong hour anchor", "19700405T030000", "19700405T020000"},
		{"wrong date anchor", "19701004T020000", "19701011T020000"},
		{"foreign zone", "TZID:Australia/Melbourne", "TZID:Australia/Sydney"},
		{"custom zone", "TZID:Australia/Melbourne", "TZID:Custom/Melbourne"},
		{"duplicate TZID", "TZID:Australia/Melbourne", "TZID:Australia/Melbourne\nTZID:Australia/Melbourne"},
		{"duplicate DTSTART", "DTSTART:19700405T030000", "DTSTART:19700405T030000\nDTSTART:19700405T030000"},
		{"duplicate RRULE", "RRULE:FREQ=YEARLY;BYMONTH=4;BYDAY=1SU", "RRULE:FREQ=YEARLY;BYMONTH=4;BYDAY=1SU\nRRULE:FREQ=YEARLY;BYMONTH=4;BYDAY=1SU"},
		{"duplicate rule part", "BYMONTH=4", "BYMONTH=4;BYMONTH=4"},
		{"missing offset", "TZOFFSETTO:+1000\n", ""},
		{"missing transition", standard, ""},
		{"duplicate transition", standard, standard + standard},
		{"extra transition", "END:VTIMEZONE", "BEGIN:DAYLIGHT\nEND:DAYLIGHT\nEND:VTIMEZONE"},
		{"extra timezone", "END:VCALENDAR", "BEGIN:VTIMEZONE\nTZID:UTC\nEND:VTIMEZONE\nEND:VCALENDAR"},
		{"unrecognized component", "BEGIN:STANDARD", "BEGIN:CUSTOM"},
		{"rule count", "BYMONTH=4", "BYMONTH=4;COUNT=10"},
		{"rule until", "BYMONTH=4", "BYMONTH=4;UNTIL=21000101T000000Z"},
		{"unsupported BY filter", "BYMONTH=4", "BYMONTH=4;BYHOUR=3"},
		{"malformed rule", "BYMONTH=4", "BYMONTH"},
		{"RDATE", "END:STANDARD", "RDATE:20260405T030000\nEND:STANDARD"},
		{"EXDATE", "END:STANDARD", "EXDATE:20260405T030000\nEND:STANDARD"},
		{"parameterized anchor", "DTSTART:19700405T030000", "DTSTART;TZID=Australia/Melbourne:19700405T030000"},
		{"malformed offset", "TZOFFSETTO:+1000", "TZOFFSETTO:1000"},
		{"parameterized TZID", "TZID:Australia/Melbourne", "TZID;X-CUSTOM=bad:Australia/Melbourne"},
		{"parameterized rule", "RRULE:FREQ=YEARLY", "RRULE;X-CUSTOM=bad:FREQ=YEARLY"},
		{"unknown property", "END:STANDARD", "X-CUSTOM:yes\nEND:STANDARD"},
		{"ambiguous label", "TZNAME:GMT+10", "TZNAME:GMT+10\nTZNAME:AEST"},
		{"foreign label", "X-LIC-LOCATION:Australia/Melbourne", "X-LIC-LOCATION:Europe/London"},
		{"nested component", "END:STANDARD", "BEGIN:VALARM\nEND:VALARM\nEND:STANDARD"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			definition := strings.Replace(raw, tc.from, tc.to, 1)
			// Keep component delimiters balanced so malformed properties reach the guard.
			if tc.name == "unrecognized component" {
				definition = strings.Replace(definition, "END:STANDARD", "END:CUSTOM", 1)
			}
			o := withTimezone(t, definition)
			task := inventory(t, o)[0]
			if task.Schedule != nil || task.Restriction != importedTimezoneRestriction || !task.Writable {
				t.Fatalf("unsupported definition exposed schedule: %+v", task)
			}
			f := &fakeBackend{objects: []caldav.CalendarObject{o}}
			_, err := Mutate(context.Background(), f, Mutation{Action: "edit", ID: task.ID, Revision: task.Revision, Schedule: sample()}, now)
			if err == nil || len(f.writes) != 0 {
				t.Fatal("unsupported definition reached write", err)
			}
		})
	}
}

func TestEmbeddedTimezoneDateBoundsAndNativeDates(t *testing.T) {
	for _, date := range []string{"1970-01-01T08:30", "2007-10-15T08:30", "2007-12-31T23:59", "2008-01-01T00:00", "2026-11-05T08:30", "2100-12-31T23:59"} {
		t.Run(date, func(t *testing.T) {
			s := sample()
			s.Start, s.Cadence, s.Interval = date, "once", 1
			for _, embedded := range []bool{false, true} {
				o := object(t, "/dates")
				if embedded {
					o = withTimezone(t, googleTimezoneFixture(t))
				}
				if err := ApplySchedule(o.Data.Events()[0], s, now); err != nil {
					t.Fatal(err)
				}
				got := inventory(t, o)[0]
				if (got.Schedule != nil) != (!embedded || date >= "2008-01-01T00:00") {
					t.Fatalf("embedded=%v date=%s: %+v", embedded, date, got)
				}
			}
		})
	}
	for _, change := range []func(*Schedule){
		func(s *Schedule) { s.Start = "2007-10-15T08:30" },
		func(s *Schedule) { s.Until = "2007-10-15T08:30" },
		func(s *Schedule) { s.Start = "2026-10-04T02:30" },
		func(s *Schedule) { s.Start = "2026-04-05T02:30" },
	} {
		o := withTimezone(t, googleTimezoneFixture(t))
		task := inventory(t, o)[0]
		s := *task.Schedule
		change(&s)
		before, _ := Encode(o.Data)
		f := &fakeBackend{objects: []caldav.CalendarObject{o}}
		_, err := Mutate(context.Background(), f, Mutation{Action: "edit", ID: task.ID, Revision: task.Revision, Schedule: s}, now)
		after, _ := Encode(o.Data)
		if err == nil || len(f.writes) != 0 || before != after {
			t.Fatal("invalid requested date mutated resource", err)
		}
	}
}

// Bound the profile's claimed modern range against the embedded IANA database.
func TestGoogleProfileAgreesWithModernIANA(t *testing.T) {
	loc, _ := time.LoadLocation(Zone)
	for year := 2008; year <= 2100; year++ {
		january := time.Date(year, 1, 1, 0, 0, 0, 0, loc)
		_, offset := january.Zone()
		if offset != 11*3600 {
			t.Fatalf("unexpected January offset: %v", january)
		}
		for _, tc := range []struct {
			month          time.Month
			hour, from, to int
		}{{time.April, 3, 11, 10}, {time.October, 2, 10, 11}} {
			first := time.Date(year, tc.month, 1, tc.hour, 0, 0, 0, time.UTC)
			day := 1 + (7-int(first.Weekday()))%7
			transition := time.Date(year, tc.month, day, tc.hour-tc.from, 0, 0, 0, time.UTC)
			_, before := transition.Add(-time.Second).In(loc).Zone()
			_, after := transition.In(loc).Zone()
			if before != tc.from*3600 || after != tc.to*3600 {
				t.Fatalf("IANA/profile mismatch at %v: %d -> %d", transition, before, after)
			}
		}
	}
}

type timezoneBackend struct {
	fakeBackend
	body string
}

func (f *timezoneBackend) Write(ctx context.Context, collection, href, etag, body string) error {
	f.body = body
	return f.fakeBackend.Write(ctx, collection, href, etag, body)
}

func protectedCalendar(t *testing.T, raw string, changingTime bool) string {
	t.Helper()
	c, err := ical.NewDecoder(strings.NewReader(raw)).Decode()
	if err != nil {
		t.Fatal(err)
	}
	e := c.Events()[0]
	for _, name := range []string{"SUMMARY", "DTSTAMP", "LAST-MODIFIED"} {
		e.Props.Del(name)
	}
	if changingTime {
		for _, name := range []string{"DTSTART", "DTEND", "RRULE"} {
			e.Props.Del(name)
		}
	}
	value, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	return string(value)
}

func TestGoogleTimezoneConditionalEditsPreserveProtectedContent(t *testing.T) {
	for _, changingTime := range []bool{false, true} {
		t.Run(map[bool]string{false: "title only", true: "time and recurrence"}[changingTime], func(t *testing.T) {
			o := withTimezone(t, googleTimezoneFixture(t))
			e := o.Data.Events()[0]
			e.Props.Get("RRULE").Value = "INTERVAL=2;FREQ=DAILY"
			e.Props.SetText("DESCRIPTION", "Human notes\n[docket]\n[hassle:v1 task="+testID+"]")
			e.Props.SetText("X-UNKNOWN", "preserve event")
			o.Data.Props.SetText("X-CUSTOM", "preserve calendar")
			e.Children = append(e.Children, ical.NewComponent("VALARM"))
			e.Children[1].Props.SetText("X-ALARM", "preserve second alarm")
			task := inventory(t, o)[0]
			s := *task.Schedule
			s.Title = "Renamed"
			if changingTime {
				s.Start, s.Until, s.Cadence, s.Interval = "2027-04-05T09:15", "2028-01-01T09:15", "weekly", 3
			}
			before, _ := Encode(o.Data)
			f := &timezoneBackend{fakeBackend: fakeBackend{objects: []caldav.CalendarObject{o}}}
			in := Mutation{Action: "edit", ID: task.ID, Revision: task.Revision, Schedule: s, Location: Location{CalendarID: testCollection}}
			if _, err := Mutate(context.Background(), f, in, now.Add(time.Hour)); err != nil {
				t.Fatal(err)
			}
			if len(f.writes) != 1 || f.writes[0] != o.Path+` "v1"` {
				t.Fatalf("not one conditional PUT to the returned path: %v", f.writes)
			}
			if protectedCalendar(t, before, changingTime) != protectedCalendar(t, f.body, changingTime) {
				t.Fatal("edit changed protected properties/components")
			}
			parsed, err := ical.NewDecoder(strings.NewReader(f.body)).Decode()
			if err != nil {
				t.Fatal(err)
			}
			got, why := readSchedule(parsed.Events()[0])
			if got == nil || *got != s {
				t.Fatalf("edited schedule: %+v %s", got, why)
			}
			f.writes = nil
			if _, err := Mutate(context.Background(), f, in, now); !errors.Is(err, ErrConflict) || len(f.writes) != 0 {
				t.Fatal("stale edit reached write", err)
			}
		})
	}
}

func TestCompatibleTimezoneRetainsReadOnlyAndScheduleGuards(t *testing.T) {
	for name, change := range map[string]func(*caldav.CalendarObject){
		"ownership": func(o *caldav.CalendarObject) {
			o.Data.Events()[0].Props.SetText("DESCRIPTION", "[hassle:v1 task="+testID+"]")
		},
		"attendee": func(o *caldav.CalendarObject) {
			o.Data.Events()[0].Props.SetText("ATTENDEE", "mailto:person@example.test")
		},
		"organizer": func(o *caldav.CalendarObject) {
			o.Data.Events()[0].Props.SetText("ORGANIZER", "mailto:person@example.test")
		},
		"weak etag":    func(o *caldav.CalendarObject) { o.ETag = `W/"1"` },
		"missing etag": func(o *caldav.CalendarObject) { o.ETag = "" },
		"method":       func(o *caldav.CalendarObject) { o.Data.Props.SetText("METHOD", "REQUEST") },
	} {
		t.Run(name, func(t *testing.T) {
			o := withTimezone(t, googleTimezoneFixture(t))
			change(&o)
			task := inventory(t, o)[0]
			if task.Writable || !strings.HasPrefix(task.Restriction, "Read-only:") {
				t.Fatalf("guard lost: %+v", task)
			}
			for _, action := range []string{"edit", "done"} {
				f := &fakeBackend{objects: []caldav.CalendarObject{o}}
				_, err := Mutate(context.Background(), f, Mutation{Action: action, ID: task.ID, Revision: task.Revision, Schedule: sample()}, now)
				if err == nil || len(f.writes) != 0 {
					t.Fatal("read-only resource reached write", err)
				}
			}
		})
	}
	for _, rule := range []string{"FREQ=HOURLY", "FREQ=DAILY;COUNT=3", "FREQ=DAILY;BYHOUR=8"} {
		o := withTimezone(t, googleTimezoneFixture(t))
		o.Data.Events()[0].Props.Get("RRULE").Value = rule
		if inventory(t, o)[0].Schedule != nil {
			t.Fatalf("embedded profile bypassed inverse parser for %s", rule)
		}
	}
}

func TestCompatibleTimezoneRetainsSimpleScheduleLimits(t *testing.T) {
	for name, change := range map[string]func(ical.Event){
		"RDATE":           func(e ical.Event) { e.Props.SetText("RDATE", "20261106T083000") },
		"EXDATE":          func(e ical.Event) { e.Props.SetText("EXDATE", "20261106T083000") },
		"duration":        func(e ical.Event) { e.Props.Get("DTEND").Value = "20261105T093000" },
		"floating":        func(e ical.Event) { e.Props.Get("DTSTART").Params.Del("TZID") },
		"foreign zone":    func(e ical.Event) { e.Props.Get("DTSTART").Params.Set("TZID", "Australia/Sydney") },
		"seconds":         func(e ical.Event) { e.Props.Get("DTSTART").Value = "20261105T083030" },
		"duplicate start": func(e ical.Event) { e.Props.Add(e.Props.Get("DTSTART")) },
	} {
		t.Run(name, func(t *testing.T) {
			o := withTimezone(t, googleTimezoneFixture(t))
			change(o.Data.Events()[0])
			task := inventory(t, o)[0]
			if task.Schedule != nil || task.Restriction == "" {
				t.Fatalf("embedded timezone bypassed schedule guard: %+v", task)
			}
			f := &fakeBackend{objects: []caldav.CalendarObject{o}}
			_, err := Mutate(context.Background(), f, Mutation{Action: "edit", ID: task.ID, Revision: task.Revision, Schedule: sample()}, now)
			if err == nil || len(f.writes) != 0 {
				t.Fatal("unsupported schedule reached write", err)
			}
		})
	}
	a := withTimezone(t, googleTimezoneFixture(t))
	b := withTimezone(t, googleTimezoneFixture(t))
	b.Path = "/second-resource"
	if task := inventory(t, a, b)[0]; task.Schedule != nil || task.Resources != 2 {
		t.Fatalf("embedded timezone bypassed group guard: %+v", task)
	}
	exception := ical.NewEvent()
	exception.Props.SetText("UID", "hassle-"+testID)
	exception.Props.SetDateTime("RECURRENCE-ID", now)
	a.Data.Children = append(a.Data.Children, exception.Component)
	if task := inventory(t, a)[0]; task.Schedule != nil || !strings.Contains(task.Restriction, "exceptions") {
		t.Fatalf("embedded timezone bypassed exception guard: %+v", task)
	}
}

func TestNativeHistoricalRequestedEditRemainsSupported(t *testing.T) {
	o := object(t, "/native-history")
	task := inventory(t, o)[0]
	s := *task.Schedule
	s.Start = "1970-01-01T08:30"
	f := &fakeBackend{objects: []caldav.CalendarObject{o}}
	_, err := Mutate(context.Background(), f, Mutation{Action: "edit", ID: task.ID, Revision: task.Revision, Schedule: s}, now)
	if err != nil || len(f.writes) != 1 {
		t.Fatal("native historical edit restricted", err)
	}
}
