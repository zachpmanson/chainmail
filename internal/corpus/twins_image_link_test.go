package corpus

import (
	"testing"
	"time"
)

func TestVisibleImageURLReplacementIsNotAnAttachmentPlaceholder(t *testing.T) {
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	author := person(t, s, "rowan@example.test", "Rowan Example")
	sent := time.Date(2026, 3, 4, 2, 0, 0, 0, time.UTC)
	prose := "Please review the inventory spreadsheet for the upcoming quarterly stock take. " +
		"We have included all the sites and their current numbers so that the team can prepare the next file. "
	original := prose + "<https://old.example.test/first.png>"
	quoted := prose + "<https://new.example.test/second.jpg>"
	base, err := s.Put(Entry{Source: SourceMail, ExtID: "mail:<image-link@example.test>",
		TS: sent, PersonID: author, BodyText: original}, &Mail{MessageID: "<image-link@example.test>"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := FindTwin(s, author, sent.Add(12*time.Hour), quoted, "", ""); err != nil || ok {
		t.Fatalf("visible destination was merged away: ok=%v err=%v", ok, err)
	}
	if d, ok, err := FindDerived(s, author, sent.Add(12*time.Hour), quoted, "", ""); err != nil || !ok || d.Base != base.ID {
		t.Fatalf("visible image URL was not an annotation: ok=%v err=%v", ok, err)
	}
}
