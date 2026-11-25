package hassles

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-ical"
	"github.com/emersion/go-webdav/caldav"
)

type dailyPartsCompletionBackend struct {
	fakeBackend
}

func (b *dailyPartsCompletionBackend) Write(_ context.Context, collection, href, tag, body string) error {
	b.writes = append(b.writes, href+" "+tag)
	if collection != dailyGroupLocation.CalendarID || body == "" {
		return errors.New("unexpected location or deletion")
	}
	if b.failAt == len(b.writes) {
		return ErrConflict
	}
	data, err := ical.NewDecoder(strings.NewReader(body)).Decode()
	if err != nil {
		return err
	}
	obj := caldav.CalendarObject{Path: href, ETag: fmt.Sprintf(`"wire-%d"`, len(b.writes)), Data: data}
	for i, old := range b.objects {
		if old.Path == href {
			if tag != old.ETag {
				return ErrConflict
			}
			b.objects[i] = obj
			return nil
		}
	}
	if tag != "*" {
		return ErrConflict
	}
	b.objects = append(b.objects, obj)
	return nil
}

func TestWebDailyPartsWorkHoursCompletion(t *testing.T) {
	ctx := context.Background()
	b := &dailyPartsCompletionBackend{}
	location := dailyGroupLocation
	wall := func(value string) time.Time {
		t.Helper()
		at, err := WallTime(value)
		if err != nil {
			t.Fatal(err)
		}
		return at
	}
	s := dailyV2Schedule("2027-02-12T11:30", 3, true, "") // Friday, before the weekend.
	now := wall("2027-02-12T11:00")
	create := Mutation{Location: location, Action: "create", ID: dailyV2Task, Schedule: s}
	if _, err := Mutate(ctx, b, create, now); err != nil || len(b.objects) != 2 {
		t.Fatalf("create: %v (%d objects)", err, len(b.objects))
	}
	read := func(at time.Time) Task {
		t.Helper()
		tasks, err := SelectedInventory(b.objects, location, at)
		if err != nil || len(tasks) != 1 || tasks[0].ID != "v2:"+dailyV2Task {
			t.Fatalf("inventory: %+v %v", tasks, err)
		}
		return tasks[0]
	}
	if task := read(now); !task.Writable {
		t.Fatalf("writer output not admitted: %+v", task)
	}
	for i, o := range b.objects {
		// Model Google serialization without obtaining or using private calendar data.
		e := o.Data.Events()[0]
		start, err := melbourneStart(e.Props.Get("DTSTART"))
		if err != nil {
			t.Fatal(err)
		}
		e.Props.Del("DURATION")
		e.Props.SetDateTime("DTEND", start.Add(5*time.Minute))
		e.Props.SetText("STATUS", "CONFIRMED")
		zone, err := ical.NewDecoder(strings.NewReader(googleTimezoneFixture(t))).Decode()
		if err != nil {
			t.Fatal(err)
		}
		o.Data.Children = append(o.Data.Children, zone.Children...)
		rule := e.Props.Get("RRULE")
		rule.Value = "BYDAY=MO,TU,WE,TH,FR;FREQ=DAILY;BYMINUTE=30;WKST=MO;INTERVAL=1"
		alarm := e.Children[0]
		alarm.Props.SetText("DESCRIPTION", "This is an event reminder")
		alarm.Props.Get("TRIGGER").Value = "P0D"
		// The member witness must use the actual href/UID, not the plan's logical ID.
		b.objects[i].Path = location.CalendarID + fmt.Sprintf("provider-%d.ics", i)
		e.Props.SetText("UID", fmt.Sprintf("synthetic-provider-uid-%d", i))
	}
	b.writes = nil
	first := read(now)
	if !first.Writable {
		t.Fatalf("provider-style alarm not admitted: %s", first.Restriction)
	}
	checkNext := func(task Task, value string) {
		t.Helper()
		want := wall(value).UTC()
		if task.Timing.Note != "" || task.Timing.Next == nil || !task.Timing.Next.Equal(want) || task.Instance == nil || task.Instance.At != want.Format(time.RFC3339) {
			t.Fatalf("wanted exact %s, got %+v / %+v (%s)", want, task.Timing, task.Instance, task.InstanceRestriction)
		}
	}
	checkNext(first, "2027-02-12T11:30")
	request := func(task Task, dry bool) Mutation {
		return Mutation{Location: location, Action: "done-instance", ID: task.ID, Revision: task.Revision, InstanceAt: &task.Instance.At, InstanceToken: &task.Instance.Token, DryRun: &dry}
	}
	in := request(first, true)
	before, sibling := b.objects[0], b.objects[1]
	preview, err := Mutate(ctx, b, in, now)
	if err != nil || len(b.writes) != 0 || preview.Completion == nil || preview.Completion.Operation != "exclude-occurrence" {
		t.Fatalf("preview: %+v, %v; writes %v", preview, err, b.writes)
	}
	*in.DryRun = false
	if _, err := Mutate(ctx, b, in, wall("2027-02-12T12:00")); !errors.Is(err, ErrConflict) || len(b.writes) != 0 {
		t.Fatalf("moved witness: %v; writes %v", err, b.writes)
	}
	b.objects[1].ETag = `"changed-sibling"`
	if _, err := Mutate(ctx, b, in, now); !errors.Is(err, ErrConflict) || len(b.writes) != 0 {
		t.Fatalf("stale sibling: %v; writes %v", err, b.writes)
	}
	b.objects[1] = sibling
	b.failAt = 1
	if _, err := Mutate(ctx, b, in, now); !errors.Is(err, ErrConflict) || len(b.writes) != 1 || b.objects[0].ETag != before.ETag {
		t.Fatalf("provider precondition: %v; writes %v", err, b.writes)
	}
	b.failAt, b.writes = 0, nil
	got, err := Mutate(ctx, b, in, now)
	if err != nil || got.Removed != 0 || len(b.writes) != 1 || b.writes[0] != before.Path+" "+before.ETag || len(b.objects) != 2 || !reflect.DeepEqual(sibling, b.objects[1]) {
		t.Fatalf("commit: %+v, %v; writes %v", got, err, b.writes)
	}
	after := b.objects[0].Data.Events()[0]
	beforeEvent := before.Data.Events()[0]
	dates, err := recurrenceDates(after.Props["EXDATE"])
	if err != nil || len(dates) != 1 || !dates[0].Equal(wall("2027-02-12T11:30")) || after.Props.Get("DESCRIPTION").Value != beforeEvent.Props.Get("DESCRIPTION").Value+`\n`+completionMarker {
		t.Fatalf("wrong exclusion/metadata: %v %v", dates, err)
	}
	// Ignore just the fields the occurrence write is expected to change.
	check := cloneCalendar(b.objects[0].Data)
	for _, name := range []string{"DESCRIPTION", "EXDATE", "DTSTAMP", "LAST-MODIFIED"} {
		check.Events()[0].Props[name] = beforeEvent.Props[name]
	}
	wantBody, _ := Encode(before.Data)
	gotBody, _ := Encode(check)
	if gotBody != wantBody {
		t.Fatal("completion changed unrelated iCalendar content")
	}
	checkNext(read(now), "2027-02-12T14:30")
	x := after.Props.Get("EXDATE")
	x.Params.Set("TZID", Zone)
	x.Value = "20270212T113000"
	checkNext(read(now), "2027-02-12T14:30")
	checkNext(read(wall("2027-02-12T15:00")), "2027-02-15T11:30")

	fresh := request(read(now), false)
	b.writes = nil
	fullRoster := b.objects
	b.objects = b.objects[1:]
	partial := read(now)
	fresh.Revision = partial.Revision
	if !partial.Writable || partial.Instance != nil || partial.Timing.State != "unknown" {
		t.Fatalf("partial group should allow management, not occurrence selection: %+v", partial)
	}
	if _, err := Mutate(ctx, b, fresh, now); !errors.Is(err, ErrConflict) || len(b.writes) != 0 {
		t.Fatalf("partial group wrote: %v; writes %v", err, b.writes)
	}
	b.objects = fullRoster
	b.objects[0].Data.Events()[0].Children[0].Props.Get("TRIGGER").Value = "-P0DT0H10M0S"
	restricted := read(now)
	fresh.Revision = restricted.Revision
	if restricted.Writable || restricted.Instance != nil || len(b.writes) != 0 {
		t.Fatalf("nonzero alarm admitted: %+v", restricted)
	}
	if _, err := Mutate(ctx, b, fresh, now); err == nil || len(b.writes) != 0 {
		t.Fatalf("unsupported group wrote: %v; writes %v", err, b.writes)
	}
}
