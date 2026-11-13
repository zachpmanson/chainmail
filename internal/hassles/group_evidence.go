package hassles

import (
	"strings"

	"github.com/emersion/go-ical"
)

const groupPhysicalPrefix = "hassle-parts-v2-"

func groupPrimary(name string) bool {
	switch name {
	case "VEVENT", "VTODO", "VJOURNAL", "VFREEBUSY":
		return true
	}
	return false
}

// The caller has already bounded the tree. Auxiliary components contribute
// denial evidence but do not participate in the primary calendar UID namespace.
func groupEvidence(c *ical.Component, counts map[string]int, uncertain *bool, physical *DailyPartsPhysical) []string {
	texts := []string{}
	identifiers := []string{physical.Href}
	metadata := false
	var walk func(*ical.Component)
	walk = func(c *ical.Component) {
		if groupPrimary(c.Name) && len(c.Props["UID"]) == 0 {
			*uncertain = true
		}
		for _, name := range []string{"UID", "DESCRIPTION"} {
			for _, p := range c.Props[name] {
				values, err := p.TextList()
				if name == "UID" && groupPrimary(c.Name) {
					if err != nil || len(values) != 1 || values[0] == "" {
						*uncertain = true
					}
					for _, uid := range values {
						counts[uid]++
						physical.UID = uid
					}
				}
				if err != nil {
					values = []string{p.Value}
				}
				texts = append(texts, values...)
				if name == "UID" {
					identifiers = append(identifiers, values...)
				}
				if name == "DESCRIPTION" {
					for _, value := range values {
						if _, err := ParseDailyPartsV2Description(value); err == nil {
							metadata = true
						}
					}
				}
			}
		}
		for _, child := range c.Children {
			walk(child)
		}
	}
	if c != nil {
		walk(c)
	}
	// Reserved physical names are denial hints, never positive provenance.
	for _, text := range identifiers {
		if metadata {
			break
		}
		folded := dailyPartsASCIIFold(text)
		if i := strings.Index(folded, groupPhysicalPrefix); i >= 0 {
			physical.Reserved = true
			tail := folded[i+len(groupPhysicalPrefix):]
			if len(tail) >= 36 && dailyPartsV2UUID(tail[:36]) {
				texts = append(texts, "task="+tail[:36])
			}
		}
	}
	return texts
}
