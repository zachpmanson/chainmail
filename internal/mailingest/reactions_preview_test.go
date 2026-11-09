package mailingest

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/zachpmanson/chainmail/internal/corpus"
)

type reactionMailbox struct {
	messages map[string]Message
	reads    []string
}

func (m *reactionMailbox) Search(string, int, string) ([]Envelope, Page, error) {
	panic("preview searched mailbox")
}
func (m *reactionMailbox) Read(id string) (Message, error) {
	m.reads = append(m.reads, id)
	return m.messages[id], nil
}

func TestReactionPreviewIsReadOnlyAndApplyIsSeparateIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "corpus.db")
	s, err := corpus.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	m := reactionFixture(t)
	legacy := m
	legacy.ReactionInspection = nil
	source := mustReactionPut(t, s, "legacy", legacy)
	target := mustReactionPut(t, s, "legacy", targetFixture())
	// Simulate old quote evidence already independently/directly sighted.
	if err = s.Sight(target.ID, source.ID, "quoted", "synthetic old fallback"); err != nil {
		t.Fatal(err)
	}
	rows, err := s.DB().Query(`select entry_id from sightings where seen_in=?`, source.ID)
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
	rows.Close()
	for _, id := range independent {
		if err = s.Sight(id, 0, "direct", "synthetic independent sighting"); err != nil {
			t.Fatal(err)
		}
	}
	snapshot := filepath.Join(t.TempDir(), "frozen.db")
	if _, err = s.DB().Exec(`vacuum into ?`, snapshot); err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(snapshot, 0o400); err != nil {
		t.Fatal(err)
	}
	ro, err := corpus.OpenReadOnly(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	defer ro.Close()
	box := &reactionMailbox{messages: map[string]Message{m.ID: m}}
	var before, after int
	if err = s.DB().QueryRow(`select total_changes()`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	beforeSource, err := s.Show("mail:" + m.MessageID)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := PreviewReactions(ro, box, "legacy", []string{m.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Items) != 1 || manifest.Items[0].Refusal != "" || manifest.Items[0].QuoteSightings != 2 || manifest.Items[0].Status != "resolved" {
		t.Fatalf("preview: %+v", manifest)
	}
	if !reflect.DeepEqual(box.reads, []string{m.ID}) {
		t.Fatalf("unbounded read: %+v", box.reads)
	}
	if err = s.DB().QueryRow(`select total_changes()`).Scan(&after); err != nil || after != before {
		t.Fatalf("preview wrote: %d→%d %v", before, after, err)
	}
	if reactionCount(t, s, "") != 0 {
		t.Fatal("preview changed classification")
	}
	changed, err := s.ApplyReactionManifest(manifest)
	if err != nil || !changed {
		t.Fatalf("apply: %t %v", changed, err)
	}
	changed, err = s.ApplyReactionManifest(manifest)
	if err != nil || changed {
		t.Fatalf("replay: %t %v", changed, err)
	}
	if reactionCount(t, s, "") != 1 {
		t.Fatal("apply did not resolve")
	}
	afterSource, err := s.Show("mail:" + m.MessageID)
	if err != nil || !reflect.DeepEqual(beforeSource, afterSource) {
		t.Fatal("apply changed source/provenance", err)
	}
	var sightings, audit int
	if err = s.DB().QueryRow(`select count(*) from sightings where entry_id=? and kind='direct'`, target.ID).Scan(&sightings); err != nil || sightings != 1 {
		t.Fatal("independent/direct evidence lost", err)
	}
	if err = s.DB().QueryRow(`select count(*) from reaction_applies where withdrawn_sightings like '%synthetic old fallback%'`).Scan(&audit); err != nil || audit != 1 {
		t.Fatal("withdrawn evidence audit missing", err)
	}
}

func TestReactionPreviewRefusesUniqueOrphansAndChangedPreconditions(t *testing.T) {
	for _, orphan := range []bool{true, false} {
		t.Run(map[bool]string{true: "unique-orphan", false: "stale-target-or-source"}[orphan], func(t *testing.T) {
			s := store(t)
			m := reactionFixture(t)
			legacy := m
			legacy.ReactionInspection = nil
			source := mustReactionPut(t, s, "legacy", legacy)
			mustReactionPut(t, s, "legacy", targetFixture())
			if orphan {
				quote, _, err := s.PutQuoted(corpus.Entry{Source: "mail", ExtID: "quote:unique-reaction-artifact", TS: time.Unix(1, 0), BodyText: "unique content retained"})
				if err != nil {
					t.Fatal(err)
				}
				if err = s.Sight(quote, source.ID, "quoted", "only sighting"); err != nil {
					t.Fatal(err)
				}
			}
			box := &reactionMailbox{messages: map[string]Message{m.ID: m}}
			manifest, err := PreviewReactions(s, box, "legacy", []string{m.ID})
			if err != nil {
				t.Fatal(err)
			}
			if orphan && manifest.Items[0].Refusal == "" {
				t.Fatal("unsafe cleanup accepted")
			}
			if !orphan {
				target := targetFixture()
				target.Body += " changed since review"
				mustReactionPut(t, s, "legacy", target)
			}
			if changed, err := s.ApplyReactionManifest(manifest); err == nil || changed {
				t.Fatal("unsafe/stale manifest applied", changed, err)
			}
			if reactionCount(t, s, "") != 0 {
				t.Fatal("refusal partially wrote")
			}
		})
	}
}

func TestReactionPreviewBoundsAndChangedMailboxSource(t *testing.T) {
	s := store(t)
	m := reactionFixture(t)
	legacy := m
	legacy.ReactionInspection = nil
	mustReactionPut(t, s, "legacy", legacy)
	for _, ids := range [][]string{nil, {m.ID, m.ID}, {"missing"}, make([]string, 21)} {
		box := &reactionMailbox{messages: map[string]Message{m.ID: m}}
		if _, err := PreviewReactions(s, box, "legacy", ids); err == nil || len(box.reads) != 0 {
			t.Fatalf("bad bound read mailbox: %v %+v", err, box.reads)
		}
	}
	m.Body += " changed on provider"
	box := &reactionMailbox{messages: map[string]Message{m.ID: m}}
	manifest, err := PreviewReactions(s, box, "legacy", []string{m.ID})
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Items[0].Refusal == "" {
		t.Fatal("preview accepted changed source")
	}
	if changed, err := s.ApplyReactionManifest(manifest); err == nil || changed {
		t.Fatal("changed source applied")
	}
}
