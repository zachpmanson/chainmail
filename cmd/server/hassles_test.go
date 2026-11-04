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
	objects []caldav.CalendarObject
	calls   int
	deny    bool
	serial  int
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
func TestHasslesRejectsHourlyBeforeUpstream(t *testing.T) {
	h, f, loc := hassleHarness(t)
	chooseHassles(t, h, loc)
	before := f.calls
	in := hassles.Mutation{Location: loc, Action: "create", ID: hassleUUID, Schedule: hassles.Schedule{Title: "Bad", Start: time.Now().Format("2006-01-02T15:04"), TimeZone: hassles.Zone, Cadence: "hourly", Interval: 1}}
	if r := h.do(t, "POST", "/v1/hassles", hassleJSON(t, in)); r.status != 400 || f.calls != before {
		t.Fatal("hourly reached upstream", r.status, string(r.body))
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
