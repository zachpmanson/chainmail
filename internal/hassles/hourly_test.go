package hassles

import (
	"context"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-ical"
	"github.com/emersion/go-webdav/caldav"
	"github.com/teambition/rrule-go"
)

func TestHourlyDailySlotProfiles(t *testing.T) {
	for _, interval := range hourlyIntervals {
		for _, checked := range []bool{false, true} {
			for _, minute := range []int{0, 17, 45, 59} {
				t.Run(fmt.Sprintf("%g/%v/%d", interval, checked, minute), func(t *testing.T) {
					s := Schedule{Title: "Slots", Start: fmt.Sprintf("2026-11-06T09:%02d", minute), TimeZone: Zone, Cadence: "hourly", Interval: interval, WorkHours: checked, Until: "2026-11-10T16:59"}
					c, err := NewCalendar(testID, s, now)
					if err != nil {
						t.Fatal(err)
					}
					raw, _ := Encode(c)
					if strings.Contains(raw, "FREQ=HOURLY") {
						t.Fatal("unsupported native recurrence emitted")
					}
					decoded, err := ical.NewDecoder(strings.NewReader(raw)).Decode()
					if err != nil {
						t.Fatal(err)
					}
					e := decoded.Events()[0]
					read, why := readSchedule(e)
					if read == nil || *read != s {
						t.Fatalf("roundtrip %+v %s\n%s", read, why, raw)
					}
					var hours []string
					for h := 0; h < 24; h++ {
						if checked && (h < 9 || h >= 17) {
							continue
						}
						if interval == 0.5 || (h+24-9)%int(interval) == 0 {
							hours = append(hours, strconv.Itoa(h))
						}
					}
					minutes := strconv.Itoa(minute)
					if interval == 0.5 {
						minutes = fmt.Sprintf("%d,%d", minute%30, minute%30+30)
					}
					want := "FREQ=DAILY;INTERVAL=1;BYHOUR=" + strings.Join(hours, ",") + ";BYMINUTE=" + minutes
					if checked {
						want += ";BYDAY=" + workDays
					}
					want += ";UNTIL=20261110T055900Z"
					if rule := e.Props.Get("RRULE").Value; rule != want {
						t.Fatalf("got %s want %s", rule, want)
					}
					// Independent bounded library expansion of the DAILY representation.
					start, _, _ := s.Validate()
					opt, err := rrule.StrToROptionInLocation(want, start.Location())
					if err != nil {
						t.Fatal(err)
					}
					opt.Dtstart = start
					r, err := rrule.NewRRule(*opt)
					if err != nil {
						t.Fatal(err)
					}
					for _, atWall := range []string{s.Start, "2026-11-06T17:00", "2026-11-09T09:01"} {
						at, _ := WallTime(atWall)
						got := resourceTiming(decoded.Events(), at)
						wantNext := r.After(at, true)
						if got.Next == nil || !got.Next.Equal(wantNext) {
							t.Fatalf("%s next=%+v library=%s", atWall, got, wantNext)
						}
					}
				})
			}
		}
	}
}

func TestHourlyIntervalsAreExplicitAndOtherCadencesStayInteger(t *testing.T) {
	s := Schedule{Title: "Bounds", Start: "2026-11-06T09:00", TimeZone: Zone, Cadence: "hourly", Interval: 1}
	for _, interval := range []float64{0, 0.6, 5, 7, 999, math.NaN(), math.Inf(1), math.Inf(-1)} {
		s.Interval = interval
		if _, _, err := s.Validate(); err == nil {
			t.Fatal("unsupported hourly interval", interval)
		}
	}
	for _, cadence := range []string{"once", "daily", "weekdays", "weekly", "monthly", "yearly"} {
		s.Cadence = cadence
		for _, interval := range []float64{0.5, 1.5, math.NaN(), math.Inf(1)} {
			s.Interval = interval
			if _, _, err := s.Validate(); err == nil {
				t.Fatal("fractional/nonfinite interval", cadence, interval)
			}
		}
	}
}

func TestHourlyMetadataAndSlotsFailClosed(t *testing.T) {
	s := Schedule{Title: "Strict", Start: "2026-11-06T09:17", TimeZone: Zone, Cadence: "hourly", Interval: 2, WorkHours: true}
	c, _ := NewCalendar(testID, s, now)
	base := c.Events()[0]
	rule := base.Props.Get("RRULE").Value
	for _, malformed := range []string{
		strings.Replace(rule, "FREQ=DAILY", "FREQ=HOURLY", 1),
		strings.Replace(rule, "INTERVAL=1", "INTERVAL=2", 1),
		strings.Replace(rule, "BYMINUTE=17", "BYMINUTE=17,47", 1),
		strings.Replace(rule, "BYMINUTE=17", "BYMINUTE=18", 1),
		strings.Replace(rule, "BYHOUR=9,11,13,15", "BYHOUR=9", 1),
		strings.Replace(rule, ";BYDAY="+workDays, "", 1),
		rule + ";BYSECOND=0", rule + ";COUNT=3", rule + ";BYMINUTE=17", rule + ";BYSETPOS=1",
	} {
		e := ical.Event{Component: cloneComponent(base.Component)}
		e.Props.Get("RRULE").Value = malformed
		if got, _ := readSchedule(e); got != nil {
			t.Fatal("malformed rule enabled editing", malformed)
		}
		if got := resourceTiming([]ical.Event{e}, now); got.State != "unknown" {
			t.Fatal("malformed slots enabled timing", malformed, got)
		}
	}
	for _, desc := range []string{
		"[docket]", hourlyMarker(2) + hourlyMarker(2), "[chainmail:hourly:v2 hours=2]",
		"[chainmail:hourly:v1 hours=0.6]", "[chainmail:hourly:v1 hours=5]",
		"[chainmail:hourly:v1 hours=2.0]", hourlyMarker(3),
	} {
		e := ical.Event{Component: cloneComponent(base.Component)}
		e.Props.SetText("DESCRIPTION", desc+"\n"+workHoursMarker)
		if got, _ := readSchedule(e); got != nil {
			t.Fatal("bad metadata enabled editing", desc)
		}
		if got := resourceTiming([]ical.Event{e}, now); got.State != "unknown" {
			t.Fatal("bad metadata enabled timing", desc, got)
		}
	}
	e := ical.Event{Component: cloneComponent(base.Component)}
	e.Props.Del("RRULE")
	if got, _ := readSchedule(e); got != nil {
		t.Fatal("hourly marker without rule enabled editing")
	}
	e = ical.Event{Component: cloneComponent(base.Component)}
	e.Props["DESCRIPTION"] = append([]ical.Prop{{Name: "DESCRIPTION", Value: "[docket]", Params: make(ical.Params)}}, e.Props["DESCRIPTION"]...)
	if got, _ := readSchedule(e); got != nil {
		t.Fatal("metadata in a second DESCRIPTION enabled editing")
	}
	e = ical.Event{Component: cloneComponent(base.Component)}
	e.Props.SetText("DESCRIPTION", "[docket]")
	e.Props.Get("RRULE").Value = "FREQ=HOURLY;INTERVAL=1"
	if got, _ := readSchedule(e); got != nil {
		t.Fatal("native imported HOURLY enabled editing")
	}
	if got := resourceTiming([]ical.Event{e}, now); got.State != "unknown" {
		t.Fatal("native imported HOURLY enabled timing")
	}
}

func TestHourlyAllChoicesRetryEditAndCompletion(t *testing.T) {
	for _, interval := range hourlyIntervals {
		for _, checked := range []bool{false, true} {
			s := Schedule{Title: "Persist", Start: "2026-11-06T16:45", TimeZone: Zone, Cadence: "hourly", Interval: interval, WorkHours: checked}
			c, _ := NewCalendar(testID, s, now)
			f := &completionBackend{objects: []caldav.CalendarObject{{Path: testCollection + "hassle-" + testID + ".ics", ETag: `"1"`, Data: c}}}
			in := Mutation{Location: Location{CalendarID: testCollection}, Action: "create", ID: testID, Schedule: s}
			if result, err := Mutate(context.Background(), f, in, now); !errors.Is(err, ErrConflict) || len(f.writes) != 0 {
				t.Fatalf("retry %g/%v: %+v %v", interval, checked, result, err)
			}
			// Title-only edits retain all original recurrence and description bytes.
			e := f.objects[0].Data.Events()[0]
			beforeRule, beforeDesc := *e.Props.Get("RRULE"), *e.Props.Get("DESCRIPTION")
			s.Title = "Renamed"
			if err := ApplySchedule(e, s, now); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(*e.Props.Get("RRULE"), beforeRule) || !reflect.DeepEqual(*e.Props.Get("DESCRIPTION"), beforeDesc) {
				t.Fatal("title edit changed recurrence/metadata")
			}
			input := completionInput(t, f)
			input.DryRun = ptr(false)
			if _, err := Mutate(context.Background(), f, input, now); err != nil {
				t.Fatal(err)
			}
			task := inventory(t, f.objects...)[0]
			if task.Schedule == nil || *task.Schedule != s || task.Instance == nil || task.Instance.At == *input.InstanceAt {
				t.Fatalf("completed %g/%v: %+v", interval, checked, task)
			}
			// Edit back through the API path, preserving the exact EXDATE identity.
			beforeEx := append([]ical.Prop(nil), f.objects[0].Data.Events()[0].Props["EXDATE"]...)
			s.WorkHours = !checked
			edit := Mutation{Location: in.Location, Action: "edit", ID: task.ID, Revision: task.Revision, Schedule: s}
			if _, err := Mutate(context.Background(), f, edit, now); err != nil {
				t.Fatal(err)
			}
			task = inventory(t, f.objects...)[0]
			if task.Schedule == nil || *task.Schedule != s || !reflect.DeepEqual(f.objects[0].Data.Events()[0].Props["EXDATE"], beforeEx) {
				t.Fatal("edit lost checkbox or exclusion")
			}
		}
	}
}

func TestHourlyMetadataTransitionsPreserveRawDescription(t *testing.T) {
	s := Schedule{Title: "Literal", Start: "2026-11-06T09:17", TimeZone: Zone, Cadence: "hourly", Interval: 0.5, WorkHours: true}
	c, _ := NewCalendar(testID, s, now)
	e := c.Events()[0]
	p := e.Props.Get("DESCRIPTION")
	p.Value = `Human\Nescaped\,notes\\kept\N[docket]\N` + completionMarker + `\N` + workHoursMarker + `\N` + hourlyMarker(0.5) + `\NTail`
	p.Params = ical.Params{"LANGUAGE": {"en-AU"}, "X-UNKNOWN": {"retained"}}
	before := p.Value
	s.Interval = 24
	if err := ApplySchedule(e, s, now); err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(before, hourlyMarker(0.5), "", 1) + `\n` + hourlyMarker(24)
	if p.Value != want || p.Params.Get("LANGUAGE") != "en-AU" || p.Params.Get("X-UNKNOWN") != "retained" {
		t.Fatal("hourly interval edit rewrote raw notes/parameters", p)
	}
	s.Cadence, s.Interval = "daily", 3
	if err := ApplySchedule(e, s, now); err != nil {
		t.Fatal(err)
	}
	if p.Value != strings.Replace(want, hourlyMarker(24), "", 1) {
		t.Fatal("leaving hourly did not remove only its literal marker")
	}
	if read, why := readSchedule(e); read == nil || *read != s {
		t.Fatalf("daily transition: %+v %s", read, why)
	}
}

func TestHalfHourlyRepeatedWallTimesUseFirstOccurrence(t *testing.T) {
	s := Schedule{Title: "Half-hour fold", Start: "2026-04-04T01:30", TimeZone: Zone, Cadence: "hourly", Interval: 0.5}
	c, _ := NewCalendar(testID, s, now)
	for _, tc := range []struct{ at, next string }{
		{"2026-04-04T15:15:00Z", "2026-04-04T15:30:00Z"},
		{"2026-04-04T15:45:00Z", "2026-04-04T17:00:00Z"},
		{"2026-04-04T16:15:00Z", "2026-04-04T17:00:00Z"},
	} {
		at, _ := time.Parse(time.RFC3339, tc.at)
		got := resourceTiming(c.Events(), at)
		if got.Next == nil || got.Next.Format(time.RFC3339) != tc.next {
			t.Fatalf("repeated half-hour %s: %+v", tc.at, got)
		}
	}
}

func TestHalfHourlyWeekendsDSTUntilAndOldSeries(t *testing.T) {
	for _, tc := range []struct {
		start, at, next string
		checked         bool
	}{
		{"2026-11-06T16:45", "2026-11-06T17:00", "2026-11-09T09:15", true},
		{"2026-11-06T23:59", "2026-11-07T00:00", "2026-11-07T00:29", false},
		{"2026-10-03T01:45", "2026-10-04T01:50", "2026-10-04T03:15", false},
		{"2026-04-04T01:45", "2026-04-05T03:00", "2026-04-05T03:15", false},
		{"1970-01-01T09:17", "2026-11-06T16:50", "2026-11-09T09:17", true},
	} {
		s := Schedule{Title: "Half hour", Start: tc.start, TimeZone: Zone, Cadence: "hourly", Interval: 0.5, WorkHours: tc.checked}
		if got := workTiming(t, s, tc.at); nextWall(got) != tc.next {
			t.Fatalf("%+v next=%s", tc, nextWall(got))
		}
	}
	s := Schedule{Title: "Inclusive", Start: "2026-11-06T16:45", TimeZone: Zone, Cadence: "hourly", Interval: 0.5, WorkHours: true, Until: "2026-11-09T09:15"}
	if got := workTiming(t, s, s.Until); nextWall(got) != s.Until {
		t.Fatal("inclusive half-hour end", got)
	}
	if got := workTiming(t, s, "2026-11-09T09:16"); got.Next != nil || got.State != "past" || got.Last == nil {
		t.Fatal("ended half-hour series", got)
	}
}
