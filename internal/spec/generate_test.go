package spec

import (
	"strings"
	"testing"
	"time"

	"github.com/zachpmanson/chainmail/internal/corpus"
)

// All test data here is invented. Real correspondence is never committed: the
// only checks against a real trail are run against an untracked fixture.

func open(t *testing.T) *corpus.Store {
	t.Helper()
	s, err := corpus.Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// person creates a person keyed on an address, as the mail ingest would.
func person(t *testing.T, s *corpus.Store, name, addr string) int64 {
	t.Helper()
	res, err := s.DB().Exec(`insert into people (display_name) values (?)`, name)
	if err != nil {
		t.Fatalf("insert person: %v", err)
	}
	id, _ := res.LastInsertId()
	if _, err := s.DB().Exec(
		`insert into identities (person_id, kind, value, rule) values (?,?,?,?)`,
		id, "email", addr, "test"); err != nil {
		t.Fatalf("insert identity: %v", err)
	}
	return id
}

type msg struct {
	ext       string
	ts        string // RFC3339, with the offset the sender stated
	tz        string
	offset    *int // minutes east of UTC, as the Date header carried it
	person    int64
	container string
	subject   string
	messageID string
	inReplyTo string
	from      string
	to        string
	cc        string
	gmail     string
	atts      []corpus.Attachment
}

func put(t *testing.T, s *corpus.Store, m msg) int64 {
	t.Helper()
	ts, err := time.Parse(time.RFC3339, m.ts)
	if err != nil {
		t.Fatalf("bad ts %q: %v", m.ts, err)
	}
	res, err := s.Put(corpus.Entry{
		Source: corpus.SourceMail, ExtID: m.ext, TS: ts, TZ: m.tz, TZOffset: m.offset,
		PersonID: m.person, Container: m.container, Subject: m.subject,
		ParentRef: m.inReplyTo, BodyText: "invented body",
	}, &corpus.Mail{
		GmailID: m.gmail, MessageID: m.messageID, InReplyTo: m.inReplyTo,
		From: m.from, To: m.to, Cc: m.cc,
	}, m.atts)
	if err != nil {
		t.Fatalf("Put %s: %v", m.ext, err)
	}
	return res.ID
}

// trail is a small invented corpus: one thread of three, plus an unrelated one.
func trail(t *testing.T) *corpus.Store {
	s := open(t)
	ida := person(t, s, "Ada Byron", "ada@loomworks.example")
	idb := person(t, s, "Bo Halvorsen", "bo@fjordline.example")

	a := put(t, s, msg{
		ext: "mail:<a@loomworks>", ts: "2026-03-02T09:15:00+11:00", tz: "AEDT",
		person: ida, container: "T1", subject: "Loom cutover",
		messageID: "<a@loomworks>", from: "Ada Byron <ada@loomworks.example>",
		to: "Bo Halvorsen <bo@fjordline.example>",
		cc: "Cy Okafor <cy@loomworks.example>", gmail: "g-a",
		atts: []corpus.Attachment{{Name: "plan.csv", Mime: "text/csv", Size: 717}},
	})
	b := put(t, s, msg{
		ext: "mail:<b@fjordline>", ts: "2026-03-02T23:40:00+01:00", tz: "+0100",
		person: idb, container: "T1", subject: "Loom cutover",
		messageID: "<b@fjordline>", inReplyTo: "<a@loomworks>",
		from: "Bo Halvorsen <bo@fjordline.example>",
		to:   "Ada Byron <ada@loomworks.example>", gmail: "g-b",
	})
	c := put(t, s, msg{
		ext: "mail:<c@loomworks>", ts: "2026-03-03T10:00:00+11:00", tz: "AEDT",
		person: ida, container: "T1", subject: "Loom cutover: dates",
		messageID: "<c@loomworks>", inReplyTo: "<b@fjordline>",
		from: "Ada Byron <ada@loomworks.example>",
		to:   "Bo Halvorsen <bo@fjordline.example>", gmail: "g-c",
	})
	put(t, s, msg{
		ext: "mail:<z@loomworks>", ts: "2026-04-01T08:00:00+11:00", tz: "AEDT",
		person: ida, container: "T9", subject: "Unrelated",
		messageID: "<z@loomworks>", from: "Ada Byron <ada@loomworks.example>",
		to: "Bo Halvorsen <bo@fjordline.example>", gmail: "g-z",
	})
	for _, id := range []int64{a, b, c} {
		if err := s.Sight(id, 0, "direct", ""); err != nil {
			t.Fatalf("Sight: %v", err)
		}
	}
	if _, err := s.ResolveParents(); err != nil {
		t.Fatalf("ResolveParents: %v", err)
	}
	return s
}

func generate(t *testing.T, s *corpus.Store, opts Options) Spec {
	t.Helper()
	sp, err := Generate(s, opts)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	return sp
}

func TestGenerateJoinsTheMechanicalFields(t *testing.T) {
	s := trail(t)
	sp := generate(t, s, Options{Containers: []string{"T1"}, Me: []string{"ada@loomworks.example"}})

	if len(sp.Messages) != 3 {
		t.Fatalf("messages = %d, want 3", len(sp.Messages))
	}
	first := sp.Messages[0]
	// Rendered in the zone the sender stated, not converted anywhere.
	if first.Date != "Mon 2 Mar 2026" || first.Time != "09:15" || first.TZ != "AEDT" {
		t.Errorf("stamp = %q %q %q, want Mon 2 Mar 2026 09:15 AEDT", first.Date, first.Time, first.TZ)
	}
	if first.Sender != "Ada Byron" || first.Org != "Loomworks" || first.FromEmail != "ada@loomworks.example" {
		t.Errorf("who = %q/%q/%q", first.Sender, first.Org, first.FromEmail)
	}
	if first.To != "Bo Halvorsen, cc Cy Okafor" {
		t.Errorf("to = %q", first.To)
	}
	if first.Subject != "Loom cutover" {
		t.Errorf("subject = %q, want the chain opener's subject", first.Subject)
	}
	if first.GmailID != "g-a" || first.ThreadID != "T1" || first.Source != "msg g-a" {
		t.Errorf("provenance = %q/%q/%q", first.GmailID, first.ThreadID, first.Source)
	}
	if !first.Me {
		t.Error("me = false for an address given in Options.Me")
	}
	if first.Quoted {
		t.Error("quoted = true for an entry with a direct sighting")
	}
	if len(first.Attachments) != 1 || first.Attachments[0] != (Attachment{
		Name: "plan.csv", Kind: "CSV", Size: "717 B", GmailID: "g-a",
	}) {
		t.Errorf("attachments = %+v", first.Attachments)
	}

	// The second entry states an offset rather than an abbreviation; it must be
	// rendered at that offset, which is the day after the first entry locally.
	second := sp.Messages[1]
	if second.Date != "Mon 2 Mar 2026" || second.Time != "23:40" || second.TZ != "+0100" {
		t.Errorf("second stamp = %q %q %q", second.Date, second.Time, second.TZ)
	}
	if second.Me {
		t.Error("me = true for someone else's address")
	}
	if second.Subject != "" {
		t.Errorf("subject = %q, want empty: it continues its parent's chain", second.Subject)
	}
	if second.Parent != first.ID {
		t.Errorf("parent = %q, want %q", second.Parent, first.ID)
	}
	// A changed subject names a new chain even mid-thread.
	if sp.Messages[2].Subject != "Loom cutover: dates" {
		t.Errorf("third subject = %q", sp.Messages[2].Subject)
	}
}

func TestGenerateLeavesInterpretationEmpty(t *testing.T) {
	s := trail(t)
	sp := generate(t, s, Options{Containers: []string{"T1"}})

	if sp.Subtitle != "" || sp.OpenItems != nil {
		t.Errorf("subtitle/openItems must be left for a human: %q %v", sp.Subtitle, sp.OpenItems)
	}
	for _, m := range sp.Messages {
		// A body carries the message's own words, rendered — never a gloss written
		// about it.
		if m.Body != "<p>invented body</p>" {
			t.Errorf("body = %q, want the stored text rendered and nothing added", m.Body)
		}
		if m.Mentions != nil || m.Label != "" || m.Meta {
			t.Errorf("interpretive field set on %s", m.ID)
		}
	}
}

func TestGenerateCarriesTheStoredTextIntoTheBody(t *testing.T) {
	s := open(t)
	id := person(t, s, "Ada Byron", "ada@loomworks.example")
	// A hard-wrapped paragraph with an address in angle brackets under it: raw,
	// the address is an unknown tag and disappears from the page.
	body := "Confirming that the meter reads for both of the sites landed inside\n" +
		"the billing period we agreed.\n\nAda <ada@loomworks.example>"
	res, err := s.Put(corpus.Entry{
		Source: corpus.SourceMail, ExtID: "mail:<w@loomworks>",
		TS: time.Date(2026, 5, 4, 9, 0, 0, 0, time.UTC), TZ: "AEST",
		PersonID: id, Container: "T3", Subject: "Meter reads", BodyText: body,
	}, &corpus.Mail{
		GmailID: "g-w", MessageID: "<w@loomworks>",
		From: "Ada Byron <ada@loomworks.example>", To: "bo@fjordline.example",
	}, nil)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := s.Sight(res.ID, 0, "direct", ""); err != nil {
		t.Fatalf("Sight: %v", err)
	}
	sp := generate(t, s, Options{Containers: []string{"T3"}})
	want := "<p>Confirming that the meter reads for both of the sites landed inside " +
		"the billing period we agreed.</p><p>Ada &lt;ada@loomworks.example&gt;</p>"
	if sp.Messages[0].Body != want {
		t.Errorf("body = %q,\nwant %q", sp.Messages[0].Body, want)
	}
}

func TestGenerateOrdersByAbsoluteTimeAndNumbersOneThread(t *testing.T) {
	s := trail(t)
	sp := generate(t, s, Options{Containers: []string{"T1"}, Title: "Cutover"})

	if sp.Title != "Cutover" || sp.SpecVersion != 1 {
		t.Errorf("title/version = %q/%d", sp.Title, sp.SpecVersion)
	}
	want := []string{"msg g-a", "msg g-b", "msg g-c"}
	for i, m := range sp.Messages {
		if m.Source != want[i] {
			t.Errorf("message %d = %s, want %s (chronological by absolute UTC)", i, m.Source, want[i])
		}
	}
	if len(sp.Threads) != 1 {
		t.Fatalf("threads = %+v", sp.Threads)
	}
	th := sp.Threads[0]
	if th.ID != "T1" || th.Count != 3 || th.Subject != "Loom cutover" {
		t.Errorf("thread = %+v", th)
	}
	if th.Span != "2 Mar 2026 – 3 Mar 2026" {
		t.Errorf("span = %q", th.Span)
	}
}

func TestGenerateTitleFallsBackToTheEarliestSubject(t *testing.T) {
	s := trail(t)
	sp := generate(t, s, Options{Containers: []string{"T1"}})
	if sp.Title != "Loom cutover" {
		t.Errorf("title = %q, want the earliest entry's subject", sp.Title)
	}
}

func TestClosurePullsInTheWholeReplyGraph(t *testing.T) {
	s := trail(t)
	// Seeded with the last reply only: its ancestors, and their other branches,
	// must come along or the page shows answers without questions.
	var id int64
	if err := s.DB().QueryRow(
		`select id from entries where ext_id = 'mail:<c@loomworks>'`).Scan(&id); err != nil {
		t.Fatalf("lookup: %v", err)
	}
	sp := generate(t, s, Options{EntryIDs: []int64{id}})
	if len(sp.Messages) != 3 {
		t.Fatalf("messages = %d, want the 3 in that chain", len(sp.Messages))
	}
	for _, m := range sp.Messages {
		if m.ThreadID != "T1" {
			t.Errorf("unrelated thread pulled in: %+v", m)
		}
	}
}

func TestSelectByExtID(t *testing.T) {
	s := trail(t)
	sp := generate(t, s, Options{ExtIDs: []string{"mail:<z@loomworks>"}})
	if len(sp.Messages) != 1 || sp.Messages[0].ThreadID != "T9" {
		t.Fatalf("messages = %+v", sp.Messages)
	}
}

func TestGenerateRefusesAnEmptySelection(t *testing.T) {
	s := trail(t)
	if _, err := Generate(s, Options{}); err == nil {
		t.Fatal("want an error: the schema requires at least one message")
	}
	if _, err := Generate(s, Options{Containers: []string{"nope"}}); err == nil {
		t.Fatal("want an error for a container that selects nothing")
	}
}

func TestQuotedIsTrueWithoutADirectSighting(t *testing.T) {
	s := trail(t)
	// An entry recovered only from someone's quoted history: no direct sighting,
	// and a source that names the message it was found inside.
	var host int64
	if err := s.DB().QueryRow(
		`select id from entries where ext_id = 'mail:<a@loomworks>'`).Scan(&host); err != nil {
		t.Fatalf("lookup: %v", err)
	}
	q := put(t, s, msg{
		ext: "quote:sha-1", ts: "2026-03-01T08:00:00+11:00", tz: "AEDT",
		container: "T1", subject: "Loom cutover", inReplyTo: "",
		from: "Cy Okafor <cy@loomworks.example>", to: "Ada Byron <ada@loomworks.example>",
	})
	if err := s.Sight(q, host, "quoted", ""); err != nil {
		t.Fatalf("Sight: %v", err)
	}
	sp := generate(t, s, Options{Containers: []string{"T1"}})

	got := sp.Messages[0]
	if !got.Quoted {
		t.Errorf("quoted = false for an entry with no direct sighting: %+v", got)
	}
	if got.Source != "unspooled from msg g-a" {
		t.Errorf("source = %q", got.Source)
	}
	if got.Sender != "Cy Okafor" {
		t.Errorf("sender = %q, want the From display name when no person is resolved", got.Sender)
	}
	if len(sp.SourceNotes) == 0 {
		t.Fatal("want a coverage note naming the reconstructed entries")
	}
}

func TestAttachmentGmailIDOnlyOnRealMessages(t *testing.T) {
	s := trail(t)
	q := put(t, s, msg{
		ext: "quote:sha-2", ts: "2026-03-01T08:00:00+11:00", tz: "AEDT",
		container: "T2", subject: "Quoted with a file",
		from: "Cy Okafor <cy@loomworks.example>", gmail: "g-a",
		atts: []corpus.Attachment{{Name: "old.pdf", Mime: "application/pdf", Size: 4096}},
	})
	if err := s.Sight(q, 0, "quoted", ""); err != nil {
		t.Fatalf("Sight: %v", err)
	}
	sp := generate(t, s, Options{Containers: []string{"T2"}})
	att := sp.Messages[0].Attachments[0]
	if att.GmailID != "" {
		t.Errorf("gmailId = %q on a reconstructed entry: it cannot be opened in Gmail", att.GmailID)
	}
	if att.Kind != "PDF" || att.Size != "4.0 KB" {
		t.Errorf("attachment = %+v", att)
	}
}

func TestDuplicateAttachmentPartsCollapse(t *testing.T) {
	s := open(t)
	id := put(t, s, msg{
		ext: "mail:<inv@loomworks>", ts: "2026-03-02T09:15:00+11:00", tz: "AEDT",
		container: "T3", subject: "Invite", from: "Ada Byron <ada@loomworks.example>",
		gmail: "g-i",
		// A calendar invite arrives as the same file under two MIME types.
		atts: []corpus.Attachment{
			{Name: "invite.ics", Mime: "text/calendar", Size: 1953},
			{Name: "invite.ics", Mime: "application/ics", Size: 1953},
		},
	})
	if err := s.Sight(id, 0, "direct", ""); err != nil {
		t.Fatalf("Sight: %v", err)
	}
	sp := generate(t, s, Options{Containers: []string{"T3"}})
	atts := sp.Messages[0].Attachments
	if len(atts) != 1 || atts[0].Size != "1.9 KB" || atts[0].Kind != "calendar" {
		t.Errorf("attachments = %+v, want one collapsed chip", atts)
	}
}

func TestTZOmittedWhenTheStoreHasNone(t *testing.T) {
	s := open(t)
	// An unparseable Date header leaves no zone. The clock can then only be UTC,
	// and tz must be absent so the renderer infers one and labels it inferred.
	id := put(t, s, msg{
		ext: "mail:<nozone@loomworks>", ts: "2026-03-02T09:15:00Z",
		container: "T4", subject: "No zone stated",
		from: "Ada Byron <ada@loomworks.example>", gmail: "g-n",
	})
	if err := s.Sight(id, 0, "direct", ""); err != nil {
		t.Fatalf("Sight: %v", err)
	}
	sp := generate(t, s, Options{Containers: []string{"T4"}})
	got := sp.Messages[0]
	if got.TZ != "" {
		t.Errorf("tz = %q, want it omitted rather than guessed", got.TZ)
	}
	if got.Time != "09:15" {
		t.Errorf("time = %q", got.Time)
	}
}

func TestUnresolvableZoneIsStatedAndFlagged(t *testing.T) {
	s := open(t)
	id := put(t, s, msg{
		ext: "mail:<odd@loomworks>", ts: "2026-03-02T09:15:00Z", tz: "XYZ",
		container: "T5", subject: "Odd zone",
		from: "Ada Byron <ada@loomworks.example>", gmail: "g-o",
	})
	if err := s.Sight(id, 0, "direct", ""); err != nil {
		t.Fatalf("Sight: %v", err)
	}
	sp := generate(t, s, Options{Containers: []string{"T5"}})
	if got := sp.Messages[0].TZ; got != "XYZ" {
		t.Errorf("tz = %q, want the label as stated", got)
	}
	if got := sp.Messages[0].Time; got != "09:15" {
		t.Errorf("time = %q, want UTC when the label yields no offset", got)
	}
	if len(sp.SourceNotes) == 0 {
		t.Fatal("an unrenderable zone must be declared, not hidden")
	}
}

// mins is minutes east of UTC, as a nullable offset column holds it.
func mins(n int) *int { return &n }

// one stores a single entry, sights it as a real message, and generates the
// spec its container renders to.
func one(t *testing.T, m msg) Spec {
	t.Helper()
	s := open(t)
	id := put(t, s, m)
	if err := s.Sight(id, 0, "direct", ""); err != nil {
		t.Fatalf("Sight: %v", err)
	}
	return generate(t, s, Options{Containers: []string{m.container}})
}

// zoneItems returns the coverage items that talk about zones, which is where a
// clock the tool could not place has to be declared.
func zoneItems(sp Spec) []string {
	var out []string
	for _, n := range sp.SourceNotes {
		for _, it := range n.Items {
			if strings.Contains(strings.ToLower(it), "zone") {
				out = append(out, it)
			}
		}
	}
	return out
}

// The stored offset came off the source's own Date header, so it outranks the
// label: an entry whose label no table knows must still render the sender's
// clock. A label is also emitted in the numeric form of the offset displayed,
// because schema/timeline.schema.json makes tz the renderer's ordering key — a
// label the renderer cannot resolve, or one implying a different offset, would
// order the entry by an inferred zone while the clock beside it read correctly.
func TestStoredOffsetRendersTheClockWhenTheLabelCannot(t *testing.T) {
	cases := []struct {
		name        string
		tz          string
		offset      *int
		ts          string
		date, clock string
		wantTZ      string
	}{{
		name: "an IANA name, which no abbreviation table holds",
		tz:   "Pacific/Auckland", offset: mins(720), ts: "2026-08-16T21:30:00Z",
		date: "Mon 17 Aug 2026", clock: "09:30", wantTZ: "+1200",
	}, {
		name: "a three-quarter-hour offset",
		tz:   "Asia/Kathmandu", offset: mins(345), ts: "2026-08-16T21:30:00Z",
		date: "Mon 17 Aug 2026", clock: "03:15", wantTZ: "+0545",
	}, {
		name: "a half-hour offset",
		tz:   "Australia/Adelaide", offset: mins(570), ts: "2026-08-16T21:30:00Z",
		date: "Mon 17 Aug 2026", clock: "07:00", wantTZ: "+0930",
	}, {
		name: "a negative offset, which lands on the previous day",
		tz:   "America/Los_Angeles", offset: mins(-420), ts: "2026-08-16T21:30:00Z",
		date: "Sun 16 Aug 2026", clock: "14:30", wantTZ: "-0700",
	}, {
		// A label that agrees with the offset carries more for a reader than
		// "+1200" does, and the renderer resolves it to the same minutes.
		name: "a label agreeing with the offset, kept as stated",
		tz:   "NZST", offset: mins(720), ts: "2026-08-16T21:30:00Z",
		date: "Mon 17 Aug 2026", clock: "09:30", wantTZ: "NZST",
	}, {
		// Daylight saving mislabelled by the sender's client. Displaying "NZST"
		// beside a 10:30 clock would have the renderer order the entry an hour
		// early, so the label yields to the offset it disagrees with.
		name: "a label an hour out from the offset",
		tz:   "NZST", offset: mins(780), ts: "2026-08-16T21:30:00Z",
		date: "Mon 17 Aug 2026", clock: "10:30", wantTZ: "+1300",
	}}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sp := one(t, msg{
				ext: "mail:<off@loomworks>", ts: c.ts, tz: c.tz, offset: c.offset,
				container: "T6", subject: "Stored offset",
				from: "Ada Byron <ada@loomworks.example>", gmail: "g-off",
			})
			got := sp.Messages[0]
			if got.Date != c.date || got.Time != c.clock || got.TZ != c.wantTZ {
				t.Errorf("stamp = %q %q %q, want %q %q %q",
					got.Date, got.Time, got.TZ, c.date, c.clock, c.wantTZ)
			}
			if items := zoneItems(sp); len(items) > 0 {
				t.Errorf("zone caveat on a renderable clock: %v", items)
			}
		})
	}
}

// The offset column is nullable, and a null means "the source stated none" —
// never zero, which would pass a UTC clock off as the sender's own.
func TestNullOffsetFallsBackToTheLabelNotToUTC(t *testing.T) {
	for _, tz := range []string{"NZST", "+0545"} {
		sp := one(t, msg{
			ext: "mail:<nulloff@loomworks>", ts: "2026-08-16T21:30:00Z", tz: tz,
			container: "T7", subject: "No offset stored",
			from: "Ada Byron <ada@loomworks.example>", gmail: "g-nul",
		})
		got := sp.Messages[0]
		if got.Time == "21:30" {
			t.Fatalf("tz %q with a null offset rendered at UTC", tz)
		}
		if got.TZ != tz {
			t.Errorf("tz = %q, want the label as stated", got.TZ)
		}
	}
	// The same two entries, checked against the clock the label implies.
	cases := map[string]string{"NZST": "09:30", "+0545": "03:15"}
	for tz, clock := range cases {
		sp := one(t, msg{
			ext: "mail:<nulloff@loomworks>", ts: "2026-08-16T21:30:00Z", tz: tz,
			container: "T7", subject: "No offset stored",
			from: "Ada Byron <ada@loomworks.example>", gmail: "g-nul",
		})
		if got := sp.Messages[0].Time; got != clock {
			t.Errorf("tz %q rendered %q, want %q", tz, got, clock)
		}
	}
}

// Neither an offset nor a readable label, and no quoting client to borrow one
// from, leaves the clock as a bare wall clock. No label is emitted for it and
// the entries it happens to are counted in the coverage notes, so a reader can
// see that those times are not comparable with the rest.
func TestNoZoneAndNoOffsetIsDeclared(t *testing.T) {
	sp := one(t, msg{
		ext: "mail:<blind@loomworks>", ts: "2026-08-16T21:30:00Z",
		container: "T8", subject: "Nothing stated",
		from: "Ada Byron <ada@loomworks.example>", gmail: "g-bl",
	})
	got := sp.Messages[0]
	if got.Time != "21:30" || got.TZ != "" {
		t.Errorf("stamp = %q %q, want a UTC clock and no label", got.Time, got.TZ)
	}
	if items := zoneItems(sp); len(items) == 0 {
		t.Fatalf("a UTC clock nothing supports must be declared: %+v", sp.SourceNotes)
	}
}

func TestParticipantsIncludeRecipientOnlyPeople(t *testing.T) {
	s := trail(t)
	sp := generate(t, s, Options{Containers: []string{"T1"}})

	want := map[string]Participant{
		"Ada Byron":    {Name: "Ada Byron", Org: "Loomworks", Email: "ada@loomworks.example"},
		"Bo Halvorsen": {Name: "Bo Halvorsen", Org: "Fjordline", Email: "bo@fjordline.example"},
		"Cy Okafor":    {Name: "Cy Okafor", Org: "Loomworks", Email: "cy@loomworks.example"},
	}
	if len(sp.Participants) != len(want) {
		t.Fatalf("participants = %+v", sp.Participants)
	}
	for _, p := range sp.Participants {
		if w, ok := want[p.Name]; !ok || p != w {
			t.Errorf("participant %+v, want %+v", p, w)
		}
	}
	// First-appearance order: the opener's sender, then their recipients.
	if sp.Participants[0].Name != "Ada Byron" {
		t.Errorf("first participant = %q", sp.Participants[0].Name)
	}
}

func TestParentEdgeIsAbsentWhenTheParentIsNotInTheSpec(t *testing.T) {
	s := trail(t)
	sp := generate(t, s, Options{ExtIDs: []string{"mail:<z@loomworks>"}})
	if sp.Messages[0].Parent != "" {
		t.Errorf("parent = %q, want none", sp.Messages[0].Parent)
	}
}
