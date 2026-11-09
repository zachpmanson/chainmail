package corpus

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zachpmanson/chainmail/internal/unnest"
)

const localeOwn = "Hello, here is the community garden bulletin."
const localeHost = "---------- Forwarded message ---------\nDa: Alex Vale <alex@example.test>\nDate: lun 5 ott 2026, 22:07\nSubject: Monthly bulletin\nTo: Robin Reed <robin@example.test>\n\n" + localeOwn

// This fixture writes the original v28 row directly: neither its key nor its
// fallback clock/basis comes from the corrected parser or PutQuoted defaults.
func seedLegacyLocale(t *testing.T, s *Store) (int64, int64, int64) {
	t.Helper()
	p := person(t, s, "alex@example.test", "Alex Vale")
	h, err := s.Put(Entry{Source: SourceMail, ExtID: "mail:synthetic-host", TS: time.Date(2026, 10, 7, 1, 2, 0, 0, time.UTC), Container: "synthetic-locales", BodyText: localeHost}, &Mail{MessageID: "synthetic-host"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := s.db.Exec(`insert into entries(source,ext_id,kind,ts,person_id,container,subject,body_text,body_sha,quoted,ingested_at) values('mail',?,'message',?,?,?,'Monthly bulletin',?,?,1,0)`, unnest.TextKey(localeOwn), time.Date(2026, 10, 7, 1, 2, 0, 0, time.UTC).Unix(), p, "synthetic-locales", localeOwn, "opaque-legacy-hash")
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	tx, err := s.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err = s.reindex(tx, id, true, "", ""); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err = s.Sight(id, h.ID, "quoted", "depth 0, time inferred from host"); err != nil {
		t.Fatal(err)
	}
	return id, h.ID, p
}
func localePlan(t *testing.T, s *Store) QuoteMetadataPlan {
	t.Helper()
	p, err := s.PlanQuotedMetadata(context.Background(), QuoteSelection{ExtID: unnest.TextKey(localeOwn)}, DefaultQuoteLimits)
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func requireLocaleChange(t *testing.T, p QuoteMetadataPlan) {
	t.Helper()
	if len(p.Changes) != 1 {
		t.Fatalf("changes=%+v skips=%+v", p.Changes, p.Skipped)
	}
	c := p.Changes[0]
	if c.TS != time.Date(2026, 10, 5, 22, 7, 0, 0, time.UTC).Unix() || c.TZ != "" || c.Basis != "stated" || c.Precision != "minute" {
		t.Fatalf("wrong wall time/zone/precision: %+v", c)
	}
	data, _ := json.Marshal(p)
	for _, secret := range []string{localeOwn, "alex@example.test", "Monthly bulletin", "Da:"} {
		if strings.Contains(string(data), secret) {
			t.Fatalf("preview leaks %q", secret)
		}
	}
}
func querySnapshot(t *testing.T, s *Store, query string) string {
	t.Helper()
	rows, err := s.db.Query(query)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	cols, _ := rows.Columns()
	var out [][]any
	for rows.Next() {
		vs := make([]any, len(cols))
		ptr := make([]any, len(cols))
		for i := range vs {
			ptr[i] = &vs[i]
		}
		if err = rows.Scan(ptr...); err != nil {
			t.Fatal(err)
		}
		out = append(out, vs)
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(out)
	return string(b)
}
func protectedLocaleSnapshot(t *testing.T, s *Store) map[string]string {
	t.Helper()
	out := map[string]string{}
	// All entry columns other than the four authorised metadata columns.
	out["entries"] = querySnapshot(t, s, `select id,source,ext_id,kind,tz_offset,person_id,container,parent_id,parent_ref,subject,body_html,body_text,permalink,body_sha,ingested_at,quoted,derived from entries order by id`)
	for _, table := range []string{"people", "identities", "participants", "sightings", "mail_detail", "gmail_copies", "attachments", "entry_embeddings", "render_offsets"} {
		out[table] = querySnapshot(t, s, "select * from "+table+" order by 1")
	}
	out["mailbox_times"] = querySnapshot(t, s, `select id,ts,tz from entries where quoted=0 order by id`)
	return out
}
func checkLocaleIntegrity(t *testing.T, s *Store) {
	t.Helper()
	checkRepairIndexes(t, s)
	rows, err := s.db.Query(`pragma foreign_key_check`)
	if err != nil {
		t.Fatal(err)
	}
	if rows.Next() {
		t.Fatal("foreign key violation")
	}
	rows.Close()
	var v string
	if err = s.db.QueryRow(`pragma integrity_check`).Scan(&v); err != nil || v != "ok" {
		t.Fatalf("integrity %s: %v", v, err)
	}
	var snippet string
	if err = s.db.QueryRow(`select snippet(entries_fts,1,'[',']','...',10) from entries_fts where entries_fts match 'bulletin' limit 1`).Scan(&snippet); err != nil {
		t.Fatal(err)
	}
}
func TestQuotedMetadataLegacyPreviewApplyProtectedAndIdempotent(t *testing.T) {
	s := open(t)
	id, host, pid := seedLegacyLocale(t, s)
	if _, err := s.db.Exec(`insert into entry_embeddings(entry_id,model,dim,body_sha,prep,skip,embedded_at) values(?,'synthetic',3,'opaque-legacy-hash',1,'no topic',0)`, id); err != nil {
		t.Fatal(err)
	}
	before := protectedLocaleSnapshot(t, s)
	state := reconcileState(t, s)
	plan := localePlan(t, s)
	requireLocaleChange(t, plan)
	if !reflect.DeepEqual(before, protectedLocaleSnapshot(t, s)) || state != reconcileState(t, s) {
		t.Fatal("preview wrote corpus")
	}
	n, err := s.ApplyQuotedMetadata(context.Background(), plan)
	if err != nil || n != 1 {
		t.Fatalf("apply %d: %v", n, err)
	}
	if !reflect.DeepEqual(before, protectedLocaleSnapshot(t, s)) {
		t.Fatal("protected data changed")
	}
	var ext, basis, precision string
	var off sql.NullInt64
	if err = s.db.QueryRow(`select ext_id,quote_time_basis,quote_time_precision,tz_offset from entries where id=?`, id).Scan(&ext, &basis, &precision, &off); err != nil {
		t.Fatal(err)
	}
	if ext != unnest.TextKey(localeOwn) || basis != "stated" || precision != "minute" || off.Valid {
		t.Fatal(ext, basis, precision, off)
	}
	if _, err = s.ApplyQuotedMetadata(context.Background(), plan); !errors.Is(err, ErrReconcileConflict) {
		t.Fatalf("repeat must refuse stale: %v", err)
	}
	fresh := localePlan(t, s)
	if len(fresh.Changes) != 0 {
		t.Fatal("fresh plan must be satisfied")
	}
	if n, err = s.ApplyQuotedMetadata(context.Background(), fresh); err != nil || n != 0 {
		t.Fatal(n, err)
	}
	rec := RecoverQuotes(localeHost).Distinct[0]
	got, err := s.ResolveQuotedKey(context.Background(), host, rec)
	if err != nil || got != id {
		t.Fatal(got, err)
	}
	if rec.Sent.IsZero() || !recoveredAuthor(s.db, rec, pid) {
		t.Fatal("fixture")
	}
	rep, err := RepairQuotedBodies(s)
	if err != nil || rep.Fixed != 0 || len(rep.Missing) != 0 {
		t.Fatal(rep, err)
	}
	checkLocaleIntegrity(t, s)
}

func TestQuotedKeyBridgeDoesNotPromoteFallbackOrForgetInference(t *testing.T) {
	s := open(t)
	id, host, _ := seedLegacyLocale(t, s)
	old, err := readQuoteRow(s.db, id, true)
	if err != nil {
		t.Fatal(err)
	}
	rec := RecoverQuotes(localeHost).Distinct[0]
	for i := 0; i < 2; i++ {
		got, err := s.ResolveQuotedKey(context.Background(), host, rec)
		if err != nil || got != id {
			t.Fatal(got, err)
		}
		if err = s.EnrichQuoted(id, Entry{TZ: "NZDT", BodyText: localeOwn}); err != nil {
			t.Fatal(err)
		}
		if err = s.Sight(id, host, "quoted", "depth 0"); err != nil {
			t.Fatal(err)
		}
	}
	now, _ := readQuoteRow(s.db, id, true)
	if now.TS != old.TS || now.Basis != "" || now.TZ != "" {
		t.Fatal("reingest promoted fallback", now)
	}
	var detail string
	s.db.QueryRow(`select detail from sightings where entry_id=?`, id).Scan(&detail)
	if !strings.Contains(detail, "time inferred") {
		t.Fatal("lost inference")
	}
	plan := localePlan(t, s)
	requireLocaleChange(t, plan)
	if _, err = s.db.Exec(`update entries set body_text=replace(body_text,'22:07','22:08') where id=?`, host); err != nil {
		t.Fatal(err)
	}
	changed := RecoverQuotes(strings.Replace(localeHost, "22:07", "22:08", 1)).Distinct[0]
	if _, err = s.ResolveQuotedKey(context.Background(), host, changed); err == nil {
		t.Fatal("changed date retargeted legacy alias")
	}
	rep, err := RepairQuotedBodies(s)
	if err != nil || len(rep.Missing) != 1 {
		t.Fatal(rep, err)
	}
}

func TestQuotedMetadataRefusalsAndAtomicStaleness(t *testing.T) {
	for _, tc := range []struct{ name, sql, reason string }{
		{"lost inference", `update sightings set detail='depth 0' where entry_id=?`, "unproved_inference"},
		{"derived", `update entries set derived=1 where id=?`, "derived"},
		{"promoted", `update entries set quoted=0 where id=?`, "mailbox_authority"},
		{"mail detail", `insert into mail_detail(entry_id,message_id) values(?,'authority')`, "mailbox_authority"},
		{"gmail copy", `insert into gmail_copies(account_id,gmail_id,entry_id) values('legacy','authority',?)`, "mailbox_authority"},
		{"wrong person", `update entries set person_id=null where id=?`, "author_mismatch"},
		{"sighting deleted", `delete from sightings where entry_id=?`, "missing_host"},
		{"own body changed", `update entries set body_text='different' where id=?`, "not_legacy_text_key"},
		{"zone", `update entries set tz='NZDT' where id=?`, "zone_conflict"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := open(t)
			id, _, _ := seedLegacyLocale(t, s)
			plan := localePlan(t, s)
			requireLocaleChange(t, plan)
			if _, err := s.db.Exec(tc.sql, id); err != nil {
				t.Fatal(err)
			}
			before := protectedLocaleSnapshot(t, s)
			if _, err := s.ApplyQuotedMetadata(context.Background(), plan); !errors.Is(err, ErrReconcileConflict) {
				t.Fatalf("stale apply: %v", err)
			}
			if !reflect.DeepEqual(before, protectedLocaleSnapshot(t, s)) {
				t.Fatal("stale plan wrote data")
			}
			fresh := localePlan(t, s)
			if len(fresh.Changes) != 0 || len(fresh.Skipped) != 1 || fresh.Skipped[0].Reason != tc.reason {
				t.Fatalf("%+v", fresh)
			}
		})
	}
	for _, change := range []string{
		`update entries set body_text=replace(body_text,'22:07','22:08') where id=?`,
		`update entries set body_text=replace(body_text,'alex@example.test','other@example.test') where id=?`,
		`update entries set body_text=replace(body_text,'22:07','25:61') where id=?`,
		`update entries set body_text='truncated' where id=?`,
		`update entries set body_html='<p>changed</p>' where id=?`,
	} {
		s := open(t)
		_, host, _ := seedLegacyLocale(t, s)
		plan := localePlan(t, s)
		if _, err := s.db.Exec(change, host); err != nil {
			t.Fatal(err)
		}
		if _, err := s.ApplyQuotedMetadata(context.Background(), plan); !errors.Is(err, ErrReconcileConflict) {
			t.Fatal(change, err)
		}
	}
}

func TestQuotedMetadataCompleteConsensusBudgetsAndCollisions(t *testing.T) {
	for _, mode := range []string{"agree", "date", "zone", "partial", "multiple", "host_limit", "bytes_limit", "key_collision", "other_host"} {
		t.Run(mode, func(t *testing.T) {
			s := open(t)
			id, host, pid := seedLegacyLocale(t, s)
			body := localeHost
			switch mode {
			case "date":
				body = strings.Replace(body, "22:07", "22:08", 1)
			case "zone":
				body = strings.Replace(body, "22:07", "22:07 +0200", 1)
			case "partial":
				body = "truncated"
			case "multiple":
				body += "\n\n" + localeHost
			}
			h, err := s.Put(Entry{Source: SourceMail, ExtID: "mail:another-host", TS: time.Now(), Container: "outside-selected-container", BodyText: body}, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			if mode != "other_host" {
				if err = s.Sight(id, h.ID, "quoted", "depth 0"); err != nil {
					t.Fatal(err)
				}
			}
			limits := DefaultQuoteLimits
			if mode == "host_limit" {
				limits.Hosts = 1
			}
			if mode == "bytes_limit" {
				limits.Bytes = int64(len(localeHost) - 1)
			}
			if mode == "key_collision" {
				rec := RecoverQuotes(localeHost).Distinct[0]
				_, _, err = s.PutQuoted(Entry{Source: SourceMail, ExtID: rec.Key, TS: rec.Sent, PersonID: pid, BodyText: localeOwn})
				if err != nil {
					t.Fatal(err)
				}
			}
			p, err := s.PlanQuotedMetadata(context.Background(), QuoteSelection{ExtID: unnest.TextKey(localeOwn)}, limits)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "agree" || mode == "other_host" {
				requireLocaleChange(t, p)
			} else if len(p.Changes) != 0 {
				t.Fatalf("unsafe consensus %s: %+v", mode, p)
			}
			if mode == "host_limit" || mode == "bytes_limit" {
				if len(p.Skipped) != 1 || p.Skipped[0].Reason != "evidence_limit" {
					t.Fatalf("%+v", p)
				}
			}
			if mode == "other_host" {
				rec := RecoverQuotes(body).Distinct[0]
				if _, err = s.ResolveQuotedKey(context.Background(), h.ID, rec); err == nil {
					t.Fatal("same-body other-host bridged")
				}
			}
			_ = host
		})
	}
}

func TestQuoteMetadataReadOnlyPreMigrationAndMigrationNoBackfill(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`pragma foreign_keys=on; create table schema_version(v integer primary key)`); err != nil {
		t.Fatal(err)
	}
	for i, m := range migrations[:28] {
		if _, err = db.Exec(m); err != nil {
			t.Fatalf("migration %d: %v", i+1, err)
		}
		if _, err = db.Exec(`insert into schema_version values(?)`, i+1); err != nil {
			t.Fatal(err)
		}
	}
	s := &Store{db: db}
	id, _, _ := seedLegacyLocale(t, s)
	beforeSchema := querySnapshot(t, s, `select type,name,sql from sqlite_schema order by type,name`)
	before := protectedLocaleSnapshot(t, s)
	s.Close()
	beforeFile, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	ro, err := OpenReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	p := localePlan(t, ro)
	requireLocaleChange(t, p)
	if got := querySnapshot(t, ro, `select type,name,sql from sqlite_schema order by type,name`); got != beforeSchema {
		t.Fatal("preview migrated schema")
	}
	if _, err = ro.db.Exec(`update entries set ts=0`); err == nil {
		t.Fatal("readonly handle writable")
	}
	ro.Close()
	afterFile, _ := os.ReadFile(path)
	if !reflect.DeepEqual(beforeFile, afterFile) {
		t.Fatal("readonly changed database bytes")
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if !reflect.DeepEqual(before, protectedLocaleSnapshot(t, s)) {
		t.Fatal("migration rewrote legacy corpus")
	}
	r, err := readQuoteRow(s.db, id, true)
	if err != nil || r.Basis != "" || r.Precision != "" {
		t.Fatal(r, err)
	}
	if _, err = s.ApplyQuotedMetadata(context.Background(), p); err != nil {
		t.Fatalf("reviewed pre-migration plan failed: %v", err)
	}
	checkLocaleIntegrity(t, s)
}

func TestQuoteMetadataPlanValidationPagingAndCancel(t *testing.T) {
	s := open(t)
	seedLegacyLocale(t, s)
	for _, sel := range []QuoteSelection{{}, {ExtID: "a", Container: "b"}, {ExtID: "a", AfterID: -1}} {
		if _, err := s.PlanQuotedMetadata(context.Background(), sel, DefaultQuoteLimits); err == nil {
			t.Fatal("unbounded selection accepted")
		}
	}
	for _, limits := range []QuoteLimits{{101, 1000, 32 << 20}, {100, 1001, 32 << 20}, {100, 1000, 33 << 20}, {0, 1, 1}} {
		if _, err := s.PlanQuotedMetadata(context.Background(), QuoteSelection{ExtID: "a"}, limits); err == nil {
			t.Fatal("bad limits")
		}
	}
	p, err := s.PlanQuotedMetadata(context.Background(), QuoteSelection{Container: "synthetic-locales"}, QuoteLimits{1, 1000, 32 << 20})
	if err != nil || !p.More || p.NextID == 0 {
		t.Fatal(p, err)
	}
	next, err := s.PlanQuotedMetadata(context.Background(), QuoteSelection{Container: "synthetic-locales", AfterID: p.NextID}, QuoteLimits{1, 1000, 32 << 20})
	if err != nil || len(next.Changes) != 1 {
		t.Fatal(next, err)
	}
	p = localePlan(t, s)
	p.Changes[0].TS++
	if _, err = s.ApplyQuotedMetadata(context.Background(), p); err == nil {
		t.Fatal("tampered digest accepted")
	}
	p = localePlan(t, s)
	p.ParserVersion++
	p.Digest = planDigest(p)
	if _, err = s.ApplyQuotedMetadata(context.Background(), p); err == nil {
		t.Fatal("wrong parser accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = s.PlanQuotedMetadata(ctx, QuoteSelection{ExtID: "a"}, DefaultQuoteLimits); err == nil {
		t.Fatal("cancellation ignored")
	}
}

func TestInferredClocksExcludedFromBulkNearbyDerivedAndMeasurements(t *testing.T) {
	for _, basis := range []string{"inferred", ""} {
		t.Run(fmt.Sprintf("basis_%s", basis), func(t *testing.T) {
			s := open(t)
			id, host, pid := seedLegacyLocale(t, s)
			if basis != "" {
				if _, err := s.db.Exec(`update entries set quote_time_basis=? where id=?`, basis, id); err != nil {
					t.Fatal(err)
				}
			}
			if err := s.Sight(id, host, "quoted", "depth 1"); err != nil {
				t.Fatal(err)
			}
			wall := time.Date(2026, 10, 7, 1, 2, 0, 0, time.UTC)
			near, err := nearbyCopies(s, pid, wall)
			if err != nil || len(near) != 0 {
				t.Fatal(near, err)
			}
			if _, ok, err := FindTwin(s, pid, wall, localeOwn, "Monthly bulletin", "synthetic-locales"); err != nil || ok {
				t.Fatal(ok, err)
			}
			if _, ok, err := FindDerived(s, pid, wall, localeOwn+" additional annotation", "Monthly bulletin", "synthetic-locales"); err != nil || ok {
				t.Fatal(ok, err)
			}
			mb, err := s.Put(Entry{Source: SourceMail, ExtID: "mail:potential-twin", PersonID: pid, TS: wall.Add(-time.Hour), Container: "synthetic-locales", Subject: "Monthly bulletin", BodyText: localeOwn}, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			bulk, err := CollapseTwins(s, false)
			if err != nil || len(bulk.Collapse) != 0 || bulk.Measured != 0 {
				t.Fatal(bulk, err)
			}
			if _, ok, err := measureOffset(s.db, mb.ID, id); err != nil || ok {
				t.Fatal(ok, err)
			}
			// Even if historical detail was already lost, an unverified textkey is not
			// evidence for a clock. Explicit inferred state survives the same edit.
			if _, err = s.db.Exec(`update sightings set detail='depth 2' where entry_id=?`, id); err != nil {
				t.Fatal(err)
			}
			copies, err := twinCopies(s.db, pid)
			if err != nil || len(copies) != 1 {
				t.Fatal(copies, err)
			}
		})
	}
}

func TestLegacyAddressDateKeyStillProvesClock(t *testing.T) {
	s := open(t)
	id, _, pid := seedLegacyLocale(t, s)
	wall := time.Date(2026, 10, 5, 22, 7, 0, 0, time.UTC)
	key := unnest.Key(unnest.Recovered{Address: "alex@example.test", Sent: wall})
	if _, err := s.db.Exec(`update entries set ext_id=?,ts=? where id=?`, key, wall.Unix(), id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`delete from sightings where entry_id=?`, id); err != nil {
		t.Fatal(err)
	}
	copies, err := twinCopies(s.db, pid)
	if err != nil || len(copies) != 1 {
		t.Fatal(copies, err)
	}
	if _, err = s.db.Exec(`update entries set ts=ts+60 where id=?`, id); err != nil {
		t.Fatal(err)
	}
	copies, err = twinCopies(s.db, pid)
	if err != nil || len(copies) != 0 {
		t.Fatal("unproved legacy clock eligible", copies, err)
	}
}

func TestQuoteAliasAbsorbPreservesTargetHostFKAndHistoricalEvidence(t *testing.T) {
	for _, moveHost := range []bool{false, true} {
		t.Run(fmt.Sprint(moveHost), func(t *testing.T) {
			s := open(t)
			id, host, pid := seedLegacyLocale(t, s)
			rec := RecoverQuotes(localeHost).Distinct[0]
			if _, err := s.ResolveQuotedKey(context.Background(), host, rec); err != nil {
				t.Fatal(err)
			}
			var oldDigest string
			if err := s.db.QueryRow(`select host_digest from quote_key_aliases`).Scan(&oldDigest); err != nil {
				t.Fatal(err)
			}
			body, personID, drop := localeOwn, pid, id
			if moveHost {
				body, personID, drop = localeHost, 0, host
			}
			keep, err := s.Put(Entry{Source: SourceMail, ExtID: "mail:absorbing-survivor", TS: time.Now(), PersonID: personID, BodyText: body}, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			tx, err := s.db.Begin()
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			if err = absorb(tx, s, keep.ID, drop); err != nil {
				t.Fatal(err)
			}
			if err = tx.Commit(); err != nil {
				t.Fatal(err)
			}
			var target, evidenceHost, originalHost int64
			var digest, oldExt string
			if err = s.db.QueryRow(`select entry_id,host_id,original_host_id,host_digest,old_ext_id from quote_key_aliases`).Scan(&target, &evidenceHost, &originalHost, &digest, &oldExt); err != nil {
				t.Fatal(err)
			}
			if originalHost != host || digest != oldDigest || oldExt != unnest.TextKey(localeOwn) {
				t.Fatal("lost historical alias evidence")
			}
			if moveHost {
				if target != id || evidenceHost != keep.ID {
					t.Fatal(target, evidenceHost)
				}
				if _, err = s.ResolveQuotedKey(context.Background(), keep.ID, rec); err == nil {
					t.Fatal("moved source silently became new evidence")
				}
			} else {
				if target != keep.ID || evidenceHost != host {
					t.Fatal(target, evidenceHost)
				}
				got, err := s.ResolveQuotedKey(context.Background(), host, rec)
				if err != nil || got != keep.ID {
					t.Fatal(got, err)
				}
			}
			checkLocaleIntegrity(t, s)
		})
	}
}

func TestQuoteAliasRevalidatesAuthorSourceSightingAndKeyCollision(t *testing.T) {
	for _, mode := range []string{"author", "host", "sighting", "collision"} {
		t.Run(mode, func(t *testing.T) {
			s := open(t)
			id, host, _ := seedLegacyLocale(t, s)
			rec := RecoverQuotes(localeHost).Distinct[0]
			if _, err := s.ResolveQuotedKey(context.Background(), host, rec); err != nil {
				t.Fatal(err)
			}
			var err error
			switch mode {
			case "author":
				_, err = s.db.Exec(`update entries set person_id=null where id=?`, id)
			case "host":
				_, err = s.db.Exec(`update entries set body_text=replace(body_text,'22:07','22:08') where id=?`, host)
			case "sighting":
				_, err = s.db.Exec(`delete from sightings where entry_id=?`, id)
			case "collision":
				_, err = s.Put(Entry{Source: SourceMail, ExtID: rec.Key, TS: rec.Sent, BodyText: localeOwn}, nil, nil)
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err = s.ResolveQuotedKey(context.Background(), host, rec); err == nil {
				t.Fatal("stale alias accepted")
			}
		})
	}
}

func TestQuotedBodyRepairUsesValidatedAliasWithoutClockWrites(t *testing.T) {
	s := open(t)
	id, host, _ := seedLegacyLocale(t, s)
	rec := RecoverQuotes(localeHost).Distinct[0]
	if _, err := s.ResolveQuotedKey(context.Background(), host, rec); err != nil {
		t.Fatal(err)
	}
	// Existing body repair must still work after the identity bridge is recorded;
	// this models a separate historical header-tail/body defect, not a date apply.
	if _, err := rewriteQuotedBody(s, id, "Monthly bulletin", localeOwn, "Old header residue.\n"+localeOwn); err != nil {
		t.Fatal(err)
	}
	before, _ := readQuoteRow(s.db, id, true)
	rep, err := RepairQuotedBodies(s)
	if err != nil || rep.Fixed != 1 || len(rep.Missing) != 0 {
		t.Fatal(rep, err)
	}
	after, _ := readQuoteRow(s.db, id, true)
	if after.Body != localeOwn || after.TS != before.TS || after.TZ != before.TZ || after.Basis != before.Basis || after.Precision != before.Precision {
		t.Fatal("body repair changed metadata", before, after)
	}
	checkLocaleIntegrity(t, s)
}

func TestLegacyMessageIDClockRequiresCompleteMatchingSource(t *testing.T) {
	s := open(t)
	id, host, pid := seedLegacyLocale(t, s)
	body := strings.Replace(localeHost, "Subject:", "Message-ID: <synthetic-message@example.test>\nSubject:", 1)
	wall := time.Date(2026, 10, 5, 22, 7, 0, 0, time.UTC)
	if _, err := s.db.Exec(`update entries set ext_id='mail:synthetic-message@example.test',ts=? where id=?`, wall.Unix(), id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`delete from sightings where entry_id=?`, id); err != nil {
		t.Fatal(err)
	}
	if err := s.Sight(id, host, "quoted", "depth 0"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`update entries set body_text=? where id=?`, body, host); err != nil {
		t.Fatal(err)
	}
	copies, err := twinCopies(s.db, pid)
	if err != nil || len(copies) != 1 {
		t.Fatal("source-proved legacy Message-ID clock lost", copies, err)
	}
	if _, err := s.db.Exec(`update entries set body_text=replace(body_text,'22:07','22:08') where id=?`, host); err != nil {
		t.Fatal(err)
	}
	copies, err = twinCopies(s.db, pid)
	if err != nil || len(copies) != 0 {
		t.Fatal("changed source proves old clock", copies, err)
	}
}

func TestQuoteBridgeRefusesEmptyBodyAndChangedAuthor(t *testing.T) {
	for _, mode := range []string{"empty", "author"} {
		t.Run(mode, func(t *testing.T) {
			s := open(t)
			id, host, _ := seedLegacyLocale(t, s)
			body := localeHost
			if mode == "empty" {
				if _, err := s.db.Exec(`update entries set body_text='',ext_id=? where id=?`, unnest.TextKey(""), id); err != nil {
					t.Fatal(err)
				}
				p, err := s.PlanQuotedMetadata(context.Background(), QuoteSelection{ExtID: unnest.TextKey("")}, DefaultQuoteLimits)
				if err != nil || len(p.Changes) != 0 {
					t.Fatal(p, err)
				}
			} else {
				person(t, s, "other@example.test", "Different Author")
				body = strings.Replace(body, "alex@example.test", "other@example.test", 1)
				if _, err := s.db.Exec(`update entries set body_text=? where id=?`, body, host); err != nil {
					t.Fatal(err)
				}
				rec := RecoverQuotes(body).Distinct[0]
				if _, err := s.ResolveQuotedKey(context.Background(), host, rec); err == nil {
					t.Fatal("different author bridged identical body")
				}
			}
		})
	}
}

func TestQuoteMetadataBoundsSelectedBodyBeforeLoadingEvidence(t *testing.T) {
	s := open(t)
	seedLegacyLocale(t, s)
	p, err := s.PlanQuotedMetadata(context.Background(), QuoteSelection{ExtID: unnest.TextKey(localeOwn)}, QuoteLimits{Entries: 1, Hosts: 1, Bytes: 1})
	if err != nil || len(p.Changes) != 0 || len(p.Skipped) != 1 || p.Skipped[0].Reason != "evidence_limit" || p.Bytes != 0 || p.Hosts != 0 {
		t.Fatal(p, err)
	}
}
