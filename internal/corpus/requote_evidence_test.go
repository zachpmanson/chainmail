package corpus

import (
	"strings"
	"testing"
	"time"
)

func TestRepairQuotedBodiesDivergentEvidencePreservesStoredVersion(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"truncated", "Hiya Lane\n\nFollowing on from our phone call this week I can confirm these arrangements."},
		{"annotated", foldMessage + "\n\nINVENTED QUOTER ANNOTATION: this is not part of the original message."},
		{"different", strings.Replace(foldMessage, "20th", "21st", -1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _, _, id, _ := foldStored(t)
			beforeBody, beforeSHA := bodyOf(t, s, id)
			addRepairHost(t, s, id, tc.name, strings.Replace(foldForwardBody(), foldMessage, tc.body, 1))
			for i := 0; i < 2; i++ {
				r, err := RepairQuotedBodies(s)
				if err != nil || r.Fixed != 0 || len(r.Ambiguous) != 1 || len(r.Missing) != 0 {
					t.Fatalf("ambiguous evidence must abstain: %+v, %v", r, err)
				}
				if body, sha := bodyOf(t, s, id); body != beforeBody || sha != beforeSHA {
					t.Fatal("ambiguous evidence changed stored body/hash")
				}
				checkRepairIndexes(t, s)
			}
		})
	}
}

func TestRepairQuotedBodiesMissingHostsAbstainAndMissingEntriesAreUnique(t *testing.T) {
	s, _, host, id, recovered := foldStored(t)
	addRepairHost(t, s, id, "absent", "Invented host without a quoted block.")
	r, err := RepairQuotedBodies(s)
	if err != nil || r.Fixed != 1 || r.Hosts != 2 || len(r.Missing) != 0 || len(r.Ambiguous) != 0 {
		t.Fatalf("agreement plus missing host should repair once: %+v, %v", r, err)
	}
	if body, _ := bodyOf(t, s, id); body != recovered {
		t.Fatal("missing host overruled real evidence")
	}
	lonely, _, err := s.PutQuoted(Entry{
		Source: SourceMail, ExtID: "quote:invented-missing", TS: time.Unix(1_700_000_000, 0),
		Subject: "Invented subject", BodyText: "Invented stored message.",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Sight(lonely, host, "quoted", "depth 0"); err != nil {
		t.Fatal(err)
	}
	addRepairHost(t, s, lonely, "missing-again", "Another invented host without quotes.")
	beforeBody, beforeSHA := bodyOf(t, s, lonely)
	r, err = RepairQuotedBodies(s)
	if err != nil || r.Fixed != 0 || len(r.Missing) != 1 || r.Missing[0] != "quote:invented-missing" {
		t.Fatalf("report unique missing entries, not sightings: %+v, %v", r, err)
	}
	if body, sha := bodyOf(t, s, lonely); body != beforeBody || sha != beforeSHA {
		t.Fatal("missing evidence changed content")
	}
	checkRepairIndexes(t, s)
}

func TestQuoteRepairWriterRejectsStalePlansAndMailboxRows(t *testing.T) {
	s, mailbox, _, quote, _ := foldStored(t)
	for _, tc := range []struct {
		name    string
		id      int64
		subject string
		stale   bool
	}{
		{"stale body", quote, "Fernbrook sites", true},
		{"stale subject", quote, "Different planned subject", false},
		{"authoritative mailbox", mailbox, "Fernbrook sites", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			beforeBody, beforeSHA := bodyOf(t, s, tc.id)
			planned := beforeBody
			if tc.stale {
				planned += " stale snapshot"
			}
			changed, err := rewriteQuotedBody(s, tc.id, tc.subject, planned, "Invented proposed replacement.")
			if err != nil || changed {
				t.Fatalf("unsafe write: changed=%t, err=%v", changed, err)
			}
			if body, sha := bodyOf(t, s, tc.id); body != beforeBody || sha != beforeSHA {
				t.Fatal("rejected plan mutated content/hash")
			}
			checkRepairIndexes(t, s)
		})
	}
}

func TestQuoteRepairNoOpDoesNotDirtyFoldsOrRewriteOpaqueHash(t *testing.T) {
	s, _, _, id, _ := foldStored(t)
	body, _ := bodyOf(t, s, id)
	// Existing hashes are opaque staleness tokens: do not bulk invalidate
	// vectors merely because the previous repair used a different field order.
	if _, err := s.DB().Exec(`update entries set body_sha='legacy-opaque-hash' where id=?`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB().Exec(`delete from fold_dirty_entries; delete from fold_dirty_people`); err != nil {
		t.Fatal(err)
	}
	changed, err := rewriteQuotedBody(s, id, "Fernbrook sites", body, body)
	if err != nil || changed {
		t.Fatalf("no-op changed=%t, err=%v", changed, err)
	}
	if _, sha := bodyOf(t, s, id); sha != "legacy-opaque-hash" {
		t.Fatal("unchanged source unnecessarily invalidated its vector")
	}
	var dirty int
	if err := s.DB().QueryRow(`select count(*) from fold_dirty_entries`).Scan(&dirty); err != nil || dirty != 0 {
		t.Fatalf("no-op dirtied folds: %d, %v", dirty, err)
	}
	checkRepairIndexes(t, s)
}
