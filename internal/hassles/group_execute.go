package hassles

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"time"
)

const groupInvocationLimit = 512
const groupInvocationTimeout = 3 * time.Minute

type GroupBindingCheck func(context.Context, GroupBinding) error
type GroupOutcome struct {
	Fence   string
	Pending bool
	Settled bool
	Calls   int
}

type groupCalls struct {
	reader GroupReader
	count  int
}

func (b *groupCalls) take(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if b.count >= groupInvocationLimit {
		return errors.New("group invocation network call budget exceeded")
	}
	b.count++
	return nil
}
func (b *groupCalls) Snapshot(ctx context.Context, calendar string) (GroupSnapshot, error) {
	if err := b.take(ctx); err != nil {
		return GroupSnapshot{}, err
	}
	return b.reader.Snapshot(ctx, calendar)
}
func (b *groupCalls) Probe(ctx context.Context, calendar, href string) (GroupProbe, error) {
	if err := b.take(ctx); err != nil {
		return GroupProbe{}, err
	}
	return b.reader.Probe(ctx, calendar, href)
}
func groupCheck(ctx context.Context, check GroupBindingCheck, binding GroupBinding) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if check == nil {
		return errors.New("fresh actor/location check required")
	}
	return check(ctx, binding)
}
func groupSave(ctx context.Context, store GroupStore, s groupState) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	raw, err := groupEncodeState(s)
	if err != nil {
		return err
	}
	return store.Save(ctx, raw)
}
func groupLoad(ctx context.Context, store GroupStore) (groupState, error) {
	if err := ctx.Err(); err != nil {
		return groupState{}, err
	}
	raw, err := store.Load(ctx)
	if err != nil {
		return groupState{}, err
	}
	return groupDecodeState(raw)
}

// ExecuteGroup requires the caller's exclusive store lease. A matching pending
// request is read-only reconciliation, never resumption or retry of a write.
func ExecuteGroup(ctx context.Context, backend GroupBackend, store GroupStore, request GroupRequest, binding GroupBinding, check GroupBindingCheck, now time.Time) (out GroupOutcome, err error) {
	ctx, cancel := context.WithTimeout(ctx, groupInvocationTimeout)
	defer cancel()
	calls := &groupCalls{reader: backend}
	defer func() { out.Calls = calls.count }()
	s, err := groupLoad(ctx, store)
	if err != nil {
		return out, err
	}
	out.Fence = s.fence()
	out.Pending = s.Pending != nil
	raw, err := json.Marshal(request)
	if err != nil {
		return out, err
	}
	if _, err = DecodeGroupRequest(raw); err != nil {
		return out, err
	}
	if s.Pending != nil {
		if s.Pending.Request != string(raw) || s.Pending.Binding != binding {
			return out, ErrConflict
		}
		return groupReconcile(ctx, calls, store, s, check)
	}
	if request.Fence != s.fence() || s.Generation == math.MaxUint64 {
		return out, ErrConflict
	}
	if err = groupCheck(ctx, check, binding); err != nil {
		return out, err
	}
	snapshot, err := calls.Snapshot(ctx, binding.Location.CalendarID)
	if err != nil {
		return out, err
	}
	prepared, err := PrepareGroup(request, binding, snapshot, now)
	if err != nil {
		return out, err
	}
	op, err := prepared.operation()
	if err != nil {
		return out, err
	}
	for _, target := range op.Targets {
		probe, e := calls.Probe(ctx, binding.Location.CalendarID, target.Href)
		if e != nil {
			return out, e
		}
		if request.Action == "create" {
			if !probe.Absent {
				return out, ErrConflict
			}
		} else if probe.Absent || !groupSame(probe.Object, *target.Original) {
			return out, ErrConflict
		}
	}
	s.Pending = &op
	if _, err = groupMaximumRecordBytes(s); err != nil {
		return out, err
	}
	if err = groupCheck(ctx, check, binding); err != nil {
		return out, err
	}
	if err = groupSave(ctx, store, s); err != nil {
		return out, err
	}
	out.Pending = true
	for i := range op.Targets {
		snapshot, err = calls.Snapshot(ctx, binding.Location.CalendarID)
		if err != nil {
			return out, err
		}
		if err = groupVerifySnapshot(op, snapshot, i); err != nil {
			return out, err
		}
		if request.Action == "delete" {
			for j := 0; j < i; j++ {
				probe, e := calls.Probe(ctx, binding.Location.CalendarID, op.Targets[j].Href)
				if e != nil {
					return out, e
				}
				if !probe.Absent {
					return out, ErrConflict
				}
			}
			probe, e := calls.Probe(ctx, binding.Location.CalendarID, op.Targets[i].Href)
			if e != nil {
				return out, e
			}
			if probe.Absent || !groupSame(probe.Object, *op.Targets[i].Original) {
				return out, ErrConflict
			}
		}
		if err = groupCheck(ctx, check, binding); err != nil {
			return out, err
		}
		op.Targets[i].Phase = "entered"
		if err = groupSave(ctx, store, s); err != nil {
			return out, err
		}
		if err = groupCheck(ctx, check, binding); err != nil {
			return out, err
		}
		condition := "*"
		if request.Action == "delete" {
			condition = op.Targets[i].Original.ETag
		}
		if err = calls.take(ctx); err != nil {
			return out, err
		}
		if err = backend.Write(ctx, binding.Location.CalendarID, op.Targets[i].Href, condition, op.Targets[i].Body); err != nil {
			return out, err
		}
		op.Targets[i].Phase = "ack"
		if err = groupSave(ctx, store, s); err != nil {
			return out, err
		}
		probe, e := calls.Probe(ctx, binding.Location.CalendarID, op.Targets[i].Href)
		if e != nil {
			return out, e
		}
		if request.Action == "create" {
			if probe.Absent {
				return out, ErrConflict
			}
			if err = groupInitialReceipt(op.Targets[i], probe.Object); err != nil {
				return out, err
			}
			receipt := dailyPartsCloneObject(probe.Object)
			op.Targets[i].Receipt = &receipt
		} else if !probe.Absent {
			return out, ErrConflict
		}
		op.Targets[i].Phase = "verified"
		if err = groupSave(ctx, store, s); err != nil {
			return out, err
		}
	}
	return groupSettleDesired(ctx, calls, store, s, check)
}

// ReconcileGroup accepts a reader with NO write capability. It can settle only
// the complete desired state under the captured binding, never finish a prefix.
func ReconcileGroup(ctx context.Context, reader GroupReader, store GroupStore, request GroupRequest, binding GroupBinding, check GroupBindingCheck) (out GroupOutcome, err error) {
	ctx, cancel := context.WithTimeout(ctx, groupInvocationTimeout)
	defer cancel()
	calls := &groupCalls{reader: reader}
	defer func() { out.Calls = calls.count }()
	s, err := groupLoad(ctx, store)
	if err != nil {
		return out, err
	}
	out.Fence = s.fence()
	out.Pending = s.Pending != nil
	raw, err := json.Marshal(request)
	if err != nil {
		return out, err
	}
	if s.Pending == nil || s.Pending.Request != string(raw) || s.Pending.Binding != binding {
		return out, ErrConflict
	}
	return groupReconcile(ctx, calls, store, s, check)
}
func groupReconcile(ctx context.Context, calls *groupCalls, store GroupStore, s groupState, check GroupBindingCheck) (GroupOutcome, error) {
	return groupSettleDesired(ctx, calls, store, s, check)
}
func groupSettleDesired(ctx context.Context, calls *groupCalls, store GroupStore, s groupState, check GroupBindingCheck) (GroupOutcome, error) {
	out := GroupOutcome{Fence: s.fence(), Pending: true}
	op := s.Pending
	if err := groupCheck(ctx, check, op.Binding); err != nil {
		return out, err
	}
	snapshot, err := calls.Snapshot(ctx, op.Binding.Location.CalendarID)
	if err != nil {
		return out, err
	}
	request, err := DecodeGroupRequest([]byte(op.Request))
	if err != nil {
		return out, err
	}
	for i, target := range op.Targets {
		probe, err := calls.Probe(ctx, op.Binding.Location.CalendarID, target.Href)
		if err != nil {
			return out, err
		}
		if request.Action == "delete" {
			if !probe.Absent {
				return out, ErrConflict
			}
		} else {
			if probe.Absent {
				return out, ErrConflict
			}
			if target.Receipt != nil {
				if !groupSame(*target.Receipt, probe.Object) {
					return out, ErrConflict
				}
			} else {
				if err := groupInitialReceipt(target, probe.Object); err != nil {
					return out, err
				}
				receipt := dailyPartsCloneObject(probe.Object)
				op.Targets[i].Receipt = &receipt
				op.Targets[i].Phase = "verified"
				// Freeze read-only discoveries before another read or settlement can fail.
				if err := groupSave(ctx, store, s); err != nil {
					return out, err
				}
			}
		}
	}
	if err := groupVerifySnapshot(*op, snapshot, len(op.Targets)); err != nil {
		return out, err
	}
	if err := groupCheck(ctx, check, op.Binding); err != nil {
		return out, err
	}
	s.Pending = nil
	s.Generation++
	if err := groupSave(ctx, store, s); err != nil {
		return out, err
	}
	out.Fence = s.fence()
	out.Pending = false
	out.Settled = true
	return out, nil
}
