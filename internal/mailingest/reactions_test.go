package mailingest

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/zachpmanson/chainmail/internal/corpus"
	"github.com/zachpmanson/chainmail/internal/spec"
	"github.com/zachpmanson/docket/gmail/mail"
)

func reactionFixture(t *testing.T) Message {
	t.Helper()
	raw, err := os.ReadFile("../../testdata/gmail-reaction-synthetic.json")
	if err != nil {
		t.Fatal(err)
	}
	var m Message
	if err = json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if m.ReactionInspection.Version != mail.ReactionInspectionVersion {
		t.Fatal("synthetic inspection fixture version drift")
	}
	if !reactionOnly(m) {
		t.Fatal("fixture not independently valid")
	}
	return m
}
func targetFixture() Message {
	return Message{Envelope: Envelope{ID: "synthetic-target", MessageID: "<synthetic-target@example.test>", ThreadID: "synthetic-thread", From: "Alex Vale <alex@example.test>", Date: "Fri, 06 Nov 2026 21:27:00 +0000", Subject: "Synthetic target"}, Body: "Synthetic target content uniqueauthoredtoken"}
}
func mustReactionPut(t *testing.T, s *corpus.Store, account string, m Message) corpus.PutResult {
	t.Helper()
	r, err := PutForAccount(s, account, m)
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func reactionCount(t *testing.T, s *corpus.Store, account string) int {
	t.Helper()
	groups, _, err := s.ReactionsFor(account, []string{"mail:<synthetic-target@example.test>"})
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, g := range groups["mail:<synthetic-target@example.test>"] {
		n += len(g.Events)
	}
	return n
}
func TestReceivedReactionLateTargetUnicodeAndSourceIdentity(t *testing.T) {
	s := store(t)
	m := reactionFixture(t)
	first := mustReactionPut(t, s, "legacy", m)
	if reactionCount(t, s, "") != 0 {
		t.Fatal("missing target resolved")
	}
	var quotes int
	if err := s.DB().QueryRow(`select count(*) from sightings where seen_in=?`, first.ID).Scan(&quotes); err != nil || quotes != 0 {
		t.Fatalf("reaction extracted quoted fallback: %d %v", quotes, err)
	}
	// Source remains normal and searchable while unresolved.
	hits, err := s.SearchEntries(corpus.Query{Text: "Casey"})
	if err != nil || len(hits) != 1 {
		t.Fatalf("unresolved fallback: %+v %v", hits, err)
	}
	mustReactionPut(t, s, "legacy", targetFixture())
	if reactionCount(t, s, "") != 1 {
		t.Fatal("late target did not resolve")
	}
	for range 2 {
		mustReactionPut(t, s, "legacy", m)
	}
	for n, emoji := range []string{"❤️", "👍🏽", "👩‍💻", "👨‍👩‍👧‍👦", "🇦🇺"} {
		copy := reactionFixture(t)
		copy.ID = "event-" + string(rune('a'+n))
		copy.MessageID = "<" + copy.ID + "@example.test>"
		copy.ReactionInspection.Candidates[0].Emoji = emoji
		payload, _ := json.Marshal(map[string]any{"version": 1, "emoji": emoji})
		copy.ReactionInspection.Candidates[0].Body = string(payload)
		mustReactionPut(t, s, "legacy", copy)
	}
	if reactionCount(t, s, "") != 6 {
		t.Fatal("same-sender distinct events merged or re-sync inflated")
	}
	if _, err = corpus.CollapseTwins(s, true); err != nil {
		t.Fatal(err)
	}
	if reactionCount(t, s, "") != 6 {
		t.Fatal("twins changed source event identities")
	}
	for _, q := range []corpus.Query{{Text: "Casey"}, {Text: "reacted"}, {Text: "example.test"}, {IncludeNoise: true, Text: "Casey"}} {
		hits, err = s.SearchEntries(q)
		if err != nil {
			t.Fatal(err)
		}
		for _, hit := range hits {
			if hit.ExtID != "mail:<synthetic-target@example.test>" {
				t.Fatalf("active source in ordinary search: %+v", hit)
			}
		}
	}
	hits, err = s.SearchEntries(corpus.Query{Text: "uniqueauthoredtoken"})
	if err != nil || len(hits) != 1 {
		t.Fatalf("target content must match once: %+v %v", hits, err)
	}
	shown, err := s.Show("mail:" + m.MessageID)
	if err != nil || shown.Body != m.Body || !shown.HasOriginal {
		t.Fatalf("source lost: %+v %v", shown, err)
	}
	evidence, err := s.ReactionEvidence("mail:"+m.MessageID, "")
	if err != nil || len(evidence) != 1 {
		t.Fatalf("evidence lost/inflated: %+v %v", evidence, err)
	}
	if _, err = s.ResolveParents(); err != nil {
		t.Fatal(err)
	}
	meta, err := s.ChainMeta("mail:<synthetic-target@example.test>")
	if err != nil || meta.Entries != 1 || meta.Unread != 6 {
		t.Fatalf("authored/provider count semantics: %+v %v", meta, err)
	}
	sp, err := spec.Generate(s, spec.Options{Containers: []string{"synthetic-thread"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(sp.Messages) != 1 || len(sp.Messages[0].Reactions) != 5 {
		t.Fatalf("shared static model: %+v", sp.Messages)
	}
	raw, err := json.Marshal(sp)
	if err != nil {
		t.Fatal(err)
	}
	var roundtrip spec.Spec
	if err = json.Unmarshal(raw, &roundtrip); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(roundtrip.Messages[0].Reactions, sp.Messages[0].Reactions) {
		t.Fatal("Unicode/event model did not round-trip")
	}
	stats, err := s.Stats()
	if err != nil || stats.Entries != 7 {
		t.Fatalf("source totals changed: %+v %v", stats, err)
	}
}

func TestReceivedReactionAccountCopiesAndConflictingEvidence(t *testing.T) {
	s := store(t)
	if err := s.PutGmailAccount(corpus.GmailAccount{ID: "other", DisplayName: "Other"}); err != nil {
		t.Fatal(err)
	}
	target := targetFixture()
	mustReactionPut(t, s, "other", target)
	m := reactionFixture(t)
	mustReactionPut(t, s, "legacy", m)
	if reactionCount(t, s, "") != 0 {
		t.Fatal("cross-account resolution")
	}
	// One logical source copied to a mailbox containing the target is one event.
	m.ID = "other-source"
	mustReactionPut(t, s, "other", m)
	if reactionCount(t, s, "") != 1 || reactionCount(t, s, "other") != 1 || reactionCount(t, s, "legacy") != 0 {
		t.Fatal("account/copy semantics")
	}
	hits, err := s.SearchEntries(corpus.Query{AccountID: "legacy", Text: "Casey"})
	if err != nil || len(hits) != 1 {
		t.Fatalf("inaccessible target must retain account fallback: %+v %v", hits, err)
	}
	m.ReactionInspection.Candidates[0].Emoji = "👍🏽"
	m.ReactionInspection.Candidates[0].Body = `{"version":1,"emoji":"👍🏽"}`
	mustReactionPut(t, s, "other", m)
	if reactionCount(t, s, "") != 0 {
		t.Fatal("last-writer-wins conflict")
	}
	status, err := s.ReactionStatus("mail:"+m.MessageID, "other")
	if err != nil || status != "conflict" {
		t.Fatalf("conflict not inspectable: %q %v", status, err)
	}
	evidence, err := s.ReactionEvidence("mail:"+m.MessageID, "other")
	if err != nil || len(evidence) != 2 {
		t.Fatalf("conflicting per-copy evidence not retained: %+v %v", evidence, err)
	}
	// Even re-reading the original evidence cannot erase the conflict.
	old := reactionFixture(t)
	old.ID = m.ID
	mustReactionPut(t, s, "other", old)
	if reactionCount(t, s, "") != 0 {
		t.Fatal("conflict erased by repeated sync")
	}
}

func TestReactionNegativesRetainOrdinaryMail(t *testing.T) {
	cases := map[string]func(*Message){
		"old-decoder":            func(m *Message) { m.ReactionInspection = nil },
		"unsafe-inspection-v1":   func(m *Message) { m.ReactionInspection.Version = 1 },
		"unknown-inspection":     func(m *Message) { m.ReactionInspection.Version = 99 },
		"payload-version":        func(m *Message) { m.ReactionInspection.Candidates[0].Body = `{"version":2,"emoji":"❤️"}` },
		"payload-string-version": func(m *Message) { m.ReactionInspection.Candidates[0].Body = `{"version":"1","emoji":"❤️"}` },
		"duplicate-json": func(m *Message) {
			m.ReactionInspection.Candidates[0].Body = `{"version":1,"emoji":"❤️","emoji":"👍🏽"}`
		},
		"forged-emoji": func(m *Message) {
			m.ReactionInspection.Candidates[0].Emoji = "A"
			m.ReactionInspection.Candidates[0].Body = `{"version":1,"emoji":"A"}`
		},
		"bare-heart": func(m *Message) {
			m.ReactionInspection.Candidates[0].Emoji = "❤"
			m.ReactionInspection.Candidates[0].Body = `{"version":1,"emoji":"❤"}`
		},
		"two-emojis": func(m *Message) {
			m.ReactionInspection.Candidates[0].Emoji = "❤️👍🏽"
			m.ReactionInspection.Candidates[0].Body = `{"version":1,"emoji":"❤️👍🏽"}`
		},
		"wrong-payload-emoji":    func(m *Message) { m.ReactionInspection.Candidates[0].Emoji = "👍🏽" },
		"unsupported-rfc":        func(m *Message) { m.ReactionInspection.Candidates[0].Format = "rfc9078" },
		"mixed":                  func(m *Message) { m.ReactionInspection.Layout = "other" },
		"incomplete-fallback":    func(m *Message) { m.ReactionInspection.Fallbacks[0].BodyComplete = false },
		"missing-fallback":       func(m *Message) { m.ReactionInspection.Fallbacks = nil },
		"duplicate-irt":          func(m *Message) { m.ReactionInspection.InReplyTo = append(m.ReactionInspection.InReplyTo, m.InReplyTo) },
		"references-only":        func(m *Message) { m.InReplyTo = ""; m.References = []string{"<synthetic-target@example.test>"} },
		"contradictory-envelope": func(m *Message) { m.InReplyTo = "<different@example.test>" },
		"body-only-localized":    func(m *Message) { m.ReactionInspection = nil; m.Body = "❤️\nCasey a réagi via Gmail" },
		"ordinary-emoji":         func(m *Message) { m.ReactionInspection = nil; m.Body = "❤️" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			s := store(t)
			mustReactionPut(t, s, "legacy", targetFixture())
			m := reactionFixture(t)
			mutate(&m)
			mustReactionPut(t, s, "legacy", m)
			if reactionCount(t, s, "") != 0 {
				t.Fatal("negative silently consumed")
			}
			sp, err := spec.Generate(s, spec.Options{Containers: []string{"synthetic-thread"}})
			if err != nil || len(sp.Messages) < 2 {
				t.Fatalf("ordinary source hidden: %d %v", len(sp.Messages), err)
			}
		})
	}
}

func TestOldDecoderContentChangeCannotHideOrEraseReactionConflict(t *testing.T) {
	s := store(t)
	mustReactionPut(t, s, "legacy", targetFixture())
	m := reactionFixture(t)
	mustReactionPut(t, s, "legacy", m)
	if reactionCount(t, s, "") != 1 {
		t.Fatal("fixture did not resolve")
	}
	old := reactionFixture(t)
	old.ReactionInspection = nil
	old.Body += " extra authored content from an older decoder"
	mustReactionPut(t, s, "legacy", old)
	if reactionCount(t, s, "") != 0 {
		t.Fatal("stale evidence hid changed content")
	}
	mustReactionPut(t, s, "legacy", m)
	if reactionCount(t, s, "") != 0 {
		t.Fatal("last writer erased uninspected content conflict")
	}
	evidence, err := s.ReactionEvidence("mail:"+m.MessageID, "")
	if err != nil || len(evidence) != 2 || evidence[0].SourceDigest == evidence[1].SourceDigest {
		t.Fatalf("source snapshot conflict not inspectable: %+v %v", evidence, err)
	}
}
