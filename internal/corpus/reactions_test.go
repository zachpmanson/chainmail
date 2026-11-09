package corpus

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zachpmanson/docket/gmail/mail"
)

func corpusReactionFixture(t *testing.T) (Entry, *Mail) {
	t.Helper()
	raw, err := os.ReadFile("../../testdata/gmail-reaction-synthetic.json")
	if err != nil {
		t.Fatal(err)
	}
	var decoded mail.Message
	if err = json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	e := Entry{Source: SourceMail, ExtID: "mail:" + decoded.MessageID, BodyText: decoded.Body, BodyHTML: decoded.BodyHTML, TS: time.Unix(1_799_222_400, 0)}
	m := &Mail{GmailID: decoded.ID, MessageID: decoded.MessageID, InReplyTo: decoded.InReplyTo, From: decoded.From, SourceDate: decoded.Date, ReactionInspection: decoded.ReactionInspection}
	return e, m
}
func putReactionTarget(t *testing.T, s *Store, ext, gmail, reference string) int64 {
	t.Helper()
	r, err := s.Put(Entry{Source: SourceMail, ExtID: ext, BodyText: "ordinary target text", TS: time.Unix(1, 0)}, &Mail{GmailID: gmail, MessageID: reference}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return r.ID
}
func TestReactionExactReferenceAmbiguitySelfAndRemapping(t *testing.T) {
	s := open(t)
	e, m := corpusReactionFixture(t)
	source, err := s.Put(e, m, nil)
	if err != nil {
		t.Fatal(err)
	}
	target := putReactionTarget(t, s, "mail:<synthetic-target@example.test>", "target", "<synthetic-target@example.test>")
	count := func(want int) {
		t.Helper()
		var n int
		if err := s.db.QueryRow(`select count(*) from reaction_active`).Scan(&n); err != nil || n != want {
			t.Fatalf("active=%d want%d err=%v", n, want, err)
		}
	}
	count(1)
	second := putReactionTarget(t, s, "mail:duplicate-reference", "second-target", "<synthetic-target@example.test>")
	count(0)
	if _, err = s.db.Exec(`delete from reaction_message_refs where entry_id=?`, second); err != nil {
		t.Fatal(err)
	}
	count(1)
	// Absorb the exact referenced target into an independently recovered copy.
	keep, _, err := s.PutQuoted(Entry{Source: SourceMail, ExtID: "quote:surviving-target", BodyText: "ordinary target text", TS: time.Unix(1, 0)})
	if err != nil {
		t.Fatal(err)
	}
	tx, err := s.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err = absorb(tx, s, keep, target); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	count(1)
	groups, hidden, err := s.ReactionsFor("legacy", []string{"quote:surviving-target"})
	if err != nil || len(groups["quote:surviving-target"]) != 1 || hidden[e.ExtID] != "quote:surviving-target" {
		t.Fatalf("exact target reference not remapped: %+v %+v %v", groups, hidden, err)
	}
	// Source identities cannot be absorbed into another body/sender twin.
	tx, err = s.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err = absorb(tx, s, second, source.ID); err == nil {
		tx.Rollback()
		t.Fatal("source notification identity absorbed")
	}
	tx.Rollback()
	count(1)
	// A lost target/copy cannot leave a stale hidden source.
	if _, err = s.db.Exec(`delete from gmail_copies where entry_id=?`, keep); err != nil {
		t.Fatal(err)
	}
	count(0)
	if _, err = s.db.Exec(`delete from sightings where entry_id=?;delete from entries where id=?`, keep, keep); err != nil {
		t.Fatal(err)
	}
	count(0)
	// Reingesting the same explicit target reference restores safe late resolution.
	putReactionTarget(t, s, "mail:<synthetic-target@example.test>", "target-again", "<synthetic-target@example.test>")
	count(1)
	// An explicit self reference is never redirected through thread parents.
	m.ReactionInspection.InReplyTo = []string{m.MessageID}
	m.ReactionInspection.TargetMessageID = m.MessageID
	m.InReplyTo = m.MessageID
	e.ExtID = "mail:self-event"
	m.GmailID = "self"
	m.MessageID = "self-event"
	// Use the source's own Message-ID as the exact target.
	m.MessageID = "<self-event@example.test>"
	e.ExtID = "mail:" + m.MessageID
	m.InReplyTo = m.MessageID
	m.ReactionInspection.InReplyTo = []string{m.MessageID}
	m.ReactionInspection.TargetMessageID = m.MessageID
	if _, err = s.Put(e, m, nil); err != nil {
		t.Fatal(err)
	}
	count(1)
}

func TestReactionMIMEOnlyEvidenceChangesInvalidateDerivedEligibility(t *testing.T) {
	s := open(t)
	e, m := corpusReactionFixture(t)
	putReactionTarget(t, s, "mail:<synthetic-target@example.test>", "target", "<synthetic-target@example.test>")
	inspected := m.ReactionInspection
	m.ReactionInspection = nil
	source, err := s.Put(e, m, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.WarmFolds(); err != nil {
		t.Fatal(err)
	}
	m.ReactionInspection = inspected
	again, err := s.Put(e, m, nil)
	if err != nil {
		t.Fatal(err)
	}
	if again.Changed || again.ID != source.ID {
		t.Fatal("MIME evidence rewrote logical content", again)
	}
	if _, err = s.WarmFolds(); err != nil {
		t.Fatal(err)
	}
	var members int
	if err = s.db.QueryRow(`select count(*) from fold_members where entry_id=?`, source.ID).Scan(&members); err != nil || members != 0 {
		t.Fatalf("active reaction in fold evidence: %d %v", members, err)
	}
	query, args := embedSelection("synthetic-model", 3)
	var pending int
	if err = s.db.QueryRow(`select count(*) `+query+` and e.id=?`, append(args, source.ID)...).Scan(&pending); err != nil || pending != 0 {
		t.Fatalf("active reaction selected for semantic prep: %d %v", pending, err)
	}
	// Conflicting same-source evidence restores ordinary fallback, including folds.
	changed := *inspected
	changed.Status = "unsupported"
	m.ReactionInspection = &changed
	if _, err = s.Put(e, m, nil); err != nil {
		t.Fatal(err)
	}
	if _, err = s.WarmFolds(); err != nil {
		t.Fatal(err)
	}
	if err = s.db.QueryRow(`select count(*) from fold_members where entry_id=?`, source.ID).Scan(&members); err != nil || members != 1 {
		t.Fatalf("conflict did not restore ordinary folds: %d %v", members, err)
	}
}

func TestReactionSourceDeletionReingestRetainsLogicalEvent(t *testing.T) {
	s := open(t)
	e, m := corpusReactionFixture(t)
	r, err := s.Put(e, m, nil)
	if err != nil {
		t.Fatal(err)
	}
	putReactionTarget(t, s, "mail:<synthetic-target@example.test>", "target", "<synthetic-target@example.test>")
	if _, err = s.db.Exec(`delete from gmail_copies where entry_id=?;delete from mail_detail where entry_id=?;delete from entries where id=?`, r.ID, r.ID, r.ID); err != nil {
		t.Fatal(err)
	}
	var active int
	if err = s.db.QueryRow(`select count(*) from reaction_active`).Scan(&active); err != nil || active != 0 {
		t.Fatal("deleted source remained active", err)
	}
	if _, err = s.Put(e, m, nil); err != nil {
		t.Fatal(err)
	}
	groups, _, err := s.ReactionsFor("legacy", []string{"mail:<synthetic-target@example.test>"})
	if err != nil || len(groups["mail:<synthetic-target@example.test>"]) != 1 || len(groups["mail:<synthetic-target@example.test>"][0].Events) != 1 {
		t.Fatal("reingest changed logical event identity", groups, err)
	}
	var evidence int
	if err = s.db.QueryRow(`select count(*) from reaction_inspections where source_ext_id=?`, e.ExtID).Scan(&evidence); err != nil || evidence != 1 {
		t.Fatal("source replay inflated observations", err)
	}
}

func TestReactionApplyRefusesFutureSchemaUnderLock(t *testing.T) {
	s := open(t)
	if _, err := s.db.Exec(`insert into schema_version(v) values(?)`, len(migrations)+1); err != nil {
		t.Fatal(err)
	}
	before, err := readReconcileState(s.db)
	if err != nil {
		t.Fatal(err)
	}
	e, m := corpusReactionFixture(t)
	date, parseErr := time.Parse(time.RFC1123Z, m.SourceDate)
	if parseErr != nil {
		t.Fatal(parseErr)
	}
	e.TS = date
	item := ReactionPreviewItem{GmailID: m.GmailID, Source: ReactionSource{ExtID: e.ExtID, MessageID: m.MessageID, InReplyTo: m.InReplyTo, From: m.From, TS: e.TS.Unix(), Body: e.BodyText, BodyHTML: e.BodyHTML}, Precondition: "synthetic", Date: m.SourceDate, Inspection: m.ReactionInspection, EvidenceDigest: InspectionDigest(m.ReactionInspection)}
	_, err = s.ApplyReactionManifest(ReactionManifest{Version: 1, AccountID: "legacy", Items: []ReactionPreviewItem{item}})
	if err == nil || !strings.Contains(err.Error(), "supported schema") {
		t.Fatal("future schema not refused before apply", err)
	}
	after, err := readReconcileState(s.db)
	if err != nil || before != after {
		t.Fatal("future-schema refusal changed source state", err)
	}
}

func TestReactionPreviewFrozenSnapshotDoesNotCreateSQLiteCompanions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "frozen.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = OpenReadOnly(path); err == nil {
		t.Fatal("writable input accepted")
	}
	if err = os.Chmod(path, 0o400); err != nil {
		t.Fatal(err)
	}
	ro, err := OpenReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ro.db.Exec(`insert into people(display_name) values('must not write')`); err == nil {
		ro.Close()
		t.Fatal("snapshot writable")
	}
	if err = ro.Close(); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		if _, err = os.Stat(path + suffix); !os.IsNotExist(err) {
			t.Fatal("preview created companion", suffix, err)
		}
		if err = os.WriteFile(path+suffix, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err = OpenReadOnly(path); err == nil {
			t.Fatal("snapshot with live companion accepted", suffix)
		}
		if err = os.Remove(path + suffix); err != nil {
			t.Fatal(err)
		}
	}
}

func TestReactionMailboxActivityBrowseAndPagination(t *testing.T) {
	for _, account := range []string{"", "legacy"} {
		t.Run("scope="+account, func(t *testing.T) {
			s := open(t)
			target := putReactionTarget(t, s, "mail:<synthetic-target@example.test>", "target", "<synthetic-target@example.test>")
			if _, err := s.db.Exec(`update entries set ts=100,subject='Authored target' where id=?`, target); err != nil {
				t.Fatal(err)
			}
			e, m := corpusReactionFixture(t)
			e.TS = time.Unix(200, 0)
			m.Labels = []string{"INBOX", "UNREAD"}
			_, err := s.Put(e, m, nil)
			if err != nil {
				t.Fatal(err)
			}
			// The reaction's target association is authoritative even before parent repair.
			other, err := s.Put(Entry{Source: SourceMail, ExtID: "mail:other", TS: time.Unix(150, 0), BodyText: "other authored body"}, &Mail{GmailID: "other", Labels: []string{"INBOX", "UNREAD"}}, nil)
			if err != nil {
				t.Fatal(err)
			}
			for _, labels := range [][]string{nil, {"INBOX"}, {"UNREAD"}, {"INBOX", "UNREAD"}} {
				q := Query{AccountID: account, Labels: labels, Limit: 1}
				page, err := s.SearchChains(q)
				if err != nil {
					t.Fatal(err)
				}
				if len(page) != 1 || page[0].RootID != target || page[0].Last.Unix() != 200 || page[0].Unread != 1 || page[0].Entries != 1 || len(page[0].Best) != 1 || page[0].Best[0].ID != target || strings.Contains(page[0].Best[0].Snippet, "reacted") {
					t.Fatalf("activity/prose separation labels %v: %+v", labels, page)
				}
				q.After = &ChainCursor{Last: page[0].Last, RootExtID: page[0].RootExtID}
				page, err = s.SearchChains(q)
				if err != nil {
					t.Fatal(err)
				}
				if len(page) != 1 || page[0].RootID != other.ID {
					t.Fatalf("pagination lost next chain: %+v", page)
				}
				q.After = &ChainCursor{Last: page[0].Last, RootExtID: page[0].RootExtID}
				page, err = s.SearchChains(q)
				if err != nil || len(page) != 0 {
					t.Fatalf("pagination repeated: %+v %v", page, err)
				}
			}
			meta, err := s.ChainMetaForAccount("mail:<synthetic-target@example.test>", account)
			if err != nil || meta.Last.Unix() != 200 || meta.Entries != 1 || meta.Unread != 1 {
				t.Fatalf("meta: %+v %v", meta, err)
			}
			hits, err := s.SearchEntries(Query{AccountID: account, Text: "reacted"})
			if err != nil || len(hits) != 0 {
				t.Fatalf("fallback prose hit: %+v %v", hits, err)
			}
			// Two distinct notifications remain two unread source events, not per-person state.
			e.ExtID = "mail:<second-event@example.test>"
			m.GmailID = "second-event"
			m.MessageID = "<second-event@example.test>"
			e.TS = time.Unix(201, 0)
			if _, err = s.Put(e, m, nil); err != nil {
				t.Fatal(err)
			}
			meta, err = s.ChainMetaForAccount("mail:<synthetic-target@example.test>", account)
			if err != nil || meta.Unread != 2 || meta.Last.Unix() != 201 || meta.Entries != 1 {
				t.Fatalf("distinct event activity: %+v %v", meta, err)
			}
		})
	}
}

func TestReactionManifestValidateBeforeStoreAccess(t *testing.T) {
	e, m := corpusReactionFixture(t)
	date, err := time.Parse(time.RFC1123Z, m.SourceDate)
	if err != nil {
		t.Fatal(err)
	}
	item := ReactionPreviewItem{GmailID: m.GmailID, Source: ReactionSource{ExtID: e.ExtID, MessageID: m.MessageID, InReplyTo: m.InReplyTo, From: m.From, TS: date.Unix(), Body: e.BodyText, BodyHTML: e.BodyHTML}, Precondition: "synthetic", Date: m.SourceDate, Inspection: m.ReactionInspection, EvidenceDigest: InspectionDigest(m.ReactionInspection)}
	valid := ReactionManifest{Version: 1, AccountID: "legacy", Items: []ReactionPreviewItem{item}}
	if err = valid.Validate(); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*ReactionManifest){
		"version":    func(m *ReactionManifest) { m.Version = 2 },
		"account":    func(m *ReactionManifest) { m.AccountID = " " },
		"empty":      func(m *ReactionManifest) { m.Items = nil },
		"count":      func(m *ReactionManifest) { m.Items = make([]ReactionPreviewItem, 21) },
		"duplicate":  func(m *ReactionManifest) { m.Items = append(m.Items, m.Items[0]) },
		"date":       func(m *ReactionManifest) { m.Items[0].Date = "invalid" },
		"source-cap": func(m *ReactionManifest) { m.Items[0].Source.Body = strings.Repeat("x", ReactionSourceByteLimit+1) },
		"aggregate-cap": func(m *ReactionManifest) {
			for n := 0; n < 5; n++ {
				i := item
				i.GmailID = fmt.Sprint(n)
				i.Source.Body = strings.Repeat("x", ReactionSourceByteLimit-(1<<10))
				if n == 0 {
					m.Items[0] = i
				} else {
					m.Items = append(m.Items, i)
				}
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			manifest := valid
			manifest.Items = append([]ReactionPreviewItem{}, valid.Items...)
			change(&manifest)
			// A nil database intentionally proves pure refusal precedes locks/DB reads.
			if _, err := (&Store{}).ApplyReactionManifest(manifest); err == nil {
				t.Fatal("invalid manifest accepted")
			}
		})
	}
}
