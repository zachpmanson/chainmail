package hassles

import (
	"errors"
	"strings"
	"time"
)

type GroupCapabilities struct {
	Delete   bool `json:"delete"`
	Edit     bool `json:"edit"`
	Complete bool `json:"complete"`
	Instance bool `json:"instance"`
}

// locatedInventory keeps legacy authority on the full snapshot. Suppression is
// display-only: a surviving v1 sibling can never acquire authority by omission.
func locatedInventory(snapshot GroupSnapshot, loc Location, now time.Time) ([]Task, DailyPartsGroupInventoryModel, map[string]bool, error) {
	model, err := ReadDailyPartsV2Groups(snapshot.Objects, loc, now)
	if err != nil {
		return nil, model, nil, err
	}
	legacy, err := Inventory(snapshot.Objects, now)
	if err != nil {
		return nil, model, nil, err
	}
	reservedPaths, reservedHints, reservedUIDs := map[string]bool{}, map[string]bool{}, map[string]bool{}
	uids := map[string]map[string]int{}
	for _, p := range model.Physical {
		counts := map[string]int{}
		uncertain := false
		copy := DailyPartsPhysical{Href: p.Href}
		if p.original.Data != nil {
			groupEvidence(p.original.Data.Component, counts, &uncertain, &copy)
		}
		uids[p.Href] = counts
		if p.Reserved {
			reservedPaths[p.Href] = true
			for _, hint := range p.TaskHints {
				reservedHints[hint] = true
			}
			for uid := range counts {
				reservedUIDs[uid] = true
			}
		}
	}
	denied := map[string]bool{}
	for i := range legacy {
		t := &legacy[i]
		for _, o := range t.members {
			if reservedPaths[o.Path] {
				denied[t.ID] = true
			}
			for uid := range uids[o.Path] {
				if reservedUIDs[uid] {
					denied[t.ID] = true
				}
			}
			for _, p := range model.Physical {
				if p.Href == o.Path {
					for _, hint := range p.TaskHints {
						if reservedHints[hint] {
							denied[t.ID] = true
						}
					}
				}
			}
		}
		if denied[t.ID] {
			t.Writable = false
			t.Schedule = nil
			t.Instance = nil
			t.Restriction = "Reserved DAILY-parts namespace: legacy writes unavailable"
		}
	}
	return legacy, model, denied, nil
}

// LocatedInventory accepts write availability only from the server's fresh
// selected actor and healthy empty leased store. Structural readiness is not ACL proof.
func LocatedInventory(snapshot GroupSnapshot, loc Location, now time.Time, writesAvailable bool) ([]Task, error) {
	legacy, model, _, err := locatedInventory(snapshot, loc, now)
	if err != nil {
		return nil, err
	}
	handled := map[string]bool{}
	for _, href := range model.HandledHrefs {
		handled[href] = true
	}
	out := make([]Task, 0, len(legacy)+len(model.Groups))
	for _, t := range legacy {
		allHandled := len(t.members) > 0
		for _, o := range t.members {
			allHandled = allHandled && handled[o.Path]
		}
		if allHandled {
			continue
		}
		if !writesAvailable {
			t.Writable = false
			t.Instance = nil
			t.Restriction = "Hassles writes unavailable: operation store is not ready"
		}
		out = append(out, t)
	}
	for _, g := range model.Groups {
		t := Task{ID: "v2:" + g.Task, Revision: g.Revision, Title: g.Title, Resources: len(g.Members), Format: "daily-parts-v2", GroupStatus: "read-only", Restriction: strings.Join(g.Restrictions, "; "), Timing: g.Timing, InstanceRestriction: dailyPartsTimingDeferred, Reminder: dailyPartsTimingDeferred, Capabilities: &GroupCapabilities{}}
		if t.Title == "" {
			t.Title = "Unrecognized DAILY-parts group"
		}
		if g.Plan != nil {
			logic := g.Plan.Logic()
			t.Incarnation = g.Plan.Incarnation()
			t.DisplaySchedule = &Schedule{Title: g.Title, Start: logic.Start, TimeZone: logic.TimeZone, Cadence: "hourly", Interval: float64(logic.Minutes) / 60, WorkHours: logic.WorkHours, Until: logic.Until}
		}
		if g.StructuralReady && writesAvailable {
			t.GroupStatus = "ready"
			t.Capabilities.Delete = true
		} else if t.Restriction == "" {
			t.Restriction = "Hassles writes unavailable: operation store is not ready"
		}
		// Writable deliberately remains false: the legacy UI must never enable v1 controls.
		out = append(out, t)
	}
	return out, nil
}

// GuardLegacyMutation is HTTP policy; the legacy core remains a reference implementation.
func GuardLegacyMutation(snapshot GroupSnapshot, in Mutation, now time.Time) error {
	if (in.Action == "create" || in.Action == "edit") && in.Schedule.Cadence == "hourly" {
		return errors.New("hourly writes require DAILY-parts groups; refresh the client")
	}
	_, model, denied, err := locatedInventory(snapshot, in.Location, now)
	if err != nil {
		return err
	}
	if strings.HasPrefix(in.ID, "v2:") || denied[in.ID] {
		return errors.New("reserved group cannot be changed through legacy task actions")
	}
	if in.Action == "create" || in.Action == "adopt" {
		id := strings.ToLower(in.ID)
		if in.Action == "adopt" && in.AdoptID != nil {
			id = strings.ToLower(*in.AdoptID)
		}
		for _, p := range model.Physical {
			if !p.Reserved {
				continue
			}
			for _, hint := range p.TaskHints {
				if hint == id {
					return ErrConflict
				}
			}
			counts := map[string]int{}
			uncertain := false
			physical := DailyPartsPhysical{Href: p.Href}
			if p.original.Data != nil {
				groupEvidence(p.original.Data.Component, counts, &uncertain, &physical)
			}
			if counts["hassle-"+id] > 0 || p.Href == in.Location.CalendarID+"hassle-"+id+".ics" {
				return ErrConflict
			}
		}
	}
	return nil
}
