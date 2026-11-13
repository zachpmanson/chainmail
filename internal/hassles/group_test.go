package hassles

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-ical"
	"github.com/emersion/go-webdav/caldav"
)

var groupTestBinding = GroupBinding{Actor: GroupActor{ServiceUID: "offline-operator", AccountID: dailyGroupLocation.AccountID, Email: "synthetic@example.test"}, Location: dailyGroupLocation}
var groupTestError = errors.New("synthetic failure")

type groupMemoryStore struct {
	raw    []byte
	saves  int
	before func(int, []byte) error
	after  func(int, []byte) error
}

func (s *groupMemoryStore) Load(ctx context.Context) ([]byte, error) {
	return append([]byte(nil), s.raw...), ctx.Err()
}
func (s *groupMemoryStore) Save(ctx context.Context, raw []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.saves++
	if s.before != nil {
		if err := s.before(s.saves, raw); err != nil {
			return err
		}
	}
	s.raw = append([]byte(nil), raw...)
	if s.after != nil {
		return s.after(s.saves, raw)
	}
	return nil
}
func groupTestStore(t *testing.T) *groupMemoryStore {
	t.Helper()
	raw, err := NewGroupState()
	if err != nil {
		t.Fatal(err)
	}
	return &groupMemoryStore{raw: raw}
}
func groupTestRequest(t *testing.T, s *groupMemoryStore, schedule Schedule) GroupRequest {
	t.Helper()
	fence, _, err := GroupStateFence(s.raw)
	if err != nil {
		t.Fatal(err)
	}
	return GroupRequest{Action: "create", OperationID: "aaaa1111-1111-4111-8111-111111111111", Task: dailyV2Task, Incarnation: dailyV2Incarnation, Fence: fence, Schedule: &schedule}
}
func groupTestCheck(ctx context.Context, b GroupBinding) error {
	if b != groupTestBinding {
		return groupTestError
	}
	return ctx.Err()
}

type groupWriteCall struct{ Href, ETag, Body string }
type groupFake struct {
	objects    map[string]caldav.CalendarObject
	writes     []groupWriteCall
	methods    []string
	before     func(string, int) error
	afterWrite func(int) error
	report     func([]caldav.CalendarObject) []caldav.CalendarObject
	probe      func(string, GroupProbe) (GroupProbe, error)
	deadline   time.Time
}

func groupTestBackend(objects []caldav.CalendarObject) *groupFake {
	b := &groupFake{objects: map[string]caldav.CalendarObject{}}
	for _, o := range objects {
		b.objects[o.Path] = dailyPartsCloneObject(o)
	}
	return b
}
func (b *groupFake) call(ctx context.Context, method string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	deadline, ok := ctx.Deadline()
	if !ok || time.Until(deadline) > groupInvocationTimeout {
		return errors.New("missing invocation deadline")
	}
	if b.deadline.IsZero() {
		b.deadline = deadline
	} else if !deadline.Equal(b.deadline) {
		return errors.New("deadline extended within invocation")
	}
	b.methods = append(b.methods, method)
	if b.before != nil {
		return b.before(method, len(b.methods))
	}
	return nil
}
func (b *groupFake) Snapshot(ctx context.Context, calendar string) (GroupSnapshot, error) {
	if err := b.call(ctx, "Snapshot"); err != nil {
		return GroupSnapshot{}, err
	}
	out := []caldav.CalendarObject{}
	keys := []string{}
	for key := range b.objects {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		out = append(out, dailyPartsCloneObject(b.objects[key]))
	}
	if b.report != nil {
		out = b.report(out)
	}
	return GroupSnapshot{Objects: out}, nil
}
func (b *groupFake) Probe(ctx context.Context, calendar, href string) (GroupProbe, error) {
	if err := b.call(ctx, "Probe"); err != nil {
		return GroupProbe{}, err
	}
	o, exists := b.objects[href]
	p := GroupProbe{Absent: !exists}
	if exists {
		p.Object = dailyPartsCloneObject(o)
	}
	if b.probe != nil {
		return b.probe(href, p)
	}
	return p, nil
}
func (b *groupFake) Write(ctx context.Context, calendar, href, etag, body string) error {
	if err := b.call(ctx, "Write"); err != nil {
		return err
	}
	if _, err := Resource(calendar, href); err != nil {
		return err
	}
	old, exists := b.objects[href]
	b.writes = append(b.writes, groupWriteCall{href, etag, body})
	if body == "" {
		if !exists || !dailyPartsStrongETag(etag) || etag != old.ETag {
			return ErrConflict
		}
		delete(b.objects, href)
	} else {
		if etag != "*" || exists {
			return ErrConflict
		}
		data, err := groupDecodeCalendar(body)
		if err != nil {
			return err
		}
		b.objects[href] = caldav.CalendarObject{Path: href, ETag: fmt.Sprintf(`"receipt-%d"`, len(b.writes)), Data: data}
	}
	if b.afterWrite != nil {
		return b.afterWrite(len(b.writes))
	}
	return nil
}

// Compile-time proof that read-only replay needs no Write method.
type groupOnlyReader struct{ b *groupFake }

func (r groupOnlyReader) Snapshot(ctx context.Context, c string) (GroupSnapshot, error) {
	return r.b.Snapshot(ctx, c)
}
func (r groupOnlyReader) Probe(ctx context.Context, c, h string) (GroupProbe, error) {
	return r.b.Probe(ctx, c, h)
}

var _ GroupReader = groupOnlyReader{}

func groupRun(t *testing.T, b *groupFake, s *groupMemoryStore, r GroupRequest) (GroupOutcome, error) {
	t.Helper()
	b.deadline = time.Time{}
	return ExecuteGroup(context.Background(), b, s, r, groupTestBinding, groupTestCheck, now)
}
func groupDeleteRequest(t *testing.T, s *groupMemoryStore, objects []caldav.CalendarObject) GroupRequest {
	t.Helper()
	model := dailyGroupRead(t, objects)
	if len(model.Groups) != 1 || !model.Groups[0].StructuralReady {
		t.Fatal("bad fixture")
	}
	g := model.Groups[0]
	r := groupTestRequest(t, s, Schedule{})
	r.Action = "delete"
	r.Schedule = nil
	r.Revision = g.Revision
	r.Task = g.Task
	r.Incarnation = g.Plan.Incarnation()
	return r
}

func TestGroupFullNativePreparationExecutionAndFence(t *testing.T) {
	for _, tc := range []struct {
		name     string
		schedule Schedule
		n        int
	}{
		{"full24", dailyV2Schedule("2027-02-12T09:17", 0.5, false, ""), 24},
		{"work8", dailyV2Schedule("2027-02-12T09:17", 0.5, true, ""), 8},
		{"finite1", dailyV2Schedule("2027-02-12T09:17", 1, false, "2027-02-12T09:20"), 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := groupTestStore(t)
			r := groupTestRequest(t, s, tc.schedule)
			b := groupTestBackend(nil)
			out, err := groupRun(t, b, s, r)
			if err != nil || !out.Settled || out.Pending || len(b.writes) != tc.n {
				t.Fatalf("create %+v %v writes=%d", out, err, len(b.writes))
			}
			// 1 admission snapshot + n absence GETs + 3n step methods + final snapshot/n GETs.
			if out.Calls != 2+5*tc.n || out.Calls > groupInvocationLimit {
				t.Fatalf("create calls=%d", out.Calls)
			}
			for _, w := range b.writes {
				if w.ETag != "*" || !strings.Contains(w.Href, groupPhysicalPrefix+r.Task+"-"+r.Incarnation) {
					t.Fatal("nonconditional web create", w)
				}
				if strings.Contains(w.Body, "[docket]") || !strings.Contains(w.Body, "STATUS:CONFIRMED") || !strings.Contains(w.Body, "TRANSP:TRANSPARENT") {
					t.Fatal("wrong native body")
				}
			}
			// Once settled, external deletion never makes the old request executable.
			b.objects = map[string]caldav.CalendarObject{}
			before := len(b.writes)
			if _, err = groupRun(t, b, s, r); !errors.Is(err, ErrConflict) || len(b.writes) != before {
				t.Fatal("stale fence recreated resources", err)
			}
			state, err := groupDecodeState(s.raw)
			if err != nil || state.Generation != 1 || state.Pending != nil {
				t.Fatal("bad settlement", state, err)
			}
		})
	}
	// Delete captures arbitrary native hrefs and UIDs, never reconstructs web names.
	p := dailyV2Compile(t, dailyV2Schedule("2027-02-12T09:17", 0.5, false, ""))
	originals := dailyGroupObjects(t, p)
	s := groupTestStore(t)
	r := groupDeleteRequest(t, s, originals)
	b := groupTestBackend(originals)
	out, err := groupRun(t, b, s, r)
	if err != nil || !out.Settled || len(b.writes) != 24 || len(b.objects) != 0 {
		t.Fatalf("delete %+v %v", out, err)
	}
	if out.Calls != 2+24+24*4+24*23/2+24 || out.Calls > 512 {
		t.Fatalf("delete calls=%d", out.Calls)
	}
	for i, w := range b.writes {
		if w.Href != originals[i].Path || w.ETag != originals[i].ETag || w.Body != "" {
			t.Fatal("not captured native identity", w)
		}
	}
}

func TestGroupLosslessCodecAndRequestRefusals(t *testing.T) {
	s := groupTestStore(t)
	r := groupTestRequest(t, s, dailyV2Schedule("2027-02-12T09:17", 24, false, ""))
	raw, _ := json.Marshal(r)
	if _, err := DecodeGroupRequest(raw); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{
		strings.Replace(string(raw), `"action":"create"`, `"action":"create","action":"create"`, 1),
		strings.Replace(string(raw), `"action":`, `"ACTION":`, 1),
		strings.Replace(string(raw), `"action":"create"`, `"action":"create","href":"x"`, 1),
		strings.Replace(string(raw), `"schedule":{`, `"revision":null,"schedule":{`, 1),
		strings.Replace(string(raw), `"schedule":{`, `"revision":"`+strings.Repeat("a", 64)+`","schedule":{`, 1),
		string(raw) + "{}", strings.Replace(string(raw), `"schedule":{`, `"schedule":{"unknown":true,`, 1),
	} {
		if _, err := DecodeGroupRequest([]byte(bad)); err == nil {
			t.Fatalf("accepted %s", bad)
		}
	}
	p := dailyV2Compile(t, *r.Schedule)
	objects := dailyGroupObjects(t, p)
	e := objects[0].Data.Events()[0]
	// Unknown properties and every nil/empty distinction are admitted losslessly
	// for deletes, not stripped down to a metadata ownership fingerprint.
	e.Props["X-UNKNOWN"] = []ical.Prop{{Name: "X-UNKNOWN", Value: "raw\\,text", Params: ical.Params{"X-NIL": nil, "X-EMPTY": {}, "X-TEXT": {"a\\b"}}}}
	objects[0].Data.Props["X-NIL"] = nil
	request := groupDeleteRequest(t, s, objects)
	prepared, err := PrepareGroup(request, groupTestBinding, GroupSnapshot{objects}, now)
	if err != nil {
		t.Fatal(err)
	}
	op, err := prepared.operation()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(*op.Targets[0].Original, objects[0]) {
		t.Fatal("raw originals changed")
	}
	record := groupState{Version: 1, Epoch: strings.Split(r.Fence, ":")[0], Pending: &op}
	encoded, err := groupEncodeState(record)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := groupDecodeState(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(*decoded.Pending.Targets[0].Original, objects[0]) {
		t.Fatal("codec normalized nils")
	}
	// Caller mutations cannot alter an immutable prepared capture.
	e.Props["X-UNKNOWN"][0].Value = "changed"
	again, err := prepared.operation()
	if err != nil || again.Targets[0].Original.Data.Events()[0].Props["X-UNKNOWN"][0].Value != "raw\\,text" {
		t.Fatal("capture aliased input")
	}
	e.Props["X-UNKNOWN"][0].Value = string([]byte{0xff})
	b := groupTestBackend(objects)
	_, err = groupRun(t, b, s, request)
	if err == nil || len(b.writes) != 0 {
		t.Fatal("lossy UTF8 admitted", err)
	}
	for _, bad := range []string{strings.Replace(string(encoded), `"Version":1`, `"Version":2`, 1), strings.Replace(string(encoded), `"Phase":"planned"`, `"Phase":"wat"`, 1), string(encoded) + "\n", strings.Replace(string(encoded), `"Generation":0`, `"Generation":0,"Generation":0`, 1)} {
		if _, err := groupDecodeState([]byte(bad)); err == nil {
			t.Fatal("unsafe record accepted")
		}
	}
}

func TestGroupAdmissionRefusesBeforeAnyWrites(t *testing.T) {
	for _, name := range []string{"stale-revision", "retag", "raw-changed", "missing-member", "surplus-claim", "uid-collision", "malformed", "lossy-unknown", "actor", "probe-vtodo", "probe-error", "initial-store"} {
		t.Run(name, func(t *testing.T) {
			p := dailyV2Compile(t, dailyV2Schedule("2027-02-12T09:17", 8, false, ""))
			objects := dailyGroupObjects(t, p)
			s := groupTestStore(t)
			r := groupDeleteRequest(t, s, objects)
			b := groupTestBackend(objects)
			switch name {
			case "stale-revision":
				r.Revision = strings.Repeat("a", 64)
			case "retag":
				o := b.objects[objects[0].Path]
				o.ETag = `"new"`
				b.objects[o.Path] = o
			case "raw-changed":
				b.objects[objects[0].Path].Data.Events()[0].Props.SetText("X-UNKNOWN", "changed")
			case "missing-member":
				delete(b.objects, objects[1].Path)
			case "surplus-claim":
				o := dailyPartsCloneObject(objects[0])
				o.Path = dailyGroupLocation.CalendarID + "extra.ics"
				b.objects[o.Path] = o
			case "uid-collision":
				o := dailyPartsCloneObject(objects[0])
				o.Path = dailyGroupLocation.CalendarID + "todo.ics"
				o.Data.Children[0].Name = "VTODO"
				o.Data.Children[0].Props.Del("DESCRIPTION")
				b.objects[o.Path] = o
			case "malformed":
				o := b.objects[objects[0].Path]
				o.Data = nil
				b.objects[o.Path] = o
			case "lossy-unknown":
				b.objects[objects[0].Path].Data.Events()[0].Props.SetText("X-UNKNOWN", string([]byte{0xff}))
			case "actor":
				r.Incarnation = "bbbb1111-1111-4111-8111-111111111111"
			case "probe-vtodo":
				b.probe = func(h string, p GroupProbe) (GroupProbe, error) {
					p.Object.Data.Children[0].Name = "VTODO"
					return p, nil
				}
			case "probe-error":
				b.probe = func(string, GroupProbe) (GroupProbe, error) { return GroupProbe{}, groupTestError }
			case "initial-store":
				s.before = func(int, []byte) error { return groupTestError }
			}
			_, err := groupRun(t, b, s, r)
			if err == nil || len(b.writes) != 0 {
				t.Fatal("unsafe admission", err, len(b.writes))
			}
		})
	}
	// Create collisions, REPORT capacity and exact href presence are checked before journaling.
	for _, name := range []string{"href", "task", "capacity", "absence-get", "binding"} {
		t.Run("create-"+name, func(t *testing.T) {
			s := groupTestStore(t)
			r := groupTestRequest(t, s, dailyV2Schedule("2027-02-12T09:17", 1, false, ""))
			b := groupTestBackend(nil)
			switch name {
			case "task":
				objects := dailyGroupObjects(t, dailyV2Compile(t, *r.Schedule))
				b = groupTestBackend(objects)
			case "href":
				p, _ := CompileDailyPartsV2(*r.Schedule, r.Task, r.Incarnation)
				target, _ := groupCreateTarget(p, p.members[0], r.Schedule.Title, dailyGroupLocation.CalendarID, now)
				o := *target.Planned
				o.ETag = `"x"`
				b.objects[o.Path] = o
			case "capacity":
				o := dailyGroupObjects(t, dailyV2Compile(t, dailyV2Schedule("2027-02-12T09:17", 24, false, "")))[0]
				o.Data.Events()[0].Props.Del("DESCRIPTION")
				for i := 0; i < 1977; i++ {
					copy := dailyPartsCloneObject(o)
					copy.Path = fmt.Sprintf("%sx-%d.ics", dailyGroupLocation.CalendarID, i)
					copy.Data.Events()[0].Props.SetText("UID", fmt.Sprintf("foreign-%d", i))
					b.objects[copy.Path] = copy
				}
			case "absence-get":
				b.probe = func(string, GroupProbe) (GroupProbe, error) { return GroupProbe{Absent: false}, nil }
			case "binding":
				groupTestCheckBad := func(context.Context, GroupBinding) error { return groupTestError }
				_, err := ExecuteGroup(context.Background(), b, s, r, groupTestBinding, groupTestCheckBad, now)
				if err == nil || len(b.writes) != 0 {
					t.Fatal("identity unchecked")
				}
				return
			}
			_, err := groupRun(t, b, s, r)
			if err == nil || len(b.writes) != 0 || s.saves != 0 {
				t.Fatal("unsafe create admission", err, len(b.writes), s.saves)
			}
		})
	}
}

func TestGroupComponentEvidenceUsesPrimaryNamespaceOnly(t *testing.T) {
	p := dailyV2Compile(t, dailyV2Schedule("2027-02-12T09:17", 24, false, ""))
	base := dailyGroupObjects(t, p)
	for _, name := range []string{"VTODO", "VJOURNAL", "VFREEBUSY"} {
		objects := append([]caldav.CalendarObject(nil), base...)
		other := dailyPartsCloneObject(base[0])
		other.Path = dailyGroupLocation.CalendarID + name + ".ics"
		other.Data.Children[0].Name = name
		other.Data.Children[0].Props.Del("DESCRIPTION")
		objects = append(objects, other)
		dailyGroupReadonly(t, objects)
	}
	// A normal alarm needs no UID. An auxiliary UID is not a primary collision.
	objects := dailyGroupObjects(t, p)
	auxiliary := ical.NewCalendar()
	alarm := ical.NewComponent("VALARM")
	alarm.Props.SetText("UID", objects[0].Data.Events()[0].Props.Get("UID").Value)
	auxiliary.Children = append(auxiliary.Children, alarm)
	objects = append(objects, caldav.CalendarObject{Path: dailyGroupLocation.CalendarID + "auxiliary.ics", ETag: `"auxiliary"`, Data: auxiliary})
	if !dailyGroupRead(t, objects).Groups[0].StructuralReady {
		t.Fatal("auxiliary UID incorrectly shared primary namespace")
	}
	for _, name := range []string{"VTODO", "VALARM", "VTIMEZONE"} {
		objects = dailyGroupObjects(t, p)
		other := ical.NewComponent(name)
		other.Props.SetText("DESCRIPTION", "[hassle:v2 task="+p.Task()+"]")
		if name == "VTODO" {
			other.Props.SetText("UID", "unique-todo")
		}
		objects[0].Data.Children = append(objects[0].Data.Children, other)
		dailyGroupReadonly(t, objects)
	}
	// Denial from a nested UID is retained even without any metadata description.
	objects = dailyGroupObjects(t, p)
	other := dailyPartsCloneObject(objects[0])
	other.Path = dailyGroupLocation.CalendarID + "orphan.ics"
	other.Data.Events()[0].Props.Del("DESCRIPTION")
	other.Data.Events()[0].Props.SetText("UID", "benign")
	other.Data.Events()[0].Children[0].Props.SetText("UID", strings.ToUpper(groupPhysicalPrefix+p.Task())+"-orphan")
	objects = append(objects, other)
	dailyGroupReadonly(t, objects)
}

func TestGroupReservedPhysicalPrefixIsDenialNotOwnership(t *testing.T) {
	p := dailyV2Compile(t, dailyV2Schedule("2027-02-12T09:17", 24, false, ""))
	for _, kind := range []string{"href", "UID", "opaque-href"} {
		t.Run(kind, func(t *testing.T) {
			objects := dailyGroupObjects(t, p)
			o := &objects[0]
			e := o.Data.Events()[0]
			e.Props.Del("DESCRIPTION")
			if kind == "UID" {
				e.Props.SetText("UID", strings.ToUpper(groupPhysicalPrefix+p.Task())+"-orphan")
			} else {
				o.Path = dailyGroupLocation.CalendarID + strings.ToUpper(groupPhysicalPrefix+p.Task()) + "-orphan.ics"
				if kind == "opaque-href" {
					o.Data = nil
				}
			}
			model := dailyGroupRead(t, objects)
			if len(model.Groups) != 1 || model.Groups[0].StructuralReady || !model.Physical[0].Reserved || model.Groups[0].Task != p.Task() {
				t.Fatal("prefix disappeared or promoted", model)
			}
		})
	}
	// With full native metadata, arbitrary physical identities remain independent
	// of the web creator's naming convention, even if they resemble another task.
	objects := dailyGroupObjects(t, p)
	objects[0].Path = dailyGroupLocation.CalendarID + groupPhysicalPrefix + "bbbb1111-1111-4111-8111-111111111111.ics"
	objects[0].Data.Events()[0].Props.SetText("UID", groupPhysicalPrefix+"cccc1111-1111-4111-8111-111111111111")
	if !dailyGroupRead(t, objects).Groups[0].StructuralReady {
		t.Fatal("web naming conferred native authority")
	}
}

// Independently written browser-shaped JSON, not an encoding/json round trip.
const groupBrowserRequest = ` {
  "schedule": {
    "workHours": false, "until": "", "interval": 24.0,
    "cadence": "hourly", "timeZone": "Australia/Melbourne",
    "start": "2027-02-12T09:17", "title": "Caf\u00e9 <>& \ud83d\ude00 \ufffd"
  },
  "fence": "%s", "incarnation": "00000000-0000-4000-8000-000000000002",
  "task": "00000000-0000-4000-8000-000000000001",
  "operationId": "aaaa1111-1111-4111-8111-111111111111", "action": "create"
} `

func TestGroupPublicRequestSemanticJSONAndCanonicalReplay(t *testing.T) {
	s := groupTestStore(t)
	expected := groupTestRequest(t, s, dailyV2Schedule("2027-02-12T09:17", 24, false, ""))
	expected.Schedule.Title = "Café <>& 😀 �"
	wire := fmt.Sprintf(groupBrowserRequest, expected.Fence)
	request, err := DecodeGroupRequest([]byte(wire))
	if err != nil || !reflect.DeepEqual(request, expected) {
		t.Fatal("browser request differs", request, err)
	}
	canonical, _ := json.Marshal(expected)
	fromCanonical, err := DecodeGroupRequest(canonical)
	if err != nil || !reflect.DeepEqual(fromCanonical, request) {
		t.Fatal("canonical and browser requests differ", err)
	}
	for _, equivalent := range []string{
		strings.Replace(wire, `<>&`, `\u003c\u003e\u0026`, 1),
		strings.Replace(wire, `Caf\u00e9 <>& \ud83d\ude00 \ufffd`, "Café <>& 😀 �", 1),
		strings.Replace(wire, `"action"`, `"\u0061ction"`, 1),
	} {
		decoded, err := DecodeGroupRequest([]byte(equivalent))
		if err != nil || !reflect.DeepEqual(decoded, expected) {
			t.Fatal("escaped equivalent refused", err)
		}
	}
	b := groupTestBackend(nil)
	b.afterWrite = func(int) error { return groupTestError }
	if _, err := groupRun(t, b, s, request); err == nil || len(b.writes) != 1 {
		t.Fatal("lost acknowledgement fixture failed", err)
	}
	if groupPending(t, s).Request != string(canonical) {
		t.Fatal("wire representation stored instead of canonical identity")
	}
	b.afterWrite = nil
	out, err := groupRun(t, b, s, fromCanonical)
	if err != nil || !out.Settled || len(b.writes) != 1 {
		t.Fatal("equivalent request did not reconcile read-only", out, err)
	}
	// Internal stored request remains canonical even inside a canonically encoded
	// operation/state. Public decoding must not weaken this private invariant.
	prepared, err := PrepareGroup(expected, groupTestBinding, GroupSnapshot{}, now)
	if err != nil {
		t.Fatal(err)
	}
	op, err := prepared.operation()
	if err != nil {
		t.Fatal(err)
	}
	for _, noncanonical := range []string{wire, " " + string(canonical), strings.Replace(string(canonical), `\u003c`, `<`, 1)} {
		op.Request = noncanonical
		if err := groupValidateOperation(op); err == nil {
			t.Fatal("noncanonical private request accepted")
		}
		encoded, _ := json.Marshal(op)
		if _, err := (GroupOperation{canonical: string(encoded)}).operation(); err == nil {
			t.Fatal("private operation accepted noncanonical embedded request")
		}
		state, _ := groupDecodeState(s.raw)
		state.Pending = &op
		// The settled fence differs, so use the request's original generation.
		state.Generation = 0
		encoded, _ = groupEncodeState(state)
		if _, err := groupDecodeState(encoded); err == nil {
			t.Fatal("private state accepted noncanonical embedded request")
		}
	}
}

func TestGroupPublicRequestRejectsNonSemanticFieldsAndInvalidUnicode(t *testing.T) {
	s := groupTestStore(t)
	r := groupTestRequest(t, s, dailyV2Schedule("2027-02-12T09:17", 24, false, ""))
	wire := fmt.Sprintf(groupBrowserRequest, r.Fence)
	canonical, _ := json.Marshal(r)
	scheduleJSON, _ := json.Marshal(r.Schedule)
	nullSchedule := strings.Replace(string(canonical), string(scheduleJSON), "null", 1)
	for name, bad := range map[string]string{
		"top-duplicate":            strings.Replace(wire, `"action": "create"`, `"action": "create", "action": "create"`, 1),
		"escaped-duplicate":        strings.Replace(wire, `"action": "create"`, `"action": "create", "\u0061ction": "create"`, 1),
		"top-alias":                strings.Replace(wire, `"action":`, `"Action":`, 1),
		"top-unknown":              strings.Replace(wire, `"action":`, `"href": "forbidden", "action":`, 1),
		"top-null":                 strings.Replace(wire, `"action": "create"`, `"action": null`, 1),
		"empty-mixed-revision":     strings.Replace(wire, `"action":`, `"revision": "", "action":`, 1),
		"null-mixed-revision":      strings.Replace(wire, `"action":`, `"revision": null, "action":`, 1),
		"schedule-null":            nullSchedule,
		"nested-duplicate":         strings.Replace(wire, `"title":`, `"title": "other", "title":`, 1),
		"nested-escaped-duplicate": strings.Replace(wire, `"title":`, `"\u0074itle": "other", "title":`, 1),
		"nested-alias":             strings.Replace(wire, `"workHours":`, `"WorkHours":`, 1),
		"nested-unknown":           strings.Replace(wire, `"title":`, `"extra": false, "title":`, 1),
		"nested-null-string":       strings.Replace(wire, `"until": ""`, `"until": null`, 1),
		"nested-null-bool":         strings.Replace(wire, `"workHours": false`, `"workHours": null`, 1),
		"nested-null-number":       strings.Replace(wire, `"interval": 24.0`, `"interval": null`, 1),
		"nested-wrong-type":        strings.Replace(wire, `"interval": 24.0`, `"interval": "24"`, 1),
		"missing-schedule-field":   strings.Replace(wire, `"workHours": false,`, ``, 1),
		"missing-identity":         strings.Replace(wire, `"task": "00000000-0000-4000-8000-000000000001",`, ``, 1),
		"high-surrogate":           strings.Replace(wire, `\ud83d\ude00`, `\ud83d`, 1),
		"low-surrogate":            strings.Replace(wire, `\ud83d\ude00`, `\ude00`, 1),
		"high-then-normal":         strings.Replace(wire, `\ud83d\ude00`, `\ud83d\u0041`, 1),
		"reversed-pair":            strings.Replace(wire, `\ud83d\ude00`, `\ude00\ud83d`, 1),
		"escaped-slash-not-pair":   strings.Replace(wire, `\ud83d\ude00`, `\ud83d\\ude00`, 1),
		"invalid-utf8":             strings.Replace(wire, `<>&`, string([]byte{0xff}), 1),
		"trailing-object":          wire + `{}`,
		"array-root":               `[` + wire + `]`,
		"null-root":                `null`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeGroupRequest([]byte(bad)); err == nil {
				t.Fatal("unsafe public request accepted")
			}
		})
	}
	deleteWire := fmt.Sprintf(` { "revision": "%s", "fence": "%s", "action": "delete", "task": "%s", "incarnation": "%s", "operationId": "%s" } `, strings.Repeat("a", 64), r.Fence, r.Task, r.Incarnation, r.OperationID)
	if _, err := DecodeGroupRequest([]byte(deleteWire)); err != nil {
		t.Fatal("ordinary delete JSON refused", err)
	}
	for _, schedule := range []string{`{}`, `null`, `{"title":""}`} {
		bad := strings.Replace(deleteWire, `"action":`, `"schedule":`+schedule+`, "action":`, 1)
		if _, err := DecodeGroupRequest([]byte(bad)); err == nil {
			t.Fatal("delete accepted forbidden schedule presence")
		}
	}
}
