package corpus

import (
	"database/sql"
	"strings"
)

// The settings the corpus stores. Named here rather than spelled at each call
// site so a typo is a compile error rather than a silently empty setting.
const (
	// SettingDefaultFolder is the mailbox label the home page opens in. Empty
	// means every folder at once — the same as never having chosen.
	SettingDefaultFolder = "default_folder"
	// SettingMe is the addresses the reader says are theirs, so their own mail
	// can be marked on a page and in the reading pane. Nothing in the corpus
	// records which mailbox it was collected from, so this can only be told.
	SettingMe = "me"
	// SettingSlurpEvery is how often the server sweeps the mailbox by itself.
	// A duration word (`10m`, `1h`) or `off`; absent means the default cadence,
	// which is the server's to choose (see DefaultSlurpEvery). It is stored
	// rather than configured because it is a decision about this corpus's
	// freshness, made from the services page — see cmd/server/schedule.go.
	SettingSlurpEvery = "slurp_every"
	// SettingSlurpAt is when the server last reached for the mailbox, which is
	// what the cadence counts from. A stamp rather than a flag, so a restart
	// does not swallow the wait and re-ingest a mailbox that was ingested
	// minutes ago.
	SettingSlurpAt = "slurp_at"
)

// Setting reads one stored setting. A setting nobody has made is absent, not an
// error and not a zero-length string pretending to be a value: the caller
// decides what its absence means, and for a folder it is "no default", which is
// a real state rather than a missing one.
func (s *Store) Setting(key string) (string, bool, error) {
	var v string
	err := s.db.QueryRow(`select value from settings where key = ?`, key).Scan(&v)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return v, true, nil
}

// MeAddresses reads the addresses the reader has named as their own.
//
// The parse lives here rather than at each call site because two callers need
// the same one: the settings API, which serves and writes them, and the trail
// render, which marks the reader's own messages (spec.RenderTrail). Two parses
// would be two answers to "who is the reader", and the disagreement would show
// as one surface tinting a bubble the other leaves plain.
//
// A setting nobody has made is an empty list rather than an error, the same way
// Setting reports absence rather than a zero value: a reader who has never said
// who they are is a real state, and nothing is marked for them.
func (s *Store) MeAddresses() ([]string, error) {
	v, ok, err := s.Setting(SettingMe)
	if err != nil || !ok {
		return nil, err
	}
	return SplitAddresses(v), nil
}

// SplitAddresses reads addresses out of the comma-separated form they are typed
// and stored in. Each is trimmed and blanks are dropped — a trailing comma is
// something a reader writes, not an address — and duplicates are dropped
// case-insensitively, because the corpus lowercases every address it stores, so
// `Ada@x` and `ada@x` are one address and listing both must not read as two.
//
// Nothing here resolves an address to a person: that is the corpus's identity
// graph, and a list of strings is not the place to decide whose they are.
func SplitAddresses(v string) []string {
	var out []string
	seen := map[string]bool{}
	for _, a := range strings.Split(v, ",") {
		a = strings.TrimSpace(a)
		if a == "" || seen[strings.ToLower(a)] {
			continue
		}
		seen[strings.ToLower(a)] = true
		out = append(out, a)
	}
	return out
}

// JoinAddresses is SplitAddresses the other way: the addresses a caller holds,
// in the one form the setting is written in.
//
// The elements are joined and then parsed rather than parsed one at a time,
// because what arrives on the wire is the reader's own text — the whole value of
// the field they typed into, which is one comma-separated list — and a
// per-element join would store one address where they named three.
//
// Joining nothing is the empty string, which PutSetting deletes: no addresses
// and no setting are one state rather than two, exactly as for the folder.
func JoinAddresses(vs []string) string {
	return strings.Join(SplitAddresses(strings.Join(vs, ",")), ", ")
}

// PutSetting records a setting, replacing whatever was there. An empty value
// deletes the row rather than storing one: "no default folder" and "a default
// folder of nothing" would be two states for one meaning, and the first thing
// to do with such a pair is to get them out of step.
func (s *Store) PutSetting(key, value string) error {
	if value == "" {
		_, err := s.db.Exec(`delete from settings where key = ?`, key)
		return err
	}
	_, err := s.db.Exec(`
		insert into settings (key, value) values (?, ?)
		on conflict(key) do update set value = excluded.value`, key, value)
	return err
}
