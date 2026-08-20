package unnest

import "testing"

// Every shape observed in the 866-fixture corpus, most frequent first. The counts
// are from a census of 5374 real attributions, so these are not invented cases.
func TestParseAttribution(t *testing.T) {
	cases := []struct {
		name            string
		in              string
		sender, addr    string
		date, clock, tz string
	}{
		{"gmail en-GB, 3640 cases",
			"On Tue, 3 Feb 2026 at 07:29, Reginald Stamets <reginald.booker@vulcan.fed> wrote:",
			"Reginald Stamets", "reginald.booker@vulcan.fed", "2026-02-03", "07:29", ""},
		{"apple mail, 413 cases",
			"On 10 Feb 2026, at 10:00, Katherine Barclay <katherine.ogawa@vulcan.fed> wrote:",
			"Katherine Barclay", "katherine.ogawa@vulcan.fed", "2026-02-10", "10:00", ""},
		{"gmail en-US month first",
			"On Wed, Aug 12, 2026 at 9:47 Reginald Rhys <selar.rhys@ferenginar.fed> wrote:",
			"Reginald Rhys", "selar.rhys@ferenginar.fed", "2026-08-12", "09:47", ""},
		{"comma instead of at",
			"On Fri, Mar 14, 2025, 6:12 Reginald Rhys <selar.rhys@ferenginar.fed> wrote:",
			"Reginald Rhys", "selar.rhys@ferenginar.fed", "2025-03-14", "06:12", ""},
		{"12-hour with meridiem",
			"On Mon, 2 Feb 2026 at 9:02 PM, Miles Quinteros <miles.elbrun@ferenginar.fed> wrote:",
			"Miles Quinteros", "miles.elbrun@ferenginar.fed", "2026-02-02", "21:02", ""},
		{"us style with at and meridiem",
			"On Jan 19, 2026, at 2:46 AM, Selar Culber <selar.booker@trill.fed> wrote:",
			"Selar Culber", "selar.booker@trill.fed", "2026-01-19", "02:46", ""},
		{"seconds and a timezone label",
			"On Wed, 19 Aug 2026 at 07:52:13 NZST, Ro Laren <ro.laren@daystrom.fed> wrote:",
			"Ro Laren", "ro.laren@daystrom.fed", "2026-08-19", "07:52", "NZST"},
		// Outlook renders a hyperlinked address as <addr <mailto:addr>>. Taking the
		// LAST bracketed span would capture "mailto:..." and taking the outermost
		// would capture both, so the mailto: form has to be understood, not stripped.
		{"outlook mailto hyperlink, 589 cases",
			"On Wed, 1 Oct 2025 at 13:46, Una Ishka <una.pike@betazed.fed <mailto:una.pike@betazed.fed>> wrote:",
			"Una Ishka", "una.pike@betazed.fed", "2025-10-01", "13:46", ""},
		{"bare address, no angle brackets",
			"On Wed, 19 Aug 2026 at 07:52, ro.laren@daystrom.fed wrote:",
			"", "ro.laren@daystrom.fed", "2026-08-19", "07:52", ""},
		{"no address at all",
			"On Tue, 3 Feb 2026 at 07:29, Reginald Stamets wrote:",
			"Reginald Stamets", "", "2026-02-03", "07:29", ""},
		{"full month name",
			"On Monday, 2 February 2026 at 14:03, Ro Laren <ro.laren@daystrom.fed> wrote:",
			"Ro Laren", "ro.laren@daystrom.fed", "2026-02-02", "14:03", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ParseAttribution(c.in)
			if got.Sender != c.sender {
				t.Errorf("sender = %q, want %q", got.Sender, c.sender)
			}
			if got.Address != c.addr {
				t.Errorf("address = %q, want %q", got.Address, c.addr)
			}
			if d := got.DateString(); d != c.date {
				t.Errorf("date = %q, want %q", d, c.date)
			}
			if k := got.ClockString(); k != c.clock {
				t.Errorf("clock = %q, want %q", k, c.clock)
			}
			if got.TZ != c.tz {
				t.Errorf("tz = %q, want %q", got.TZ, c.tz)
			}
		})
	}
}

// A sender is worthless if we cannot say when. Measure recall over the whole
// corpus so a regression in one dialect shows up as a number, not a vibe.
func TestAttributionRecallOverCorpus(t *testing.T) {
	var total, withDate, withAddr, withSender int
	for _, f := range fixtures(t) {
		for _, b := range Peel(f.Body) {
			if b.Kind != KindAttribution {
				continue
			}
			total++
			a := ParseAttribution(b.Sentinel)
			if !a.Sent.IsZero() {
				withDate++
			}
			if a.Address != "" {
				withAddr++
			}
			if a.Sender != "" {
				withSender++
			}
		}
	}
	t.Logf("attributions=%d date=%d (%.1f%%) address=%d (%.1f%%) sender=%d (%.1f%%)",
		total, withDate, 100*float64(withDate)/float64(total),
		withAddr, 100*float64(withAddr)/float64(total),
		withSender, 100*float64(withSender)/float64(total))
	if total == 0 {
		t.Fatal("no attributions in corpus")
	}
	// Set at the measured floor. Raise these when a dialect is added; never lower
	// them to make a change pass.
	if r := float64(withDate) / float64(total); r < 0.999 {
		t.Errorf("date recall %.4f below 0.999", r)
	}
	if r := float64(withAddr) / float64(total); r < 0.999 {
		t.Errorf("address recall %.4f below 0.999", r)
	}
	if r := float64(withSender) / float64(total); r < 0.99 {
		t.Errorf("sender recall %.4f below 0.99", r)
	}
}
