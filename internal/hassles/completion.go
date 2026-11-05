package hassles

import (
	"context"
	"strings"
	"time"

	"github.com/emersion/go-ical"
)

const completionMarker = "[chainmail:completed-instances:v1]"
const maxCompletions = 1000

type Instance struct {
	At    string `json:"at"`
	Token string `json:"token"`
}

type Completion struct {
	InstanceAt string `json:"instanceAt"`
	Operation  string `json:"operation"`
	ResourceID string `json:"resourceId"`
	Before     string `json:"before"`
	After      string `json:"after"`
}

// Only our bounded, UTC-only exclusion profile permits base-schedule editing.
// Imported exclusions without this provenance remain unsupported.
func validCompletionDates(e ical.Event) bool {
	desc, err := e.Props.Text("DESCRIPTION")
	if err != nil || len(e.Props["DESCRIPTION"]) != 1 || strings.Count(desc, "[chainmail:completed-instances:") != 1 || !strings.Contains(desc, completionMarker) {
		return false
	}
	seen := map[string]bool{}
	for _, p := range e.Props["EXDATE"] {
		if len(p.Params) != 0 {
			return false
		}
		for _, value := range strings.Split(p.Value, ",") {
			t, err := time.Parse("20060102T150405Z", value)
			if err != nil || t.Year() < 1970 || t.Year() > 2100 || seen[value] || len(seen) >= maxCompletions {
				return false
			}
			seen[value] = true
		}
	}
	return len(seen) > 0
}

func instanceToken(path, uid, at string) string { return digest(path + "\x00" + uid + "\x00" + at) }

// A witness targets Next, or Last only for a fully known ended group. Any
// incomplete member or tied occurrence makes selection unavailable.
func selectedInstance(task *Task, now time.Time) (*Instance, string) {
	reason := "This instance only unavailable: unsupported or ambiguous occurrence; use All instances"
	if !task.Writable {
		return nil, task.Restriction
	}
	if task.Timing.Note != "" || task.Timing.State == "unknown" {
		return nil, reason
	}
	target := task.Timing.Next
	if target == nil && task.Timing.State == "past" {
		target = task.Timing.Last
	}
	if target == nil || target.UTC().Year() < 1970 || target.UTC().Year() > 2100 {
		return nil, reason
	}
	var selected *Instance
	for _, o := range task.members {
		events := o.Data.Events()
		if len(events) != 1 {
			return nil, reason
		}
		e := events[0]
		status, _ := e.Props.Text("STATUS")
		if strings.EqualFold(status, "CANCELLED") {
			continue
		}
		timing := resourceTiming(events, now)
		if timing.State == "unknown" || timing.Note != "" {
			return nil, reason
		}
		at := timing.Next
		if task.Timing.Next == nil {
			at = timing.Last
		}
		if at == nil || !at.Equal(*target) {
			continue
		}
		// Do not widen generic schedule recognition, including timezone validation.
		s, _ := readSchedule(e)
		dates, datesErr := recurrenceDates(e.Props["EXDATE"])
		if s == nil || scheduleTimezoneRestriction(o.Data, s) != "" || len(e.Props["UID"]) != 1 || datesErr != nil || len(dates) >= maxCompletions {
			return nil, reason
		}
		desc, _ := e.Props.Text("DESCRIPTION")
		if strings.Contains(desc, "[chainmail:completed-instances:") && !validCompletionDates(e) {
			return nil, reason
		}
		uid, err := e.Props.Text("UID")
		if err != nil || uid == "" || selected != nil {
			return nil, reason
		}
		instant := target.UTC().Format(time.RFC3339)
		selected = &Instance{At: instant, Token: instanceToken(o.Path, uid, instant)}
	}
	if selected == nil {
		return nil, reason
	}
	return selected, ""
}

func completeInstance(ctx context.Context, b Backend, in Mutation, task *Task, now time.Time) (Result, error) {
	result := Result{}
	// Recomputed only to validate the displayed witness, never to substitute it.
	// Clock movement to another Next/Last is a conflict, even without an ETag change.
	if task.Instance == nil || task.Instance.At != *in.InstanceAt || task.Instance.Token != *in.InstanceToken {
		return result, ErrConflict
	}
	for _, o := range task.members {
		e := o.Data.Events()[0]
		uid, _ := e.Props.Text("UID")
		if instanceToken(o.Path, uid, *in.InstanceAt) != *in.InstanceToken {
			continue
		}
		before, err := Encode(o.Data)
		if err != nil {
			return result, err
		}
		preview := &Completion{InstanceAt: *in.InstanceAt, Operation: "delete-resource", ResourceID: "resource:" + digest(o.Path), Before: before}
		body := ""
		if e.Props.Get("RRULE") != nil {
			copy := cloneCalendar(o.Data)
			event := copy.Events()[0]
			desc, _ := event.Props.Text("DESCRIPTION")
			if !strings.Contains(desc, completionMarker) {
				// Preserve original description escaping/parameters and all other content.
				p := event.Props.Get("DESCRIPTION")
				p.Value += `\n` + completionMarker
			}
			at, _ := time.Parse(time.RFC3339, *in.InstanceAt)
			event.Props["EXDATE"] = append(event.Props["EXDATE"], ical.Prop{Name: "EXDATE", Value: at.UTC().Format("20060102T150405Z"), Params: make(ical.Params)})
			event.Props.SetDateTime("DTSTAMP", now.UTC())
			event.Props.SetDateTime("LAST-MODIFIED", now.UTC())
			body, err = Encode(copy)
			if err != nil {
				return result, err
			}
			preview.Operation = "exclude-occurrence"
		}
		preview.After = body
		result.Completion = preview
		if *in.DryRun {
			result.Message = "Instance completion preview; no writes"
			return result, nil
		}
		if err := b.Write(ctx, in.Location.CalendarID, o.Path, o.ETag, body); err != nil {
			return Result{}, err
		}
		if body == "" {
			result.Removed = 1
		}
		result.Message = "Completed this instance only"
		return result, nil
	}
	return result, ErrConflict
}
