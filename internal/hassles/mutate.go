package hassles

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

type Mutation struct {
	Location  Location `json:"location"`
	Action    string   `json:"action"`
	ID        string   `json:"id"`
	Revision  string   `json:"revision"`
	Schedule  Schedule `json:"schedule"`
	AdoptID   *string  `json:"adoptId,omitempty"`
	AdoptHash *string  `json:"adoptHash,omitempty"`
	DryRun    *bool    `json:"dryRun,omitempty"`
}

// JSON null is not absence: an adoption-only flag must never be silently
// ignored on a destructive action. Keep strict unknown-field decoding here too.
func (in *Mutation) UnmarshalJSON(data []byte) error {
	type wire Mutation
	var value wire
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(&value); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	for name, raw := range fields {
		if (strings.EqualFold(name, "adoptId") || strings.EqualFold(name, "adoptHash") || strings.EqualFold(name, "dryRun")) && bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return fmt.Errorf("%s must not be null", name)
		}
	}
	*in = Mutation(value)
	return nil
}

type Result struct {
	Removed  int       `json:"removed"`
	Message  string    `json:"message"`
	Adoption *Adoption `json:"adoption,omitempty"`
}

// Caller serializes mutations and selection changes. Real ETags remain the
// cross-process/upstream concurrency boundary, not that local mutex.
func Mutate(ctx context.Context, b Backend, in Mutation, now time.Time) (Result, error) {
	result := Result{}
	if err := ValidateAdoptionFields(in); err != nil {
		return result, err
	}
	objects, err := b.Objects(ctx, in.Location.CalendarID)
	if err != nil {
		return result, err
	}
	tasks, err := Inventory(objects, now)
	if err != nil {
		return result, err
	}
	if in.Action == "adopt" {
		return adoptReminder(ctx, b, in, objects, tasks)
	}
	if in.Action == "create" {
		c, err := NewCalendar(in.ID, in.Schedule, now)
		if err != nil {
			return result, err
		}
		body, err := Encode(c)
		if err != nil {
			return result, err
		}
		href := in.Location.CalendarID + "hassle-" + strings.ToLower(in.ID) + ".ics"
		// Never overwrite an idempotency key with a different payload.
		for _, task := range tasks {
			if task.ID == "task:"+strings.ToLower(in.ID) {
				if task.Resources == 1 && task.Writable && task.members[0].Path == href && task.Schedule != nil && *task.Schedule == in.Schedule {
					result.Message = "Already created"
					return result, nil
				}
				return result, ErrConflict
			}
		}
		err = b.Write(ctx, in.Location.CalendarID, href, "*", body)
		if err != nil {
			return result, err
		}
		result.Message = "Created"
		return result, nil
	}
	var task *Task
	for i := range tasks {
		if tasks[i].ID == in.ID {
			task = &tasks[i]
			break
		}
	}
	if task == nil || task.Revision != in.Revision {
		return result, ErrConflict
	}
	if !task.Writable {
		return result, errors.New(task.Restriction)
	}
	switch in.Action {
	case "edit":
		if task.Schedule == nil || task.Resources != 1 {
			return result, errors.New("complex/imported schedule cannot be safely edited")
		}
		o := task.members[0]
		if why := scheduleTimezoneRestriction(o.Data, &in.Schedule); why != "" {
			return result, errors.New(why)
		}
		if err := ApplySchedule(o.Data.Events()[0], in.Schedule, now); err != nil {
			return result, err
		}
		body, err := Encode(o.Data)
		if err != nil {
			return result, err
		}
		if err = b.Write(ctx, in.Location.CalendarID, o.Path, o.ETag, body); err != nil {
			return result, err
		}
		result.Message = "Updated"
	case "delete", "done":
		for _, o := range task.members {
			if err := b.Write(ctx, in.Location.CalendarID, o.Path, o.ETag, ""); err != nil {
				return result, fmt.Errorf("removed %d of %d resources; refresh inventory before retrying: %w", result.Removed, task.Resources, err)
			}
			result.Removed++
		}
		result.Message = "Removed all reminder resources"
	default:
		return result, errors.New("unsupported task action")
	}
	return result, nil
}
