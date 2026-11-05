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

// Google normalizes submitted UTC exclusions into Melbourne local values.
// Accept only instants that round-trip unambiguously through both forms.
func completionInstantSupported(t time.Time) bool {
	if t.UTC().Year() < 1970 || t.UTC().Year() > 2100 || t.Second() != 0 || t.Nanosecond() != 0 {
		return false
	}
	loc, err := time.LoadLocation(Zone)
	if err != nil {
		return false
	}
	wall, err := WallTime(t.In(loc).Format("2006-01-02T15:04"))
	return err == nil && wall.Equal(t)
}

// Only bounded app exclusions in UTC or the verified provider form permit
// base-schedule editing. Imported exclusions without provenance stay unsupported.
func validCompletionDates(e ical.Event) bool {
	desc, err := e.Props.Text("DESCRIPTION")
	if err != nil || len(e.Props["DESCRIPTION"]) != 1 || strings.Count(desc, "[chainmail:completed-instances:") != 1 || !strings.Contains(desc, completionMarker) {
		return false
	}
	seen := map[string]bool{}
	for _, p := range e.Props["EXDATE"] {
		tzid := ""
		for name, values := range p.Params {
			if len(values) != 1 {
				return false
			}
			switch name {
			case "TZID":
				if values[0] != Zone {
					return false
				}
				tzid = values[0]
			case "VALUE":
				if values[0] != "DATE-TIME" {
					return false
				}
			default:
				return false
			}
		}
		for _, value := range strings.Split(p.Value, ",") {
			var t time.Time
			var err error
			if tzid == Zone {
				local, parseErr := time.Parse("20060102T150405", value)
				if parseErr != nil || local.Second() != 0 || local.Format("20060102T150405") != value {
					return false
				}
				t, err = WallTime(local.Format("2006-01-02T15:04"))
			} else {
				t, err = time.Parse("20060102T150405Z", value)
				if err == nil && t.UTC().Format("20060102T150405Z") != value {
					return false
				}
			}
			key := t.UTC().Format(time.RFC3339)
			if err != nil || !completionInstantSupported(t) || seen[key] || len(seen) >= maxCompletions {
				return false
			}
			seen[key] = true
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
	if target == nil || !completionInstantSupported(*target) {
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
		// DATE exclusions are outside the UTC DATE-TIME completion profile.
		if timing.State == "unknown" || timing.Note != "" || timing.NextAllDay || timing.LastAllDay {
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
