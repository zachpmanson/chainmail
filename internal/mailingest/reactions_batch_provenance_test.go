package mailingest

import (
	"testing"
	"time"

	"github.com/zachpmanson/chainmail/internal/corpus"
)

func TestReactionBatchCannotWithdrawItsOwnIndependentQuoteEvidence(t *testing.T) {
	s := store(t)
	m1 := reactionFixture(t)
	m2 := m1
	m2.ID = "synthetic-second-reaction"
	m2.MessageID = "<synthetic-second-reaction@example.test>"
	legacy1, legacy2 := m1, m2
	legacy1.ReactionInspection, legacy2.ReactionInspection = nil, nil
	s1 := mustReactionPut(t, s, "legacy", legacy1)
	s2 := mustReactionPut(t, s, "legacy", legacy2)
	mustReactionPut(t, s, "legacy", targetFixture())
	q, _, err := s.PutQuoted(corpus.Entry{Source: "mail", ExtID: "quote:mutual-reaction-only-evidence", TS: time.Unix(1, 0), BodyText: "Synthetic historical quote witnessed only by the two selected notifications."})
	if err != nil {
		t.Fatal(err)
	}
	for _, source := range []int64{s1.ID, s2.ID} {
		if err = s.Sight(q, source, "quoted", "synthetic mutual evidence"); err != nil {
			t.Fatal(err)
		}
	}
	box := &reactionMailbox{messages: map[string]Message{m1.ID: m1, m2.ID: m2}}
	// Each source separately has independent evidence in the other source.
	one, err := PreviewReactions(s, box, "legacy", []string{m1.ID})
	if err != nil || one.Items[0].Refusal != "" {
		t.Fatalf("single-source preview: %+v %v", one, err)
	}
	two, err := PreviewReactions(s, box, "legacy", []string{m2.ID})
	if err != nil || two.Items[0].Refusal != "" {
		t.Fatalf("single-source preview: %+v %v", two, err)
	}
	combined := one
	combined.Items = append(combined.Items, two.Items...)
	if changed, err := s.ApplyReactionManifest(combined); err == nil || changed {
		t.Fatalf("batch withdrew the only evidence supporting its own independence: changed=%t err=%v", changed, err)
	}
	var sightings, inspections, audits int
	if err = s.DB().QueryRow(`select count(*) from sightings where entry_id=?`, q).Scan(&sightings); err != nil || sightings != 2 {
		t.Fatalf("mutual provenance changed: %d %v", sightings, err)
	}
	if err = s.DB().QueryRow(`select count(*) from reaction_inspections`).Scan(&inspections); err != nil || inspections != 0 {
		t.Fatalf("partial sidecar apply: %d %v", inspections, err)
	}
	if err = s.DB().QueryRow(`select count(*) from reaction_applies`).Scan(&audits); err != nil || audits != 0 {
		t.Fatalf("partial audit apply: %d %v", audits, err)
	}
	if _, err = PreviewReactions(s, box, "legacy", []string{m1.ID, m2.ID}); err == nil {
		t.Fatal("batch preview claimed mutually withdrawn provenance was independent")
	}
	// Genuine evidence outside the selected set permits the same batch.
	rows, err := s.DB().Query(`select distinct entry_id from sightings where seen_in in (?,?)`, s1.ID, s2.ID)
	if err != nil {
		t.Fatal(err)
	}
	var independent []int64
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		independent = append(independent, id)
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	rows.Close()
	for _, id := range independent {
		if err = s.Sight(id, 0, "direct", "synthetic evidence outside the batch"); err != nil {
			t.Fatal(err)
		}
	}
	allowed, err := PreviewReactions(s, box, "legacy", []string{m1.ID, m2.ID})
	if err != nil {
		t.Fatal(err)
	}
	if changed, err := s.ApplyReactionManifest(allowed); err != nil || !changed {
		t.Fatalf("genuinely independent batch refused: changed=%t err=%v", changed, err)
	}
	if err = s.DB().QueryRow(`select count(*) from sightings where entry_id=? and seen_in is null`, q).Scan(&sightings); err != nil || sightings != 1 {
		t.Fatalf("outside evidence lost: %d %v", sightings, err)
	}
	if reactionCount(t, s, "legacy") != 2 {
		t.Fatal("independent notifications did not both resolve")
	}
}
