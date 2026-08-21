// Package slackingest fills the corpus from Slack, by way of a slackdump
// archive.
//
// Reading slackdump's own SQLite database rather than calling Slack keeps the
// API out of the ingest path entirely: a re-ingest costs nothing, hits no rate
// limit and cannot lose data to a half-finished pagination. slackdump owns the
// tokens, the cursor walking and the retries; this package owns the conversion.
// The cost is that the corpus is only as current as the last archive run, which
// is the same trade mailingest makes by shelling out to docket.
package slackingest

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	// Slack states a user's zone as an IANA name, and rendering the sender's own
	// clock needs the offset that name implied at the instant of the message.
	// Embedding the database means that works on a machine with no system tzdata
	// and gives the tests one answer everywhere.
	_ "time/tzdata"

	_ "modernc.org/sqlite"
)

// Archive is an open slackdump database. Opened read-only: this process is a
// reader of someone else's store and must never migrate or write to it.
type Archive struct {
	db   *sql.DB
	path string
}

// OpenArchive opens a slackdump archive.
func OpenArchive(path string) (*Archive, error) {
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		return nil, fmt.Errorf("opening slack archive %s: %w", path, err)
	}
	// sql.Open is lazy, so probe for the one table everything depends on. A
	// misspelled path otherwise fails much later, as a confusing query error.
	var n int
	if err := db.QueryRow(
		`select count(*) from sqlite_master where type='table' and name='MESSAGE'`).
		Scan(&n); err != nil {
		db.Close()
		return nil, fmt.Errorf("reading slack archive %s: %w", path, err)
	}
	if n == 0 {
		db.Close()
		return nil, fmt.Errorf("%s is not a slackdump archive: no MESSAGE table", path)
	}
	return &Archive{db: db, path: path}, nil
}

func (a *Archive) Close() error { return a.db.Close() }

// Workspace is the subdomain a permalink is built from, e.g. "acme" for
// acme.slack.com. Empty when the archive holds no WORKSPACE row, in which case
// permalinks are omitted rather than guessed — a permalink pointing at the wrong
// workspace is worse than none, because it looks clickable.
func (a *Archive) Workspace() (string, error) {
	var raw string
	err := a.db.QueryRow(`select URL from WORKSPACE order by ID desc limit 1`).Scan(&raw)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("reading workspace: %w", err)
	}
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", nil
	}
	host := u.Hostname()
	if host == "" {
		host = strings.TrimSpace(raw)
	}
	if i := strings.Index(host, "."); i > 0 {
		return host[:i], nil
	}
	return host, nil
}

// User is one Slack account, as the archive holds it.
type User struct {
	ID       string
	Name     string // the @handle
	RealName string
	Email    string
	IsBot    bool
	Deleted  bool
	TZ       string // IANA name, e.g. "Pacific/Auckland"
	TZOffset int    // seconds east of UTC, as of the archive run
}

// DisplayName is the most human of the names Slack offers, in the order a reader
// would prefer them. Falls back to the id so a person is never nameless.
func (u User) DisplayName() string {
	for _, c := range []string{u.RealName, u.Name, u.ID} {
		if s := strings.TrimSpace(c); s != "" {
			return s
		}
	}
	return ""
}

// Zone returns the label and offset to store for a message this user sent at t.
//
// The label is numeric ("+1200") rather than Slack's tz_label ("New Zealand
// Standard Time"), because the spec generator turns a label into an offset from
// a table of abbreviations and a prose label resolves to nothing — the entry
// would then be listed as an unrenderable zone despite the offset being known
// exactly.
//
// The offset is computed from the IANA zone AT the message's instant, not taken
// from profile.tz_offset: that field is the user's offset when the archive ran,
// so half the year's messages would display an hour out. tz_offset is the
// fallback for a zone name the tz database does not know.
func (u User) Zone(t time.Time) (string, *int) {
	if u.TZ != "" {
		if loc, err := time.LoadLocation(u.TZ); err == nil {
			local := t.In(loc)
			_, secs := local.Zone()
			mins := secs / 60
			return local.Format("-0700"), &mins
		}
	}
	if u.TZOffset != 0 {
		mins := u.TZOffset / 60
		return formatOffset(mins), &mins
	}
	return "", nil
}

func formatOffset(mins int) string {
	sign := "+"
	if mins < 0 {
		sign, mins = "-", -mins
	}
	return fmt.Sprintf("%s%02d%02d", sign, mins/60, mins%60)
}

type userJSON struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	RealName string `json:"real_name"`
	Deleted  bool   `json:"deleted"`
	IsBot    bool   `json:"is_bot"`
	TZ       string `json:"tz"`
	TZOffset int    `json:"tz_offset"`
	Profile  struct {
		Email       string `json:"email"`
		RealName    string `json:"real_name"`
		DisplayName string `json:"display_name"`
	} `json:"profile"`
}

// Users reads every user in the archive, keyed by id.
//
// The DATA blob wins over the flattened columns wherever they overlap: it is the
// Slack payload as received, and it is the only place the profile — and so the
// email that ties a Slack account to a mail identity — appears at all.
func (a *Archive) Users() (map[string]User, error) {
	// A user is re-recorded by every archive run, once per chunk. max(CHUNK_ID)
	// picks the newest copy: SQLite guarantees the bare columns of a min/max
	// aggregate come from the row that produced the extreme.
	rows, err := a.db.Query(`select ID, max(CHUNK_ID), DATA from S_USER group by ID`)
	if err != nil {
		return nil, fmt.Errorf("reading users: %w", err)
	}
	defer rows.Close()
	out := map[string]User{}
	for rows.Next() {
		var id string
		var chunk int64
		var blob []byte
		if err := rows.Scan(&id, &chunk, &blob); err != nil {
			return nil, err
		}
		var u userJSON
		if err := json.Unmarshal(blob, &u); err != nil {
			// One unparseable user must not cost the whole archive; the id alone
			// still makes a usable identity.
			out[id] = User{ID: id}
			continue
		}
		name := firstNonEmpty(u.Profile.RealName, u.RealName, u.Profile.DisplayName)
		out[id] = User{
			ID:       firstNonEmpty(u.ID, id),
			Name:     u.Name,
			RealName: name,
			Email:    strings.ToLower(strings.TrimSpace(u.Profile.Email)),
			IsBot:    u.IsBot,
			Deleted:  u.Deleted,
			TZ:       u.TZ,
			TZOffset: u.TZOffset,
		}
	}
	return out, rows.Err()
}

// Channel is one conversation the archive covers.
type Channel struct {
	ID     string
	Name   string
	IsIM   bool
	IsMPIM bool
}

type channelJSON struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	IsIM   bool   `json:"is_im"`
	IsMPIM bool   `json:"is_mpim"`
	User   string `json:"user"` // the other party, on a DM
}

// Channels reads every channel in the archive, keyed by id.
func (a *Archive) Channels() (map[string]Channel, error) {
	rows, err := a.db.Query(`select ID, max(CHUNK_ID), NAME, DATA from CHANNEL group by ID`)
	if err != nil {
		return nil, fmt.Errorf("reading channels: %w", err)
	}
	defer rows.Close()
	out := map[string]Channel{}
	for rows.Next() {
		var id string
		var chunk int64
		var name sql.NullString
		var blob []byte
		if err := rows.Scan(&id, &chunk, &name, &blob); err != nil {
			return nil, err
		}
		c := Channel{ID: id, Name: name.String}
		var cj channelJSON
		if err := json.Unmarshal(blob, &cj); err == nil {
			c.IsIM, c.IsMPIM = cj.IsIM, cj.IsMPIM
			if cj.Name != "" {
				c.Name = cj.Name
			}
			// A DM has no name of its own. Naming it after the other party is what
			// makes it identifiable at all; the id alone says nothing.
			if c.Name == "" && cj.User != "" {
				c.Name = "@" + cj.User
			}
		}
		out[id] = c
	}
	return out, rows.Err()
}

// File is an attachment on a message, as metadata only.
type File struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Title     string `json:"title"`
	Mimetype  string `json:"mimetype"`
	Size      int64  `json:"size"`
	Permalink string `json:"permalink"`
}

// Message is one Slack message from the DATA blob.
type Message struct {
	ChannelID  string
	Type       string `json:"type"`
	Subtype    string `json:"subtype"`
	User       string `json:"user"`
	BotID      string `json:"bot_id"`
	Username   string `json:"username"` // set on bot posts, which have no user
	Text       string `json:"text"`
	TS         string `json:"ts"`
	ThreadTS   string `json:"thread_ts"`
	ReplyCount int    `json:"reply_count"`
	Files      []File `json:"files"`
}

// IsThreadParent reports whether this message leads a thread rather than
// replying inside one. Slack sets thread_ts to the parent's ts on both, so the
// parent names itself — and treating that as a reply edge would make the message
// its own parent.
func (m Message) IsThreadParent() bool {
	return m.ThreadTS != "" && m.ThreadTS == m.TS
}

// ParentRef is the ts of the message this one replies to, empty for a root.
func (m Message) ParentRef() string {
	if m.IsThreadParent() {
		return ""
	}
	return m.ThreadTS
}

// Time parses a Slack ts, which is float seconds since the epoch with the
// fractional part carrying the microseconds that make it unique.
func (m Message) Time() (time.Time, error) {
	return ParseTS(m.TS)
}

// ParseTS turns a Slack ts string into an instant.
func ParseTS(ts string) (time.Time, error) {
	whole, frac, _ := strings.Cut(strings.TrimSpace(ts), ".")
	secs, err := strconv.ParseInt(whole, 10, 64)
	if err != nil {
		return time.Time{}, fmt.Errorf("slack ts %q: %w", ts, err)
	}
	var nanos int64
	if frac != "" {
		// Pad or trim to nanosecond precision rather than parsing as a float:
		// float64 cannot hold epoch-seconds to microsecond precision, and the
		// microseconds are what distinguish two messages in the same second.
		f := (frac + "000000000")[:9]
		nanos, err = strconv.ParseInt(f, 10, 64)
		if err != nil {
			return time.Time{}, fmt.Errorf("slack ts %q: %w", ts, err)
		}
	}
	return time.Unix(secs, nanos).UTC(), nil
}

// Messages walks every message in the archive, oldest first within each channel,
// calling fn for each. Streamed rather than returned as a slice: a full
// workspace archive is hundreds of thousands of messages and each carries its
// whole JSON payload.
//
// One message can appear in several chunks, because every archive run that
// covers the channel records it again. The newest chunk wins: reply_count grows
// and text can be edited, so an older copy is a stale copy.
func (a *Archive) Messages(fn func(Message) error) error {
	rows, err := a.db.Query(`
		select CHANNEL_ID, TS, max(CHUNK_ID), DATA from MESSAGE
		group by CHANNEL_ID, TS
		order by CHANNEL_ID, cast(TS as real)`)
	if err != nil {
		return fmt.Errorf("reading messages: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var channel, ts string
		var chunk int64
		var blob []byte
		if err := rows.Scan(&channel, &ts, &chunk, &blob); err != nil {
			return err
		}
		var m Message
		if err := json.Unmarshal(blob, &m); err != nil {
			return fmt.Errorf("message %s/%s: %w", channel, ts, err)
		}
		m.ChannelID = channel
		if m.TS == "" {
			m.TS = ts
		}
		if err := fn(m); err != nil {
			return err
		}
	}
	return rows.Err()
}

// Permalink is the archive URL for a message. Slack's own form drops the dot
// from the ts and prefixes it with "p".
func Permalink(workspace, channelID, ts string) string {
	if workspace == "" || channelID == "" || ts == "" {
		return ""
	}
	return fmt.Sprintf("https://%s.slack.com/archives/%s/p%s",
		workspace, channelID, strings.ReplaceAll(ts, ".", ""))
}

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}
