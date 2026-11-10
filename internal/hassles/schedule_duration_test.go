package hassles

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-ical"
)

func TestScheduleDurationUTCEndRoundTrip(t *testing.T) {
	clock := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	for _, start := range []string{"2026-04-05T01:59", "2026-04-05T03:00", "2026-10-04T01:59", "2026-10-04T03:00", "2026-01-09T15:17"} {
		for _, cadence := range []string{"once", "daily"} {
			for _, edit := range []bool{false, true} {
				name := start + "/" + cadence + "/create"
				if edit {
					name = start + "/" + cadence + "/full-edit"
				}
				t.Run(name, func(t *testing.T) {
					schedule := Schedule{Title: "Synthetic duration task", Start: start, TimeZone: Zone, Cadence: cadence, Interval: 1}
					initial := schedule
					if edit {
						initial.Start = "2026-01-08T15:17"
					}
					cal, err := NewCalendar(formatUUID, initial, clock)
					if err != nil {
						t.Fatal(err)
					}
					e := cal.Events()[0]
					if edit {
						// Existing valid local ends must lose TZID, not other parameters.
						first, _, _ := initial.Validate()
						p := ical.NewProp("DTEND")
						p.SetDateTime(first.Add(5 * time.Minute))
						p.Params.Set("X-FORMAT-PROTECTED", "keep")
						e.Props.Set(p)
						e.Props.SetText("X-FORMAT-OPAQUE", "keep event data")
						if err := ApplySchedule(e, schedule, clock); err != nil {
							t.Fatal(err)
						}
					}
					body, err := Encode(cal)
					if err != nil {
						t.Fatal(err)
					}
					saved, err := ical.NewDecoder(strings.NewReader(body)).Decode()
					if err != nil {
						t.Fatal(err)
					}
					e = saved.Events()[0]
					begin, err := e.DateTimeStart(nil)
					if err != nil {
						t.Fatal(err)
					}
					end, err := e.DateTimeEnd(nil)
					if err != nil {
						t.Fatal(err)
					}
					if end.Sub(begin) != 5*time.Minute {
						t.Fatalf("elapsed %s, want 5m; %s", end.Sub(begin), body)
					}
					if e.Props.Get("DTSTART").Params.Get("TZID") != Zone {
						t.Fatal("start lost Melbourne TZID")
					}
					p := e.Props.Get("DTEND")
					if p.Params.Get("TZID") != "" || !strings.HasSuffix(p.Value, "Z") || p.ValueType() != ical.ValueDateTime {
						t.Fatalf("end not unambiguous UTC DATE-TIME: %+v", p)
					}
					if edit {
						opaque, _ := e.Props.Text("X-FORMAT-OPAQUE")
						if p.Params.Get("X-FORMAT-PROTECTED") != "keep" || opaque != "keep event data" {
							t.Fatal("lost protected parameter/property")
						}
					}
					recognized, why := readSchedule(e)
					if recognized == nil || *recognized != schedule {
						t.Fatalf("roundtrip schedule: %+v (%s), want %+v", recognized, why, schedule)
					}
				})
			}
		}
	}
}

func TestScheduleDurationTitleOnlyKeepsLocalEnd(t *testing.T) {
	clock := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	schedule := Schedule{Title: "Synthetic local end", Start: "2026-01-09T15:17", TimeZone: Zone, Cadence: "daily", Interval: 1}
	cal, err := NewCalendar(formatUUID, schedule, clock)
	if err != nil {
		t.Fatal(err)
	}
	e := cal.Events()[0]
	first, _, _ := schedule.Validate()
	p := ical.NewProp("DTEND")
	p.SetDateTime(first.Add(5 * time.Minute))
	p.Params.Set("X-FORMAT-PROTECTED", "keep")
	e.Props.Set(p)
	e.Props.SetText("X-FORMAT-OPAQUE", "keep event data")
	cal.Props.SetText("X-FORMAT-OPAQUE", "keep root data")
	originalEnd := *cloneComponent(e.Component).Props.Get("DTEND")
	before := formatProjection(cal, "SUMMARY", "DTSTAMP", "LAST-MODIFIED")
	schedule.Title = "Edited synthetic local end"
	if err := ApplySchedule(e, schedule, clock.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if before != formatProjection(cal, "SUMMARY", "DTSTAMP", "LAST-MODIFIED") || !reflect.DeepEqual(originalEnd, *e.Props.Get("DTEND")) {
		t.Fatal("title-only rewrote protected timing/data")
	}
	body, err := Encode(cal)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := ical.NewDecoder(strings.NewReader(body)).Decode()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(originalEnd, *saved.Events()[0].Props.Get("DTEND")) {
		t.Fatal("title-only normalized original local end")
	}
	recognized, why := readSchedule(saved.Events()[0])
	if recognized == nil || *recognized != schedule {
		t.Fatalf("title-only roundtrip: %+v (%s)", recognized, why)
	}
}
