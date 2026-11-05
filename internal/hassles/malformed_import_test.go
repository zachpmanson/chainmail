package hassles

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-ical"
	"github.com/emersion/go-webdav/caldav"
)

func decodedImport(t *testing.T, c *ical.Calendar) *ical.Calendar {
	t.Helper()
	raw, err := Encode(c)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := ical.NewDecoder(strings.NewReader(raw)).Decode()
	if err != nil {
		t.Fatal(err)
	}
	return decoded
}

func TestMetadataRejectsNonScalarRawDescription(t *testing.T) {
	for _, tc := range []struct {
		name, suffix, valueType string
	}{
		{"hidden unknown hourly", ",[chainmail:hourly:v2 hours=3]", ""},
		{"hidden duplicate hourly", "," + hourlyMarker(2), ""},
		{"hidden unknown work hours", ",[chainmail:work-hours:v2]", ""},
		{"hidden duplicate work hours", "," + workHoursMarker, ""},
		{"invalid escape", `\q`, ""},
		{"trailing escape", `\`, ""},
		{"non-text type", "", "URI"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := Schedule{Title: "Strict import", Start: "2026-11-06T09:17", TimeZone: Zone, Cadence: "hourly", Interval: 2, WorkHours: true}
			c, err := NewCalendar(testID, s, now)
			if err != nil {
				t.Fatal(err)
			}
			p := c.Events()[0].Props.Get("DESCRIPTION")
			p.Value += tc.suffix
			if tc.valueType != "" {
				p.Params["VALUE"] = []string{tc.valueType}
			}
			e := decodedImport(t, c).Events()[0]
			before := cloneComponent(e.Component)
			if readScheduleMetadata(e, &Schedule{}) {
				t.Fatal("malformed scalar description accepted")
			}
			if s, why := readSchedule(e); s != nil || why == "" {
				t.Fatalf("malformed description enabled editing: %+v %q", s, why)
			}
			if got := resourceTiming([]ical.Event{e}, now); got.State != "unknown" {
				t.Fatalf("malformed hourly metadata enabled timing: %+v", got)
			}
			if !reflect.DeepEqual(e.Component, before) {
				t.Fatal("validation changed raw event properties")
			}
		})
	}
	// The shared gate must also reject a hidden work-hours marker on a
	// schedule that does not carry any hourly metadata.
	c, _ := NewCalendar(testID, sample(), now)
	c.Events()[0].Props.Get("DESCRIPTION").Value += "," + workHoursMarker
	if s, _ := readSchedule(decodedImport(t, c).Events()[0]); s != nil {
		t.Fatal("hidden work-hours intent accepted on a non-hourly schedule")
	}
}

func TestMetadataEscapedCommaPreservedAndDoesNotGrantOwnership(t *testing.T) {
	s := Schedule{Title: "Notes", Start: "2026-11-06T09:17", TimeZone: Zone, Cadence: "hourly", Interval: 2, WorkHours: true}
	c, _ := NewCalendar(testID, s, now)
	p := c.Events()[0].Props.Get("DESCRIPTION")
	p.Value = `Human\, notes\N` + p.Value + `\NMore\\notes`
	p.Params = ical.Params{"LANGUAGE": {"en-AU"}, "X-UNKNOWN": {"retained"}}
	c = decodedImport(t, c)
	e := c.Events()[0]
	before := *e.Props.Get("DESCRIPTION")
	if got, why := readSchedule(e); got == nil || *got != s {
		t.Fatalf("legitimate escaped comma rejected: %+v %q", got, why)
	}
	s.Title = "Title only"
	if err := ApplySchedule(e, s, now); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(*e.Props.Get("DESCRIPTION"), before) {
		t.Fatal("title edit changed escaped scalar description")
	}
	s.Interval = 3
	if err := ApplySchedule(e, s, now); err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(before.Value, hourlyMarker(2), "", 1) + `\n` + hourlyMarker(3)
	if e.Props.Get("DESCRIPTION").Value != want || !reflect.DeepEqual(e.Props.Get("DESCRIPTION").Params, before.Params) {
		t.Fatal("interval edit changed raw human notes or parameters")
	}
	if got, why := readSchedule(e); got == nil || *got != s {
		t.Fatalf("valid edit lost recognition: %+v %q", got, why)
	}
	e.Props.SetText("DESCRIPTION", "Human, notes\n"+hourlyMarker(3)+"\n"+workHoursMarker)
	tasks := inventory(t, caldav.CalendarObject{Path: "/unowned.ics", ETag: `"v1"`, Data: c})
	if len(tasks) != 1 || tasks[0].Writable {
		t.Fatal("cadence metadata granted ownership")
	}
}

func TestImportedMelbourneStartRejectsRawGapAndOverlap(t *testing.T) {
	for _, date := range []string{"2026-10-04", "2026-04-05"} {
		for _, minute := range []int{0, 30, 45} {
			t.Run(fmt.Sprintf("%s/02:%02d", date, minute), func(t *testing.T) {
				s := Schedule{Title: "Raw time", Start: fmt.Sprintf("%sT03:%02d", date, minute), TimeZone: Zone, Cadence: "hourly", Interval: 0.5}
				c, err := NewCalendar(testID, s, now)
				if err != nil {
					t.Fatal(err)
				}
				raw, _ := time.Parse("2006-01-02T15:04", fmt.Sprintf("%sT02:%02d", date, minute))
				e := c.Events()[0]
				e.Props.Get("DTSTART").Value = raw.Format("20060102T150405")
				e.Props.Get("DTEND").Value = raw.Add(5 * time.Minute).Format("20060102T150405")
				e = decodedImport(t, c).Events()[0]
				before := cloneComponent(e.Component)
				if got, why := readSchedule(e); got != nil || why == "" {
					t.Fatalf("invalid raw start enabled editing: %+v %q", got, why)
				}
				parsed, _ := e.DateTimeStart(nil)
				if timingProfile(e, e.Props.Get("RRULE"), parsed) != nil {
					t.Fatal("invalid raw start admitted as a timing profile")
				}
				if got := resourceTiming([]ical.Event{e}, now); got.State != "unknown" || got.Note == "" {
					t.Fatalf("invalid raw start normalized into known timing: %+v", got)
				}
				if !reflect.DeepEqual(e.Component, before) {
					t.Fatal("raw-start validation changed the event")
				}
			})
		}
	}
}

func TestImportedMelbourneValidNeighborsAndMissingDescriptionTiming(t *testing.T) {
	for _, start := range []string{"2026-10-04T01:30", "2026-10-04T03:30", "2026-04-05T01:30", "2026-04-05T03:30"} {
		s := Schedule{Title: "Valid neighbor", Start: start, TimeZone: Zone, Cadence: "hourly", Interval: 0.5}
		c, err := NewCalendar(testID, s, now)
		if err != nil {
			t.Fatal(err)
		}
		e := decodedImport(t, c).Events()[0]
		if got, why := readSchedule(e); got == nil || *got != s {
			t.Fatalf("valid neighboring start rejected: %+v %q", got, why)
		}
		if got := resourceTiming([]ical.Event{e}, now); got.State != "upcoming" || got.Next == nil {
			t.Fatalf("valid neighboring timing rejected: %+v", got)
		}
	}
	c, _ := NewCalendar(testID, sample(), now)
	e := c.Events()[0]
	e.Props.Del("DESCRIPTION")
	if got := resourceTiming([]ical.Event{e}, now); got.State != "upcoming" {
		t.Fatal("missing description changed legitimate generic timing", got)
	}
	// The raw-start guard must not make generic second-precision timing
	// subject to the editor's separate minute-only restriction.
	e.Props.Get("DTSTART").Value = "20261105T083030"
	e.Props.Get("DTEND").Value = "20261105T083530"
	if got := resourceTiming([]ical.Event{e}, now); got.State != "upcoming" || got.Next == nil || got.Next.Second() != 30 {
		t.Fatal("valid generic seconds lost timing", got)
	}
}
