package hassles

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/emersion/go-ical"
	"github.com/emersion/go-webdav/caldav"
)

const dailyPartsTimingDeferred = "DAILY-parts v2 timing deferred: no physical instance witness available"

const (
	dailyPartsComponentDepthLimit = 64
	dailyPartsComponentNodeLimit  = 4096
	dailyPartsResourceHintLimit   = 24
	dailyPartsClaimEdgeLimit      = 2000
)

// Bound every traversal before JSON, raw cloning or legacy timezone delegation.
// Count occurrences, not unique pointers: an acyclic shared subtree is copied.
func dailyPartsValidateTree(root *ical.Component) error {
	active := map[*ical.Component]bool{}
	nodes := 0
	var visit func(*ical.Component, int) error
	visit = func(c *ical.Component, depth int) error {
		if c == nil || active[c] {
			return errors.New("unreadable or cyclic calendar component")
		}
		if depth > dailyPartsComponentDepthLimit || nodes >= dailyPartsComponentNodeLimit {
			return errors.New("calendar component tree exceeds safety limit")
		}
		nodes++
		if len(c.Children) > dailyPartsComponentNodeLimit-nodes {
			return errors.New("calendar component tree exceeds safety limit")
		}
		active[c] = true
		for _, child := range c.Children {
			if err := visit(child, depth+1); err != nil {
				return err
			}
		}
		delete(active, c)
		return nil
	}
	return visit(root, 1)
}

// DailyPartsPhysical is an immutable parsed-tree capture, not HTTP wire bytes.
// Raw includes every CalendarObject field and all ordered properties/children.
// A logical MemberID is never an actual href or ICS UID.
type DailyPartsPhysical struct {
	Href                  string
	ETag                  string
	UID                   string
	MemberID              string
	RawRule               string
	Exclusions            []time.Time // Decoded only for a validated completion profile; Raw remains authoritative.
	HasCompletionMetadata bool
	Raw                   string
	Fingerprint           string
	Reserved              bool
	TaskHints             []string
	original              caldav.CalendarObject
}

// Original returns a deep raw copy, including nil/empty distinctions and text
// bytes which JSON cannot represent (for example invalid UTF-8 in denied input).
// The JSON fingerprint alone is never later admission/mutation authority.
func (p DailyPartsPhysical) Original() caldav.CalendarObject {
	return dailyPartsCloneObject(p.original)
}

func dailyPartsCloneObject(o caldav.CalendarObject) caldav.CalendarObject {
	if o.Data != nil {
		o.Data = &ical.Calendar{Component: dailyPartsCloneComponent(o.Data.Component)}
	}
	return o
}

func dailyPartsCloneComponent(c *ical.Component) *ical.Component {
	if c == nil {
		return nil
	}
	out := &ical.Component{Name: c.Name}
	if c.Props != nil {
		out.Props = make(ical.Props, len(c.Props))
		for name, props := range c.Props {
			var copies []ical.Prop
			if props != nil {
				copies = make([]ical.Prop, len(props))
			}
			for i, prop := range props {
				copies[i] = prop
				if prop.Params != nil {
					copies[i].Params = make(ical.Params, len(prop.Params))
					for key, values := range prop.Params {
						if values == nil {
							copies[i].Params[key] = nil
						} else {
							copies[i].Params[key] = append([]string{}, values...)
						}
					}
				}
			}
			out.Props[name] = copies
		}
	}
	if c.Children != nil {
		out.Children = make([]*ical.Component, len(c.Children))
		for i, child := range c.Children {
			out.Children[i] = dailyPartsCloneComponent(child)
		}
	}
	return out
}

// DailyPartsGroupModel carries structural evidence only. Neither metadata nor
// StructuralReady grants account/owner/ACL authorization or mutation permission.
// No instance operation can execute from this reader's deferred timing result.
type DailyPartsGroupModel struct {
	Task                 string
	Location             Location
	Plan                 *DailyPartsV2Plan
	Title                string
	Members              []DailyPartsPhysical
	Revision             string
	StructuralReady      bool
	Restrictions         []string
	Timing               Timing
	ContextAuthorization string // Always unavailable; not supplied by object/Location evidence.
}

// DailyPartsGroupInventoryModel is separate from legacy Inventory/Task. Future
// integration must run legacy authority on the FULL snapshot before suppressing
// handled DISPLAY resources; removing evidence first could promote a v1 sibling.
type DailyPartsGroupInventoryModel struct {
	Location         Location
	Physical         []DailyPartsPhysical
	Groups           []DailyPartsGroupModel
	HandledHrefs     []string
	SnapshotRevision string
}

var dailyPartsTaskHint = regexp.MustCompile(`(?i)task=([0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12})(?:\]|\s|$)`)

func dailyPartsASCIIFold(text string) string {
	bytes := []byte(text)
	for i, ch := range bytes {
		if ch >= 'A' && ch <= 'Z' {
			bytes[i] = ch + ('a' - 'A')
		}
	}
	return string(bytes)
}

func dailyPartsReserved(desc string) bool {
	// Genuine legacy-only markers remain the legacy reader's responsibility.
	// Case-folding locates denial evidence only, never recognizes an alias.
	// Fold once: repeated valid legacy markers must not recopy every suffix.
	folded := dailyPartsASCIIFold(desc)
	for _, prefix := range []string{"[hassle:", "[chainmail:hourly:", "[chainmail:completed-instances:"} {
		for offset := 0; offset < len(desc); {
			i := strings.Index(folded[offset:], prefix)
			if i < 0 {
				break
			}
			start := offset + i
			rest := desc[start:]
			legacy := false
			switch prefix {
			case "[hassle:":
				match := markerPattern.FindStringSubmatchIndex(rest)
				legacy = len(match) == 4 && match[0] == 0 && ValidUUID(rest[match[2]:match[3]])
			case "[chainmail:hourly:":
				for _, interval := range hourlyIntervals {
					legacy = legacy || strings.HasPrefix(rest, hourlyMarker(interval))
				}
			case "[chainmail:completed-instances:":
				legacy = strings.HasPrefix(rest, completionMarker)
			}
			if !legacy {
				return true
			}
			offset = start + len(prefix)
		}
	}
	return strings.Contains(folded, "[chainmail:parts:")
}

// Hints can only join/deny groups, never validate one. Even a lone parts line can
// recover a task hint without trusting its payload, roster or hash.
func dailyPartsHints(desc string) []string {
	texts := []string{desc}
	const prefix = "[hassle:parts:v2 data="
	for rest := desc; ; {
		i := strings.Index(rest, prefix)
		if i < 0 {
			break
		}
		rest = rest[i+len(prefix):]
		end := strings.IndexByte(rest, ']')
		if end < 0 {
			break
		}
		if end <= base64.RawURLEncoding.EncodedLen(dailyPartsV2PayloadLimit) {
			if b, err := base64.RawURLEncoding.DecodeString(rest[:end]); err == nil {
				texts = append(texts, string(b))
			}
		}
		rest = rest[end+1:]
	}
	seen := map[string]bool{}
	for _, text := range texts {
		// The actual legacy reader accepts case-insensitive UUID values. Recover
		// its canonical identity ONLY for collision denial; v2 stays strict.
		if legacyTask, ok := marker(text); ok {
			seen[legacyTask] = true
		}
		for _, match := range dailyPartsTaskHint.FindAllStringSubmatch(text, -1) {
			if dailyPartsV2UUID(match[1]) {
				seen[match[1]] = true
			}
		}
	}
	out := make([]string, 0, len(seen))
	for hint := range seen {
		out = append(out, hint)
	}
	sort.Strings(out)
	return out
}

func dailyPartsText(props ical.Props, name string) (string, bool) {
	if len(props[name]) != 1 {
		return "", false
	}
	p := props.Get(name)
	for key, values := range p.Params {
		if len(values) != 1 {
			return "", false
		}
		switch key {
		case "VALUE":
			if values[0] != "TEXT" {
				return "", false
			}
		case "LANGUAGE":
			if values[0] == "" {
				return "", false
			}
		default:
			return "", false
		}
	}
	values, err := p.TextList()
	returnValue := ""
	if err == nil && len(values) == 1 {
		returnValue = values[0]
	}
	return returnValue, err == nil && len(values) == 1
}

func dailyPartsDateParams(p *ical.Prop, local bool) bool {
	if p == nil {
		return false
	}
	for key, values := range p.Params {
		if len(values) != 1 {
			return false
		}
		switch key {
		case "VALUE":
			if values[0] != "DATE-TIME" {
				return false
			}
		case "TZID":
			if !local || values[0] != Zone {
				return false
			}
		default:
			return false
		}
	}
	return !local || p.Params.Get("TZID") == Zone
}

// Only property order is irrelevant for read comparison. Missing defaults,
// alternate values and duplicates are NOT filled or normalized into authority.
func dailyPartsRule(p *ical.Prop, expected string) bool {
	if p == nil || len(p.Params) != 0 || len(p.Value) > 1024 {
		return false
	}
	want := map[string]string{}
	for _, part := range strings.Split(expected, ";") {
		kv := strings.SplitN(part, "=", 2)
		want[kv[0]] = kv[1]
	}
	for _, part := range strings.Split(p.Value, ";") {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			return false
		}
		value, exists := want[kv[0]]
		if !exists || value != kv[1] {
			return false
		}
		delete(want, kv[0])
	}
	return len(want) == 0
}

func dailyPartsDuration(e ical.Event, start time.Time) bool {
	if len(e.Props["DURATION"])+len(e.Props["DTEND"]) != 1 {
		return false
	}
	if p := e.Props.Get("DURATION"); p != nil {
		if len(p.Params) > 0 {
			return false
		}
		return p.Value == "PT5M" || p.Value == "PT300S" || p.Value == "P300S"
	}
	p := e.Props.Get("DTEND")
	local := p != nil && p.Params.Get("TZID") == Zone
	if !dailyPartsDateParams(p, local) {
		return false
	}
	var end time.Time
	var err error
	if local {
		end, err = melbourneStart(p)
	} else {
		end, err = time.Parse("20060102T150405Z", p.Value)
		if err == nil && end.UTC().Format("20060102T150405Z") != p.Value {
			return false
		}
	}
	return err == nil && end.UnixMilli() == start.UnixMilli()+300000
}

func dailyPartsAlarm(e ical.Event) bool {
	if len(e.Children) != 1 {
		return false
	}
	a := e.Children[0]
	if a == nil || a.Name != "VALARM" || len(a.Children) != 0 {
		return false
	}
	if !profileProps(a, map[string]string{"ACTION": "DISPLAY", "DESCRIPTION": "Reminder", "TRIGGER": "PT0S"}, nil) {
		return false
	}
	return true
}

func dailyPartsExclusions(e ical.Event, meta *DailyPartsV2Metadata) bool {
	if len(e.Props["EXDATE"]) == 0 {
		return !meta.HasCompletionMetadata
	}
	if !meta.HasCompletionMetadata || !validCompletionDates(e) {
		return false
	}
	dates, err := recurrenceDates(e.Props["EXDATE"])
	if err != nil || len(dates) == 0 || len(dates) > maxCompletions {
		return false
	}
	for _, at := range dates {
		if !meta.Plan.SupportsCompletion(meta.MemberID, at) {
			return false
		}
	}
	return true
}

func dailyPartsAdmit(o caldav.CalendarObject, loc Location, uidCounts map[string]int, uidUncertain bool) (*DailyPartsV2Metadata, string, string, string) {
	deny := "Unsupported or conflicting physical DAILY-parts member"
	path, err := Resource(loc.CalendarID, o.Path)
	if err != nil || path != o.Path || !dailyPartsStrongETag(o.ETag) || o.Data == nil || o.Data.Component == nil || o.Data.Name != "VCALENDAR" {
		return nil, "", "", deny
	}
	if len(o.Data.Props["METHOD"]) != 0 {
		return nil, "", "", deny
	}
	for _, child := range o.Data.Children {
		if child == nil || (child.Name != "VEVENT" && child.Name != "VTIMEZONE") {
			return nil, "", "", deny
		}
	}
	events := o.Data.Events()
	if len(events) != 1 {
		return nil, "", "", deny
	}
	e := events[0]
	for _, name := range []string{"RECURRENCE-ID", "ATTENDEE", "ORGANIZER", "RDATE", "EXRULE"} {
		if len(e.Props[name]) != 0 {
			return nil, "", "", deny
		}
	}
	uid, ok := dailyPartsText(e.Props, "UID")
	if !ok || uid == "" || uidUncertain || uidCounts[uid] != 1 {
		return nil, "", "", deny
	}
	title, ok := dailyPartsText(e.Props, "SUMMARY")
	if !ok || !utf8.ValidString(title) || strings.TrimSpace(title) == "" || len(title) > 500 || strings.ContainsAny(title, "\r\n") {
		return nil, "", uid, deny
	}
	desc, ok := dailyPartsText(e.Props, "DESCRIPTION")
	if !ok {
		return nil, title, uid, deny
	}
	meta, err := ParseDailyPartsV2Description(desc)
	if err != nil {
		return nil, title, uid, deny
	}
	status := e.Props["STATUS"]
	if len(status) > 1 {
		return meta, title, uid, deny
	}
	if len(status) == 1 && (len(status[0].Params) != 0 || (status[0].Value != "CONFIRMED" && status[0].Value != "TENTATIVE")) {
		return meta, title, uid, deny
	}
	if len(e.Props["TRANSP"]) > 1 {
		return meta, title, uid, deny
	}
	if p := e.Props.Get("TRANSP"); p != nil && (len(p.Params) != 0 || p.Value != "TRANSPARENT") {
		return meta, title, uid, deny
	}
	if len(e.Props["DTSTART"]) != 1 || !dailyPartsDateParams(e.Props.Get("DTSTART"), true) || len(e.Props["RRULE"]) != 1 {
		return meta, title, uid, deny
	}
	start, err := melbourneStart(e.Props.Get("DTSTART"))
	if err != nil || start.Second() != 0 || !dailyPartsDuration(e, start) || !dailyPartsAlarm(e) || !dailyPartsExclusions(e, meta) {
		return meta, title, uid, deny
	}
	logic := meta.Plan.Logic()
	if scheduleTimezoneRestriction(o.Data, &Schedule{Start: logic.Start, Until: logic.Until}) != "" {
		return meta, title, uid, deny
	}
	for _, member := range meta.Plan.Members() {
		if member.MemberID == meta.MemberID && member.AnchorEpochMillis == start.UnixMilli() && dailyPartsRule(e.Props.Get("RRULE"), member.RRule) {
			return meta, title, uid, ""
		}
	}
	return meta, title, uid, deny
}

func dailyPartsStrongETag(tag string) bool {
	if len(tag) < 2 || tag[0] != '"' || tag[len(tag)-1] != '"' {
		return false
	}
	for _, ch := range tag[1 : len(tag)-1] {
		if ch < 0x21 || ch == '"' || ch == 0x7f {
			return false
		}
	}
	return true
}

// ReadDailyPartsV2Groups is pure. Location is inert captured data used ONLY for
// path membership, not evidence of a selected/usable account, owner or ACL.
// now is intentionally unused: no recurrence evaluator or instance is invented.
// Snapshot bounds cannot establish producer query completeness or the client's
// 16 MiB HTTP response bound; those remain separate fresh-admission evidence.
func ReadDailyPartsV2Groups(objects []caldav.CalendarObject, location Location, now time.Time) (DailyPartsGroupInventoryModel, error) {
	out := DailyPartsGroupInventoryModel{Location: location}
	_ = now
	collection, err := Collection(location.CalendarID)
	if err != nil || collection != location.CalendarID {
		return out, errors.New("canonical collection required")
	}
	if len(objects) > 2000 {
		return out, errors.New("calendar exceeds 2000-resource safety limit")
	}
	paths := map[string]bool{}
	uidCounts := map[string]int{}
	uidUncertain := false
	bucket := map[string][]int{}
	claimEdges := 0
	for _, o := range objects {
		if o.Data != nil {
			if err := dailyPartsValidateTree(o.Data.Component); err != nil {
				return DailyPartsGroupInventoryModel{Location: location}, err
			}
		}
		if paths[o.Path] {
			return out, errors.New("duplicate calendar resource href")
		}
		paths[o.Path] = true
		raw, err := json.Marshal(o)
		if err != nil {
			return out, err
		}
		if o.Data != nil {
			data, err := json.Marshal(o.Data)
			if err != nil || len(data) > 1<<20 || len(o.Data.Events()) > 1000 {
				return out, errors.New("calendar resource exceeds safety limit")
			}
		}
		physical := DailyPartsPhysical{Href: o.Path, ETag: o.ETag, Raw: string(raw), Fingerprint: digest(string(raw)), original: dailyPartsCloneObject(o)}
		texts := []string{}
		if o.Data == nil || o.Data.Component == nil {
			uidUncertain = true
		} else {
			for _, e := range o.Data.Events() {
				if len(e.Props["UID"]) == 0 {
					uidUncertain = true
				}
				for _, p := range e.Props["UID"] {
					values, err := p.TextList()
					if err != nil || len(values) != 1 || values[0] == "" {
						uidUncertain = true
					}
					for _, uid := range values {
						uidCounts[uid]++
						physical.UID = uid
					}
				}
				for _, p := range e.Props["DESCRIPTION"] {
					values, err := p.TextList()
					if err != nil {
						values = []string{p.Value}
					}
					texts = append(texts, values...)
				}
			}
		}
		for _, text := range texts {
			physical.Reserved = physical.Reserved || dailyPartsReserved(text)
		}
		physical.TaskHints = dailyPartsHints(strings.Join(texts, "\n"))
		if len(physical.TaskHints) > dailyPartsResourceHintLimit {
			return DailyPartsGroupInventoryModel{Location: location}, errors.New("calendar resource exceeds task-hint safety limit")
		}
		// Count even unreserved claims: they may be legacy collision evidence.
		// A reserved resource without hints still creates one denial bucket.
		edges := len(physical.TaskHints)
		if physical.Reserved && edges == 0 {
			edges = 1
		}
		claimEdges += edges
		if claimEdges > dailyPartsClaimEdgeLimit {
			return DailyPartsGroupInventoryModel{Location: location}, errors.New("calendar snapshot exceeds claim-edge safety limit")
		}
		out.Physical = append(out.Physical, physical)
	}
	// Capture the entire inventory revision before any cancellation/display filter.
	full := append([]DailyPartsPhysical(nil), out.Physical...)
	sort.Slice(full, func(i, j int) bool { return full[i].Href < full[j].Href })
	rawFull, err := json.Marshal(struct {
		Location Location
		Physical []DailyPartsPhysical
	}{location, full})
	if err != nil {
		return out, err
	}
	out.SnapshotRevision = digest(string(rawFull))
	for i, physical := range out.Physical {
		if !physical.Reserved {
			continue
		}
		out.HandledHrefs = append(out.HandledHrefs, physical.Href)
		hints := physical.TaskHints
		if len(hints) == 0 {
			hints = []string{"resource:" + digest(physical.Href)}
		}
		for _, task := range hints {
			bucket[task] = append(bucket[task], i)
		}
	}
	// A genuine v1 sibling sharing a recovered task must remain collision
	// evidence, even though it is not itself a handled v2 display resource.
	for i, physical := range out.Physical {
		if physical.Reserved {
			continue
		}
		for _, task := range physical.TaskHints {
			if _, exists := bucket[task]; exists {
				bucket[task] = append(bucket[task], i)
			}
		}
	}
	keys := make([]string, 0, len(bucket))
	for key := range bucket {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	sort.Strings(out.HandledHrefs)
	for _, task := range keys {
		group := DailyPartsGroupModel{Task: task, Location: location, StructuralReady: true, ContextAuthorization: "unavailable", Timing: Timing{State: "unknown", Note: dailyPartsTimingDeferred}}
		seen := map[string]bool{}
		deny := func(reason string) {
			group.StructuralReady = false
			for _, old := range group.Restrictions {
				if old == reason {
					return
				}
			}
			group.Restrictions = append(group.Restrictions, reason)
		}
		for _, i := range bucket[task] {
			physical := out.Physical[i]
			meta, title, uid, why := dailyPartsAdmit(objects[i], location, uidCounts, uidUncertain)
			if why != "" {
				deny(why)
			}
			if len(physical.TaskHints) != 1 || physical.TaskHints[0] != task {
				deny("Ambiguous or missing reserved task identity")
			}
			if meta != nil {
				physical.MemberID = meta.MemberID
				physical.UID = uid
				physical.HasCompletionMetadata = meta.HasCompletionMetadata
				events := objects[i].Data.Events()
				if len(events) == 1 {
					if p := events[0].Props.Get("RRULE"); p != nil {
						physical.RawRule = p.Value
					}
					if dailyPartsExclusions(events[0], meta) {
						physical.Exclusions, _ = recurrenceDates(events[0].Props["EXDATE"])
					}
				}
				if meta.Plan.Task() != task {
					deny("Conflicting task identity")
				}
				if group.Plan == nil {
					group.Plan = meta.Plan
					group.Title = title
				} else if group.Plan.Payload() != meta.Plan.Payload() || group.Plan.PlanHash() != meta.Plan.PlanHash() || group.Title != title {
					deny("Conflicting plan, incarnation or title")
				}
				if seen[meta.MemberID] {
					deny("Duplicate logical role")
				}
				seen[meta.MemberID] = true
			}
			// TaskHints slices are copied so captures never alias each other/input.
			physical.TaskHints = append([]string(nil), physical.TaskHints...)
			group.Members = append(group.Members, physical)
		}
		if group.Plan == nil {
			deny("No validated full plan")
		} else {
			if len(group.Members) != len(group.Plan.Members()) {
				deny("Incomplete or extra physical roster")
			}
			for _, member := range group.Plan.Members() {
				if !seen[member.MemberID] {
					deny("Missing expected logical role")
				}
			}
		}
		order := map[string]int{}
		if group.Plan != nil {
			for i, member := range group.Plan.Members() {
				order[member.MemberID] = i + 1
			}
		}
		sort.Slice(group.Members, func(i, j int) bool {
			a, b := group.Members[i], group.Members[j]
			if order[a.MemberID] != order[b.MemberID] {
				return order[a.MemberID] < order[b.MemberID]
			}
			return a.Href < b.Href
		})
		revision, err := json.Marshal(struct {
			Snapshot string
			Task     string
			Members  []DailyPartsPhysical
		}{out.SnapshotRevision, task, group.Members})
		if err != nil {
			return out, err
		}
		group.Revision = digest(string(revision))
		out.Groups = append(out.Groups, group)
	}
	return out, nil
}
