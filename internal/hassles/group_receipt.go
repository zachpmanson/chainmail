package hassles

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/emersion/go-ical"
	"github.com/emersion/go-webdav/caldav"
)

const groupReceiptGrowth = 8192
const groupReceiptTagLimit = 512

func groupSingleProps(c *ical.Component) bool {
	for key, props := range c.Props {
		if len(props) != 1 || props[0].Name != key {
			return false
		}
		for _, values := range props[0].Params {
			if len(values) != 1 {
				return false
			}
		}
	}
	for _, child := range c.Children {
		if !groupSingleProps(child) {
			return false
		}
	}
	return true
}
func groupUTCTimestamp(p *ical.Prop) bool {
	if p == nil || len(p.Params) != 0 {
		return false
	}
	t, err := time.Parse("20060102T150405Z", p.Value)
	return err == nil && t.Year() >= 1970 && t.Year() <= 2100 && t.Format("20060102T150405Z") == p.Value
}
func groupInitialReceipt(target groupTarget, actual caldav.CalendarObject) error {
	deny := errors.New("unexpected initial create receipt")
	if target.Planned == nil || actual.Path != target.Href || !groupNewReceiptTag(actual.ETag) || groupLosslessObject(actual) != nil {
		return deny
	}
	if err := groupReceiptSize(target.Planned.Data, actual.Data); err != nil {
		return err
	}
	if !groupSingleProps(actual.Data.Component) {
		return deny
	}
	want := dailyPartsCloneObject(*target.Planned).Data
	got := dailyPartsCloneObject(actual).Data
	prod := got.Props.Get("PRODID")
	if prod == nil || len(prod.Params) != 0 {
		return deny
	}
	text, err := prod.Text()
	if err != nil || !utf8.ValidString(text) || len(text) > 512 || strings.ContainsAny(text, "\r\n") {
		return deny
	}
	got.Props["PRODID"] = want.Props["PRODID"]
	if props, ok := got.Props["CALSCALE"]; ok {
		if len(props) != 1 || len(props[0].Params) != 0 || props[0].Value != "GREGORIAN" {
			return deny
		}
		delete(got.Props, "CALSCALE")
	}
	children := []*ical.Component{}
	zones := 0
	for _, c := range got.Children {
		if c.Name == "VTIMEZONE" {
			zones++
			if !googleMelbourneTimezone(c) {
				return deny
			}
		} else {
			children = append(children, c)
		}
	}
	if zones > 1 || len(children) != 1 || children[0].Name != "VEVENT" {
		return deny
	}
	if zones > 0 {
		meta, err := ParseDailyPartsV2Description(mustGroupText(want.Events()[0].Props, "DESCRIPTION"))
		if err != nil {
			return deny
		}
		logic := meta.Plan.Logic()
		if scheduleTimezoneRestriction(got, &Schedule{Start: logic.Start, Until: logic.Until}) != "" {
			return deny
		}
	}
	got.Children = children
	e := got.Events()[0]
	expected := want.Events()[0]
	for _, key := range []string{"DTSTAMP", "LAST-MODIFIED"} {
		if !groupUTCTimestamp(e.Props.Get(key)) {
			return deny
		}
		e.Props[key] = expected.Props[key]
	}
	if p, ok := e.Props["CREATED"]; ok {
		if len(p) != 1 || !groupUTCTimestamp(&p[0]) {
			return deny
		}
		delete(e.Props, "CREATED")
	}
	if p, ok := e.Props["SEQUENCE"]; ok {
		if len(p) != 1 || len(p[0].Params) != 0 || p[0].Value != "0" {
			return deny
		}
		delete(e.Props, "SEQUENCE")
	}
	start := e.Props.Get("DTSTART")
	if start == nil {
		return deny
	}
	if values, ok := start.Params["VALUE"]; ok {
		if len(values) != 1 || values[0] != "DATE-TIME" {
			return deny
		}
		delete(start.Params, "VALUE")
	}
	rule := e.Props.Get("RRULE")
	if !dailyPartsRule(rule, expected.Props.Get("RRULE").Value) {
		return deny
	}
	rule.Value = expected.Props.Get("RRULE").Value
	if !reflect.DeepEqual(got, want) {
		return deny
	}
	return nil
}

// Size the actual JSON bytes, independently of the managed-property allowlist.
func groupReceiptSize(planned, actual *ical.Calendar) error {
	plannedJSON, err := json.Marshal(planned)
	if err != nil {
		return err
	}
	actualJSON, err := json.Marshal(actual)
	if err != nil {
		return err
	}
	if len(actualJSON) > len(plannedJSON)+groupReceiptGrowth {
		return errors.New("create receipt exceeded preflight allowance")
	}
	return nil
}

func mustGroupText(props ical.Props, key string) string { value, _ := props.Text(key); return value }

func groupNewReceiptTag(tag string) bool {
	if !groupTransportTag(tag) || len(tag) > groupReceiptTagLimit {
		return false
	}
	for i := range tag {
		if tag[i] > 127 {
			return false
		}
	}
	return true
}
