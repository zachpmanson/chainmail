package corpus

import (
	"reflect"
	"strings"
	"testing"
)

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

// The reader's addresses are stored as the one comma-separated string the field
// they were typed into holds, so reading them back is a parse of the reader's
// own text rather than of a list: spaces and stray commas are how they wrote it,
// and one address is one address however many times it appears. Getting this
// wrong is not cosmetic — the list is what a message's author is resolved
// against, so a duplicate is a second chance at a match and a missed entry is a
// reader's own mail left unmarked.
func TestTheReadersAddressesAreSplitTrimmedAndDeduplicated(t *testing.T) {
	s := open(t)
	// What the settings API writes: the whole value of the field, tidied into
	// one form. The two wire shapes are the same list — a caller that split the
	// text itself and one that sent it whole must store the same thing, because
	// what the server keeps is a list of addresses and not a transcript of the
	// request.
	const typed = " ada@loomworks.example , bo@fjordline.example,,"
	want := []string{"ada@loomworks.example", "bo@fjordline.example"}
	for _, vs := range [][]string{{typed}, strings.Split(typed, ",")} {
		if err := s.PutSetting(SettingMe, JoinAddresses(vs)); err != nil {
			t.Fatalf("PutSetting(%q): %v", vs, err)
		}
		got, err := s.MeAddresses()
		if err != nil {
			t.Fatalf("MeAddresses: %v", err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("JoinAddresses(%q) read back as %q, want %q", vs, got, want)
		}
	}

	// Case is not identity for an address — the corpus lowercases every one it
	// stores — so listing the same mailbox twice is listing one address, and the
	// spelling the reader used is the one kept.
	joined := JoinAddresses([]string{"Ada@Loomworks.example, ada@loomworks.example"})
	if joined != "Ada@Loomworks.example" {
		t.Errorf("a duplicate read as two addresses: %q", joined)
	}

	// And a list of nothing is the setting gone, not a setting holding nothing —
	// the same one-state rule the folder has.
	if got := JoinAddresses([]string{"  ", ""}); got != "" {
		t.Errorf("JoinAddresses of blanks = %q, want the empty value that clears", got)
	}
}

// A reader who has never named an address is a real state, and the corpus serves
// it as an empty list rather than as an error: nothing in the corpus records
// which mailbox it was collected from, so "they have not said" is the answer
// until they do — and it is also what a cleared field leaves behind.
func TestUnnamedAddressesAreAnEmptyListRatherThanAnError(t *testing.T) {
	s := open(t)
	got, err := s.MeAddresses()
	if err != nil {
		t.Fatalf("MeAddresses on a corpus nobody has configured: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("addresses = %q for a reader who has named none", got)
	}

	if err := s.PutSetting(SettingMe, "ada@loomworks.example"); err != nil {
		t.Fatal(err)
	}
	if err := s.PutSetting(SettingMe, ""); err != nil {
		t.Fatal(err)
	}
	got, err = s.MeAddresses()
	if err != nil {
		t.Fatalf("MeAddresses after clearing: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("addresses = %q after the setting was cleared", got)
	}
}
