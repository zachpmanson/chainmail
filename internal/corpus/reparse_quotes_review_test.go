package corpus

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestQuotedMetadataMessageIDDoesNotHideHostClockDisagreement(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		t.Run(map[bool]string{false: "forward", true: "reverse"}[reverse], func(t *testing.T) {
			s := open(t)
			id, host, _ := seedLegacyLocale(t, s)
			first := strings.Replace(localeHost, "Subject:", "Message-ID: <same-message@example.test>\nSubject:", 1)
			second := strings.Replace(first, "22:07", "22:08", 1)
			if reverse {
				first, second = second, first
			}
			if _, err := s.DB().Exec(`update entries set body_text=? where id=?`, first, host); err != nil {
				t.Fatal(err)
			}
			h, err := s.Put(Entry{Source: SourceMail, ExtID: "mail:second-host", TS: time.Now(), BodyText: second}, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			if err = s.Sight(id, h.ID, "quoted", "depth 0"); err != nil {
				t.Fatal(err)
			}
			a, b := RecoverQuotes(first).Distinct[0], RecoverQuotes(second).Distinct[0]
			if a.Key != b.Key || a.Block.Text != b.Block.Text || a.Address != b.Address || a.Sent.Equal(b.Sent) {
				t.Fatal("fixture does not isolate clocks", a, b)
			}
			before := querySnapshot(t, s, `select * from entries order by id`)
			p := localePlan(t, s)
			if len(p.Changes) != 0 || len(p.Skipped) != 1 || p.Skipped[0].Reason != "host_disagreement" {
				t.Fatalf("conflicting clocks accepted: %+v", p)
			}
			if n, err := s.ApplyQuotedMetadata(context.Background(), p); err != nil || n != 0 {
				t.Fatal(n, err)
			}
			if before != querySnapshot(t, s, `select * from entries order by id`) {
				t.Fatal("refused clocks changed rows")
			}
		})
	}
}

func TestQuotedMetadataRejectsUnsupportedSchemaWithoutSourceRevisionChange(t *testing.T) {
	for _, version := range []int{27, len(migrations) + 1} {
		t.Run(map[bool]string{true: "future", false: "old"}[version > len(migrations)], func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "corpus.db")
			s, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			seedLegacyLocale(t, s)
			plan := localePlan(t, s)
			requireLocaleChange(t, plan)
			state := reconcileState(t, s)
			if _, err = s.DB().Exec(`delete from schema_version; insert into schema_version values(?)`, version); err != nil {
				t.Fatal(err)
			}
			if reconcileState(t, s) != state {
				t.Fatal("schema edit unexpectedly changed source revision")
			}
			before := protectedLocaleSnapshot(t, s)
			entries := querySnapshot(t, s, `select * from entries order by id`)
			aliases := querySnapshot(t, s, `select * from quote_key_aliases order by 1`)
			if _, err = s.PlanQuotedMetadata(context.Background(), plan.Selection, plan.Limits); err == nil {
				t.Error("planning unsupported schema accepted")
			}
			if n, err := s.ApplyQuotedMetadata(context.Background(), plan); err == nil || n != 0 {
				t.Error("reviewed apply on unsupported schema accepted", n, err)
			}
			if !reflect.DeepEqual(before, protectedLocaleSnapshot(t, s)) || entries != querySnapshot(t, s, `select * from entries order by id`) || aliases != querySnapshot(t, s, `select * from quote_key_aliases order by 1`) || reconcileState(t, s) != state {
				t.Error("unsupported schema changed protected data/metadata/aliases/revision")
			}
			// DELETE mode makes journal-mode changes by a refused opener observable.
			if _, err = s.DB().Exec(`pragma journal_mode=delete`); err != nil {
				t.Fatal(err)
			}
			if err = s.Close(); err != nil {
				t.Fatal(err)
			}
			freezeQuoteFixture(t, path)
			bytes, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			ro, err := OpenReadOnly(path)
			if err == nil {
				ro.Close()
				t.Error("readonly unsupported schema accepted")
			}
			assertNoSQLiteCompanions(t, path)
			if err = os.Chmod(path, 0600); err != nil {
				t.Fatal(err)
			}
			if version > len(migrations) {
				normal, err := Open(path)
				if err == nil {
					normal.Close()
					t.Error("ordinary Open accepted future database")
				}
			}
			after, err := os.ReadFile(path)
			if err != nil || !reflect.DeepEqual(bytes, after) {
				t.Error("refused open changed main file", err)
			}
		})
	}
}

func TestOpenFutureSchemaPreflightReadsCommittedWAL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "live.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	seedLegacyLocale(t, s)
	if _, err = s.DB().Exec(`pragma wal_checkpoint(truncate)`); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// The new version is committed only in WAL, not in the main file.
	if _, err = s.DB().Exec(`insert into schema_version values(?)`, len(migrations)+1); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("fixture checkpointed future version", err)
	}
	for _, opener := range []func(string) (*Store, error){Open, OpenForMetadata} {
		got, err := opener(path)
		if err == nil {
			got.Close()
			t.Fatal("committed future schema in WAL ignored")
		}
		if !strings.Contains(err.Error(), "unsupported corpus schema") {
			t.Fatal(err)
		}
	}
}
