package hassles

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-ical"
	"github.com/emersion/go-webdav/caldav"
)

type completionBackend struct {
	objects []caldav.CalendarObject
	writes  []adoptionWrite
	fail    bool
}

func (f *completionBackend) Calendars(context.Context) ([]Calendar, error) { return nil, nil }
func (f *completionBackend) Objects(context.Context, string) ([]caldav.CalendarObject, error) {
	return f.objects, nil
}
func (f *completionBackend) Write(_ context.Context, collection, path, etag, body string) error {
	f.writes = append(f.writes, adoptionWrite{collection, path, etag, body})
	if f.fail {
		return ErrConflict
	}
	for i, o := range f.objects {
		if o.Path != path {
			continue
		}
		if o.ETag != etag {
			return ErrConflict
		}
		if body == "" {
			f.objects = append(f.objects[:i], f.objects[i+1:]...)
			return nil
		}
		c, err := ical.NewDecoder(strings.NewReader(body)).Decode()
		if err != nil {
			return err
		}
		f.objects[i].Data = c
		f.objects[i].ETag = o.ETag + "next"
		return nil
	}
	return ErrConflict
}
func completionInput(t *testing.T, f *completionBackend) Mutation {
	t.Helper()
	task := inventory(t, f.objects...)[0]
	if task.Instance == nil {
		t.Fatalf("instance unavailable: %+v", task)
	}
	return Mutation{Location: Location{CalendarID: testCollection}, Action: "done-instance", ID: task.ID, Revision: task.Revision, InstanceAt: ptr(task.Instance.At), InstanceToken: ptr(task.Instance.Token), DryRun: ptr(true)}
}
func TestInstanceOutsideCompletionYearProfileFailsWithoutWrite(t *testing.T) {
	for _, test := range []struct {
		name, start, cadence string
		interval             int
	}{
		{"derived future", "1970-06-01T08:30", "yearly", 200},
		{"UTC lower boundary", "1970-01-01T00:30", "once", 1},
		{"local upper boundary", "1970-01-01T00:30", "yearly", 131},
		{"derived DST overlap", "2026-04-04T02:30", "yearly", 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			s := sample()
			s.Start, s.Cadence, s.Interval, s.Until = test.start, test.cadence, test.interval, ""
			calendar, err := NewCalendar(testID, s, now)
			if err != nil {
				t.Fatal(err)
			}
			o := caldav.CalendarObject{Path: testCollection + "range.ics", ETag: `"v1"`, Data: calendar}
			task := inventory(t, o)[0]
			if task.Schedule == nil || task.Instance != nil {
				t.Fatalf("editability/witness: %+v", task)
			}
			at := task.Timing.Next
			if at == nil {
				at = task.Timing.Last
			}
			if at == nil {
				t.Fatal("expected a known out-of-profile occurrence")
			}
			instant := at.UTC().Format(time.RFC3339)
			uid, _ := calendar.Events()[0].Props.Text("UID")
			f := &completionBackend{objects: []caldav.CalendarObject{o}}
			_, err = Mutate(context.Background(), f, Mutation{Location: Location{CalendarID: testCollection}, Action: "done-instance", ID: task.ID, Revision: task.Revision, InstanceAt: ptr(instant), InstanceToken: ptr(instanceToken(o.Path, uid, instant)), DryRun: ptr(false)}, now)
			if !errors.Is(err, ErrConflict) || len(f.writes) != 0 || inventory(t, f.objects...)[0].Schedule == nil {
				t.Fatalf("out-of-profile completion changed reminder: %v, %v", err, f.writes)
			}
		})
	}
}

func TestCompletionFractionalTimestampSyntaxRejected(t *testing.T) {
	for _, tzid := range []string{"", Zone} {
		for _, fraction := range []string{".0", ".5"} {
			o := object(t, testCollection+"fraction.ics")
			e := o.Data.Events()[0]
			e.Props.SetText("DESCRIPTION", "[docket]\\n"+completionMarker)
			value := "20270104T121700" + fraction
			params := make(ical.Params)
			if tzid == "" {
				value += "Z"
			} else {
				params["TZID"] = []string{tzid}
			}
			e.Props["EXDATE"] = []ical.Prop{{Name: "EXDATE", Value: value, Params: params}}
			if validCompletionDates(e) {
				t.Fatalf("accepted fractional compact syntax: %s %s", tzid, value)
			}
		}
	}
}

func TestGoogleNormalizedCompletionRetainsEditingAndNextWitness(t *testing.T) {
	for _, explicitValue := range []bool{false, true} {
		t.Run(map[bool]string{false: "TZID", true: "TZID and VALUE"}[explicitValue], func(t *testing.T) {
			f := &completionBackend{objects: []caldav.CalendarObject{object(t, testCollection+"google.ics")}}
			in := completionInput(t, f)
			in.DryRun = ptr(false)
			if _, err := Mutate(context.Background(), f, in, now); err != nil {
				t.Fatal(err)
			}
			at, _ := time.Parse(time.RFC3339, *in.InstanceAt)
			loc, _ := time.LoadLocation(Zone)
			e := f.objects[0].Data.Events()[0]
			p := e.Props.Get("EXDATE")
			p.Value = at.In(loc).Format("20060102T150405")
			p.Params = ical.Params{"TZID": []string{Zone}}
			if explicitValue {
				p.Params["VALUE"] = []string{"DATE-TIME"}
			}
			f.objects[0].ETag += "google"
			task := inventory(t, f.objects...)[0]
			if task.Schedule == nil || task.Instance == nil || task.Instance.At == *in.InstanceAt || !validCompletionDates(e) {
				t.Fatalf("provider normalization lost editing/next witness: %+v", task)
			}
			before := snapshot(t, e.Props["EXDATE"])
			s := *task.Schedule
			s.Title = "Renamed after Google normalization"
			if _, err := Mutate(context.Background(), f, Mutation{Location: Location{CalendarID: testCollection}, Action: "edit", ID: task.ID, Revision: task.Revision, Schedule: s}, now); err != nil {
				t.Fatal(err)
			}
			if snapshot(t, f.objects[0].Data.Events()[0].Props["EXDATE"]) != before {
				t.Fatal("title edit changed provider-normalized exclusion")
			}
			next := completionInput(t, f)
			if _, err := Mutate(context.Background(), f, next, now); err != nil {
				t.Fatal("next preview rejected normalized exclusion", err)
			}
		})
	}
}

func TestInstancePreviewCommitPreservesAndRemainsEditable(t *testing.T) {
	o := object(t, testCollection+"a.ics")
	e := o.Data.Events()[0]
	e.Props.SetText("X-UNKNOWN", "keep")
	e.Props.Get("DESCRIPTION").Params["LANGUAGE"] = []string{"en-AU"}
	f := &completionBackend{objects: []caldav.CalendarObject{o}}
	original := snapshot(t, o.Data)
	in := completionInput(t, f)
	preview, err := Mutate(context.Background(), f, in, now)
	if err != nil || len(f.writes) != 0 || original != snapshot(t, o.Data) || preview.Completion == nil || preview.Completion.Operation != "exclude-occurrence" {
		t.Fatalf("preview %+v %v", preview, err)
	}
	in.DryRun = ptr(false)
	result, err := Mutate(context.Background(), f, in, now)
	if err != nil || result.Removed != 0 || !reflect.DeepEqual(result.Completion, preview.Completion) || len(f.writes) != 1 {
		t.Fatalf("commit %+v %v", result, err)
	}
	if f.writes[0].collection != testCollection || f.writes[0].path != o.Path || f.writes[0].etag != o.ETag || original != snapshot(t, o.Data) {
		t.Fatal("conditional target or input changed")
	}
	after := f.objects[0].Data.Events()[0]
	for _, name := range []string{"UID", "DTSTART", "DTEND", "RRULE", "X-UNKNOWN"} {
		if !reflect.DeepEqual(e.Props[name], after.Props[name]) {
			t.Fatal("lost", name)
		}
	}
	if !reflect.DeepEqual(e.Children, after.Children) || !reflect.DeepEqual(e.Props.Get("DESCRIPTION").Params, after.Props.Get("DESCRIPTION").Params) {
		t.Fatal("lost children/params")
	}
	at, _ := time.Parse(time.RFC3339, *in.InstanceAt)
	if after.Props.Get("EXDATE").Value != at.Format("20060102T150405Z") {
		t.Fatal("wrong exclusion")
	}
	task := inventory(t, f.objects...)[0]
	if task.Schedule == nil || task.Instance == nil || task.Instance.At == *in.InstanceAt {
		t.Fatalf("lost editability/next %+v", task)
	}
	if _, err = Mutate(context.Background(), f, in, now); !errors.Is(err, ErrConflict) || len(f.writes) != 1 {
		t.Fatal("retry completed another instance", err)
	}
	exclusions := snapshot(t, after.Props["EXDATE"])
	s := *task.Schedule
	s.Title = "Edited title"
	if err = ApplySchedule(after, s, now); err != nil || snapshot(t, after.Props["EXDATE"]) != exclusions {
		t.Fatal("title edit lost exclusions", err)
	}
	s.Start = "2026-12-01T08:30"
	if err = ApplySchedule(after, s, now); err != nil || snapshot(t, after.Props["EXDATE"]) != exclusions {
		t.Fatal("timing edit remapped exclusions", err)
	}
}
func TestInstanceStaleClockWrongWitnessAndFailureZeroMutation(t *testing.T) {
	for _, mode := range []string{"revision", "token", "at", "clock", "conditional"} {
		t.Run(mode, func(t *testing.T) {
			o := object(t, testCollection+"a.ics")
			f := &completionBackend{objects: []caldav.CalendarObject{o}}
			in := completionInput(t, f)
			in.DryRun = ptr(false)
			original := snapshot(t, o.Data)
			clock := now
			switch mode {
			case "revision":
				in.Revision = strings.Repeat("0", 64)
			case "token":
				in.InstanceToken = ptr(strings.Repeat("0", 64))
			case "at":
				in.InstanceAt = ptr("2026-11-06T00:00:00Z")
			case "clock":
				at, _ := time.Parse(time.RFC3339, *in.InstanceAt)
				clock = at.Add(time.Second)
			case "conditional":
				f.fail = true
			}
			_, err := Mutate(context.Background(), f, in, clock)
			expected := 0
			if mode == "conditional" {
				expected = 1
			}
			if !errors.Is(err, ErrConflict) || len(f.writes) != expected || snapshot(t, o.Data) != original {
				t.Fatalf("unsafe failure %v writes %d", err, len(f.writes))
			}
		})
	}
}
func TestInstanceOneOffAndMultiResourceSelection(t *testing.T) {
	for _, mode := range []string{"once", "unique", "tied", "unknown", "ended"} {
		t.Run(mode, func(t *testing.T) {
			a := object(t, testCollection+"a.ics")
			a.Data.Events()[0].Props.Del("RRULE")
			objects := []caldav.CalendarObject{a}
			if mode == "ended" {
				s := sample()
				s.Start = "2026-11-01T08:30"
				s.Cadence = "once"
				s.Interval = 1
				if err := ApplySchedule(a.Data.Events()[0], s, now); err != nil {
					t.Fatal(err)
				}
			}
			if mode != "once" && mode != "ended" {
				b := object(t, testCollection+"b.ics")
				if mode == "unique" {
					s := sample()
					s.Start = "2026-11-08T08:30"
					if err := ApplySchedule(b.Data.Events()[0], s, now); err != nil {
						t.Fatal(err)
					}
				}
				if mode == "unknown" {
					b.Data.Events()[0].Props.Get("RRULE").Value = "FREQ=HOURLY"
				}
				objects = append(objects, b)
			}
			f := &completionBackend{objects: objects}
			task := inventory(t, objects...)[0]
			if mode == "tied" || mode == "unknown" {
				if task.Instance != nil || task.InstanceRestriction == "" || !task.Writable {
					t.Fatalf("ambiguous %+v", task)
				}
				return
			}
			in := completionInput(t, f)
			in.DryRun = ptr(false)
			result, err := Mutate(context.Background(), f, in, now)
			if err != nil || result.Removed != 1 || len(f.writes) != 1 || f.writes[0].path != a.Path || f.writes[0].body != "" || len(f.objects) != len(objects)-1 {
				t.Fatalf("delete %+v %v", result, err)
			}
		})
	}
}
func TestInstanceRestrictionsAndCompletionProvenance(t *testing.T) {
	for _, mode := range []string{"unowned", "participant", "weak", "exdate", "rdate", "count", "timezone", "bad-marker", "bad-date", "date-params", "duplicate-date", "too-many"} {
		t.Run(mode, func(t *testing.T) {
			o := object(t, "/a.ics")
			e := o.Data.Events()[0]
			switch mode {
			case "unowned":
				e.Props.SetText("DESCRIPTION", "[hassle:v1 task="+testID+"]")
			case "participant":
				e.Props.SetText("ATTENDEE", "mailto:offline@example.test")
			case "weak":
				o.ETag = `W/"x"`
			case "exdate":
				e.Props["EXDATE"] = []ical.Prop{{Name: "EXDATE", Value: "20261106T000000Z"}}
			case "rdate":
				e.Props["RDATE"] = []ical.Prop{{Name: "RDATE", Value: "20261106T000000Z"}}
			case "count":
				e.Props.Get("RRULE").Value = "FREQ=DAILY;COUNT=3"
			case "timezone":
				e.Props.Get("DTSTART").Params.Set("TZID", "Europe/London")
			default:
				p := e.Props.Get("DESCRIPTION")
				p.Value += `\n` + completionMarker
				e.Props["EXDATE"] = []ical.Prop{{Name: "EXDATE", Value: "20261106T000000Z"}}
				switch mode {
				case "bad-marker":
					p.Value += completionMarker
				case "bad-date":
					e.Props["EXDATE"][0].Value = "not-a-date"
				case "date-params":
					e.Props["EXDATE"][0].Params = ical.Params{"TZID": []string{Zone}}
				case "duplicate-date":
					e.Props["EXDATE"][0].Value += ",20261106T000000Z"
				case "too-many":
					for i := 0; i <= maxCompletions; i++ {
						e.Props["EXDATE"] = append(e.Props["EXDATE"], ical.Prop{Name: "EXDATE", Value: now.Add(time.Duration(i) * time.Hour).Format("20060102T150405Z")})
					}
				}
			}
			task := inventory(t, o)[0]
			if task.Instance != nil || task.InstanceRestriction == "" {
				t.Fatalf("unsupported enabled %+v", task)
			}
			if mode != "unowned" && mode != "participant" && mode != "weak" && task.Schedule != nil {
				t.Fatal("unsupported edit enabled")
			}
		})
	}
}
func TestInstanceFieldsStrict(t *testing.T) {
	for _, action := range []string{"create", "edit", "delete", "done", "adopt"} {
		for _, field := range []string{"instanceAt", "instanceToken"} {
			for _, value := range []string{`null`, `"2026-11-05T00:00:00Z"`} {
				var in Mutation
				err := json.Unmarshal([]byte(`{"action":"`+action+`","`+field+`":`+value+`}`), &in)
				if err == nil {
					err = ValidateAdoptionFields(in)
				}
				if err == nil {
					t.Fatalf("ignored %s %s %s", action, field, value)
				}
			}
		}
	}
	f := &completionBackend{objects: []caldav.CalendarObject{object(t, "/a.ics")}}
	for _, field := range []string{"instanceAt", "instanceToken", "dryRun", "revision"} {
		in := completionInput(t, f)
		switch field {
		case "instanceAt":
			in.InstanceAt = nil
		case "instanceToken":
			in.InstanceToken = nil
		case "dryRun":
			in.DryRun = nil
		case "revision":
			in.Revision = ""
		}
		if ValidateAdoptionFields(in) == nil {
			t.Fatal("missing", field)
		}
	}
}

func TestInstanceRepeatedCompletionGoogleProfileAndGroupPreservation(t *testing.T) {
	a := withTimezone(t, googleTimezoneFixture(t))
	b := object(t, testCollection+"later.ics")
	s := sample()
	s.Start = "2026-12-01T08:30"
	if err := ApplySchedule(b.Data.Events()[0], s, now); err != nil {
		t.Fatal(err)
	}
	f := &completionBackend{objects: []caldav.CalendarObject{a, b}}
	otherBefore := snapshot(t, b)
	zoneBefore := snapshot(t, a.Data.Children[0])
	first := completionInput(t, f)
	first.DryRun = ptr(false)
	if _, err := Mutate(context.Background(), f, first, now); err != nil {
		t.Fatal(err)
	}
	oldDates := snapshot(t, f.objects[0].Data.Events()[0].Props["EXDATE"])
	second := completionInput(t, f)
	second.DryRun = ptr(false)
	if _, err := Mutate(context.Background(), f, second, now); err != nil {
		t.Fatal(err)
	}
	dates := f.objects[0].Data.Events()[0].Props["EXDATE"]
	if len(dates) != 2 || snapshot(t, dates[:1]) != oldDates || snapshot(t, f.objects[1]) != otherBefore || snapshot(t, f.objects[0].Data.Children[0]) != zoneBefore {
		t.Fatal("lost previous completion/other member/timezone")
	}
	task := inventory(t, f.objects[0])[0]
	if task.Schedule == nil || task.Instance == nil {
		t.Fatal("Google schedule lost editing", task.Restriction)
	}
	s = *task.Schedule
	s.Title = "After two completions"
	if _, err := Mutate(context.Background(), f, Mutation{Location: Location{CalendarID: testCollection}, Action: "edit", ID: task.ID, Revision: inventory(t, f.objects...)[0].Revision, Schedule: s}, now); err == nil {
		t.Fatal("multi-resource edit was incorrectly enabled")
	}
	// A single-resource title edit retains exact original exclusions and timezone.
	f.objects = f.objects[:1]
	task = inventory(t, f.objects...)[0]
	if _, err := Mutate(context.Background(), f, Mutation{Location: Location{CalendarID: testCollection}, Action: "edit", ID: task.ID, Revision: task.Revision, Schedule: s}, now); err != nil {
		t.Fatal(err)
	}
	if snapshot(t, f.objects[0].Data.Events()[0].Props["EXDATE"]) != snapshot(t, dates) || snapshot(t, f.objects[0].Data.Children[0]) != zoneBefore {
		t.Fatal("edit lost completion/timezone")
	}
}
