package hassles

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-ical"
	"github.com/emersion/go-webdav/caldav"
)

// These exercise the UNCHANGED production Inventory/Mutate/adopt paths, not
// copies of historical policy and not the new reader's capability flags.
type dailyOldBackend struct {
	objects     []caldav.CalendarObject
	objectCalls int
	collections []string
	writes      []adoptionWrite
}

func (b *dailyOldBackend) Calendars(context.Context) ([]Calendar, error) { return nil, nil }
func (b *dailyOldBackend) Objects(_ context.Context, collection string) ([]caldav.CalendarObject, error) {
	b.objectCalls++
	b.collections = append(b.collections, collection)
	return b.objects, nil
}
func (b *dailyOldBackend) Write(_ context.Context, collection, path, etag, body string) error {
	b.writes = append(b.writes, adoptionWrite{collection, path, etag, body})
	return nil
}

func dailyOldRetain(t *testing.T, objects []caldav.CalendarObject, mask int) {
	t.Helper()
	for _, o := range objects {
		e := o.Data.Events()[0]
		desc, _ := e.Props.Text("DESCRIPTION")
		lines := strings.Split(desc, "\n")
		if len(lines) != 3 {
			t.Fatal("bad synthetic H/P/M fixture")
		}
		kept := []string{}
		for bit, line := range lines {
			if mask&(1<<bit) != 0 {
				kept = append(kept, line)
			}
		}
		e.Props.SetText("DESCRIPTION", strings.Join(kept, "\n"))
		if !strings.Contains(e.Props.Get("RRULE").Value, "BYMINUTE=") {
			t.Fatal("old-action fixture dropped minute filter")
		}
	}
}

func dailyOldRequests(t *testing.T, task Task, o caldav.CalendarObject) []struct {
	name string
	in   Mutation
} {
	t.Helper()
	e := o.Data.Events()[0]
	uid, err := e.Props.Text("UID")
	if err != nil || uid == "" {
		t.Fatal("bad synthetic UID")
	}
	start, err := melbourneStart(e.Props.Get("DTSTART"))
	if err != nil {
		t.Fatal(err)
	}
	at := start.UTC().Format(time.RFC3339)
	token := instanceToken(o.Path, uid, at)
	base := Mutation{Location: dailyGroupLocation, ID: task.ID, Revision: task.Revision}
	out := []struct {
		name string
		in   Mutation
	}{}
	edit := base
	edit.Action = "edit"
	edit.Schedule = dailyV2Schedule(start.Format(dailyPartsV2WallLayout), 24, false, "")
	edit.Schedule.Title = "Edited synthetic title"
	if _, _, err := edit.Schedule.Validate(); err != nil {
		t.Fatal(err)
	}
	// This is a title-only logical request for singleton24; other unsupported
	// shapes still exercise the real edit route with a fully valid schedule.
	out = append(out, struct {
		name string
		in   Mutation
	}{"edit", edit})
	for _, action := range []string{"delete", "done"} {
		in := base
		in.Action = action
		out = append(out, struct {
			name string
			in   Mutation
		}{action, in})
	}
	for _, dry := range []bool{true, false} {
		in := base
		in.Action = "done-instance"
		in.InstanceAt = ptr(at)
		in.InstanceToken = ptr(token)
		in.DryRun = ptr(dry)
		out = append(out, struct {
			name string
			in   Mutation
		}{fmt.Sprintf("done-instance/dry-%t", dry), in})
	}
	for _, mode := range []string{"preview", "commit"} {
		in := base
		in.Action = "adopt"
		in.ID = "resource:" + digest(o.Path)
		in.AdoptID = ptr(adoptionID)
		in.DryRun = ptr(mode == "preview")
		if mode != "preview" {
			in.AdoptHash = ptr(digest("syntactically valid saved synthetic fingerprint"))
		}
		out = append(out, struct {
			name string
			in   Mutation
		}{"adopt/" + mode, in})
	}
	for _, request := range out {
		if !validDigest(request.in.Revision) {
			t.Fatal("non-digest actual Inventory revision")
		}
		if err := ValidateAdoptionFields(request.in); err != nil {
			t.Fatalf("argument-only denial is not evidence (%s): %v", request.name, err)
		}
	}
	return out
}

func dailyOldDeny(t *testing.T, objects []caldav.CalendarObject, task Task, absent bool, actions ...string) {
	t.Helper()
	dailyOldDenyMember(t, objects, task, task.members[0], absent, actions...)
}

func dailyOldDenyMember(t *testing.T, objects []caldav.CalendarObject, task Task, member caldav.CalendarObject, absent bool, actions ...string) {
	t.Helper()
	if !absent && task.Writable {
		t.Fatalf("present task must be read-only: %+v", task)
	}
	original := snapshot(t, objects)
	run := 0
	for _, request := range dailyOldRequests(t, task, member) {
		if len(actions) != 0 && !slices.Contains(actions, request.name) {
			continue
		}
		run++
		t.Run(request.name, func(t *testing.T) {
			backend := &dailyOldBackend{objects: objects}
			result, err := Mutate(context.Background(), backend, request.in, now)
			if err == nil || result != (Result{}) || len(backend.writes) != 0 {
				t.Fatalf("unexpected successful old action/preview: %+v %v writes=%+v", result, err, backend.writes)
			}
			if backend.objectCalls != 1 || len(backend.collections) != 1 || backend.collections[0] != dailyGroupLocation.CalendarID {
				t.Fatal("fresh Objects not reached; syntax-only rejection is not protection")
			}
			if absent && !errors.Is(err, ErrConflict) {
				t.Fatalf("absent target must fail fresh lookup: %v", err)
			}
			if original != snapshot(t, objects) {
				t.Fatal("old rejection modified input raw properties/parameters/children or sibling")
			}
		})
	}
	if run == 0 || (len(actions) != 0 && run != len(actions)) {
		t.Fatalf("requested actions %v, ran %d", actions, run)
	}
}

func TestDailyPartsOldReaderDeniesWrites(t *testing.T) {
	p := dailyV2Compile(t, dailyV2Schedule("2027-02-12T09:17", 24, false, ""))
	for mask := 0; mask < 8; mask++ {
		t.Run(fmt.Sprintf("retained-HPM-%03b", mask), func(t *testing.T) {
			objects := dailyGroupObjects(t, p)
			dailyOldRetain(t, objects, mask)
			before := snapshot(t, objects)
			tasks := inventory(t, objects...)
			if len(tasks) != 1 || tasks[0].Resources != 1 || tasks[0].Writable {
				t.Fatalf("want a single read-only resource, got %+v", tasks)
			}
			// Intact markers exercise reserved-metadata rejection; erased
			// markers exercise the fallback without native provenance.
			if mask == 0 || mask == 7 {
				dailyOldDeny(t, objects, tasks[0], false)
			}
			if before != snapshot(t, objects) {
				t.Fatal("legacy inventory or action normalized raw source")
			}
		})
	}
}

func TestDailyPartsOldReaderMalformedSummaryAndV1Cases(t *testing.T) {
	p := dailyV2Compile(t, dailyV2Schedule("2027-02-12T09:17", 24, false, ""))
	for _, mode := range []string{"summary-only-Docket", "same-v1", "different-v1", "unknown-version", "duplicate-header", "duplicate-description", "case", "malformed", "already-adopted-looking"} {
		t.Run(mode, func(t *testing.T) {
			objects := dailyGroupObjects(t, p)
			e := objects[0].Data.Events()[0]
			desc, _ := e.Props.Text("DESCRIPTION")
			switch mode {
			case "summary-only-Docket":
				e.Props.SetText("SUMMARY", "[docket] Synthetic title")
			case "same-v1":
				desc += "\n[hassle:v1 task=" + dailyV2Task + "]"
			case "different-v1":
				desc += "\n[hassle:v1 task=" + testID + "]"
			case "unknown-version":
				desc = strings.Replace(desc, "[hassle:v2", "[hassle:v99", 1)
			case "duplicate-header":
				desc += "\n" + strings.Split(desc, "\n")[0]
			case "case":
				desc = strings.ReplaceAll(desc, "[hassle:", "[HASSLE:")
			case "malformed":
				desc = "[hassle:v2 task=" + dailyV2Task + " malformed]\n[chainmail:hourly:v2 minutes=1440]"
			case "already-adopted-looking":
				desc = "No Docket\n[hassle:v1 task=" + adoptionID + "]"
			}
			e.Props.SetText("DESCRIPTION", desc)
			if mode == "duplicate-description" {
				e.Props["DESCRIPTION"] = append(e.Props["DESCRIPTION"], e.Props["DESCRIPTION"][0])
			}
			tasks := inventory(t, objects...)
			if len(tasks) != 1 {
				t.Fatal("expected actual present target")
			}
			if mode == "already-adopted-looking" && tasks[0].ID != "task:"+adoptionID {
				t.Fatal("retry did not reach actual already-adopted-looking identity")
			}
			action := "delete"
			if mode == "already-adopted-looking" {
				action = "adopt/commit"
			}
			dailyOldDeny(t, objects, tasks[0], false, action)
		})
	}
}

func TestDailyPartsOldReaderCollisionFullSnapshotNeverPromotesV1(t *testing.T) {
	p := dailyV2Compile(t, dailyV2Schedule("2027-02-12T09:17", 24, false, ""))
	objects := dailyGroupObjects(t, p)
	// P retained, H/M removed, an accidental v1 tag: the unchanged old reader
	// actually groups this hostile resource with the genuine Docket sibling.
	dailyOldRetain(t, objects, 2)
	e := objects[0].Data.Events()[0]
	desc, _ := e.Props.Text("DESCRIPTION")
	e.Props.SetText("DESCRIPTION", desc+"\n[hassle:v1 task="+dailyV2Task+"]")
	legacy := object(t, dailyGroupLocation.CalendarID+"genuine-v1.ics")
	legacy.Data.Events()[0].Props.SetText("DESCRIPTION", "[docket]\n[hassle:v1 task="+dailyV2Task+"]")
	legacy.Data.Events()[0].Props.SetText("UID", "genuine-distinct-v1-uid")
	objects = append(objects, legacy)
	full := inventory(t, objects...)
	if len(full) != 1 || full[0].ID != "task:"+dailyV2Task || full[0].Resources != 2 || full[0].Writable {
		t.Fatal("full physical collision failed whole-task block", full)
	}
	dailyOldDeny(t, objects, full[0], false, "delete")
	// Adoption addressed to the genuine sibling bypasses the target's reserved
	// marker check, but must still reject adoption of a multi-resource task.
	dailyOldDenyMember(t, objects, full[0], legacy, false, "adopt/commit")
	dailyGroupReadonly(t, objects)
	model := dailyGroupRead(t, objects)
	if len(model.HandledHrefs) != 1 || len(model.Physical) != 2 || len(model.Groups[0].Members) != 2 {
		t.Fatal("new model lost collision or mishandled legacy display evidence")
	}
	// Deliberately show why handled HREFs may suppress DISPLAY only. This is a
	// labeled authority-scope counterexample, never integration guidance.
	filtered := inventory(t, legacy)
	if len(filtered) != 1 || !filtered[0].Writable {
		t.Fatal("fixture did not demonstrate evidence-removal promotion")
	}
}

func TestDailyPartsOldReaderRejectsStaleRequestsAfterCancellation(t *testing.T) {
	p := dailyV2Compile(t, dailyV2Schedule("2027-02-12T09:17", 2, false, ""))
	for _, mask := range []int{0, 7} {
		t.Run(fmt.Sprintf("retained-HPM-%03b", mask), func(t *testing.T) {
			objects := dailyGroupObjects(t, p)
			dailyOldRetain(t, objects, mask)
			previous := inventory(t, objects...)
			if len(previous) < 2 {
				t.Fatal("expected previously visible members")
			}
			for _, o := range objects {
				o.Data.Events()[0].Props.SetText("STATUS", "CANCELLED")
			}
			if fresh := inventory(t, objects...); len(fresh) != 0 {
				t.Fatal("cancelled resources still visible", fresh)
			}
			// Ordinary mutations and adoption take different lookup paths.
			dailyOldDeny(t, objects, previous[0], true, "delete", "adopt/commit")
			if mask != 0 {
				dailyGroupReadonly(t, objects)
			}
		})
	}
}

func TestDailyPartsOldReaderDocketAddedBOUNDARYCounterexamples(t *testing.T) {
	p := dailyV2Compile(t, dailyV2Schedule("2027-02-12T09:17", 24, false, ""))
	// Inventory remains a historical reference for the old writable hole.
	// Current mutation admission must reject that legacy-ID escape.
	for _, action := range []string{"delete", "done"} {
		t.Run("BOUNDARY/full-v2-plus-Docket/"+action, func(t *testing.T) {
			objects := dailyGroupObjects(t, p)
			e := objects[0].Data.Events()[0]
			d, _ := e.Props.Text("DESCRIPTION")
			e.Props.SetText("DESCRIPTION", d+"\n[docket]")
			tasks := inventory(t, objects...)
			if len(tasks) != 1 || !tasks[0].Writable {
				t.Fatal("boundary no longer demonstrates old writable hole")
			}
			before := snapshot(t, objects)
			b := &dailyOldBackend{objects: objects}
			in := Mutation{Location: dailyGroupLocation, Action: action, ID: tasks[0].ID, Revision: tasks[0].Revision}
			if err := ValidateAdoptionFields(in); err != nil {
				t.Fatal(err)
			}
			result, err := Mutate(context.Background(), b, in, now)
			if !errors.Is(err, ErrConflict) || result.Removed != 0 || len(b.writes) != 0 || b.objectCalls != 1 {
				t.Fatalf("legacy-ID escape was not rejected after a fresh read: %+v %v", result, err)
			}
			if snapshot(t, objects) != before {
				t.Fatal("boundary backend unexpectedly mutated fixture")
			}
			dailyGroupReadonly(t, objects)
		})
	}
	// Not a no-Docket guarantee: erasing H/P/M then ADDING Docket can permit
	// actual adoption preview and commit. Raw BYMINUTE rule remains untouched.
	t.Run("BOUNDARY/erased-markers-plus-Docket/adopt", func(t *testing.T) {
		objects := dailyGroupObjects(t, p)
		dailyOldRetain(t, objects, 0)
		objects[0].Data.Events()[0].Props.SetText("DESCRIPTION", "[docket]")
		tasks := inventory(t, objects...)
		if len(tasks) != 1 || !tasks[0].Writable {
			t.Fatal("boundary fixture not writable")
		}
		before := snapshot(t, objects)
		b := &dailyOldBackend{objects: objects}
		in := Mutation{Location: dailyGroupLocation, Action: "adopt", ID: "resource:" + digest(objects[0].Path), Revision: tasks[0].Revision, AdoptID: ptr(adoptionID), DryRun: ptr(true)}
		if err := ValidateAdoptionFields(in); err != nil {
			t.Fatal(err)
		}
		preview, err := Mutate(context.Background(), b, in, now)
		if err != nil || preview.Adoption == nil || len(b.writes) != 0 || b.objectCalls != 1 {
			t.Fatalf("expected demonstrated adoption preview boundary: %+v %v", preview, err)
		}
		in.DryRun = ptr(false)
		in.AdoptHash = ptr(preview.Adoption.Fingerprint)
		if err := ValidateAdoptionFields(in); err != nil {
			t.Fatal(err)
		}
		result, err := Mutate(context.Background(), b, in, now)
		if err != nil || result.Adoption == nil || len(b.writes) != 1 || b.objectCalls != 2 {
			t.Fatalf("expected demonstrated adoption commit boundary: %+v %v", result, err)
		}
		if snapshot(t, objects) != before {
			t.Fatal("adoption mutated source instead of clone")
		}
	})
}

// Keep parameter/child-bearing source evidence in the real old-action harness.
func TestDailyPartsOldReaderPreservesAllRawSiblings(t *testing.T) {
	p := dailyV2Compile(t, dailyV2Schedule("2027-02-12T09:47", 0.5, true, ""))
	objects := dailyGroupObjects(t, p)
	dailyOldRetain(t, objects, 0)
	for _, o := range objects {
		e := o.Data.Events()[0]
		e.Props.Get("DESCRIPTION").Params.Set("LANGUAGE", "en-AU")
		e.Props["X-RAW"] = []ical.Prop{{Name: "X-RAW", Value: "a,b", Params: ical.Params{"X-LIST": []string{"one", "two"}}}}
		e.Children[0].Props.SetText("X-ALARM", "preserve")
		o.Data.Props.SetText("X-ROOT", "preserve")
	}
	before := snapshot(t, objects)
	tasks, err := SelectedInventory(objects, dailyGroupLocation, now)
	if err != nil {
		t.Fatal(err)
	}
	want := make(map[string]bool, len(objects))
	for _, o := range objects {
		want[o.Path] = true
	}
	for _, task := range tasks {
		if task.Writable || task.Resources != 1 || len(task.members) != 1 || !want[task.members[0].Path] {
			t.Fatalf("want one read-only row per marker-erased resource, got %+v", task)
		}
		delete(want, task.members[0].Path)
	}
	if len(want) != 0 || len(tasks) < 2 {
		t.Fatal("missing marker-erased siblings", want)
	}
	dailyOldDeny(t, objects, tasks[0], false, "edit")
	if before != snapshot(t, objects) {
		t.Fatal("inventory or rejection changed source evidence")
	}
}
