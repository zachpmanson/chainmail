package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-ical"
	"github.com/emersion/go-webdav/caldav"
	"github.com/zachpmanson/chainmail/internal/corpus"
	"github.com/zachpmanson/chainmail/internal/gmailclient"
	"github.com/zachpmanson/chainmail/internal/hassles"
)

type hookedCalendar struct {
	*calendarFixture
	snapshot  func(context.Context, string) (hassles.GroupSnapshot, error)
	write     func(context.Context, string, string, string, string) error
	calendars func(context.Context) ([]hassles.Calendar, error)
}

func (b *hookedCalendar) Snapshot(ctx context.Context, c string) (hassles.GroupSnapshot, error) {
	if b.snapshot != nil {
		return b.snapshot(ctx, c)
	}
	return b.calendarFixture.Snapshot(ctx, c)
}
func (b *hookedCalendar) Write(ctx context.Context, c, h, e, body string) error {
	if b.write != nil {
		return b.write(ctx, c, h, e, body)
	}
	return b.calendarFixture.Write(ctx, c, h, e, body)
}
func (b *hookedCalendar) Calendars(ctx context.Context) ([]hassles.Calendar, error) {
	if b.calendars != nil {
		return b.calendars(ctx)
	}
	return b.calendarFixture.Calendars(ctx)
}
func hookHassleCalendar(h *harness, f *calendarFixture) *hookedCalendar {
	b := &hookedCalendar{calendarFixture: f}
	h.openHassles = func(context.Context, string) (hassles.Backend, error) { return b, nil }
	return b
}
func syntheticGroupSchedule() hassles.Schedule {
	return hassles.Schedule{Title: "Synthetic", Start: "2027-01-04T09:00", TimeZone: hassles.Zone, Cadence: "hourly", Interval: 1}
}
func assertGroupHTTPResult(t *testing.T, r *response, status string, settled bool) {
	t.Helper()
	loadAPI(t).assert(t, "HassleGroupResult", r.body)
	var out struct {
		Recovery hassleRecovery `json:"recovery"`
		Settled  bool           `json:"settled"`
	}
	if err := json.Unmarshal(r.body, &out); err != nil {
		t.Fatal(err)
	}
	if out.Recovery.Status != status || out.Settled != settled {
		t.Fatalf("unexpected outcome %d %s", r.status, r.body)
	}
}

func TestHasslesHTTPPartialWritesAndReadOnlyRecovery(t *testing.T) {
	for _, action := range []string{"create", "delete"} {
		t.Run(action, func(t *testing.T) {
			h, f, loc := hassleHarness(t)
			lease := injectHassleLease(t, h)
			chooseHassles(t, h, loc)
			in := groupHTTPCreate(t, h, loc, syntheticGroupSchedule())
			if action == "delete" {
				if r := groupHTTPPost(t, h, in); r.status != 200 {
					t.Fatal(r.status, string(r.body))
				}
				task := hassleList(t, h)[0]
				status := decode[hassleRecovery](t, h.do(t, "GET", "/v1/hassles/recovery", nil))
				in.Group = hassles.GroupRequest{Action: "delete", OperationID: "cccc3333-3333-4333-8333-333333333333", Task: hassleUUID, Incarnation: groupIncarnation, Fence: status.Fence, Revision: task.Revision}
			}
			b := hookHassleCalendar(h, f)
			before := f.writes
			b.write = func(ctx context.Context, c, p, e, body string) error {
				if f.writes == before+2 {
					return errors.New("synthetic failure after successful prefix")
				}
				return f.Write(ctx, c, p, e, body)
			}
			r := groupHTTPPost(t, h, in)
			if r.status != 409 || f.writes != before+2 {
				t.Fatal(r.status, string(r.body), f.writes)
			}
			assertGroupHTTPResult(t, r, "pending", false)
			tasks := hassleList(t, h)
			if len(tasks) != 1 || tasks[0].ID != "v2:"+hassleUUID || tasks[0].Instance != nil || tasks[0].Writable || tasks[0].Capabilities == nil || tasks[0].Capabilities.Delete || tasks[0].GroupStatus != "read-only" {
				t.Fatalf("unsafe partial card %+v", tasks)
			}
			if tasks[0].Title != "Synthetic" || tasks[0].DisplaySchedule == nil || *tasks[0].DisplaySchedule != syntheticGroupSchedule() || tasks[0].Timing.State != "unknown" {
				t.Fatal(tasks[0])
			}
			b.write = nil
			writes := f.writes
			for _, r = range []*response{groupHTTPPost(t, h, in), h.do(t, "POST", "/v1/hassles/recovery", []byte(`{}`))} {
				if r.status != 409 || f.writes != writes {
					t.Fatal("recovery wrote", r.status, string(r.body))
				}
				assertGroupHTTPResult(t, r, "pending", false)
			}
			loadAPI(t).assert(t, "HassleRecovery", h.do(t, "GET", "/v1/hassles/recovery", nil).body)
			if lease.active != 0 {
				t.Fatal("lease leaked")
			}
		})
	}
}

func TestHasslesHTTPLostFinalWriteAcknowledgementSettlesReadOnly(t *testing.T) {
	for _, recovery := range []string{"repeat", "original"} {
		for _, action := range []string{"create", "delete"} {
			t.Run(action+"/"+recovery, func(t *testing.T) {
				h, f, loc := hassleHarness(t)
				lease := injectHassleLease(t, h)
				chooseHassles(t, h, loc)
				schedule := syntheticGroupSchedule()
				schedule.Until = "2027-01-04T09:01"
				in := groupHTTPCreate(t, h, loc, schedule)
				if action == "delete" {
					if r := groupHTTPPost(t, h, in); r.status != 200 {
						t.Fatal(r.status)
					}
					task := hassleList(t, h)[0]
					status := decode[hassleRecovery](t, h.do(t, "GET", "/v1/hassles/recovery", nil))
					in.Group = hassles.GroupRequest{Action: "delete", OperationID: "cccc3333-3333-4333-8333-333333333333", Task: hassleUUID, Incarnation: groupIncarnation, Fence: status.Fence, Revision: task.Revision}
				}
				b := hookHassleCalendar(h, f)
				b.write = func(ctx context.Context, c, p, e, body string) error {
					if err := f.Write(ctx, c, p, e, body); err != nil {
						return err
					}
					return errors.New("applied but response lost")
				}
				r := groupHTTPPost(t, h, in)
				if r.status != 409 {
					t.Fatal(r.status, string(r.body))
				}
				assertGroupHTTPResult(t, r, "pending", false)
				writes := f.writes
				b.write = nil
				if recovery == "repeat" {
					r = groupHTTPPost(t, h, in)
				} else {
					r = h.do(t, "POST", "/v1/hassles/recovery", []byte(`{"expectedOperationId":"`+in.Group.OperationID+`"}`))
				}
				if r.status != 200 || f.writes != writes {
					t.Fatal("read-only settlement", r.status, string(r.body))
				}
				assertGroupHTTPResult(t, r, "ready", true)
				if lease.active != 0 {
					t.Fatal("lease leaked")
				}
			})
		}
	}
}

type phaseFaultStore struct {
	base     *leaseFixture
	phase    string
	persist  bool
	failLoad bool
	tripped  bool
}

func (s *phaseFaultStore) Load(ctx context.Context) ([]byte, error) {
	if s.tripped && s.failLoad {
		return nil, errors.New("following load failed")
	}
	return s.base.Load(ctx)
}
func (s *phaseFaultStore) Save(ctx context.Context, raw []byte) error {
	var state struct {
		Pending *struct{ Targets []struct{ Phase string } }
	}
	if err := json.Unmarshal(raw, &state); err != nil {
		return err
	}
	match := state.Pending == nil && s.phase == "clear"
	if state.Pending != nil && len(state.Pending.Targets) > 0 {
		match = state.Pending.Targets[0].Phase == s.phase
	}
	if !s.tripped && match {
		s.tripped = true
		if s.persist {
			if err := s.base.Save(ctx, raw); err != nil {
				return err
			}
		}
		return errors.New("synthetic phase save failure")
	}
	return s.base.Save(ctx, raw)
}
func TestHasslesHTTPSaveFaultMatrix(t *testing.T) {
	for _, phase := range []string{"entered", "ack", "verified", "clear"} {
		for _, persist := range []bool{false, true} {
			for _, loadFails := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/persist=%t/loadFails=%t", phase, persist, loadFails), func(t *testing.T) {
					h, f, loc := hassleHarness(t)
					lease := injectHassleLease(t, h)
					chooseHassles(t, h, loc)
					schedule := syntheticGroupSchedule()
					schedule.Until = "2027-01-04T09:01"
					in := groupHTTPCreate(t, h, loc, schedule)
					fault := &phaseFaultStore{base: lease, phase: phase, persist: persist, failLoad: loadFails}
					provider := h.leaseHassles
					h.leaseHassles = func(ctx context.Context) (hassles.GroupStore, func(), error) {
						_, release, err := provider(ctx)
						return fault, release, err
					}
					r := groupHTTPPost(t, h, in)
					if !fault.tripped || r.status != 409 {
						t.Fatal("fault not exercised", r.status, string(r.body))
					}
					want := "pending"
					if phase == "clear" && persist {
						want = "ready"
					}
					if loadFails {
						want = "unavailable"
					}
					assertGroupHTTPResult(t, r, want, false)
					if phase == "entered" && f.writes != 0 || phase != "entered" && f.writes != 1 {
						t.Fatal("wrong write count", f.writes)
					}
					if lease.active != 0 {
						t.Fatal("lease leaked")
					}
					fault.failLoad = false
					status := decode[hassleRecovery](t, h.do(t, "GET", "/v1/hassles/recovery", nil))
					want = "pending"
					if phase == "clear" && persist {
						want = "ready"
					}
					if status.Status != want {
						t.Fatal(status)
					}
				})
			}
		}
	}
}

func TestHasslesHTTPFreshBindingEveryWrite(t *testing.T) {
	for _, kind := range []string{"legacy", "group"} {
		for _, when := range []string{"admission", "snapshot", "between"} {
			for _, change := range []string{"selection", "email", "token", "grant"} {
				t.Run(kind+"/"+when+"/"+change, func(t *testing.T) {
					h, f, loc := hassleHarness(t)
					lease := injectHassleLease(t, h)
					chooseHassles(t, h, loc)
					group := groupHTTPCreate(t, h, loc, syntheticGroupSchedule())
					legacy := hassles.Mutation{Location: loc, Action: "create", ID: hassleUUID, Schedule: hassles.Schedule{Title: "Legacy", Start: "2027-01-04T09:00", TimeZone: hassles.Zone, Cadence: "daily", Interval: 1}}
					if kind == "legacy" && when == "between" {
						c, err := hassles.NewCalendar(hassleUUID, legacy.Schedule, time.Now())
						if err != nil {
							t.Fatal(err)
						}
						// Genuine v1 siblings constitute one multi-resource legacy task.
						f.objects = []caldav.CalendarObject{{Path: loc.CalendarID + "first.ics", ETag: `"a"`, Data: c}, {Path: loc.CalendarID + "second.ics", ETag: `"b"`, Data: c}}
						tasks, err := hassles.Inventory(f.objects, time.Now())
						if err != nil || len(tasks) != 1 || tasks[0].Resources != 2 {
							t.Fatalf("fixture %+v %v", tasks, err)
						}
						legacy.Action = "delete"
						legacy.ID = tasks[0].ID
						legacy.Revision = tasks[0].Revision
					}
					mutate := func() {
						switch change {
						case "grant":
							h.hasslesEnabled = false
						case "selection":
							newLoc := loc
							newLoc.CalendarID = otherCalendar
							if err := h.store.PutSetting(corpus.SettingHasslesLocation, string(hassleJSON(t, newLoc))); err != nil {
								t.Fatal(err)
							}
						case "email":
							if err := h.store.PutGmailAccount(corpus.GmailAccount{ID: "personal", Email: "changed@example.test", DisplayName: "Changed"}); err != nil {
								t.Fatal(err)
							}
						case "token":
							p, _ := gmailclient.AccountTokenPath("personal")
							if err := os.Remove(p); err != nil {
								t.Fatal(err)
							}
						}
					}
					b := hookHassleCalendar(h, f)
					if when == "admission" {
						mutate()
					}
					if when == "snapshot" {
						b.snapshot = func(ctx context.Context, c string) (hassles.GroupSnapshot, error) {
							out, err := f.Snapshot(ctx, c)
							mutate()
							return out, err
						}
					}
					b.write = func(ctx context.Context, c, p, e, body string) error {
						if c != loc.CalendarID || !strings.HasPrefix(p, loc.CalendarID) {
							t.Fatal("retargeted", c, p)
						}
						err := f.Write(ctx, c, p, e, body)
						if when == "between" && f.writes == 1 {
							mutate()
						}
						return err
					}
					// At admission an email change is the actual actor, not a stale browser claim.
					expected := 0
					if when == "between" {
						expected = 1
					}
					if when == "admission" && change == "email" {
						if kind == "group" {
							expected = 24
						} else {
							expected = 1
						}
					}
					var r *response
					if kind == "group" {
						r = groupHTTPPost(t, h, group)
					} else {
						r = h.do(t, "POST", "/v1/hassles", hassleJSON(t, legacy))
					}
					if f.writes != expected || expected == 0 && r.status == 200 || when != "admission" && r.status == 200 {
						t.Fatal("stale binding wrote", r.status, string(r.body), f.writes, expected)
					}
					if lease.active != 0 {
						t.Fatal("lease leaked")
					}
				})
			}
		}
	}
}

func TestHasslesHTTPStrictScheduleContract(t *testing.T) {
	h, f, loc := hassleHarness(t)
	chooseHassles(t, h, loc)
	in := groupHTTPCreate(t, h, loc, syntheticGroupSchedule())
	raw := hassleJSON(t, in)
	doc := loadAPI(t)
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatal(err)
	}
	group := value["group"].(map[string]any)
	schema := doc.schemas["HassleGroupCreate"].(map[string]any)
	if problems := doc.violations(schema, group, "create"); len(problems) != 0 {
		t.Fatal(problems)
	}
	delete(group["schedule"].(map[string]any), "workHours")
	if problems := doc.violations(schema, group, "create"); len(problems) == 0 {
		t.Fatal("schema permits missing workHours")
	}
	if r := h.do(t, "POST", "/v1/hassles/groups", hassleJSON(t, value)); r.status != 400 || f.writes != 0 {
		t.Fatal(r.status, string(r.body))
	}
	legacySchema := doc.schemas["HassleSchedule"].(map[string]any)
	if problems := doc.violations(legacySchema, group["schedule"], "legacy"); len(problems) != 0 {
		t.Fatal("legacy default changed", problems)
	}
	if r := h.do(t, "POST", "/v1/hassles/groups", raw); r.status != 200 {
		t.Fatal(r.status, string(r.body))
	}
}

func TestHasslesHTTPRetiredFenceAfterExternalDeletion(t *testing.T) {
	h, f, loc := hassleHarness(t)
	chooseHassles(t, h, loc)
	in := groupHTTPCreate(t, h, loc, syntheticGroupSchedule())
	if r := groupHTTPPost(t, h, in); r.status != 200 {
		t.Fatal(r.status)
	}
	f.objects = nil
	writes := f.writes
	if r := groupHTTPPost(t, h, in); r.status != 409 || f.writes != writes || len(f.objects) != 0 {
		t.Fatal("retired intent resurrected", r.status, string(r.body))
	}
}

func TestHasslesHTTPPendingUnrelatedDisconnectAndChangedIdentityOAuth(t *testing.T) {
	h, f, loc := hassleHarness(t)
	chooseHassles(t, h, loc)
	in := groupHTTPCreate(t, h, loc, syntheticGroupSchedule())
	f.deny = true
	if r := groupHTTPPost(t, h, in); r.status != 409 {
		t.Fatal(r.status)
	}
	if err := h.store.PutGmailAccount(corpus.GmailAccount{ID: "other", Email: "other@example.test", DisplayName: "Other"}); err != nil {
		t.Fatal(err)
	}
	p, _ := gmailclient.AccountTokenPath("other")
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("inert synthetic token"), 0600); err != nil {
		t.Fatal(err)
	}
	if r := h.do(t, "POST", "/auth/accounts/other/disconnect", nil); r.status != 200 {
		t.Fatal("unrelated disconnect blocked", r.status, string(r.body))
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	state, _ := beginFakeLogin(t, h, "?account=personal")
	fakeOAuth(t, `{"email":"changed@example.test"}`, 200, nil)
	r := h.do(t, "GET", "/?code=fake&state="+state, nil)
	if r.status == 200 {
		t.Fatal("known pending actor replaced")
	}
	status := decode[hassleRecovery](t, h.do(t, "GET", "/v1/hassles/recovery", nil))
	if status.Status != "pending" {
		t.Fatal(status)
	}
}

// A genuine pre-collision witness must still be denied at the namespace guard.
func TestHasslesHTTPNamespaceGenuineWitnesses(t *testing.T) {
	for _, kind := range []string{"instance-uid-todo", "adoption-commit", "reserved-physical-prefix", "forged-resource-alias"} {
		t.Run(kind, func(t *testing.T) {
			h, f, loc := hassleHarness(t)
			chooseHassles(t, h, loc)
			var in hassles.Mutation
			if kind == "adoption-commit" {
				in = legacyHassleMutation(t, f, loc)
				r := h.do(t, "POST", "/v1/hassles", hassleJSON(t, in))
				if r.status != 200 {
					t.Fatal(r.status, string(r.body))
				}
				preview := decode[hassles.Result](t, r)
				dry := false
				in.DryRun = &dry
				in.AdoptHash = &preview.Adoption.Fingerprint
			} else {
				in = instanceHassleMutation(t, f, loc)
				r := h.do(t, "POST", "/v1/hassles", hassleJSON(t, in))
				if r.status != 200 {
					t.Fatal(r.status, string(r.body))
				}
				dry := false
				in.DryRun = &dry
			}
			uid, _ := f.objects[0].Data.Events()[0].Props.Text("UID")
			if kind == "reserved-physical-prefix" {
				f.objects[0].Path = loc.CalendarID + "hassle-parts-v2-" + hassleUUID + ".ics"
			} else {
				todo := ical.NewComponent("VTODO")
				todo.Props.SetText("UID", uid)
				// UID-only collision: no task metadata hint may be needed to deny the witness.
				reserved := ical.NewCalendar()
				reserved.Children = append(reserved.Children, todo)
				path := loc.CalendarID + "hassle-parts-v2-collision.ics"
				if kind == "forged-resource-alias" {
					path = f.objects[0].Path
					todo.Props.SetText("DESCRIPTION", "[docket]\n[hassle:parts:v99 task="+hassleUUID+"]")
				}
				f.objects = append(f.objects, caldav.CalendarObject{Path: path, ETag: `"collision"`, Data: reserved})
			}
			r := h.do(t, "POST", "/v1/hassles", hassleJSON(t, in))
			if r.status != 409 || f.writes != 0 || !(strings.Contains(string(r.body), "reserved group") || kind == "forged-resource-alias" && strings.Contains(string(r.body), "duplicate calendar resource href")) {
				t.Fatal("namespace guard not reached", r.status, string(r.body), f.writes)
			}
			if kind == "forged-resource-alias" {
				r = h.do(t, "GET", "/v1/hassles?accountId=personal&calendarId="+reminderCalendar, nil)
				if r.status != 502 {
					t.Fatal("ambiguous inventory promoted", r.status, string(r.body))
				}
				return
			}
			for _, task := range hassleList(t, h) {
				if task.ID == in.ID && (task.Writable || task.Instance != nil) {
					t.Fatal("promoted witness", task)
				}
			}
		})
	}
}

func TestHasslesHTTPDeadlinesAdmissionCancellationAndRelease(t *testing.T) {
	for _, endpoint := range []string{"legacy", "selection", "group", "reconcile", "disconnect", "inventory", "status"} {
		t.Run(endpoint, func(t *testing.T) {
			h, f, loc := hassleHarness(t)
			base := injectHassleLease(t, h)
			chooseHassles(t, h, loc)
			in := groupHTTPCreate(t, h, loc, syntheticGroupSchedule())
			legacy := hassles.Mutation{Location: loc, Action: "create", ID: hassleUUID, Schedule: hassles.Schedule{Title: "Legacy", Start: "2027-01-04T09:00", TimeZone: hassles.Zone, Cadence: "daily", Interval: 1}}
			method, path, body := "POST", "/v1/hassles", hassleJSON(t, legacy)
			want := 45 * time.Second
			switch endpoint {
			case "selection":
				path = "/v1/hassles/location"
				body = hassleJSON(t, map[string]any{"location": loc, "previous": loc, "dedicated": true})
			case "group":
				path = "/v1/hassles/groups"
				body = hassleJSON(t, in)
				want = 3 * time.Minute
			case "reconcile":
				path = "/v1/hassles/recovery"
				body = []byte(`{}`)
				want = 3 * time.Minute
			case "disconnect":
				path = "/auth/accounts/personal/disconnect"
				body = nil
			case "inventory":
				method = "GET"
				path = "/v1/hassles?accountId=personal&calendarId=" + reminderCalendar
				body = nil
			case "status":
				method = "GET"
				path = "/v1/hassles/recovery"
				body = nil
				want = 250 * time.Millisecond
			}
			provider := h.leaseHassles
			seen := 0
			h.leaseHassles = func(ctx context.Context) (hassles.GroupStore, func(), error) {
				deadline, ok := ctx.Deadline()
				if !ok {
					t.Error("lease admission has no deadline")
				} else if left := time.Until(deadline); left <= 0 || left > want || left < want-time.Second {
					t.Errorf("deadline %s, want <=%s", left, want)
				}
				seen++
				return provider(ctx)
			}
			_ = h.do(t, method, path, body)
			if seen == 0 {
				t.Fatal("provider not reached")
			}
			if base.active != 0 {
				t.Fatal("lease leaked")
			}
			// An already expired/canceled request must never delegate a write.
			for _, expired := range []bool{false, true} {
				var ctx context.Context
				var cancel context.CancelFunc
				if expired {
					ctx, cancel = context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
				} else {
					ctx, cancel = context.WithCancel(context.Background())
					cancel()
				}
				h.leaseHassles = func(ctx context.Context) (hassles.GroupStore, func(), error) {
					if ctx.Err() == nil {
						t.Error("lost cancellation")
					}
					return nil, nil, ctx.Err()
				}
				writes := f.writes
				_ = h.doWithContext(t, ctx, method, path, body)
				cancel()
				if f.writes != writes {
					t.Fatal("canceled request wrote")
				}
				if !h.hasslesMu.TryLock() {
					t.Fatal("mutex leaked after cancellation")
				}
				h.hasslesMu.Unlock()
			}
			// A held process lease must refuse, not queue and execute old intent later.
			h.leaseHassles = provider
			h.hasslesMu.Lock()
			done := make(chan *response, 1)
			go func() { done <- h.do(t, method, path, body) }()
			select {
			case r := <-done:
				if method == "POST" && r.status != 409 {
					t.Error("busy write not refused", r.status, string(r.body))
				}
			case <-time.After(time.Second):
				h.hasslesMu.Unlock()
				<-done
				t.Fatal("request silently queued")
			}
			h.hasslesMu.Unlock()
			// External lease contention returns a bounded refusal and releases process admission.
			h.leaseHassles = func(ctx context.Context) (hassles.GroupStore, func(), error) {
				<-ctx.Done()
				return nil, nil, ctx.Err()
			}
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Millisecond)
			defer cancel()
			started := time.Now()
			_ = h.doWithContext(t, ctx, method, path, body)
			if time.Since(started) > time.Second {
				t.Fatal("external lease ignored context")
			}
			if !h.hasslesMu.TryLock() {
				t.Fatal("mutex leaked on lease error")
			}
			h.hasslesMu.Unlock()
		})
	}
}

func TestHasslesHTTPConcurrentBoundDisconnectAndFastStatus(t *testing.T) {
	h, f, loc := hassleHarness(t)
	lease := injectHassleLease(t, h)
	chooseHassles(t, h, loc)
	in := groupHTTPCreate(t, h, loc, syntheticGroupSchedule())
	b := hookHassleCalendar(h, f)
	entered, unblock := make(chan struct{}), make(chan struct{})
	b.snapshot = func(ctx context.Context, c string) (hassles.GroupSnapshot, error) {
		close(entered)
		select {
		case <-unblock:
		case <-ctx.Done():
			return hassles.GroupSnapshot{}, ctx.Err()
		}
		return hassles.GroupSnapshot{}, errors.New("synthetic read failure")
	}
	done := make(chan *response, 1)
	go func() { done <- groupHTTPPost(t, h, in) }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("operation did not enter snapshot")
	}
	r := h.do(t, "POST", "/auth/accounts/personal/disconnect", nil)
	if r.status != 409 {
		t.Error("disconnect queued or mutated", r.status, string(r.body))
	}
	for _, path := range []string{"/v1/hassles/location", "/v1/hassles/recovery"} {
		r = h.do(t, "GET", path, nil)
		if r.status != 200 || !strings.Contains(string(r.body), `"busy"`) {
			t.Error("status blocked", r.status, string(r.body))
		}
	}
	close(unblock)
	select {
	case r = <-done:
		if r.status != 409 {
			t.Error(r.status, string(r.body))
		}
	case <-time.After(time.Second):
		t.Fatal("operation failed to release")
	}
	if lease.active != 0 {
		t.Fatal("lease leaked")
	}
	if r = h.do(t, "POST", "/auth/accounts/personal/disconnect", nil); r.status != 200 {
		t.Fatal("disconnect remained blocked", r.status, string(r.body))
	}
}

func TestHasslesHTTPInventoryCapabilitiesFailClosed(t *testing.T) {
	for _, mode := range []string{"pending", "missing", "corrupt", "busy"} {
		t.Run(mode, func(t *testing.T) {
			h, f, loc := hassleHarness(t)
			lease := injectHassleLease(t, h)
			chooseHassles(t, h, loc)
			in := groupHTTPCreate(t, h, loc, syntheticGroupSchedule())
			if r := groupHTTPPost(t, h, in); r.status != 200 {
				t.Fatal(r.status)
			}
			switch mode {
			case "pending":
				status := decode[hassleRecovery](t, h.do(t, "GET", "/v1/hassles/recovery", nil))
				task := hassleList(t, h)[0]
				in.Group = hassles.GroupRequest{Action: "delete", OperationID: "cccc3333-3333-4333-8333-333333333333", Task: hassleUUID, Incarnation: groupIncarnation, Fence: status.Fence, Revision: task.Revision}
				f.deny = true
				if r := groupHTTPPost(t, h, in); r.status != 409 {
					t.Fatal(r.status)
				}
			case "missing":
				h.leaseHassles = nil
			case "corrupt":
				lease.raw = []byte(`{}`)
			case "busy":
				lease.fail = true
			}
			r := h.do(t, "GET", "/v1/hassles?accountId=personal&calendarId="+reminderCalendar, nil)
			if r.status != 200 {
				t.Fatal(r.status, string(r.body))
			}
			loadAPI(t).assert(t, "HassleInventory", r.body)
			tasks := decode[struct {
				Tasks []hassles.Task `json:"tasks"`
			}](t, r).Tasks
			if len(tasks) != 1 || tasks[0].Capabilities == nil || tasks[0].Capabilities.Delete || tasks[0].Writable || tasks[0].Instance != nil {
				t.Fatal("unsafe availability", tasks)
			}
		})
	}
}

func TestHasslesHTTPGroupFreshRevisionCollisionAndLimits(t *testing.T) {
	for _, mode := range []string{"revision", "collision", "limits"} {
		t.Run(mode, func(t *testing.T) {
			h, f, loc := hassleHarness(t)
			chooseHassles(t, h, loc)
			in := groupHTTPCreate(t, h, loc, syntheticGroupSchedule())
			if mode == "revision" {
				if r := groupHTTPPost(t, h, in); r.status != 200 {
					t.Fatal(r.status)
				}
				task := hassleList(t, h)[0]
				status := decode[hassleRecovery](t, h.do(t, "GET", "/v1/hassles/recovery", nil))
				in.Group = hassles.GroupRequest{Action: "delete", OperationID: "cccc3333-3333-4333-8333-333333333333", Task: hassleUUID, Incarnation: groupIncarnation, Fence: status.Fence, Revision: task.Revision}
				f.objects[0].ETag = `"changed"`
			} else if mode == "collision" {
				// A reserved physical create target with no valid logical group is not empty space.
				c := ical.NewCalendar()
				todo := ical.NewComponent("VTODO")
				todo.Props.SetText("UID", "hassle-"+hassleUUID)
				c.Children = append(c.Children, todo)
				f.objects = []caldav.CalendarObject{{Path: loc.CalendarID + "hassle-parts-v2-" + hassleUUID + "-" + groupIncarnation + "-09.ics", ETag: `"collision"`, Data: c}}
			} else {
				for i := 0; i < 2001; i++ {
					f.objects = append(f.objects, caldav.CalendarObject{Path: fmt.Sprintf("%slimit-%d.ics", loc.CalendarID, i), ETag: `"limit"`, Data: ical.NewCalendar()})
				}
			}
			writes := f.writes
			r := groupHTTPPost(t, h, in)
			if r.status != 409 || f.writes != writes {
				t.Fatal("unsafe group admission", r.status, string(r.body), f.writes)
			}
		})
	}
}
