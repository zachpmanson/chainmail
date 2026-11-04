package hassles

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-ical"
	"github.com/emersion/go-webdav/caldav"
)

const testID = "e3ae442f-5498-4cd6-a362-312ba81b9123"
const testCollection = "/caldav/v2/reminders%40example.test/events/"

var now = time.Date(2026, 11, 4, 0, 0, 0, 0, time.UTC)

func sample() Schedule {
	return Schedule{Title: "Pay bill", Start: "2026-11-05T08:30", TimeZone: Zone, Cadence: "daily", Interval: 2}
}
func object(t *testing.T, href string) caldav.CalendarObject {
	t.Helper()
	c, err := NewCalendar(testID, sample(), now)
	if err != nil {
		t.Fatal(err)
	}
	return caldav.CalendarObject{Path: href, ETag: `"v1"`, Data: c}
}
func inventory(t *testing.T, objects ...caldav.CalendarObject) []Task {
	t.Helper()
	tasks, err := Inventory(objects, now)
	if err != nil {
		t.Fatal(err)
	}
	return tasks
}

func TestGroupingMarkersAndOwnership(t *testing.T) {
	a := object(t, "/a.ics")
	b := object(t, "/b.ics")
	tasks := inventory(t, a, b)
	if len(tasks) != 1 || tasks[0].Resources != 2 || !tasks[0].Writable || tasks[0].Schedule != nil {
		t.Fatalf("group: %+v", tasks)
	}
	original := tasks[0].Revision
	b.ETag = `"v2"`
	if inventory(t, a, b)[0].Revision == original {
		t.Fatal("revision ignored member ETag")
	}
	b.Data.Events()[0].Props.SetText("DESCRIPTION", "a human imported event")
	tasks = inventory(t, a, b)
	if len(tasks) != 2 {
		t.Fatalf("untagged grouped by title: %+v", tasks)
	}
	for _, task := range tasks {
		if strings.HasPrefix(task.ID, "resource:") && task.Writable {
			t.Fatal("imported event writable")
		}
	}
	for _, description := range []string{"[docket] [hassle:v1 task=invalid]", "[docket] [hassle:v1 task=" + testID + "] [hassle:v1 task=" + testID + "]", "[docket] [hassle:v1 task=" + testID + "] [hassle:broken]"} {
		b.Data.Events()[0].Props.SetText("DESCRIPTION", description)
		tasks = inventory(t, a, b)
		if len(tasks) != 2 {
			t.Fatalf("malformed marker grouped: %s", description)
		}
	}
	b.Data.Events()[0].Props.SetText("DESCRIPTION", "[hassle:v1 task="+testID+"]")
	tasks = inventory(t, a, b)
	if len(tasks) != 1 || tasks[0].Writable {
		t.Fatal("UUID incorrectly granted ownership")
	}
	b.Data.Events()[0].Props.SetText("DESCRIPTION", "[docket] [hassle:v1 task="+testID+"]")
	b.Data.Events()[0].Props.SetText("ATTENDEE", "mailto:person@example.test")
	if inventory(t, a, b)[0].Writable {
		t.Fatal("meeting participants writable")
	}
}

func TestCanceledMembersExceptionsAndUnknownFields(t *testing.T) {
	a := object(t, "/a.ics")
	b := object(t, "/b.ics")
	b.Data.Events()[0].Props.SetText("STATUS", "CANCELLED")
	if len(inventory(t, b)) != 0 || inventory(t, a, b)[0].Resources != 2 {
		t.Fatal("cancellation grouping")
	}
	a.Data.Props.SetText("X-CALENDAR-CUSTOM", "preserve root")
	e := a.Data.Events()[0]
	e.Props.SetText("DESCRIPTION", "Human notes\n[docket]\n[hassle:v1 task="+testID+"]")
	e.Props.SetText("X-UNKNOWN", "preserve me")
	originalDesc, _ := e.Props.Text("DESCRIPTION")
	s := sample()
	s.Title = "Edited"
	if err := ApplySchedule(e, s, now); err != nil {
		t.Fatal(err)
	}
	raw, err := Encode(a.Data)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ical.NewDecoder(strings.NewReader(raw)).Decode()
	if err != nil {
		t.Fatal(err)
	}
	if desc, _ := parsed.Events()[0].Props.Text("DESCRIPTION"); desc != originalDesc {
		t.Fatal("lost description")
	}
	unknown, _ := parsed.Events()[0].Props.Text("X-UNKNOWN")
	rootUnknown, _ := parsed.Props.Text("X-CALENDAR-CUSTOM")
	if unknown != "preserve me" || rootUnknown != "preserve root" {
		t.Fatal("lost unknown fields")
	}
	exception := ical.NewEvent()
	exception.Props.SetText("UID", "hassle-"+testID)
	exception.Props.SetDateTime("DTSTAMP", now)
	exception.Props.SetDateTime("RECURRENCE-ID", now)
	exception.Props.SetDateTime("DTSTART", now.Add(time.Hour))
	exception.Props.SetText("X-EXCEPTION", "keep")
	a.Data.Children = append(a.Data.Children, exception.Component)
	tasks := inventory(t, a)
	if len(tasks) != 1 || !tasks[0].Writable || tasks[0].Schedule != nil {
		t.Fatalf("exception handling %+v", tasks)
	}
	raw, err = Encode(a.Data)
	if err != nil || !strings.Contains(raw, "X-EXCEPTION") {
		t.Fatal("lost exception")
	}
}

func TestSchedulesValidationAndInventoryDates(t *testing.T) {
	for _, change := range []func(*Schedule){func(s *Schedule) { s.Cadence = "hourly" }, func(s *Schedule) { s.Interval = 0 }, func(s *Schedule) { s.Interval = 1000 }, func(s *Schedule) { s.TimeZone = "UTC" }, func(s *Schedule) { s.Start = "2026-10-04T02:30" }, func(s *Schedule) { s.Start = "2026-04-05T02:30" }, func(s *Schedule) { s.Until = s.Start }, func(s *Schedule) { s.Title = "" }, func(s *Schedule) { s.Cadence = "once"; s.Interval = 1; s.Until = "2027-01-01T08:30" }} {
		s := sample()
		change(&s)
		if _, _, err := s.Validate(); err == nil {
			t.Fatalf("accepted invalid %+v", s)
		}
	}
	for _, cadence := range []string{"once", "daily", "weekdays", "weekly", "monthly", "yearly"} {
		s := sample()
		s.Cadence = cadence
		s.Interval = 1
		if cadence != "once" {
			s.Until = "2027-01-01T08:30"
		}
		c, err := NewCalendar(testID, s, now)
		if err != nil {
			t.Fatal(err)
		}
		got, why := readSchedule(c.Events()[0])
		if got == nil || *got != s {
			t.Fatalf("roundtrip %s: %+v %s", cadence, got, why)
		}
		raw, _ := Encode(c)
		if !strings.Contains(raw, "DTSTART;TZID=Australia/Melbourne:") || !strings.Contains(raw, "TRANSP:TRANSPARENT") || !strings.Contains(raw, "ACTION:DISPLAY") {
			t.Fatal(raw)
		}
	}
	s := sample()
	s.Cadence = "once"
	s.Interval = 1
	s.Start = "2020-01-01T08:30"
	c, _ := NewCalendar(testID, s, now)
	if !strings.Contains(inventory(t, caldav.CalendarObject{Path: "/old", ETag: `"1"`, Data: c})[0].Reminder, "Overdue") {
		t.Fatal("lost overdue one-off")
	}
	s.Cadence = "daily"
	s.Until = "2020-02-01T08:30"
	c, _ = NewCalendar(testID, s, now)
	if !strings.Contains(inventory(t, caldav.CalendarObject{Path: "/old", ETag: `"1"`, Data: c})[0].Reminder, "No future") {
		t.Fatal("lost exhausted series")
	}
	s.Start = "2099-01-01T08:30"
	s.Until = ""
	c, _ = NewCalendar(testID, s, now)
	if !strings.Contains(inventory(t, caldav.CalendarObject{Path: "/future", ETag: `"1"`, Data: c})[0].Reminder, "2099") {
		t.Fatal("lost distant task")
	}
	for _, rule := range []string{"FREQ=HOURLY", "FREQ=WEEKLY;COUNT=10", "FREQ=DAILY;BYHOUR=8,9", "INTERVAL=1", "FREQ=DAILY;FREQ=DAILY"} {
		c.Events()[0].Props.SetText("RRULE", rule)
		if got, _ := readSchedule(c.Events()[0]); got != nil {
			t.Fatalf("simplified imported rule %s", rule)
		}
	}
}

type fakeBackend struct {
	objects []caldav.CalendarObject
	writes  []string
	failAt  int
}

func (f *fakeBackend) Calendars(context.Context) ([]Calendar, error) { return []Calendar{}, nil }
func (f *fakeBackend) Objects(context.Context, string) ([]caldav.CalendarObject, error) {
	return f.objects, nil
}
func (f *fakeBackend) Write(_ context.Context, _, href, etag, body string) error {
	f.writes = append(f.writes, href+" "+etag)
	if f.failAt == len(f.writes) {
		return ErrConflict
	}
	return nil
}
func TestConditionalGroupMutationsAndDedupe(t *testing.T) {
	loc := Location{AccountID: "personal", CalendarID: "/caldav/v2/reminders@example.test/events/"}
	f := &fakeBackend{objects: []caldav.CalendarObject{object(t, loc.CalendarID+"non-uid-a.ics"), object(t, loc.CalendarID+"non-uid-b.ics")}}
	task := inventory(t, f.objects...)[0]
	in := Mutation{Location: loc, Action: "done", ID: task.ID, Revision: task.Revision}
	f.objects[1].ETag = `"changed"`
	if _, err := Mutate(context.Background(), f, in, now); !errors.Is(err, ErrConflict) || len(f.writes) != 0 {
		t.Fatal("stale group wrote", err)
	}
	in.Revision = inventory(t, f.objects...)[0].Revision
	f.failAt = 2
	res, err := Mutate(context.Background(), f, in, now)
	if !errors.Is(err, ErrConflict) || res.Removed != 1 || !strings.Contains(err.Error(), "1 of 2") {
		t.Fatalf("partial failure %+v %v", res, err)
	}
	f.writes = nil
	f.failAt = 0
	res, err = Mutate(context.Background(), f, in, now)
	if err != nil || res.Removed != 2 || !strings.Contains(f.writes[0], "non-uid-a.ics \"v1\"") {
		t.Fatalf("delete conditions %+v %v", f.writes, err)
	}
	f = &fakeBackend{}
	in = Mutation{Location: loc, Action: "create", ID: testID, Schedule: sample()}
	if _, err = Mutate(context.Background(), f, in, now); err != nil || len(f.writes) != 1 || !strings.HasSuffix(f.writes[0], " *") {
		t.Fatal("create condition", err)
	}
	f.objects = []caldav.CalendarObject{object(t, loc.CalendarID+"hassle-"+testID+".ics")}
	f.writes = nil
	if res, err = Mutate(context.Background(), f, in, now); err != nil || res.Message != "Already created" || len(f.writes) != 0 {
		t.Fatal("dedupe", res, err)
	}
	in.Schedule.Title = "different"
	if _, err = Mutate(context.Background(), f, in, now); !errors.Is(err, ErrConflict) {
		t.Fatal("id reused with different payload", err)
	}
	f.objects = append(f.objects, object(t, loc.CalendarID+"other.ics"))
	task = inventory(t, f.objects...)[0]
	in = Mutation{Location: loc, Action: "edit", ID: task.ID, Revision: task.Revision, Schedule: sample()}
	if _, err = Mutate(context.Background(), f, in, now); err == nil {
		t.Fatal("edited multi-resource schedule")
	}
}

func TestNewMemberConflictsAndPartialRetryNeverRecreatesResources(t *testing.T) {
	loc := Location{AccountID: "personal", CalendarID: "/caldav/v2/reminders@example.test/events/"}
	a := object(t, loc.CalendarID+"a.ics")
	b := object(t, loc.CalendarID+"b.ics")
	f := &fakeBackend{objects: []caldav.CalendarObject{a}}
	task := inventory(t, a)[0]
	in := Mutation{Location: loc, Action: "delete", ID: task.ID, Revision: task.Revision}
	f.objects = append(f.objects, b)
	if _, err := Mutate(context.Background(), f, in, now); !errors.Is(err, ErrConflict) || len(f.writes) != 0 {
		t.Fatal("new member did not conflict", err)
	}
	// Model the server result after one conditional DELETE succeeded and another failed.
	f.objects = []caldav.CalendarObject{b}
	in.Revision = inventory(t, a, b)[0].Revision
	if _, err := Mutate(context.Background(), f, in, now); !errors.Is(err, ErrConflict) || len(f.writes) != 0 {
		t.Fatal("old group retry wrote", err)
	}
	in.Revision = inventory(t, b)[0].Revision
	result, err := Mutate(context.Background(), f, in, now)
	if err != nil || result.Removed != 1 || len(f.writes) != 1 || !strings.Contains(f.writes[0], "b.ics") {
		t.Fatalf("retry did not target only remaining resource: %+v %v %v", result, err, f.writes)
	}
}
func TestImportedScheduleRestrictionAndOwnershipAreIndependent(t *testing.T) {
	o := object(t, "/imported.ics")
	e := o.Data.Events()[0]
	e.Props.SetText("DESCRIPTION", "Human notes only")
	e.Props.SetText("RRULE", "FREQ=DAILY;COUNT=3")
	task := inventory(t, o)[0]
	if task.Writable || task.Schedule != nil || !strings.Contains(task.Reminder, "unavailable") {
		t.Fatalf("unsafe import %+v", task)
	}
	f := &fakeBackend{objects: []caldav.CalendarObject{o}}
	_, err := Mutate(context.Background(), f, Mutation{Action: "delete", ID: task.ID, Revision: task.Revision}, now)
	if err == nil || len(f.writes) != 0 {
		t.Fatal("unowned delete reached write")
	}
	o = object(t, "/legacy.ics")
	e = o.Data.Events()[0]
	e.Props.SetText("DESCRIPTION", "[docket] legacy human description")
	e.Props.Del("RRULE")
	old, _ := WallTime("2020-01-01T08:30")
	e.Props.SetDateTime("DTSTART", old)
	e.Props.SetDateTime("DTEND", old.Add(time.Hour))
	task = inventory(t, o)[0]
	if !task.Writable || task.Schedule != nil || !strings.Contains(task.Reminder, "Overdue") {
		t.Fatalf("legacy visibility %+v", task)
	}
}
func TestSubMinuteImportedTimesAreNotSilentlySimplified(t *testing.T) {
	o := object(t, "/seconds.ics")
	o.Data.Events()[0].Props.Get("DTSTART").Value = "20261105T083030"
	if inventory(t, o)[0].Schedule != nil {
		t.Fatal("silently dropped DTSTART seconds")
	}
	o = object(t, "/until-seconds.ics")
	o.Data.Events()[0].Props.SetText("RRULE", "FREQ=DAILY;UNTIL=20271231T000030Z")
	if inventory(t, o)[0].Schedule != nil {
		t.Fatal("silently dropped UNTIL seconds")
	}
	o = object(t, "/params.ics")
	o.Data.Events()[0].Props.Get("SUMMARY").Params.Set("LANGUAGE", "en-AU")
	s := sample()
	s.Title = "Edited"
	if err := ApplySchedule(o.Data.Events()[0], s, now); err != nil {
		t.Fatal(err)
	}
	if o.Data.Events()[0].Props.Get("SUMMARY").Params.Get("LANGUAGE") != "en-AU" {
		t.Fatal("dropped property parameters")
	}
}

func TestInventorySortsByTitleWithStableIdentityTieBreak(t *testing.T) {
	a := object(t, "/z.ics")
	b := object(t, "/a.ics")
	c := object(t, "/c.ics")
	for _, o := range []caldav.CalendarObject{a, b, c} {
		o.Data.Events()[0].Props.SetText("DESCRIPTION", "[docket]")
	}
	a.Data.Events()[0].Props.SetText("SUMMARY", "Zulu")
	b.Data.Events()[0].Props.SetText("SUMMARY", "alpha")
	c.Data.Events()[0].Props.SetText("SUMMARY", "Alpha")
	tasks := inventory(t, a, c, b)
	if len(tasks) != 3 || strings.ToLower(tasks[0].Title) != "alpha" || strings.ToLower(tasks[1].Title) != "alpha" || tasks[0].ID > tasks[1].ID || tasks[2].Title != "Zulu" {
		t.Fatalf("not useful/stable title order: %+v", tasks)
	}
}
