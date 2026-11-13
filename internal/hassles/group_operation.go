package hassles

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/mail"
	"reflect"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/emersion/go-ical"
	"github.com/emersion/go-webdav/caldav"
)

// GroupRequest deliberately has no client-supplied physical identity or authority.
// Public JSON is semantic; the stored immutable request is canonical JSON.
type GroupRequest struct {
	Action      string    `json:"action"`
	OperationID string    `json:"operationId"`
	Task        string    `json:"task"`
	Incarnation string    `json:"incarnation"`
	Fence       string    `json:"fence"`
	Revision    string    `json:"revision,omitempty"`
	Schedule    *Schedule `json:"schedule,omitempty"`
}

func DecodeGroupRequest(raw []byte) (GroupRequest, error) {
	var r GroupRequest
	if len(raw) > groupStoreLimit-groupEnvelopeBytes || !utf8.Valid(raw) || !json.Valid(raw) || !groupValidJSONSurrogates(raw) {
		return r, errors.New("oversized or invalid JSON")
	}
	fields, err := groupRequestFields(raw, "action", "operationId", "task", "incarnation", "fence", "revision", "schedule")
	if err != nil {
		return r, err
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return r, err
	}
	switch r.Action {
	case "create":
		if _, forbidden := fields["revision"]; forbidden || len(fields) != 6 {
			return r, errors.New("create requires only schedule")
		}
		schedule, err := groupRequestFields(fields["schedule"], "title", "start", "timeZone", "cadence", "interval", "until", "workHours")
		if err != nil || len(schedule) != 7 {
			return r, errors.New("invalid schedule fields")
		}
	case "delete":
		if _, forbidden := fields["schedule"]; forbidden || len(fields) != 6 {
			return r, errors.New("delete requires only exact revision")
		}
	}
	return r, r.validate()
}

// Decode field names before structs: encoding/json otherwise accepts aliases,
// duplicates and null scalars. This is only for the two public request objects.
func groupRequestFields(raw []byte, allowed ...string) (map[string]json.RawMessage, error) {
	d := json.NewDecoder(bytes.NewReader(raw))
	token, err := d.Token()
	if err != nil || token != json.Delim('{') {
		return nil, errors.New("request object required")
	}
	fields := map[string]json.RawMessage{}
	for d.More() {
		token, err := d.Token()
		if err != nil {
			return nil, err
		}
		key, ok := token.(string)
		known := false
		for _, name := range allowed {
			known = known || key == name
		}
		if !ok || !known || fields[key] != nil {
			return nil, errors.New("unknown or duplicate request field")
		}
		var value json.RawMessage
		if err := d.Decode(&value); err != nil {
			return nil, err
		}
		if bytes.Equal(value, []byte("null")) {
			return nil, errors.New("null request field")
		}
		fields[key] = value
	}
	if _, err := d.Token(); err != nil {
		return nil, err
	}
	return fields, nil
}

// encoding/json replaces unpaired UTF-16 escapes with U+FFFD. Refuse those
// before decoding, while accepting literal U+FFFD and valid surrogate pairs.
func groupValidJSONSurrogates(raw []byte) bool {
	for i := 0; i < len(raw); i++ {
		if raw[i] != '\\' {
			continue
		}
		i++
		if raw[i] != 'u' {
			continue
		}
		value, _ := strconv.ParseUint(string(raw[i+1:i+5]), 16, 16)
		i += 4
		if value >= 0xDC00 && value <= 0xDFFF {
			return false
		}
		if value >= 0xD800 && value <= 0xDBFF {
			if i+6 >= len(raw) || raw[i+1] != '\\' || raw[i+2] != 'u' {
				return false
			}
			low, err := strconv.ParseUint(string(raw[i+3:i+7]), 16, 16)
			if err != nil || low < 0xDC00 || low > 0xDFFF {
				return false
			}
			i += 6
		}
	}
	return true
}
func (r GroupRequest) validate() error {
	if !dailyPartsV2UUID(r.OperationID) || !dailyPartsV2UUID(r.Task) || !dailyPartsV2UUID(r.Incarnation) || !groupFenceValid(r.Fence) {
		return errors.New("invalid group request identity/fence")
	}
	switch r.Action {
	case "create":
		if r.Schedule == nil || r.Revision != "" {
			return errors.New("create requires only schedule")
		}
		_, err := CompileDailyPartsV2(*r.Schedule, r.Task, r.Incarnation)
		return err
	case "delete":
		if r.Schedule != nil || !validDigest(r.Revision) {
			return errors.New("delete requires only exact revision")
		}
	default:
		return errors.New("unsupported group action")
	}
	return nil
}

type GroupActor struct {
	ServiceUID string
	AccountID  string
	Email      string
}
type GroupBinding struct {
	Actor    GroupActor
	Location Location
}

func (b GroupBinding) validate() error {
	a, err := mail.ParseAddress(b.Actor.Email)
	c, e := Collection(b.Location.CalendarID)
	if err != nil || a.Address != b.Actor.Email || b.Actor.ServiceUID == "" || b.Actor.AccountID == "" || b.Actor.AccountID != b.Location.AccountID || e != nil || c != b.Location.CalendarID || !utf8.ValidString(b.Actor.ServiceUID+b.Actor.AccountID+b.Actor.Email) {
		return errors.New("fresh known actor and canonical location required")
	}
	return nil
}

type groupTarget struct {
	Href     string
	UID      string
	MemberID string
	Body     string
	Planned  *caldav.CalendarObject
	Original *caldav.CalendarObject
	Phase    string
	Receipt  *caldav.CalendarObject
}
type groupOperation struct {
	Request    string
	Binding    GroupBinding
	CapturedAt string
	Payload    string
	PlanHash   string
	Targets    []groupTarget
}

// GroupOperation has no mutable exported fields; serialization is lossless and
// every store transition is a separate encoded value.
type GroupOperation struct{ canonical string }

func groupCanonicalDecode(raw []byte, out any) error {
	if len(raw) > groupStoreLimit-groupEnvelopeBytes || !utf8.Valid(raw) {
		return errors.New("oversized or invalid JSON")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		return err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return errors.New("trailing JSON")
	}
	encoded, err := json.Marshal(out)
	if err != nil || !bytes.Equal(raw, encoded) {
		return errors.New("only canonical lossless JSON is admitted")
	}
	return nil
}
func groupLosslessObject(o caldav.CalendarObject) error {
	if o.Data == nil || o.Data.Name != "VCALENDAR" || dailyPartsValidateTree(o.Data.Component) != nil {
		return errors.New("unreadable calendar object")
	}
	b, err := json.Marshal(o)
	if err != nil {
		return err
	}
	var copy caldav.CalendarObject
	if err = groupCanonicalDecode(b, &copy); err != nil {
		return err
	}
	if !reflect.DeepEqual(o, copy) {
		return errors.New("calendar object JSON is lossy")
	}
	return nil
}
func groupSame(a, b caldav.CalendarObject) bool { return reflect.DeepEqual(a, b) }

func groupCreateTarget(p *DailyPartsV2Plan, m DailyPartsV2Member, title, calendar string, stamp time.Time) (groupTarget, error) {
	uid := fmt.Sprintf("%s%s-%s-%02d", groupPhysicalPrefix, p.Task(), p.Incarnation(), m.Hour)
	href := calendar + uid + ".ics"
	c := ical.NewCalendar()
	c.Props.SetText("VERSION", "2.0")
	c.Props.SetText("PRODID", "-//Chainmail Hassles v2//EN")
	e := ical.NewEvent()
	e.Props.SetText("UID", uid)
	e.Props.SetText("SUMMARY", title)
	desc, err := p.Description(m.MemberID, false)
	if err != nil {
		return groupTarget{}, err
	}
	e.Props.SetText("DESCRIPTION", desc)
	e.Props.SetText("STATUS", "CONFIRMED")
	e.Props.SetText("TRANSP", "TRANSPARENT")
	e.Props.SetDateTime("DTSTAMP", stamp)
	e.Props.SetDateTime("LAST-MODIFIED", stamp)
	for name, value := range map[string]string{"DTSTART": time.UnixMilli(m.AnchorEpochMillis).In(p.start.Location()).Format("20060102T150405"), "DURATION": "PT5M", "RRULE": m.RRule} {
		prop := ical.NewProp(name)
		prop.Value = value
		if name == "DTSTART" {
			prop.Params.Set("TZID", Zone)
		}
		e.Props.Set(prop)
	}
	alarm := ical.NewComponent("VALARM")
	alarm.Props.SetText("ACTION", "DISPLAY")
	alarm.Props.SetText("DESCRIPTION", "Reminder")
	trigger := ical.NewProp("TRIGGER")
	trigger.Value = "PT0S"
	alarm.Props.Set(trigger)
	e.Children = append(e.Children, alarm)
	c.Children = append(c.Children, e.Component)
	body, err := Encode(c)
	if err != nil {
		return groupTarget{}, err
	}
	parsed, err := groupDecodeCalendar(body)
	if err != nil {
		return groupTarget{}, err
	}
	planned := caldav.CalendarObject{Path: href, Data: parsed}
	return groupTarget{Href: href, UID: uid, MemberID: m.MemberID, Body: body, Planned: &planned, Phase: "planned"}, nil
}

// PrepareGroup captures fresh full structural evidence. The executor separately
// probes all planned/new or original hrefs before saving this immutable intent.
func PrepareGroup(r GroupRequest, binding GroupBinding, snapshot GroupSnapshot, now time.Time) (GroupOperation, error) {
	if err := r.validate(); err != nil {
		return GroupOperation{}, err
	}
	if err := binding.validate(); err != nil {
		return GroupOperation{}, err
	}
	for _, o := range snapshot.Objects {
		if err := groupLosslessObject(o); err != nil {
			return GroupOperation{}, err
		}
	}
	model, err := ReadDailyPartsV2Groups(snapshot.Objects, binding.Location, now)
	if err != nil {
		return GroupOperation{}, err
	}
	raw, err := json.Marshal(r)
	if err != nil {
		return GroupOperation{}, err
	}
	stamp := now.UTC().Truncate(time.Second)
	if stamp.Year() < 1970 || stamp.Year() > 2100 {
		return GroupOperation{}, errors.New("unsupported capture timestamp")
	}
	op := groupOperation{Request: string(raw), Binding: binding, CapturedAt: stamp.Format(time.RFC3339)}
	if r.Action == "create" {
		p, err := CompileDailyPartsV2(*r.Schedule, r.Task, r.Incarnation)
		if err != nil {
			return GroupOperation{}, err
		}
		if len(snapshot.Objects) > 2000-len(p.members) {
			return GroupOperation{}, errors.New("calendar capacity exceeded")
		}
		op.Payload = p.Payload()
		op.PlanHash = p.PlanHash()
		for _, m := range p.Members() {
			target, err := groupCreateTarget(p, m, r.Schedule.Title, binding.Location.CalendarID, stamp)
			if err != nil {
				return GroupOperation{}, err
			}
			op.Targets = append(op.Targets, target)
		}
		if err := groupVerifySnapshot(op, snapshot, 0); err != nil {
			return GroupOperation{}, err
		}
	} else {
		var found *DailyPartsGroupModel
		for i := range model.Groups {
			g := &model.Groups[i]
			if g.Task == r.Task {
				found = g
			}
		}
		if found == nil || !found.StructuralReady || found.Revision != r.Revision || found.Plan.Incarnation() != r.Incarnation {
			return GroupOperation{}, ErrConflict
		}
		op.Payload = found.Plan.Payload()
		op.PlanHash = found.Plan.PlanHash()
		for _, m := range found.Members {
			original := m.Original()
			if err := groupLosslessObject(original); err != nil {
				return GroupOperation{}, err
			}
			op.Targets = append(op.Targets, groupTarget{Href: m.Href, UID: m.UID, MemberID: m.MemberID, Original: &original, Phase: "planned"})
		}
	}
	b, err := json.Marshal(op)
	if err != nil {
		return GroupOperation{}, err
	}
	return GroupOperation{canonical: string(b)}, nil
}

func (o GroupOperation) operation() (groupOperation, error) {
	var op groupOperation
	if err := groupCanonicalDecode([]byte(o.canonical), &op); err != nil {
		return op, err
	}
	return op, groupValidateOperation(op)
}

func groupValidateOperation(op groupOperation) error {
	r, err := DecodeGroupRequest([]byte(op.Request))
	if err != nil {
		return err
	}
	if err = groupCanonicalDecode([]byte(op.Request), &r); err != nil {
		return err
	}
	if err = op.Binding.validate(); err != nil {
		return err
	}
	stamp, err := time.Parse(time.RFC3339, op.CapturedAt)
	if err != nil || stamp.UTC().Format(time.RFC3339) != op.CapturedAt || stamp.Year() < 1970 || stamp.Year() > 2100 {
		return errors.New("invalid capture timestamp")
	}
	p, err := ParseDailyPartsV2Payload(op.Payload, op.PlanHash)
	if err != nil || p.Task() != r.Task || p.Incarnation() != r.Incarnation || len(op.Targets) != len(p.members) {
		return errors.New("invalid immutable manifest")
	}
	if r.Action == "create" {
		want, err := CompileDailyPartsV2(*r.Schedule, r.Task, r.Incarnation)
		if err != nil || want.Payload() != p.Payload() {
			return errors.New("request/manifest mismatch")
		}
	}
	originals := []caldav.CalendarObject{}
	seen := map[string]bool{}
	incomplete := false
	for i, t := range op.Targets {
		path, e := Resource(op.Binding.Location.CalendarID, t.Href)
		if e != nil || path != t.Href || seen[path] || t.MemberID != p.members[i].MemberID {
			return errors.New("invalid target roster")
		}
		seen[path] = true
		if incomplete && t.Phase != "planned" {
			return errors.New("non-prefix execution state")
		}
		switch t.Phase {
		case "verified":
		case "planned", "entered", "ack":
			incomplete = true
		default:
			return errors.New("invalid execution state")
		}
		if r.Action == "create" {
			want, e := groupCreateTarget(p, p.members[i], r.Schedule.Title, op.Binding.Location.CalendarID, stamp)
			if e != nil || t.Original != nil || t.Href != want.Href || t.UID != want.UID || t.Body != want.Body || t.Planned == nil || !groupSame(*t.Planned, *want.Planned) {
				return errors.New("changed immutable create target")
			}
			if t.Receipt != nil {
				if t.Phase != "verified" || groupInitialReceipt(t, *t.Receipt) != nil {
					return errors.New("invalid create receipt")
				}
			} else if t.Phase == "verified" {
				return errors.New("missing create receipt")
			}
		} else {
			if t.Planned != nil || t.Body != "" || t.Receipt != nil || t.Original == nil || t.Original.Path != t.Href || !dailyPartsStrongETag(t.Original.ETag) || groupLosslessObject(*t.Original) != nil {
				return errors.New("invalid captured original")
			}
			originals = append(originals, *t.Original)
		}
	}
	if r.Action == "delete" {
		model, e := ReadDailyPartsV2Groups(originals, op.Binding.Location, stamp)
		if e != nil || len(model.Groups) != 1 || !model.Groups[0].StructuralReady || model.Groups[0].Plan.Payload() != op.Payload {
			return errors.New("invalid original structural group")
		}
		for i, m := range model.Groups[0].Members {
			if m.UID != op.Targets[i].UID || m.Href != op.Targets[i].Href {
				return errors.New("changed original identity")
			}
		}
	}
	return nil
}

func groupTaskClaim(p DailyPartsPhysical, task string) bool {
	for _, hint := range p.TaskHints {
		if hint == task {
			return true
		}
	}
	return false
}

// Verify the complete claim set before any further write. Prefix receipts are
// compared raw; initial-create tolerance is never later write authority.
func groupVerifySnapshot(op groupOperation, snapshot GroupSnapshot, prefix int) error {
	r, err := DecodeGroupRequest([]byte(op.Request))
	if err != nil {
		return err
	}
	model, err := ReadDailyPartsV2Groups(snapshot.Objects, op.Binding.Location, time.Time{})
	if err != nil {
		return err
	}
	targets := map[string]int{}
	uids := map[string]bool{}
	for i, t := range op.Targets {
		targets[t.Href] = i
		uids[t.UID] = true
	}
	present := map[string]bool{}
	for i, p := range model.Physical {
		index, known := targets[p.Href]
		if known {
			var expected *caldav.CalendarObject
			if r.Action == "create" && index < prefix {
				expected = op.Targets[index].Receipt
			}
			if r.Action == "delete" && index >= prefix {
				expected = op.Targets[index].Original
			}
			if expected == nil || !groupSame(snapshot.Objects[i], *expected) {
				return ErrConflict
			}
			present[p.Href] = true
		} else {
			if groupTaskClaim(p, r.Task) {
				return ErrConflict
			}
			// Check every primary UID rather than the convenience last-UID field.
			if groupObjectHasUID(snapshot.Objects[i], uids) {
				return ErrConflict
			}
		}
	}
	for i, t := range op.Targets {
		required := (r.Action == "create" && i < prefix) || (r.Action == "delete" && i >= prefix)
		if required && !present[t.Href] {
			return ErrConflict
		}
	}
	if r.Action == "create" && len(snapshot.Objects) > 2000-(len(op.Targets)-prefix) {
		return errors.New("calendar capacity exceeded")
	}
	return nil
}
func groupObjectHasUID(o caldav.CalendarObject, uids map[string]bool) bool {
	if o.Data == nil {
		return true
	}
	found := false
	var walk func(*ical.Component)
	walk = func(c *ical.Component) {
		if groupPrimary(c.Name) {
			if len(c.Props["UID"]) == 0 {
				found = true
			}
			for _, p := range c.Props["UID"] {
				values, err := p.TextList()
				if err != nil || len(values) != 1 || values[0] == "" {
					found = true
				}
				for _, uid := range values {
					found = found || uids[uid]
				}
			}
		}
		for _, child := range c.Children {
			walk(child)
		}
	}
	walk(o.Data.Component)
	return found
}
