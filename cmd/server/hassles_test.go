package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
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

const reminderCalendar = "/caldav/v2/reminders@example.test/events/"
const otherCalendar = "/caldav/v2/other@example.test/events/"
const hassleUUID = "607cbf13-2aac-414d-bebd-df221c943de9"

type calendarFixture struct {
	objects  []caldav.CalendarObject
	calls    int
	writes   int
	deny     bool
	conflict bool
	serial   int
}

func (f *calendarFixture) Calendars(context.Context) ([]hassles.Calendar, error) {
	f.calls++
	return []hassles.Calendar{{ID: reminderCalendar, Name: "Reminders"}}, nil
}
func (f *calendarFixture) Objects(context.Context, string) ([]caldav.CalendarObject, error) {
	f.calls++
	return f.objects, nil
}
func (f *calendarFixture) Write(_ context.Context, _, href, etag, body string) error {
	f.calls++
	f.writes++
	if strings.Contains(body, "FREQ=HOURLY") {
		return errors.New("offline provider rejects native HOURLY")
	}
	if f.conflict {
		return hassles.ErrConflict
	}
	if f.deny {
		return errors.New("calendar is read-only (HTTP 403)")
	}
	idx := -1
	for i, o := range f.objects {
		if o.Path == href {
			idx = i
			break
		}
	}
	if etag == "*" && idx >= 0 || etag != "*" && (idx < 0 || f.objects[idx].ETag != etag) {
		return hassles.ErrConflict
	}
	if body == "" {
		f.objects = append(f.objects[:idx], f.objects[idx+1:]...)
		return nil
	}
	c, err := ical.NewDecoder(strings.NewReader(body)).Decode()
	if err != nil {
		return err
	}
	f.serial++
	o := caldav.CalendarObject{Path: href, ETag: fmt.Sprintf(`"%d"`, f.serial), Data: c}
	if idx < 0 {
		f.objects = append(f.objects, o)
	} else {
		f.objects[idx] = o
	}
	return nil
}
func hassleHarness(t *testing.T) (*harness, *calendarFixture, hassles.Location) {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	h := testServer(t)
	h.hasslesEnabled = true
	injectHassleLease(t, h)
	f := &calendarFixture{}
	h.openHassles = func(_ context.Context, id string) (hassles.Backend, error) {
		if id != "personal" {
			t.Errorf("wrong account opened %q", id)
		}
		return f, nil
	}
	if err := h.store.PutGmailAccount(corpus.GmailAccount{ID: "personal", Email: "offline@example.test", DisplayName: "Offline"}); err != nil {
		t.Fatal(err)
	}
	p, _ := gmailclient.AccountTokenPath("personal")
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("inert offline fixture; never read"), 0600); err != nil {
		t.Fatal(err)
	}
	return h, f, hassles.Location{AccountID: "personal", CalendarID: reminderCalendar}
}
func hassleJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func chooseHassles(t *testing.T, h *harness, loc hassles.Location) {
	t.Helper()
	r := h.do(t, "POST", "/v1/hassles/location", hassleJSON(t, map[string]any{"location": loc, "previous": hassles.Location{}, "dedicated": true}))
	if r.status != 200 {
		t.Fatalf("choose %d %s", r.status, r.body)
	}
}
func hassleList(t *testing.T, h *harness) []hassles.Task {
	t.Helper()
	r := h.do(t, "GET", "/v1/hassles?accountId=personal&calendarId="+reminderCalendar, nil)
	if r.status != 200 {
		t.Fatalf("list %d %s", r.status, r.body)
	}
	return decode[struct {
		Tasks []hassles.Task `json:"tasks"`
	}](t, r).Tasks
}
func TestHasslesOfflineCRUDAndPermissionErrors(t *testing.T) {
	h, f, loc := hassleHarness(t)
	chooseHassles(t, h, loc)
	s := hassles.Schedule{Title: "Offline bill", Start: "2027-01-01T08:30", TimeZone: hassles.Zone, Cadence: "weekly", Interval: 1}
	in := hassles.Mutation{Location: loc, Action: "create", ID: hassleUUID, Schedule: s}
	r := h.do(t, "POST", "/v1/hassles", hassleJSON(t, in))
	if r.status != 200 {
		t.Fatalf("create %d %s", r.status, r.body)
	}
	r = h.do(t, "POST", "/v1/hassles", hassleJSON(t, in))
	if r.status != 200 || len(f.objects) != 1 {
		t.Fatal("duplicate create", string(r.body))
	}
	tasks := hassleList(t, h)
	if len(tasks) != 1 || tasks[0].Title != s.Title {
		t.Fatal(tasks)
	}
	in.Action = "edit"
	in.ID = tasks[0].ID
	in.Revision = tasks[0].Revision
	in.Schedule.Title = "Edited bill"
	r = h.do(t, "POST", "/v1/hassles", hassleJSON(t, in))
	if r.status != 200 {
		t.Fatalf("edit %d %s", r.status, r.body)
	}
	r = h.do(t, "POST", "/v1/hassles", hassleJSON(t, in))
	if r.status != 409 {
		t.Fatal("stale edit accepted", r.status, string(r.body))
	}
	tasks = hassleList(t, h)
	in.Action = "done"
	in.Revision = tasks[0].Revision
	f.deny = true
	r = h.do(t, "POST", "/v1/hassles", hassleJSON(t, in))
	if r.status != 502 || !strings.Contains(string(r.body), "read-only") || len(f.objects) != 1 {
		t.Fatal("permission failure lied", string(r.body))
	}
	f.deny = false
	r = h.do(t, "POST", "/v1/hassles", hassleJSON(t, in))
	if r.status != 200 || len(hassleList(t, h)) != 0 {
		t.Fatal("done did not remove", string(r.body))
	}
	// Task content never becomes a setting/table row.
	raw, _, _ := h.store.Setting(corpus.SettingHasslesLocation)
	if strings.Contains(raw, "bill") || raw != string(hassleJSON(t, loc)) {
		t.Fatal("location not atomic", raw)
	}
}
func TestHasslesFeatureSelectionIsolationAndDisconnect(t *testing.T) {
	h, f, loc := hassleHarness(t)
	h.hasslesEnabled = false
	for _, p := range []string{"/v1/hassles?accountId=personal&calendarId=" + reminderCalendar, "/v1/hassles/calendars?accountId=personal"} {
		if r := h.do(t, "GET", p, nil); r.status != 403 {
			t.Fatal("ungated calendar read", r.status)
		}
	}
	if r := h.do(t, "POST", "/v1/hassles", []byte(`{}`)); r.status != 403 || f.calls != 0 {
		t.Fatal("ungated write")
	}
	config := decode[hassleConfig](t, h.do(t, "GET", "/v1/hassles/location", nil))
	if config.Enabled {
		t.Fatal("grant incorrectly exposed")
	}
	h.hasslesEnabled = true
	for _, account := range []string{"", "legacy", "wrong"} {
		if r := h.do(t, "GET", "/v1/hassles/calendars?accountId="+account, nil); r.status == 200 {
			t.Fatal("account fallback", account)
		}
	}
	chooseHassles(t, h, loc)
	original, _, _ := h.store.Setting(corpus.SettingHasslesLocation)
	for _, body := range []string{
		`{"location":{"accountId":"personal","calendarId":"` + otherCalendar + `"},"previous":{"accountId":"personal","calendarId":"` + reminderCalendar + `"},"dedicated":true}`,
		`{"location":{"accountId":"personal","calendarId":"` + reminderCalendar + `"},"previous":{"accountId":"personal","calendarId":"` + reminderCalendar + `"},"dedicated":false}`,
		`{"location":{"accountId":"personal","calendarId":"` + reminderCalendar + `"},"previous":{},"dedicated":true}`,
		`{"location":{"accountId":"personal","calendarId":"` + reminderCalendar + `"},"previous":{},"dedicated":true,"extra":"bad"}`,
	} {
		if r := h.do(t, "POST", "/v1/hassles/location", []byte(body)); r.status == 200 {
			t.Fatal("invalid selection accepted", body)
		}
		raw, _, _ := h.store.Setting(corpus.SettingHasslesLocation)
		if raw != original {
			t.Fatal("failed selection partially changed location")
		}
	}
	calls := f.calls
	for _, path := range []string{"/v1/hassles?accountId=wrong&calendarId=" + reminderCalendar, "/v1/hassles?accountId=personal&calendarId=" + otherCalendar} {
		if r := h.do(t, "GET", path, nil); r.status != 409 {
			t.Fatal("wrong location accepted", r.status)
		}
	}
	if f.calls != calls {
		t.Fatal("wrong location reached calendar")
	}
	in := hassles.Mutation{Location: hassles.Location{AccountID: "personal", CalendarID: otherCalendar}, Action: "delete", ID: "task:fake", Revision: "stale"}
	if r := h.do(t, "POST", "/v1/hassles", hassleJSON(t, in)); r.status != 409 || f.calls != calls {
		t.Fatal("stale location mutation reached calendar")
	}
	p, _ := gmailclient.AccountTokenPath("personal")
	_ = os.Remove(p)
	if r := h.do(t, "GET", "/v1/hassles?accountId=personal&calendarId="+reminderCalendar, nil); r.status != 409 || f.calls != calls {
		t.Fatal("disconnected calendar reached upstream")
	}
}
func TestHasslesRejectsUnsupportedCadenceBeforeUpstream(t *testing.T) {
	h, f, loc := hassleHarness(t)
	chooseHassles(t, h, loc)
	before := f.calls
	in := hassles.Mutation{Location: loc, Action: "create", ID: hassleUUID, Schedule: hassles.Schedule{Title: "Bad", Start: "2026-11-06T09:00", TimeZone: hassles.Zone, Cadence: "minutely", Interval: 1}}
	if r := h.do(t, "POST", "/v1/hassles", hassleJSON(t, in)); r.status != 400 || f.calls != before {
		t.Fatal("unsupported cadence reached upstream", r.status, string(r.body))
	}
}

// HTTP policy now rejects legacy hourly bodies; internal hourly tests retain the reference transport fixtures.
func TestHasslesLegacyHourlyHTTPRefusesFallback(t *testing.T) {
	h, f, loc := hassleHarness(t)
	chooseHassles(t, h, loc)
	for _, action := range []string{"create", "edit"} {
		in := hassles.Mutation{Location: loc, Action: action, ID: hassleUUID, Schedule: hassles.Schedule{Title: "Office", Start: "2026-11-06T09:00", TimeZone: hassles.Zone, Cadence: "hourly", Interval: 0.5, WorkHours: true}}
		before := f.calls
		r := h.do(t, "POST", "/v1/hassles", hassleJSON(t, in))
		if r.status != 400 || f.calls != before || f.writes != 0 {
			t.Fatalf("hourly fallback reached provider: %d %s", r.status, r.body)
		}
	}
}

func TestEnabledHasslesCrossOriginNeverReachesCalendar(t *testing.T) {
	h, f, loc := hassleHarness(t)
	for _, endpoint := range []string{"/v1/hassles/location", "/v1/hassles"} {
		req := httptest.NewRequest("POST", "http://localhost:9876"+endpoint, strings.NewReader(string(hassleJSON(t, map[string]any{"location": loc, "dedicated": true}))))
		req.Header.Set("Origin", "http://attacker.invalid")
		req.Header.Set("Sec-Fetch-Site", "cross-site")
		req.Header.Set("Content-Type", "text/plain")
		w := httptest.NewRecorder()
		h.routes().ServeHTTP(w, req)
		if w.Code != 403 || f.calls != 0 {
			t.Fatalf("CSRF reached Calendar: %d %d", w.Code, f.calls)
		}
	}
}

func legacyHassleMutation(t *testing.T, f *calendarFixture, loc hassles.Location) hassles.Mutation {
	t.Helper()
	c, err := hassles.NewCalendar(hassleUUID, hassles.Schedule{Title: "Legacy offline", Start: "2027-01-01T08:30", TimeZone: hassles.Zone, Cadence: "daily", Interval: 1}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	c.Events()[0].Props.SetText("UID", "original-legacy-uid")
	c.Events()[0].Props.SetText("DESCRIPTION", "[docket]\nHuman notes; keep")
	f.objects = []caldav.CalendarObject{{Path: reminderCalendar + "original.ics", ETag: `"original"`, Data: c}}
	tasks, err := hassles.Inventory(f.objects, time.Now())
	if err != nil || len(tasks) != 1 {
		t.Fatalf("legacy fixture: %+v %v", tasks, err)
	}
	id, dry := strings.ToUpper(hassleUUID), true
	return hassles.Mutation{Location: loc, Action: "adopt", ID: tasks[0].ID, Revision: tasks[0].Revision, AdoptID: &id, DryRun: &dry}
}

func TestHasslesAdoptionHTTPPreviewCommitAndRetry(t *testing.T) {
	h, f, loc := hassleHarness(t)
	chooseHassles(t, h, loc)
	in := legacyHassleMutation(t, f, loc)
	before, _ := hassles.Encode(f.objects[0].Data)
	r := h.do(t, "POST", "/v1/hassles", hassleJSON(t, in))
	if r.status != 200 {
		t.Fatalf("preview %d %s", r.status, r.body)
	}
	preview := decode[hassles.Result](t, r)
	if f.writes != 0 || preview.Adoption == nil || preview.Adoption.Before != before || preview.Adoption.TaskID != "task:"+hassleUUID {
		t.Fatalf("preview mutated or missing export: %+v", preview)
	}
	unchanged, _ := hassles.Encode(f.objects[0].Data)
	if before != unchanged {
		t.Fatal("preview mutated input")
	}
	commit := false
	in.DryRun, in.AdoptHash = &commit, &preview.Adoption.Fingerprint
	r = h.do(t, "POST", "/v1/hassles", hassleJSON(t, in))
	if r.status != 200 || f.writes != 1 || len(f.objects) != 1 || f.objects[0].Path != reminderCalendar+"original.ics" {
		t.Fatalf("commit %d %s", r.status, r.body)
	}
	uid, _ := f.objects[0].Data.Events()[0].Props.Text("UID")
	if uid != "original-legacy-uid" {
		t.Fatal("adoption replaced UID")
	}
	r = h.do(t, "POST", "/v1/hassles", hassleJSON(t, in))
	if r.status != 200 || decode[hassles.Result](t, r).Message != "Already adopted" || f.writes != 1 {
		t.Fatalf("retry %d %s", r.status, r.body)
	}
	wrong := strings.Repeat("0", 64)
	in.AdoptHash = &wrong
	if r = h.do(t, "POST", "/v1/hassles", hassleJSON(t, in)); r.status != 409 || f.writes != 1 {
		t.Fatal("wrong hash accepted", r.status, string(r.body))
	}
}

func TestHasslesAdoptionHTTPRejectsInvalidFieldsBeforeUpstream(t *testing.T) {
	h, f, loc := hassleHarness(t)
	chooseHassles(t, h, loc)
	valid := legacyHassleMutation(t, f, loc)
	for _, action := range []string{"create", "edit", "done", "delete"} {
		for _, flag := range []map[string]any{{"dryRun": false}, {"dryRun": true}, {"adoptId": hassleUUID}, {"adoptHash": strings.Repeat("0", 64)}, {"dryRun": nil}, {"adoptId": nil}, {"adoptHash": nil}, {"DRYRUN": nil}} {
			body := map[string]any{"location": loc, "action": action, "id": hassleUUID}
			for k, v := range flag {
				body[k] = v
			}
			calls := f.calls
			if r := h.do(t, "POST", "/v1/hassles", hassleJSON(t, body)); r.status != 400 || f.calls != calls {
				t.Fatalf("%s flag reached upstream: %d %s", action, r.status, r.body)
			}
		}
	}
	for _, change := range []func(*hassles.Mutation){
		func(in *hassles.Mutation) { in.DryRun = nil },
		func(in *hassles.Mutation) { dry := false; in.DryRun = &dry },
		func(in *hassles.Mutation) { in.AdoptID = nil },
		func(in *hassles.Mutation) { in.ID = "task:" + hassleUUID },
		func(in *hassles.Mutation) { in.Revision = "stale" },
		func(in *hassles.Mutation) { in.Schedule.Title = "not metadata" },
	} {
		in := valid
		change(&in)
		calls := f.calls
		if r := h.do(t, "POST", "/v1/hassles", hassleJSON(t, in)); r.status != 400 || f.calls != calls {
			t.Fatalf("invalid adoption reached upstream: %d %s", r.status, r.body)
		}
	}
	body := string(hassleJSON(t, valid))
	body = strings.TrimSuffix(body, "}") + `,"unknown":true}`
	calls := f.calls
	if r := h.do(t, "POST", "/v1/hassles", []byte(body)); r.status != 400 || f.calls != calls {
		t.Fatal("unknown field reached upstream")
	}
}

func TestHasslesAdoptionHTTPGrantLocationDiscoveryAndOriginGuards(t *testing.T) {
	for _, kind := range []string{"disabled", "unselected", "wrong location", "discovery", "disconnect", "origin", "stale revision", "protected", "upstream denied", "upstream conflict"} {
		t.Run(kind, func(t *testing.T) {
			h, f, loc := hassleHarness(t)
			if kind != "unselected" {
				chooseHassles(t, h, loc)
			}
			in := legacyHassleMutation(t, f, loc)
			want := 409
			switch kind {
			case "disabled":
				h.hasslesEnabled = false
				want = 403
			case "wrong location":
				in.Location.CalendarID = otherCalendar
			case "discovery":
				in.Location.CalendarID = otherCalendar
				_ = h.store.PutSetting(corpus.SettingHasslesLocation, string(hassleJSON(t, in.Location)))
			case "disconnect":
				p, _ := gmailclient.AccountTokenPath("personal")
				_ = os.Remove(p)
			case "stale revision":
				f.objects[0].ETag = `"new"`
			case "protected":
				f.objects[0].Data.Events()[0].Props.SetText("ORGANIZER", "mailto:owner@example.test")
				tasks, _ := hassles.Inventory(f.objects, time.Now())
				in.Revision = tasks[0].Revision
				want = 502
			case "upstream denied", "upstream conflict":
				f.deny = kind == "upstream denied"
				f.conflict = kind == "upstream conflict"
				preview := decode[hassles.Result](t, h.do(t, "POST", "/v1/hassles", hassleJSON(t, in)))
				dry := false
				in.DryRun, in.AdoptHash = &dry, &preview.Adoption.Fingerprint
				if kind == "upstream denied" {
					want = 502
				}
			}
			calls := f.calls
			if kind == "origin" {
				req := httptest.NewRequest("POST", "http://localhost:9876/v1/hassles", strings.NewReader(string(hassleJSON(t, in))))
				req.Header.Set("Origin", "http://attacker.invalid")
				req.Header.Set("Sec-Fetch-Site", "cross-site")
				req.Header.Set("Content-Type", "application/json")
				w := httptest.NewRecorder()
				h.routes().ServeHTTP(w, req)
				if w.Code != 403 || f.calls != calls || f.writes != 0 {
					t.Fatal("adoption CSRF reached calendar")
				}
				return
			}
			r := h.do(t, "POST", "/v1/hassles", hassleJSON(t, in))
			wantWrites := 0
			if kind == "upstream denied" || kind == "upstream conflict" {
				wantWrites = 1
			}
			if r.status != want || f.writes != wantWrites {
				t.Fatalf("guard %s: %d %s, %d writes", kind, r.status, r.body, f.writes)
			}
			if (kind == "disabled" || kind == "unselected" || kind == "wrong location" || kind == "disconnect") && f.calls != calls {
				t.Fatal("guard reached calendar")
			}
		})
	}
}

func instanceHassleMutation(t *testing.T, f *calendarFixture, loc hassles.Location) hassles.Mutation {
	t.Helper()
	c, err := hassles.NewCalendar(hassleUUID, hassles.Schedule{Title: "Instance offline", Start: "2027-01-01T08:30", TimeZone: hassles.Zone, Cadence: "daily", Interval: 1}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	f.objects = []caldav.CalendarObject{{Path: reminderCalendar + "instance.ics", ETag: `"original"`, Data: c}}
	tasks, err := hassles.Inventory(f.objects, time.Now())
	if err != nil || len(tasks) != 1 || tasks[0].Instance == nil {
		t.Fatalf("instance fixture %+v %v", tasks, err)
	}
	task := tasks[0]
	dry := true
	return hassles.Mutation{Location: loc, Action: "done-instance", ID: task.ID, Revision: task.Revision, InstanceAt: &task.Instance.At, InstanceToken: &task.Instance.Token, DryRun: &dry}
}
func TestHasslesInstanceHTTPPreviewCommitAndRetry(t *testing.T) {
	h, f, loc := hassleHarness(t)
	chooseHassles(t, h, loc)
	in := instanceHassleMutation(t, f, loc)
	r := h.do(t, "POST", "/v1/hassles", hassleJSON(t, in))
	if r.status != 200 || f.writes != 0 {
		t.Fatalf("preview %d %s writes %d", r.status, r.body, f.writes)
	}
	preview := decode[hassles.Result](t, r)
	if preview.Completion == nil || preview.Completion.Operation != "exclude-occurrence" || preview.Completion.Before == preview.Completion.After {
		t.Fatal("missing semantic preview", string(r.body))
	}
	dry := false
	in.DryRun = &dry
	r = h.do(t, "POST", "/v1/hassles", hassleJSON(t, in))
	if r.status != 200 || f.writes != 1 {
		t.Fatalf("commit %d %s writes %d", r.status, r.body, f.writes)
	}
	tasks := hassleList(t, h)
	if len(tasks) != 1 || tasks[0].Schedule == nil || tasks[0].Instance == nil || tasks[0].Instance.At == *in.InstanceAt {
		t.Fatalf("bad inventory %+v", tasks)
	}
	r = h.do(t, "POST", "/v1/hassles", hassleJSON(t, in))
	if r.status != 409 || f.writes != 1 {
		t.Fatal("retry completed next", r.status, string(r.body))
	}
}
func TestHasslesInstanceHTTPBoundaryGuards(t *testing.T) {
	for _, kind := range []string{"disabled", "unselected", "wrong location", "discovery", "disconnect", "origin", "stale revision", "wrong token", "weak etag", "participant", "upstream denied", "upstream conflict"} {
		t.Run(kind, func(t *testing.T) {
			h, f, loc := hassleHarness(t)
			if kind != "unselected" {
				chooseHassles(t, h, loc)
			}
			in := instanceHassleMutation(t, f, loc)
			dry := false
			in.DryRun = &dry
			want := 409
			switch kind {
			case "disabled":
				h.hasslesEnabled = false
				want = 403
			case "wrong location":
				in.Location.CalendarID = otherCalendar
			case "discovery":
				in.Location.CalendarID = otherCalendar
				_ = h.store.PutSetting(corpus.SettingHasslesLocation, string(hassleJSON(t, in.Location)))
			case "disconnect":
				p, _ := gmailclient.AccountTokenPath("personal")
				_ = os.Remove(p)
			case "stale revision":
				f.objects[0].ETag = `"changed"`
			case "wrong token":
				token := strings.Repeat("0", 64)
				in.InstanceToken = &token
			case "weak etag", "participant":
				if kind == "weak etag" {
					f.objects[0].ETag = `W/"weak"`
				} else {
					f.objects[0].Data.Events()[0].Props.SetText("ATTENDEE", "mailto:offline@example.test")
				}
				tasks, _ := hassles.Inventory(f.objects, time.Now())
				in.Revision = tasks[0].Revision
				want = 502
			case "upstream denied":
				f.deny = true
				want = 502
			case "upstream conflict":
				f.conflict = true
			}
			calls := f.calls
			if kind == "origin" {
				req := httptest.NewRequest("POST", "http://localhost:9876/v1/hassles", strings.NewReader(string(hassleJSON(t, in))))
				req.Header.Set("Origin", "http://attacker.invalid")
				req.Header.Set("Sec-Fetch-Site", "cross-site")
				req.Header.Set("Content-Type", "application/json")
				w := httptest.NewRecorder()
				h.routes().ServeHTTP(w, req)
				if w.Code != 403 || f.calls != calls || f.writes != 0 {
					t.Fatal("CSRF reached Calendar")
				}
				return
			}
			r := h.do(t, "POST", "/v1/hassles", hassleJSON(t, in))
			writes := 0
			if kind == "upstream denied" || kind == "upstream conflict" {
				writes = 1
			}
			if r.status != want || f.writes != writes {
				t.Fatalf("%s: %d %s writes %d", kind, r.status, r.body, f.writes)
			}
			if (kind == "disabled" || kind == "unselected" || kind == "wrong location" || kind == "disconnect") && f.calls != calls {
				t.Fatal("guard reached Calendar")
			}
		})
	}
}
func TestHasslesInstanceHTTPStrictFieldsBeforeUpstream(t *testing.T) {
	h, f, loc := hassleHarness(t)
	chooseHassles(t, h, loc)
	in := instanceHassleMutation(t, f, loc)
	for _, field := range []string{"instanceAt", "instanceToken", "dryRun", "adoptId", "adoptHash"} {
		for _, value := range []any{nil, "invalid"} {
			var body map[string]any
			_ = json.Unmarshal(hassleJSON(t, in), &body)
			body[field] = value
			calls := f.calls
			r := h.do(t, "POST", "/v1/hassles", hassleJSON(t, body))
			if r.status != 400 || f.calls != calls || f.writes != 0 {
				t.Fatalf("ignored %s %v: %d %s", field, value, r.status, r.body)
			}
		}
	}
	for _, action := range []string{"done", "delete", "create", "edit", "adopt"} {
		for _, field := range []string{"instanceAt", "instanceToken"} {
			for _, value := range []any{nil, "ignored"} {
				body := map[string]any{"location": loc, "action": action, "id": hassleUUID, field: value}
				calls := f.calls
				r := h.do(t, "POST", "/v1/hassles", hassleJSON(t, body))
				if r.status != 400 || f.calls != calls {
					t.Fatal("cross-action witness ignored", action, field, r.status)
				}
			}
		}
	}
}
