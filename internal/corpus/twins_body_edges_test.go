package corpus

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestTwinBodyCodecAndExactInputs(t *testing.T) {
	for _, quoted := range []bool{false, true} {
		for _, text := range []string{"", "repeat Repeat repeat", "a\x00b", askBody, "You don't often get email from invented@example.test. Learn why this is important\n" + askBody, "[image: a.png] <a.jpg> " + askBody, "one https://Example.test/Case?x=Y. two https://example.test/\xffRaw three https://Example.test/Case?x=Y.", "Begin\n\nOn Tue, 3 Jan 2023, Someone wrote:\n> Old words"} {
			for _, html := range []string{"", "<p>one two</p><p>three four</p>"} {
				c := twinCopy{quoted: quoted, bodyHTML: html}
				reduceTwinBody(&c, text)
				payload := encodeTwinBody(&c)
				var got twinCopy
				if payload == nil || !decodeTwinBody(payload, &got) || !reflect.DeepEqual(c.words, got.words) || !reflect.DeepEqual(c.prose, got.prose) || !reflect.DeepEqual(c.links, got.links) {
					t.Fatalf("codec %q %q quoted=%v", text, html, quoted)
				}
				key := twinBodyDigest("mail", text, html, quoted)
				cache := new(twinBodyCache)
				c.id = 1
				cache.reduce(&c, text, "mail", twinBodyVersion, key[:], payload)
				if cache.hits != 1 {
					t.Fatal("valid reduction missed")
				}
				for _, change := range []struct {
					source, body, html string
					quoted             bool
				}{
					{"slack", text, html, quoted}, {"mail", text + " ", html, quoted}, {"mail", text, html + " ", quoted}, {"mail", text, html, !quoted},
				} {
					other := twinCopy{quoted: change.quoted, bodyHTML: change.html}
					miss := new(twinBodyCache)
					miss.reduce(&other, change.body, change.source, twinBodyVersion, key[:], payload)
					if miss.hits != 0 || miss.reductions != 1 {
						t.Fatal("changed exact input hit")
					}
				}
			}
		}
	}
	if twinBodyDigest("a", "bc", "", false) == twinBodyDigest("ab", "c", "", false) {
		t.Fatal("ambiguous key framing")
	}
	// Raw invalid UTF-8 must survive, not JSON's replacement rune.
	c := twinCopy{links: quoteLinks{shape: []string{"a", "\x00", "a", "\x00"}, urls: []string{"https://X/\xff", "https://X/\xff"}, words: 2}}
	var out twinCopy
	if !decodeTwinBody(encodeTwinBody(&c), &out) || !reflect.DeepEqual(c.links, out.links) {
		t.Fatal("raw URL/order lost")
	}
}

func TestTwinBodyMalformedCodec(t *testing.T) {
	c := twinCopy{words: []string{"one", "two"}}
	good := encodeTwinBody(&c)
	badCount := bytes.Clone(good)
	binary.LittleEndian.PutUint32(badCount[sha256.Size+4:], twinBodyMaxStrings+1)
	sum := sha256.Sum256(badCount[sha256.Size:])
	copy(badCount, sum[:])
	trailing := append(bytes.Clone(good), 0)
	sum = sha256.Sum256(trailing[sha256.Size:])
	copy(trailing, sum[:])
	for _, b := range [][]byte{nil, good[:20], good[:len(good)-1], append([]byte{0}, good[1:]...), badCount, trailing, make([]byte, twinBodyMaxPayload+1)} {
		before := twinCopy{words: []string{"untouched"}}
		if decodeTwinBody(b, &before) || !reflect.DeepEqual(before.words, []string{"untouched"}) {
			t.Fatal("malformed accepted/partial assignment")
		}
	}
	huge := twinCopy{words: []string{strings.Repeat("x", twinBodyMaxPayload)}}
	if encodeTwinBody(&huge) != nil {
		t.Fatal("oversize encoded")
	}
}

func assertCachedOracle(t *testing.T, s *Store) *twinBodyCache {
	t.Helper()
	want, err := CollapseTwins(s, false)
	if err != nil {
		t.Fatal(err)
	}
	got, c := cachedTwinPlan(t, s, false)
	if !reflect.DeepEqual(want, got) || c.inspected != got.BodiesScanned || c.inspected != c.hits+c.reductions {
		t.Fatalf("plan/counters differ want=%+v got=%+v counts=%+v", want, got, c)
	}
	return c
}

func TestTwinBodySQLInputAndMetadataMatrix(t *testing.T) {
	cases := []struct {
		name, sql string
		misses    int
	}{
		{"text same hash", `update entries set body_text=body_text||' ' where id=?`, 1},
		{"HTML", `update entries set body_html='<p>changed HTML only</p>' where id=?`, 1},
		{"quoted", `update entries set quoted=1,quote_time_basis='stated' where id=?`, 1},
		{"source ineligible", `update entries set source='slack' where id=?`, 0},
		{"subject", `update entries set subject='different witness' where id=?`, 0},
		{"container", `update entries set container='different thread' where id=?`, 0},
		{"ext id", `update entries set ext_id='mail:new-exact-id' where id=?`, 0},
		{"time", `update entries set ts=ts+60 where id=?`, 0},
		{"graph", `update entries set parent_id=(select max(id) from entries) where id=?`, 0},
		{"attachments", `insert into attachments(entry_id,name) values(?,'another attachment')`, 0},
		{"hash irrelevant", `update entries set body_sha='arbitrary hash' where id=?`, 0},
		{"no op", `update entries set body_text=body_text,quoted=quoted where id=?`, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := open(t)
			keep, _ := twinFixture(t, s)
			cachedTwinPlan(t, s, true)
			var sha string
			if err := s.db.QueryRow(`select body_sha from entries where id=?`, keep).Scan(&sha); err != nil {
				t.Fatal(err)
			}
			if _, err := s.db.Exec(tc.sql, keep); err != nil {
				t.Fatal(err)
			}
			c := assertCachedOracle(t, s)
			if c.reductions != tc.misses {
				t.Fatalf("%+v", c)
			}
			if tc.name == "text same hash" {
				var now string
				scanTwinCache(t, s.db.QueryRow(`select body_sha from entries where id=?`, keep), &now)
				if now != sha {
					t.Fatal("hash changed")
				}
			}
		})
	}
}

func TestTwinBodyFreshOffsetsMembershipAndVeto(t *testing.T) {
	s := open(t)
	keep, drop := twinFixture(t, s)
	original, _ := cachedTwinPlan(t, s, true)
	if original.Measured != 1 {
		t.Fatal("fixture lacks offset")
	}
	hostID := original.Collapse[0].Offsets[0].Host
	if _, err := s.db.Exec(`update entries set ts=ts+600 where id=?`, hostID); err != nil {
		t.Fatal(err)
	}
	c := assertCachedOracle(t, s)
	if c.reductions != 0 {
		t.Fatal("host metadata discarded reductions")
	}
	changed, _ := cachedTwinPlan(t, s, false)
	if changed.Collapse[0].Offsets[0].At.Equal(original.Collapse[0].Offsets[0].At) {
		t.Fatal("stale host clock")
	}
	if _, err := s.db.Exec(`insert into attachments(entry_id,name) values(?,'veto')`, drop); err != nil {
		t.Fatal(err)
	}
	c = assertCachedOracle(t, s)
	if c.reductions != 0 {
		t.Fatal("attachment evicted reductions")
	}
	veto, _ := cachedTwinPlan(t, s, false)
	if len(veto.Collapse) != 0 {
		t.Fatal("stale attachment veto")
	}
	if _, err := s.db.Exec(`delete from attachments where entry_id=?; update entries set person_id=(select person_id from entries where id=?) where id=?`, drop, hostID, drop); err != nil {
		t.Fatal(err)
	}
	c = assertCachedOracle(t, s)
	if c.reductions != 1 || c.hits != 2 {
		t.Fatalf("membership not fresh/all-author traversal: %+v keep=%d", c, keep)
	}
}

func TestTwinBodyStaleRowsReuseNullAndRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reuse.db")
	s := fileFoldStore(t, path)
	p := person(t, s, "reuse@example.test", "Invented reuse")
	id, _, err := s.PutQuoted(Entry{Source: SourceMail, ExtID: "reuse", PersonID: p, TS: time.Unix(1, 0)})
	if err != nil {
		t.Fatal(err)
	}
	cachedTwinPlan(t, s, true)
	if _, err := s.db.Exec(`update entries set body_text=null,body_html=null where id=?`, id); err != nil {
		t.Fatal(err)
	}
	if c := assertCachedOracle(t, s); c.hits != 1 {
		t.Fatal("coalesced null missed")
	}
	if _, err := s.db.Exec(`delete from entries where id=?`, id); err != nil {
		t.Fatal(err)
	}
	// No canonical invalidation triggers: the orphan remains harmless.
	var n int
	scanTwinCache(t, s.db.QueryRow(`select count(*) from twins_body_cache where entry_id=?`, id), &n)
	if n != 1 {
		t.Fatal("unexpected invalidation")
	}
	replacement, _, err := s.PutQuoted(Entry{Source: SourceMail, ExtID: "new-reuse", PersonID: p, TS: time.Unix(1, 0), BodyText: "replacement raw evidence"})
	if err != nil || replacement != id {
		t.Fatalf("reuse %d/%d %v", replacement, id, err)
	}
	if c := assertCachedOracle(t, s); c.reductions != 1 {
		t.Fatal("reused ID hit stale payload")
	}
	cachedTwinPlan(t, s, true)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened := fileFoldStore(t, path)
	if c := assertCachedOracle(t, reopened); c.hits != 1 || c.reductions != 0 {
		t.Fatal("restart lost hits")
	}
}

func TestTwinBodyDryRunAndSourceReadErrors(t *testing.T) {
	s := open(t)
	twinFixture(t, s)
	if _, err := CollapseTwins(s, false); err != nil {
		t.Fatal(err)
	}
	var n int
	scanTwinCache(t, s.db.QueryRow(`select count(*) from twins_body_cache`), &n)
	if n != 0 {
		t.Fatal("dry run backfilled")
	}
	cachedTwinPlan(t, s, true)
	// A missing cache table demonstrates there are no public dry-run reads.
	if _, err := s.db.Exec(`drop table twins_body_cache`); err != nil {
		t.Fatal(err)
	}
	if _, err := CollapseTwins(s, false); err != nil {
		t.Fatal(err)
	}
	if _, err := CollapseTwins(s, true); err == nil {
		t.Fatal("genuine SQL read failure hidden")
	}
	if _, err := s.db.Exec(`alter table entries rename column body_text to missing_body`); err != nil {
		t.Fatal(err)
	}
	if _, err := CollapseTwins(s, false); err == nil {
		t.Fatal("source read failure hidden")
	}
}

func TestTwinBodyMalformedAndNewerRows(t *testing.T) {
	for _, mode := range []string{"checksum", "digest", "version", "newer", "oversize", "text-version", "real-version", "text-payload"} {
		t.Run(mode, func(t *testing.T) {
			s := open(t)
			keep, _ := twinFixture(t, s)
			cachedTwinPlan(t, s, true)
			var payload []byte
			scanTwinCache(t, s.db.QueryRow(`select payload from twins_body_cache where entry_id=?`, keep), &payload)
			switch mode {
			case "checksum":
				payload[len(payload)-1] ^= 1
				execTwinCache(t, s, `update twins_body_cache set payload=? where entry_id=?`, payload, keep)
			case "digest":
				execTwinCache(t, s, `update twins_body_cache set input_digest=x'00' where entry_id=?`, keep)
			case "version":
				execTwinCache(t, s, `update twins_body_cache set format_version=0 where entry_id=?`, keep)
			case "newer":
				execTwinCache(t, s, `update twins_body_cache set format_version=? where entry_id=?`, twinBodyVersion+1, keep)
			case "oversize":
				execTwinCache(t, s, `update twins_body_cache set payload=zeroblob(?) where entry_id=?`, twinBodyMaxPayload+1, keep)
			case "text-payload":
				execTwinCache(t, s, `update twins_body_cache set payload=cast(payload as text) where entry_id=?`, keep)
			case "text-version":
				execTwinCache(t, s, `update twins_body_cache set format_version='malformed' where entry_id=?`, keep)
			case "real-version":
				execTwinCache(t, s, `update twins_body_cache set format_version=1.5 where entry_id=?`, keep)
			}
			c := assertCachedOracle(t, s)
			if c.reductions != 1 || c.hits != 1 {
				t.Fatalf("%+v", c)
			}
			cachedTwinPlan(t, s, true)
			if mode == "newer" {
				var version int
				scanTwinCache(t, s.db.QueryRow(`select format_version from twins_body_cache where entry_id=?`, keep), &version)
				if version != twinBodyVersion+1 {
					t.Fatal("downgraded newer row")
				}
			} else if healed := assertCachedOracle(t, s); healed.hits != 2 || healed.reductions != 0 {
				t.Fatalf("cache did not heal: hits=%d reductions=%d", healed.hits, healed.reductions)
			}
		})
	}
}

func TestTwinBodyPublicationConflictsAndCancellation(t *testing.T) {
	for _, phase := range []string{"before-publication", "after-publication", "cancel-publication", "cancel-inside-publication", "rollback"} {
		t.Run(phase, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "conflict.db")
			s := fileFoldStore(t, path)
			other := fileFoldStore(t, path)
			keep, _ := twinFixture(t, s)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0
			failure := errors.New("publication rolled back")
			publish := func(ctx context.Context, expected *int64, apply func(corpusWriter) error) (time.Duration, error) {
				calls++
				if phase == "before-publication" {
					if _, err := other.db.Exec(`update entries set ts=ts+1 where id=?`, keep); err != nil {
						t.Fatal(err)
					}
				}
				if phase == "cancel-publication" {
					cancel()
				}
				if phase == "rollback" || phase == "cancel-inside-publication" {
					return s.guardedReconcileWrite(ctx, expected, func(w corpusWriter) error {
						if err := apply(w); err != nil {
							return err
						}
						if phase == "cancel-inside-publication" {
							cancel()
							return nil
						}
						return failure
					})
				}
				wait, err := s.guardedReconcileWrite(ctx, expected, apply)
				if phase == "after-publication" && calls == 1 && err == nil {
					if _, e := other.db.Exec(`update entries set ts=ts+1 where id=?`, keep); e != nil {
						t.Fatal(e)
					}
				}
				return wait, err
			}
			result, err := collapseTwinsContext(ctx, s, true, s.guardedReconcileWrite, publish)
			var n int
			scanTwinCache(t, s.db.QueryRow(`select count(*) from twins_body_cache`), &n)
			switch phase {
			case "before-publication":
				if !errors.Is(err, ErrReconcileConflict) || result.Retries != 3 || result.Removed != 0 || n != 0 {
					t.Fatalf("%+v %v cache=%d", result, err, n)
				}
			case "after-publication":
				if err != nil || result.Retries != 1 || result.Removed != 1 {
					t.Fatalf("%+v %v", result, err)
				}
			case "cancel-publication", "cancel-inside-publication":
				if !errors.Is(err, context.Canceled) || n != 0 || result.Removed != 0 {
					t.Fatalf("%+v %v cache=%d", result, err, n)
				}
			case "rollback":
				if !errors.Is(err, failure) || n != 0 || result.Removed != 0 {
					t.Fatalf("%+v %v cache=%d", result, err, n)
				}
			}
		})
	}
}

func TestTwinBodyPublicationBatchesAndNewerRace(t *testing.T) {
	s := open(t)
	state := reconcileState(t, s)
	rows := make([]twinBodyRow, 130)
	for i := range rows {
		rows[i] = twinBodyRow{id: int64(i + 1), payload: []byte(fmt.Sprint(i))}
	}
	calls := 0
	_, err := publishTwinBodies(context.Background(), rows, state.Source, func(ctx context.Context, e *int64, f func(corpusWriter) error) (time.Duration, error) {
		calls++
		if *e != state.Source {
			t.Fatal("publication relabelled original generation")
		}
		return s.guardedReconcileWrite(ctx, e, f)
	})
	if err != nil || calls != 3 || reconcileState(t, s) != state {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
	if _, err := s.db.Exec(`update twins_body_cache set format_version=2 where entry_id=1`); err != nil {
		t.Fatal(err)
	}
	if _, err := publishTwinBodies(context.Background(), rows[:1], state.Source, s.guardedReconcileWrite); err != nil {
		t.Fatal(err)
	}
	var version int
	scanTwinCache(t, s.db.QueryRow(`select format_version from twins_body_cache where entry_id=1`), &version)
	if version != 2 {
		t.Fatal("publication downgraded concurrent newer writer")
	}
}

func TestTwinBodyRealProducers(t *testing.T) {
	t.Run("Put promotion and same hash whitespace", func(t *testing.T) {
		s := open(t)
		p := person(t, s, "producer@example.test", "Invented producer")
		e := Entry{Source: SourceMail, ExtID: "producer", PersonID: p, TS: time.Unix(1, 0), BodyText: askBody}
		id, _, err := s.PutQuoted(e)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := s.PutQuoted(Entry{Source: SourceMail, ExtID: "other-producer", PersonID: p, TS: time.Unix(172800, 0), BodyText: otherBody}); err != nil {
			t.Fatal(err)
		}
		cachedTwinPlan(t, s, true)
		res, err := s.Put(e, &Mail{MessageID: "producer"}, nil)
		if err != nil || res.ID != id || res.Changed {
			t.Fatalf("%+v %v", res, err)
		}
		if c := assertCachedOracle(t, s); c.reductions != 1 {
			t.Fatal("promotion reused quoted reduction")
		}
		cachedTwinPlan(t, s, true)
		e.BodyText += " \n"
		e.BodyHTML = "<p>HTML changed without BodySHA</p>"
		res, err = s.Put(e, &Mail{MessageID: "producer"}, nil)
		if err != nil || res.Changed {
			t.Fatalf("%+v %v", res, err)
		}
		if c := assertCachedOracle(t, s); c.reductions != 1 {
			t.Fatal("Put.Changed was used as cache key")
		}
	})
	t.Run("quote repair", func(t *testing.T) {
		s, _, _, _, _ := foldStored(t)
		cachedTwinPlan(t, s, true)
		rep, err := RepairQuotedBodies(s)
		if err != nil || rep.Fixed != 1 {
			t.Fatalf("%+v %v", rep, err)
		}
		if c := assertCachedOracle(t, s); c.reductions != 1 {
			t.Fatal("repair reused old body")
		}
		checkRepairIndexes(t, s)
	})
	t.Run("own collapse enrich", func(t *testing.T) {
		s := open(t)
		p := person(t, s, "own-enrich@example.test", "Invented own enrich")
		first, _, err := s.PutQuoted(Entry{Source: SourceMail, ExtID: "own-first", PersonID: p, TS: time.Unix(1700000000, 0), BodyText: askBody})
		if err != nil {
			t.Fatal(err)
		}
		_, _, err = s.PutQuoted(Entry{Source: SourceMail, ExtID: "own-second", PersonID: p, TS: time.Unix(1700000000+3600, 0), BodyText: strings.ReplaceAll(askBody, " ", "   ")})
		if err != nil {
			t.Fatal(err)
		}
		before, _ := cachedTwinPlan(t, s, false)
		if len(before.Collapse) != 1 || before.Collapse[0].Keep != first {
			t.Fatalf("fixture %+v", before)
		}
		result, err := CollapseTwins(s, true)
		if err != nil || result.Removed != 1 {
			t.Fatalf("%+v %v", result, err)
		}
		if c := assertCachedOracle(t, s); c.reductions != 1 || c.hits != 0 {
			t.Fatalf("own-advanced revision blessed old payload %+v", c)
		}
		checkRepairIndexes(t, s)
	})
}

func TestTwinBodyPartialPublicationOriginalGuard(t *testing.T) {
	path := filepath.Join(t.TempDir(), "partial.db")
	s := fileFoldStore(t, path)
	other := fileFoldStore(t, path)
	keep, _ := twinFixture(t, s)
	state := reconcileState(t, s)
	rows := make([]twinBodyRow, 130)
	for i := range rows {
		rows[i] = twinBodyRow{id: int64(i + 1), payload: []byte("synthetic")}
	}
	calls := 0
	_, err := publishTwinBodies(context.Background(), rows, state.Source, func(ctx context.Context, e *int64, f func(corpusWriter) error) (time.Duration, error) {
		calls++
		wait, err := s.guardedReconcileWrite(ctx, e, f)
		if calls == 1 && err == nil {
			if _, e := other.db.Exec(`update entries set body_text=body_text||' changed raw evidence' where id=?`, keep); e != nil {
				t.Fatal(e)
			}
		}
		return wait, err
	})
	var n int
	scanTwinCache(t, s.db.QueryRow(`select count(*) from twins_body_cache`), &n)
	if !errors.Is(err, ErrReconcileConflict) || calls != 2 || n != 64 {
		t.Fatalf("partial guard calls=%d rows=%d %v", calls, n, err)
	}
	// The committed prefix is merely derived data; current keys and payload
	// validation turn both the external edit and malformed synthetic rows into misses.
	if c := assertCachedOracle(t, s); c.reductions != 2 {
		t.Fatalf("stale prefix hit %+v", c)
	}
}

func TestTwinBodyWarmDoesNotWriteOrChangeReadiness(t *testing.T) {
	s := open(t)
	twinFixture(t, s)
	cachedTwinPlan(t, s, true)
	checkpoint(t, s)
	before := reconcileState(t, s)
	if _, err := s.db.Exec(`create trigger reject_cache_insert before insert on twins_body_cache begin select raise(abort,'warm wrote'); end;
 create trigger reject_cache_update before update on twins_body_cache begin select raise(abort,'warm wrote'); end;`); err != nil {
		t.Fatal(err)
	}
	cachedTwinPlan(t, s, true)
	if reconcileState(t, s) != before || !before.Clean() {
		t.Fatal("warm altered readiness")
	}
	if _, err := s.db.Exec(`delete from twins_body_cache`); err != nil {
		t.Fatal(err)
	}
	if reconcileState(t, s) != before {
		t.Fatal("cache absence means pending source work")
	}
	if _, err := CollapseTwins(s, false); err != nil {
		t.Fatal(err)
	}
}

func TestTwinBodyCrossAuthorGlobalGraphParity(t *testing.T) {
	seed := func(s *Store) []int64 {
		p := person(t, s, "graph-a@example.test", "Invented graph A")
		q := person(t, s, "graph-b@example.test", "Invented graph B")
		h := person(t, s, "graph-host@example.test", "Invented graph host")
		at := time.Unix(1700000000, 0)
		hostID := host(t, s, h, "graph-host", at.Add(24*time.Hour))
		a := mailbox(t, s, p, "graph-a", askBody, at)
		ad := recovered(t, s, p, "graph-a-copy", askQuoted, at.Add(10*time.Hour), hostID)
		b := mailbox(t, s, q, "graph-b", askBody, at)
		bd := recovered(t, s, q, "graph-b-copy", askQuoted, at.Add(10*time.Hour), hostID)
		return []int64{a, ad, b, bd}
	}
	full, candidate := open(t), open(t)
	ids := seed(full)
	if !reflect.DeepEqual(ids, seed(candidate)) {
		t.Fatal("different clones")
	}
	cachedTwinPlan(t, candidate, true)
	for _, s := range []*Store{full, candidate} {
		if err := s.SetParent(ids[0], ids[3]); err != nil {
			t.Fatal(err)
		}
		if err := s.SetParent(ids[2], ids[1]); err != nil {
			t.Fatal(err)
		}
	}
	if c := assertCachedOracle(t, candidate); c.reductions != 0 || c.hits != 4 {
		t.Fatal("graph-only edit invalidated reductions")
	}
	if err := uncachedOracleRound(full); err != nil {
		t.Fatal(err)
	}
	if err := fullReconcileRound(candidate); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(oracleSemantic(allTwinTables(t, full)), oracleSemantic(allTwinTables(t, candidate))) {
		t.Fatal("cross-author global application differs")
	}
	var aParent, bParent int64
	scanTwinCache(t, candidate.db.QueryRow(`select coalesce(parent_id,0) from entries where id=?`, ids[0]), &aParent)
	scanTwinCache(t, candidate.db.QueryRow(`select coalesce(parent_id,0) from entries where id=?`, ids[2]), &bParent)
	if aParent != 0 || bParent != ids[0] {
		t.Fatalf("globally ordered cycle guards: a=%d b=%d", aParent, bParent)
	}
}

func TestTwinBodyRawURLDatabaseRoundTrip(t *testing.T) {
	s := open(t)
	p := person(t, s, "raw@example.test", "Invented raw byte author")
	body := "repeat Repeat https://Example.test/Case?x=\xffP. repeat https://Example.test/Case?x=\xffP. end"
	id, _, err := s.PutQuoted(Entry{Source: SourceMail, ExtID: "raw", PersonID: p, TS: time.Unix(1, 0), BodyText: body})
	if err != nil {
		t.Fatal(err)
	}
	cachedTwinPlan(t, s, true)
	if c := assertCachedOracle(t, s); c.hits != 1 {
		t.Fatal("raw URL missed")
	}
	copies, err := twinCopiesCached(s.db, p, new(twinBodyCache))
	if err != nil || len(copies) != 1 {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(copies[0].links.urls, []string{"https://Example.test/Case?x=\xffP.", "https://Example.test/Case?x=\xffP."}) {
		t.Fatalf("raw bytes lost %q", copies[0].links.urls)
	}
	tx, err := s.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`update entries set body_text='rollback' where id=?`, id); err != nil {
		t.Fatal(err)
	}
	tx.Rollback()
	if c := assertCachedOracle(t, s); c.hits != 1 {
		t.Fatal("rollback lost exact key")
	}
	if _, err := s.db.Exec(`delete from entries where id=?`, id); err != nil {
		t.Fatal(err)
	}
	reused, _, err := s.PutQuoted(Entry{Source: SourceMail, ExtID: "raw-reused", PersonID: p, TS: time.Unix(2, 0), BodyText: body})
	if err != nil || reused != id {
		t.Fatal("fixture failed to reuse ID", err)
	}
	if c := assertCachedOracle(t, s); c.hits != 1 {
		t.Fatal("identical exact inputs on reused ID should hit")
	}
}

func TestTwinBodyFreshOffsetEvidenceMatrix(t *testing.T) {
	for _, mode := range []string{"no-host", "two-hosts", "host-person-null", "host-person-change"} {
		t.Run(mode, func(t *testing.T) {
			s := open(t)
			keep, drop := twinFixture(t, s)
			before, _ := cachedTwinPlan(t, s, true)
			hostID := before.Collapse[0].Offsets[0].Host
			switch mode {
			case "no-host":
				execTwinCache(t, s, `delete from sightings where entry_id=?`, drop)
			case "two-hosts":
				if err := s.Sight(drop, keep, "quoted", "synthetic"); err != nil {
					t.Fatal(err)
				}
			case "host-person-null":
				execTwinCache(t, s, `update entries set person_id=null where id=?`, hostID)
			case "host-person-change":
				execTwinCache(t, s, `update entries set person_id=(select person_id from entries where id=?) where id=?`, keep, hostID)
			}
			misses := 0
			if mode == "host-person-change" {
				// Moving the host into the quoted author's history newly inspects it.
				misses = 1
			}
			if c := assertCachedOracle(t, s); c.reductions != misses || c.hits != 2 {
				t.Fatalf("offset evidence changed reductions %+v", c)
			}
			after, _ := cachedTwinPlan(t, s, false)
			if reflect.DeepEqual(before.Collapse[0].Offsets, after.Collapse[0].Offsets) {
				t.Fatal("stale offsets")
			}
		})
	}
}

func scanTwinCache(t testing.TB, row *sql.Row, dest ...any) {
	t.Helper()
	if err := row.Scan(dest...); err != nil {
		t.Fatal(err)
	}
}

func execTwinCache(t testing.TB, s *Store, query string, args ...any) {
	t.Helper()
	if _, err := s.db.Exec(query, args...); err != nil {
		t.Fatal(err)
	}
}

func TestTwinBodyIDUpdateAndEligibilityRoundTrip(t *testing.T) {
	s := open(t)
	p := person(t, s, "id-update@example.test", "Invented ID update")
	for i := 0; i < 2; i++ {
		if _, _, err := s.PutQuoted(Entry{Source: SourceMail, ExtID: fmt.Sprint("id-update:", i), PersonID: p, TS: time.Unix(int64(i)*172800, 0), BodyText: fmt.Sprint("different raw body ", i)}); err != nil {
			t.Fatal(err)
		}
	}
	cachedTwinPlan(t, s, true)
	execTwinCache(t, s, `update entries set id=1000 where id=1`)
	if c := assertCachedOracle(t, s); c.hits != 1 || c.reductions != 1 {
		t.Fatal("updated ID used another row's features")
	}
	execTwinCache(t, s, `update entries set id=1 where id=1000`)
	if c := assertCachedOracle(t, s); c.hits != 2 {
		t.Fatal("identical key on returned ID should hit")
	}
	execTwinCache(t, s, `update entries set source='slack' where id=1`)
	if c := assertCachedOracle(t, s); c.hits != 1 || c.inspected != 1 {
		t.Fatal("ineligible source inspected")
	}
	execTwinCache(t, s, `update entries set source='mail' where id=1`)
	if c := assertCachedOracle(t, s); c.hits != 2 {
		t.Fatal("returned exact eligible inputs should hit")
	}
}
