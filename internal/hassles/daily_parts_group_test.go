package hassles

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-ical"
	"github.com/emersion/go-webdav/caldav"
)

var dailyGroupLocation = Location{AccountID: "inert-synthetic", CalendarID: "/caldav/v2/synthetic@example.test/events/"}

func dailyGroupObjects(t *testing.T, p *DailyPartsV2Plan) []caldav.CalendarObject {
	t.Helper()
	out := []caldav.CalendarObject{}
	for i, member := range p.Members() {
		c := ical.NewCalendar()
		c.Props.SetText("VERSION", "2.0")
		c.Props.SetText("PRODID", "-//Synthetic offline M2R//EN")
		e := ical.NewEvent()
		// A valid wire event is needed for the ACTUAL adoption encoder boundary,
		// not merely an in-memory tree rejected before serialization.
		e.Props.SetDateTime("DTSTAMP", time.Date(2027, 2, 12, 0, 0, 0, 0, time.UTC))
		// Physical IDs deliberately differ from logical member IDs and web creates.
		e.Props.SetText("UID", fmt.Sprintf("observed-native-uid-%d", i))
		e.Props.SetText("SUMMARY", "Synthetic title")
		desc, err := p.Description(member.MemberID, false)
		if err != nil {
			t.Fatal(err)
		}
		e.Props.SetText("DESCRIPTION", desc)
		e.Props.SetText("TRANSP", "TRANSPARENT")
		start := ical.NewProp("DTSTART")
		at := time.UnixMilli(member.AnchorEpochMillis)
		loc, _ := time.LoadLocation(Zone)
		start.Value = at.In(loc).Format("20060102T150405")
		start.Params.Set("TZID", Zone)
		e.Props.Set(start)
		rule := ical.NewProp("RRULE")
		rule.Value = member.RRule
		e.Props.Set(rule)
		duration := ical.NewProp("DURATION")
		duration.Value = member.Duration
		e.Props.Set(duration)
		alarm := ical.NewComponent("VALARM")
		alarm.Props.SetText("ACTION", "DISPLAY")
		alarm.Props.SetText("DESCRIPTION", "Reminder")
		trigger := ical.NewProp("TRIGGER")
		trigger.Value = "PT0S"
		alarm.Props.Set(trigger)
		e.Children = append(e.Children, alarm)
		c.Children = append(c.Children, e.Component)
		out = append(out, caldav.CalendarObject{Path: dailyGroupLocation.CalendarID + fmt.Sprintf("actual-path-%d.ics", i), ETag: fmt.Sprintf(`"opaque-%d"`, i), Data: c})
	}
	return out
}

func dailyGroupRead(t *testing.T, objects []caldav.CalendarObject) DailyPartsGroupInventoryModel {
	t.Helper()
	model, err := ReadDailyPartsV2Groups(objects, dailyGroupLocation, now)
	if err != nil {
		t.Fatal(err)
	}
	return model
}

func dailyGroupReadonly(t *testing.T, objects []caldav.CalendarObject) {
	t.Helper()
	model := dailyGroupRead(t, objects)
	if len(model.Groups) == 0 {
		t.Fatal("reserved evidence disappeared")
	}
	for _, group := range model.Groups {
		if group.StructuralReady || len(group.Restrictions) == 0 || group.Timing.State != "unknown" || group.Timing.Next != nil || group.Timing.Last != nil {
			t.Fatalf("unsafe group: %+v", group)
		}
	}
}

func TestDailyPartsGroupsCompleteStructuralNotPermission(t *testing.T) {
	for _, tc := range []struct {
		name     string
		schedule Schedule
		count    int
	}{
		{"singleton", dailyV2Schedule("2027-02-12T09:17", 24, false, ""), 1},
		{"full12", dailyV2Schedule("2027-02-12T09:17", 2, false, ""), 12},
		{"work8-half", dailyV2Schedule("2027-02-12T09:47", 0.5, true, ""), 8},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := dailyV2Compile(t, tc.schedule)
			objects := dailyGroupObjects(t, p)
			before := snapshot(t, objects)
			model := dailyGroupRead(t, objects)
			if len(model.Groups) != 1 || len(model.Physical) != tc.count || len(model.HandledHrefs) != tc.count {
				t.Fatalf("inventory: %+v", model)
			}
			g := model.Groups[0]
			if !g.StructuralReady || len(g.Restrictions) != 0 || g.Plan.Payload() != p.Payload() || g.Location != dailyGroupLocation || g.ContextAuthorization != "unavailable" || g.Title != "Synthetic title" || len(g.Members) != tc.count {
				t.Fatalf("group: %+v", g)
			}
			if g.Timing.State != "unknown" || g.Timing.Note != dailyPartsTimingDeferred || g.Timing.Next != nil || g.Timing.Last != nil {
				t.Fatal("invented timing witness")
			}
			for i, m := range g.Members {
				if m.MemberID != p.Members()[i].MemberID || m.UID == m.MemberID || m.Raw == "" || !validDigest(m.Fingerprint) {
					t.Fatal("physical/logical capture")
				}
			}
			if before != snapshot(t, objects) || !validDigest(g.Revision) {
				t.Fatal("reader changed source or lacks revision")
			}
		})
	}
	for _, title := range []string{"  Exact whitespace [docket]  ", strings.Repeat("é", 250)} {
		p := dailyV2Compile(t, dailyV2Schedule("2027-02-12T09:17", 24, false, ""))
		objects := dailyGroupObjects(t, p)
		objects[0].Data.Events()[0].Props.SetText("SUMMARY", title)
		g := dailyGroupRead(t, objects).Groups[0]
		if !g.StructuralReady || g.Title != title {
			t.Fatal("external title normalized or used as ownership")
		}
	}
	// The independent M1 literals also enter the physical reader, without encoding
	// or decoding their raw RRULE or metadata to manufacture expected values.
	for _, fixture := range dailyV2Fixtures(t).Cases {
		p, err := ParseDailyPartsV2Payload(fixture.Expected.Payload, fixture.Expected.PlanHash)
		if err != nil {
			t.Fatal(err)
		}
		if !dailyGroupRead(t, dailyGroupObjects(t, p)).Groups[0].StructuralReady {
			t.Fatal(fixture.Name)
		}
	}
}

func TestDailyPartsGroupsEveryMemberGuards(t *testing.T) {
	p := dailyV2Compile(t, dailyV2Schedule("2027-02-12T09:47", 0.5, true, "2027-03-12T16:47"))
	changes := map[string]func(*caldav.CalendarObject){
		"foreign-path":      func(o *caldav.CalendarObject) { o.Path = "/caldav/v2/foreign@example.test/events/x.ics" },
		"noncanonical-path": func(o *caldav.CalendarObject) { o.Path = Origin + o.Path },
		"etag-missing":      func(o *caldav.CalendarObject) { o.ETag = "" },
		"etag-weak":         func(o *caldav.CalendarObject) { o.ETag = `W/"x"` },
		"etag-wildcard":     func(o *caldav.CalendarObject) { o.ETag = "*" },
		"etag-control":      func(o *caldav.CalendarObject) { o.ETag = "\"x\ny\"" },
		"method":            func(o *caldav.CalendarObject) { o.Data.Props.SetText("METHOD", "REQUEST") },
		"calendar-child":    func(o *caldav.CalendarObject) { o.Data.Children = append(o.Data.Children, ical.NewComponent("VTODO")) },
		"attendee": func(o *caldav.CalendarObject) {
			o.Data.Events()[0].Props.SetText("ATTENDEE", "mailto:any@example.test")
		},
		"self-organizer": func(o *caldav.CalendarObject) {
			o.Data.Events()[0].Props.SetText("ORGANIZER", "mailto:synthetic@example.test")
		},
		"canceled":       func(o *caldav.CalendarObject) { o.Data.Events()[0].Props.SetText("STATUS", "CANCELLED") },
		"unknown-status": func(o *caldav.CalendarObject) { o.Data.Events()[0].Props.SetText("STATUS", "UNKNOWN") },
		"detached":       func(o *caldav.CalendarObject) { o.Data.Events()[0].Props.SetDateTime("RECURRENCE-ID", now) },
		"uid-empty":      func(o *caldav.CalendarObject) { o.Data.Events()[0].Props.SetText("UID", "") },
		"uid-list":       func(o *caldav.CalendarObject) { o.Data.Events()[0].Props.Get("UID").Value = "a,b" },
		"uid-duplicate": func(o *caldav.CalendarObject) {
			e := o.Data.Events()[0]
			e.Props["UID"] = append(e.Props["UID"], e.Props["UID"][0])
		},
		"title-blank":             func(o *caldav.CalendarObject) { o.Data.Events()[0].Props.SetText("SUMMARY", " ") },
		"title-conflict":          func(o *caldav.CalendarObject) { o.Data.Events()[0].Props.SetText("SUMMARY", "Other") },
		"title-long-utf8":         func(o *caldav.CalendarObject) { o.Data.Events()[0].Props.SetText("SUMMARY", strings.Repeat("é", 251)) },
		"title-crlf":              func(o *caldav.CalendarObject) { o.Data.Events()[0].Props.SetText("SUMMARY", "a\nb") },
		"title-invalid-utf8":      func(o *caldav.CalendarObject) { o.Data.Events()[0].Props.Get("SUMMARY").Value = string([]byte{0xff}) },
		"title-hidden-list":       func(o *caldav.CalendarObject) { o.Data.Events()[0].Props.Get("SUMMARY").Value = "a,b" },
		"description-hidden-list": func(o *caldav.CalendarObject) { o.Data.Events()[0].Props.Get("DESCRIPTION").Value += ",hidden" },
		"description-duplicate": func(o *caldav.CalendarObject) {
			e := o.Data.Events()[0]
			e.Props["DESCRIPTION"] = append(e.Props["DESCRIPTION"], e.Props["DESCRIPTION"][0])
		},
		"description-value-type": func(o *caldav.CalendarObject) { o.Data.Events()[0].Props.Get("DESCRIPTION").Params.Set("VALUE", "URI") },
		"description-parameter-list": func(o *caldav.CalendarObject) {
			o.Data.Events()[0].Props.Get("DESCRIPTION").Params["LANGUAGE"] = []string{"en", "fr"}
		},
		"docket-added": func(o *caldav.CalendarObject) {
			e := o.Data.Events()[0]
			d, _ := e.Props.Text("DESCRIPTION")
			e.Props.SetText("DESCRIPTION", d+"\n[docket]")
		},
		"unknown-version": func(o *caldav.CalendarObject) {
			e := o.Data.Events()[0]
			d, _ := e.Props.Text("DESCRIPTION")
			e.Props.SetText("DESCRIPTION", strings.Replace(d, "[hassle:v2", "[hassle:v3", 1))
		},
		"hash-conflict": func(o *caldav.CalendarObject) {
			e := o.Data.Events()[0]
			d, _ := e.Props.Text("DESCRIPTION")
			e.Props.SetText("DESCRIPTION", strings.Replace(d, "plan="+p.PlanHash(), "plan="+strings.Repeat("0", 64), 1))
		},
		"start-utc": func(o *caldav.CalendarObject) {
			e := o.Data.Events()[0]
			s, _ := melbourneStart(e.Props.Get("DTSTART"))
			e.Props.Get("DTSTART").Params.Del("TZID")
			e.Props.Get("DTSTART").SetDateTime(s.UTC())
		},
		"start-floating":    func(o *caldav.CalendarObject) { o.Data.Events()[0].Props.Get("DTSTART").Params.Del("TZID") },
		"start-date":        func(o *caldav.CalendarObject) { o.Data.Events()[0].Props.Get("DTSTART").Params.Set("VALUE", "DATE") },
		"start-extra-param": func(o *caldav.CalendarObject) { o.Data.Events()[0].Props.Get("DTSTART").Params.Set("X-START", "x") },
		"start-seconds": func(o *caldav.CalendarObject) {
			e := o.Data.Events()[0]
			p := e.Props.Get("DTSTART")
			p.Value = p.Value[:len(p.Value)-2] + "01"
		},
		"start-tzid-list": func(o *caldav.CalendarObject) {
			o.Data.Events()[0].Props.Get("DTSTART").Params["TZID"] = []string{Zone, Zone}
		},
		"start-wrong-anchor": func(o *caldav.CalendarObject) {
			e := o.Data.Events()[0]
			s, _ := melbourneStart(e.Props.Get("DTSTART"))
			e.Props.Get("DTSTART").SetDateTime(s.Add(24 * time.Hour))
		},
		"duration-wrong": func(o *caldav.CalendarObject) { o.Data.Events()[0].Props.Get("DURATION").Value = "PT6M" },
		"duration-duplicate": func(o *caldav.CalendarObject) {
			e := o.Data.Events()[0]
			e.Props["DURATION"] = append(e.Props["DURATION"], e.Props["DURATION"][0])
		},
		"duration-and-end": func(o *caldav.CalendarObject) { o.Data.Events()[0].Props.SetDateTime("DTEND", now) },
		"rule-missing-interval": func(o *caldav.CalendarObject) {
			e := o.Data.Events()[0]
			e.Props.Get("RRULE").Value = strings.Replace(e.Props.Get("RRULE").Value, ";INTERVAL=1", "", 1)
		},
		"rule-missing-wkst": func(o *caldav.CalendarObject) {
			e := o.Data.Events()[0]
			e.Props.Get("RRULE").Value = strings.Replace(e.Props.Get("RRULE").Value, ";WKST=MO", "", 1)
		},
		"rule-missing-minute": func(o *caldav.CalendarObject) {
			e := o.Data.Events()[0]
			e.Props.Get("RRULE").Value = strings.Replace(e.Props.Get("RRULE").Value, ";BYMINUTE=17,47", "", 1)
		},
		"rule-missing-until": func(o *caldav.CalendarObject) {
			e := o.Data.Events()[0]
			r := strings.Split(e.Props.Get("RRULE").Value, ";")
			e.Props.Get("RRULE").Value = r[0] + ";" + strings.Join(r[2:], ";")
		},
		"rule-missing-workday": func(o *caldav.CalendarObject) {
			e := o.Data.Events()[0]
			e.Props.Get("RRULE").Value = strings.Replace(e.Props.Get("RRULE").Value, ";BYDAY="+workDays, "", 1)
		},
		"rule-seconds": func(o *caldav.CalendarObject) { o.Data.Events()[0].Props.Get("RRULE").Value += ";BYSECOND=0" },
		"rule-hour":    func(o *caldav.CalendarObject) { o.Data.Events()[0].Props.Get("RRULE").Value += ";BYHOUR=9" },
		"rule-count":   func(o *caldav.CalendarObject) { o.Data.Events()[0].Props.Get("RRULE").Value += ";COUNT=1" },
		"rule-minute-alias": func(o *caldav.CalendarObject) {
			e := o.Data.Events()[0]
			e.Props.Get("RRULE").Value = strings.Replace(e.Props.Get("RRULE").Value, "BYMINUTE=17,47", "BYMINUTE=47,17", 1)
		},
		"rule-unknown-empty": func(o *caldav.CalendarObject) { o.Data.Events()[0].Props.Get("RRULE").Value += ";UNKNOWN=" },
		"rule-duplicate":     func(o *caldav.CalendarObject) { o.Data.Events()[0].Props.Get("RRULE").Value += ";INTERVAL=1" },
		"rule-parameter":     func(o *caldav.CalendarObject) { o.Data.Events()[0].Props.Get("RRULE").Params.Set("X-RULE", "x") },
		"rdate":              func(o *caldav.CalendarObject) { o.Data.Events()[0].Props.SetDateTime("RDATE", now) },
		"exrule":             func(o *caldav.CalendarObject) { o.Data.Events()[0].Props.SetText("EXRULE", "FREQ=DAILY") },
		"alarm-missing":      func(o *caldav.CalendarObject) { o.Data.Events()[0].Children = nil },
		"alarm-extra": func(o *caldav.CalendarObject) {
			e := o.Data.Events()[0]
			e.Children = append(e.Children, cloneComponent(e.Children[0]))
		},
		"alarm-offset": func(o *caldav.CalendarObject) { o.Data.Events()[0].Children[0].Props.Get("TRIGGER").Value = "-PT1M" },
		"alarm-action": func(o *caldav.CalendarObject) { o.Data.Events()[0].Children[0].Props.Get("ACTION").Value = "EMAIL" },
		"alarm-nested": func(o *caldav.CalendarObject) {
			o.Data.Events()[0].Children[0].Children = append(o.Data.Events()[0].Children[0].Children, ical.NewComponent("VALARM"))
		},
		"alarm-parameter": func(o *caldav.CalendarObject) {
			o.Data.Events()[0].Children[0].Props.Get("TRIGGER").Params.Set("RELATED", "END")
		},
		"alarm-participant": func(o *caldav.CalendarObject) {
			o.Data.Events()[0].Children[0].Props.SetText("ATTENDEE", "mailto:a@example.test")
		},
	}
	for i := range p.Members() {
		for name, change := range changes {
			t.Run(fmt.Sprintf("member-%d/%s", i, name), func(t *testing.T) {
				objects := dailyGroupObjects(t, p)
				change(&objects[i])
				before := snapshot(t, objects)
				dailyGroupReadonly(t, objects)
				if before != snapshot(t, objects) {
					t.Fatal("negative read mutated raw source")
				}
			})
		}
		for mask := 0; mask < 7; mask++ {
			t.Run(fmt.Sprintf("member-%d/retained-markers-%d", i, mask), func(t *testing.T) {
				objects := dailyGroupObjects(t, p)
				e := objects[i].Data.Events()[0]
				d, _ := e.Props.Text("DESCRIPTION")
				lines := strings.Split(d, "\n")
				kept := []string{}
				for bit, line := range lines {
					if mask&(1<<bit) != 0 {
						kept = append(kept, line)
					}
				}
				e.Props.SetText("DESCRIPTION", strings.Join(kept, "\n"))
				dailyGroupReadonly(t, objects)
			})
		}
	}
}

func TestDailyPartsGroupsWholeEvidenceAndRoster(t *testing.T) {
	p := dailyV2Compile(t, dailyV2Schedule("2027-02-12T09:17", 2, false, ""))
	for i := range p.Members() {
		objects := dailyGroupObjects(t, p)
		dailyGroupReadonly(t, append(objects[:i], objects[i+1:]...))
	}
	for _, mode := range []string{"duplicate-role", "different-incarnation", "different-plan", "external-canceled-uid", "external-detached-uid", "genuine-v1-collision"} {
		t.Run(mode, func(t *testing.T) {
			objects := dailyGroupObjects(t, p)
			extra := dailyGroupObjects(t, p)[0]
			extra.Path = dailyGroupLocation.CalendarID + "extra.ics"
			extra.Data.Events()[0].Props.SetText("UID", "unique-extra")
			switch mode {
			case "different-incarnation":
				other, err := CompileDailyPartsV2(dailyV2Schedule("2027-02-12T09:17", 2, false, ""), dailyV2Task, adoptionID)
				if err != nil {
					t.Fatal(err)
				}
				extra = dailyGroupObjects(t, other)[0]
				extra.Path = dailyGroupLocation.CalendarID + "extra.ics"
				extra.Data.Events()[0].Props.SetText("UID", "unique-extra")
			case "different-plan":
				other := dailyV2Compile(t, dailyV2Schedule("2027-02-12T09:18", 2, false, ""))
				extra = dailyGroupObjects(t, other)[0]
				extra.Path = dailyGroupLocation.CalendarID + "extra.ics"
				extra.Data.Events()[0].Props.SetText("UID", "unique-extra")
			case "external-canceled-uid", "external-detached-uid":
				e := extra.Data.Events()[0]
				uid, _ := objects[0].Data.Events()[0].Props.Text("UID")
				e.Props.SetText("UID", uid)
				e.Props.SetText("DESCRIPTION", "Unowned external evidence")
				e.Props.SetText("STATUS", "CANCELLED")
				if mode == "external-detached-uid" {
					e.Props.SetDateTime("RECURRENCE-ID", now)
				}
			case "genuine-v1-collision":
				extra.Data.Events()[0].Props.SetText("DESCRIPTION", "[docket]\n[hassle:v1 task="+dailyV2Task+"]")
			}
			objects = append(objects, extra)
			dailyGroupReadonly(t, objects)
			if len(dailyGroupRead(t, objects).Physical) != 13 {
				t.Fatal("physical evidence filtered")
			}
		})
	}
	for _, mode := range []string{"nil-resource", "malformed-external-uid", "duplicate-uid-list"} {
		objects := dailyGroupObjects(t, p)
		extra := object(t, dailyGroupLocation.CalendarID+"uncertain.ics")
		switch mode {
		case "nil-resource":
			extra.Data = nil
		case "malformed-external-uid":
			extra.Data.Events()[0].Props.Get("UID").Value = `unreadable\x`
		case "duplicate-uid-list":
			uid, _ := objects[0].Data.Events()[0].Props.Text("UID")
			extra.Data.Events()[0].Props.Get("UID").Value = "other," + uid
		}
		dailyGroupReadonly(t, append(objects, extra))
	}
	// Cancellation never erases the new reader's reserved evidence.
	objects := dailyGroupObjects(t, p)
	for _, o := range objects {
		o.Data.Events()[0].Props.SetText("STATUS", "CANCELLED")
	}
	dailyGroupReadonly(t, objects)
	// An unrelated genuine legacy-only resource is not dispatched as v2.
	legacy := object(t, dailyGroupLocation.CalendarID+"legacy.ics")
	model := dailyGroupRead(t, []caldav.CalendarObject{legacy})
	if len(model.Groups) != 0 || len(model.HandledHrefs) != 0 || len(model.Physical) != 1 {
		t.Fatal("legacy path intercepted")
	}
}

func TestDailyPartsGroupsRawRevisionAndDefensiveCapture(t *testing.T) {
	p := dailyV2Compile(t, dailyV2Schedule("2027-02-12T09:17", 24, false, ""))
	objects := dailyGroupObjects(t, p)
	e := objects[0].Data.Events()[0]
	e.Props["X-NIL"] = nil
	e.Props["X-EMPTY"] = []ical.Prop{}
	e.Props["X-PARAMS"] = []ical.Prop{{Name: "X-PARAMS", Value: "raw", Params: ical.Params{"NIL": nil, "EMPTY": []string{}}}}
	original := dailyGroupRead(t, objects)
	captured := snapshot(t, original)
	exact := original.Groups[0].Members[0].Original()
	if !reflect.DeepEqual(exact, objects[0]) {
		t.Fatal("full physical copy lost nil/empty fields")
	}
	returned := original.Groups[0].Members[0].Original()
	returned.Data.Events()[0].Props.Get("SUMMARY").SetText("caller changed")
	returned.Data.Events()[0].Props.Get("X-PARAMS").Params["EMPTY"] = []string{"caller changed"}
	returned.Data.Events()[0].Children[0].Props.Get("DESCRIPTION").SetText("caller changed")
	if !reflect.DeepEqual(original.Groups[0].Members[0].Original(), exact) {
		t.Fatal("Original accessor exposes mutable captured tree")
	}
	rule := objects[0].Data.Events()[0].Props.Get("RRULE")
	parts := strings.Split(rule.Value, ";")
	parts[0], parts[1] = parts[1], parts[0]
	rule.Value = strings.Join(parts, ";")
	reordered := dailyGroupRead(t, objects)
	if !reordered.Groups[0].StructuralReady || reordered.Groups[0].Revision == original.Groups[0].Revision || snapshot(t, original) != captured {
		t.Fatal("read comparison became raw authority normalization or alias")
	}
	objects[0].Data.Events()[0].Props.Get("SUMMARY").Params["LANGUAGE"] = []string{"en"}
	objects[0].Data.Events()[0].Children[0].Props.Get("DESCRIPTION").Value = "mutated"
	objects[0].ETag = `"new"`
	objects[0].ModTime = now
	objects[0].ContentLength = 123
	if snapshot(t, original) != captured || !reflect.DeepEqual(original.Groups[0].Members[0].Original(), exact) {
		t.Fatal("raw nested capture aliases backend")
	}
	if dailyGroupRead(t, objects).Groups[0].Revision == reordered.Groups[0].Revision {
		t.Fatal("full raw revision ignored changes")
	}
	invalid := dailyGroupObjects(t, p)
	invalid[0].Data.Events()[0].Props.Get("SUMMARY").Value = string([]byte{0xff})
	invalidModel := dailyGroupRead(t, invalid)
	if invalidModel.Groups[0].StructuralReady || invalidModel.Physical[0].Original().Data.Events()[0].Props.Get("SUMMARY").Value != string([]byte{0xff}) {
		t.Fatal("denied invalid text lost original raw bytes")
	}
	// Location is captured, never treated as authorization; a caller-supplied
	// empty account changes revision but cannot supply owner/ACL evidence.
	loc := dailyGroupLocation
	loc.AccountID = ""
	model, err := ReadDailyPartsV2Groups(dailyGroupObjects(t, p), loc, now)
	if err != nil || !model.Groups[0].StructuralReady || model.Groups[0].ContextAuthorization != "unavailable" || model.Groups[0].Revision == original.Groups[0].Revision {
		t.Fatal("location was inferred/authorized or omitted from revision", err)
	}
}

func TestDailyPartsGroupsDurationAndExclusions(t *testing.T) {
	for _, startText := range []string{"2027-02-12T09:17", "2027-10-03T01:59", "2027-04-04T01:59"} {
		p := dailyV2Compile(t, dailyV2Schedule(startText, 24, false, ""))
		member := p.Members()[0]
		for _, form := range []string{"PT5M", "PT300S", "P300S", "end-utc", "end-local"} {
			objects := dailyGroupObjects(t, p)
			e := objects[0].Data.Events()[0]
			if strings.HasPrefix(form, "end-") {
				e.Props.Del("DURATION")
				end := time.UnixMilli(member.AnchorEpochMillis + 300000).UTC()
				if form == "end-local" {
					loc, _ := time.LoadLocation(Zone)
					end = end.In(loc)
				}
				e.Props.SetDateTime("DTEND", end)
			} else {
				e.Props.Get("DURATION").Value = form
			}
			model := dailyGroupRead(t, objects)
			// Fall 01:59 + 5 elapsed minutes is 02:04 in the fold. UTC end is
			// verifiable; local ambiguous end intentionally remains read-only.
			want := !(startText == "2027-04-04T01:59" && form == "end-local")
			if model.Groups[0].StructuralReady != want {
				t.Fatalf("%s/%s: %+v", startText, form, model.Groups[0])
			}
		}
	}
	p := dailyV2Compile(t, dailyV2Schedule("2027-02-12T09:47", 0.5, true, ""))
	for _, count := range []int{1000, 1001} {
		objects := dailyGroupObjects(t, p)
		e := objects[0].Data.Events()[0]
		member := p.Members()[0]
		d, _ := p.Description(member.MemberID, true)
		e.Props.SetText("DESCRIPTION", d)
		loc, _ := time.LoadLocation(Zone)
		anchor := time.UnixMilli(member.AnchorEpochMillis).In(loc)
		values := make([]string, count)
		for i := range values {
			at := anchor.AddDate(0, 0, 7*i)
			if !p.SupportsCompletion(member.MemberID, at) {
				t.Fatal("bad distinct cap fixture")
			}
			values[i] = at.UTC().Format("20060102T150405Z")
		}
		prop := ical.NewProp("EXDATE")
		prop.Value = strings.Join(values, ",")
		e.Props.Set(prop)
		g := dailyGroupRead(t, objects).Groups[0]
		if g.StructuralReady != (count == 1000) {
			t.Fatalf("distinct exclusion cap %d: %+v", count, g)
		}
		if count == 1000 && (len(g.Members[0].Exclusions) != 1000 || !g.Members[0].HasCompletionMetadata) {
			t.Fatal("typed completion capture missing")
		}
	}
	for i, member := range p.Members() {
		for _, mode := range []string{"utc", "local", "no-marker", "marker-without-date", "duplicate", "wrong-role", "unknown-param", "too-many", "unknown-marker"} {
			t.Run(fmt.Sprintf("%d/%s", i, mode), func(t *testing.T) {
				objects := dailyGroupObjects(t, p)
				e := objects[i].Data.Events()[0]
				d, _ := p.Description(member.MemberID, true)
				e.Props.SetText("DESCRIPTION", d)
				at := time.UnixMilli(member.AnchorEpochMillis).UTC()
				prop := ical.NewProp("EXDATE")
				prop.Value = at.Format("20060102T150405Z")
				e.Props.Set(prop)
				switch mode {
				case "local":
					loc, _ := time.LoadLocation(Zone)
					prop = e.Props.Get("EXDATE")
					prop.Value = at.In(loc).Format("20060102T150405")
					prop.Params.Set("TZID", Zone)
				case "no-marker":
					d, _ = p.Description(member.MemberID, false)
					e.Props.SetText("DESCRIPTION", d)
				case "marker-without-date":
					e.Props.Del("EXDATE")
				case "duplicate":
					e.Props["EXDATE"] = append(e.Props["EXDATE"], e.Props["EXDATE"][0])
				case "wrong-role":
					e.Props.Get("EXDATE").Value = at.Add(time.Hour).Format("20060102T150405Z")
				case "unknown-param":
					e.Props.Get("EXDATE").Params.Set("X-UNKNOWN", "x")
				case "too-many":
					e.Props.Get("EXDATE").Value = strings.Repeat(at.Format("20060102T150405Z")+",", 1000) + at.Format("20060102T150405Z")
				case "unknown-marker":
					e.Props.SetText("DESCRIPTION", strings.Replace(d, completionMarker, "[chainmail:completed-instances:v2]", 1))
				}
				g := dailyGroupRead(t, objects).Groups[0]
				if g.StructuralReady != (mode == "utc" || mode == "local") {
					t.Fatalf("completion admission: %+v", g)
				}
			})
		}
	}
}

func TestDailyPartsGroupsLegacyCanonicalHintsDenyOnly(t *testing.T) {
	const task = "e3ae442f-5498-4cd6-a362-312ba81b9123"
	p, err := CompileDailyPartsV2(dailyV2Schedule("2027-02-12T09:17", 2, false, ""), task, dailyV2Incarnation)
	if err != nil {
		t.Fatal(err)
	}
	if !dailyGroupRead(t, dailyGroupObjects(t, p)).Groups[0].StructuralReady {
		t.Fatal("invalid complete-v2 control")
	}
	for _, legacyID := range []string{task, strings.ToUpper(task), "E3ae442f-5498-4cd6-A362-312ba81b9123"} {
		t.Run(legacyID, func(t *testing.T) {
			objects := dailyGroupObjects(t, p)
			legacy := object(t, dailyGroupLocation.CalendarID+"genuine-case-v1.ics")
			legacy.Data.Events()[0].Props.SetText("UID", "distinct-legacy-case-uid")
			legacy.Data.Events()[0].Props.SetText("DESCRIPTION", "[docket]\n[hassle:v1 task="+legacyID+"]")
			actual := inventory(t, legacy)
			if len(actual) != 1 || actual[0].ID != "task:"+task {
				t.Fatal("not actual canonical legacy identity", actual)
			}
			objects = append(objects, legacy)
			model := dailyGroupRead(t, objects)
			if len(model.Groups) != 1 || model.Groups[0].StructuralReady || len(model.Groups[0].Restrictions) == 0 || len(model.Groups[0].Members) != 13 || len(model.HandledHrefs) != 12 {
				t.Fatal("legacy collision escaped denial", model)
			}
			found := false
			for _, member := range model.Groups[0].Members {
				found = found || member.Href == legacy.Path
			}
			if !found {
				t.Fatal("legacy collision original omitted")
			}
			before := model.Groups[0].Revision
			legacy.Data.Events()[0].Props.SetText("X-CAPTURED", "changed raw collision evidence")
			changed := dailyGroupRead(t, objects)
			if changed.Groups[0].StructuralReady || changed.Groups[0].Revision == before {
				t.Fatal("collision change not captured")
			}
		})
	}
	// Denial-only legacy canonicalization must NOT relax the closed v2 parser.
	desc, _ := p.Description(p.Members()[0].MemberID, false)
	if _, err := ParseDailyPartsV2Description(strings.Replace(desc, "task="+task, "task="+strings.ToUpper(task), 1)); err == nil {
		t.Fatal("v2 uppercase identity admitted")
	}
	if hints := dailyPartsHints("[hassle:v2 task=" + strings.ToUpper(task) + "]"); len(hints) != 0 {
		t.Fatal("legacy canonicalization leaked into v2 hints")
	}
}

func TestDailyPartsGroupsReservedScanRepeatedLegacyMarkers(t *testing.T) {
	// Well under a resource cap. No timing threshold: source uses one ASCII fold,
	// not repeated suffix allocations, and classification must remain unchanged.
	for _, marker := range []string{"[hassle:v1 task=e3ae442f-5498-4cd6-a362-312ba81b9123]", hourlyMarker(2), completionMarker} {
		text := strings.Repeat(marker+"\n", 8000)
		if dailyPartsReserved(text) {
			t.Fatal("legacy-only denial classification changed")
		}
		if !dailyPartsReserved(text + "[HASSLE:V2 broken]") {
			t.Fatal("unknown reserved suffix lost")
		}
	}
}

func TestDailyPartsGroupsBounds(t *testing.T) {
	p := dailyV2Compile(t, dailyV2Schedule("2027-02-12T09:17", 24, false, ""))
	objects := make([]caldav.CalendarObject, 2000)
	for i := range objects {
		objects[i].Path = fmt.Sprintf("/caldav/v2/synthetic@example.test/events/bound-%d.ics", i)
	}
	if _, err := ReadDailyPartsV2Groups(objects, dailyGroupLocation, now); err != nil {
		t.Fatal("2000 refused", err)
	}
	objects = append(objects, caldav.CalendarObject{Path: dailyGroupLocation.CalendarID + "overflow.ics"})
	if _, err := ReadDailyPartsV2Groups(objects, dailyGroupLocation, now); err == nil {
		t.Fatal("2001 accepted")
	}
	one := dailyGroupObjects(t, p)[0]
	if _, err := ReadDailyPartsV2Groups([]caldav.CalendarObject{one, one}, dailyGroupLocation, now); err == nil {
		t.Fatal("duplicate href accepted")
	}
	for _, count := range []int{1000, 1001} {
		c := ical.NewCalendar()
		for i := 0; i < count; i++ {
			c.Children = append(c.Children, ical.NewEvent().Component)
		}
		o := one
		o.Data = c
		_, err := ReadDailyPartsV2Groups([]caldav.CalendarObject{o}, dailyGroupLocation, now)
		if (err == nil) != (count == 1000) {
			t.Fatalf("event cap %d: %v", count, err)
		}
	}
	for _, size := range []int{1 << 20, (1 << 20) + 1} {
		o := dailyGroupObjects(t, p)[0]
		o.Data.Props.SetText("X-PADDING", "")
		raw, _ := json.Marshal(o.Data)
		o.Data.Props.SetText("X-PADDING", strings.Repeat("x", size-len(raw)))
		raw, _ = json.Marshal(o.Data)
		if len(raw) != size {
			t.Fatal("bad cap fixture")
		}
		_, err := ReadDailyPartsV2Groups([]caldav.CalendarObject{o}, dailyGroupLocation, now)
		if (err == nil) != (size == 1<<20) {
			t.Fatalf("byte cap %d: %v", size, err)
		}
	}
	for _, desc := range []string{"[HASSLE:V2 task=" + dailyV2Task + "]", "[hassle:v99 broken]", "[hassle:parts:v2 broken]", "[chainmail:hourly:v9 minutes=30]"} {
		o := dailyGroupObjects(t, p)[0]
		o.Data.Events()[0].Props.SetText("DESCRIPTION", desc)
		dailyGroupReadonly(t, []caldav.CalendarObject{o})
	}
	bad := dailyGroupLocation
	bad.CalendarID = "/caldav/v2/foreign@example.test/events/../events/"
	if _, err := ReadDailyPartsV2Groups(nil, bad, now); err == nil {
		t.Fatal("noncanonical collection")
	}
}
