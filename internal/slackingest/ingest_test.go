package slackingest

import (
	"database/sql"
	"testing"

	"github.com/zachpmanson/chainmail/internal/corpus"
	"github.com/zachpmanson/chainmail/internal/mailingest"
)

const (
	general = "C0GENERAL"
	other   = "C0OTHER"
	dm      = "D0ADABO"
)

// people is the cast for most tests: two with addresses, one without, one bot.
func (f *fixture) people() {
	f.user("U100", "ada", "Ada Fenwick", "ada@northwind.fed", "Pacific/Auckland", false)
	f.user("U200", "bo", "Bo Kestrel", "bo@northwind.fed", "Australia/Brisbane", false)
	f.user("U300", "casper", "Casper Vole", "", "", false)
	f.user("B900", "hookbot", "Hook Bot", "", "", true)
}

func entryID(t *testing.T, s *corpus.Store, extID string) int64 {
	t.Helper()
	var id int64
	if err := s.DB().QueryRow(
		`select id from entries where ext_id=?`, extID).Scan(&id); err != nil {
		t.Fatalf("entry %s: %v", extID, err)
	}
	return id
}

func parentOf(t *testing.T, s *corpus.Store, extID string) (int64, bool) {
	t.Helper()
	var parent sql.NullInt64
	if err := s.DB().QueryRow(
		`select parent_id from entries where ext_id=?`, extID).Scan(&parent); err != nil {
		t.Fatalf("entry %s: %v", extID, err)
	}
	return parent.Int64, parent.Valid
}

// A thread is the only reply graph Slack gives us, so if this does not link the
// corpus has no Slack conversations at all — only a heap of messages.
func TestThreadRepliesLinkToTheirParent(t *testing.T) {
	f := newFixture(t)
	f.people()
	f.channel(general, "general", false)
	f.message(1, general, "1700000000.000100", "U100", "who owns the meter audit?", nil)
	f.message(1, general, "1700000100.000200", "U200", "I do",
		map[string]any{"thread_ts": "1700000000.000100"})
	f.message(1, general, "1700000200.000300", "U100", "thanks",
		map[string]any{"thread_ts": "1700000000.000100"})

	s := store(t)
	r := f.ingest(s)
	if r.Resolved != 2 {
		t.Fatalf("resolved %d thread edges, want 2", r.Resolved)
	}

	root := entryID(t, s, ExtID(general, "1700000000.000100"))
	for _, ts := range []string{"1700000100.000200", "1700000200.000300"} {
		got, ok := parentOf(t, s, ExtID(general, ts))
		if !ok || got != root {
			t.Fatalf("reply %s: parent %d (set=%v), want %d", ts, got, ok, root)
		}
	}
}

// Slack sets thread_ts on the parent as well as on the replies, so the parent
// names itself. Treating that as a reply edge would make it its own parent and
// every walk of the graph a cycle.
func TestThreadParentIsNotItsOwnParent(t *testing.T) {
	f := newFixture(t)
	f.people()
	f.channel(general, "general", false)
	// A parent as slackdump records it once the thread has replies: thread_ts
	// equal to its own ts, with a reply count.
	f.message(1, general, "1700000000.000100", "U100", "who owns the meter audit?",
		map[string]any{"thread_ts": "1700000000.000100", "reply_count": 1})
	f.message(1, general, "1700000100.000200", "U200", "I do",
		map[string]any{"thread_ts": "1700000000.000100"})

	s := store(t)
	f.ingest(s)

	ext := ExtID(general, "1700000000.000100")
	if _, ok := parentOf(t, s, ext); ok {
		t.Fatal("thread parent was given a parent")
	}
	var ref sql.NullString
	var replies int
	if err := s.DB().QueryRow(`
		select e.parent_ref, sd.reply_count from entries e
		join slack_detail sd on sd.entry_id = e.id where e.ext_id=?`,
		ext).Scan(&ref, &replies); err != nil {
		t.Fatal(err)
	}
	if ref.Valid {
		t.Fatalf("thread parent kept parent_ref %q; a root has no parent to name", ref.String)
	}
	// thread_ts is still recorded: it is the thread's id, and the fact that this
	// message leads one is worth keeping even though it is not an edge.
	var threadTS string
	if err := s.DB().QueryRow(`
		select sd.thread_ts from slack_detail sd join entries e on e.id = sd.entry_id
		where e.ext_id=?`, ext).Scan(&threadTS); err != nil {
		t.Fatal(err)
	}
	if threadTS != "1700000000.000100" || replies != 1 {
		t.Fatalf("slack_detail: thread_ts %q, reply_count %d", threadTS, replies)
	}
}

// A ts is unique within a channel and nowhere else. Resolution scoped only on ts
// would hang a reply off a stranger's message in an unrelated channel — a wrong
// answer that looks exactly like a right one.
func TestSameTimestampInTwoChannelsDoesNotCollide(t *testing.T) {
	f := newFixture(t)
	f.people()
	f.channel(general, "general", false)
	f.channel(other, "tenders", false)
	const shared = "1700000000.000100"
	// One chunk per channel, as slackdump writes them: a chunk covers a single
	// channel, which is what lets MESSAGE key on (ts, chunk) at all.
	f.message(1, general, shared, "U100", "general: the audit", nil)
	f.message(2, other, shared, "U200", "tenders: the audit", nil)
	f.message(2, other, "1700000100.000200", "U100", "in tenders only",
		map[string]any{"thread_ts": shared})

	s := store(t)
	r := f.ingest(s)
	if r.Created != 3 {
		t.Fatalf("created %d entries, want 3", r.Created)
	}

	inGeneral := entryID(t, s, ExtID(general, shared))
	inOther := entryID(t, s, ExtID(other, shared))
	if inGeneral == inOther {
		t.Fatal("the same ts in two channels became one entry")
	}
	got, ok := parentOf(t, s, ExtID(other, "1700000100.000200"))
	if !ok || got != inOther {
		t.Fatalf("reply resolved to %d (set=%v), want the same-channel parent %d",
			got, ok, inOther)
	}
}

// The single most valuable thing this package does: one human with a Slack
// account and a mail account is one person, not two halves of a conversation.
func TestSlackAndMailIdentitiesCollapseToOnePerson(t *testing.T) {
	s := store(t)

	m := mailingest.Message{Body: "as discussed on Slack"}
	m.ID = "g1"
	m.MessageID = "<g1@northwind.fed>"
	m.ThreadID = "t1"
	m.Date = "Mon, 02 Jan 2006 15:04:05 +1200"
	m.Subject = "the meter audit"
	m.From = "Ada Fenwick <ada@northwind.fed>"
	m.To = "bo@northwind.fed"
	if _, err := mailingest.Put(s, m); err != nil {
		t.Fatalf("mail put: %v", err)
	}

	f := newFixture(t)
	f.people()
	f.channel(general, "general", false)
	f.message(1, general, "1700000000.000100", "U100", "and here on Slack", nil)
	f.ingest(s)

	byEmail, err := corpus.PersonByIdentity(s, corpus.KindEmail, "ada@northwind.fed")
	if err != nil {
		t.Fatal(err)
	}
	byUID, err := corpus.PersonByIdentity(s, corpus.KindSlackUID, "U100")
	if err != nil {
		t.Fatalf("slack uid was not attached to the person its email resolved to: %v", err)
	}
	if byEmail != byUID {
		t.Fatalf("ada is two people: %d by email, %d by uid", byEmail, byUID)
	}

	var sent int
	if err := s.DB().QueryRow(
		`select count(*) from participants where person_id=? and role='from'`,
		byEmail).Scan(&sent); err != nil {
		t.Fatal(err)
	}
	if sent != 2 {
		t.Fatalf("ada authored %d entries, want 2 (one mail, one slack)", sent)
	}
}

// 24 of 177 accounts in a real workspace have no profile email. Inventing one
// would collide with a real address the day somebody registered it, so they are
// keyed on the uid — and the count is reported, not swallowed.
func TestEmaillessUserIsKeyedOnItsSlackUID(t *testing.T) {
	f := newFixture(t)
	f.people()
	f.channel(general, "general", false)
	f.message(1, general, "1700000000.000100", "U300", "no address on my profile", nil)

	s := store(t)
	r := f.ingest(s)
	if r.AuthorsWithoutEmail != 1 || r.Authors != 1 {
		t.Fatalf("authors %d, without email %d; want 1 and 1", r.Authors, r.AuthorsWithoutEmail)
	}
	if r.UsersWithoutEmail != 2 { // Casper and the bot
		t.Fatalf("users without email %d, want 2", r.UsersWithoutEmail)
	}

	person, err := corpus.PersonByIdentity(s, corpus.KindSlackUID, "U300")
	if err != nil {
		t.Fatalf("no person for the email-less author: %v", err)
	}
	var author sql.NullInt64
	if err := s.DB().QueryRow(`select person_id from entries where ext_id=?`,
		ExtID(general, "1700000000.000100")).Scan(&author); err != nil {
		t.Fatal(err)
	}
	if !author.Valid || author.Int64 != person {
		t.Fatalf("entry author %v, want person %d", author, person)
	}
	// And no address was conjured for them.
	var addrs int
	if err := s.DB().QueryRow(
		`select count(*) from identities where person_id=? and kind=?`,
		person, corpus.KindEmail).Scan(&addrs); err != nil {
		t.Fatal(err)
	}
	if addrs != 0 {
		t.Fatalf("%d email identities invented for an account with no address", addrs)
	}
}

func TestIngestedSlackMessageIsSearchable(t *testing.T) {
	f := newFixture(t)
	f.people()
	f.channel(general, "general", false)
	f.message(1, general, "1700000000.000100", "U100",
		"the Fernhill reconciliation is off by 40 kWh", nil)

	s := store(t)
	f.ingest(s)

	hits, err := s.SearchEntries(corpus.Query{Text: "reconciliation"})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].ExtID != ExtID(general, "1700000000.000100") {
		t.Fatalf("search returned %+v", hits)
	}
	if hits[0].Source != corpus.SourceSlack {
		t.Fatalf("source %q, want slack", hits[0].Source)
	}
}

// Mail and Slack share one FTS index, and the noise predicates are written over
// the outer-joined slack_detail. A mail row has no slack_detail, so an
// uncoalesced `not (sd.is_bot = 1)` is NULL and excludes it — every mail row
// would vanish from every search the moment Slack rows existed.
func TestMailRowsStayVisibleOnceSlackRowsExist(t *testing.T) {
	s := store(t)

	m := mailingest.Message{Body: "the Fernhill reconciliation is off"}
	m.ID = "g1"
	m.MessageID = "<g1@northwind.fed>"
	m.ThreadID = "t1"
	m.Date = "Mon, 02 Jan 2006 15:04:05 +1200"
	m.Subject = "Fernhill"
	m.From = "Ada Fenwick <ada@northwind.fed>"
	if _, err := mailingest.Put(s, m); err != nil {
		t.Fatalf("mail put: %v", err)
	}

	f := newFixture(t)
	f.people()
	f.channel(general, "general", false)
	f.message(1, general, "1700000000.000100", "U200", "Fernhill reconciliation again", nil)
	f.ingest(s)

	hits, err := s.SearchEntries(corpus.Query{Text: "Fernhill"})
	if err != nil {
		t.Fatal(err)
	}
	var sources []string
	for _, h := range hits {
		sources = append(sources, h.Source)
	}
	if len(hits) != 2 {
		t.Fatalf("search found %d entries (%v), want both the mail and the slack one",
			len(hits), sources)
	}
}

// A bot is ingested and flagged, not dropped: whether it is noise is the
// ranking's decision, and a corpus that never stored it could not change its
// mind later.
func TestBotMessagesAreFlaggedNotFiltered(t *testing.T) {
	f := newFixture(t)
	f.people()
	f.channel(general, "general", false)
	f.message(1, general, "1700000000.000100", "B900",
		"deploy of Fernhill finished", nil)
	f.message(1, general, "1700000100.000200", "U100",
		"Fernhill looks good to me", nil)

	s := store(t)
	f.ingest(s)

	var isBot int
	if err := s.DB().QueryRow(`
		select sd.is_bot from slack_detail sd join entries e on e.id = sd.entry_id
		where e.ext_id=?`, ExtID(general, "1700000000.000100")).Scan(&isBot); err != nil {
		t.Fatal(err)
	}
	if isBot != 1 {
		t.Fatal("a message from an is_bot account was not flagged as one")
	}

	quiet, err := s.SearchEntries(corpus.Query{Text: "Fernhill"})
	if err != nil {
		t.Fatal(err)
	}
	if len(quiet) != 1 {
		t.Fatalf("default search returned %d entries, want the human's only", len(quiet))
	}
	loud, err := s.SearchEntries(corpus.Query{Text: "Fernhill", IncludeNoise: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(loud) != 2 {
		t.Fatalf("search with noise returned %d entries, want 2", len(loud))
	}
}

// A backfill of a full workspace runs for as long as it runs and is restarted
// freely, so a second pass must cost nothing and change nothing.
func TestReIngestCreatesNothing(t *testing.T) {
	f := newFixture(t)
	f.people()
	f.channel(general, "general", false)
	f.message(1, general, "1700000000.000100", "U100", "first", nil)
	f.message(1, general, "1700000100.000200", "U200", "second",
		map[string]any{"thread_ts": "1700000000.000100"})

	s := store(t)
	first := f.ingest(s)
	if first.Created != 2 || first.Skipped != 0 {
		t.Fatalf("first pass: %+v", first)
	}

	before := counts(t, s)
	second := f.ingest(s)
	if second.Created != 0 || second.Changed != 0 || second.Skipped != 2 {
		t.Fatalf("second pass: %+v, want everything skipped", second)
	}
	if second.Resolved != 0 {
		t.Fatalf("second pass resolved %d edges again", second.Resolved)
	}
	if after := counts(t, s); after != before {
		t.Fatalf("second pass changed the corpus: %+v -> %+v", before, after)
	}

	// The FTS indexes are external-content, so a retraction with the wrong values
	// corrupts them silently. Only an integrity check catches it.
	for _, tbl := range []string{"entries_fts", "entries_ident"} {
		if _, err := s.DB().Exec(
			`insert into ` + tbl + `(` + tbl + `) values ('integrity-check')`); err != nil {
			t.Fatalf("%s integrity: %v", tbl, err)
		}
	}
}

type rowCounts struct {
	Entries, Details, People, Participants, Sightings, Attachments int
}

func counts(t *testing.T, s *corpus.Store) rowCounts {
	t.Helper()
	var c rowCounts
	for tbl, dst := range map[string]*int{
		"entries": &c.Entries, "slack_detail": &c.Details, "people": &c.People,
		"participants": &c.Participants, "sightings": &c.Sightings,
		"attachments": &c.Attachments,
	} {
		if err := s.DB().QueryRow(`select count(*) from ` + tbl).Scan(dst); err != nil {
			t.Fatal(err)
		}
	}
	return c
}

// An edited message is the one case where a re-ingest should write: the archive
// now holds different text for a ts already in the corpus.
func TestReIngestOfEditedTextRewritesTheEntry(t *testing.T) {
	f := newFixture(t)
	f.people()
	f.channel(general, "general", false)
	f.message(1, general, "1700000000.000100", "U100", "off by 40 kWh", nil)

	s := store(t)
	f.ingest(s)

	// A later archive run re-records the message in a new chunk with the edit.
	f.message(2, general, "1700000000.000100", "U100", "off by 400 kWh", nil)
	r := f.ingest(s)
	if r.Changed != 1 || r.Created != 0 {
		t.Fatalf("edited pass: %+v, want one changed", r)
	}

	var body string
	if err := s.DB().QueryRow(`select body_text from entries where ext_id=?`,
		ExtID(general, "1700000000.000100")).Scan(&body); err != nil {
		t.Fatal(err)
	}
	if body != "off by 400 kWh" {
		t.Fatalf("body %q, want the newest chunk's text", body)
	}
	// The old terms must be gone from the index, not merely joined by the new.
	hits, err := s.SearchEntries(corpus.Query{Text: `"40 kWh"`})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		t.Fatalf("the pre-edit text is still indexed: %+v", hits)
	}
}

func TestDetailAndPermalinkAreRecorded(t *testing.T) {
	f := newFixture(t)
	f.people()
	f.channel(general, "general", false)
	f.channel(dm, "", true)
	f.message(1, general, "1700000000.000100", "U100", "in the channel",
		map[string]any{"subtype": "thread_broadcast"})
	f.message(1, dm, "1700000100.000200", "U200", "just between us", nil)

	s := store(t)
	f.ingest(s)

	var chID, chName, ts, subtype, permalink string
	var isDM int
	if err := s.DB().QueryRow(`
		select sd.channel_id, sd.channel_name, sd.ts, coalesce(sd.subtype,''),
		       sd.is_dm, coalesce(e.permalink,'')
		from slack_detail sd join entries e on e.id = sd.entry_id
		where e.ext_id=?`, ExtID(general, "1700000000.000100")).
		Scan(&chID, &chName, &ts, &subtype, &isDM, &permalink); err != nil {
		t.Fatal(err)
	}
	if chID != general || chName != "general" || ts != "1700000000.000100" {
		t.Fatalf("detail: %s %s %s", chID, chName, ts)
	}
	if subtype != "thread_broadcast" {
		t.Fatalf("subtype %q, want the one the source stated", subtype)
	}
	if isDM != 0 {
		t.Fatal("a public channel was marked a DM")
	}
	// The workspace comes from the archive, never from a constant.
	want := "https://northwind.slack.com/archives/" + general + "/p1700000000000100"
	if permalink != want {
		t.Fatalf("permalink %q, want %q", permalink, want)
	}

	if err := s.DB().QueryRow(`
		select sd.is_dm from slack_detail sd join entries e on e.id = sd.entry_id
		where e.ext_id=?`, ExtID(dm, "1700000100.000200")).Scan(&isDM); err != nil {
		t.Fatal(err)
	}
	if isDM != 1 {
		t.Fatal("a D-prefixed channel was not marked a DM")
	}
}

// The clock a Slack message is shown on comes from the author's zone at that
// instant, not from their offset when the archive ran — otherwise half the
// year's messages display an hour out.
func TestZoneComesFromTheAuthorsOwnZoneAtThatInstant(t *testing.T) {
	f := newFixture(t)
	f.people()
	f.channel(general, "general", false)
	f.message(1, general, "1700000000.000100", "U100", "midwinter in Auckland", nil) // Nov: NZDT
	f.message(1, general, "1688000000.000200", "U100", "midsummer in Auckland", nil) // Jun: NZST
	f.message(1, general, "1700000200.000300", "U200", "Brisbane does not shift", nil)

	s := store(t)
	f.ingest(s)

	for _, tc := range []struct{ ts, tz string }{
		{"1700000000.000100", "+1300"},
		{"1688000000.000200", "+1200"},
		{"1700000200.000300", "+1000"},
	} {
		var tz string
		var off int
		if err := s.DB().QueryRow(`select tz, tz_offset from entries where ext_id=?`,
			ExtID(general, tc.ts)).Scan(&tz, &off); err != nil {
			t.Fatal(err)
		}
		if tz != tc.tz {
			t.Fatalf("%s: tz %q, want %q", tc.ts, tz, tc.tz)
		}
	}
}

func TestFilesAreStoredAsMetadata(t *testing.T) {
	f := newFixture(t)
	f.people()
	f.channel(general, "general", false)
	f.message(1, general, "1700000000.000100", "U100", "the audit sheet",
		map[string]any{"files": []map[string]any{{
			"id": "F1", "name": "audit.csv", "mimetype": "text/csv", "size": 2048,
			"permalink": "https://northwind.slack.com/files/U100/F1/audit.csv",
		}}})

	s := store(t)
	f.ingest(s)

	var name, mime, ref string
	var size int64
	if err := s.DB().QueryRow(`
		select a.name, a.mime, a.size, a.source_ref from attachments a
		join entries e on e.id = a.entry_id where e.ext_id=?`,
		ExtID(general, "1700000000.000100")).Scan(&name, &mime, &size, &ref); err != nil {
		t.Fatal(err)
	}
	if name != "audit.csv" || mime != "text/csv" || size != 2048 || ref != "F1" {
		t.Fatalf("attachment: %s %s %d %s", name, mime, size, ref)
	}
}

// An account the users dump never covered — deactivated, or from a shared
// channel in another workspace — still authored what it authored.
func TestUnknownAccountStillGetsAPerson(t *testing.T) {
	f := newFixture(t)
	f.people()
	f.channel(general, "general", false)
	f.message(1, general, "1700000000.000100", "U999", "I left last year", nil)

	s := store(t)
	f.ingest(s)

	if _, err := corpus.PersonByIdentity(s, corpus.KindSlackUID, "U999"); err != nil {
		t.Fatalf("no person for an account absent from the users dump: %v", err)
	}
}

// A group DM is a small private channel, not a DM. Folding the two together
// would make "what did we say just between us" unanswerable.
func TestGroupDMIsNotMarkedADM(t *testing.T) {
	f := newFixture(t)
	f.people()
	f.channel("G0MPIM", "mpdm-ada--bo--casper", false)
	f.message(1, "G0MPIM", "1700000000.000100", "U100", "the three of us", nil)

	s := store(t)
	f.ingest(s)

	var isDM int
	if err := s.DB().QueryRow(`
		select sd.is_dm from slack_detail sd join entries e on e.id = sd.entry_id
		where e.ext_id=?`, ExtID("G0MPIM", "1700000000.000100")).Scan(&isDM); err != nil {
		t.Fatal(err)
	}
	if isDM != 0 {
		t.Fatal("a group DM was marked is_dm; it is a private channel")
	}
}

// Channel membership is not participation. A post in a channel is not addressed
// to its members, and recording it as such would empty "who was this sent to" of
// meaning for mail, where the answer is a deliberate list.
func TestOnlyTheAuthorParticipates(t *testing.T) {
	f := newFixture(t)
	f.people()
	f.channel(general, "general", false)
	f.message(1, general, "1700000000.000100", "U100", "morning all", nil)

	s := store(t)
	f.ingest(s)

	rows, err := s.DB().Query(`
		select p.role, count(*) from participants p
		join entries e on e.id = p.entry_id where e.ext_id=? group by p.role`,
		ExtID(general, "1700000000.000100"))
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	roles := map[string]int{}
	for rows.Next() {
		var role string
		var n int
		if err := rows.Scan(&role, &n); err != nil {
			t.Fatal(err)
		}
		roles[role] = n
	}
	if len(roles) != 1 || roles[corpus.RoleFrom] != 1 {
		t.Fatalf("participants %v, want exactly one 'from'", roles)
	}
}
