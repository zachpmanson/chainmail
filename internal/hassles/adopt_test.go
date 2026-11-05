package hassles

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/emersion/go-ical"
	"github.com/emersion/go-webdav/caldav"
)

const adoptionID = "744aa8f9-491b-4473-8dbb-554c957a075c"

func ptr[T any](v T) *T { return &v }

func legacyObject(t *testing.T) caldav.CalendarObject {
	t.Helper()
	body := `BEGIN:VCALENDAR
VERSION:2.0
PRODID:-//Offline fixture//EN
X-ROOT;X-PARAM="a,b":root
BEGIN:VTIMEZONE
TZID:Custom/Preserved
X-ZONE:keep
BEGIN:STANDARD
DTSTART:19700405T030000
TZOFFSETFROM:+1100
TZOFFSETTO:+1000
RRULE:FREQ=YEARLY;BYMONTH=4;BYDAY=1SU
END:STANDARD
BEGIN:DAYLIGHT
DTSTART:19701004T020000
TZOFFSETFROM:+1000
TZOFFSETTO:+1100
RRULE:FREQ=YEARLY;BYMONTH=10;BYDAY=1SU
END:DAYLIGHT
END:VTIMEZONE
BEGIN:VEVENT
UID:legacy-uid
DTSTAMP:20200101T000000Z
LAST-MODIFIED:20200102T000000Z
SEQUENCE:7
DTSTART;TZID=Custom/Preserved:20261105T083012
DURATION:PT17M
RRULE:FREQ=MONTHLY;BYMONTHDAY=5;COUNT=30
EXDATE;TZID=Custom/Preserved:20261205T083012
SUMMARY:Same human title
DESCRIPTION;LANGUAGE=en-AU;VALUE=TEXT;X-NOTES="a,b":[docket]\NHuman\, notes\; path\\end\nMore notes
X-UNKNOWN;X-PARAM="a,b":unknown
BEGIN:VALARM
ACTION:DISPLAY
DESCRIPTION:Alarm [hassle:leave this alone]
TRIGGER:-PT10M
X-ALARM:keep
END:VALARM
END:VEVENT
BEGIN:VEVENT
UID:legacy-uid
DTSTAMP:20200101T000000Z
RECURRENCE-ID;TZID=Custom/Preserved:20270105T083012
DTSTART;TZID=Custom/Preserved:20270106T091500
STATUS:CANCELLED
X-EXCEPTION:preserve missing description
END:VEVENT
BEGIN:VEVENT
UID:legacy-uid
DTSTAMP:20200101T000000Z
RECURRENCE-ID;TZID=Custom/Preserved:20270205T083012
DTSTART;TZID=Custom/Preserved:20270206T091500
DESCRIPTION:
X-EXCEPTION:preserve empty description
END:VEVENT
END:VCALENDAR
`
	c, err := ical.NewDecoder(strings.NewReader(body)).Decode()
	if err != nil {
		t.Fatal(err)
	}
	return caldav.CalendarObject{Path: testCollection + "original-import-path.ics", ETag: `"original"`, Data: c}
}

type adoptionWrite struct{ collection, path, etag, body string }
type adoptionBackend struct {
	objects []caldav.CalendarObject
	writes  []adoptionWrite
	fail    bool
}

func (f *adoptionBackend) Calendars(context.Context) ([]Calendar, error) { return nil, nil }
func (f *adoptionBackend) Objects(context.Context, string) ([]caldav.CalendarObject, error) {
	return f.objects, nil
}
func (f *adoptionBackend) Write(_ context.Context, collection, path, etag, body string) error {
	f.writes = append(f.writes, adoptionWrite{collection, path, etag, body})
	if f.fail {
		return ErrConflict
	}
	for i, o := range f.objects {
		if o.Path == path && o.ETag == etag {
			c, err := ical.NewDecoder(strings.NewReader(body)).Decode()
			if err != nil {
				return err
			}
			f.objects[i].Data = c
			f.objects[i].ETag = `"adopted"`
			return nil
		}
	}
	return ErrConflict
}
func adoptionInput(t *testing.T, o caldav.CalendarObject) Mutation {
	t.Helper()
	task := inventory(t, o)[0]
	return Mutation{Location: Location{AccountID: "offline", CalendarID: testCollection}, Action: "adopt", ID: "resource:" + digest(o.Path), Revision: task.Revision, AdoptID: ptr(adoptionID), DryRun: ptr(true)}
}
func snapshot(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
func parseAdoption(t *testing.T, body string) *ical.Calendar {
	t.Helper()
	c, err := ical.NewDecoder(strings.NewReader(body)).Decode()
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestAdoptionPreviewPreservesImportedContentAndInput(t *testing.T) {
	o := legacyObject(t)
	original := snapshot(t, o)
	f := &adoptionBackend{objects: []caldav.CalendarObject{o}}
	in := adoptionInput(t, o)
	in.AdoptID = ptr(strings.ToUpper(adoptionID))
	first, err := Mutate(context.Background(), f, in, now)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Mutate(context.Background(), f, in, now)
	if err != nil || !reflect.DeepEqual(first, second) || len(f.writes) != 0 || snapshot(t, o) != original {
		t.Fatalf("preview impure/non-deterministic: %+v %v", second, err)
	}
	a := first.Adoption
	if a == nil || a.TaskID != "task:"+adoptionID || a.ResourceID != in.ID || a.Revision != in.Revision || !validDigest(a.Fingerprint) {
		t.Fatalf("preview identity: %+v", a)
	}
	before, _ := Encode(o.Data)
	if a.Before != before {
		t.Fatal("before export was modified")
	}
	// Independently construct only the allowed raw DESCRIPTION additions, then
	// round-trip both sides to compare every property, parameter and child.
	expected := cloneCalendar(o.Data)
	for _, e := range expected.Events() {
		if p := e.Props.Get("DESCRIPTION"); p != nil {
			p.Value += `\n[hassle:v1 task=` + adoptionID + `]`
		} else {
			e.Props.SetText("DESCRIPTION", "[hassle:v1 task="+adoptionID+"]")
		}
	}
	expectedBody, _ := Encode(expected)
	if !reflect.DeepEqual(parseAdoption(t, a.After), parseAdoption(t, expectedBody)) {
		t.Fatal("unexpected content change")
	}
	parsed := parseAdoption(t, a.After)
	if parsed.Events()[0].Props.Get("DESCRIPTION").Params.Get("VALUE") != "TEXT" {
		t.Fatal("lost explicit description VALUE parameter")
	}
	// Mutating a clone's nested data must not alias original properties/params.
	clone := cloneCalendar(o.Data)
	clone.Events()[0].Props.Get("DESCRIPTION").Params["X-NOTES"][0] = "changed"
	clone.Events()[0].Children[0].Props.SetText("X-ALARM", "changed")
	clone.Children[0].Children[0].Props.SetText("TZOFFSETTO", "changed")
	if snapshot(t, o) != original {
		t.Fatal("deep clone aliases original")
	}
}

func TestAdoptionCommitAndExactLostResponseRetry(t *testing.T) {
	o := legacyObject(t)
	f := &adoptionBackend{objects: []caldav.CalendarObject{o}}
	in := adoptionInput(t, o)
	preview, err := Mutate(context.Background(), f, in, now)
	if err != nil {
		t.Fatal(err)
	}
	in.DryRun = ptr(false)
	in.AdoptHash = ptr(preview.Adoption.Fingerprint)
	committed, err := Mutate(context.Background(), f, in, now)
	if err != nil || len(f.writes) != 1 || committed.Message != "Adopted existing reminder" {
		t.Fatalf("commit: %+v %v", committed, err)
	}
	w := f.writes[0]
	if w.path != o.Path || w.etag != o.ETag || w.collection != testCollection || w.body != preview.Adoption.After {
		t.Fatalf("wrong conditional update: %+v", w)
	}
	// Only provider-maintained VEVENT metadata may drift on exact retry.
	for _, e := range f.objects[0].Data.Events() {
		e.Props.SetDateTime("DTSTAMP", now)
		e.Props.SetDateTime("LAST-MODIFIED", now)
		e.Props.SetText("SEQUENCE", "123")
	}
	for _, dry := range []bool{false, true} {
		in.DryRun = ptr(dry)
		retry, err := Mutate(context.Background(), f, in, now)
		if err != nil || retry.Message != "Already adopted" || len(f.writes) != 1 || retry.Adoption.Fingerprint != *in.AdoptHash || retry.Adoption.Before != retry.Adoption.After {
			t.Fatalf("retry wrote or lost provenance: %+v %v", retry, err)
		}
	}
	in.AdoptHash = nil
	if _, err := Mutate(context.Background(), f, in, now); err == nil || len(f.writes) != 1 {
		t.Fatal("accepted tagged resource without saved fingerprint")
	}
}

func TestAdoptionRefusesUnsafeTargetsWithoutWrites(t *testing.T) {
	cases := map[string]func(*caldav.CalendarObject){
		"unowned":             func(o *caldav.CalendarObject) { o.Data.Events()[0].Props.SetText("DESCRIPTION", "human only") },
		"attendee master":     func(o *caldav.CalendarObject) { o.Data.Events()[0].Props.SetText("ATTENDEE", "mailto:p@example.test") },
		"organizer exception": func(o *caldav.CalendarObject) { o.Data.Events()[1].Props.SetText("ORGANIZER", "mailto:p@example.test") },
		"weak etag":           func(o *caldav.CalendarObject) { o.ETag = `W/"weak"` },
		"missing etag":        func(o *caldav.CalendarObject) { o.ETag = "" },
		"create etag":         func(o *caldav.CalendarObject) { o.ETag = "*" },
		"wholly canceled": func(o *caldav.CalendarObject) {
			for _, e := range o.Data.Events() {
				e.Props.SetText("STATUS", "CANCELLED")
			}
		},
		"method":               func(o *caldav.CalendarObject) { o.Data.Props.SetText("METHOD", "REQUEST") },
		"unexpected component": func(o *caldav.CalendarObject) { o.Data.Children = append(o.Data.Children, ical.NewComponent("VTODO")) },
		"duplicate uid": func(o *caldav.CalendarObject) {
			o.Data.Events()[0].Props["UID"] = append(o.Data.Events()[0].Props["UID"], o.Data.Events()[0].Props["UID"][0])
		},
		"missing uid": func(o *caldav.CalendarObject) { o.Data.Events()[0].Props.Del("UID") },
		"mixed uid":   func(o *caldav.CalendarObject) { o.Data.Events()[1].Props.SetText("UID", "other") },
		"two masters": func(o *caldav.CalendarObject) { o.Data.Events()[1].Props.Del("RECURRENCE-ID") },
		"duplicate exception": func(o *caldav.CalendarObject) {
			o.Data.Events()[2].Props["RECURRENCE-ID"] = o.Data.Events()[1].Props["RECURRENCE-ID"]
		},
		"empty exception": func(o *caldav.CalendarObject) { o.Data.Events()[1].Props.Get("RECURRENCE-ID").Value = "" },
		"duplicate description": func(o *caldav.CalendarObject) {
			e := o.Data.Events()[0]
			e.Props["DESCRIPTION"] = append(e.Props["DESCRIPTION"], e.Props["DESCRIPTION"][0])
		},
		"unreadable description": func(o *caldav.CalendarObject) { o.Data.Events()[0].Props.Get("DESCRIPTION").Value += `\x` },
		"description list":       func(o *caldav.CalendarObject) { o.Data.Events()[0].Props.Get("DESCRIPTION").Value += ",truncated" },
		"nontext description":    func(o *caldav.CalendarObject) { o.Data.Events()[0].Props.Get("DESCRIPTION").Params.Set("VALUE", "URI") },
		"outside location":       func(o *caldav.CalendarObject) { o.Path = "/caldav/v2/other@example.test/events/a.ics" },
	}
	for _, desc := range []string{"[hassle:broken", "[HASSLE:v1 task=" + adoptionID + "]", "[hassle:v2 task=" + adoptionID + "]", "[hassle:v1 task=invalid]", adoptionMarker(testID), adoptionMarker(adoptionID) + " " + adoptionMarker(adoptionID), adoptionMarker(adoptionID) + " [HASSLE:broken]"} {
		cases[desc] = func(o *caldav.CalendarObject) { o.Data.Events()[0].Props.SetText("DESCRIPTION", "[docket]\n"+desc) }
	}
	cases["exception preexisting marker"] = func(o *caldav.CalendarObject) {
		o.Data.Events()[1].Props.SetText("DESCRIPTION", adoptionMarker(adoptionID))
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			o := legacyObject(t)
			in := adoptionInput(t, o)
			change(&o)
			// Use the fresh revision so every case exercises its actual safety gate.
			if tasks := inventory(t, o); len(tasks) > 0 {
				in.Revision = tasks[0].Revision
			}
			f := &adoptionBackend{objects: []caldav.CalendarObject{o}}
			before := snapshot(t, f.objects)
			if _, err := Mutate(context.Background(), f, in, now); err == nil || len(f.writes) != 0 || before != snapshot(t, f.objects) {
				t.Fatalf("unsafe target changed: %v", err)
			}
		})
	}
}

func TestAdoptionRawCollisionScanIncludesCanceledMalformedResources(t *testing.T) {
	for _, kind := range []string{"tag", "uppercase malformed", "canceled", "duplicate description", "duplicate UID", "UID list"} {
		t.Run(kind, func(t *testing.T) {
			o := legacyObject(t)
			other := object(t, testCollection+"other.ics")
			e := other.Data.Events()[0]
			e.Props.SetText("DESCRIPTION", "[docket]\n"+adoptionMarker(adoptionID))
			switch kind {
			case "uppercase malformed":
				e.Props.SetText("DESCRIPTION", "[HASSLE:broken "+strings.ToUpper(adoptionID))
			case "canceled":
				e.Props.SetText("STATUS", "CANCELLED")
			case "duplicate description":
				e.Props["DESCRIPTION"] = append(e.Props["DESCRIPTION"], *ical.NewProp("DESCRIPTION"))
			case "duplicate UID":
				e.Props.SetText("DESCRIPTION", "unrelated")
				e.Props.SetText("UID", "legacy-uid")
			case "UID list":
				e.Props.SetText("DESCRIPTION", "unrelated")
				e.Props.Get("UID").Value = "different,legacy-uid"
			}
			f := &adoptionBackend{objects: []caldav.CalendarObject{o, other}}
			if _, err := Mutate(context.Background(), f, adoptionInput(t, o), now); !errors.Is(err, ErrConflict) || len(f.writes) != 0 {
				t.Fatalf("raw collision accepted: %v", err)
			}
		})
	}
}

func TestAdoptionStalePreviewAndRetryFailClosed(t *testing.T) {
	for _, kind := range []string{"revision", "hash", "changed notes fresh revision", "412", "retry notes", "retry alarm", "retry params", "retry timezone", "retry exception marker", "retry collision", "retry ownership", "retry etag", "retry moved resource"} {
		t.Run(kind, func(t *testing.T) {
			o := legacyObject(t)
			f := &adoptionBackend{objects: []caldav.CalendarObject{o}}
			in := adoptionInput(t, o)
			preview, err := Mutate(context.Background(), f, in, now)
			if err != nil {
				t.Fatal(err)
			}
			in.DryRun, in.AdoptHash = ptr(false), ptr(preview.Adoption.Fingerprint)
			if strings.HasPrefix(kind, "retry") {
				if _, err := Mutate(context.Background(), f, in, now); err != nil {
					t.Fatal(err)
				}
				f.writes = nil
			}
			e := f.objects[0].Data.Events()[0]
			switch kind {
			case "revision":
				f.objects[0].ETag = `"new"`
			case "hash":
				in.AdoptHash = ptr(strings.Repeat("0", 64))
			case "changed notes fresh revision":
				e.Props.Get("DESCRIPTION").Value += "changed"
				in.Revision = inventory(t, f.objects...)[0].Revision
			case "412":
				f.fail = true
			case "retry notes":
				e.Props.Get("DESCRIPTION").Value = "changed\\n" + e.Props.Get("DESCRIPTION").Value
			case "retry alarm":
				e.Children[0].Props.SetText("TRIGGER", "-PT3M")
			case "retry params":
				e.Props.Get("DESCRIPTION").Params.Set("LANGUAGE", "fr")
			case "retry timezone":
				f.objects[0].Data.Children[0].Children[0].Props.SetText("TZOFFSETTO", "+0300")
			case "retry exception marker":
				f.objects[0].Data.Events()[1].Props.Del("DESCRIPTION")
			case "retry collision":
				other := object(t, testCollection+"collision.ics")
				other.Data.Events()[0].Props.SetText("DESCRIPTION", "[docket] "+adoptionMarker(adoptionID))
				f.objects = append(f.objects, other)
			case "retry ownership":
				e.Props.SetText("ATTENDEE", "mailto:person@example.test")
			case "retry etag":
				f.objects[0].ETag = `W/"weak"`
			case "retry moved resource":
				f.objects[0].Path = testCollection + "moved.ics"
			}
			_, err = Mutate(context.Background(), f, in, now)
			wantWrites := 0
			if kind == "412" {
				wantWrites = 1
			}
			if err == nil || len(f.writes) != wantWrites {
				t.Fatalf("unsafe commit/retry: %v, %d writes", err, len(f.writes))
			}
		})
	}
}

func TestAdoptionRequestFields(t *testing.T) {
	base := adoptionInput(t, legacyObject(t))
	for _, change := range []func(*Mutation){
		func(in *Mutation) { in.AdoptID = nil }, func(in *Mutation) { in.AdoptID = ptr("invalid") },
		func(in *Mutation) { in.DryRun = nil }, func(in *Mutation) { in.DryRun = ptr(false) },
		func(in *Mutation) { in.ID = "task:" + adoptionID }, func(in *Mutation) { in.ID = "resource:bad" },
		func(in *Mutation) { in.Revision = "" }, func(in *Mutation) { in.AdoptHash = ptr("bad") },
		func(in *Mutation) { in.Schedule.Title = "not metadata only" },
	} {
		in := base
		change(&in)
		if err := ValidateAdoptionFields(in); err == nil {
			t.Fatalf("invalid request accepted: %+v", in)
		}
	}
	for _, action := range []string{"create", "edit", "done", "delete"} {
		for _, flag := range []Mutation{{AdoptID: ptr(adoptionID)}, {AdoptHash: ptr(strings.Repeat("0", 64))}, {DryRun: ptr(false)}, {DryRun: ptr(true)}} {
			flag.Action = action
			if err := ValidateAdoptionFields(flag); err == nil {
				t.Fatalf("cross-action flag accepted: %+v", flag)
			}
		}
	}
}

func TestNewCalendarDocketCompatibilityIdentity(t *testing.T) {
	id := strings.ToUpper(testID)
	f := &adoptionBackend{}
	loc := Location{AccountID: "offline", CalendarID: testCollection}
	// This backend refuses creates, but records the complete conditional request.
	_, _ = Mutate(context.Background(), f, Mutation{Location: loc, Action: "create", ID: id, Schedule: sample()}, now)
	if len(f.writes) != 1 {
		t.Fatal("missing create")
	}
	w := f.writes[0]
	e := parseAdoption(t, w.body).Events()[0]
	desc, _ := e.Props.Text("DESCRIPTION")
	uid, _ := e.Props.Text("UID")
	if desc != "[docket]\n"+adoptionMarker(testID) || uid != "hassle-"+testID || w.path != testCollection+"hassle-"+testID+".ics" || w.etag != "*" {
		t.Fatalf("docket compatibility regression: %s %s %+v", desc, uid, w)
	}
}

func TestAdoptionIdentityAndSelectionNeverFallBackToTitle(t *testing.T) {
	for _, kind := range []string{"unrelated same title", "missing", "changed path", "duplicate path", "empty account", "empty calendar"} {
		t.Run(kind, func(t *testing.T) {
			o := legacyObject(t)
			in := adoptionInput(t, o)
			f := &adoptionBackend{objects: []caldav.CalendarObject{o}}
			switch kind {
			case "unrelated same title":
				other := object(t, testCollection+"same-title.ics")
				other.Data.Events()[0].Props.SetText("DESCRIPTION", "[docket]")
				other.Data.Events()[0].Props.SetText("SUMMARY", "Same human title")
				f.objects = append(f.objects, other)
			case "missing":
				f.objects = nil
			case "changed path":
				f.objects[0].Path = testCollection + "same-title.ics"
			case "duplicate path":
				f.objects = append(f.objects, o)
			case "empty account":
				in.Location.AccountID = ""
			case "empty calendar":
				in.Location.CalendarID = ""
			}
			result, err := Mutate(context.Background(), f, in, now)
			if len(f.writes) != 0 {
				t.Fatal("preview wrote")
			}
			if kind == "unrelated same title" {
				if err != nil || result.Adoption == nil {
					t.Fatalf("title interfered with identity: %+v %v", result, err)
				}
			} else if err == nil {
				t.Fatalf("identity/selection fallback: %+v", result)
			}
		})
	}
}

func TestAdoptionRefusesIncompleteRawCollisionIndex(t *testing.T) {
	for _, kind := range []string{"missing calendar data", "malformed UID"} {
		t.Run(kind, func(t *testing.T) {
			o := legacyObject(t)
			other := object(t, testCollection+"unreadable.ics")
			if kind == "missing calendar data" {
				other.Data = nil
			} else {
				other.Data.Events()[0].Props.Get("UID").Value = `legacy-uid\x`
			}
			f := &adoptionBackend{objects: []caldav.CalendarObject{o, other}}
			if _, err := Mutate(context.Background(), f, adoptionInput(t, o), now); err == nil || len(f.writes) != 0 {
				t.Fatalf("incomplete raw collision scan accepted: %v", err)
			}
		})
	}
}
