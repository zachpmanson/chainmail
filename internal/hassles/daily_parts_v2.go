package hassles

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	dailyPartsV2PayloadLimit     = 12000
	dailyPartsV2DescriptionLimit = 16384
	dailyPartsV2WallLayout       = "2006-01-02T15:04"
)

// DailyPartsV2Logic is title-free timing intent, not resource ownership.
type DailyPartsV2Logic struct {
	Start     string
	TimeZone  string
	Minutes   int
	WorkHours bool
	Until     string
}

// DailyPartsV2Member describes a logical role, never a physical UID or href.
type DailyPartsV2Member struct {
	Hour              int
	MemberID          string
	AnchorLocal       string
	AnchorEpochMillis int64
	EndEpochMillis    int64
	Minutes           []int
	RRule             string
	Duration          string
}

// DailyPartsV2Plan is immutable outside this module. Slice accessors return copies.
// A valid plan/hash is not permission to mutate any calendar resource.
type DailyPartsV2Plan struct {
	logic       DailyPartsV2Logic
	task        string
	incarnation string
	members     []DailyPartsV2Member
	payload     string
	planHash    string
	start       time.Time
	until       time.Time
}

func (p *DailyPartsV2Plan) Logic() DailyPartsV2Logic { return p.logic }
func (p *DailyPartsV2Plan) Task() string             { return p.task }
func (p *DailyPartsV2Plan) Incarnation() string      { return p.incarnation }
func (p *DailyPartsV2Plan) Payload() string          { return p.payload }
func (p *DailyPartsV2Plan) PlanHash() string         { return p.planHash }
func (p *DailyPartsV2Plan) Members() []DailyPartsV2Member {
	out := append([]DailyPartsV2Member(nil), p.members...)
	for i := range out {
		out[i].Minutes = append([]int(nil), out[i].Minutes...)
	}
	return out
}

func dailyPartsV2UUID(s string) bool {
	return len(s) == 36 && s == strings.ToLower(s) && ValidUUID(s)
}

func dailyPartsV2ASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] > 127 || (s[i] < 32 && s[i] != '\n') || s[i] == 127 {
			return false
		}
	}
	return true
}

func dailyPartsV2Minutes(s string) (int, bool) {
	switch s {
	case "30", "60", "120", "180", "240", "360", "480", "720", "1440":
		n, _ := strconv.Atoi(s)
		return n, true
	}
	return 0, false
}

// CompileDailyPartsV2 is a pure, restricted hourly compiler. It does not create
// events or change the existing Schedule validator/legacy compilation path.
func CompileDailyPartsV2(s Schedule, task, incarnation string) (*DailyPartsV2Plan, error) {
	start, _, err := s.Validate()
	if err != nil {
		return nil, err
	}
	if s.Cadence != "hourly" || !utf8.ValidString(s.Title) || !dailyPartsV2UUID(task) || !dailyPartsV2UUID(incarnation) {
		return nil, errors.New("unsupported DAILY-parts v2 input or identity")
	}
	minutes := int(s.Interval * 60)
	minuteSlots := []int{start.Minute()}
	if minutes == 30 {
		minuteSlots = []int{start.Minute() % 30, start.Minute()%30 + 30}
	}
	var hours []int
	for hour := 0; hour < 24; hour++ {
		if s.WorkHours && (hour < 9 || hour >= 17) {
			continue
		}
		if minutes >= 60 && (hour-start.Hour()+24)%(minutes/60) != 0 {
			continue
		}
		// Reject the potential grid before any finite-UNTIL omission.
		if hour == 2 {
			return nil, errors.New("DAILY-parts v2 cannot include hour 02")
		}
		hours = append(hours, hour)
	}
	p := &DailyPartsV2Plan{
		logic: DailyPartsV2Logic{Start: s.Start, TimeZone: Zone, Minutes: minutes, WorkHours: s.WorkHours, Until: s.Until},
		task:  task, incarnation: incarnation, start: start,
	}
	if s.Until != "" {
		p.until, err = WallTime(s.Until)
		if err != nil {
			return nil, err
		}
	}
	minuteText := strconv.Itoa(minuteSlots[0])
	if len(minuteSlots) == 2 {
		minuteText += "," + strconv.Itoa(minuteSlots[1])
	}
	rule := "FREQ=DAILY"
	if !p.until.IsZero() {
		rule += ";UNTIL=" + p.until.UTC().Format("20060102T150405Z")
	}
	rule += ";INTERVAL=1;WKST=MO;BYMINUTE=" + minuteText
	if s.WorkHours {
		rule += ";BYDAY=" + workDays
	}
	for _, hour := range hours {
		anchor, err := dailyPartsV2Anchor(start, hour, minuteSlots, s.WorkHours)
		if err != nil {
			return nil, err
		}
		// A required anchor may be in 2101: omit by UNTIL before checking range.
		if !p.until.IsZero() && anchor.After(p.until) {
			continue
		}
		if anchor.Year() < 1970 || anchor.Year() > 2100 {
			return nil, errors.New("DAILY-parts v2 required anchor outside supported years")
		}
		memberID := digest(fmt.Sprintf("hassles-daily-part-member/2\n%s\n%s\n%02d\n", task, incarnation, hour))
		p.members = append(p.members, DailyPartsV2Member{
			Hour: hour, MemberID: memberID, AnchorLocal: anchor.Format(dailyPartsV2WallLayout),
			AnchorEpochMillis: anchor.UnixMilli(), EndEpochMillis: anchor.UnixMilli() + 300000,
			Minutes: append([]int(nil), minuteSlots...), RRule: rule, Duration: "PT5M",
		})
	}
	if len(p.members) == 0 || len(p.members) > 24 {
		return nil, errors.New("invalid DAILY-parts v2 roster")
	}
	work := "0"
	if s.WorkHours {
		work = "1"
	}
	until := s.Until
	if until == "" {
		until = "-"
	}
	var payload strings.Builder
	fmt.Fprintf(&payload, "hassles-daily-parts/2\ntask=%s\nincarnation=%s\ntimezone=%s\nstart=%s\nminutes=%d\nwork=%s\nuntil=%s\n", task, incarnation, Zone, s.Start, minutes, work, until)
	for _, member := range p.members {
		fmt.Fprintf(&payload, "member=%02d,%s,%s\n", member.Hour, member.AnchorLocal, member.MemberID)
	}
	p.payload = payload.String()
	if len(p.payload) > dailyPartsV2PayloadLimit || !dailyPartsV2ASCII(p.payload) {
		return nil, errors.New("invalid DAILY-parts v2 payload")
	}
	p.planHash = digest(p.payload)
	return p, nil
}

// Deliberately range-free: the caller must compare UNTIL before rejecting a
// derived year. Parsing and roundtrip checks still forbid normalized wall times.
func dailyPartsV2Anchor(start time.Time, hour int, minutes []int, work bool) (time.Time, error) {
	date := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, time.UTC)
	for day := 0; day < 8; day++ {
		d := date.AddDate(0, 0, day)
		if work && (d.Weekday() == time.Saturday || d.Weekday() == time.Sunday) {
			continue
		}
		for _, minute := range minutes {
			wall := fmt.Sprintf("%sT%02d:%02d", d.Format("2006-01-02"), hour, minute)
			anchor, err := time.ParseInLocation(dailyPartsV2WallLayout, wall, start.Location())
			if err != nil || anchor.Format(dailyPartsV2WallLayout) != wall || anchor.Add(-time.Hour).Format(dailyPartsV2WallLayout) == wall || anchor.Add(time.Hour).Format(dailyPartsV2WallLayout) == wall {
				return time.Time{}, errors.New("invalid or ambiguous DAILY-parts v2 anchor")
			}
			if !anchor.Before(start) {
				return anchor, nil
			}
		}
	}
	return time.Time{}, errors.New("no DAILY-parts v2 anchor within bounded search")
}

// ParseDailyPartsV2Payload validates bytes, then independently recompiles the
// complete roster. Neither the supplied roster nor its hash grants authority.
func ParseDailyPartsV2Payload(payload, planHash string) (*DailyPartsV2Plan, error) {
	if len(payload) > dailyPartsV2PayloadLimit || !dailyPartsV2ASCII(payload) || len(planHash) != 64 || digest(payload) != planHash || !strings.HasSuffix(payload, "\n") || strings.Count(payload, "\n") > 32 {
		return nil, errors.New("invalid DAILY-parts v2 payload bytes/hash")
	}
	lines := strings.Split(payload, "\n")
	if len(lines) < 10 || lines[0] != "hassles-daily-parts/2" || lines[len(lines)-1] != "" {
		return nil, errors.New("unsupported DAILY-parts v2 payload")
	}
	keys := []string{"task=", "incarnation=", "timezone=", "start=", "minutes=", "work=", "until="}
	values := make([]string, len(keys))
	for i, key := range keys {
		if !strings.HasPrefix(lines[i+1], key) {
			return nil, errors.New("noncanonical DAILY-parts v2 fields")
		}
		values[i] = strings.TrimPrefix(lines[i+1], key)
	}
	minutes, ok := dailyPartsV2Minutes(values[4])
	if !ok || (values[5] != "0" && values[5] != "1") {
		return nil, errors.New("invalid DAILY-parts v2 minutes/work")
	}
	for _, line := range lines[8 : len(lines)-1] {
		if len(line) != len("member=")+2+1+16+1+64 || !strings.HasPrefix(line, "member=") {
			return nil, errors.New("invalid DAILY-parts v2 member")
		}
	}
	until := values[6]
	if until == "-" {
		until = ""
	} else if until == "" {
		return nil, errors.New("invalid DAILY-parts v2 end")
	}
	p, err := CompileDailyPartsV2(Schedule{Title: "Synthetic", Start: values[3], TimeZone: values[2], Cadence: "hourly", Interval: float64(minutes) / 60, WorkHours: values[5] == "1", Until: until}, values[0], values[1])
	if err != nil {
		return nil, err
	}
	if p.payload != payload || p.planHash != planHash {
		return nil, errors.New("noncanonical DAILY-parts v2 roster")
	}
	return p, nil
}

// DailyPartsV2Metadata is decoded provenance only, not inventory/edit readiness.
type DailyPartsV2Metadata struct {
	Plan                  *DailyPartsV2Plan
	MemberID              string
	HasCompletionMetadata bool
}

// Description formats only the closed three-line codec (plus optional exact
// completion marker). Titles/notes/physical identities cannot enter this codec.
func (p *DailyPartsV2Plan) Description(memberID string, completion bool) (string, error) {
	if p == nil || p.payload == "" {
		return "", errors.New("uninitialized DAILY-parts v2 plan")
	}
	found := false
	for _, member := range p.members {
		found = found || member.MemberID == memberID
	}
	if !found {
		return "", errors.New("unknown DAILY-parts v2 member")
	}
	desc := fmt.Sprintf("[hassle:v2 task=%s incarnation=%s member=%s plan=%s]\n[chainmail:hourly:v2 minutes=%d]\n[hassle:parts:v2 data=%s]", p.task, p.incarnation, memberID, p.planHash, p.logic.Minutes, base64.RawURLEncoding.EncodeToString([]byte(p.payload)))
	if completion {
		desc += "\n" + completionMarker
	}
	if len(desc) > dailyPartsV2DescriptionLimit || strings.Contains(desc, "[docket]") {
		return "", errors.New("invalid DAILY-parts v2 description")
	}
	return desc, nil
}

func ParseDailyPartsV2Description(desc string) (*DailyPartsV2Metadata, error) {
	if len(desc) > dailyPartsV2DescriptionLimit || !dailyPartsV2ASCII(desc) || strings.Contains(desc, "[docket]") || (strings.Count(desc, "\n") != 2 && strings.Count(desc, "\n") != 3) {
		return nil, errors.New("invalid DAILY-parts v2 description bytes")
	}
	lines := strings.Split(desc, "\n")
	completion := len(lines) == 4
	if completion && lines[3] != completionMarker {
		return nil, errors.New("unsupported DAILY-parts v2 completion marker")
	}
	header := strings.Split(lines[0], " ")
	if len(header) != 5 || header[0] != "[hassle:v2" || !strings.HasPrefix(header[1], "task=") || !strings.HasPrefix(header[2], "incarnation=") || !strings.HasPrefix(header[3], "member=") || !strings.HasPrefix(header[4], "plan=") || !strings.HasSuffix(header[4], "]") {
		return nil, errors.New("invalid DAILY-parts v2 header")
	}
	planHash := strings.TrimSuffix(strings.TrimPrefix(header[4], "plan="), "]")
	const prefix = "[hassle:parts:v2 data="
	if !strings.HasPrefix(lines[2], prefix) || !strings.HasSuffix(lines[2], "]") {
		return nil, errors.New("invalid DAILY-parts v2 payload marker")
	}
	encoded := strings.TrimSuffix(strings.TrimPrefix(lines[2], prefix), "]")
	if len(encoded) > base64.RawURLEncoding.EncodedLen(dailyPartsV2PayloadLimit) {
		return nil, errors.New("oversize DAILY-parts v2 payload")
	}
	payload, err := base64.RawURLEncoding.Strict().DecodeString(encoded)
	if err != nil || len(payload) > dailyPartsV2PayloadLimit || base64.RawURLEncoding.EncodeToString(payload) != encoded {
		return nil, errors.New("noncanonical DAILY-parts v2 base64")
	}
	p, err := ParseDailyPartsV2Payload(string(payload), planHash)
	if err != nil {
		return nil, err
	}
	memberID := strings.TrimPrefix(header[3], "member=")
	canonical, err := p.Description(memberID, completion)
	if err != nil || canonical != desc {
		return nil, errors.New("conflicting DAILY-parts v2 metadata")
	}
	return &DailyPartsV2Metadata{Plan: p, MemberID: memberID, HasCompletionMetadata: completion}, nil
}

// SupportsCompletion reports membership only for the bounded completion-witness
// domain. False does not establish non-recurrence, nor validate elapsed DTEND.
func (p *DailyPartsV2Plan) SupportsCompletion(memberID string, at time.Time) bool {
	if p == nil || p.payload == "" || at.UTC().Year() < 1970 || at.UTC().Year() > 2100 || at.UTC().Second() != 0 || at.Nanosecond() != 0 {
		return false
	}
	local := at.In(p.start.Location())
	checked, err := WallTime(local.Format(dailyPartsV2WallLayout))
	if err != nil || !checked.Equal(at) || at.Before(p.start) || (!p.until.IsZero() && at.After(p.until)) || (p.logic.WorkHours && !withinWorkHours(local)) {
		return false
	}
	if p.logic.Minutes == 30 {
		if local.Minute()%30 != p.start.Minute()%30 {
			return false
		}
	} else if (local.Hour()-p.start.Hour()+24)%(p.logic.Minutes/60) != 0 || local.Minute() != p.start.Minute() {
		return false
	}
	for _, member := range p.members {
		if member.MemberID != memberID || local.Hour() != member.Hour || at.UnixMilli() < member.AnchorEpochMillis {
			continue
		}
		for _, minute := range member.Minutes {
			if local.Minute() == minute {
				return true
			}
		}
	}
	return false
}
