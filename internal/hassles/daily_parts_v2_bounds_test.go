package hassles

import (
	"math"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestDailyPartsV2LogicalAdmissionBoundaries(t *testing.T) {
	base := dailyV2Schedule("2027-02-12T09:17", 24, false, "")
	changes := map[string]func(*Schedule){
		"blank-title":          func(s *Schedule) { s.Title = " " },
		"title-cap":            func(s *Schedule) { s.Title = strings.Repeat("x", 501) },
		"title-byte-cap":       func(s *Schedule) { s.Title = strings.Repeat("é", 251) },
		"title-newline":        func(s *Schedule) { s.Title = "a\nb" },
		"title-CR":             func(s *Schedule) { s.Title = "a\rb" },
		"invalid-title-utf8":   func(s *Schedule) { s.Title = string([]byte{0xff}) },
		"zone":                 func(s *Schedule) { s.TimeZone = "UTC" },
		"daily":                func(s *Schedule) { s.Cadence = "daily" },
		"once":                 func(s *Schedule) { s.Cadence = "once"; s.Interval = 1 },
		"interval-zero":        func(s *Schedule) { s.Interval = 0 },
		"interval-negative":    func(s *Schedule) { s.Interval = -1 },
		"interval-fraction":    func(s *Schedule) { s.Interval = 1.5 },
		"interval-unsupported": func(s *Schedule) { s.Interval = 5 },
		"interval-NaN":         func(s *Schedule) { s.Interval = math.NaN() },
		"interval-Inf":         func(s *Schedule) { s.Interval = math.Inf(1) },
		"start-seconds":        func(s *Schedule) { s.Start = "2027-02-12T09:17:00" },
		"start-Unicode":        func(s *Schedule) { s.Start = "2027-02-12T０９:17" },
		"start-lower-year":     func(s *Schedule) { s.Start = "1969-12-31T09:17" },
		"start-upper-year":     func(s *Schedule) { s.Start = "2101-01-01T09:17" },
		"non-leap2100":         func(s *Schedule) { s.Start = "2100-02-29T09:17" },
		"non-leap2027":         func(s *Schedule) { s.Start = "2027-02-29T09:17" },
		"start-gap":            func(s *Schedule) { s.Start = "2027-10-03T02:17" },
		"start-fold":           func(s *Schedule) { s.Start = "2027-04-04T02:17" },
		"until-before":         func(s *Schedule) { s.Until = "2027-02-12T09:16" },
		"until-equal":          func(s *Schedule) { s.Until = s.Start },
		"until-upper-year":     func(s *Schedule) { s.Until = "2101-01-01T09:17" },
		"until-fold":           func(s *Schedule) { s.Until = "2027-04-04T02:17" },
		"until-gap":            func(s *Schedule) { s.Until = "2027-10-03T02:17" },
		"work-before":          func(s *Schedule) { s.WorkHours = true; s.Start = "2027-02-12T08:59" },
		"work-after":           func(s *Schedule) { s.WorkHours = true; s.Start = "2027-02-12T17:00" },
		"work-Saturday":        func(s *Schedule) { s.WorkHours = true; s.Start = "2027-02-13T09:17" },
		"work-Sunday":          func(s *Schedule) { s.WorkHours = true; s.Start = "2027-02-14T09:17" },
		"required-next-year":   func(s *Schedule) { s.Start = "2100-12-31T23:47"; s.Interval = 2 },
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			s := base
			change(&s)
			if _, err := CompileDailyPartsV2(s, dailyV2Task, dailyV2Incarnation); err == nil {
				t.Fatal("accepted unsupported logical input")
			}
		})
	}
	for _, uuid := range []string{
		"", " " + dailyV2Task, dailyV2Task + " ",
		"ABCDEFAB-0000-4000-8000-000000000001",
		"00000000-0000-0000-8000-000000000001",
		"00000000-0000-9000-8000-000000000001",
		"00000000-0000-4000-7000-000000000001",
		"00000000-0000-4000-c000-000000000001",
		"00000000000040008000000000000001",
	} {
		if _, err := CompileDailyPartsV2(base, uuid, dailyV2Incarnation); err == nil {
			t.Fatalf("accepted task %q", uuid)
		}
		if _, err := CompileDailyPartsV2(base, dailyV2Task, uuid); err == nil {
			t.Fatalf("accepted incarnation %q", uuid)
		}
	}
	for _, uuid := range []string{
		"abcdefab-0000-1000-8000-000000000001",
		"abcdefab-0000-8000-b000-000000000001",
		dailyV2Task,
	} {
		if _, err := CompileDailyPartsV2(base, uuid, uuid); err != nil {
			t.Fatalf("canonical UUIDs need not differ: %v", err)
		}
	}
	for _, start := range []string{"1970-01-01T09:17", "2000-02-29T09:17", "2028-02-29T09:17", "2100-12-31T23:59"} {
		p := dailyV2Compile(t, dailyV2Schedule(start, 24, false, ""))
		if p.Members()[0].AnchorLocal != start {
			t.Fatal("valid boundary changed")
		}
	}
}

func TestDailyPartsV2CivilAnchorSelectionBeforeGapFoldResolution(t *testing.T) {
	// 02:xx on the start date is already BEFORE the valid civil start. Its gap
	// or fold must not reject a plan whose selected anchor is the next day.
	for _, c := range []struct {
		start, anchor, utc string
		interval           float64
	}{
		{"2027-10-03T03:17", "2027-10-04T02:17", "2027-10-03T15:17:00Z", 1},
		{"2027-10-03T03:47", "2027-10-04T02:17", "2027-10-03T15:17:00Z", 0.5},
		{"2027-04-04T03:17", "2027-04-05T02:17", "2027-04-04T16:17:00Z", 1},
		{"2027-04-04T03:47", "2027-04-05T02:17", "2027-04-04T16:17:00Z", 0.5},
	} {
		t.Run(c.start, func(t *testing.T) {
			p := dailyV2Compile(t, dailyV2Schedule(c.start, c.interval, false, ""))
			if members := p.Members(); len(members) != 24 || members[2].Hour != 2 || members[2].AnchorLocal != c.anchor || members[2].AnchorEpochMillis != dailyV2Instant(t, c.utc).UnixMilli() {
				t.Fatalf("pre-start gap/fold affected selected civil anchor: %+v", members)
			}
			if _, err := ParseDailyPartsV2Payload(p.Payload(), p.PlanHash()); err != nil {
				t.Fatal(err)
			}
			start, _ := WallTime(c.start)
			until := start.Add(time.Minute).Format(dailyPartsV2WallLayout)
			finite := dailyV2Compile(t, dailyV2Schedule(c.start, c.interval, false, until))
			if members := finite.Members(); len(members) != 1 || members[0].Hour != 3 || members[0].AnchorLocal != c.start {
				t.Fatalf("finite pre-start gap/fold roster: %+v", members)
			}
		})
	}
	// Initial invalid starts still fail; a selected derived gap/fold cannot be
	// skipped, normalized, or rescued by an UNTIL earlier than that candidate.
	for _, start := range []string{
		"2027-10-03T02:17", "2027-04-04T02:17",
		"2027-10-03T01:47", "2027-04-04T01:47",
		"2027-10-02T03:17", "2027-04-03T03:17",
	} {
		for _, interval := range []float64{0.5, 1} {
			for _, finite := range []bool{false, true} {
				until := ""
				if finite {
					// Civil arithmetic, not DST-normalizing Melbourne arithmetic.
					civil, err := time.Parse(dailyPartsV2WallLayout, start)
					if err != nil {
						t.Fatal(err)
					}
					until = civil.Add(time.Minute).Format(dailyPartsV2WallLayout)
				}
				if _, err := CompileDailyPartsV2(dailyV2Schedule(start, interval, false, until), dailyV2Task, dailyV2Incarnation); err == nil {
					t.Fatalf("accepted invalid initial/selected anchor: %s interval=%g finite=%v", start, interval, finite)
				}
			}
		}
	}
	last := dailyV2Schedule("2100-12-31T23:47", 2, false, "2100-12-31T23:59")
	finite := dailyV2Compile(t, last)
	if members := finite.Members(); len(members) != 1 || members[0].Hour != 23 {
		t.Fatalf("unique 2101 anchor not omitted before range check: %+v", members)
	}
	last.Until = ""
	if _, err := CompileDailyPartsV2(last, dailyV2Task, dailyV2Incarnation); err == nil {
		t.Fatal("included 2101 anchor accepted")
	}
}

func TestDailyPartsV2TitleFreeAndDefensivePlan(t *testing.T) {
	s := dailyV2Schedule("2027-02-12T10:47", 0.5, true, "")
	p := dailyV2Compile(t, s)
	s.Title = "Changed [docket] and Unicode café 日本語"
	other := dailyV2Compile(t, s)
	if p.Payload() != other.Payload() || p.PlanHash() != other.PlanHash() {
		t.Fatal("title affected manifest identity")
	}
	members := p.Members()
	desc, err := p.Description(members[0].MemberID, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, title := range []string{"Synthetic", s.Title, "[docket]"} {
		if strings.Contains(p.Payload(), title) || strings.Contains(desc, title) {
			t.Fatalf("title leaked to timing metadata: %q", title)
		}
	}
	before := p.Members()
	members[0].Hour = 2
	members[0].Minutes[0] = 0
	members[0].MemberID = strings.Repeat("0", 64)
	members[0].RRule = "FREQ=HOURLY"
	members[0].AnchorEpochMillis = 0
	members[0].EndEpochMillis = 0
	logic := p.Logic()
	logic.Start = "1970-01-01T00:00"
	logic.Minutes = 60
	logic.TimeZone = "UTC"
	if !reflect.DeepEqual(before, p.Members()) || p.Logic() == logic {
		t.Fatal("returned data mutated the plan")
	}
	after, err := p.Description(before[0].MemberID, false)
	if err != nil || desc != after || !p.SupportsCompletion(before[0].MemberID, time.UnixMilli(before[0].AnchorEpochMillis)) {
		t.Fatal("defensive copy failed to protect formatter/predicate")
	}
	if _, err := p.Description(members[0].MemberID, false); err == nil {
		t.Fatal("formatter accepted mutated/unknown member identity")
	}
	var zero DailyPartsV2Plan
	if _, err := zero.Description("", false); err == nil || zero.SupportsCompletion("", time.Time{}) {
		t.Fatal("zero plan accepted")
	}
	var absent *DailyPartsV2Plan
	if _, err := absent.Description("", false); err == nil || absent.SupportsCompletion("", time.Time{}) {
		t.Fatal("nil plan accepted")
	}
}

func TestDailyPartsV2HalfHourClipMondayAndInclusiveEnd(t *testing.T) {
	p := dailyV2Compile(t, dailyV2Schedule("2027-02-12T10:47", 0.5, true, "2027-02-15T09:17"))
	members := p.Members()
	if len(members) != 8 || members[0].AnchorLocal != "2027-02-15T09:17" || members[1].AnchorLocal != "2027-02-12T10:47" {
		t.Fatalf("Friday/Monday clipped roster: %+v", members)
	}
	if p.SupportsCompletion(members[1].MemberID, dailyV2Instant(t, "2027-02-11T23:17:00Z")) {
		t.Fatal("half-hour phantom before global/role anchor")
	}
	if !p.SupportsCompletion(members[0].MemberID, dailyV2Instant(t, "2027-02-14T22:17:00Z")) || p.SupportsCompletion(members[0].MemberID, dailyV2Instant(t, "2027-02-14T22:47:00Z")) {
		t.Fatal("UNTIL must include equality and reject later slots")
	}
	for _, instant := range []string{"2027-02-12T22:17:00Z", "2027-02-13T22:17:00Z"} {
		if p.SupportsCompletion(members[0].MemberID, dailyV2Instant(t, instant)) {
			t.Fatal("weekend witness admitted")
		}
	}
	// A final week-day boundary in 2100 can omit roles whose next weekday is 2101.
	finite := dailyV2Compile(t, dailyV2Schedule("2100-12-31T16:47", 0.5, true, "2100-12-31T16:59"))
	if got := finite.Members(); len(got) != 1 || got[0].Hour != 16 {
		t.Fatalf("overflow roles not omitted before year check: %+v", got)
	}
	if _, err := CompileDailyPartsV2(dailyV2Schedule("2100-12-31T16:47", 0.5, true, ""), dailyV2Task, dailyV2Incarnation); err == nil {
		t.Fatal("unbounded required Monday 2101 accepted")
	}
}

func TestDailyPartsV2SupportedCompletionDomain(t *testing.T) {
	p := dailyV2Compile(t, dailyV2Schedule("2027-02-12T09:17", 2, false, "2027-02-13T09:17"))
	members := p.Members()
	var role DailyPartsV2Member
	for _, member := range members {
		if member.Hour == 9 {
			role = member
		}
	}
	for _, value := range []string{"2027-02-11T22:17:00Z", "2027-02-12T22:17:00Z"} {
		if !p.SupportsCompletion(role.MemberID, dailyV2Instant(t, value)) {
			t.Fatalf("supported exact anchor/end witness rejected: %s", value)
		}
	}
	for _, value := range []string{
		"2027-02-10T22:17:00Z", // Before anchor/global start.
		"2027-02-11T22:18:00Z", // Wrong minute.
		"2027-02-11T23:17:00Z", // Wrong role and logical phase.
		"2027-02-12T00:17:00Z", // Another member's hour.
		"2027-02-13T22:17:00Z", // After inclusive end.
		"2027-02-11T22:17:01Z", // Not an exact minute.
		"2027-02-11T22:17:00.000000001Z",
		"1969-12-31T23:17:00Z",
		"2101-01-01T00:17:00Z",
	} {
		if p.SupportsCompletion(role.MemberID, dailyV2Instant(t, value)) {
			t.Fatalf("unsupported witness accepted: %s", value)
		}
	}
	// The caller's time.Location cannot change the exact-UTC-minute predicate.
	anchor := time.UnixMilli(role.AnchorEpochMillis)
	if !p.SupportsCompletion(role.MemberID, anchor.In(time.FixedZone("odd-offset", 1))) || p.SupportsCompletion(role.MemberID, anchor.Add(-time.Second).In(time.FixedZone("odd-offset", 1))) {
		t.Fatal("completion membership depended on caller display timezone")
	}
	if p.SupportsCompletion("physical-uid@calendar.invalid", time.UnixMilli(role.AnchorEpochMillis)) || p.SupportsCompletion(p.PlanHash(), time.UnixMilli(role.AnchorEpochMillis)) {
		t.Fatal("physical identity or plan hash substituted for member domain")
	}
	lower := dailyV2Compile(t, dailyV2Schedule("1970-01-01T09:17", 24, false, ""))
	lowerRole := lower.Members()[0]
	if lowerRole.AnchorEpochMillis >= 0 || lower.SupportsCompletion(lowerRole.MemberID, dailyV2Instant(t, "1969-12-31T23:17:00Z")) {
		t.Fatal("local1970/UTC1969 is valid compile input but unsupported witness")
	}
	if !lower.SupportsCompletion(lowerRole.MemberID, dailyV2Instant(t, "1970-01-01T23:17:00Z")) {
		t.Fatal("bounded next-day witness rejected")
	}
	upper := dailyV2Compile(t, dailyV2Schedule("2100-12-31T00:17", 24, false, ""))
	upperRole := upper.Members()[0]
	if !upper.SupportsCompletion(upperRole.MemberID, time.UnixMilli(upperRole.AnchorEpochMillis)) || upper.SupportsCompletion(upperRole.MemberID, dailyV2Instant(t, "2100-12-31T13:17:00Z")) {
		t.Fatal("UTC2100/local2101 witness bound incorrect")
	}
}

func TestDailyPartsV2ElapsedDurationIsNotWitnessValidation(t *testing.T) {
	for _, c := range []struct {
		start              string
		endLocal           string
		unsupportedEndWall bool
	}{
		{"2027-10-03T01:59", "2027-10-03T03:04", false},
		{"2027-04-04T01:59", "2027-04-04T02:04", true},
		{"2100-12-31T23:59", "2101-01-01T00:04", true},
	} {
		p := dailyV2Compile(t, dailyV2Schedule(c.start, 24, false, ""))
		member := p.Members()[0]
		start := time.UnixMilli(member.AnchorEpochMillis)
		end := time.UnixMilli(member.EndEpochMillis)
		loc, _ := time.LoadLocation(Zone)
		if end.Sub(start) != 5*time.Minute || end.In(loc).Format(dailyPartsV2WallLayout) != c.endLocal || member.Duration != "PT5M" {
			t.Fatalf("duration was civil rather than elapsed: %+v", member)
		}
		_, err := WallTime(c.endLocal)
		if (err != nil) != c.unsupportedEndWall {
			t.Fatalf("unexpected endpoint support: %s %v", c.endLocal, err)
		}
		if p.SupportsCompletion(member.MemberID, end) {
			t.Fatal("elapsed endpoint was mistaken for an occurrence witness")
		}
	}
	// Isolate the fold guard with a matching 02:17 role anchored on an ordinary
	// date. This is witness validation, not provider recurrence expansion.
	p := dailyV2Compile(t, dailyV2Schedule("2027-02-12T02:17", 24, false, ""))
	member := p.Members()[0]
	if member.Hour != 2 || !p.SupportsCompletion(member.MemberID, dailyV2Instant(t, "2027-04-02T15:17:00Z")) {
		t.Fatal("valid nonfold 02:17 control rejected")
	}
	loc, _ := time.LoadLocation(Zone)
	for _, value := range []string{"2027-04-03T15:17:00Z", "2027-04-03T16:17:00Z"} {
		at := dailyV2Instant(t, value)
		if at.In(loc).Format(dailyPartsV2WallLayout) != "2027-04-04T02:17" {
			t.Fatal("invalid literal fold witness")
		}
		if p.SupportsCompletion(member.MemberID, at) {
			t.Fatal("fold instant accepted")
		}
	}
}
