package hassles

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-ical"
	"github.com/emersion/go-webdav/caldav"
)

const formatCorpusSHA = "f066f7a4da030781aecde87b42a467bef58df2a13f42c3232d1cd44a46c5a62e"
const formatUUID = "10000000-0000-4000-8000-000000000001"

type formatDate struct {
	Type string `json:"type"`
	Raw  string `json:"raw"`
	TZID string `json:"tzid,omitempty"`
}
type formatEvent struct {
	Key         string       `json:"key"`
	UID         string       `json:"uid"`
	Description string       `json:"description"`
	Title       string       `json:"title"`
	Start       formatDate   `json:"dtstart"`
	End         *formatDate  `json:"dtend,omitempty"`
	Duration    *int         `json:"durationSeconds"`
	Rule        string       `json:"rrule,omitempty"`
	RDate       []formatDate `json:"rdate,omitempty"`
	EXDate      []formatDate `json:"exdate,omitempty"`
	Alarm       int          `json:"alarmMinutes"`
	Attendees   []string     `json:"attendees"`
	Organizer   string       `json:"organizer,omitempty"`
	Detached    string       `json:"detachedFrom,omitempty"`
	ETag        string       `json:"etagStrength"`
}
type formatNativeInstance struct {
	Key   string `json:"eventKey"`
	Begin string `json:"begin"`
	End   string `json:"end"`
}
type formatDifference struct {
	Path    string   `json:"path"`
	Web     any      `json:"web"`
	Android any      `json:"android"`
	Reason  string   `json:"reason"`
	Source  []string `json:"source"`
}
type formatCase struct {
	ID          string                 `json:"id"`
	Kind        string                 `json:"kind"`
	Now         string                 `json:"now"`
	Schedule    *Schedule              `json:"schedule,omitempty"`
	Events      []formatEvent          `json:"events,omitempty"`
	Instances   []formatNativeInstance `json:"nativeInstances"`
	Actions     []string               `json:"actions"`
	Expected    formatObservation      `json:"expected"`
	Differences []formatDifference     `json:"differences"`
}
type formatCorpus struct {
	Version int          `json:"schemaVersion"`
	ID      string       `json:"contractId"`
	Cases   []formatCase `json:"cases"`
}
type formatValidation struct {
	Accepted *bool `json:"accepted"`
}
type formatEmission struct {
	Start    string            `json:"start"`
	Zone     string            `json:"timeZone"`
	Duration int               `json:"durationSeconds"`
	Cadence  string            `json:"cadence"`
	Interval float64           `json:"interval"`
	Until    string            `json:"until"`
	Work     bool              `json:"workHours"`
	Rule     map[string]string `json:"rule"`
}
type formatProfile struct {
	Recognized bool     `json:"recognized"`
	Cadence    *string  `json:"cadence"`
	Interval   *float64 `json:"interval"`
	Until      *string  `json:"until"`
	Work       *bool    `json:"workHours"`
}
type formatCapabilities struct {
	Edit     bool `json:"edit"`
	Title    bool `json:"titleOnly"`
	Instance bool `json:"instance"`
	All      bool `json:"allDelete"`
}
type formatTiming struct {
	Knowledge string  `json:"knowledge"`
	Next      *string `json:"next"`
	Last      *string `json:"last"`
	AllDay    bool    `json:"allDay"`
}
type formatAction struct {
	Accepted  bool    `json:"accepted"`
	Writes    int     `json:"writes"`
	Effect    string  `json:"effect"`
	Target    string  `json:"target"`
	Witness   *string `json:"witness"`
	Preserved bool    `json:"preserved"`
}
type formatObservation struct {
	Validation   formatValidation        `json:"validation"`
	Emission     *formatEmission         `json:"emission,omitempty"`
	Profile      *formatProfile          `json:"profile,omitempty"`
	Capabilities *formatCapabilities     `json:"capabilities,omitempty"`
	Timing       *formatTiming           `json:"timing,omitempty"`
	Actions      map[string]formatAction `json:"actions"`
}

// The schedule observation includes an explicit null emission on invalid input.
func (o formatObservation) MarshalJSON() ([]byte, error) {
	type plain formatObservation
	b, err := json.Marshal(plain(o))
	if err != nil {
		return nil, err
	}
	if o.Profile == nil && o.Emission == nil {
		var m map[string]any
		_ = json.Unmarshal(b, &m)
		m["emission"] = nil
		return json.Marshal(m)
	}
	return b, nil
}

type formatCaseReport struct {
	ID          string            `json:"id"`
	Passed      bool              `json:"passed"`
	Observation formatObservation `json:"observation"`
}
type formatReport struct {
	Version   int                `json:"schemaVersion"`
	ID        string             `json:"contractId"`
	SHA       string             `json:"corpusSha256"`
	Platform  string             `json:"platform"`
	Authority string             `json:"timingAuthority"`
	Generated string             `json:"generatedAt"`
	Passed    bool               `json:"passed"`
	Consumed  int                `json:"consumedCaseCount"`
	Cases     []formatCaseReport `json:"cases"`
}

func formatStrict(data []byte, into any) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(into); err != nil {
		return err
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return fmt.Errorf("trailing JSON: %v", err)
	}
	return nil
}
func formatLeaves(v any) map[string]any {
	b, _ := json.Marshal(v)
	var value any
	_ = json.Unmarshal(b, &value)
	out := map[string]any{}
	var walk func(any, string)
	walk = func(v any, p string) {
		if m, ok := v.(map[string]any); ok && len(m) > 0 {
			for k, x := range m {
				walk(x, p+"/"+strings.ReplaceAll(strings.ReplaceAll(k, "~", "~0"), "/", "~1"))
			}
		} else {
			out[p] = v
		}
	}
	walk(value, "")
	return out
}
func formatCheck(c formatCase, actual formatObservation) error {
	expected := formatLeaves(c.Expected)
	observed := formatLeaves(actual)
	seen := map[string]bool{}
	for _, d := range c.Differences {
		v, ok := expected[d.Path]
		if !ok || seen[d.Path] || reflect.DeepEqual(d.Web, d.Android) || !reflect.DeepEqual(v, d.Web) || strings.TrimSpace(d.Reason) == "" || len(d.Source) < 2 {
			return fmt.Errorf("invalid/stale difference %s", d.Path)
		}
		for _, s := range d.Source {
			if !strings.Contains(s, ":") {
				return fmt.Errorf("missing source %s", d.Path)
			}
		}
		seen[d.Path] = true
	}
	if len(expected) != len(observed) {
		return fmt.Errorf("leaf coverage: expected %d observed %d", len(expected), len(observed))
	}
	keys := make([]string, 0, len(expected))
	for k := range expected {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v, ok := observed[k]
		if !ok || !reflect.DeepEqual(expected[k], v) {
			return fmt.Errorf("%s: got %v want %v", k, v, expected[k])
		}
	}
	return nil
}
func formatLoad(data []byte) (formatCorpus, error) {
	var c formatCorpus
	if err := formatStrict(data, &c); err != nil {
		return c, err
	}
	if c.Version != 1 || c.ID != "hassles-format-v1" {
		return c, fmt.Errorf("unsupported contract")
	}
	// Typed zero values must not make a missing/null observation pass as false.
	var original, roundTrip any
	if err := json.Unmarshal(data, &original); err != nil {
		return c, err
	}
	encoded, err := json.Marshal(c)
	if err != nil {
		return c, err
	}
	if err := json.Unmarshal(encoded, &roundTrip); err != nil {
		return c, err
	}
	if !reflect.DeepEqual(original, roundTrip) {
		return c, fmt.Errorf("missing/null/defaulted contract field")
	}
	seen := map[string]bool{}
	for _, f := range c.Cases {
		if f.ID == "" || seen[f.ID] {
			return c, fmt.Errorf("duplicate/empty ID %s", f.ID)
		}
		seen[f.ID] = true
		if _, err := time.Parse(time.RFC3339, f.Now); err != nil {
			return c, err
		}
		if f.Kind == "schedule" {
			if f.Schedule == nil || len(f.Events) != 0 || !reflect.DeepEqual(f.Actions, []string{"create"}) {
				return c, fmt.Errorf("invalid schedule case %s", f.ID)
			}
		} else if f.Kind == "import" {
			if f.Schedule != nil || len(f.Events) == 0 || !reflect.DeepEqual(f.Actions, []string{"edit", "titleOnly", "instance", "all"}) {
				return c, fmt.Errorf("invalid import case %s", f.ID)
			}
		} else {
			return c, fmt.Errorf("invalid kind")
		}
		keys := map[string]bool{}
		for _, e := range f.Events {
			if e.Key == "" || keys[e.Key] || e.UID == "" || (e.ETag != "strong" && e.ETag != "weak") {
				return c, fmt.Errorf("invalid event")
			}
			keys[e.Key] = true
			for _, d := range append(append([]formatDate{e.Start}, e.RDate...), e.EXDate...) {
				if d.Type != "DATE" && d.Type != "UTC" && d.Type != "LOCAL" {
					return c, fmt.Errorf("invalid typed date")
				}
				if (d.Type == "LOCAL") != (d.TZID != "") {
					return c, fmt.Errorf("invalid TZID")
				}
			}
		}
		for _, e := range f.Events {
			if e.Detached != "" && !keys[e.Detached] {
				return c, fmt.Errorf("missing detached master")
			}
		}
		for _, i := range f.Instances {
			if !keys[i.Key] {
				return c, fmt.Errorf("missing instance event")
			}
			for _, v := range []string{i.Begin, i.End} {
				if _, err := time.Parse(time.RFC3339, v); err != nil {
					return c, err
				}
			}
		}
		if err := formatCheck(f, f.Expected); err != nil {
			return c, fmt.Errorf("%s: %w", f.ID, err)
		}
	}
	return c, nil
}
func formatProp(name string, d formatDate) ical.Prop {
	p := ical.Prop{Name: name, Value: d.Raw, Params: make(ical.Params)}
	switch d.Type {
	case "DATE":
		p.Params.Set("VALUE", "DATE")
	case "LOCAL":
		p.Params.Set("TZID", d.TZID)
	}
	return p
}
func formatObjects(c formatCase) []caldav.CalendarObject {
	out := []caldav.CalendarObject{}
	index := map[string]int{}
	for _, input := range c.Events {
		e := ical.NewEvent()
		e.Props.SetText("UID", input.UID)
		e.Props.SetText("SUMMARY", input.Title)
		e.Props.SetText("DESCRIPTION", input.Description)
		e.Props.SetText("X-FORMAT-OPAQUE", "event-"+input.Key)
		clock, _ := time.Parse(time.RFC3339, c.Now)
		e.Props.SetDateTime("DTSTAMP", clock)
		e.Props["DTSTART"] = []ical.Prop{formatProp("DTSTART", input.Start)}
		if input.End != nil {
			e.Props["DTEND"] = []ical.Prop{formatProp("DTEND", *input.End)}
		} else if input.Duration != nil {
			p := ical.NewProp("DURATION")
			p.Value = "PT" + strconv.Itoa(*input.Duration) + "S"
			e.Props.Set(p)
		}
		if input.Rule != "" {
			p := ical.NewProp("RRULE")
			p.Value = input.Rule
			e.Props.Set(p)
		}
		for _, d := range input.RDate {
			e.Props["RDATE"] = append(e.Props["RDATE"], formatProp("RDATE", d))
		}
		for _, d := range input.EXDate {
			e.Props["EXDATE"] = append(e.Props["EXDATE"], formatProp("EXDATE", d))
		}
		for _, a := range input.Attendees {
			p := ical.NewProp("ATTENDEE")
			p.Value = "mailto:" + a
			e.Props.Add(p)
		}
		if input.Organizer != "" {
			e.Props.SetText("ORGANIZER", "mailto:"+input.Organizer)
		}
		alarm := ical.NewComponent("VALARM")
		alarm.Props.SetText("ACTION", "DISPLAY")
		p := ical.NewProp("TRIGGER")
		p.Value = "PT0S"
		if input.Alarm != 0 {
			p.Value = "-PT" + strconv.Itoa(input.Alarm) + "M"
		}
		alarm.Props.Set(p)
		e.Children = append(e.Children, alarm)
		if input.Detached != "" {
			e.Props["RECURRENCE-ID"] = []ical.Prop{formatProp("RECURRENCE-ID", input.Start)}
			out[index[input.Detached]].Data.Children = append(out[index[input.Detached]].Data.Children, e.Component)
			continue
		}
		cal := ical.NewCalendar()
		cal.Props.SetText("VERSION", "2.0")
		cal.Props.SetText("PRODID", "-//Synthetic format fixture//EN")
		cal.Props.SetText("X-FORMAT-OPAQUE", "root")
		cal.Children = append(cal.Children, e.Component)
		etag := `"fixture-` + input.Key + `"`
		if input.ETag == "weak" {
			etag = "W/" + etag
		}
		index[input.Key] = len(out)
		out = append(out, caldav.CalendarObject{Path: "/format/" + input.Key + ".ics", ETag: etag, Data: cal})
	}
	return out
}

type formatWrite struct{ Path, ETag, Body string }
type formatBackend struct {
	fakeBackend
	captured []formatWrite
}

func (b *formatBackend) Write(ctx context.Context, collection, path, etag, body string) error {
	b.captured = append(b.captured, formatWrite{path, etag, body})
	return b.fakeBackend.Write(ctx, collection, path, etag, body)
}
func formatFingerprint(v any) string { b, _ := json.Marshal(v); return string(b) }
func formatInstant(v *time.Time) *string {
	if v == nil {
		return nil
	}
	s := v.UTC().Format(time.RFC3339)
	return &s
}
func formatTask(t *testing.T, c formatCase, objects []caldav.CalendarObject) Task {
	t.Helper()
	clock, _ := time.Parse(time.RFC3339, c.Now)
	tasks, err := Inventory(objects, clock)
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range tasks {
		for _, member := range task.members {
			if member.Path == "/format/"+c.Events[0].Key+".ics" {
				return task
			}
		}
	}
	t.Fatal("missing first task")
	return Task{}
}
func formatProfileOf(s *Schedule) *formatProfile {
	p := &formatProfile{}
	if s != nil {
		p.Recognized = true
		p.Cadence = &s.Cadence
		p.Interval = &s.Interval
		p.Until = &s.Until
		p.Work = &s.WorkHours
	}
	return p
}
func formatRule(p *ical.Prop) map[string]string {
	out := map[string]string{}
	if p != nil {
		for _, part := range strings.Split(p.Value, ";") {
			kv := strings.SplitN(part, "=", 2)
			if len(kv) == 2 {
				out[kv[0]] = kv[1]
			}
		}
	}
	return out
}
func formatEmissionOf(t *testing.T, cal *ical.Calendar) *formatEmission {
	t.Helper()
	e := cal.Events()[0]
	s, why := readSchedule(e)
	if s == nil {
		start, startErr := e.DateTimeStart(nil)
		end, endErr := e.DateTimeEnd(nil)
		t.Logf("actual emitted event not recognized: %s; start=%s (%v), end=%s (%v), elapsed=%s", why, start.Format(time.RFC3339), startErr, end.Format(time.RFC3339), endErr, end.Sub(start))
		return nil
	}
	start, err := e.DateTimeStart(nil)
	if err != nil {
		t.Fatal(err)
	}
	end, err := e.DateTimeEnd(nil)
	if err != nil {
		t.Fatal(err)
	}
	return &formatEmission{Start: start.UTC().Format(time.RFC3339), Zone: e.Props.Get("DTSTART").Params.Get("TZID"), Duration: int(end.Sub(start).Seconds()), Cadence: s.Cadence, Interval: s.Interval, Until: s.Until, Work: s.WorkHours, Rule: formatRule(e.Props.Get("RRULE"))}
}
func formatCloneObjects(objects []caldav.CalendarObject) []caldav.CalendarObject {
	out := append([]caldav.CalendarObject(nil), objects...)
	for i := range out {
		out[i].Data = cloneCalendar(out[i].Data)
	}
	return out
}
func formatProjection(cal *ical.Calendar, remove ...string) string {
	cal = cloneCalendar(cal)
	for _, e := range cal.Events() {
		for _, key := range remove {
			e.Props.Del(key)
		}
	}
	return formatFingerprint(cal)
}
func formatSavedInvariant(before, after *ical.Calendar, action string, witness *string) bool {
	if len(before.Events()) != 1 || len(after.Events()) != 1 {
		return false
	}
	old := before.Events()[0]
	saved := after.Events()[0]
	switch action {
	case "titleOnly":
		return formatProjection(before, "SUMMARY", "DTSTAMP", "LAST-MODIFIED") == formatProjection(after, "SUMMARY", "DTSTAMP", "LAST-MODIFIED")
	case "edit":
		a := old.Props.Get("DESCRIPTION").Value
		for _, marker := range []string{"[chainmail:work-hours:v1]", "[chainmail:hourly:v1 hours=0.5]", "[chainmail:hourly:v1 hours=6]"} {
			a = strings.Replace(a, marker, "", 1)
		}
		b := saved.Props.Get("DESCRIPTION").Value
		return a == b && formatProjection(before, "DESCRIPTION", "SUMMARY", "DTSTART", "DTEND", "DURATION", "RRULE", "DTSTAMP", "LAST-MODIFIED") == formatProjection(after, "DESCRIPTION", "SUMMARY", "DTSTART", "DTEND", "DURATION", "RRULE", "DTSTAMP", "LAST-MODIFIED")
	case "instance":
		if witness == nil {
			return false
		}
		at, err := time.Parse(time.RFC3339, *witness)
		if err != nil {
			return false
		}
		oldDates := old.Props["EXDATE"]
		dates := saved.Props["EXDATE"]
		if len(dates) != len(oldDates)+1 || (len(oldDates) > 0 && !reflect.DeepEqual(oldDates, dates[:len(oldDates)])) {
			return false
		}
		last := dates[len(dates)-1]
		if last.Value != at.UTC().Format("20060102T150405Z") || len(last.Params) != 0 {
			return false
		}
		a, _ := old.Props.Text("DESCRIPTION")
		b, _ := saved.Props.Text("DESCRIPTION")
		wantDescription := a
		if !strings.Contains(a, completionMarker) {
			wantDescription += "\n" + completionMarker
		}
		if b != wantDescription {
			return false
		}
		return formatProjection(before, "DESCRIPTION", "EXDATE", "DTSTAMP", "LAST-MODIFIED") == formatProjection(after, "DESCRIPTION", "EXDATE", "DTSTAMP", "LAST-MODIFIED")
	}
	return false
}
func formatRunAction(t *testing.T, c formatCase, name string) formatAction {
	t.Helper()
	clock, _ := time.Parse(time.RFC3339, c.Now)
	objects := formatObjects(c)
	before := formatCloneObjects(objects)
	fingerprint := formatFingerprint(objects)
	backend := &formatBackend{fakeBackend: fakeBackend{objects: objects}}
	in := Mutation{Location: Location{AccountID: "owner@fixture.invalid", CalendarID: "/format/"}}
	var witness *string
	if name == "create" {
		in.Action = "create"
		in.ID = formatUUID
		in.Schedule = *c.Schedule
	} else {
		task := formatTask(t, c, objects)
		in.ID = task.ID
		in.Revision = task.Revision
		switch name {
		case "edit", "titleOnly":
			in.Action = "edit"
			in.Schedule = Schedule{Title: "Edited synthetic task", Start: "2026-01-12T15:17", TimeZone: Zone, Cadence: "daily", Interval: 2}
			if name == "titleOnly" && task.Schedule != nil {
				in.Schedule = *task.Schedule
				in.Schedule.Title = "Edited synthetic task"
			}
		case "all":
			in.Action = "delete"
		case "instance":
			in.Action = "done-instance"
			at, token := "", ""
			dry := false
			if task.Instance != nil {
				at, token = task.Instance.At, task.Instance.Token
				witness = &at
			}
			in.InstanceAt = &at
			in.InstanceToken = &token
			in.DryRun = &dry
		}
	}
	_, err := Mutate(context.Background(), backend, in, clock)
	result := formatAction{Accepted: err == nil, Writes: len(backend.captured), Effect: "none", Preserved: true}
	if err != nil {
		t.Logf("action %s denied: %v", name, err)
		result.Preserved = len(backend.captured) == 0 && fingerprint == formatFingerprint(objects)
		return result
	}
	targets := []string{}
	result.Effect = "update"
	if name == "create" {
		result.Effect = "create"
	}
	if name == "instance" {
		result.Witness = witness
	}
	for _, write := range backend.captured {
		if name == "create" {
			targets = append(targets, "new")
			if write.Path != "/format/hassle-"+formatUUID+".ics" || write.ETag != "*" {
				result.Preserved = false
			}
		} else {
			targets = append(targets, strings.TrimSuffix(strings.TrimPrefix(write.Path, "/format/"), ".ics"))
			found := false
			for _, original := range before {
				if original.Path == write.Path {
					found = true
					if original.ETag != write.ETag {
						result.Preserved = false
					}
					if write.Body != "" {
						saved, decodeErr := ical.NewDecoder(strings.NewReader(write.Body)).Decode()
						if decodeErr != nil || !formatSavedInvariant(original.Data, saved, name, witness) {
							result.Preserved = false
						}
					}
				}
			}
			if !found {
				result.Preserved = false
			}
		}
		if write.Body == "" {
			result.Effect = "delete"
		} else {
			saved, decodeErr := ical.NewDecoder(strings.NewReader(write.Body)).Decode()
			if decodeErr != nil {
				t.Fatal(decodeErr)
			}
			if name == "instance" {
				result.Effect = "exclude"
			}
			if name == "create" {
				emitted := formatEmissionOf(t, saved)
				title, _ := saved.Events()[0].Props.Text("SUMMARY")
				alarms := saved.Events()[0].Children
				if emitted == nil || emitted.Duration != 300 || title != strings.TrimSpace(c.Schedule.Title) || len(alarms) != 1 || alarms[0].Props.Get("TRIGGER").Value != "PT0S" {
					result.Preserved = false
				}
			}
			if name == "edit" || name == "titleOnly" {
				title, _ := saved.Events()[0].Props.Text("SUMMARY")
				recognized, _ := readSchedule(saved.Events()[0])
				if title != "Edited synthetic task" || recognized == nil || *recognized != in.Schedule {
					result.Preserved = false
				}
			}
		}
	}
	if len(backend.captured) == 0 {
		result.Preserved = false
	}
	sort.Strings(targets)
	result.Target = strings.Join(targets, ",")
	return result
}
func formatObserve(t *testing.T, c formatCase) formatObservation {
	t.Helper()
	o := formatObservation{Actions: map[string]formatAction{}}
	if c.Kind == "schedule" {
		_, _, err := c.Schedule.Validate()
		ok := err == nil
		o.Validation.Accepted = &ok
		clock, _ := time.Parse(time.RFC3339, c.Now)
		cal, compileErr := NewCalendar(formatUUID, *c.Schedule, clock)
		if (compileErr == nil) != ok {
			t.Fatal("Validate/NewCalendar disagree")
		}
		if ok {
			o.Emission = formatEmissionOf(t, cal)
		}
	} else {
		task := formatTask(t, c, formatObjects(c))
		o.Profile = formatProfileOf(task.Schedule)
		o.Capabilities = &formatCapabilities{Edit: task.Writable && task.Schedule != nil, Title: task.Writable && task.Schedule != nil, Instance: task.Instance != nil, All: task.Writable}
		o.Timing = &formatTiming{Knowledge: task.Timing.State, Next: formatInstant(task.Timing.Next), Last: formatInstant(task.Timing.Last), AllDay: task.Timing.NextAllDay || task.Timing.LastAllDay}
	}
	for _, name := range c.Actions {
		o.Actions[name] = formatRunAction(t, c, name)
	}
	return o
}
func TestFormatConformance(t *testing.T) {
	data, err := os.ReadFile("testdata/format-contract-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != formatCorpusSHA {
		t.Fatal("corpus SHA pin mismatch")
	}
	corpus, err := formatLoad(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(corpus.Cases) != 185 {
		t.Fatal("case coverage changed")
	}
	report := formatReport{Version: corpus.Version, ID: corpus.ID, SHA: formatCorpusSHA, Platform: "web", Authority: "web-evaluation", Generated: time.Now().UTC().Format(time.RFC3339Nano), Passed: true, Cases: []formatCaseReport{}}
	for _, c := range corpus.Cases {
		t.Run(c.ID, func(t *testing.T) {
			actual := formatObserve(t, c)
			err := formatCheck(c, actual)
			report.Cases = append(report.Cases, formatCaseReport{ID: c.ID, Passed: err == nil, Observation: actual})
			report.Consumed++
			if err != nil {
				report.Passed = false
				t.Error(err)
			}
		})
	}
	if report.Consumed != len(corpus.Cases) {
		report.Passed = false
		t.Error("unconsumed cases")
	}
	if path := os.Getenv("HASSLES_CONFORMANCE_REPORT"); path != "" {
		b, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, append(b, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
	}
}
func TestFormatConformanceContractNegatives(t *testing.T) {
	data, err := os.ReadFile("testdata/format-contract-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, mutation := range []func(map[string]any){func(c map[string]any) { c["unknown"] = true }, func(c map[string]any) { c["schemaVersion"] = 2 },
		func(c map[string]any) {
			delete(c["cases"].([]any)[0].(map[string]any)["expected"].(map[string]any)["actions"].(map[string]any)["create"].(map[string]any), "accepted")
		},
		func(c map[string]any) {
			c["cases"].([]any)[0].(map[string]any)["expected"].(map[string]any)["actions"].(map[string]any)["create"].(map[string]any)["accepted"] = nil
		},
		func(c map[string]any) {
			delete(c["cases"].([]any)[0].(map[string]any)["schedule"].(map[string]any), "workHours")
		}, func(c map[string]any) { cases := c["cases"].([]any); cases[1] = cases[0] }} {
		var value map[string]any
		_ = json.Unmarshal(data, &value)
		mutation(value)
		changed, _ := json.Marshal(value)
		if _, err := formatLoad(changed); err == nil {
			t.Fatal("accepted invalid contract")
		}
	}
	corpus, err := formatLoad(data)
	if err != nil {
		t.Fatal(err)
	}
	c := corpus.Cases[0]
	c.Expected.Actions["create"] = formatAction{}
	if err := formatCheck(c, formatObservation{}); err == nil {
		t.Fatal("accepted missing observation")
	}
	c = corpus.Cases[0]
	c.Differences = []formatDifference{{Path: "/missing", Web: true, Android: false, Reason: "test", Source: []string{"web:f", "android:f"}}}
	if err := formatCheck(c, c.Expected); err == nil {
		t.Fatal("accepted unused difference")
	}
	c = corpus.Cases[0]
	c.Differences = []formatDifference{{Path: "/validation/accepted", Web: true, Android: true, Reason: "test", Source: []string{"web:f", "android:f"}}}
	if err := formatCheck(c, c.Expected); err == nil {
		t.Fatal("accepted converged difference")
	}
}

func TestFormatConformanceInstanceMarkerInvariant(t *testing.T) {
	data, err := os.ReadFile("testdata/format-contract-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	corpus, err := formatLoad(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range corpus.Cases {
		if c.ID != "interval-omitted" && c.ID != "completed-exdate-utc" {
			continue
		}
		t.Run(c.ID, func(t *testing.T) {
			objects := formatObjects(c)
			before := cloneCalendar(objects[0].Data)
			task := formatTask(t, c, objects)
			if task.Instance == nil {
				t.Fatal("fixture needs an original occurrence witness")
			}
			at, token := task.Instance.At, task.Instance.Token
			dry := false
			clock, _ := time.Parse(time.RFC3339, c.Now)
			backend := &formatBackend{fakeBackend: fakeBackend{objects: objects}}
			_, err := Mutate(context.Background(), backend, Mutation{
				Location: Location{AccountID: "owner@fixture.invalid", CalendarID: "/format/"},
				ID:       task.ID, Revision: task.Revision, Action: "done-instance",
				InstanceAt: &at, InstanceToken: &token, DryRun: &dry,
			}, clock)
			if err != nil || len(backend.captured) != 1 {
				t.Fatalf("completion failed: %v, writes=%d", err, len(backend.captured))
			}
			saved, err := ical.NewDecoder(strings.NewReader(backend.captured[0].Body)).Decode()
			if err != nil || !formatSavedInvariant(before, saved, "instance", &at) {
				t.Fatalf("valid completion must preserve invariants: %v", err)
			}
			original, _ := before.Events()[0].Props.Text("DESCRIPTION")
			if strings.Contains(original, completionMarker) {
				saved.Events()[0].Props.SetText("DESCRIPTION", original+"\n"+completionMarker)
			} else {
				saved.Events()[0].Props.SetText("DESCRIPTION", original)
			}
			if formatSavedInvariant(before, saved, "instance", &at) {
				t.Fatal("accepted missing or duplicated completion provenance marker")
			}
		})
	}
}
