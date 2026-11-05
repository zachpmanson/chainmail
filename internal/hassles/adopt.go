package hassles

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	"github.com/emersion/go-ical"
	"github.com/emersion/go-webdav/caldav"
)

// Before/After are semantic iCalendar exports, not byte-exact HTTP backups.
type Adoption struct {
	ResourceID  string `json:"resourceId"`
	Revision    string `json:"revision"`
	TaskID      string `json:"taskId"`
	Before      string `json:"before"`
	After       string `json:"after"`
	Fingerprint string `json:"fingerprint"`
}

func validDigest(value string) bool {
	b, err := hex.DecodeString(value)
	return err == nil && len(b) == sha256.Size
}

// Presence-aware dryRun prevents adoption flags being silently ignored by delete/create.
func ValidateAdoptionFields(in Mutation) error {
	if in.Action != "adopt" {
		if in.AdoptID != nil || in.AdoptHash != nil || in.DryRun != nil {
			return errors.New("adoptId, adoptHash and dryRun are only valid for adopt")
		}
		return nil
	}
	if in.AdoptID == nil || !ValidUUID(*in.AdoptID) || in.DryRun == nil {
		return errors.New("adopt requires a valid adoptId UUID and explicit dryRun")
	}
	if !strings.HasPrefix(in.ID, "resource:") || !validDigest(strings.TrimPrefix(in.ID, "resource:")) || !validDigest(in.Revision) {
		return errors.New("adopt requires the original resource ID and revision")
	}
	if in.Schedule != (Schedule{}) {
		return errors.New("adopt cannot change a schedule")
	}
	if in.AdoptHash != nil && !validDigest(*in.AdoptHash) {
		return errors.New("invalid adoption preview fingerprint")
	}
	if !*in.DryRun && in.AdoptHash == nil {
		return errors.New("adopt commit requires adoptHash from a saved preview")
	}
	return nil
}

func cloneComponent(c *ical.Component) *ical.Component {
	copy := &ical.Component{Name: c.Name, Props: make(ical.Props)}
	for name, props := range c.Props {
		for _, p := range props {
			params := make(ical.Params)
			for key, values := range p.Params {
				params[key] = append([]string(nil), values...)
			}
			copy.Props[name] = append(copy.Props[name], ical.Prop{Name: p.Name, Value: p.Value, Params: params})
		}
	}
	for _, child := range c.Children {
		copy.Children = append(copy.Children, cloneComponent(child))
	}
	return copy
}

func cloneCalendar(c *ical.Calendar) *ical.Calendar {
	return &ical.Calendar{Component: cloneComponent(c.Component)}
}

func adoptionMarker(id string) string { return "[hassle:v1 task=" + id + "]" }

// Text() alone silently truncates unescaped comma-separated values.
func adoptionText(p *ical.Prop) (string, error) {
	values, err := p.TextList()
	if err != nil || len(values) != 1 {
		return "", errors.New("unreadable or ambiguous description/identity text")
	}
	return values[0], nil
}

// Preserve original escaping and parameters. A marker-only property was newly
// created; an existing empty property gets a newline, retaining that distinction.
func stripAdoptionMarker(p *ical.Prop, id string) (created, stripped bool) {
	tag := adoptionMarker(id)
	if p.Value == tag && len(p.Params) == 0 {
		return true, true
	}
	if strings.HasSuffix(p.Value, `\n`+tag) {
		p.Value = strings.TrimSuffix(p.Value, `\n`+tag)
		return false, true
	}
	return false, false
}

// Retains all content except our added marker and provider-maintained revision fields.
func adoptionFingerprint(c *ical.Calendar, id string) (string, error) {
	return adoptionContentHash(c, id, true)
}

func adoptionContentHash(c *ical.Calendar, id string, ignoreRevision bool) (string, error) {
	copy := cloneCalendar(c)
	for _, e := range copy.Events() {
		if p := e.Props.Get("DESCRIPTION"); p != nil {
			created, _ := stripAdoptionMarker(p, id)
			text, err := adoptionText(p)
			if err != nil {
				return "", err
			}
			if !created && strings.Contains(strings.ToLower(text), "[hassle:") {
				return "", errors.New("ambiguous pre-existing hassle metadata")
			}
			if created {
				e.Props.Del("DESCRIPTION")
			}
		}
		if ignoreRevision {
			for _, name := range []string{"DTSTAMP", "LAST-MODIFIED", "SEQUENCE"} {
				e.Props.Del(name)
			}
		}
	}
	body, err := json.Marshal(copy)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:]), nil
}

func adoptReminder(ctx context.Context, b Backend, in Mutation, objects []caldav.CalendarObject, tasks []Task) (Result, error) {
	result := Result{}
	id := strings.ToLower(*in.AdoptID)
	// Adoption changes the logical task ID. Always resolve retries by the
	// original resource path digest, never by title or the requested new UUID.
	var target *caldav.CalendarObject
	for i := range objects {
		if "resource:"+digest(objects[i].Path) == in.ID {
			if target != nil {
				return result, ErrConflict
			}
			target = &objects[i]
		}
	}
	if target == nil {
		return result, ErrConflict
	}
	var task *Task
	for i := range tasks {
		for _, member := range tasks[i].members {
			if member.Path == target.Path {
				task = &tasks[i]
			}
		}
	}
	if task == nil || task.Resources != 1 {
		return result, ErrConflict
	}
	if !task.Writable {
		return result, errors.New("adoption requires one existing docket-owned resource with a strong ETag and no participants")
	}
	o := *target
	if o.ETag == "*" {
		return result, errors.New("adoption requires an existing resource ETag, never a create condition")
	}
	if in.Location.AccountID == "" {
		return result, errors.New("adoption requires a selected account")
	}
	if _, err := Resource(in.Location.CalendarID, o.Path); err != nil {
		return result, err
	}
	already := task.ID == "task:"+id
	if task.ID != in.ID && !already {
		return result, ErrConflict
	}
	if !already && task.Revision != in.Revision {
		return result, ErrConflict
	}
	if already && in.AdoptHash == nil {
		return result, ErrConflict
	}
	masters, uid := 0, ""
	exceptions := map[string]bool{}
	for _, e := range o.Data.Events() {
		currentUID := ""
		var err error
		if p := e.Props.Get("UID"); p != nil {
			currentUID, err = adoptionText(p)
		}
		if err != nil || currentUID == "" || len(e.Props["UID"]) != 1 || (uid != "" && uid != currentUID) || len(e.Props["RECURRENCE-ID"]) > 1 || len(e.Props["DESCRIPTION"]) > 1 {
			return result, errors.New("ambiguous event identity or description")
		}
		uid = currentUID
		if e.Props.Get("RECURRENCE-ID") == nil {
			masters++
		} else {
			p := e.Props.Get("RECURRENCE-ID")
			if p.Value == "" || exceptions[p.Value] {
				return result, errors.New("ambiguous exception identity")
			}
			exceptions[p.Value] = true
		}
		desc := ""
		if p := e.Props.Get("DESCRIPTION"); p != nil {
			desc, err = adoptionText(p)
			if err != nil {
				return result, err
			}
		}
		if already {
			marked, ok := marker(desc)
			p := e.Props.Get("DESCRIPTION")
			suffix := false
			if p != nil {
				copy := *p
				_, suffix = stripAdoptionMarker(&copy, id)
			}
			if !ok || marked != id || !suffix {
				return result, ErrConflict
			}
		} else if strings.Contains(strings.ToLower(desc), "[hassle:") {
			return result, errors.New("pre-existing hassle metadata needs manual review")
		}
	}
	if masters != 1 {
		return result, errors.New("adoption requires one unambiguous series master")
	}
	// Include canceled/ambiguous resources, which logical Inventory may hide.
	for _, other := range objects {
		if other.Path == o.Path {
			continue
		}
		if other.Data == nil {
			return result, errors.New("cannot exclude adoption collisions in unreadable resource")
		}
		for _, e := range other.Data.Events() {
			for _, p := range e.Props["UID"] {
				values, err := p.TextList()
				if err != nil {
					return result, errors.New("cannot exclude duplicate UID in unreadable resource")
				}
				for _, otherUID := range values {
					if otherUID == uid {
						return result, ErrConflict
					}
				}
			}
			for _, p := range e.Props["DESCRIPTION"] {
				// UUIDs are cheap: refuse reuse even inside malformed metadata/text.
				if strings.Contains(strings.ToLower(p.Value), id) {
					return result, ErrConflict
				}
			}
		}
	}
	fingerprint, err := adoptionFingerprint(o.Data, id)
	if err != nil {
		return result, err
	}
	if in.AdoptHash != nil && strings.ToLower(*in.AdoptHash) != fingerprint {
		return result, ErrConflict
	}
	before, err := Encode(o.Data)
	if err != nil {
		return result, err
	}
	result.Adoption = &Adoption{ResourceID: in.ID, Revision: task.Revision, TaskID: "task:" + id, Before: before, After: before, Fingerprint: fingerprint}
	if already {
		result.Message = "Already adopted"
		return result, nil
	}
	copy := cloneCalendar(o.Data)
	for _, e := range copy.Events() {
		p := e.Props.Get("DESCRIPTION")
		if p == nil {
			p = ical.NewProp("DESCRIPTION")
			p.Value = adoptionMarker(id)
			e.Props.Set(p)
		} else {
			p.Value += `\n` + adoptionMarker(id)
		}
	}
	// Preparation may change only DESCRIPTION, not even provider revision fields.
	beforeHash, err := adoptionContentHash(o.Data, id, false)
	if err != nil {
		return Result{}, err
	}
	if afterHash, err := adoptionContentHash(copy, id, false); err != nil || afterHash != beforeHash {
		return Result{}, errors.New("adoption changed protected reminder content")
	}
	result.Adoption.After, err = Encode(copy)
	if err != nil {
		return Result{}, err
	}
	if *in.DryRun {
		result.Message = "Preview: add hassle ID"
		return result, nil
	}
	if err := b.Write(ctx, in.Location.CalendarID, o.Path, o.ETag, result.Adoption.After); err != nil {
		return Result{}, err
	}
	result.Message = "Adopted existing reminder"
	return result, nil
}
