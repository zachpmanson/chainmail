package hassles

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/emersion/go-ical"
	"github.com/emersion/go-webdav/caldav"
	"github.com/google/uuid"
)

// SelectedInventory retains legacy authority on the full snapshot before hiding
// physical v2 rows. The caller supplies the selected Calendar context.
func SelectedInventory(objects []caldav.CalendarObject, loc Location, now time.Time) ([]Task, error) {
	reserved := false
	for _, o := range objects {
		if o.Data != nil {
			for _, e := range o.Data.Events() {
				for _, p := range e.Props["DESCRIPTION"] {
					text, _ := p.Text()
					reserved = reserved || dailyPartsReserved(text)
				}
			}
		}
	}
	if !reserved {
		return Inventory(objects, now)
	}
	model, err := readDailyPartsV2Groups(objects, loc, now, true)
	if err != nil {
		return nil, err
	}
	legacy, err := Inventory(objects, now)
	if err != nil {
		return nil, err
	}
	handled := map[string]bool{}
	for _, href := range model.HandledHrefs {
		handled[href] = true
	}
	out := []Task{}
	for _, task := range legacy {
		hide := false
		for _, o := range task.members {
			hide = hide || handled[o.Path]
		}
		if !hide {
			out = append(out, task)
		}
	}
	for _, group := range model.Groups {
		task := Task{ID: "v2:" + group.Task, Revision: group.Revision, Title: group.Title, Resources: len(group.Members), Writable: group.StructuralReady, Timing: Timing{State: "unknown", Note: "Next reminder time is approximate; view Calendar for details."}, InstanceRestriction: "Only rename and whole-reminder deletion are available", active: true}
		if task.Title == "" {
			task.Title = "Unsupported recurring reminder"
		}
		task.Restriction = strings.Join(group.Restrictions, "; ")
		if group.Plan != nil {
			logic := group.Plan.Logic()
			task.Schedule = &Schedule{Title: task.Title, Start: logic.Start, TimeZone: logic.TimeZone, Cadence: "hourly", Interval: float64(logic.Minutes) / 60, WorkHours: logic.WorkHours, Until: logic.Until}
			task.Reminder = fmt.Sprintf("Every %g hours", task.Schedule.Interval)
			if logic.Until != "" {
				task.Reminder += " · Until " + logic.Until + " Melbourne"
			}
			if task.Writable && len(group.Members) != len(group.Plan.Members()) {
				task.Restriction = fmt.Sprintf("Partial reminder: %d of %d Calendar resources remain. Rename or delete the known remaining resources.", len(group.Members), len(group.Plan.Members()))
			}
		}
		mixed := false
		for _, member := range group.Members {
			o := member.Original()
			task.members = append(task.members, o)
			if o.Data != nil && len(o.Data.Events()) == 1 {
				title, _ := o.Data.Events()[0].Props.Text("SUMMARY")
				mixed = mixed || title != group.Title
			}
		}
		if mixed {
			task.Restriction += " Titles differ across Calendar resources; rename to set one title."
		}
		out = append(out, task)
	}
	return out, nil
}

func createDailyParts(ctx context.Context, b Backend, in Mutation, objects []caldav.CalendarObject, now time.Time) (Result, error) {
	result := Result{}
	taskID := strings.ToLower(in.ID)
	// Any existing claim, including an incomplete or unsupported one, consumes
	// this draft UUID. Never start another incarnation on a repeated POST.
	for _, o := range objects {
		if o.Data == nil {
			continue
		}
		for _, e := range o.Data.Events() {
			for _, prop := range e.Props["DESCRIPTION"] {
				text, _ := prop.Text()
				for _, hint := range dailyPartsHints(text) {
					if hint == taskID {
						return result, ErrConflict
					}
				}
			}
		}
	}
	plan, err := CompileDailyPartsV2(in.Schedule, taskID, uuid.NewString())
	if err != nil {
		return result, err
	}
	for i, member := range plan.Members() {
		c := ical.NewCalendar()
		c.Props.SetText("VERSION", "2.0")
		c.Props.SetText("PRODID", "-//Chainmail Hassles//EN")
		e := ical.NewEvent()
		e.Props.SetText("UID", "hassle-"+member.MemberID)
		e.Props.SetText("SUMMARY", strings.TrimSpace(in.Schedule.Title))
		description, err := plan.Description(member.MemberID, false)
		if err != nil {
			return result, err
		}
		e.Props.SetText("DESCRIPTION", description)
		e.Props.SetText("TRANSP", "TRANSPARENT")
		e.Props.SetDateTime("DTSTAMP", now.UTC())
		start := ical.NewProp("DTSTART")
		start.Value = strings.ReplaceAll(strings.ReplaceAll(member.AnchorLocal, "-", ""), ":", "") + "00"
		start.Params.Set("TZID", Zone)
		e.Props.Set(start)
		rule := ical.NewProp("RRULE")
		rule.Value = member.RRule
		e.Props.Set(rule)
		duration := ical.NewProp("DURATION")
		duration.Value = member.Duration
		e.Props.Set(duration)
		alarm := ical.NewComponent("VALARM")
		alarm.Props.SetText("ACTION", "DISPLAY")
		alarm.Props.SetText("DESCRIPTION", "Reminder")
		trigger := ical.NewProp("TRIGGER")
		trigger.Value = "PT0S"
		alarm.Props.Set(trigger)
		e.Children = append(e.Children, alarm)
		c.Children = append(c.Children, e.Component)
		body, err := Encode(c)
		if err == nil {
			err = b.Write(ctx, in.Location.CalendarID, in.Location.CalendarID+"hassle-"+member.MemberID+".ics", "*", body)
		}
		if err != nil {
			return result, fmt.Errorf("created %d of %d resources; Changes may have partially applied; refresh Calendar: %w", i, len(plan.Members()), err)
		}
	}
	result.Message = "Created recurring reminder"
	return result, nil
}

func renameReminder(ctx context.Context, b Backend, in Mutation, task *Task) (Result, error) {
	result := Result{}
	title := strings.TrimSpace(in.Title)
	if title == "" || len(title) > 500 || !utf8.ValidString(title) || strings.ContainsAny(title, "\r\n") {
		return result, errors.New("enter a one-line title (maximum 500 bytes)")
	}
	for i, original := range task.members {
		o := dailyPartsCloneObject(original)
		if o.Data == nil || len(o.Data.Events()) != 1 {
			return result, errors.New("title editing unavailable for complex events")
		}
		summary := o.Data.Events()[0].Props.Get("SUMMARY")
		if summary == nil {
			return result, errors.New("title editing unavailable without SUMMARY")
		}
		summary.SetText(title)
		body, err := Encode(o.Data)
		if err == nil {
			err = b.Write(ctx, in.Location.CalendarID, o.Path, o.ETag, body)
		}
		if err != nil {
			return result, fmt.Errorf("renamed %d of %d resources; Changes may have partially applied; refresh Calendar: %w", i, task.Resources, err)
		}
	}
	result.Message = "Renamed reminder"
	return result, nil
}
