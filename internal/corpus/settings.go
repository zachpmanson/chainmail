package corpus

import "database/sql"

// The settings the corpus stores. Named here rather than spelled at each call
// site so a typo is a compile error rather than a silently empty setting.
const (
	// SettingDefaultFolder is the mailbox label the home page opens in. Empty
	// means every folder at once — the same as never having chosen.
	SettingDefaultFolder = "default_folder"
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
