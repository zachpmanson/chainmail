package hassles

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

type Mutation struct {
	Location Location `json:"location"`
	Action   string   `json:"action"`
	ID       string   `json:"id"`
	Revision string   `json:"revision"`
	Schedule Schedule `json:"schedule"`
}

type Result struct {
	Removed int    `json:"removed"`
	Message string `json:"message"`
}

// Caller serializes mutations and selection changes. Real ETags remain the
// cross-process/upstream concurrency boundary, not that local mutex.
func Mutate(ctx context.Context, b Backend, in Mutation, now time.Time) (Result, error) {
	result := Result{}
	objects, err := b.Objects(ctx, in.Location.CalendarID)
	if err != nil {
		return result, err
	}
	tasks, err := Inventory(objects, now)
	if err != nil {
		return result, err
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
