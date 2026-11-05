package hassles

import (
	"strings"

	"github.com/emersion/go-ical"
)

const importedTimezoneRestriction = "Imported timezone definition: schedule editing unavailable"

// This is a deliberately narrow provider profile, not a general VTIMEZONE parser.
// Google's 1970 anchors project modern Melbourne rules into incompatible history.
func scheduleTimezoneRestriction(c *ical.Calendar, s *Schedule) string {
	var zone *ical.Component
	for _, child := range c.Children {
		if child.Name != "VTIMEZONE" {
			continue
		}
		if zone != nil {
			return importedTimezoneRestriction
		}
		zone = child
	}
	if zone == nil {
		return ""
	}
	if !googleMelbourneTimezone(zone) {
		return importedTimezoneRestriction
	}
	// Modern rules agree from 2008 onward, including the summer already in
	// progress on January 1. Keep native (no embedded definition) dates unchanged.
	if s != nil {
		for _, wall := range []string{s.Start, s.Until} {
			if wall != "" && (wall < "2008-01-01T00:00" || wall > "2100-12-31T23:59") {
				return importedTimezoneRestriction + " (compatible dates: 2008–2100)"
			}
		}
	}
	return ""
}

func googleMelbourneTimezone(zone *ical.Component) bool {
	if !profileProps(zone, map[string]string{"TZID": Zone}, map[string][]string{"X-LIC-LOCATION": {Zone}}) || len(zone.Children) != 2 {
		return false
	}
	seen := map[string]bool{}
	for _, transition := range zone.Children {
		if seen[transition.Name] || len(transition.Children) != 0 {
			return false
		}
		seen[transition.Name] = true
		var anchor, month, from, to string
		var labels []string
		switch transition.Name {
		case "STANDARD":
			anchor, month, from, to = "19700405T030000", "4", "+1100", "+1000"
			labels = []string{"GMT+10", "AEST"}
		case "DAYLIGHT":
			anchor, month, from, to = "19701004T020000", "10", "+1000", "+1100"
			labels = []string{"GMT+11", "AEDT"}
		default:
			return false
		}
		rules := transition.Props["RRULE"]
		if len(rules) != 1 || len(rules[0].Params) != 0 {
			return false
		}
		parts := strings.Split(rules[0].Value, ";")
		expected := map[string]bool{"FREQ=YEARLY": true, "BYMONTH=" + month: true, "BYDAY=1SU": true}
		if len(parts) != len(expected) {
			return false
		}
		for _, part := range parts {
			if !expected[part] {
				return false
			}
			delete(expected, part)
		}
		if !profileProps(transition, map[string]string{"DTSTART": anchor, "TZOFFSETFROM": from, "TZOFFSETTO": to, "RRULE": rules[0].Value}, map[string][]string{"TZNAME": labels}) {
			return false
		}
	}
	return seen["STANDARD"] && seen["DAYLIGHT"]
}

// Reject unknown, duplicate and parameterized properties, even on cosmetic labels.
func profileProps(c *ical.Component, required map[string]string, optional map[string][]string) bool {
	for name, value := range required {
		props := c.Props[name]
		if len(props) != 1 || props[0].Value != value || len(props[0].Params) != 0 {
			return false
		}
	}
	for name, props := range c.Props {
		if _, ok := required[name]; ok {
			continue
		}
		allowed, ok := optional[name]
		if !ok || len(props) != 1 || len(props[0].Params) != 0 {
			return false
		}
		match := false
		for _, value := range allowed {
			match = match || props[0].Value == value
		}
		if !match {
			return false
		}
	}
	return true
}
