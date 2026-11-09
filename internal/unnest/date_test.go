package unnest

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestDateDiagnosticsAndPrecision(t *testing.T) {
	cases := []struct{ value, when, tz, precision, reason string }{
		{"lun 5 ott 2026, 22:07", "2026-10-05 22:07:00", "", PrecisionMinute, DateParsed},
		{"5 Sept 2026 22:07", "2026-09-05 22:07:00", "", PrecisionMinute, DateParsed},
		{"5 Oct 2026 22:07, NZST", "2026-10-05 22:07:00", "NZST", PrecisionMinute, DateParsed},
		{"LUN. 5 OTT. 2026, 22:07:00", "2026-10-05 22:07:00", "", PrecisionSecond, DateParsed},
		{"5 Oct 2026 12:00 AM", "2026-10-05 00:00:00", "", PrecisionMinute, DateParsed},
		{"5 Oct 2026 12:00PM", "2026-10-05 12:00:00", "", PrecisionMinute, DateParsed},
		{"5 Oct 2026 22:07 +0200", "2026-10-05 22:07:00", "+0200", PrecisionMinute, DateParsed},
		{"5 Oct 2026 22:07 +02:00", "2026-10-05 22:07:00", "+02:00", PrecisionMinute, DateParsed},
		{"5 Oct 2026 22:07 -0330", "2026-10-05 22:07:00", "-0330", PrecisionMinute, DateParsed},
		{"5 Oct 2026 22:07 XYZ", "2026-10-05 22:07:00", "XYZ", PrecisionMinute, DateParsed},
		{"2026-10-05 22:07", "2026-10-05 22:07:00", "", PrecisionMinute, DateParsed},
		{"5\u00a0octobre\u202f2026\u2009à\u00a022:07", "2026-10-05 22:07:00", "", PrecisionMinute, DateParsed},
		{"lun. 2 fe\u0301vr. 2026 à 22:07", "2026-02-02 22:07:00", "", PrecisionMinute, DateParsed},
		{"", "", "", PrecisionUnknown, DateMissing},
		{"22:07", "", "", PrecisionUnknown, DateMissing},
		{"at 22:07", "", "", PrecisionUnknown, DateMissing},
		{"5 Oct 2026", "", "", PrecisionUnknown, DateMissingClock},
		{"5 Oct 22:07", "", "", PrecisionUnknown, DateIncomplete},
		{"Oct 2026 22:07", "", "", PrecisionUnknown, DateIncomplete},
		{"Monday 22:07", "", "", PrecisionUnknown, DateIncomplete},
		{"5 Oct 26 22:07", "", "", PrecisionUnknown, DateIncomplete},
		{"03/04/2026 22:07", "", "", PrecisionUnknown, DateAmbiguousNumeric},
		{"13/04/2026 22:07", "", "", PrecisionUnknown, DateAmbiguousNumeric},
		{"03.04.2026 22:07", "", "", PrecisionUnknown, DateAmbiguousNumeric},
		{"Mon, 03/04/2026 at 22:07", "", "", PrecisionUnknown, DateAmbiguousNumeric},
		{"31 Feb 2026 22:07", "", "", PrecisionUnknown, DateInvalid},
		{"29 Feb 2025 22:07", "", "", PrecisionUnknown, DateInvalid},
		{"2026-02-30 22:07", "", "", PrecisionUnknown, DateInvalid},
		{"0 Oct 2026 22:07", "", "", PrecisionUnknown, DateInvalid},
		{"5 Oct 0000 22:07", "", "", PrecisionUnknown, DateInvalid},
		{"1 Jan 0001 00:00", "", "", PrecisionUnknown, DateInvalid},
		{"+5 Oct 2026 22:07", "", "", PrecisionUnknown, DateInvalid},
		{"5 Oct +026 22:07", "", "", PrecisionUnknown, DateInvalid},
		{"Tue 5 Oct 2026 22:07", "", "", PrecisionUnknown, DateContradictory},
		{"5 Oct 2026 22:07 +2460", "", "", PrecisionUnknown, DateInvalidZone},
		{"5 Oct 2026 22:07 +02:99", "", "", PrecisionUnknown, DateInvalidZone},
		{"5 Oct 2026 22:07 +02000", "", "", PrecisionUnknown, DateInvalidZone},
		{"5 Oct 2026 x22:07", "", "", PrecisionUnknown, DateInvalidClock},
		{"5 Oct 2026 -1:07", "", "", PrecisionUnknown, DateInvalidClock},
		{"5 Oct 2026 12:00:00:01", "", "", PrecisionUnknown, DateInvalidClock},
		{"5 Oct 2026 12:00oops", "", "", PrecisionUnknown, DateInvalidClock},
		{"5 Oct 2026 112:00", "", "", PrecisionUnknown, DateInvalidClock},
		{"5 Oct 2026 0:12 PM", "", "", PrecisionUnknown, DateInvalidClock},
		{"5 Oct 2026 13:12 PM", "", "", PrecisionUnknown, DateInvalidClock},
		{"5 Oct 2026 1:12 p.m.", "", "", PrecisionUnknown, DateInvalidClock},
		{"5 Oct 2026 12:00 AM PM", "", "", PrecisionUnknown, DateInvalidClock},
		{"5 Oct 2026 12:00,12", "", "", PrecisionUnknown, DateInvalidClock},
		{"5.. Oct 2026 12:00", "", "", PrecisionUnknown, DateUnsupported},
		{"5 Oct.. 2026 12:00", "", "", PrecisionUnknown, DateUnsupported},
		{"5 Oct 2026 22:07 trailing prose", "", "", PrecisionUnknown, DateUnsupported},
		{"5 NotAMonth 2026 22:07 NZST", "", "NZST", PrecisionUnknown, DateUnsupported},
		{"5 octember 2026 22:07", "", "", PrecisionUnknown, DateUnsupported},
	}
	for _, c := range cases {
		t.Run(c.value, func(t *testing.T) {
			d := ParseDate(c.value)
			got := ""
			if !d.Sent.IsZero() {
				got = d.Sent.Format("2006-01-02 15:04:05")
			}
			if got != c.when || d.TZ != c.tz || d.Precision != c.precision || d.Reason != c.reason {
				t.Errorf("got %q/%q/%s/%s, want %q/%q/%s/%s", got, d.TZ, d.Precision, d.Reason, c.when, c.tz, c.precision, c.reason)
			}
			// The same semantics must survive Normalise -> Peel -> Parse.
			blocks := Peel("From: Alex <alex@example.test>\nDate: " + c.value + "\nSubject: Bulletin\n\nSynthetic bulletin.")
			if len(blocks) != 1 {
				t.Fatalf("blocks=%d", len(blocks))
			}
			r := Parse(blocks[0])
			if r.Sent != d.Sent || r.TZ != d.TZ || r.DatePrecision != d.Precision || r.DateReason != d.Reason || r.Address != "alex@example.test" {
				t.Errorf("pipeline disagrees with date result: %+v / %+v", r, d)
			}
		})
	}
}

// Every accepted lexicon token has an actual date grammar and pipeline check,
// in addition to the independent language exemplars in formats.json.
func TestDateLexiconsThroughHeaderPipeline(t *testing.T) {
	for _, g := range dateGrammars {
		t.Run(g.name, func(t *testing.T) {
			check := func(value string, want time.Time) {
				t.Helper()
				bs := Peel("From: Alex <alex@example.test>\nDate: " + value + "\n\nSynthetic bulletin.")
				if len(bs) != 1 {
					t.Fatalf("blocks=%d for %s", len(bs), value)
				}
				r := Parse(bs[0])
				if !r.Sent.Equal(want) || r.DateReason != DateParsed || r.DatePrecision != PrecisionMinute {
					t.Errorf("%s -> %+v, want %s", value, r, want)
				}
			}
			for month, group := range g.months {
				for _, token := range strings.Fields(group) {
					for _, form := range []string{token, strings.ToUpper(token) + "."} {
						check(fmt.Sprintf("5 %s 2026 22:07", form), time.Date(2026, time.Month(month+1), 5, 22, 7, 0, 0, time.UTC))
					}
				}
			}
			for weekday, group := range g.weekdays {
				day := 4 + weekday // 4 Oct 2026 is Sunday.
				month := strings.Fields(g.months[9])[0]
				for _, token := range strings.Fields(group) {
					for _, form := range []string{token, strings.ToUpper(token) + "."} {
						check(fmt.Sprintf("%s, %d %s 2026 22:07", form, day, month), time.Date(2026, 10, day, 22, 7, 0, 0, time.UTC))
					}
				}
			}
		})
	}
}

func TestSplitWhenBooleanIsOnlyAPivot(t *testing.T) {
	sent, _, rest, pivot := SplitWhen("03/04/2026 22:07, Alex <alex@example.test>")
	if !pivot || !sent.IsZero() || !strings.Contains(rest, "Alex") {
		t.Fatal("pivot was confused with parse success")
	}
}
