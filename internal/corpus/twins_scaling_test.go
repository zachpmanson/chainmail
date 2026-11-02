package corpus

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func seedScopedTwins(tb testing.TB, s *Store, authors, history int) (int64, int64) {
	tb.Helper()
	var selected, quoted int64
	at := time.Unix(1700000000, 0)
	for i := 0; i < authors; i++ {
		p, err := Resolve(s, KindEmail, fmt.Sprintf("scale-%d@example.test", i), fmt.Sprintf("Synthetic scale author %d", i))
		if err != nil {
			tb.Fatal(err)
		}
		count := 2
		if i == 0 {
			count = history
		}
		for j := 0; j < count; j++ {
			body := askBody
			if j%2 == 1 {
				body = strings.Repeat(fmt.Sprintf("Independent synthetic observatory discussion number %d with optical telescopes and astronomical instruments. ", j), 8)
			}
			when := at.Add(time.Duration(j) * 100 * time.Hour)
			if j == 1 {
				when = at.Add(time.Hour)
			}
			e := Entry{Source: SourceMail, ExtID: fmt.Sprintf("scale:%d:%d", i, j), PersonID: p, TS: when, BodyText: body, Subject: fmt.Sprintf("Independent subject %d", j)}
			if j%2 == 0 {
				if _, err := s.Put(e, &Mail{MessageID: e.ExtID}, nil); err != nil {
					tb.Fatal(err)
				}
			} else {
				id, _, err := s.PutQuoted(e)
				if err != nil {
					tb.Fatal(err)
				}
				if i == 0 && j == 1 {
					selected, quoted = p, id
				}
			}
		}
	}
	plan, err := ReconcileTwinsContext(context.Background(), s)
	if err != nil {
		tb.Fatal(err)
	}
	if plan.Removed != 0 || !mustWorkState(tb, s).Clean() {
		tb.Fatalf("negative fixture not settled %+v", plan)
	}
	return selected, quoted
}
func mustWorkState(tb testing.TB, s *Store) TwinsWorkState {
	tb.Helper()
	r, err := s.TwinsWorkState()
	if err != nil {
		tb.Fatal(err)
	}
	return r
}

func TestTwinsScalingAndFallbackCounts(t *testing.T) {
	for _, n := range []int{10, 100} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			s := fileFoldStore(t, filepath.Join(t.TempDir(), "scale.db"))
			p, id := seedScopedTwins(t, s, n, 2)
			full, _ := snapshotTwinPlan(t, s)
			if full.PeopleScanned != n || full.BodiesScanned != 2*n {
				t.Fatalf("full %+v", full)
			}
			counts := map[string]int{}
			run := func(label, mode, reason string, bodies int) {
				t.Helper()
				plan, err := ReconcileTwinsContext(context.Background(), s)
				if err != nil {
					t.Fatal(err)
				}
				counts[plan.Mode+":"+plan.Fallback]++
				t.Logf("%s mode=%s fallback=%s authors=%d bodies=%d comparisons=%d", label, plan.Mode, plan.Fallback, plan.PeopleScanned, plan.BodiesScanned, plan.Comparisons)
				if plan.Mode != mode || plan.Fallback != reason || (bodies >= 0 && plan.BodiesScanned != bodies) {
					t.Fatalf("%s %+v", label, plan)
				}
			}
			run("settled", "current", "", 0)
			if _, err := s.db.Exec(`update entries set ts=ts+1 where id=?`, id); err != nil {
				t.Fatal(err)
			}
			run("same-author time edit", "authors", "", 2)
			if err := s.EnrichQuoted(id, Entry{BodyText: strings.Repeat("Another independent astronomical observatory narrative without any shared opening. ", 20)}); err != nil {
				t.Fatal(err)
			}
			run("body-only edit", "authors", "", 2)
			if _, err := s.Put(Entry{Source: SourceMail, ExtID: "independent-new", PersonID: p, TS: time.Unix(1800000000, 0), BodyText: "Wholly independent synthetic new mail"}, &Mail{MessageID: "independent-new"}, nil); err != nil {
				t.Fatal(err)
			}
			run("new independent mail", "authors", "", 3)
			fresh, _, err := s.PutQuoted(Entry{Source: SourceMail, ExtID: "simple-quote", PersonID: p, TS: time.Unix(1900000000, 0), BodyText: "A distinct newly quoted message about a different subject."})
			if err != nil {
				t.Fatal(err)
			}
			run("simple quoted insert", "authors", "", 4)
			// Initial graph policy: every effective edge, including new quote chains.
			if err := s.SetParent(fresh, id); err != nil {
				t.Fatal(err)
			}
			run("nested quoted edge", "full", "graph-edge", 2*n+2)
			t.Logf("observed mode/fallback frequency over six workloads: %v", counts)
		})
	}
}

func BenchmarkTwinsScopedUnrelatedGrowth(b *testing.B) {
	for _, n := range []int{10, 100} {
		for _, mode := range []string{"current", "authors", "authors-plan", "forced-full", "graph-fallback", "structural-fallback", "cold"} {
			b.Run(fmt.Sprintf("authors=%d/%s", n, mode), func(b *testing.B) {
				var people, bodies, comparisons int
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					b.StopTimer()
					s, err := Open(filepath.Join(b.TempDir(), "bench.db"))
					if err != nil {
						b.Fatal(err)
					}
					p, id := seedScopedTwins(b, s, n, 2)
					switch mode {
					case "authors", "authors-plan":
						_, err = s.db.Exec(`update entries set ts=ts+1 where id=?`, id)
					case "structural-fallback":
						_, _, err = s.PutQuoted(Entry{Source: SourceMail, ExtID: "structural-extra", PersonID: p, TS: time.Unix(1700000000, 0).Add(10 * time.Hour), BodyText: askQuoted})
					case "graph-fallback":
						_, err = s.db.Exec(`update entries set parent_id=id where id=?`, id)
					case "cold":
						_, err = s.db.Exec(`update twins_work_state set ready=0,full_revision=work_revision,full_reason='initial'`)
					}
					if err != nil {
						b.Fatal(err)
					}
					b.StartTimer()
					var plan TwinPlan
					if mode == "forced-full" {
						plan, err = CollapseTwins(s, false)
					} else if mode == "authors-plan" {
						plan, err = runTwinsContext(context.Background(), s, false, true, s.guardedReconcileWrite)
					} else {
						plan, err = ReconcileTwinsContext(context.Background(), s)
					}
					b.StopTimer()
					if err != nil {
						b.Fatal(err)
					}
					people += plan.PeopleScanned
					bodies += plan.BodiesScanned
					comparisons += plan.Comparisons
					if (mode == "authors" || mode == "authors-plan") && (plan.PeopleScanned != 1 || plan.BodiesScanned != 2) {
						b.Fatalf("not local %+v", plan)
					}
					s.Close()
				}
				b.ReportMetric(float64(people)/float64(b.N), "people/op")
				b.ReportMetric(float64(bodies)/float64(b.N), "bodies/op")
				b.ReportMetric(float64(comparisons)/float64(b.N), "comparisons/op")
			})
		}
	}
}

func BenchmarkTwinsSelectedHistoryGrowth(b *testing.B) {
	for _, history := range []int{2, 20} {
		b.Run(fmt.Sprint(history), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				s, err := Open(":memory:")
				if err != nil {
					b.Fatal(err)
				}
				_, id := seedScopedTwins(b, s, 10, history)
				if _, err := s.db.Exec(`update entries set ts=ts+1 where id=?`, id); err != nil {
					b.Fatal(err)
				}
				b.StartTimer()
				plan, err := ReconcileTwinsContext(context.Background(), s)
				b.StopTimer()
				if err != nil || plan.PeopleScanned != 1 || plan.BodiesScanned != history {
					b.Fatalf("%+v %v", plan, err)
				}
				b.ReportMetric(float64(plan.BodiesScanned), "bodies/op")
				b.ReportMetric(float64(plan.Comparisons), "comparisons/op")
				s.Close()
			}
		})
	}
}
