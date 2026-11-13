package hassles

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

const dailyV2Task = "00000000-0000-4000-8000-000000000001"
const dailyV2Incarnation = "00000000-0000-4000-8000-000000000002"

type dailyV2Fixture struct {
	Contract string
	Cases    []struct {
		Name        string
		Task        string
		Incarnation string
		Input       Schedule
		Expected    struct {
			Payload  string
			PlanHash string
			Members  []struct {
				DailyPartsV2Member
				Description string
			}
		}
	}
	Rejections []struct {
		Name    string
		Start   string
		Minutes int
		Work    bool
		Until   string
	}
}

func dailyV2Fixtures(t *testing.T) dailyV2Fixture {
	t.Helper()
	data, err := os.ReadFile("testdata/daily-parts-v2.json")
	if err != nil {
		t.Fatal(err)
	}
	if digest(string(data)) != "11070c2e368589a042dfb64ba553430d23606a300f60258f6e8de04da83a41a0" {
		t.Fatal("frozen independent fixture changed")
	}
	var fixture dailyV2Fixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Contract != "hassles-daily-parts/2" || len(fixture.Cases) != 8 || len(fixture.Rejections) != 10 {
		t.Fatal("unexpected literal fixture coverage")
	}
	return fixture
}

func dailyV2Schedule(start string, interval float64, work bool, until string) Schedule {
	return Schedule{Title: "Synthetic", Start: start, TimeZone: Zone, Cadence: "hourly", Interval: interval, WorkHours: work, Until: until}
}

func dailyV2Compile(t *testing.T, s Schedule) *DailyPartsV2Plan {
	t.Helper()
	p, err := CompileDailyPartsV2(s, dailyV2Task, dailyV2Incarnation)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func dailyV2Instant(t *testing.T, value string) time.Time {
	t.Helper()
	at, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatal(err)
	}
	return at
}

func TestDailyPartsV2IndependentLiterals(t *testing.T) {
	fixture := dailyV2Fixtures(t)
	members := 0
	for _, c := range fixture.Cases {
		t.Run(c.Name, func(t *testing.T) {
			p, err := CompileDailyPartsV2(c.Input, c.Task, c.Incarnation)
			if err != nil {
				t.Fatal(err)
			}
			if p.Payload() != c.Expected.Payload || p.PlanHash() != c.Expected.PlanHash || p.Task() != c.Task || p.Incarnation() != c.Incarnation {
				t.Fatalf("literal payload/hash/IDs mismatch: %q %s", p.Payload(), p.PlanHash())
			}
			got := p.Members()
			if len(got) != len(c.Expected.Members) {
				t.Fatalf("members %d want %d", len(got), len(c.Expected.Members))
			}
			for i, want := range c.Expected.Members {
				members++
				if !reflect.DeepEqual(got[i], want.DailyPartsV2Member) {
					t.Fatalf("member %d: %+v want %+v", i, got[i], want.DailyPartsV2Member)
				}
				for _, completion := range []bool{false, true} {
					exact := want.Description
					if completion {
						exact += "\n[chainmail:completed-instances:v1]"
					}
					desc, err := p.Description(got[i].MemberID, completion)
					if err != nil || desc != exact || strings.Contains(desc, "[docket]") || strings.HasSuffix(desc, "\n") {
						t.Fatalf("exact description: %q, %v", desc, err)
					}
					meta, err := ParseDailyPartsV2Description(exact)
					if err != nil {
						t.Fatal(err)
					}
					if meta.MemberID != got[i].MemberID || meta.HasCompletionMetadata != completion || meta.Plan.Payload() != c.Expected.Payload || meta.Plan.Logic() != p.Logic() {
						t.Fatal("parsed provenance/timing mismatch")
					}
				}
			}
			parsed, err := ParseDailyPartsV2Payload(c.Expected.Payload, c.Expected.PlanHash)
			if err != nil || !reflect.DeepEqual(parsed.Members(), got) {
				t.Fatalf("literal parse: %v", err)
			}
		})
	}
	if members != 16 {
		t.Fatalf("literal member count: %d", members)
	}
	// Preserve the frozen restricted checkpoint, explicitly reclassifying ONLY
	// these five 02-only negatives as current native DailyPartsProfileTest does.
	// This policy change is not proof of correct recurrence expansion across DST.
	newlyAdmitted := map[string]int{
		"plain-half": 24, "plain-hour": 24, "plain2-even": 12,
		"plain24-at02": 1, "hour02-grid-before-tiny-end": 1,
	}
	reclassified := 0
	for _, c := range fixture.Rejections {
		t.Run(c.Name, func(t *testing.T) {
			p, err := CompileDailyPartsV2(dailyV2Schedule(c.Start, float64(c.Minutes)/60, c.Work, c.Until), dailyV2Task, dailyV2Incarnation)
			count, admitted := newlyAdmitted[c.Name]
			if !admitted {
				if err == nil {
					t.Fatal("accepted retained historical rejection")
				}
				return
			}
			reclassified++
			if err != nil {
				t.Fatal(err)
			}
			if len(p.Members()) != count {
				t.Fatalf("current native roster count=%d want=%d", len(p.Members()), count)
			}
			if _, err := ParseDailyPartsV2Payload(p.Payload(), p.PlanHash()); err != nil {
				t.Fatal(err)
			}
		})
	}
	if reclassified != 5 {
		t.Fatalf("historical reclassification count=%d", reclassified)
	}
}

func TestDailyPartsV2EveryPhaseMinuteIntervalWorkAndFiniteMode(t *testing.T) {
	// Every ordinary phase, including 02:xx, in the current native profile.
	// Initial anchors only: no provider/DST recurrence expansion is asserted.
	plainCounts := [...]int{24, 24, 12, 8, 6, 4, 3, 2, 1}
	// Literal counts for start hours 09..16, in interval order below.
	workCounts := [...][8]int{
		{8, 8, 8, 8, 8, 8, 8, 8}, {8, 8, 8, 8, 8, 8, 8, 8},
		{4, 4, 4, 4, 4, 4, 4, 4}, {3, 3, 2, 3, 3, 2, 3, 3},
		{2, 2, 2, 2, 2, 2, 2, 2}, {2, 2, 1, 1, 1, 1, 2, 2},
		{1, 1, 1, 1, 1, 1, 1, 1}, {1, 1, 1, 1, 1, 1, 1, 1},
		{1, 1, 1, 1, 1, 1, 1, 1},
	}
	for _, date := range []string{"2027-02-12", "2028-02-29"} {
		for _, finite := range []bool{false, true} {
			accepted := 0
			for _, work := range []bool{false, true} {
				for hour := 0; hour < 24; hour++ {
					if work && (hour < 9 || hour >= 17) {
						continue
					}
					for minute := 0; minute < 60; minute++ {
						startText := fmt.Sprintf("%sT%02d:%02d", date, hour, minute)
						start, err := WallTime(startText)
						if err != nil {
							t.Fatal(err)
						}
						until := ""
						if finite {
							until = start.Add(time.Minute).Format(dailyPartsV2WallLayout)
						}
						for intervalIndex, interval := range []float64{0.5, 1, 2, 3, 4, 6, 8, 12, 24} {
							p, err := CompileDailyPartsV2(dailyV2Schedule(startText, interval, work, until), dailyV2Task, dailyV2Incarnation)
							accepted++
							if err != nil {
								t.Fatalf("safe %s interval=%g work=%v: %v", startText, interval, work, err)
							}
							members := p.Members()
							limit := 24
							if work {
								limit = 8
							}
							if len(members) == 0 || len(members) > limit {
								t.Fatalf("roster bound: %d", len(members))
							}
							minuteText := fmt.Sprint(minute)
							if interval == 0.5 {
								minuteText = fmt.Sprintf("%d,%d", minute%30, minute%30+30)
							}
							wantRule := "FREQ=DAILY"
							if finite {
								wantRule += ";UNTIL=" + start.Add(time.Minute).UTC().Format("20060102T150405Z")
							}
							wantRule += ";INTERVAL=1;WKST=MO;BYMINUTE=" + minuteText
							if work {
								wantRule += ";BYDAY=MO,TU,WE,TH,FR"
							}
							for _, member := range members {
								if member.RRule != wantRule || !p.SupportsCompletion(member.MemberID, time.UnixMilli(member.AnchorEpochMillis)) {
									t.Fatalf("invalid member rule/witness: %+v want rule=%s", member, wantRule)
								}
							}
							if !finite {
								wantCount := plainCounts[intervalIndex]
								if work {
									wantCount = workCounts[intervalIndex][hour-9]
								}
								if len(members) != wantCount {
									t.Fatalf("unbounded roster count=%d want=%d start=%s interval=%g work=%v", len(members), wantCount, startText, interval, work)
								}
								continue
							}
							if len(members) != 1 || members[0].Hour != hour || members[0].AnchorLocal != startText || members[0].AnchorEpochMillis != start.UnixMilli() || members[0].EndEpochMillis != start.UnixMilli()+300000 {
								t.Fatalf("finite anchor: %+v", members)
							}
							if !p.SupportsCompletion(members[0].MemberID, start) || p.SupportsCompletion(members[0].MemberID, start.Add(time.Minute)) {
								t.Fatal("finite membership mismatch")
							}
						}
					}
				}
			}
			if accepted != 17280 {
				t.Fatalf("%s finite=%v: accepted=%d", date, finite, accepted)
			}
		}
	}
}

func TestDailyPartsV2CurrentNativeTwentyFourMemberLiterals(t *testing.T) {
	// Independent literals shared with current native DailyPartsProfileTest,
	// never generated by the candidate compiler or a golden-update command.
	rows := strings.Split(`00 2027-02-13T00:47 2027-02-13T00:17
01 2027-02-13T01:47 2027-02-13T01:17
02 2027-02-13T02:47 2027-02-13T02:17
03 2027-02-13T03:47 2027-02-13T03:17
04 2027-02-13T04:47 2027-02-13T04:17
05 2027-02-13T05:47 2027-02-13T05:17
06 2027-02-13T06:47 2027-02-13T06:17
07 2027-02-13T07:47 2027-02-13T07:17
08 2027-02-13T08:47 2027-02-13T08:17
09 2027-02-12T09:47 2027-02-12T09:47
10 2027-02-12T10:47 2027-02-12T10:17
11 2027-02-12T11:47 2027-02-12T11:17
12 2027-02-12T12:47 2027-02-12T12:17
13 2027-02-12T13:47 2027-02-12T13:17
14 2027-02-12T14:47 2027-02-12T14:17
15 2027-02-12T15:47 2027-02-12T15:17
16 2027-02-12T16:47 2027-02-12T16:17
17 2027-02-12T17:47 2027-02-12T17:17
18 2027-02-12T18:47 2027-02-12T18:17
19 2027-02-12T19:47 2027-02-12T19:17
20 2027-02-12T20:47 2027-02-12T20:17
21 2027-02-12T21:47 2027-02-12T21:17
22 2027-02-12T22:47 2027-02-12T22:17
23 2027-02-12T23:47 2027-02-12T23:17`, "\n")
	for _, half := range []bool{false, true} {
		t.Run(fmt.Sprintf("half=%v", half), func(t *testing.T) {
			interval, column := 1.0, 1
			minutes := []int{47}
			rule := "FREQ=DAILY;INTERVAL=1;WKST=MO;BYMINUTE=47"
			hour02UTC := "2027-02-12T15:47:00Z"
			if half {
				interval, column = 0.5, 2
				minutes = []int{17, 47}
				rule = "FREQ=DAILY;INTERVAL=1;WKST=MO;BYMINUTE=17,47"
				hour02UTC = "2027-02-12T15:17:00Z"
			}
			p := dailyV2Compile(t, dailyV2Schedule("2027-02-12T09:47", interval, false, ""))
			members := p.Members()
			if len(members) != 24 {
				t.Fatalf("roster count=%d", len(members))
			}
			seen := map[string]bool{}
			for i, row := range rows {
				want := strings.Fields(row)
				member := members[i]
				if fmt.Sprintf("%02d", member.Hour) != want[0] || member.AnchorLocal != want[column] || !reflect.DeepEqual(member.Minutes, minutes) || member.RRule != rule || member.Duration != "PT5M" || member.EndEpochMillis-member.AnchorEpochMillis != 300000 {
					t.Fatalf("literal member mismatch: %+v want=%v", member, want)
				}
				if seen[member.MemberID] {
					t.Fatal("duplicate member identity")
				}
				seen[member.MemberID] = true
				if member.Hour == 2 && member.AnchorEpochMillis != dailyV2Instant(t, hour02UTC).UnixMilli() {
					t.Fatal("hour02 literal UTC mismatch")
				}
				if member.Hour == 9 && member.MemberID != "52bebc2b014c15a9b2d936df6aca8fbad7ac16fecf764d90f9df58530ae61fca" {
					t.Fatal("native literal member identity mismatch")
				}
				for _, completed := range []bool{false, true} {
					desc, err := p.Description(member.MemberID, completed)
					if err != nil {
						t.Fatal(err)
					}
					meta, err := ParseDailyPartsV2Description(desc)
					if err != nil || meta.MemberID != member.MemberID || meta.HasCompletionMetadata != completed || meta.Plan.Payload() != p.Payload() || !reflect.DeepEqual(meta.Plan.Members(), members) {
						t.Fatalf("24-member metadata round trip: %v", err)
					}
				}
			}
			parsed, err := ParseDailyPartsV2Payload(p.Payload(), p.PlanHash())
			if err != nil || !reflect.DeepEqual(parsed.Members(), members) {
				t.Fatalf("24-member payload round trip: %v", err)
			}
		})
	}
}

func TestDailyPartsV2WholeTwoHourUnboundedPhases(t *testing.T) {
	for hour := 0; hour < 24; hour++ {
		for minute := 0; minute < 60; minute++ {
			startText := fmt.Sprintf("2027-02-12T%02d:%02d", hour, minute)
			p := dailyV2Compile(t, dailyV2Schedule(startText, 2, false, ""))
			members := p.Members()
			if len(members) != 12 {
				t.Fatalf("plain2 roster count %d", len(members))
			}
			for i, member := range members {
				date := "2027-02-12"
				if member.Hour < hour {
					date = "2027-02-13"
				}
				if member.Hour != i*2+hour%2 || member.AnchorLocal != fmt.Sprintf("%sT%02d:%02d", date, member.Hour, minute) || !reflect.DeepEqual(member.Minutes, []int{minute}) {
					t.Fatalf("whole phase anchor %+v", member)
				}
				if !p.SupportsCompletion(member.MemberID, time.UnixMilli(member.AnchorEpochMillis)) {
					t.Fatal("anchor not supported")
				}
			}
		}
	}
}

func TestDailyPartsV2PayloadRejectsNoncanonicalBytes(t *testing.T) {
	fixture := dailyV2Fixtures(t)
	base := fixture.Cases[0].Expected.Payload
	member := strings.Split(base, "\n")[8]
	cases := map[string]string{
		"version":             strings.Replace(base, "parts/2", "parts/3", 1),
		"no-final-LF":         strings.TrimSuffix(base, "\n"),
		"extra-LF":            base + "\n",
		"CR":                  strings.ReplaceAll(base, "\n", "\r\n"),
		"tab":                 strings.Replace(base, "start=", "start=\t", 1),
		"invalid-utf8":        base + string([]byte{0xff}),
		"unicode":             strings.Replace(base, "minutes=1440", "minutes=１４４０", 1),
		"title":               strings.Replace(base, "minutes=", "title=Injected\nminutes=", 1),
		"timezone":            strings.Replace(base, Zone, "UTC", 1),
		"order":               strings.Replace(base, "minutes=1440\nwork=0", "work=0\nminutes=1440", 1),
		"duplicate":           strings.Replace(base, "minutes=1440\n", "minutes=1440\nminutes=1440\n", 1),
		"integer-zero":        strings.Replace(base, "minutes=1440", "minutes=01440", 1),
		"integer-plus":        strings.Replace(base, "minutes=1440", "minutes=+1440", 1),
		"integer-decimal":     strings.Replace(base, "minutes=1440", "minutes=1440.0", 1),
		"integer-unsupported": strings.Replace(base, "minutes=1440", "minutes=90", 1),
		"work":                strings.Replace(base, "work=0", "work=false", 1),
		"until-empty":         strings.Replace(base, "until=-", "until=", 1),
		"end-equals-start":    strings.Replace(base, "until=-", "until=2027-02-12T09:17", 1),
		"start-format":        strings.Replace(base, "start=2027-02-12T09:17", "start=2027-02-12T9:17", 1),
		"no-roster":           strings.Replace(base, member+"\n", "", 1),
		"duplicate-role":      base + member + "\n",
		"wrong-role":          strings.Replace(base, "member=09,", "member=08,", 1),
		"wrong-anchor":        strings.Replace(base, "member=09,2027-02-12T09:17", "member=09,2027-02-13T09:17", 1),
		"wrong-member-ID":     strings.Replace(base, fixture.Cases[0].Expected.Members[0].MemberID, strings.Repeat("0", 64), 1),
		"roster-unknown":      base + "unknown=1\n",
		"empty-uuid":          strings.Replace(base, "task="+dailyV2Task, "task=", 1),
		"uuid-version":        strings.Replace(base, "task="+dailyV2Task, "task=00000000-0000-0000-8000-000000000001", 1),
		"uuid-variant":        strings.Replace(base, "task="+dailyV2Task, "task=00000000-0000-4000-7000-000000000001", 1),
		"uppercase-uuid":      strings.Replace(base, "task="+dailyV2Task, "task=ABCDEFAB-0000-4000-8000-000000000001", 1),
		"payload-cap":         strings.Repeat("x", 12001),
		"member-cap":          strings.Split(base, "member=")[0] + strings.Repeat(member+"\n", 25),
	}
	multi := fixture.Cases[3].Expected.Payload
	lines := strings.Split(multi, "\n")
	lines[8], lines[9] = lines[9], lines[8]
	cases["roster-order"] = strings.Join(lines, "\n")
	for name, payload := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseDailyPartsV2Payload(payload, digest(payload)); err == nil {
				t.Fatal("accepted noncanonical payload with matching SHA")
			}
		})
	}
	for _, hash := range []string{"", strings.Repeat("0", 64), strings.ToUpper(fixture.Cases[0].Expected.PlanHash), fixture.Cases[0].Expected.PlanHash + "0"} {
		if _, err := ParseDailyPartsV2Payload(base, hash); err == nil {
			t.Fatal("accepted invalid payload hash")
		}
	}
}

func TestDailyPartsV2DescriptionRejectsClosedCodecViolations(t *testing.T) {
	fixture := dailyV2Fixtures(t)
	base := fixture.Cases[0].Expected.Members[0].Description
	lines := strings.Split(base, "\n")
	encoded := base64.RawURLEncoding.EncodeToString([]byte(fixture.Cases[0].Expected.Payload))
	cases := map[string]string{
		"empty":                "",
		"trailing-LF":          base + "\n",
		"CR":                   strings.ReplaceAll(base, "\n", "\r\n"),
		"missing-H":            strings.Join(lines[1:], "\n"),
		"missing-P":            lines[0] + "\n" + lines[2],
		"missing-M":            strings.Join(lines[:2], "\n"),
		"duplicate-H":          base + "\n" + lines[0],
		"duplicate-P":          base + "\n" + lines[1],
		"duplicate-M":          base + "\n" + lines[2],
		"order":                lines[1] + "\n" + lines[0] + "\n" + lines[2],
		"extras":               base + "\nopaque notes",
		"completion-extra":     base + "\n" + completionMarker + "\nextra",
		"completion-duplicate": base + "\n" + completionMarker + "\n" + completionMarker,
		"completion-unknown":   base + "\n[chainmail:completed-instances:v2]",
		"old-hourly":           strings.Replace(base, "hourly:v2 minutes=1440", "hourly:v1 hours=24", 1),
		"unknown-version":      strings.Replace(base, "[hassle:v2", "[hassle:v3", 1),
		"unknown-field":        strings.Replace(base, " plan=", " extra=1 plan=", 1),
		"wrong-minutes":        strings.Replace(base, "minutes=1440", "minutes=120", 1),
		"noncanonical-minutes": strings.Replace(base, "minutes=1440", "minutes=01440", 1),
		"wrong-task":           strings.Replace(base, "task="+dailyV2Task, "task="+dailyV2Incarnation, 1),
		"wrong-incarnation":    strings.Replace(base, "incarnation="+dailyV2Incarnation, "incarnation="+dailyV2Task, 1),
		"wrong-member":         strings.Replace(base, "member="+fixture.Cases[0].Expected.Members[0].MemberID, "member="+strings.Repeat("0", 64), 1),
		"wrong-plan":           strings.Replace(base, "plan="+fixture.Cases[0].Expected.PlanHash, "plan="+strings.Repeat("0", 64), 1),
		"padded-b64":           strings.Replace(base, encoded, encoded+"=", 1),
		"invalid-b64":          strings.Replace(base, encoded, "!", 1),
		"b64-space":            strings.Replace(base, encoded, encoded+" ", 1),
		"b64-invalid-utf8":     strings.Replace(base, encoded, base64.RawURLEncoding.EncodeToString([]byte{0xff}), 1),
		"b64-cap":              strings.Replace(base, encoded, strings.Repeat("A", 16001), 1),
		"description-cap":      strings.Repeat("x", 16385),
		"unicode":              strings.Replace(base, "minutes=1440", "minutes=１４４０", 1),
		"invalid-utf8":         base + string([]byte{0xff}),
	}
	// This finite literal has unused final-quantum bits. Changing only those
	// bits is an alternate spelling, not a permitted equivalent encoding.
	unusedCase := fixture.Cases[2]
	unusedEncoded := base64.RawURLEncoding.EncodeToString([]byte(unusedCase.Expected.Payload))
	alphabet := "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
	if len(unusedEncoded)%4 != 2 && len(unusedEncoded)%4 != 3 {
		t.Fatal("literal must exercise unused base64 bits")
	}
	last := strings.IndexByte(alphabet, unusedEncoded[len(unusedEncoded)-1])
	alternate := unusedEncoded[:len(unusedEncoded)-1] + string(alphabet[last+1])
	decoded, err := base64.RawURLEncoding.DecodeString(alternate)
	if err != nil || string(decoded) != unusedCase.Expected.Payload {
		t.Fatal("unused-bit negative is not an alternate encoding")
	}
	cases["base64-unused-bits"] = strings.Replace(unusedCase.Expected.Members[0].Description, unusedEncoded, alternate, 1)
	for i := range lines {
		bad := append([]string(nil), lines...)
		bad[i] += "[docket]"
		cases[fmt.Sprintf("Docket-line-%d", i)] = strings.Join(bad, "\n")
	}
	cases["Docket-opaque"] = base + "\n[docket]"
	cases["legacy-conflict"] = "[docket]\n[hassle:v1 task=" + dailyV2Task + "]\n" + base
	for name, desc := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseDailyPartsV2Description(desc); err == nil {
				t.Fatal("accepted invalid description")
			}
		})
	}
}
