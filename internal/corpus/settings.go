package corpus

import (
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// The settings the corpus stores. Named here rather than spelled at each call
// site so a typo is a compile error rather than a silently empty setting.
const (
	// SettingDefaultFolder is the mailbox label the home page opens in. Empty
	// means every folder at once — the same as never having chosen.
	SettingDefaultFolder = "default_folder"
	// SettingMePerson is the person the reader says they are, so their own mail
	// can be marked on a page and in the reading pane. Nothing in the corpus
	// records which mailbox it was collected from, so this can only be told —
	// and what is told is a person rather than a list of their addresses,
	// because which addresses are one human is the identity graph's answer
	// rather than the reader's to retype (see MePerson).
	SettingMePerson = "me_person"
	// SettingMe is the comma-separated address list the reader used to write,
	// before the setting named a person. Nothing writes it any more —
	// SetMePerson clears it — and it is read for one reason: a corpus that was
	// configured with it keeps marking its reader's own mail until they pick
	// themselves out of the people list, rather than silently unmarking it the
	// day this changed.
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

// MePerson reads the person the reader has named as themselves: the id of a row
// in `people`, which is the same person the corpus resolved their mail to rather
// than a second idea of them kept in a string.
//
// A person rather than a list of addresses because the list was the reader doing
// the identity graph's work by hand, and doing it wrong in the two ways a person
// does not: an address they forgot to list stayed somebody else's, and every
// address the corpus later learned of the same human arrived after the list was
// written. This way the answer is looked up when the question is asked, so a
// page built today marks the aliases the corpus knows today.
//
// A reader the corpus holds nobody for reads as unnamed, which is every state the
// caller has no reason to tell apart: no setting at all, a value that will not
// parse as an id, an id whose person is gone (a merge takes the row away, and
// repoints this setting on the way out — see Merge; an id left dangling by a
// hand-edited database reads as nobody rather than taking down every trail render
// with an error about a preference), and a person no address can have sent from,
// who marks exactly what nobody marks. The last is the rule SetMePerson refuses
// a write by, applied to reads as well: a reader the API would not store is not
// one it serves either, so the control is never handed an id it cannot show.
func (s *Store) MePerson() (int64, bool, error) {
	v, ok, err := s.Setting(SettingMePerson)
	if err != nil || !ok {
		return 0, false, err
	}
	id, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
	if err != nil || id <= 0 {
		return 0, false, nil
	}
	_, emails, err := personMailbox(s, id)
	if err != nil {
		if errors.Is(err, ErrNoPerson) {
			return 0, false, nil
		}
		return 0, false, err
	}
	if len(emails) == 0 {
		return 0, false, nil
	}
	return id, true, nil
}

// MeAddresses reads the addresses that are the reader's own, so a message from
// one of them — or from the person they belong to — can be marked.
//
// This is the accessor the marking reads, and it resolves the stored person here
// rather than at each call site because two callers need the same one: the
// settings API, which serves and writes the setting, and the trail render, which
// marks the reader's own messages (spec.RenderTrail). Two resolutions would be
// two answers to "who is the reader", and the disagreement would show as one
// surface tinting a bubble the other leaves plain.
//
// A setting nobody has made is an empty list rather than an error, the same way
// Setting reports absence rather than a zero value: a reader who has never said
// who they are is a real state, and nothing is marked for them.
func (s *Store) MeAddresses() ([]string, error) {
	id, ok, err := s.MePerson()
	if err != nil {
		return nil, err
	}
	if ok {
		emails, err := emailsOf(s, id)
		if err != nil {
			return nil, fmt.Errorf("reading the addresses of person %d: %w", id, err)
		}
		return emails, nil
	}
	// The address list of a corpus configured before the setting named a person.
	// Read, never written: the reader who picks themselves replaces it (see
	// SetMePerson), and until then their own mail keeps being marked.
	v, ok, err := s.Setting(SettingMe)
	if err != nil || !ok {
		return nil, err
	}
	return SplitAddresses(v), nil
}

// SetMePerson records the reader as a person, or as nobody with id 0. The person
// must be one the corpus holds and one it has a mailbox for: the setting exists
// to mark the reader's own mail, and a person no address can have sent from
// would mark nothing while reading back as a choice that had been made. Both
// refusals are ErrNoPerson wrapped around the reason, so a caller decides what a
// 400 is without re-deriving either rule.
//
// The address list the setting used to be is cleared with it, in the same
// transaction: the two are one answer to "whose mail is the reader's", and a
// stale list left behind would be read again the moment the person was cleared,
// resurrecting who the reader said they were a change ago.
func (s *Store) SetMePerson(id int64) error {
	value := ""
	if id != 0 {
		name, emails, err := personMailbox(s, id)
		if err != nil {
			return err
		}
		if len(emails) == 0 {
			return fmt.Errorf("%w: %s is not known by any address, so nothing could be marked as theirs",
				ErrNoPerson, name)
		}
		value = strconv.FormatInt(id, 10)
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`delete from settings where key = ?`, SettingMe); err != nil {
		return err
	}
	if value == "" {
		if _, err := tx.Exec(`delete from settings where key = ?`, SettingMePerson); err != nil {
			return err
		}
	} else if _, err := tx.Exec(`
		insert into settings (key, value) values (?, ?)
		on conflict(key) do update set value = excluded.value`, SettingMePerson, value); err != nil {
		return err
	}
	return tx.Commit()
}

// SplitAddresses reads addresses out of the one comma-separated form the reader
// used to write them in (SettingMe). Each is trimmed and blanks are dropped — a
// trailing comma is something a reader writes, not an address — and duplicates
// are dropped case-insensitively, because the corpus lowercases every address it
// stores, so `Ada@x` and `ada@x` are one address and listing both must not read
// as two.
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
