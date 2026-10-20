package corpus

import "testing"

func TestSettingsUpdateRollsBackEarlierWritesOnDatabaseFailure(t *testing.T) {
	s := open(t)
	before := map[string]string{
		SettingDefaultFolder: "INBOX", SettingDefaultFolderAccountID: "legacy", SettingSlurpEvery: "1h",
	}
	for key, value := range before {
		if err := s.PutSetting(key, value); err != nil {
			t.Fatal(err)
		}
	}
	// Fail the second changed preference, regardless of map iteration order.
	if _, err := s.db.Exec(`create trigger fail_second_setting after update on settings
 when (select count(*) from settings where value in ('TRASH','personal','off')) >= 2
 begin select raise(abort, 'injected settings failure'); end`); err != nil {
		t.Fatal(err)
	}
	folder, account, every := "TRASH", "personal", "off"
	if err := s.UpdateSettings(SettingsUpdate{DefaultFolder: &folder, DefaultFolderAccountID: &account, SlurpEvery: &every}); err == nil {
		t.Fatal("injected write failure was ignored")
	}
	for key, want := range before {
		got, ok, err := s.Setting(key)
		if err != nil || !ok || got != want {
			t.Fatalf("%s=%q present=%v err=%v, want %q", key, got, ok, err, want)
		}
	}
}

func TestSettingsUpdateAppliesAllPreferencesAndClearsLegacyReader(t *testing.T) {
	s := open(t)
	person, err := Resolve(s, KindEmail, "reader@example.invalid", "Reader")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.PutSetting(SettingMe, "old@example.invalid"); err != nil {
		t.Fatal(err)
	}
	folder, account, every := "INBOX", "legacy", "off"
	if err := s.UpdateSettings(SettingsUpdate{DefaultFolder: &folder, DefaultFolderAccountID: &account, SlurpEvery: &every, MePersonID: &person}); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{SettingDefaultFolder: folder, SettingDefaultFolderAccountID: account, SettingSlurpEvery: every} {
		got, ok, err := s.Setting(key)
		if err != nil || !ok || got != want {
			t.Fatalf("%s=%q present=%v err=%v", key, got, ok, err)
		}
	}
	if id, ok, err := s.MePerson(); err != nil || !ok || id != person {
		t.Fatalf("reader=%d present=%v err=%v", id, ok, err)
	}
	if _, ok, err := s.Setting(SettingMe); err != nil || ok {
		t.Fatalf("legacy reader not cleared: present=%v err=%v", ok, err)
	}
}
