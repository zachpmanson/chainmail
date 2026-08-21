package slackingest

import (
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/zachpmanson/chainmail/internal/corpus"
	_ "modernc.org/sqlite"
)

// Everything below is invented — invented people, invented .fed domains,
// invented text. Nothing from a real workspace belongs in a committed test, and
// a fixture built in code cannot leak one by accident.
//
// The DDL mirrors slackdump's own for the tables this package reads, including
// the composite primary keys: a message is keyed on (ID, CHUNK_ID), which is
// what lets the same message exist several times over, and a fixture without
// that key could not exercise the dedup at all.
const fixtureDDL = `
create table WORKSPACE (
  ID integer primary key, CHUNK_ID integer not null, TEAM text not null,
  TEAM_ID text not null, USER_ID text not null, URL text not null,
  DATA blob not null
);
create table S_USER (
  ID text not null, CHUNK_ID integer not null, IDX integer not null,
  USERNAME text not null, DATA blob not null,
  primary key (ID, CHUNK_ID)
);
create table CHANNEL (
  ID text not null, CHUNK_ID integer not null, NAME text, IDX integer not null,
  DATA blob not null,
  primary key (ID, CHUNK_ID)
);
create table MESSAGE (
  ID integer not null, CHUNK_ID integer not null, CHANNEL_ID text not null,
  TS text not null, PARENT_ID integer, THREAD_TS text, LATEST_REPLY text,
  IS_PARENT smallint not null default 0, IDX integer not null,
  NUM_FILES integer not null default 0, TXT text, DATA blob not null,
  primary key (ID, CHUNK_ID)
);
`

type fixture struct {
	t    *testing.T
	db   *sql.DB
	path string
	idx  int
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	path := filepath.Join(t.TempDir(), "slackdump.sqlite")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("creating fixture: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(fixtureDDL); err != nil {
		t.Fatalf("fixture ddl: %v", err)
	}
	f := &fixture{t: t, db: db, path: path}
	f.workspace("https://northwind.slack.com/")
	return f
}

func (f *fixture) exec(q string, args ...any) {
	f.t.Helper()
	if _, err := f.db.Exec(q, args...); err != nil {
		f.t.Fatalf("fixture write: %v", err)
	}
}

func (f *fixture) blob(v map[string]any) []byte {
	f.t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		f.t.Fatal(err)
	}
	return b
}

func (f *fixture) workspace(url string) {
	f.t.Helper()
	f.exec(`delete from WORKSPACE`)
	f.exec(`insert into WORKSPACE (ID, CHUNK_ID, TEAM, TEAM_ID, USER_ID, URL, DATA)
	        values (1, 1, 'Northwind', 'T1', 'U100', ?, ?)`,
		url, f.blob(map[string]any{"url": url}))
}

// user adds an account. email may be empty, which is the case this package has
// to handle without inventing an address.
func (f *fixture) user(id, handle, realName, email, tz string, isBot bool) {
	f.t.Helper()
	f.idx++
	profile := map[string]any{"real_name": realName, "display_name": handle}
	if email != "" {
		profile["email"] = email
	}
	f.exec(`insert into S_USER (ID, CHUNK_ID, IDX, USERNAME, DATA) values (?,1,?,?,?)`,
		id, f.idx, handle, f.blob(map[string]any{
			"id": id, "name": handle, "real_name": realName, "is_bot": isBot,
			"tz": tz, "tz_offset": 0, "profile": profile,
		}))
}

func (f *fixture) channel(id, name string, isIM bool) {
	f.t.Helper()
	f.idx++
	f.exec(`insert into CHANNEL (ID, CHUNK_ID, NAME, IDX, DATA) values (?,1,?,?,?)`,
		id, name, f.idx, f.blob(map[string]any{"id": id, "name": name, "is_im": isIM}))
}

// message adds one message. extra is merged into the Slack payload, so a test
// can set thread_ts, subtype, bot_id, files or anything else without this
// helper growing a parameter per field.
func (f *fixture) message(chunk int64, channel, ts, user, text string, extra map[string]any) {
	f.t.Helper()
	f.idx++
	data := map[string]any{"type": "message", "ts": ts, "text": text}
	if user != "" {
		data["user"] = user
	}
	threadTS := ""
	for k, v := range extra {
		data[k] = v
		if k == "thread_ts" {
			threadTS, _ = v.(string)
		}
	}
	// MESSAGE.ID is the ts with the dot removed, as slackdump stores it.
	id := int64(0)
	for _, r := range ts {
		if r >= '0' && r <= '9' {
			id = id*10 + int64(r-'0')
		}
	}
	f.exec(`insert into MESSAGE (ID, CHUNK_ID, CHANNEL_ID, TS, THREAD_TS, IS_PARENT, IDX, TXT, DATA)
	        values (?,?,?,?,?,?,?,?,?)`,
		id, chunk, channel, ts, threadTS, boolToInt(threadTS == ts && threadTS != ""),
		f.idx, text, f.blob(data))
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func (f *fixture) open() *Archive {
	f.t.Helper()
	a, err := OpenArchive(f.path)
	if err != nil {
		f.t.Fatalf("OpenArchive: %v", err)
	}
	f.t.Cleanup(func() { a.Close() })
	return a
}

// ingest runs a full ingest of the fixture into a fresh in-memory corpus.
func (f *fixture) ingest(s *corpus.Store) Result {
	f.t.Helper()
	r, err := Ingest(s, f.open())
	if err != nil {
		f.t.Fatalf("Ingest: %v", err)
	}
	return r
}

func store(t *testing.T) *corpus.Store {
	t.Helper()
	s, err := corpus.Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}
