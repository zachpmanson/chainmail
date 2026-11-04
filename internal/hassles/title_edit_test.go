package hassles

import (
	"context"
	"testing"

	"github.com/emersion/go-webdav/caldav"
)

func TestTitleEditUpdatesSeriesMasterNotSameTitleTask(t *testing.T) {
	loc := Location{AccountID: "personal", CalendarID: testCollection}
	a := object(t, testCollection+"series.ics")
	other, err := NewCalendar("744aa8f9-491b-4473-8dbb-554c957a075c", sample(), now)
	if err != nil {
		t.Fatal(err)
	}
	b := caldav.CalendarObject{Path: testCollection + "separate.ics", ETag: `"other"`, Data: other}
	f := &fakeBackend{objects: []caldav.CalendarObject{a, b}}
	var task Task
	for _, candidate := range inventory(t, a, b) {
		if candidate.ID == "task:"+testID {
			task = candidate
		}
	}
	if task.Schedule == nil {
		t.Fatal("series is not editable")
	}
	master := a.Data.Events()[0]
	uid, _ := master.Props.Text("UID")
	rule := master.Props.Get("RRULE").Value
	schedule := *task.Schedule
	schedule.Title = "Renamed recurring bill"
	_, err = Mutate(context.Background(), f, Mutation{
		Location: loc, Action: "edit", ID: task.ID, Revision: task.Revision, Schedule: schedule,
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.writes) != 1 || f.writes[0] != a.Path+` "v1"` {
		t.Fatalf("expected one conditional master update, got %v", f.writes)
	}
	if len(a.Data.Events()) != 1 {
		t.Fatal("title edit created occurrence overrides")
	}
	if got, _ := master.Props.Text("SUMMARY"); got != schedule.Title {
		t.Fatalf("series master title = %q", got)
	}
	if got, _ := master.Props.Text("UID"); got != uid || master.Props.Get("RRULE").Value != rule {
		t.Fatal("title edit changed series identity or recurrence")
	}
	if got, _ := b.Data.Events()[0].Props.Text("SUMMARY"); got != sample().Title {
		t.Fatalf("separate task with same title was changed: %q", got)
	}
}
