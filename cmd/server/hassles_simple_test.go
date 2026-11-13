package main

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/zachpmanson/chainmail/internal/hassles"
)

func TestHasslesSimpleNativeChoices(t *testing.T) {
	for _, interval := range []float64{0.5, 1, 2, 3, 4, 6, 8, 12, 24} {
		for _, work := range []bool{false, true} {
			t.Run(fmt.Sprintf("%g/work-%v", interval, work), func(t *testing.T) {
				h, f, loc := hassleHarness(t)
				chooseHassles(t, h, loc)
				schedule := hassles.Schedule{Title: "Native reminder", Start: "2027-02-12T09:17", TimeZone: hassles.Zone, Cadence: "hourly", Interval: interval, WorkHours: work}
				plan, err := hassles.CompileDailyPartsV2(schedule, hassleUUID, "e3ae442f-5498-4cd6-a362-312ba81b9123")
				if err != nil {
					t.Fatal(err)
				}
				in := hassles.Mutation{Location: loc, Action: "create", ID: hassleUUID, Schedule: schedule}
				if r := h.do(t, "POST", "/v1/hassles", hassleJSON(t, in)); r.status != 200 {
					t.Fatal(r.status, string(r.body))
				}
				tasks := hassleList(t, h)
				if len(f.objects) != len(plan.Members()) || len(tasks) != 1 || tasks[0].ID != "v2:"+hassleUUID || !tasks[0].Writable || tasks[0].Instance != nil || tasks[0].Schedule == nil || tasks[0].Schedule.Interval != interval {
					t.Fatalf("bad native inventory: %+v / %d", tasks, len(f.objects))
				}
				writes := f.writes
				if r := h.do(t, "POST", "/v1/hassles", hassleJSON(t, in)); r.status != 409 || f.writes != writes {
					t.Fatal("repeated UUID wrote again", r.status)
				}
			})
		}
	}
}

func TestHasslesSimpleFiniteRenamePartialAndDelete(t *testing.T) {
	h, f, loc := hassleHarness(t)
	chooseHassles(t, h, loc)
	schedule := hassles.Schedule{Title: "Original", Start: "2027-02-12T09:17", Until: "2027-02-12T11:17", TimeZone: hassles.Zone, Cadence: "hourly", Interval: 1}
	in := hassles.Mutation{Location: loc, Action: "create", ID: hassleUUID, Schedule: schedule}
	if r := h.do(t, "POST", "/v1/hassles", hassleJSON(t, in)); r.status != 200 || len(f.objects) != 3 {
		t.Fatal("finite create", r.status, string(r.body), len(f.objects))
	}
	before := map[string]string{}
	for i := range f.objects {
		o := &f.objects[i]
		// Native-imported actual identities need not match generated member names.
		o.Path = reminderCalendar + fmt.Sprintf("observed-%d.ics", i)
		o.Data.Events()[0].Props.SetText("UID", fmt.Sprintf("actual-native-uid-%d", i))
		o.Data.Props.SetText("X-CALENDAR-CUSTOM", "keep root")
		o.Data.Events()[0].Props.SetText("X-UNKNOWN", "keep event")
		before[o.Path], _ = hassles.Encode(o.Data)
	}
	task := hassleList(t, h)[0]
	rename := hassles.Mutation{Location: loc, Action: "rename", ID: task.ID, Revision: task.Revision, Title: "Renamed"}
	stale := rename
	stale.Revision = strings.Repeat("0", 64)
	writes := f.writes
	if r := h.do(t, "POST", "/v1/hassles", hassleJSON(t, stale)); r.status != 409 || f.writes != writes {
		t.Fatal("stale revision wrote", r.status)
	}
	f.conflict = true
	if r := h.do(t, "POST", "/v1/hassles", hassleJSON(t, rename)); r.status != 409 {
		t.Fatal("stale ETag not reported", r.status)
	}
	f.conflict = false
	f.failAt = f.writes + 2
	if r := h.do(t, "POST", "/v1/hassles", hassleJSON(t, rename)); r.status != 409 || !strings.Contains(string(r.body), "partially applied") {
		t.Fatal("partial rename not reported", r.status, string(r.body))
	}
	tasks := hassleList(t, h)
	if len(tasks) != 1 || !tasks[0].Writable || !strings.Contains(tasks[0].Restriction, "Titles differ") {
		t.Fatalf("mixed-title group unavailable: %+v", tasks)
	}
	rename.Revision = tasks[0].Revision
	f.failAt = 0
	if r := h.do(t, "POST", "/v1/hassles", hassleJSON(t, rename)); r.status != 200 {
		t.Fatal("fresh rename", r.status, string(r.body))
	}
	for _, o := range f.objects {
		title, _ := o.Data.Events()[0].Props.Text("SUMMARY")
		if title != "Renamed" {
			t.Fatal(title)
		}
		o.Data.Events()[0].Props.SetText("SUMMARY", "Original")
		raw, _ := hassles.Encode(o.Data)
		if raw != before[o.Path] {
			t.Fatal("rename changed properties other than SUMMARY", o.Path)
		}
		o.Data.Events()[0].Props.SetText("SUMMARY", "Renamed")
	}
	task = hassleList(t, h)[0]
	remove := hassles.Mutation{Location: loc, Action: "done", ID: task.ID, Revision: task.Revision}
	f.failAt = f.writes + 2
	if r := h.do(t, "POST", "/v1/hassles", hassleJSON(t, remove)); r.status != 409 || len(f.objects) != 2 {
		t.Fatal("partial delete", r.status, len(f.objects))
	}
	tasks = hassleList(t, h)
	if len(tasks) != 1 || tasks[0].Resources != 2 || !tasks[0].Writable || !strings.Contains(tasks[0].Restriction, "Partial reminder") {
		t.Fatalf("remaining resources unavailable: %+v", tasks)
	}
	remove.Revision = tasks[0].Revision
	f.failAt = 0
	if r := h.do(t, "POST", "/v1/hassles", hassleJSON(t, remove)); r.status != 200 || len(f.objects) != 0 {
		t.Fatal("remaining whole delete", r.status, len(f.objects))
	}
}

func TestHasslesSimplePartialCreateRefusesRepeatedUUID(t *testing.T) {
	h, f, loc := hassleHarness(t)
	chooseHassles(t, h, loc)
	in := hassles.Mutation{Location: loc, Action: "create", ID: hassleUUID, Schedule: hassles.Schedule{Title: "Partial", Start: "2027-02-12T09:17", TimeZone: hassles.Zone, Cadence: "hourly", Interval: 1}}
	f.failAt = 3
	if r := h.do(t, "POST", "/v1/hassles", hassleJSON(t, in)); r.status != 409 || len(f.objects) != 2 || !strings.Contains(string(r.body), "partially applied") {
		t.Fatal("partial create", r.status, string(r.body))
	}
	writes := f.writes
	if r := h.do(t, "POST", "/v1/hassles", hassleJSON(t, in)); r.status != 409 || f.writes != writes {
		t.Fatal("partial UUID reused", r.status)
	}
	tasks := hassleList(t, h)
	if len(tasks) != 1 || !tasks[0].Writable || tasks[0].Resources != 2 {
		t.Fatalf("partial inventory: %+v", tasks)
	}
	in.Schedule.Cadence = "daily"
	if r := h.do(t, "POST", "/v1/hassles", hassleJSON(t, in)); r.status != 409 || f.writes != writes {
		t.Fatal("partial UUID reused for another cadence", r.status)
	}
}

func TestHasslesSimpleReservedCannotUseLegacyActions(t *testing.T) {
	for _, description := range []string{
		"[docket]\n[chainmail:parts:v2 malformed]",
		"[docket]\n[chainmail:hourly:v99 malformed]",
		"[docket]\n[HASSLE:V2 malformed]",
		"[docket]\n[hassle:v2 malformed]",
	} {
		for _, action := range []string{"delete", "rename", "edit", "adopt"} {
			t.Run(description+"/"+action, func(t *testing.T) {
				h, f, loc := hassleHarness(t)
				chooseHassles(t, h, loc)
				schedule := hassles.Schedule{Title: "Reserved reminder", Start: "2027-02-12T09:17", TimeZone: hassles.Zone, Cadence: "daily", Interval: 1}
				create := hassles.Mutation{Location: loc, Action: "create", ID: hassleUUID, Schedule: schedule}
				if r := h.do(t, "POST", "/v1/hassles", hassleJSON(t, create)); r.status != 200 {
					t.Fatal(r.status, string(r.body))
				}
				f.objects[0].Data.Events()[0].Props.SetText("DESCRIPTION", description)
				visible := hassleList(t, h)
				if len(visible) != 1 || visible[0].Writable {
					t.Fatalf("reserved resource is writable: %+v", visible)
				}
				// A caller can reconstruct the old resource ID; it cannot choose
				// a weaker inventory than the one used by the selected UI.
				legacy, err := hassles.Inventory(f.objects, time.Now())
				if err != nil || len(legacy) != 1 {
					t.Fatal(err, legacy)
				}
				in := hassles.Mutation{Location: loc, Action: action, ID: legacy[0].ID, Revision: legacy[0].Revision}
				if action == "rename" {
					in.Title = "Changed"
				}
				if action == "edit" {
					in.Schedule = schedule
				}
				if action == "adopt" {
					id, preview := "e3ae442f-5498-4cd6-a362-312ba81b9123", true
					in.AdoptID, in.DryRun = &id, &preview
				}
				if err := hassles.ValidateAdoptionFields(in); err != nil {
					t.Fatal("invalid regression setup", err)
				}
				writes := f.writes
				if r := h.do(t, "POST", "/v1/hassles", hassleJSON(t, in)); r.status == 200 || f.writes != writes || len(f.objects) != 1 {
					t.Fatalf("reserved resource escaped via %s: status %d, writes %d", action, r.status, f.writes-writes)
				}
			})
		}
	}
}

func TestHasslesSimpleLegacyUUIDCannotStartNativeCohort(t *testing.T) {
	h, f, loc := hassleHarness(t)
	chooseHassles(t, h, loc)
	in := hassles.Mutation{Location: loc, Action: "create", ID: hassleUUID, Schedule: hassles.Schedule{Title: "Existing daily", Start: "2027-02-12T09:17", TimeZone: hassles.Zone, Cadence: "daily", Interval: 1}}
	if r := h.do(t, "POST", "/v1/hassles", hassleJSON(t, in)); r.status != 200 {
		t.Fatal(r.status, string(r.body))
	}
	writes := f.writes
	in.Schedule.Cadence = "hourly"
	if r := h.do(t, "POST", "/v1/hassles", hassleJSON(t, in)); r.status != 409 || f.writes != writes || len(f.objects) != 1 {
		t.Fatal("existing legacy UUID started a native cohort", r.status, f.writes-writes)
	}
}
