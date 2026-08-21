package slackingest

import (
	"testing"
	"time"
)

// The microseconds in a ts are what make it unique within a channel, so losing
// them would merge two messages sent in the same second into one entry.
func TestParseTSKeepsSubSecondPrecision(t *testing.T) {
	a, err := ParseTS("1700000000.000100")
	if err != nil {
		t.Fatal(err)
	}
	b, err := ParseTS("1700000000.000200")
	if err != nil {
		t.Fatal(err)
	}
	if !a.Before(b) {
		t.Fatalf("%v is not before %v", a, b)
	}
	if a.Unix() != 1_700_000_000 {
		t.Fatalf("seconds %d", a.Unix())
	}
	if got := b.Sub(a); got != 100*time.Microsecond {
		t.Fatalf("gap %v, want 100µs", got)
	}
	if _, err := ParseTS("not-a-ts"); err == nil {
		t.Fatal("a malformed ts parsed without complaint")
	}
}

func TestPermalinkDropsTheDot(t *testing.T) {
	got := Permalink("northwind", "C0GENERAL", "1700000000.000100")
	want := "https://northwind.slack.com/archives/C0GENERAL/p1700000000000100"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	// No workspace means no link. A permalink into the wrong workspace is worse
	// than none, because it looks clickable.
	if got := Permalink("", "C0GENERAL", "1700000000.000100"); got != "" {
		t.Fatalf("got %q, want empty", got)
	}
}

func TestWorkspaceComesFromTheArchive(t *testing.T) {
	f := newFixture(t)
	f.workspace("https://southwind.slack.com/")
	got, err := f.open().Workspace()
	if err != nil {
		t.Fatal(err)
	}
	if got != "southwind" {
		t.Fatalf("workspace %q, want southwind", got)
	}
}

// The same message is re-recorded by every archive run that covers its channel.
// An older chunk is a stale copy: text can be edited and reply_count grows.
func TestNewestChunkWinsForADuplicateMessage(t *testing.T) {
	f := newFixture(t)
	f.people()
	f.channel(general, "general", false)
	f.message(1, general, "1700000000.000100", "U100", "the first pass", nil)
	f.message(7, general, "1700000000.000100", "U100", "the later pass",
		map[string]any{"reply_count": 3})

	var seen []Message
	if err := f.open().Messages(func(m Message) error {
		seen = append(seen, m)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 1 {
		t.Fatalf("walked %d messages, want the duplicate collapsed to 1", len(seen))
	}
	if seen[0].Text != "the later pass" || seen[0].ReplyCount != 3 {
		t.Fatalf("got %+v, want the newest chunk", seen[0])
	}
}

func TestOpenArchiveRejectsSomethingElse(t *testing.T) {
	f := newFixture(t)
	if _, err := f.db.Exec(`drop table MESSAGE`); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenArchive(f.path); err == nil {
		t.Fatal("a database with no MESSAGE table was accepted as an archive")
	}
	if _, err := OpenArchive(f.path + ".nope"); err == nil {
		t.Fatal("a missing archive was accepted")
	}
}

func TestPlainText(t *testing.T) {
	names := Names{
		User: func(id string) string {
			return map[string]string{"U100": "Ada Fenwick", "U200": "Bo Kestrel"}[id]
		},
		Channel: func(id string) string {
			return map[string]string{"C0GENERAL": "general"}[id]
		},
	}
	for _, tc := range []struct{ name, in, want string }{
		{"mention resolves to a name",
			"<@U100> can you check", "@Ada Fenwick can you check"},
		{"unknown mention keeps the id rather than dropping the reference",
			"<@U999> ?", "@U999 ?"},
		{"mention with a stale inline label prefers the archive's name",
			"<@U100|ada.old> hi", "@Ada Fenwick hi"},
		{"channel reference",
			"see <#C0GENERAL>", "see #general"},
		{"channel reference with a label",
			"see <#C0OTHER|tenders>", "see #tenders"},
		{"broadcast",
			"<!here> heads up", "@here heads up"},
		{"subteam keeps the label it displayed",
			"<!subteam^S1|@ops> ping", "@ops ping"},
		{"a bare link is just the url",
			"<https://x.fed/a>", "https://x.fed/a"},
		{"a labelled link keeps both, so either is searchable",
			"<https://x.fed/a|the audit>", "the audit (https://x.fed/a)"},
		{"mailto renders as the address",
			"<mailto:ada@northwind.fed|ada@northwind.fed>", "ada@northwind.fed"},
		{"entities are unescaped",
			"a &lt; b &amp;&amp; c &gt; d", "a < b && c > d"},
		{"an escaped mention is text, not a mention",
			"type &lt;@U100&gt; to ping", "type <@U100> to ping"},
		{"plain prose is untouched",
			"off by 40 kWh", "off by 40 kWh"},
	} {
		if got := PlainText(tc.in, names); got != tc.want {
			t.Errorf("%s:\n  in   %q\n  got  %q\n  want %q", tc.name, tc.in, got, tc.want)
		}
	}
}

func TestDisplayNamePrefersTheMostHumanName(t *testing.T) {
	if got := (User{ID: "U1", Name: "ada", RealName: "Ada Fenwick"}).DisplayName(); got != "Ada Fenwick" {
		t.Fatalf("got %q", got)
	}
	if got := (User{ID: "U1", Name: "ada"}).DisplayName(); got != "ada" {
		t.Fatalf("got %q", got)
	}
	// Never nameless: a bare id reads badly but is still an identity.
	if got := (User{ID: "U1"}).DisplayName(); got != "U1" {
		t.Fatalf("got %q", got)
	}
}
