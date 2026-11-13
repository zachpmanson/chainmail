package main

import (
	"context"
	"errors"
	"fmt"
	"github.com/emersion/go-ical"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/emersion/go-webdav/caldav"
	"github.com/zachpmanson/chainmail/internal/gmailclient"
	"github.com/zachpmanson/chainmail/internal/hassles"
)

type leaseFixture struct {
	mu        sync.Mutex
	raw       []byte
	active    int
	releases  int
	fail      bool
	afterSave bool
	saves     int
}

func (f *leaseFixture) Load(ctx context.Context) ([]byte, error) {
	if f.active != 1 {
		return nil, errors.New("load outside lease")
	}
	return append([]byte(nil), f.raw...), ctx.Err()
}
func (f *leaseFixture) Save(ctx context.Context, raw []byte) error {
	if f.active != 1 {
		return errors.New("save outside lease")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	f.raw = append([]byte(nil), raw...)
	f.saves++
	if f.afterSave {
		return errors.New("ambiguous saved acknowledgement")
	}
	return nil
}
func injectHassleLease(t *testing.T, h *harness) *leaseFixture {
	t.Helper()
	raw, err := hassles.NewGroupState()
	if err != nil {
		t.Fatal(err)
	}
	f := &leaseFixture{raw: raw}
	h.leaseHassles = func(ctx context.Context) (hassles.GroupStore, func(), error) {
		if f.fail || !f.mu.TryLock() {
			return nil, nil, errors.New("busy fixture")
		}
		f.active++
		return f, func() { f.active--; f.releases++; f.mu.Unlock() }, nil
	}
	return f
}
func (f *calendarFixture) Snapshot(ctx context.Context, _ string) (hassles.GroupSnapshot, error) {
	f.calls++
	return hassles.GroupSnapshot{Objects: append([]caldav.CalendarObject(nil), f.objects...)}, ctx.Err()
}
func (f *calendarFixture) Probe(ctx context.Context, _, href string) (hassles.GroupProbe, error) {
	f.calls++
	for _, o := range f.objects {
		if o.Path == href {
			return hassles.GroupProbe{Object: o}, ctx.Err()
		}
	}
	return hassles.GroupProbe{Absent: true}, ctx.Err()
}

const groupOperationID = "aaaa1111-1111-4111-8111-111111111111"
const groupIncarnation = "bbbb2222-2222-4222-8222-222222222222"

func groupHTTPCreate(t *testing.T, h *harness, loc hassles.Location, schedule hassles.Schedule) hassles.LocatedGroupRequest {
	t.Helper()
	status := decode[hassleRecovery](t, h.do(t, "GET", "/v1/hassles/recovery", nil))
	if status.Status != "ready" {
		t.Fatal(status)
	}
	return hassles.LocatedGroupRequest{Location: loc, Group: hassles.GroupRequest{Action: "create", OperationID: groupOperationID, Task: hassleUUID, Incarnation: groupIncarnation, Fence: status.Fence, Schedule: &schedule}}
}
func groupHTTPPost(t *testing.T, h *harness, in hassles.LocatedGroupRequest) *response {
	t.Helper()
	return h.do(t, "POST", "/v1/hassles/groups", hassleJSON(t, in))
}

func TestHasslesGroupsHTTPCreateInventoryDelete(t *testing.T) {
	for _, tc := range []struct {
		name     string
		interval float64
		work     bool
		until    string
		count    int
	}{{"full", 1, false, "", 24}, {"half", 0.5, false, "", 24}, {"work", 1, true, "", 8}, {"finite", 1, false, "2027-01-04T09:01", 1}} {
		t.Run(tc.name, func(t *testing.T) {
			h, f, loc := hassleHarness(t)
			lease := injectHassleLease(t, h)
			chooseHassles(t, h, loc)
			in := groupHTTPCreate(t, h, loc, hassles.Schedule{Title: "Synthetic group", Start: "2027-01-04T09:00", TimeZone: hassles.Zone, Cadence: "hourly", Interval: tc.interval, WorkHours: tc.work, Until: tc.until})
			r := groupHTTPPost(t, h, in)
			if r.status != 200 || len(f.objects) != tc.count {
				t.Fatalf("create %d %s resources %d", r.status, r.body, len(f.objects))
			}
			loadAPI(t).assert(t, "HassleGroupResult", r.body)
			// Provider-owned physical identities need not equal planned names. Delete must
			// use the fresh captured actual href, UID and ETag, never reconstruct them.
			for i := range f.objects {
				f.objects[i].Path = loc.CalendarID + fmt.Sprintf("provider-%d.ics", i)
				f.objects[i].ETag = fmt.Sprintf(`"provider-tag-%d"`, i)
				f.objects[i].Data.Events()[0].Props.SetText("UID", fmt.Sprintf("provider-uid-%d", i))
			}
			tasks := hassleList(t, h)
			if len(tasks) != 1 {
				t.Fatal(tasks)
			}
			task := tasks[0]
			if task.Format != "daily-parts-v2" || task.ID != "v2:"+hassleUUID || task.Writable || task.Schedule != nil || task.Instance != nil || task.Capabilities == nil || !task.Capabilities.Delete || task.Capabilities.Edit || task.DisplaySchedule == nil || task.DisplaySchedule.Interval != tc.interval || task.DisplaySchedule.WorkHours != tc.work || task.DisplaySchedule.Until != tc.until || task.Timing.State != "unknown" {
				t.Fatalf("unsafe/incorrect card %+v", task)
			}
			writes := f.writes
			// Retired fence cannot be refreshed into a second operation, even after external deletion.
			if r = groupHTTPPost(t, h, in); r.status != 409 || f.writes != writes {
				t.Fatal("stale fence wrote", r.status, string(r.body))
			}
			status := decode[hassleRecovery](t, h.do(t, "GET", "/v1/hassles/recovery", nil))
			in.Group = hassles.GroupRequest{Action: "delete", OperationID: "cccc3333-3333-4333-8333-333333333333", Task: hassleUUID, Incarnation: groupIncarnation, Fence: status.Fence, Revision: task.Revision}
			if r = groupHTTPPost(t, h, in); r.status != 200 || len(f.objects) != 0 || f.writes != writes+tc.count {
				t.Fatal("delete failed", r.status, string(r.body))
			}
			if lease.active != 0 || lease.releases == 0 {
				t.Fatal("lease leaked")
			}
		})
	}
}

func TestHasslesGroupsPendingGlobalGatesAndOriginalRecovery(t *testing.T) {
	h, f, loc := hassleHarness(t)
	lease := injectHassleLease(t, h)
	chooseHassles(t, h, loc)
	in := groupHTTPCreate(t, h, loc, hassles.Schedule{Title: "Pending synthetic", Start: "2027-01-04T09:00", TimeZone: hassles.Zone, Cadence: "hourly", Interval: 1})
	f.deny = true
	r := groupHTTPPost(t, h, in)
	if r.status != 409 {
		t.Fatal(r.status, string(r.body))
	}
	calls, writes := f.calls, f.writes
	status := decode[hassleRecovery](t, h.do(t, "GET", "/v1/hassles/recovery", nil))
	if status.Status != "pending" || status.Pending == nil || status.Pending.OperationID != groupOperationID || status.Pending.Components != 24 || status.Pending.Title != "Pending synthetic" || f.calls != calls {
		t.Fatal("local status", status)
	}
	if strings.Contains(string(hassleJSON(t, status)), "ETag") || strings.Contains(string(hassleJSON(t, status)), "ServiceUID") {
		t.Fatal("secret evidence exposed")
	}
	for _, body := range []map[string]any{{"location": loc, "previous": loc, "dedicated": true}} {
		if r = h.do(t, "POST", "/v1/hassles/location", hassleJSON(t, body)); r.status != 409 {
			t.Fatal("pending selection", r.status)
		}
	}
	legacy := hassles.Mutation{Location: loc, Action: "create", ID: "dddd4444-4444-4444-8444-444444444444", Schedule: hassles.Schedule{Title: "legacy", Start: "2027-01-04T09:00", TimeZone: hassles.Zone, Cadence: "daily", Interval: 1}}
	if r = h.do(t, "POST", "/v1/hassles", hassleJSON(t, legacy)); r.status != 409 {
		t.Fatal("pending legacy bypass", r.status)
	}
	if r = h.do(t, "POST", "/auth/accounts/personal/disconnect", nil); r.status != 409 {
		t.Fatal("bound disconnect allowed", r.status, string(r.body))
	}
	path, _ := gmailclient.AccountTokenPath("personal")
	if _, err := os.Stat(path); err != nil {
		t.Fatal("token mutated", err)
	}
	f.deny = false
	if r = groupHTTPPost(t, h, in); r.status != 409 || f.writes != writes {
		t.Fatal("repeat resumed writes", r.status, string(r.body))
	}
	if r = h.do(t, "POST", "/v1/hassles/recovery", []byte(`{"expectedOperationId":"dddd4444-4444-4444-8444-444444444444"}`)); r.status != 409 {
		t.Fatal("wrong recovery operation accepted")
	}
	if r = h.do(t, "POST", "/v1/hassles/recovery", []byte(`{}`)); r.status != 409 || f.writes != writes {
		t.Fatal("recovery wrote")
	}
	// Status survives a browser reload and offline original account, with no Calendar I/O.
	_ = os.Remove(path)
	calls = f.calls
	status = decode[hassleRecovery](t, h.do(t, "GET", "/v1/hassles/recovery", nil))
	if status.Status != "pending" || f.calls != calls {
		t.Fatal("offline recovery status unavailable")
	}
	if lease.active != 0 {
		t.Fatal("lease leaked")
	}
}

func TestHasslesStoreFailureAndBusyHTTPGates(t *testing.T) {
	for _, mode := range []string{"missing", "corrupt", "busy", "ambiguous"} {
		t.Run(mode, func(t *testing.T) {
			h, f, loc := hassleHarness(t)
			lease := injectHassleLease(t, h)
			chooseHassles(t, h, loc)
			in := groupHTTPCreate(t, h, loc, hassles.Schedule{Title: "Synthetic", Start: "2027-01-04T09:00", TimeZone: hassles.Zone, Cadence: "hourly", Interval: 1})
			switch mode {
			case "missing":
				h.leaseHassles = nil
			case "corrupt":
				lease.raw = []byte(`{}`)
			case "busy":
				lease.fail = true
			case "ambiguous":
				lease.afterSave = true
			}
			if r := groupHTTPPost(t, h, in); r.status != 409 || f.writes != 0 {
				t.Fatal("bad store wrote", r.status, string(r.body))
			}
			status := decode[hassleRecovery](t, h.do(t, "GET", "/v1/hassles/recovery", nil))
			expected := "unavailable"
			if mode == "ambiguous" {
				expected = "pending"
			}
			if status.Status != expected {
				t.Fatal(status)
			}
			if r := h.do(t, "POST", "/v1/hassles/location", hassleJSON(t, map[string]any{"location": loc, "previous": loc, "dedicated": true})); r.status != 409 {
				t.Fatal("bad store selection", r.status)
			}
			legacy := hassles.Mutation{Location: loc, Action: "create", ID: hassleUUID, Schedule: hassles.Schedule{Title: "Synthetic", Start: "2027-01-04T09:00", TimeZone: hassles.Zone, Cadence: "daily", Interval: 1}}
			if r := h.do(t, "POST", "/v1/hassles", hassleJSON(t, legacy)); r.status != 409 || f.writes != 0 {
				t.Fatal("legacy store bypass", r.status)
			}
			if r := h.do(t, "POST", "/auth/accounts/personal/disconnect", nil); r.status != 409 {
				t.Fatal("bound unavailable disconnect", r.status)
			}
			if lease.active != 0 {
				t.Fatal("lease leaked")
			}
		})
	}
	h, _, _ := hassleHarness(t)
	h.hasslesMu.Lock()
	start := time.Now()
	r := h.do(t, "GET", "/v1/hassles/recovery", nil)
	h.hasslesMu.Unlock()
	if r.status != 200 || !strings.Contains(string(r.body), `"busy"`) || time.Since(start) > time.Second {
		t.Fatal("status blocked", r.status, string(r.body))
	}
	r = h.do(t, "GET", "/v1/hassles/recovery", nil)
	if !strings.Contains(string(r.body), `"ready"`) {
		t.Fatal("busy status stuck", string(r.body))
	}
}

func TestHasslesLocatedStrictWireHTTPAndFreshSelection(t *testing.T) {
	h, f, loc := hassleHarness(t)
	chooseHassles(t, h, loc)
	in := groupHTTPCreate(t, h, loc, hassles.Schedule{Title: "Synthetic", Start: "2027-01-04T09:00", TimeZone: hassles.Zone, Cadence: "hourly", Interval: 1})
	raw := string(hassleJSON(t, in))
	for _, bad := range []string{strings.Replace(raw, `"location":`, `"Location":`, 1), strings.Replace(raw, `"location":`, `"location":null,"location":`, 1), strings.Replace(raw, `"accountId":`, `"AccountId":`, 1), strings.Replace(raw, `"calendarId":`, `"extra":1,"calendarId":`, 1), raw + `{}`, strings.Replace(raw, `"group":`, `"group":null,"group":`, 1)} {
		if r := h.do(t, "POST", "/v1/hassles/groups", []byte(bad)); r.status != 400 {
			t.Fatal("non-strict located wire", r.status, bad, string(r.body))
		}
	}
	in.Location.CalendarID = otherCalendar
	if r := groupHTTPPost(t, h, in); r.status != 409 || f.writes != 0 {
		t.Fatal("stale location accepted", r.status)
	}
	if f.writes != 0 {
		t.Fatal(fmt.Sprint("invalid requests wrote ", f.writes))
	}
}

func TestHasslesReservedNamespaceEveryLegacyHTTPAction(t *testing.T) {
	h, f, loc := hassleHarness(t)
	chooseHassles(t, h, loc)
	c, err := hassles.NewCalendar(hassleUUID, hassles.Schedule{Title: "Synthetic", Start: "2027-01-04T09:00", TimeZone: hassles.Zone, Cadence: "daily", Interval: 1}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	// A reserved VTODO sharing a v1 task hint denies the ENTIRE old task,
	// including the apparently genuine v1 sibling. No survivor promotion.
	todo := ical.NewComponent("VTODO")
	todo.Props.SetText("UID", "hassle-parts-v2-"+hassleUUID)
	todo.Props.SetText("DESCRIPTION", "[docket]\n[hassle:parts:v99 task="+hassleUUID+"]")
	reserved := ical.NewCalendar()
	reserved.Children = append(reserved.Children, todo)
	f.objects = []caldav.CalendarObject{{Path: loc.CalendarID + "legacy.ics", ETag: `"v1"`, Data: c}, {Path: loc.CalendarID + "reserved.ics", ETag: `"reserved"`, Data: reserved}}
	legacy, _ := hassles.Inventory(f.objects, time.Now())
	if len(legacy) != 1 {
		t.Fatal(legacy)
	}
	tasks := hassleList(t, h)
	for _, task := range tasks {
		if task.ID == legacy[0].ID && (task.Writable || task.Instance != nil) {
			t.Fatal("survivor promoted", task)
		}
	}
	for _, action := range []string{"edit", "done", "delete", "adopt", "done-instance"} {
		in := hassles.Mutation{Location: loc, Action: action, ID: legacy[0].ID, Revision: legacy[0].Revision, Schedule: hassles.Schedule{Title: "Edited", Start: "2027-01-04T09:00", TimeZone: hassles.Zone, Cadence: "daily", Interval: 1}}
		if action == "done-instance" {
			in.Schedule = hassles.Schedule{}
			at := "2027-01-03T22:00:00Z"
			token := strings.Repeat("a", 64)
			dry := true
			in.InstanceAt = &at
			in.InstanceToken = &token
			in.DryRun = &dry
		}
		if action == "adopt" {
			c.Events()[0].Props.SetText("DESCRIPTION", "[docket]")
			uid, _ := c.Events()[0].Props.Text("UID")
			todo.Props.SetText("UID", uid)
			imported, _ := hassles.Inventory(f.objects, time.Now())
			in.ID = imported[0].ID
			in.Revision = imported[0].Revision
			in.Schedule = hassles.Schedule{}
			adoptID := hassleUUID
			in.AdoptID = &adoptID
			dry := true
			in.DryRun = &dry
		}
		r := h.do(t, "POST", "/v1/hassles", hassleJSON(t, in))
		if r.status == 200 || f.writes != 0 {
			t.Fatal("legacy reserved write", action, r.status, string(r.body))
		}
	}
}

func TestHasslesPendingSameIdentityReconnectNoReverseLock(t *testing.T) {
	h, f, loc := hassleHarness(t)
	chooseHassles(t, h, loc)
	in := groupHTTPCreate(t, h, loc, hassles.Schedule{Title: "Pending", Start: "2027-01-04T09:00", TimeZone: hassles.Zone, Cadence: "hourly", Interval: 1})
	f.deny = true
	if r := groupHTTPPost(t, h, in); r.status != 409 {
		t.Fatal(r.status)
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	state, _ := beginFakeLogin(t, h, "?account=personal")
	fakeOAuth(t, `{"email":"offline@example.test"}`, 200, nil)
	// Holding the Hassles lock cannot obstruct the authMu-only callback.
	h.hasslesMu.Lock()
	r := h.do(t, "GET", "/?code=fake&state="+url.QueryEscape(state), nil)
	h.hasslesMu.Unlock()
	if r.status != 200 {
		t.Fatal("same identity reconnect refused", r.status, string(r.body))
	}
	status := decode[hassleRecovery](t, h.do(t, "GET", "/v1/hassles/recovery", nil))
	if status.Status != "pending" {
		t.Fatal("reconnect retired operation", status)
	}
}
