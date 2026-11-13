package hassles

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-ical"
	"github.com/emersion/go-webdav/caldav"
	"github.com/teambition/rrule-go"
)

func TestWorkHoursValidationAndRoundTrip(t *testing.T) {
	for _, cadence := range []string{"once", "hourly", "daily", "weekdays", "weekly", "monthly", "yearly"} {
		for _, checked := range []bool{false, true} {
			s := Schedule{Title: "Work", Start: "2026-11-06T16:59", TimeZone: Zone, Cadence: cadence, Interval: 1, WorkHours: checked}
			c, err := NewCalendar(testID, s, now)
			if err != nil {
				t.Fatal(err)
			}
			raw, err := Encode(c)
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := ical.NewDecoder(strings.NewReader(raw)).Decode()
			if err != nil {
				t.Fatal(err)
			}
			read, why := readSchedule(decoded.Events()[0])
			if read == nil || *read != s {
				t.Fatalf("%s/%v roundtrip: %+v %s\n%s", cadence, checked, read, why, raw)
			}
			e := decoded.Events()[0]
			e.Props.SetText("X-UNKNOWN", "preserved")
			e.Props.Get("DESCRIPTION").Value = `Human notes\n` + e.Props.Get("DESCRIPTION").Value
			e.Props.Get("DESCRIPTION").Params["LANGUAGE"] = []string{"en-AU"}
			s.WorkHours = !checked
			if err := ApplySchedule(e, s, now); err != nil {
				t.Fatal(err)
			}
			read, why = readSchedule(e)
			if read == nil || *read != s {
				t.Fatalf("toggle %s: %+v %s", cadence, read, why)
			}
			desc, _ := e.Props.Text("DESCRIPTION")
			if !strings.HasPrefix(desc, "Human notes\n[docket]") || e.Props.Get("DESCRIPTION").Params.Get("LANGUAGE") != "en-AU" || e.Props.Get("X-UNKNOWN").Value != "preserved" {
				t.Fatal("edit lost other content")
			}
		}
	}
	for _, wall := range []string{"2026-11-06T08:59", "2026-11-06T09:00", "2026-11-06T16:59", "2026-11-06T17:00", "2026-11-07T10:00", "2026-11-08T10:00"} {
		s := Schedule{Title: "Work", Start: wall, TimeZone: Zone, Cadence: "once", Interval: 1, WorkHours: true}
		_, _, err := s.Validate()
		valid := wall == "2026-11-06T09:00" || wall == "2026-11-06T16:59"
		if (err == nil) != valid {
			t.Fatalf("boundary %s: %v", wall, err)
		}
	}
}

func TestUncheckWorkHoursPreservesRawDescription(t *testing.T) {
	s := Schedule{Title: "Work", Start: "2026-11-06T09:00", TimeZone: Zone, Cadence: "hourly", Interval: 3, WorkHours: true}
	c, err := NewCalendar(testID, s, now)
	if err != nil {
		t.Fatal(err)
	}
	e := c.Events()[0]
	p := e.Props.Get("DESCRIPTION")
	p.Value = `Human\Nnotes\, with\; escapes\\kept\N[docket]\N[hassle:v1 task=` + testID + `]\N` + completionMarker + `\N` + hourlyMarker(s.Interval) + `\N` + workHoursMarker + `\NTrailing notes`
	p.Params = ical.Params{"VALUE": {"TEXT"}, "LANGUAGE": {"en-AU"}, "X-EXTRA": {"preserved"}}
	want := *p
	want.Value = strings.Replace(p.Value, workHoursMarker, "", 1)
	want.Params = make(ical.Params)
	for name, values := range p.Params {
		want.Params[name] = append([]string(nil), values...)
	}
	s.WorkHours = false
	if err := ApplySchedule(e, s, now); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(*p, want) {
		t.Fatalf("uncheck must remove only the raw marker:\n got %#v\nwant %#v", *p, want)
	}
	read, why := readSchedule(e)
	if read == nil || *read != s {
		t.Fatalf("unchecked schedule did not roundtrip: %+v %s", read, why)
	}
}

func workTiming(t *testing.T, s Schedule, wallNow string) Timing {
	t.Helper()
	c, err := NewCalendar(testID, s, now)
	if err != nil {
		t.Fatal(err)
	}
	at, err := WallTime(wallNow)
	if err != nil {
		t.Fatal(err)
	}
	return resourceTiming(c.Events(), at)
}
func nextWall(timing Timing) string {
	if timing.Next == nil {
		return ""
	}
	loc, _ := time.LoadLocation(Zone)
	return timing.Next.In(loc).Format("2006-01-02T15:04")
}
func TestHourlyWorkHoursPhaseAndOldSchedules(t *testing.T) {
	for _, tc := range []struct {
		interval float64
		at, next string
	}{
		{1, "2026-11-06T17:00", "2026-11-09T09:00"},
		{2, "2026-11-06T17:00", "2026-11-09T09:00"},
		{3, "2026-11-06T17:00", "2026-11-09T09:00"},
		{4, "2026-11-06T17:00", "2026-11-09T09:00"},
		{6, "2026-11-06T17:00", "2026-11-09T09:00"},
		{8, "2026-11-06T17:00", "2026-11-09T09:00"},
		{12, "2026-11-06T17:00", "2026-11-09T09:00"},
		{24, "2026-11-06T17:00", "2026-11-09T09:00"},
	} {
		s := Schedule{Title: "Phase", Start: "2026-11-06T09:00", TimeZone: Zone, Cadence: "hourly", Interval: tc.interval, WorkHours: true}
		got := workTiming(t, s, tc.at)
		if got.State != "upcoming" || nextWall(got) != tc.next {
			t.Fatalf("N=%g: %+v next=%s want=%s", tc.interval, got, nextWall(got), tc.next)
		}
	}
	for _, checked := range []bool{false, true} {
		s := Schedule{Title: "Old", Start: "2020-01-01T09:00", TimeZone: Zone, Cadence: "hourly", Interval: 1, WorkHours: checked}
		got := workTiming(t, s, "2026-11-06T10:30")
		if nextWall(got) != "2026-11-06T11:00" || got.Last == nil || got.Note != "" {
			t.Fatalf("historical hourly: %+v", got)
		}
	}
}

func TestWorkHoursCalendarFiltersSkipNotExpand(t *testing.T) {
	for _, tc := range []struct{ cadence, start, at, next string }{
		{"daily", "2026-11-06T10:00", "2026-11-06T17:00", "2026-11-09T10:00"},
		{"weekly", "2026-11-06T10:00", "2026-11-06T17:00", "2026-11-13T10:00"},
		{"weekdays", "2026-11-06T10:00", "2026-11-06T17:00", "2026-11-09T10:00"},
		{"monthly", "2026-01-30T10:00", "2026-01-30T17:00", "2026-03-30T10:00"},
		{"monthly", "2026-03-31T10:00", "2026-03-31T17:00", "2026-07-31T10:00"},
		{"yearly", "2026-11-06T10:00", "2026-11-06T17:00", "2028-11-06T10:00"},
		{"yearly", "2024-02-29T10:00", "2024-02-29T17:00", "2028-02-29T10:00"},
	} {
		s := Schedule{Title: "Calendar", Start: tc.start, TimeZone: Zone, Cadence: tc.cadence, Interval: 1, WorkHours: true}
		got := workTiming(t, s, tc.at)
		if nextWall(got) != tc.next {
			t.Fatalf("%s %s got=%+v next=%s want=%s", tc.cadence, tc.start, got, nextWall(got), tc.next)
		}
		// Short bounded provider-rule parity checks for the non-hourly filters.
		start, rule, err := s.Validate()
		if err != nil {
			t.Fatal(err)
		}
		opt, err := rrule.StrToROptionInLocation(rule, start.Location())
		if err != nil {
			t.Fatal(err)
		}
		opt.Dtstart = start
		r, err := rrule.NewRRule(*opt)
		if err != nil {
			t.Fatal(err)
		}
		at, _ := WallTime(tc.at)
		if next := r.After(at, true); next.Format("2006-01-02T15:04") != tc.next {
			t.Fatalf("RRULE expands/changes dates: %s -> %s", rule, next)
		}
	}
}

func TestHourlyWallClockDSTAndUntil(t *testing.T) {
	for _, tc := range []struct {
		start, at, next string
		interval        float64
	}{
		{"2026-10-03T01:30", "2026-10-04T01:45", "2026-10-04T03:30", 1},
		{"2026-04-04T01:30", "2026-04-05T03:15", "2026-04-05T03:30", 1},
		{"2026-10-03T09:00", "2026-10-04T08:30", "2026-10-04T09:00", 24},
		{"2026-04-04T09:00", "2026-04-05T08:30", "2026-04-05T09:00", 24},
	} {
		s := Schedule{Title: "DST", Start: tc.start, TimeZone: Zone, Cadence: "hourly", Interval: tc.interval}
		if got := workTiming(t, s, tc.at); nextWall(got) != tc.next {
			t.Fatalf("DST %+v next=%s", got, nextWall(got))
		}
	}
	s := Schedule{Title: "End", Start: "2026-11-06T09:00", TimeZone: Zone, Cadence: "hourly", Interval: 2, WorkHours: true, Until: "2026-11-09T09:00"}
	if got := workTiming(t, s, s.Until); nextWall(got) != s.Until {
		t.Fatalf("inclusive UNTIL %+v", got)
	}
	got := workTiming(t, s, "2026-11-09T09:01")
	if got.State != "past" || got.Next != nil || got.Last == nil || got.Last.In(time.FixedZone("AEDT", 11*3600)).Format("2006-01-02T15:04") != s.Until {
		t.Fatalf("ended %+v", got)
	}
}

func TestHourlyOverlapUsesFirstOccurrenceOnly(t *testing.T) {
	s := Schedule{Title: "Fold", Start: "2026-04-04T01:30", TimeZone: Zone, Cadence: "hourly", Interval: 1}
	c, err := NewCalendar(testID, s, now)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ at, next string }{
		{"2026-04-04T15:15:00Z", "2026-04-04T15:30:00Z"},
		{"2026-04-04T15:45:00Z", "2026-04-04T17:30:00Z"},
		{"2026-04-04T16:15:00Z", "2026-04-04T17:30:00Z"},
	} {
		at, _ := time.Parse(time.RFC3339, tc.at)
		got := resourceTiming(c.Events(), at)
		if got.Next == nil || got.Next.Format(time.RFC3339) != tc.next {
			t.Fatalf("fold %s: %+v", tc.at, got)
		}
	}
}

func TestProfileRecognitionOrderAndBudget(t *testing.T) {
	s := Schedule{Title: "Order", Start: "2026-11-06T09:00", TimeZone: Zone, Cadence: "hourly", Interval: 1, WorkHours: true}
	c, err := NewCalendar(testID, s, now)
	if err != nil {
		t.Fatal(err)
	}
	e := c.Events()[0]
	e.Props.Get("RRULE").Value = "BYMINUTE=0;BYHOUR=" + workHourList + ";BYDAY=" + workDays + ";FREQ=DAILY"
	read, why := readSchedule(e)
	if read == nil || *read != s {
		t.Fatalf("reordered/implicit DAILY interval/marked profile: %+v %s", read, why)
	}
	start, _ := WallTime(s.Start)
	excluded := map[int64]bool{}
	s.WorkHours = false
	for n := int64(0); n < 10001; n++ {
		date, _, _ := profileCandidate(s, start, n)
		excluded[date.Unix()] = true
	}
	if got, complete := profileTiming(s, start, start, excluded); complete || got.Next != nil {
		t.Fatalf("budget exhaustion inferred an end: %+v %v", got, complete)
	}
}

func TestNewProfileRecognitionRemainsNarrowAndBounded(t *testing.T) {
	for _, rule := range []string{
		"FREQ=HOURLY;INTERVAL=24;BYHOUR=9",
		"FREQ=HOURLY;BYDAY=MO,TU,WE,TH,FR",
		"FREQ=HOURLY;BYHOUR=9,10,11,12,13,14,15,16",
		"FREQ=HOURLY;COUNT=3",
		"FREQ=HOURLY;INTERVAL=0",
		"FREQ=HOURLY;INTERVAL=999999999999999999",
		"FREQ=DAILY;INTERVAL=7;BYDAY=MO,TU,WE,TH,FR",
		"FREQ=MONTHLY;BYDAY=MO,TU,WE,TH,FR",
		"FREQ=YEARLY;BYDAY=MO,TU,WE,TH,FR",
		"FREQ=YEARLY;BYDAY=MO,TU,WE,TH,FR;BYMONTH=2;BYMONTHDAY=30",
		"FREQ=HOURLY;INTERVAL=1;INTERVAL=2",
	} {
		o := timingObject(t, ";TZID=Australia/Melbourne:20261107T180000", "RRULE:"+rule+"\r\n", "")
		got := resourceTiming(o.Data.Events(), now)
		if got.State != "unknown" {
			t.Fatalf("hostile %s: %+v", rule, got)
		}
	}
	for _, desc := range []string{workHoursMarker + workHoursMarker, "[chainmail:work-hours:v2]"} {
		o := object(t, "/metadata.ics")
		e := o.Data.Events()[0]
		e.Props.SetText("DESCRIPTION", "[docket]\n"+desc)
		if s, _ := readSchedule(e); s != nil {
			t.Fatal("ambiguous marker enabled editing")
		}
	}
	// DATE rules never enter the new hourly evaluator.
	o := timingObject(t, ";VALUE=DATE:20261106", "RRULE:FREQ=HOURLY\r\n", "")
	if got := resourceTiming(o.Data.Events(), now); got.State != "unknown" {
		t.Fatal("all-day support broadened", got)
	}
}

func TestWorkHoursCompletionAndCreateRetry(t *testing.T) {
	for _, cadence := range []string{"once", "weekdays", "weekly", "hourly", "daily", "monthly", "yearly"} {
		s := Schedule{Title: "Retry", Start: "2026-11-06T16:00", TimeZone: Zone, Cadence: cadence, Interval: 1, WorkHours: true}
		calendar, err := NewCalendar(testID, s, now)
		if err != nil {
			t.Fatal(err)
		}
		o := caldav.CalendarObject{Path: testCollection + "hassle-" + testID + ".ics", ETag: `"1"`, Data: calendar}
		f := &completionBackend{objects: []caldav.CalendarObject{o}}
		in := Mutation{Location: Location{CalendarID: testCollection}, Action: "create", ID: testID, Schedule: s}
		result, err := Mutate(context.Background(), f, in, now)
		if cadence == "hourly" {
			if !errors.Is(err, ErrConflict) || len(f.writes) != 0 {
				t.Fatalf("repeated hourly UUID accepted: %+v %v", result, err)
			}
		} else if err != nil || result.Message != "Already created" || len(f.writes) != 0 {
			t.Fatalf("retry %s: %+v %v", cadence, result, err)
		}
		if cadence == "once" {
			continue
		}
		input := completionInput(t, f)
		input.DryRun = ptr(false)
		if _, err := Mutate(context.Background(), f, input, now); err != nil {
			t.Fatal(err)
		}
		task := inventory(t, f.objects...)[0]
		if task.Schedule == nil || *task.Schedule != s || task.Instance == nil || task.Instance.At == *input.InstanceAt {
			t.Fatalf("completion %s: %+v", cadence, task)
		}
		if cadence == "hourly" && nextWall(task.Timing) != "2026-11-09T09:00" {
			t.Fatalf("completion did not skip out-of-hours occurrences: %+v", task.Timing)
		}
		event := f.objects[0].Data.Events()[0]
		if !validCompletionDates(event) || !strings.Contains(event.Props.Get("DESCRIPTION").Value, workHoursMarker) {
			t.Fatal("lost schedule/completion metadata")
		}
	}
}
