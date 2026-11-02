package corpus

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

// This portable harness is also overlaid unchanged onto the pinned source.
// Only the candidate adapter knows about the pending coordinator.
var twinsOracleRound = fullReconcileRound

type oracleTable struct {
	Class   string
	Columns []string
	Rows    []string
}

func allTwinTables(t *testing.T, s *Store) map[string]oracleTable {
	t.Helper()
	rows, err := s.db.Query(`select name from sqlite_master where type='table' and name not like 'sqlite_%' order by name`)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		names = append(names, name)
	}
	rows.Close()
	out := map[string]oracleTable{}
	for _, name := range names {
		class := "exact"
		switch {
		case name == "reconcile_state" || name == "schema_version" || strings.HasPrefix(name, "twins_"):
			class = "queue/schema: independently asserted, not semantic parity"
		case name == "entries_fts" || strings.HasPrefix(name, "entries_fts_") || name == "entries_ident" || strings.HasPrefix(name, "entries_ident_"):
			class = "derived FTS: rank=1 integrity and MATCH checks"
		case name == "entries" || name == "person_merges" || name == "render_offsets" || name == "gmail_accounts":
			class = "observation times normalized; other columns exact"
		}
		table := oracleTable{Class: class, Rows: []string{}}
		r, err := s.db.Query(`select * from "` + name + `"`)
		if err != nil {
			t.Fatal(err)
		}
		table.Columns, err = r.Columns()
		if err != nil {
			t.Fatal(err)
		}
		for r.Next() {
			values := make([]any, len(table.Columns))
			ptrs := make([]any, len(values))
			for i := range values {
				ptrs[i] = &values[i]
			}
			if err := r.Scan(ptrs...); err != nil {
				t.Fatal(err)
			}
			if strings.HasPrefix(class, "queue/") || strings.HasPrefix(class, "derived FTS") {
				continue
			}
			for i, col := range table.Columns {
				if col == "ingested_at" || col == "merged_at" || col == "measured_at" || (name == "gmail_accounts" && col == "created_at") {
					values[i] = "<observation-time>"
				}
				if b, ok := values[i].([]byte); ok {
					values[i] = fmt.Sprintf("blob:%x", b)
				}
			}
			data, err := json.Marshal(values)
			if err != nil {
				t.Fatal(err)
			}
			table.Rows = append(table.Rows, string(data))
		}
		if err := r.Err(); err != nil {
			t.Fatal(err)
		}
		r.Close()
		sort.Strings(table.Rows)
		out[name] = table
	}
	checkRepairIndexes(t, s)
	for _, index := range []string{"entries_fts", "entries_ident"} {
		want := `select id from entries where lower(coalesce(body_text,'')) like '%rothwell%'`
		if index == "entries_fts" {
			want += ` or lower(coalesce(subject,'')) like '%rothwell%'`
		}
		got := `select rowid from ` + index + ` where ` + index + ` match 'Rothwell'`
		for _, difference := range []string{got + ` except ` + want, want + ` except ` + got} {
			var n int
			if err := s.db.QueryRow(`select count(*) from (` + difference + `)`).Scan(&n); err != nil || n != 0 {
				t.Fatalf("%s MATCH mismatch %d %v", index, n, err)
			}
		}
	}
	fk, err := s.db.Query(`pragma foreign_key_check`)
	if err != nil {
		t.Fatal(err)
	}
	defer fk.Close()
	if fk.Next() {
		t.Fatal("foreign key violation")
	}
	if err := fk.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func oracleSemantic(tables map[string]oracleTable) map[string]oracleTable {
	out := map[string]oracleTable{}
	for name, table := range tables {
		if !strings.HasPrefix(table.Class, "queue/") {
			out[name] = table
		}
	}
	return out
}

func seedTwinOracle(t *testing.T, s *Store) []int64 {
	t.Helper()
	keep, drop := twinFixture(t, s)
	p := person(t, s, "annotated@example.test", "Synthetic annotated author")
	h := person(t, s, "independent@example.test", "Synthetic host")
	at := time.Unix(1703000000, 0)
	hostID := host(t, s, h, "annotated-host", at.Add(time.Hour))
	annotated := recovered(t, s, p, "annotated-copy", askAnnotated, at.Add(10*time.Hour), hostID)
	base := mailbox(t, s, p, "annotated-base", askBody, at)
	q := person(t, s, "veto@example.test", "Synthetic veto author")
	vetoBase := mailbox(t, s, q, "veto-base", askBody, at)
	veto := recovered(t, s, q, "veto-copy", askQuoted, at.Add(10*time.Hour), hostID)
	if _, err := s.db.Exec(`insert into attachments(entry_id,name) values(?,'extra veto')`, veto); err != nil {
		t.Fatal(err)
	}
	// A negative selected author and its unrelated old history stay available.
	n := person(t, s, "negative@example.test", "Synthetic negative author")
	negative := recovered(t, s, n, "negative-old", otherBody, at, hostID)
	mailbox(t, s, n, "negative-unrelated", askBody, at.Add(100*time.Hour))
	if _, err := s.db.Exec(`insert into entry_embeddings(entry_id,model,dim,body_sha,prep,skip,embedded_at) select id,'oracle',3,body_sha,1,'no topic',0 from entries`); err != nil {
		t.Fatal(err)
	}
	return []int64{keep, drop, hostID, annotated, base, veto, vetoBase, negative, n, p}
}

func mutateTwinOracle(t *testing.T, s *Store, ids []int64, step int) {
	t.Helper()
	var err error
	switch step {
	case 0: // initial unsettled full catch-up
	case 1:
		var body string
		if err = s.db.QueryRow(`select body_text from entries where id=?`, ids[7]).Scan(&body); err == nil {
			err = s.EnrichQuoted(ids[7], Entry{BodyText: body + " A distinct negative ending."})
		}
	case 2:
		_, err = s.db.Exec(`update entries set ts=ts+60 where id=?`, ids[2])
	case 3:
		_, err = s.db.Exec(`update sightings set kind='forwarded' where entry_id=?`, ids[7])
	case 4:
		_, err = s.db.Exec(`update entries set person_id=null where id=?`, ids[7])
	case 5:
		_, err = s.db.Exec(`update entries set person_id=? where id=?`, ids[8], ids[7])
	case 6:
		_, err = s.db.Exec(`delete from attachments where entry_id=?`, ids[5])
	case 7:
		_, err = s.db.Exec(`update entries set parent_id=? where id=?`, ids[7], ids[4])
	case 8:
		_, err = s.db.Exec(`insert into domain_aliases values('old-oracle.test','new-oracle.test','synthetic',0); update people set display_name='Renamed synthetic author' where id=?`, ids[8])
	case 9:
		_, err = s.db.Exec(`update entries set source='slack' where id=?`, ids[7])
	case 10:
		_, err = s.db.Exec(`update entries set source='mail',person_id=? where id=?`, ids[9], ids[7])
	case 11:
		_, err = s.Put(Entry{Source: SourceMail, ExtID: "oracle-independent-new", PersonID: ids[8], TS: time.Unix(1710000000, 0), BodyText: "A wholly independent new synthetic message."}, &Mail{MessageID: "oracle-independent-new"}, nil)
	}
	if err != nil {
		t.Fatal(err)
	}
}

func TestTwinsCompleteTableParity(t *testing.T) {
	full := open(t)
	candidate := open(t)
	a := seedTwinOracle(t, full)
	b := seedTwinOracle(t, candidate)
	if !reflect.DeepEqual(a, b) {
		t.Fatal("nonidentical seed")
	}
	for step := 0; step < 12; step++ {
		mutateTwinOracle(t, full, a, step)
		mutateTwinOracle(t, candidate, b, step)
		for round := 0; round < 2; round++ {
			if err := fullReconcileRound(full); err != nil {
				t.Fatal(err)
			}
			if err := twinsOracleRound(candidate); err != nil {
				t.Fatal(err)
			}
			want, got := allTwinTables(t, full), allTwinTables(t, candidate)
			if !reflect.DeepEqual(oracleSemantic(want), oracleSemantic(got)) {
				for name, w := range oracleSemantic(want) {
					if !reflect.DeepEqual(w, got[name]) {
						t.Errorf("step=%d round=%d table=%s\nwant=%+v\ngot=%+v", step, round, name, w, got[name])
					}
				}
				t.FailNow()
			}
		}
	}
	t.Logf("classified %d SQLite user tables (including FTS/shadows/empty tables); exact columns retained", len(allTwinTables(t, candidate)))
}

func TestTwinsPinnedOracle(t *testing.T) {
	path := os.Getenv("CHAINMAIL_SYNTHETIC_ORACLE_OUTPUT")
	if path == "" {
		t.Skip("portable pinned-source oracle requires output path")
	}
	s := open(t)
	ids := seedTwinOracle(t, s)
	snapshots := map[string]map[string]oracleTable{}
	snapshots["seed"] = allTwinTables(t, s)
	for step := 0; step < 12; step++ {
		mutateTwinOracle(t, s, ids, step)
		for round := 0; round < 2; round++ {
			if err := twinsOracleRound(s); err != nil {
				t.Fatal(err)
			}
			snapshots[fmt.Sprintf("step-%02d-round-%d", step, round)] = allTwinTables(t, s)
		}
	}
	data, err := json.MarshalIndent(snapshots, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}

func BenchmarkTwinsTriggerCost(b *testing.B) {
	for _, n := range []int{10, 100} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			s, err := Open(":memory:")
			if err != nil {
				b.Fatal(err)
			}
			defer s.Close()
			var selected int64
			for i := 0; i < n; i++ {
				p, err := Resolve(s, KindEmail, fmt.Sprintf("trigger-%d@example.test", i), "Synthetic trigger author")
				if err != nil {
					b.Fatal(err)
				}
				id, _, err := s.PutQuoted(Entry{Source: SourceMail, ExtID: fmt.Sprintf("trigger:%d", i), PersonID: p, TS: time.Unix(1700000000, 0), BodyText: "Synthetic trigger-only quoted body"})
				if err != nil {
					b.Fatal(err)
				}
				if i == 0 {
					selected = id
				}
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := s.db.Exec(`update entries set ts=ts+1 where id=?`, selected); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
