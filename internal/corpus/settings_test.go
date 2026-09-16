package corpus

import "testing"

// A setting nobody has made is absent, not empty: the caller has to decide what
// "unset" means, and for a default folder it means every folder at once — a
// real answer, not a missing one.
func TestAnUnsetSettingIsAbsentRatherThanEmpty(t *testing.T) {
	s := open(t)
	v, ok, err := s.Setting(SettingDefaultFolder)
	if err != nil {
		t.Fatal(err)
	}
	if ok || v != "" {
		t.Fatalf("Setting returned %q, %v for a setting nobody made", v, ok)
	}
}

// One meaning, one row: setting the default folder to nothing deletes it rather
// than storing an empty string, and the two cannot drift apart afterwards.
func TestSettingAnEmptyValueClearsTheRow(t *testing.T) {
	s := open(t)
	if err := s.PutSetting(SettingDefaultFolder, "INBOX"); err != nil {
		t.Fatal(err)
	}
	if v, ok, _ := s.Setting(SettingDefaultFolder); !ok || v != "INBOX" {
		t.Fatalf("after setting: %q, %v", v, ok)
	}
	if err := s.PutSetting(SettingDefaultFolder, ""); err != nil {
		t.Fatal(err)
	}
	v, ok, err := s.Setting(SettingDefaultFolder)
	if err != nil {
		t.Fatal(err)
	}
	if ok || v != "" {
		t.Fatalf("after clearing: %q, %v — want absent", v, ok)
	}
	// And the row is gone rather than blank, so nothing can read an empty folder
	// out of a table that is supposed to hold choices.
	var n int
	if err := s.db.QueryRow(`select count(*) from settings`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("settings holds %d rows after a clear", n)
	}
}

// A setting is replaced, not appended, and one key does not disturb another.
func TestSettingIsReplacedInPlace(t *testing.T) {
	s := open(t)
	if err := s.PutSetting(SettingDefaultFolder, "INBOX"); err != nil {
		t.Fatal(err)
	}
	if err := s.PutSetting(SettingDefaultFolder, "SENT"); err != nil {
		t.Fatal(err)
	}
	if err := s.PutSetting("another", "kept"); err != nil {
		t.Fatal(err)
	}
	v, _, err := s.Setting(SettingDefaultFolder)
	if err != nil {
		t.Fatal(err)
	}
	if v != "SENT" {
		t.Errorf("default folder = %q, want the replacement", v)
	}
	other, ok, err := s.Setting("another")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || other != "kept" {
		t.Errorf("another = %q, %v — replacing one key disturbed it", other, ok)
	}
}
