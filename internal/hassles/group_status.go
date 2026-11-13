package hassles

import (
	"context"
	"encoding/json"
	"errors"
	"unicode/utf8"
)

// GroupStatus contains only local, validated operation metadata, never resource evidence.
// Callers must hold the same exclusive lease used by ExecuteGroup.
type GroupStatus struct {
	Fence   string        `json:"fence"`
	Pending *GroupPending `json:"pending,omitempty"`
}
type GroupPending struct {
	OperationID string       `json:"operationId"`
	Action      string       `json:"action"`
	Title       string       `json:"title"`
	Components  int          `json:"components"`
	Binding     GroupBinding `json:"-"`
	Location    Location     `json:"location"`
}

func InspectGroupState(ctx context.Context, store GroupStore) (GroupStatus, error) {
	s, err := groupLoad(ctx, store)
	if err != nil {
		return GroupStatus{}, err
	}
	out := GroupStatus{Fence: s.fence()}
	if s.Pending != nil {
		r, err := DecodeGroupRequest([]byte(s.Pending.Request))
		if err != nil {
			return GroupStatus{}, err
		}
		title := ""
		if r.Schedule != nil {
			title = r.Schedule.Title
		} else if len(s.Pending.Targets) > 0 && s.Pending.Targets[0].Original != nil {
			events := s.Pending.Targets[0].Original.Data.Events()
			if len(events) > 0 {
				title, _ = events[0].Props.Text("SUMMARY")
			}
		}
		out.Pending = &GroupPending{OperationID: r.OperationID, Action: r.Action, Title: title, Components: len(s.Pending.Targets), Binding: s.Pending.Binding, Location: s.Pending.Binding.Location}
	}
	return out, nil
}

// ReconcilePending uses only the original stored intent and a reader. No new intent
// or location can be supplied by a browser recovering after a reload.
func ReconcilePending(ctx context.Context, reader GroupReader, store GroupStore, expectedOperationID string, check GroupBindingCheck) (GroupOutcome, error) {
	ctx, cancel := context.WithTimeout(ctx, groupInvocationTimeout)
	defer cancel()
	s, err := groupLoad(ctx, store)
	if err != nil {
		return GroupOutcome{}, err
	}
	if s.Pending == nil {
		return GroupOutcome{}, ErrConflict
	}
	r, err := DecodeGroupRequest([]byte(s.Pending.Request))
	if err != nil {
		return GroupOutcome{}, err
	}
	if expectedOperationID != "" && expectedOperationID != r.OperationID {
		return GroupOutcome{}, ErrConflict
	}
	calls := &groupCalls{reader: reader}
	out, err := groupReconcile(ctx, calls, store, s, check)
	out.Calls = calls.count
	return out, err
}

type LocatedGroupRequest struct {
	Location Location     `json:"location"`
	Group    GroupRequest `json:"group"`
}

func DecodeLocatedGroupRequest(raw []byte) (LocatedGroupRequest, error) {
	var out LocatedGroupRequest
	if len(raw) > groupStoreLimit-groupEnvelopeBytes || !utf8.Valid(raw) || !json.Valid(raw) || !groupValidJSONSurrogates(raw) {
		return out, errors.New("invalid located group JSON")
	}
	fields, err := groupRequestFields(raw, "location", "group")
	if err != nil || len(fields) != 2 {
		return out, errors.New("location and group required")
	}
	location, err := groupRequestFields(fields["location"], "accountId", "calendarId")
	if err != nil || len(location) != 2 {
		return out, errors.New("exact accountId and calendarId required")
	}
	if err = json.Unmarshal(fields["location"], &out.Location); err != nil {
		return out, err
	}
	canonical, err := Collection(out.Location.CalendarID)
	if err != nil || canonical != out.Location.CalendarID || out.Location.AccountID == "" {
		return out, errors.New("canonical location required")
	}
	out.Group, err = DecodeGroupRequest(fields["group"])
	return out, err
}

func DecodeGroupRecoveryRequest(raw []byte) (string, error) {
	if !utf8.Valid(raw) || !json.Valid(raw) || !groupValidJSONSurrogates(raw) {
		return "", errors.New("invalid recovery JSON")
	}
	fields, err := groupRequestFields(raw, "expectedOperationId")
	if err != nil {
		return "", err
	}
	var id string
	if field, ok := fields["expectedOperationId"]; ok {
		if err = json.Unmarshal(field, &id); err != nil || !dailyPartsV2UUID(id) {
			return "", errors.New("invalid expected operation ID")
		}
	}
	return id, nil
}
