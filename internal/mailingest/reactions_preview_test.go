package mailingest

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
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

func TestReactionPreviewByteBudgetsBeforeAnyProviderRead(t *testing.T) {
	for _, aggregate := range []bool{false, true} {
		t.Run(map[bool]string{false: "per-source", true: "aggregate"}[aggregate], func(t *testing.T) {
			s := store(t)
			count := 1
			size := corpus.ReactionSourceByteLimit + 1
			if aggregate {
				count = 5
				size = corpus.ReactionSourceByteLimit - (1 << 10)
			}
			var ids []string
			for n := 0; n < count; n++ {
				id := fmt.Sprintf("bounded-%d", n)
				_, err := s.Put(corpus.Entry{Source: "mail", ExtID: "mail:" + id, TS: time.Unix(1, 0), BodyText: strings.Repeat("x", size)}, &corpus.Mail{GmailID: id}, nil)
				if err != nil {
					t.Fatal(err)
				}
				ids = append(ids, id)
			}
			box := &reactionMailbox{}
			if _, err := PreviewReactions(s, box, "legacy", ids); err == nil || !strings.Contains(err.Error(), "byte budget") {
				t.Fatalf("oversized source accepted: %v", err)
			}
			if len(box.reads) != 0 {
				t.Fatalf("provider opened before complete local budget check: %v", box.reads)
			}
		})
	}
}

func independentReactionArtifacts(t *testing.T, s *corpus.Store, sources ...int64) {
	t.Helper()
	for _, source := range sources {
		rows, err := s.DB().Query(`select entry_id from sightings where seen_in=?`, source)
		if err != nil {
			t.Fatal(err)
		}
		var ids []int64
		for rows.Next() {
			var id int64
			if err = rows.Scan(&id); err != nil {
				t.Fatal(err)
			}
			ids = append(ids, id)
		}
		if err = rows.Err(); err != nil {
			t.Fatal(err)
		}
		rows.Close()
		for _, id := range ids {
			if err = s.Sight(id, 0, "direct", "synthetic independent proof"); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestReactionPreviewUsesOnlyPostApplyInspectedSourceAccounts(t *testing.T) {
	for _, inspectedB := range []bool{false, true} {
		t.Run(fmt.Sprint("inspected-B=", inspectedB), func(t *testing.T) {
			s := store(t)
			if err := s.PutGmailAccount(corpus.GmailAccount{ID: "B", DisplayName: "B"}); err != nil {
				t.Fatal(err)
			}
			m := reactionFixture(t)
			legacy := m
			legacy.ReactionInspection = nil
			source := mustReactionPut(t, s, "legacy", legacy)
			b := legacy
			b.ID = "B-source"
			if inspectedB {
				b.ReactionInspection = m.ReactionInspection
			}
			mustReactionPut(t, s, "B", b)
			mustReactionPut(t, s, "legacy", targetFixture())
			if _, err := s.PutForAccount("B", corpus.Entry{Source: "mail", ExtID: "mail:independent-duplicate-ref", TS: time.Unix(1, 0), BodyText: "B ordinary target"}, &corpus.Mail{GmailID: "B-target", MessageID: targetFixture().MessageID}, nil); err != nil {
				t.Fatal(err)
			}
			independentReactionArtifacts(t, s, source.ID)
			manifest, err := PreviewReactions(s, &reactionMailbox{messages: map[string]Message{m.ID: m}}, "legacy", []string{m.ID})
			if err != nil {
				t.Fatal(err)
			}
			item := manifest.Items[0]
			wantTarget, wantStatus := "mail:"+targetFixture().MessageID, "resolved"
			if inspectedB {
				wantTarget, wantStatus = "", "unresolved-missing-ambiguous-or-inaccessible"
			}
			if item.Refusal != "" || item.TargetExtID != wantTarget || item.Status != wantStatus {
				t.Fatalf("wrong inspected-account projection: %+v", item)
			}
			if changed, err := s.ApplyReactionManifest(manifest); err != nil || !changed {
				t.Fatalf("apply: %v %v", changed, err)
			}
			_, hidden, err := s.ReactionsFor("legacy", []string{"mail:" + targetFixture().MessageID})
			if err != nil {
				t.Fatal(err)
			}
			if hidden[item.Source.ExtID] != item.TargetExtID {
				t.Fatalf("review silently replanned: %+v versus %+v", item, hidden)
			}
		})
	}
}

func reactionSidecarSnapshot(t *testing.T, s *corpus.Store) string {
	t.Helper()
	var parts []string
	for _, table := range []string{"reaction_sources", "reaction_inspections", "reaction_applies", "sightings", "reconcile_state", "fold_dirty_entries", "entry_embeddings"} {
		rows, err := s.DB().Query(`select * from ` + table + ` order by 1`)
		if err != nil {
			t.Fatal(err)
		}
		cols, err := rows.Columns()
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			values := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for n := range values {
				ptrs[n] = &values[n]
			}
			if err = rows.Scan(ptrs...); err != nil {
				t.Fatal(err)
			}
			parts = append(parts, fmt.Sprintf("%s:%v", table, values))
		}
		if err = rows.Err(); err != nil {
			t.Fatal(err)
		}
		rows.Close()
	}
	return strings.Join(parts, "\n")
}

func TestReactionApplyRejectsEditedReviewedTargetAndStatusWithFullRollback(t *testing.T) {
	for _, edit := range []string{"target", "status"} {
		t.Run(edit, func(t *testing.T) {
			s := store(t)
			m := reactionFixture(t)
			legacy := m
			legacy.ReactionInspection = nil
			source := mustReactionPut(t, s, "legacy", legacy)
			mustReactionPut(t, s, "legacy", targetFixture())
			second := reactionFixture(t)
			second.ID = "second-source"
			second.MessageID = "<second-source@example.test>"
			secondLegacy := second
			secondLegacy.ReactionInspection = nil
			secondSource := mustReactionPut(t, s, "legacy", secondLegacy)
			independentReactionArtifacts(t, s, source.ID, secondSource.ID)
			manifest, err := PreviewReactions(s, &reactionMailbox{messages: map[string]Message{m.ID: m, second.ID: second}}, "legacy", []string{m.ID, second.ID})
			if err != nil {
				t.Fatal(err)
			}
			if edit == "target" {
				manifest.Items[1].TargetExtID = "mail:operator-edited-target"
			} else {
				manifest.Items[1].Status = "unresolved-self"
			}
			before := reactionSidecarSnapshot(t, s)
			if changed, err := s.ApplyReactionManifest(manifest); err == nil || changed || !strings.Contains(err.Error(), "post-apply association") {
				t.Fatalf("edited projection accepted: %v %v", changed, err)
			}
			if after := reactionSidecarSnapshot(t, s); after != before {
				t.Fatalf("refusal left partial sidecars, withdrawals or audit:\nbefore %s\nafter %s", before, after)
			}
		})
	}
}

func TestReactionPreviewProjectsWholeInterdependentBatchAndCycles(t *testing.T) {
	for _, cycle := range []bool{false, true} {
		for _, reverse := range []bool{false, true} {
			t.Run(fmt.Sprintf("cycle=%v/reverse=%v", cycle, reverse), func(t *testing.T) {
				s := store(t)
				mustReactionPut(t, s, "legacy", targetFixture())
				a, b := reactionFixture(t), reactionFixture(t)
				a.ID = "batch-a"
				a.MessageID = "<batch-a@example.test>"
				b.ID = "batch-b"
				b.MessageID = "<batch-b@example.test>"
				a.InReplyTo = b.MessageID
				a.ReactionInspection.InReplyTo = []string{b.MessageID}
				a.ReactionInspection.TargetMessageID = b.MessageID
				if cycle {
					b.InReplyTo = a.MessageID
					b.ReactionInspection.InReplyTo = []string{a.MessageID}
					b.ReactionInspection.TargetMessageID = a.MessageID
				}
				for _, m := range []Message{a, b} {
					legacy := m
					legacy.ReactionInspection = nil
					r := mustReactionPut(t, s, "legacy", legacy)
					independentReactionArtifacts(t, s, r.ID)
				}
				ids := []string{a.ID, b.ID}
				if reverse {
					ids = []string{b.ID, a.ID}
				}
				manifest, err := PreviewReactions(s, &reactionMailbox{messages: map[string]Message{a.ID: a, b.ID: b}}, "legacy", ids)
				if err != nil {
					t.Fatal(err)
				}
				for _, item := range manifest.Items {
					if item.Refusal != "" {
						t.Fatalf("unexpected refusal %+v", item)
					}
					if item.GmailID == a.ID || cycle {
						if item.TargetExtID != "" || item.Status != "unresolved-reaction-target" {
							t.Fatalf("batch target classification ignored: %+v", item)
						}
					} else if item.Status != "resolved" {
						t.Fatalf("ordinary target unresolved: %+v", item)
					}
				}
				if changed, err := s.ApplyReactionManifest(manifest); err != nil || !changed {
					t.Fatalf("post-apply diverged: %v %v", changed, err)
				}
				_, hidden, err := s.ReactionsFor("legacy", []string{"mail:" + a.MessageID, "mail:" + b.MessageID, "mail:" + targetFixture().MessageID})
				if err != nil {
					t.Fatal(err)
				}
				for _, item := range manifest.Items {
					if hidden[item.Source.ExtID] != item.TargetExtID {
						t.Fatalf("batch silently replanned %+v: %+v", item, hidden)
					}
				}
				for _, m := range []Message{a, b} {
					sh, err := s.Show("mail:" + m.MessageID)
					if err != nil || sh.Body != m.Body {
						t.Fatal("source lost", err)
					}
				}
			})
		}
	}
}

func TestReactionPreviewConservativeFallbacksMatchApply(t *testing.T) {
	for _, kind := range []string{"missing-late", "ambiguous", "self", "inaccessible"} {
		t.Run(kind, func(t *testing.T) {
			s := store(t)
			m := reactionFixture(t)
			if kind == "self" {
				m.InReplyTo = m.MessageID
				m.ReactionInspection.InReplyTo = []string{m.MessageID}
				m.ReactionInspection.TargetMessageID = m.MessageID
			}
			legacy := m
			legacy.ReactionInspection = nil
			source := mustReactionPut(t, s, "legacy", legacy)
			switch kind {
			case "ambiguous":
				mustReactionPut(t, s, "legacy", targetFixture())
				if _, err := s.Put(corpus.Entry{Source: "mail", ExtID: "mail:duplicate-ref", TS: time.Unix(1, 0)}, &corpus.Mail{GmailID: "duplicate-ref", MessageID: targetFixture().MessageID}, nil); err != nil {
					t.Fatal(err)
				}
			case "inaccessible":
				if err := s.PutGmailAccount(corpus.GmailAccount{ID: "B", DisplayName: "B"}); err != nil {
					t.Fatal(err)
				}
				b := m
				b.ID = "B-source"
				mustReactionPut(t, s, "B", b)
				mustReactionPut(t, s, "B", targetFixture())
			}
			independentReactionArtifacts(t, s, source.ID)
			manifest, err := PreviewReactions(s, &reactionMailbox{messages: map[string]Message{m.ID: m}}, "legacy", []string{m.ID})
			if err != nil {
				t.Fatal(err)
			}
			item := manifest.Items[0]
			want := "unresolved-missing-ambiguous-or-inaccessible"
			if kind == "self" {
				want = "unresolved-self"
			}
			if kind == "inaccessible" {
				want = "unresolved-inaccessible"
			}
			if item.Refusal != "" || item.TargetExtID != "" || item.Status != want {
				t.Fatalf("fallback changed: %+v", item)
			}
			if changed, err := s.ApplyReactionManifest(manifest); err != nil || !changed {
				t.Fatalf("fallback apply diverged: %v %v", changed, err)
			}
			if reactionCount(t, s, "legacy") != 0 {
				t.Fatal("fallback hid source under unavailable target")
			}
			if sh, err := s.Show(item.Source.ExtID); err != nil || sh.Body != m.Body {
				t.Fatal("fallback source lost", err)
			}
			if kind == "missing-late" {
				mustReactionPut(t, s, "legacy", targetFixture())
				if reactionCount(t, s, "legacy") != 1 {
					t.Fatal("late exact target no longer resolves")
				}
			}
		})
	}
}
