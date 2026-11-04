package corpus

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Portable, invented workload, also overlaid on the exact pinned source. There
// are no collapse proposals: this isolates full-pass reduction/lookup/backfill
// cost, not the cost of applying a large canonical collapse plan.
func seedBodyBenchmark(b *testing.B, s *Store) int64 {
	b.Helper()
	var first int64
	body := strings.Repeat(askBody+"\nhttps://Example.test/Case?x=Raw&y=1.\n", 8)
	for p := 0; p < 20; p++ {
		author, err := Resolve(s, KindEmail, fmt.Sprintf("bench%d@example.test", p), "Invented benchmark author")
		if err != nil {
			b.Fatal(err)
		}
		for j := 0; j < 10; j++ {
			e := Entry{Source: SourceMail, ExtID: fmt.Sprintf("bench:%d:%d", p, j), PersonID: author, TS: time.Unix(1700000000+int64(j)*172800, 0), BodyText: body}
			var id int64
			if j%2 == 0 {
				id, _, err = s.PutQuoted(e)
			} else {
				e.BodyHTML = "<p>" + body + "</p>"
				var res PutResult
				res, err = s.Put(e, &Mail{MessageID: e.ExtID}, nil)
				id = res.ID
			}
			if err != nil {
				b.Fatal(err)
			}
			if p == 0 && j == 1 {
				first = id
			}
		}
	}
	return first
}

func mutateBodyBenchmark(b *testing.B, s *Store, mode string, first int64, i int) {
	b.Helper()
	var query string
	var args []any
	switch mode {
	case "single-change":
		query = `update entries set body_text=body_text||' ' where id=?`
		args = []any{first}
	case "HTML-edit":
		query = `update entries set body_html=? where id=?`
		args = []any{fmt.Sprintf("<p>Edited HTML iteration %d</p>", i), first}
	case "graph-only":
		query = `update entries set parent_id=? where id=?`
		args = []any{any(nil), first}
		if i%2 == 0 {
			args[0] = first + 1
		}
	case "metadata-only":
		query = `update entries set subject=? where id=?`
		args = []any{fmt.Sprintf("Witness %d", i), first}
	}
	if query != "" {
		if _, err := s.db.Exec(query, args...); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkTwinBodyUncached(b *testing.B) {
	for _, mode := range []string{"warm", "single-change", "HTML-edit", "graph-only", "metadata-only"} {
		b.Run(mode, func(b *testing.B) {
			s, err := Open(filepath.Join(b.TempDir(), "bench.db"))
			if err != nil {
				b.Fatal(err)
			}
			defer s.Close()
			first := seedBodyBenchmark(b, s)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				mutateBodyBenchmark(b, s, mode, first, i)
				plan, err := CollapseTwins(s, false)
				if err != nil || len(plan.Collapse) != 0 {
					b.Fatalf("%+v %v", plan, err)
				}
			}
		})
	}
}
