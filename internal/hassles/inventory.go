package hassles

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/emersion/go-ical"
	"github.com/emersion/go-webdav/caldav"
)

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-8][0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$`)
var markerPattern = regexp.MustCompile(`\[hassle:v1 task=([^\]]+)\]`)

func ValidUUID(s string) bool { return uuidPattern.MatchString(s) }
func digest(s string) string  { sum := sha256.Sum256([]byte(s)); return hex.EncodeToString(sum[:]) }

// A malformed or repeated marker never grants grouping or ownership.
func marker(description string) (string, bool) {
	m := markerPattern.FindAllStringSubmatch(description, -1)
	if len(m) != 1 || strings.Count(description, "[hassle:") != 1 || !ValidUUID(m[0][1]) {
		return "", false
	}
	return strings.ToLower(m[0][1]), true
}

type Task struct {
	ID          string    `json:"id"`
	Revision    string    `json:"revision"`
	Title       string    `json:"title"`
	Reminder    string    `json:"reminder"`
	Resources   int       `json:"resources"`
	Writable    bool      `json:"writable"`
	Restriction string    `json:"restriction"`
	Schedule    *Schedule `json:"schedule"`
	members     []caldav.CalendarObject
	active      bool
	reminders   []string
}

func Inventory(objects []caldav.CalendarObject, now time.Time) ([]Task, error) {
	if len(objects) > 2000 {
		return nil, errors.New("calendar exceeds 2000-resource safety limit")
	}
	groups := map[string]*Task{}
	paths := map[string]bool{}
	for _, o := range objects {
		if paths[o.Path] {
			return nil, errors.New("duplicate calendar resource href")
		}
		paths[o.Path] = true
		if o.Data == nil || len(o.Data.Events()) == 0 {
			continue
		}
		events := o.Data.Events()
		masters := 0
		group := ""
		ambiguous := false
		owned := true
		active := false
		title := "(untitled reminder)"
		uid := ""
		if o.Data.Props.Get("METHOD") != nil {
			owned = false
		}
		for _, child := range o.Data.Children {
			if child.Name != "VEVENT" && child.Name != "VTIMEZONE" {
				owned = false
			}
		}
		for _, e := range events {
			eventUID, _ := e.Props.Text("UID")
			if eventUID == "" || (uid != "" && eventUID != uid) {
				owned = false
			}
			uid = eventUID
			desc, _ := e.Props.Text("DESCRIPTION")
			id, ok := marker(desc)
			if len(e.Props["DESCRIPTION"]) > 1 {
				owned = false
				ok = false
				ambiguous = true
			}
			if strings.Contains(desc, "[hassle:") && !ok {
				ambiguous = true
			}
			if ok {
				if group != "" && group != id {
					ambiguous = true
				}
				group = id
			}
			if e.Props.Get("RECURRENCE-ID") == nil {
				masters++
				if !ok {
					ambiguous = true
				}
				if text, _ := e.Props.Text("SUMMARY"); text != "" {
					title = text
				}
			}
			// Exceptions may inherit the master description, but never grant ownership themselves.
			if e.Props.Get("RECURRENCE-ID") == nil && !strings.Contains(desc, "[docket]") {
				owned = false
			}
			if len(e.Props["ATTENDEE"])+len(e.Props["ORGANIZER"]) > 0 {
				owned = false
			}
			status, _ := e.Props.Text("STATUS")
			if strings.ToUpper(status) != "CANCELLED" {
				active = true
			}
		}
		if masters == 0 {
			owned = false
			ambiguous = true
		}
		id := "resource:" + digest(o.Path)
		if group != "" && !ambiguous {
			id = "task:" + group
		}
		t := groups[id]
		if t == nil {
			t = &Task{ID: id, Title: title, Writable: true, Reminder: "Imported schedule — view in calendar", members: []caldav.CalendarObject{}}
			groups[id] = t
		}
		resourceReminders := []string{}
		for _, e := range events {
			status, _ := e.Props.Text("STATUS")
			if strings.ToUpper(status) != "CANCELLED" {
				resourceReminders = append(resourceReminders, eventReminder(e, now))
			}
		}
		t.reminders = append(t.reminders, resourceReminders...)
		t.Reminder = strings.Join(t.reminders, "; ")
		t.members = append(t.members, o)
		t.active = t.active || active
		if !owned {
			t.Writable = false
			t.Restriction = "Read-only: not a docket-owned reminder, or contains meeting participants"
		}
		if o.ETag == "" || strings.HasPrefix(o.ETag, "W/") {
			t.Writable = false
			t.Restriction = "Read-only: calendar did not provide a strong ETag"
		}
		hasTimezone := false
		for _, child := range o.Data.Children {
			hasTimezone = hasTimezone || child.Name == "VTIMEZONE"
		}
		if hasTimezone {
			t.Schedule = nil
			if t.Restriction == "" {
				t.Restriction = "Imported timezone definition: schedule editing unavailable"
			}
		} else if len(events) == 1 && masters == 1 {
			s, why := readSchedule(events[0])
			t.Schedule = s
			if s == nil && t.Restriction == "" {
				t.Restriction = why
			}
		} else if t.Restriction == "" {
			t.Restriction = "Imported exceptions/multiple events: schedule editing unavailable"
		}
	}
	out := make([]Task, 0, len(groups))
	for _, t := range groups {
		sort.Slice(t.members, func(i, j int) bool { return t.members[i].Path < t.members[j].Path })
		revision := sha256.New()
		for _, o := range t.members {
			io.WriteString(revision, o.Path+"\x00"+o.ETag+"\x00")
			if err := json.NewEncoder(revision).Encode(o.Data); err != nil {
				return nil, err
			}
			io.WriteString(revision, "\x00")
		}
		t.Revision = hex.EncodeToString(revision.Sum(nil))
		t.Resources = len(t.members)
		if t.Resources > 1 {
			t.Schedule = nil
			if t.Restriction == "" {
				t.Restriction = "Multiple resources: schedule editing unavailable; Done/Delete removes the whole group"
			}
			t.Reminder = strings.Join(t.reminders, "; ")
		}
		if t.active {
			out = append(out, *t)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		left, right := strings.ToLower(out[i].Title), strings.ToLower(out[j].Title)
		if left != right {
			return left < right
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

func Encode(c *ical.Calendar) (string, error) {
	var b strings.Builder
	err := ical.NewEncoder(&b).Encode(c)
	return b.String(), err
}

func NewCalendar(id string, s Schedule, now time.Time) (*ical.Calendar, error) {
	if !ValidUUID(id) {
		return nil, errors.New("invalid task UUID")
	}
	c := ical.NewCalendar()
	c.Props.SetText("VERSION", "2.0")
	c.Props.SetText("PRODID", "-//Chainmail Hassles//EN")
	e := ical.NewEvent()
	e.Props.SetText("UID", "hassle-"+strings.ToLower(id))
	e.Props.SetText("DESCRIPTION", "[docket]\n[hassle:v1 task="+strings.ToLower(id)+"]")
	e.Props.SetText("TRANSP", "TRANSPARENT")
	alarm := ical.NewComponent("VALARM")
	alarm.Props.SetText("ACTION", "DISPLAY")
	alarm.Props.SetText("DESCRIPTION", "Reminder")
	p := ical.NewProp("TRIGGER")
	p.Value = "PT0S"
	alarm.Props.Set(p)
	e.Children = append(e.Children, alarm)
	if err := ApplySchedule(*e, s, now); err != nil {
		return nil, err
	}
	c.Children = append(c.Children, e.Component)
	return c, nil
}

func ApplySchedule(e ical.Event, s Schedule, now time.Time) error {
	start, rule, err := s.Validate()
	if err != nil {
		return err
	}
	summary := e.Props.Get("SUMMARY")
	if summary == nil {
		summary = ical.NewProp("SUMMARY")
		e.Props.Set(summary)
		summary = e.Props.Get("SUMMARY")
	}
	summary.SetText(strings.TrimSpace(s.Title))
	for name, value := range map[string]time.Time{"DTSTART": start, "DTEND": start.Add(5 * time.Minute)} {
		p := e.Props.Get(name)
		if p == nil {
			p = ical.NewProp(name)
			e.Props.Set(p)
			p = e.Props.Get(name)
		}
		p.SetDateTime(value)
	}
	e.Props.Del("DURATION")
	e.Props.SetDateTime("DTSTAMP", now.UTC())
	e.Props.SetDateTime("LAST-MODIFIED", now.UTC())
	e.Props.Del("RRULE")
	if rule != "" {
		p := ical.NewProp("RRULE")
		p.Value = rule
		e.Props.Set(p)
	}
	return nil
}
