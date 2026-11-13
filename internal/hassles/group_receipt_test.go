package hassles

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/emersion/go-ical"
	"github.com/emersion/go-webdav/caldav"
)

func groupReceiptFixture(t *testing.T) (groupTarget, caldav.CalendarObject) {
	t.Helper()
	s := groupTestStore(t)
	r := groupTestRequest(t, s, dailyV2Schedule("2027-02-12T09:17", 24, false, ""))
	prepared, err := PrepareGroup(r, groupTestBinding, GroupSnapshot{}, now)
	if err != nil {
		t.Fatal(err)
	}
	op, err := prepared.operation()
	if err != nil {
		t.Fatal(err)
	}
	target := op.Targets[0]
	actual := dailyPartsCloneObject(*target.Planned)
	actual.ETag = `"actual"`
	return target, actual
}
func TestGroupInitialReceiptManagedFieldAllowlist(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*caldav.CalendarObject)
	}{
		{"unchanged", func(*caldav.CalendarObject) {}},
		{"prodid", func(o *caldav.CalendarObject) { o.Data.Props.SetText("PRODID", strings.Repeat("x", 512)) }},
		{"calscale", func(o *caldav.CalendarObject) { o.Data.Props.SetText("CALSCALE", "GREGORIAN") }},
		{"created", func(o *caldav.CalendarObject) { groupSetRaw(o.Data.Events()[0].Props, "CREATED", "20280212T000000Z") }},
		{"dtstamp", func(o *caldav.CalendarObject) { groupSetRaw(o.Data.Events()[0].Props, "DTSTAMP", "19700101T000000Z") }},
		{"modified", func(o *caldav.CalendarObject) {
			groupSetRaw(o.Data.Events()[0].Props, "LAST-MODIFIED", "21001231T235959Z")
		}},
		{"sequence", func(o *caldav.CalendarObject) { groupSetRaw(o.Data.Events()[0].Props, "SEQUENCE", "0") }},
		{"value-date-time", func(o *caldav.CalendarObject) {
			o.Data.Events()[0].Props.Get("DTSTART").Params.Set("VALUE", "DATE-TIME")
		}},
		{"rrule-order", func(o *caldav.CalendarObject) {
			p := o.Data.Events()[0].Props.Get("RRULE")
			parts := strings.Split(p.Value, ";")
			for i, j := 0, len(parts)-1; i < j; i, j = i+1, j-1 {
				parts[i], parts[j] = parts[j], parts[i]
			}
			p.Value = strings.Join(parts, ";")
		}},
		{"timezone", func(o *caldav.CalendarObject) { o.Data.Children = append(o.Data.Children, groupKnownTimezone()) }},
		{"etag-escaping-max", func(o *caldav.CalendarObject) { o.ETag = `"` + strings.Repeat(`\`, 510) + `"` }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			target, actual := groupReceiptFixture(t)
			tc.change(&actual)
			before := snapshot(t, actual)
			if err := groupInitialReceipt(target, actual); err != nil {
				t.Fatal(err)
			}
			if snapshot(t, actual) != before {
				t.Fatal("comparison normalized receipt")
			}
		})
	}
}
func groupKnownTimezone() *ical.Component {
	z := ical.NewComponent("VTIMEZONE")
	z.Props.SetText("TZID", Zone)
	for _, tc := range []struct{ name, start, from, to, rule string }{
		{"STANDARD", "19700405T030000", "+1100", "+1000", "FREQ=YEARLY;BYMONTH=4;BYDAY=1SU"},
		{"DAYLIGHT", "19701004T020000", "+1000", "+1100", "FREQ=YEARLY;BYMONTH=10;BYDAY=1SU"},
	} {
		c := ical.NewComponent(tc.name)
		for name, value := range map[string]string{"DTSTART": tc.start, "TZOFFSETFROM": tc.from, "TZOFFSETTO": tc.to, "RRULE": tc.rule} {
			p := ical.NewProp(name)
			p.Value = value
			c.Props.Set(p)
		}
		z.Children = append(z.Children, c)
	}
	// Parsed expected/actual maps have the decoder's empty-map representation.
	return z
}
func TestGroupInitialReceiptRejectsEveryUnapprovedChange(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*caldav.CalendarObject)
	}{
		{"prodid-long", func(o *caldav.CalendarObject) { o.Data.Props.SetText("PRODID", strings.Repeat("x", 513)) }},
		{"prodid-newline", func(o *caldav.CalendarObject) { o.Data.Props.SetText("PRODID", "a\nb") }},
		{"prodid-param", func(o *caldav.CalendarObject) { o.Data.Props.Get("PRODID").Params.Set("LANGUAGE", "en") }},
		{"prodid-missing", func(o *caldav.CalendarObject) { o.Data.Props.Del("PRODID") }},
		{"prodid-duplicate", func(o *caldav.CalendarObject) {
			o.Data.Props["PRODID"] = append(o.Data.Props["PRODID"], o.Data.Props["PRODID"][0])
		}},
		{"calscale", func(o *caldav.CalendarObject) { o.Data.Props.SetText("CALSCALE", "JULIAN") }},
		{"calscale-param", func(o *caldav.CalendarObject) {
			o.Data.Props.SetText("CALSCALE", "GREGORIAN")
			o.Data.Props.Get("CALSCALE").Params.Set("VALUE", "TEXT")
		}},
		{"created-local", func(o *caldav.CalendarObject) { groupSetRaw(o.Data.Events()[0].Props, "CREATED", "20280212T000000") }},
		{"created-low-year", func(o *caldav.CalendarObject) { groupSetRaw(o.Data.Events()[0].Props, "CREATED", "19691231T235959Z") }},
		{"created-high-year", func(o *caldav.CalendarObject) { groupSetRaw(o.Data.Events()[0].Props, "CREATED", "21010101T000000Z") }},
		{"dtstamp-param", func(o *caldav.CalendarObject) {
			o.Data.Events()[0].Props.Get("DTSTAMP").Params.Set("VALUE", "DATE-TIME")
		}},
		{"dtstamp-missing", func(o *caldav.CalendarObject) { o.Data.Events()[0].Props.Del("DTSTAMP") }},
		{"modified-duplicate", func(o *caldav.CalendarObject) {
			e := o.Data.Events()[0]
			e.Props["LAST-MODIFIED"] = append(e.Props["LAST-MODIFIED"], e.Props["LAST-MODIFIED"][0])
		}},
		{"sequence", func(o *caldav.CalendarObject) { groupSetRaw(o.Data.Events()[0].Props, "SEQUENCE", "1") }},
		{"sequence-noncanonical", func(o *caldav.CalendarObject) { groupSetRaw(o.Data.Events()[0].Props, "SEQUENCE", "00") }},
		{"timezone-unknown", func(o *caldav.CalendarObject) {
			z := groupKnownTimezone()
			z.Props.SetText("X-UNKNOWN", "x")
			o.Data.Children = append(o.Data.Children, z)
		}},
		{"timezone-duplicate", func(o *caldav.CalendarObject) {
			o.Data.Children = append(o.Data.Children, groupKnownTimezone(), groupKnownTimezone())
		}},
		{"organizer", func(o *caldav.CalendarObject) { o.Data.Events()[0].Props.SetText("ORGANIZER", "person@example.test") }},
		{"attendee", func(o *caldav.CalendarObject) { o.Data.Events()[0].Props.SetText("ATTENDEE", "person@example.test") }},
		{"rdate", func(o *caldav.CalendarObject) { o.Data.Events()[0].Props.SetText("RDATE", "20280212T000000Z") }},
		{"exdate", func(o *caldav.CalendarObject) { o.Data.Events()[0].Props.SetText("EXDATE", "20280212T000000Z") }},
		{"exception", func(o *caldav.CalendarObject) { o.Data.Events()[0].Props.SetText("RECURRENCE-ID", "20280212T000000Z") }},
		{"event-x", func(o *caldav.CalendarObject) { o.Data.Events()[0].Props.SetText("X-UNKNOWN", "x") }},
		{"calendar-x", func(o *caldav.CalendarObject) { o.Data.Props.SetText("X-UNKNOWN", "x") }},
		{"rrule-default-missing", func(o *caldav.CalendarObject) {
			p := o.Data.Events()[0].Props.Get("RRULE")
			p.Value = strings.Replace(p.Value, ";INTERVAL=1", "", 1)
		}},
		{"rrule-extra", func(o *caldav.CalendarObject) { o.Data.Events()[0].Props.Get("RRULE").Value += ";BYSECOND=0" }},
		{"rrule-duplicate", func(o *caldav.CalendarObject) { o.Data.Events()[0].Props.Get("RRULE").Value += ";WKST=MO" }},
		{"duration-alternate", func(o *caldav.CalendarObject) { o.Data.Events()[0].Props.Get("DURATION").Value = "PT300S" }},
		{"status", func(o *caldav.CalendarObject) { o.Data.Events()[0].Props.SetText("STATUS", "TENTATIVE") }},
		{"transp", func(o *caldav.CalendarObject) { o.Data.Events()[0].Props.Del("TRANSP") }},
		{"alarm-extra", func(o *caldav.CalendarObject) { o.Data.Events()[0].Children[0].Props.SetText("X-UNKNOWN", "x") }},
		{"alarm-trigger", func(o *caldav.CalendarObject) { o.Data.Events()[0].Children[0].Props.Get("TRIGGER").Value = "PT1M" }},
		{"start-parameter-duplicate", func(o *caldav.CalendarObject) {
			o.Data.Events()[0].Props.Get("DTSTART").Params["TZID"] = []string{Zone, Zone}
		}},
		{"title", func(o *caldav.CalendarObject) { o.Data.Events()[0].Props.SetText("SUMMARY", "different") }},
		{"description", func(o *caldav.CalendarObject) { o.Data.Events()[0].Props.SetText("DESCRIPTION", "[docket]") }},
		{"uid", func(o *caldav.CalendarObject) { o.Data.Events()[0].Props.SetText("UID", "different") }},
		{"href", func(o *caldav.CalendarObject) { o.Path += "x" }},
		{"weak-tag", func(o *caldav.CalendarObject) { o.ETag = `W/"weak"` }},
		{"long-tag", func(o *caldav.CalendarObject) { o.ETag = `"` + strings.Repeat(`\`, 511) + `"` }},
		{"non-ascii-tag", func(o *caldav.CalendarObject) { o.ETag = `"é"` }},
		{"oversize-data", func(o *caldav.CalendarObject) { o.Data.Props.SetText("PRODID", strings.Repeat("x", 10000)) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			target, actual := groupReceiptFixture(t)
			tc.change(&actual)
			if err := groupInitialReceipt(target, actual); err == nil {
				t.Fatal("unapproved normalization")
			}
		})
	}
}

func TestGroupMaximumActualEnvelopeBudgetAndEscapingBoundary(t *testing.T) {
	s := groupTestStore(t)
	r := groupTestRequest(t, s, dailyV2Schedule("2027-02-12T09:17", 0.5, false, ""))
	r.Schedule.Title = strings.Repeat(`"\`, 250)
	prepared, err := PrepareGroup(r, groupTestBinding, GroupSnapshot{}, now)
	if err != nil {
		t.Fatal(err)
	}
	op, err := prepared.operation()
	if err != nil {
		t.Fatal(err)
	}
	state, _ := groupDecodeState(s.raw)
	state.Pending = &op
	maximum, err := groupMaximumRecordBytes(state)
	if err != nil {
		t.Fatal(err)
	}
	// Construct the largest typed receipts using maximal ETag JSON escaping, a
	// bounded PRODID, the known timezone and all permitted managed timestamps.
	for i, target := range op.Targets {
		receipt := dailyPartsCloneObject(*target.Planned)
		receipt.ETag = `"` + strings.Repeat(`<`, 510) + `"`
		receipt.Data.Props.SetText("PRODID", strings.Repeat(`\`, 512))
		receipt.Data.Props.SetText("CALSCALE", "GREGORIAN")
		receipt.Data.Children = append(receipt.Data.Children, groupKnownTimezone())
		e := receipt.Data.Events()[0]
		groupSetRaw(e.Props, "CREATED", "21001231T235959Z")
		groupSetRaw(e.Props, "SEQUENCE", "0")
		e.Props.Get("DTSTART").Params.Set("VALUE", "DATE-TIME")
		if err := groupInitialReceipt(target, receipt); err != nil {
			t.Fatal(err)
		}
		op.Targets[i].Receipt = &receipt
		op.Targets[i].Phase = "verified"
	}
	actual, err := groupEncodeState(state)
	if err != nil {
		t.Fatal(err)
	}
	if len(actual)+36 > maximum {
		t.Fatalf("underestimated actual serialized record: actual=%d bound=%d", len(actual)+36, maximum)
	}
	// At admission, ALL 24 allowances (not just the first receipt) must fit.
	for i := range op.Targets {
		op.Targets[i].Receipt = nil
		op.Targets[i].Phase = "planned"
	}
	base, err := groupMaximumRecordBytes(state)
	if err != nil {
		t.Fatal(err)
	}
	op.Binding.Actor.ServiceUID += strings.Repeat("x", groupStoreLimit-base)
	exact, err := groupMaximumRecordBytes(state)
	if err != nil || exact != groupStoreLimit {
		t.Fatalf("exact budget boundary %d %v", exact, err)
	}
	op.Binding.Actor.ServiceUID += "x"
	if _, err := groupMaximumRecordBytes(state); err == nil {
		t.Fatal("one-byte budget excess admitted")
	}
	// An actual durable record must independently fit the fixed 36-byte framing.
	raw, _ := groupEncodeState(groupState{Version: 1, Epoch: strings.Split(r.Fence, ":")[0]})
	var minimal groupState
	_ = json.Unmarshal(raw, &minimal)
	minimal.Epoch = strings.Repeat("x", groupStoreLimit-len(raw)+len(minimal.Epoch)-36)
	encoded, err := groupEncodeState(minimal)
	if err != nil || len(encoded)+36 != groupStoreLimit {
		t.Fatal("envelope boundary", len(encoded), err)
	}
	minimal.Epoch += "x"
	if _, err := groupEncodeState(minimal); err == nil {
		t.Fatal("envelope overflow allowed")
	}
}

func groupSetRaw(props ical.Props, name, value string) {
	p := ical.NewProp(name)
	p.Value = value
	props.Set(p)
}

func TestGroupMaximumReceiptAdmissionFailsBeforeJournalAndPUT(t *testing.T) {
	s := groupTestStore(t)
	r := groupTestRequest(t, s, dailyV2Schedule("2027-02-12T09:17", 0.5, false, ""))
	prepared, err := PrepareGroup(r, groupTestBinding, GroupSnapshot{}, now)
	if err != nil {
		t.Fatal(err)
	}
	op, err := prepared.operation()
	if err != nil {
		t.Fatal(err)
	}
	state, _ := groupDecodeState(s.raw)
	state.Pending = &op
	maximum, err := groupMaximumRecordBytes(state)
	if err != nil {
		t.Fatal(err)
	}
	binding := groupTestBinding
	binding.Actor.ServiceUID += strings.Repeat("x", groupStoreLimit-maximum+1)
	op.Binding = binding
	initial, err := groupEncodeState(state)
	if err != nil || len(initial)+36 >= groupStoreLimit {
		t.Fatal("initial record should fit; future receipts must not", err)
	}
	b := groupTestBackend(nil)
	check := func(_ context.Context, got GroupBinding) error {
		if got != binding {
			return groupTestError
		}
		return nil
	}
	_, err = ExecuteGroup(context.Background(), b, s, r, binding, check, now)
	if err == nil || len(b.writes) != 0 || s.saves != 0 {
		t.Fatal("journal/PUT before all maximum receipts fit", err, len(b.writes), s.saves)
	}
}

func TestGroupReceiptJSONGrowthBoundaryIndependentOfAllowlist(t *testing.T) {
	for _, unit := range []string{"x", "é", "界", "<>&", "\"\\\n", "\u2028"} {
		for _, growth := range []int{groupReceiptGrowth - 1, groupReceiptGrowth, groupReceiptGrowth + 1} {
			t.Run(fmt.Sprintf("%q/%d", unit, growth), func(t *testing.T) {
				// X-SIZE is intentionally NOT an allowed receipt property. These
				// synthetic calendars isolate only the actual-JSON size gate.
				planned := ical.NewCalendar()
				groupSetRaw(planned.Props, "X-SIZE", "baseline")
				actual := ical.NewCalendar()
				encodedUnit, _ := json.Marshal(unit)
				unitBytes := len(encodedUnit) - 2
				value := "baseline" + strings.Repeat(unit, growth/unitBytes) + strings.Repeat("x", growth%unitBytes)
				groupSetRaw(actual.Props, "X-SIZE", value)
				plannedJSON, err := json.Marshal(planned)
				if err != nil {
					t.Fatal(err)
				}
				actualJSON, err := json.Marshal(actual)
				if err != nil || len(actualJSON)-len(plannedJSON) != growth {
					t.Fatal("fixture JSON size mismatch", len(actualJSON)-len(plannedJSON), err)
				}
				if err := groupReceiptSize(planned, actual); (err != nil) != (growth > groupReceiptGrowth) {
					t.Fatalf("growth=%d accepted=%v", growth, err == nil)
				}
			})
		}
	}
}
