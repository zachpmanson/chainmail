package corpus

import (
	"errors"
	"reflect"
	"strconv"
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

// The reader is a person in this corpus rather than a list of addresses the
// setting holds. What that buys is the whole reason for the change: the addresses
// are read out of the identity graph when the question is asked, so an alias the
// corpus learns later — or folds in from a merge — is marked without the reader
// going back to the page and writing it down.
func TestTheReaderIsAPersonWhoseAddressesAreLookedUp(t *testing.T) {
	s := open(t)
	reader := person(t, s, "ada@loomworks.example", "Ada Byron")
	other := person(t, s, "bo@fjordline.example", "Bo Halvorsen")

	if err := s.SetMePerson(reader); err != nil {
		t.Fatalf("SetMePerson: %v", err)
	}
	if id, ok, err := s.MePerson(); err != nil || !ok || id != reader {
		t.Fatalf("MePerson = %d, %v, %v, want %d, true", id, ok, err, reader)
	}
	got, err := s.MeAddresses()
	if err != nil {
		t.Fatalf("MeAddresses: %v", err)
	}
	if want := []string{"ada@loomworks.example"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("addresses = %q, want %q", got, want)
	}

	// The alias arrives after the setting was made, and is marked without the
	// setting being touched: a stored list would still be the one address.
	if err := AddAlias(s, reader, KindEmail, "ada+salsa@loomworks.example", "test"); err != nil {
		t.Fatalf("AddAlias: %v", err)
	}
	got, err = s.MeAddresses()
	if err != nil {
		t.Fatalf("MeAddresses: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("addresses = %q, want the alias the corpus learned as well", got)
	}

	// One reader, one person: the setting names no other, and clearing it is a
	// state of its own rather than an address list that happens to be empty.
	if err := s.SetMePerson(other); err != nil {
		t.Fatalf("SetMePerson: %v", err)
	}
	if id, _, _ := s.MePerson(); id != other {
		t.Fatalf("MePerson = %d after being set to %d", id, other)
	}
	if err := s.SetMePerson(0); err != nil {
		t.Fatalf("clearing: %v", err)
	}
	if id, ok, _ := s.MePerson(); ok || id != 0 {
		t.Fatalf("MePerson = %d, %v after clearing", id, ok)
	}
	if addresses, err := s.MeAddresses(); err != nil || len(addresses) != 0 {
		t.Fatalf("addresses = %q, %v after clearing", addresses, err)
	}
}

// A person the corpus does not hold, or one no address can have sent from, is a
// refusal rather than a setting: the setting exists to mark the reader's own
// mail, and either would mark nothing while reading back as a choice made.
func TestSetMePersonRefusesAPersonNobodyCouldHaveReceivedMailFrom(t *testing.T) {
	s := open(t)
	if err := s.SetMePerson(404); !errors.Is(err, ErrNoPerson) {
		t.Fatalf("an id with no person behind it: %v, want ErrNoPerson", err)
	}
	// A person the corpus holds by a name recovered from somebody else's quote
	// has no mailbox, so no message could ever have come from them.
	named, err := Resolve(s, KindDisplayName, "Ben", "Ben")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if err := s.SetMePerson(named); !errors.Is(err, ErrNoPerson) {
		t.Fatalf("a person with no address: %v, want ErrNoPerson", err)
	}
	if _, ok, _ := s.MePerson(); ok {
		t.Error("a refused write stored a reader anyway")
	}

	// The same rule holds on the read, for a setting that got there another way:
	// a person with no address marks what nobody marks, so they are not served as
	// the reader either — the control is never handed an id it would filter out of
	// its own options and show as Nobody. A value that is not an id at all reads
	// the same way rather than taking down every trail render with an error about
	// a preference.
	for _, v := range []string{strconv.FormatInt(named, 10), strconv.Itoa(404), "ada", "   "} {
		if err := s.PutSetting(SettingMePerson, v); err != nil {
			t.Fatalf("PutSetting(%q): %v", v, err)
		}
		if id, ok, err := s.MePerson(); err != nil || ok || id != 0 {
			t.Errorf("MePerson = %d, %v, %v for %q, want nobody", id, ok, err, v)
		}
		if addresses, err := s.MeAddresses(); err != nil || len(addresses) != 0 {
			t.Errorf("addresses = %q, %v for %q, want none", addresses, err, v)
		}
	}
}

// A corpus configured before this setting named a person keeps saying who its
// reader is: the address list is still read, and the reader picking themselves
// replaces it rather than sitting beside it — two answers to whose mail is
// theirs is the drift this whole change is against.
func TestTheAddressListACorpusWasConfiguredWithIsStillRead(t *testing.T) {
	s := open(t)
	// What the setting used to be written by the API: the whole value of the
	// field, tidied into one form. Spaces, stray commas and a repeated address
	// are all how a reader writes one list.
	if err := s.PutSetting(SettingMe, " Ada@loomworks.example , bo@fjordline.example, ada@loomworks.example ,"); err != nil {
		t.Fatalf("PutSetting: %v", err)
	}
	got, err := s.MeAddresses()
	if err != nil {
		t.Fatalf("MeAddresses: %v", err)
	}
	want := []string{"Ada@loomworks.example", "bo@fjordline.example"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("addresses = %q, want %q", got, want)
	}
	// It is a reader nobody can be *named* as: an address list is not a person,
	// and the API has no id to offer the control.
	if id, ok, err := s.MePerson(); err != nil || ok || id != 0 {
		t.Fatalf("MePerson = %d, %v, %v for a corpus holding only addresses", id, ok, err)
	}

	// Picking a person replaces both: the list is cleared with it, and clearing
	// the person does not resurrect the list it replaced.
	reader := person(t, s, "ada@loomworks.example", "Ada Byron")
	if err := s.SetMePerson(reader); err != nil {
		t.Fatalf("SetMePerson: %v", err)
	}
	if v, ok, _ := s.Setting(SettingMe); ok {
		t.Errorf("the address list survived being replaced by person %d: %q", reader, v)
	}
	if err := s.SetMePerson(0); err != nil {
		t.Fatalf("clearing: %v", err)
	}
	if addresses, err := s.MeAddresses(); err != nil || len(addresses) != 0 {
		t.Errorf("addresses = %q, %v after the reader was cleared", addresses, err)
	}
}

// A reader who has never named anybody is a real state, and the corpus serves it
// as an empty list rather than as an error: nothing in the corpus records which
// mailbox it was collected from, so "they have not said" is the answer until they
// do — and it is also what clearing the setting leaves behind.
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
