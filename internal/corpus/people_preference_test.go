package corpus

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestResetOriginalPreference(t *testing.T) {
	s := open(t)
	ada, err := Resolve(s, KindEmail, "ada@example.com", "Ada Okoye")
	if err != nil {
		t.Fatal(err)
	}
	bo, err := Resolve(s, KindEmail, "bo@example.com", "Bo Halvorsen")
	if err != nil {
		t.Fatal(err)
	}
	on := true
	if _, err := UpdatePerson(s, bo, PersonEdit{PreferOriginal: &on}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Put(Entry{
		Source: SourceMail, ExtID: "preference", Kind: "message",
		TS: time.Unix(1_700_000_000, 0), BodyText: "hello", PersonID: ada,
	}, &Mail{MessageID: "preference"}, nil); err != nil {
		t.Fatal(err)
	}
	before, err := People(s)
	if err != nil {
		t.Fatal(err)
	}
	assertPreference := func(want, set bool) {
		t.Helper()
		entry, err := s.Show("preference")
		if err != nil {
			t.Fatal(err)
		}
		if entry.PersonID != ada || entry.PreferOriginal != want || entry.PreferOriginalSet != set {
			t.Fatalf("entry preference = (%d, %v, %v), want (%d, %v, %v)",
				entry.PersonID, entry.PreferOriginal, entry.PreferOriginalSet, ada, want, set)
		}
	}
	for _, forced := range []bool{false, true} {
		if _, err := UpdatePerson(s, ada, PersonEdit{PreferOriginal: &forced}); err != nil {
			t.Fatal(err)
		}
		assertPreference(forced, true)
		for _, edit := range []PersonEdit{{}, {ResetOriginalPreference: false}} {
			if _, err := UpdatePerson(s, ada, edit); err != nil {
				t.Fatal(err)
			}
			assertPreference(forced, true)
		}
		for range 2 {
			if _, err := UpdatePerson(s, ada, PersonEdit{ResetOriginalPreference: true}); err != nil {
				t.Fatal(err)
			}
			assertPreference(false, false)
		}
	}
	after, err := People(s)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("reset changed names, identities, counts or another person: before %+v, after %+v", before, after)
	}
	var want, set bool
	if err := s.DB().QueryRow(`select prefer_original, prefer_original_set from people where id = ?`, bo).Scan(&want, &set); err != nil {
		t.Fatal(err)
	}
	if !want || !set {
		t.Fatalf("another person's preference changed: (%v, %v)", want, set)
	}
	if _, err := UpdatePerson(s, 9999, PersonEdit{ResetOriginalPreference: true}); !errors.Is(err, ErrNoPerson) {
		t.Fatalf("reset unknown person: %v", err)
	}
}

func TestResetOriginalPreferenceRefusesPartialEdits(t *testing.T) {
	s := open(t)
	ada, err := Resolve(s, KindEmail, "ada@example.com", "Ada Okoye")
	if err != nil {
		t.Fatal(err)
	}
	on := true
	if _, err := UpdatePerson(s, ada, PersonEdit{PreferOriginal: &on}); err != nil {
		t.Fatal(err)
	}
	before, err := PersonByID(s, ada)
	if err != nil {
		t.Fatal(err)
	}
	for _, forced := range []bool{false, true} {
		_, err := UpdatePerson(s, ada, PersonEdit{
			DisplayName: "Changed Name", Add: []string{"email:new@example.com"},
			Remove:         []string{"email:ada@example.com"},
			PreferOriginal: &forced, ResetOriginalPreference: true,
		})
		if !errors.Is(err, ErrConflictingOriginalPreference) {
			t.Fatalf("conflicting edit: %v", err)
		}
	}
	if _, err := UpdatePerson(s, ada, PersonEdit{
		DisplayName: "Changed Name", Add: []string{"not-an-identity"}, ResetOriginalPreference: true,
	}); !errors.Is(err, ErrBadIdentity) {
		t.Fatalf("bad identity with reset: %v", err)
	}
	after, err := PersonByID(s, ada)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("invalid edit partially applied: before %+v, after %+v", before, after)
	}
	if _, err := PersonByIdentity(s, KindDisplayName, "changed name"); err == nil {
		t.Fatal("invalid edit left a name identity")
	}
}
