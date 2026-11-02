package corpus

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

// Portable on the pinned source too: only fixture setup detects work tracking.
// Ready cases start each timed transaction with an empty fine-grained queue;
// clearing that benchmark-only queue is outside the timer, never production.
func BenchmarkTwinsTrackingTransaction(b *testing.B) {
	for _, file := range []bool{false, true} {
		for _, pending := range []bool{false, true} {
			for _, fanout := range []int{1, 100} {
				b.Run(fmt.Sprintf("file=%t/full=%t/host-authors=%d", file, pending, fanout), func(b *testing.B) {
					path := ":memory:"
					if file {
						path = filepath.Join(b.TempDir(), "trigger.db")
					}
					s, err := Open(path)
					if err != nil {
						b.Fatal(err)
					}
					defer s.Close()
					host, err := s.Put(Entry{Source: SourceSlack, ExtID: "shared-host", TS: time.Unix(1700000000, 0), BodyText: "Invented shared host"}, nil, nil)
					if err != nil {
						b.Fatal(err)
					}
					for i := 0; i < fanout; i++ {
						p, err := Resolve(s, KindEmail, fmt.Sprintf("fanout-%d@example.test", i), "Invented fanout author")
						if err != nil {
							b.Fatal(err)
						}
						id, _, err := s.PutQuoted(Entry{Source: SourceMail, ExtID: fmt.Sprintf("fanout-%d", i), PersonID: p, TS: time.Unix(1700000000, 0), BodyText: "Independent synthetic quoted evidence"})
						if err != nil {
							b.Fatal(err)
						}
						if _, err := s.db.Exec(`insert into sightings(entry_id,seen_in,kind) values(?,?,'quote')`, id, host.ID); err != nil {
							b.Fatal(err)
						}
					}
					var tracking bool
					if err := s.db.QueryRow(`select exists(select 1 from sqlite_schema where name='twins_work_state')`).Scan(&tracking); err != nil {
						b.Fatal(err)
					}
					if tracking && !pending {
						if _, err := s.db.Exec(`delete from twins_dirty_people; update twins_work_state set full_revision=null,full_reason=null,ready=1,covered_version=rules_version`); err != nil {
							b.Fatal(err)
						}
					}
					var acquisition, execution, commit time.Duration
					b.ReportAllocs()
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						if tracking && !pending {
							b.StopTimer()
							if _, err := s.db.Exec(`delete from twins_dirty_people`); err != nil {
								b.Fatal(err)
							}
							b.StartTimer()
						}
						at := time.Now()
						w, err := s.beginWriter(context.Background(), writerBusyMS*time.Millisecond)
						acquired := time.Now()
						if err != nil {
							b.Fatal(err)
						}
						if _, err := w.ExecContext(context.Background(), `update entries set ts=ts+1 where id=?`, host.ID); err != nil {
							w.close()
							b.Fatal(err)
						}
						applied := time.Now()
						if err := w.commit(context.Background()); err != nil {
							w.close()
							b.Fatal(err)
						}
						committed := time.Now()
						w.close()
						acquisition += acquired.Sub(at)
						execution += applied.Sub(acquired)
						commit += committed.Sub(applied)
					}
					b.ReportMetric(float64(acquisition.Nanoseconds())/float64(b.N), "acquire-ns/op")
					b.ReportMetric(float64(execution.Nanoseconds())/float64(b.N), "execute-ns/op")
					b.ReportMetric(float64(commit.Nanoseconds())/float64(b.N), "commit-ns/op")
				})
			}
		}
	}
}
