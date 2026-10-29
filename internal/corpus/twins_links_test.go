package corpus

import (
	"encoding/json"
	"os"
	"testing"
	"time"
)

type replacedLinkCase struct {
	Name, Subject, Original, Quoted string
	OffsetMinutes                   int
}

func replacedLinkCases(t *testing.T) []replacedLinkCase {
	t.Helper()
	data, err := os.ReadFile("../../testdata/quotes/replaced-links.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []replacedLinkCase
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	return cases
}

func TestReplacedQuoteLinksRemainAnnotations(t *testing.T) {
	for _, tc := range replacedLinkCases(t) {
		t.Run(tc.Name, func(t *testing.T) {
			s, err := Open(":memory:")
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			author := person(t, s, "rowan@example.test", "Rowan Example")
			sent := time.Date(2026, 3, 4, 2, 23, 38, 0, time.UTC)
			put, err := s.Put(Entry{Source: SourceMail, ExtID: "mail:<original@example.test>",
				TS: sent, PersonID: author, Subject: tc.Subject, Container: "pilot",
				BodyText: tc.Original}, &Mail{MessageID: "<original@example.test>"}, nil)
			if err != nil {
				t.Fatal(err)
			}
			base := put.ID
			wall := sent.Truncate(time.Minute).Add(time.Duration(tc.OffsetMinutes) * time.Minute)
			if id, ok, err := FindTwin(s, author, wall, tc.Quoted, tc.Subject, "pilot"); err != nil || ok {
				t.Fatalf("changed URL must not disappear into twin %d: ok=%v err=%v", id, ok, err)
			}
			if d, ok, err := FindDerived(s, author, wall, tc.Quoted, tc.Subject, "pilot"); err != nil || !ok || d.Base != base {
				t.Errorf("modified quote base = %+v, ok=%v err=%v; want %d", d, ok, err, base)
			}
			// Simulate the quote arriving before its original: repair must make
			// the same classification without deleting either destination.
			copy, _, err := s.PutQuoted(Entry{Source: SourceMail, ExtID: "quote:replaced-link",
				TS: wall, PersonID: author, Subject: tc.Subject, Container: "pilot", BodyText: tc.Quoted})
			if err != nil {
				t.Fatal(err)
			}
			for _, apply := range []bool{false, true, true} {
				plan, err := CollapseTwins(s, apply)
				if err != nil {
					t.Fatal(err)
				}
				if plan.Removed != 0 || len(plan.Derived) != 1 || plan.Derived[0] != (DerivedRelation{Copy: copy, Base: base}) {
					t.Fatalf("apply=%v: plan=%+v; want one retained annotated quote", apply, plan)
				}
			}
			var derived bool
			var parent int64
			var original, quoted string
			if err := s.DB().QueryRow("select derived, parent_id, body_text from entries where id=?", copy).Scan(&derived, &parent, &quoted); err != nil {
				t.Fatal(err)
			}
			if err := s.DB().QueryRow("select body_text from entries where id=?", base).Scan(&original); err != nil {
				t.Fatal(err)
			}
			if !derived || parent != base || original != tc.Original || quoted != tc.Quoted {
				t.Fatal("repair lost a body or did not persist the annotation")
			}
		})
	}
}
