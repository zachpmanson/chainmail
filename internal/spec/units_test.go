package spec

import (
	"testing"
	"time"
)

// entryID must stay in lockstep with entryId in src/lib/anchors.ts: the ids
// emitted here are the anchors the renderer would have derived, and `parent`
// names them.
func TestEntryIDMirrorsTheRendererDerivation(t *testing.T) {
	cases := []struct {
		name string
		in   Entry
		want string
	}{
		{"date time initials", Entry{Date: "Thu 20 Aug 2026", Time: "14:40", Sender: "Zora Miller"}, "m-20260820-1440-zm"},
		{"three initials at most", Entry{Date: "Wed 15 Apr 2026", Time: "09:08", Sender: "Ana Bea Cyd Dee"}, "m-20260415-0908-abc"},
		{"punctuated name", Entry{Date: "Wed 15 Jul 2026", Time: "10:45", Sender: "Beverly Wells-Rhys"}, "m-20260715-1045-bwr"},
		{"note ignores the clock", Entry{Kind: "note", Date: "Mon 17 Aug 2026", Time: "11:00"}, "m-20260817-note"},
		{"no sender", Entry{Date: "Mon 17 Aug 2026", Time: "11:00"}, "m-20260817-1100-x"},
		{"no time", Entry{Date: "Mon 17 Aug 2026", Sender: "Ada Byron"}, "m-20260817-0000-ab"},
		{"unparseable date", Entry{Date: "sometime in 2026 or so", Sender: "Ada Byron"}, "m-undated-0000-ab"},
	}
	for _, c := range cases {
		if got := entryID(c.in); got != c.want {
			t.Errorf("%s: entryID = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestIDCollisionsAreSuffixedLikeTheRenderer(t *testing.T) {
	a := newIDAllocator()
	e := Entry{Date: "Thu 20 Aug 2026", Time: "14:40", Sender: "Zora Miller"}
	want := []string{"m-20260820-1440-zm", "m-20260820-1440-zm-2", "m-20260820-1440-zm-3"}
	for _, w := range want {
		if got := a.take(entryID(e)); got != w {
			t.Errorf("take = %q, want %q", got, w)
		}
	}
}

func TestTZMinutes(t *testing.T) {
	cases := []struct {
		in   string
		want int
		ok   bool
	}{
		{"+1000", 600, true},
		{"+05:30", 330, true},
		{"-0700", -420, true},
		{"AEST", 600, true},
		{"nzdt", 780, true},
		{"UTC", 0, true},
		{"", 0, false},
		{"XYZ", 0, false},
	}
	for _, c := range cases {
		got, ok := tzMinutes(c.in)
		if got != c.want || ok != c.ok {
			t.Errorf("tzMinutes(%q) = %d,%v, want %d,%v", c.in, got, ok, c.want, c.ok)
		}
	}
}

// A stated zone is displayed, never converted: the same instant reads
// differently depending on what the sender's mail client said. Where an offset
// was captured it decides, and the label follows it.
func TestStampRendersTheStatedZone(t *testing.T) {
	ts := time.Date(2026, 8, 17, 2, 30, 0, 0, time.UTC)
	cases := []struct {
		tz          string
		off         *int
		date, clock string
		label       string
		resolved    bool
	}{
		{tz: "AEST", date: "Mon 17 Aug 2026", clock: "12:30", label: "AEST", resolved: true},
		{tz: "+1200", date: "Mon 17 Aug 2026", clock: "14:30", label: "+1200", resolved: true},
		{tz: "+0000", date: "Mon 17 Aug 2026", clock: "02:30", label: "+0000", resolved: true},
		{tz: "-0700", date: "Sun 16 Aug 2026", clock: "19:30", label: "-0700", resolved: true},
		{tz: "", date: "Mon 17 Aug 2026", clock: "02:30", resolved: false},
		{tz: "XYZ", date: "Mon 17 Aug 2026", clock: "02:30", label: "XYZ", resolved: false},
		// An offset with no label at all is still enough to place the clock.
		{off: mins(-420), date: "Sun 16 Aug 2026", clock: "19:30", label: "-0700", resolved: true},
		// A zero offset is a fact the source stated, unlike a missing one.
		{off: mins(0), date: "Mon 17 Aug 2026", clock: "02:30", label: "+0000", resolved: true},
		{tz: "Asia/Kolkata", off: mins(330), date: "Mon 17 Aug 2026", clock: "08:00", label: "+0530", resolved: true},
		{tz: "IST", off: mins(330), date: "Mon 17 Aug 2026", clock: "08:00", label: "IST", resolved: true},
	}
	for _, c := range cases {
		date, clock, label, resolved := stamp(ts, c.tz, c.off)
		if date != c.date || clock != c.clock || label != c.label || resolved != c.resolved {
			t.Errorf("stamp(%q, %v) = %q %q %q %v, want %q %q %q %v",
				c.tz, c.off, date, clock, label, resolved, c.date, c.clock, c.label, c.resolved)
		}
	}
}

func TestOrgOf(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"ada@loomworks.example", "Loomworks"},
		{"bo@fjord.co.nz", "Fjord"},
		{"cy@fjord.com.au", "Fjord"},
		{"di@mail.loomworks.example", "Loomworks"},
		{"someone@gmail.com", ""},
		{"not-an-address", ""},
		{"eve@localhost", ""},
	}
	for _, c := range cases {
		if got := orgOf(c.in, nil); got != c.want {
			t.Errorf("orgOf(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	if got := orgOf("ada@loomworks.example", map[string]string{"loomworks.example": "The Loom"}); got != "The Loom" {
		t.Errorf("override ignored: %q", got)
	}
}

func TestRecipientLine(t *testing.T) {
	to := parseAddrList("Ada Byron <ada@loomworks.example>, bo@fjord.example")
	cc := parseAddrList(`"Okafor, Cy" <cy@loomworks.example>, ada@loomworks.example`)
	want := "Ada Byron, bo@fjord.example, cc Okafor, Cy, ada@loomworks.example"
	if got := recipientLine(to, cc); got != want {
		t.Errorf("recipientLine = %q, want %q", got, want)
	}
	// De-duplication is per field and by address: a repeat within To collapses,
	// while someone in both To and Cc is shown in both, as the header had it.
	dup := parseAddrList("Ada Byron <ada@loomworks.example>, ADA@loomworks.example")
	if got := recipientLine(dup, nil); got != "Ada Byron" {
		t.Errorf("recipientLine dedupe = %q", got)
	}
}

func TestParseAddrToleratesBrokenHeaders(t *testing.T) {
	cases := []struct {
		in   string
		name string
		addr string
	}{
		{"Ada Byron <ada@loomworks.example>", "Ada Byron", "ada@loomworks.example"},
		{"ADA@Loomworks.Example", "", "ada@loomworks.example"},
		{"Byron, Ada <ada@loomworks.example>", "Byron, Ada", "ada@loomworks.example"},
		{"Fjord Commercial", "Fjord Commercial", ""},
		{"", "", ""},
	}
	for _, c := range cases {
		got := parseAddr(c.in)
		if got.Name != c.name || got.Address != c.addr {
			t.Errorf("parseAddr(%q) = %+v, want %q/%q", c.in, got, c.name, c.addr)
		}
	}
}

func TestHumanSizeAndKind(t *testing.T) {
	sizes := []struct {
		in   int64
		want string
	}{{0, ""}, {717, "717 B"}, {1953, "1.9 KB"}, {17408, "17 KB"}, {5 << 20, "5.0 MB"}}
	for _, c := range sizes {
		if got := humanSize(c.in); got != c.want {
			t.Errorf("humanSize(%d) = %q, want %q", c.in, got, c.want)
		}
	}
	kinds := []struct {
		mime, name, want string
	}{
		{"text/csv", "a.csv", "CSV"},
		{"text/calendar; charset=utf-8", "invite.ics", "calendar"},
		{"image/png", "image001.png", "image"},
		{"application/octet-stream", "report.xlsx", "XLSX"},
		{"", "notes.md", "MD"},
		{"", "noext", ""},
	}
	for _, c := range kinds {
		if got := attachmentKind(c.mime, c.name); got != c.want {
			t.Errorf("attachmentKind(%q,%q) = %q, want %q", c.mime, c.name, got, c.want)
		}
	}
}
