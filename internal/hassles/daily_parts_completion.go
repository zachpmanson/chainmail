package hassles

import "time"

// Only called for a completely admitted roster. The work-hours compiler
// partitions the Melbourne civil grid by hour; every supported interval
// divides a day, and each member starts at its first eligible slot. The
// logical work-hours iterator therefore visits exactly the union of the
// physical DAILY rules. Do not use it for all-day grids that can cross a DST
// gap or fold at an occurrence's wall time.
func applyDailyPartsTiming(task *Task, group DailyPartsGroupModel, now time.Time) {
	if !task.Schedule.WorkHours {
		return
	}
	excluded := map[int64]bool{}
	for _, member := range group.Members {
		for _, at := range member.Exclusions {
			excluded[at.Unix()] = true
		}
	}
	timing, ok := profileTiming(*task.Schedule, group.Plan.start, now, excluded)
	if !ok {
		return
	}
	var selected *DailyPartsPhysical
	for _, at := range []*time.Time{timing.Last, timing.Next} {
		if at == nil {
			continue
		}
		var witness *DailyPartsPhysical
		for i := range group.Members {
			member := &group.Members[i]
			if group.Plan.SupportsCompletion(member.MemberID, *at) {
				if witness != nil {
					return
				}
				witness = member
			}
		}
		if witness == nil {
			return
		}
		selected = witness // Next takes precedence over Last.
	}
	task.Timing = timing
	if selected == nil {
		return
	}
	if len(selected.Exclusions) >= maxCompletions {
		task.InstanceRestriction = "This instance only unavailable: Calendar resource has reached the completion limit"
		return
	}
	at := timing.Next
	if at == nil {
		at = timing.Last
	}
	instant := at.UTC().Format(time.RFC3339)
	task.Instance = &Instance{At: instant, Token: instanceToken(selected.Href, selected.UID, instant)}
	task.InstanceRestriction = ""
}
