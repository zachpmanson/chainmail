package hassles

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestLocatedGroupStrictWireAndStatusOriginalReconcile(t *testing.T) {
	ctx := context.Background()
	store := groupTestStore(t)
	request := groupTestRequest(t, store, Schedule{Title: "Synthetic recovery", Start: "2027-01-04T09:00", TimeZone: Zone, Cadence: "hourly", Interval: 1})
	raw, _ := json.Marshal(LocatedGroupRequest{Location: groupTestBinding.Location, Group: request})
	if _, err := DecodeLocatedGroupRequest(raw); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{`null`, `{}`, strings.Replace(string(raw), `"location":`, `"location":null,"location":`, 1), strings.Replace(string(raw), `"accountId":`, `"AccountId":`, 1), strings.Replace(string(raw), `"calendarId":`, `"accountId":null,"calendarId":`, 1), string(raw) + `{}`} {
		if _, err := DecodeLocatedGroupRequest([]byte(bad)); err == nil {
			t.Fatal("accepted", bad)
		}
	}
	backend := groupTestBackend(nil)
	backend.afterWrite = func(int) error { return errors.New("ambiguous response after actual provider write") }
	// A finite one-role create can be recovered using only stored intent and a reader.
	request.Schedule.Until = "2027-01-04T09:01"
	_, err := ExecuteGroup(ctx, backend, store, request, groupTestBinding, groupTestCheck, time.Now())
	if err == nil {
		t.Fatal("expected ambiguous response")
	}
	status, err := InspectGroupState(ctx, store)
	if err != nil || status.Pending == nil || status.Pending.OperationID != request.OperationID || status.Pending.Title != request.Schedule.Title || status.Pending.Components != 1 || status.Pending.Binding != groupTestBinding {
		t.Fatal(status, err)
	}
	before := len(backend.writes)
	if _, err = ReconcilePending(ctx, backend, store, "dddd4444-4444-4444-8444-444444444444", groupTestCheck); err == nil {
		t.Fatal("wrong original accepted")
	}
	backend.deadline = time.Time{}
	out, err := ReconcilePending(ctx, backend, store, request.OperationID, groupTestCheck)
	if err != nil || !out.Settled || len(backend.writes) != before {
		t.Fatal(out, err)
	}
	status, err = InspectGroupState(ctx, store)
	if err != nil || status.Pending != nil || status.Fence == request.Fence {
		t.Fatal(status, err)
	}
	store.raw = []byte(`{}`)
	if _, err = InspectGroupState(ctx, store); err == nil {
		t.Fatal("corrupt status accepted")
	}
}
