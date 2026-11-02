package corpus

import (
	"database/sql"
	"strings"
	"testing"
)

type twinOpcode struct {
	addr       int
	op         string
	p1, p2, p3 int
	p4         sql.NullString
	p5         int
	comment    sql.NullString
}

func TestTwinsFullFanoutVM(t *testing.T) {
	s := open(t)
	var stateRoot int
	if err := s.db.QueryRow(`select rootpage from sqlite_schema where name='twins_work_state'`).Scan(&stateRoot); err != nil {
		t.Fatal(err)
	}
	checked := map[string]bool{}
	for _, query := range []string{
		`insert into entries(source,ext_id,ts,body_text,body_sha,ingested_at) values('mail','vm',0,'synthetic','synthetic',0)`,
		`update entries set ts=ts+1 where id=1`, `delete from entries where id=1`,
		`insert into sightings(entry_id,seen_in,kind) values(1,2,'quote')`, `update sightings set seen_in=3 where entry_id=1`, `delete from sightings where entry_id=1`,
		`insert into attachments(entry_id,name) values(1,'vm')`, `update attachments set entry_id=2 where entry_id=1`, `delete from attachments where entry_id=1`,
		`update people set id=id+100 where id=1`, `delete from people where id=1`,
	} {
		rows, err := s.db.Query("explain " + query)
		if err != nil {
			t.Fatal(err)
		}
		var program []twinOpcode
		name := ""
		check := func() {
			if !strings.HasPrefix(name, "-- TRIGGER twins_") {
				return
			}
			trace := -1
			singletonWrites := 0
			for i, op := range program {
				if op.op == "Trace" && (strings.Contains(op.p4.String, "into twins_work_state") || strings.Contains(op.p4.String, "update twins_work_state")) {
					singletonWrites++
				}
				if op.op == "Trace" && strings.Contains(op.p4.String, "into twins_dirty_people") {
					trace = i
				}
			}
			if trace < 0 || singletonWrites != 1 {
				t.Fatalf("%s: missing queue or redundant singleton statements", name)
			}
			if program[trace+1].op != "InitCoroutine" {
				t.Fatal("queue producer is not a coroutine", name)
			}
			// Follow the actual modernc SQLite producer's pending-full path. It must
			// terminate before materializing the UNION, opening any dependency cursor,
			// yielding any author, or reaching any queue UPSERT instructions.
			pc := program[trace+1].p3
			cursor, fullRegister, gates := -1, -1, 0
			for steps := 0; steps < len(program); steps++ {
				op := program[pc]
				switch op.op {
				case "Goto":
					pc = op.p2
					continue
				case "OpenRead":
					if op.p2 != stateRoot {
						t.Fatalf("%s: dependency opened before full gate at %d: %+v", name, pc, op)
					}
					cursor = op.p1
				case "Column":
					if op.p1 != cursor || op.p2 != 2 {
						t.Fatalf("%s: unexpected pre-gate column: %+v", name, op)
					}
					fullRegister = op.p3
				case "NotNull":
					if op.p1 != fullRegister {
						t.Fatal("not the full epoch gate", name, op)
					}
					gates++
					pc = op.p2
					continue
				case "EndCoroutine":
					if gates != 1 {
						t.Fatal("missing full gate", name)
					}
					checked[name] = true
					t.Logf("%s: one singleton UPSERT; pending-full gate bypasses author UNION/dependency cursors and yields no queue row", name)
					return
				case "Integer", "SeekRowid", "Noop", "Once":
				default:
					t.Fatalf("%s: pending path executes unexpected work at %d: %+v", name, pc, op)
				}
				pc++
			}
			t.Fatal("producer did not terminate", name)
		}
		for rows.Next() {
			var op twinOpcode
			if err := rows.Scan(&op.addr, &op.op, &op.p1, &op.p2, &op.p3, &op.p4, &op.p5, &op.comment); err != nil {
				t.Fatal(err)
			}
			if op.op == "Init" {
				check()
				program = nil
				name = op.p4.String
			}
			program = append(program, op)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		rows.Close()
		check()
	}
	if len(checked) != 11 {
		t.Fatal("missing production trigger programs", checked)
	}
}
