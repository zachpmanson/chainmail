package corpus

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func BenchmarkTwinBodyCached(b *testing.B) {
	for _, mode := range []string{"cold", "warm", "single-change", "HTML-edit", "graph-only", "metadata-only", "restart", "malformed"} {
		b.Run(mode, func(b *testing.B) {
			path := filepath.Join(b.TempDir(), "bench.db")
			s, err := Open(path)
			if err != nil {
				b.Fatal(err)
			}
			defer func() { s.Close() }()
			first := seedBodyBenchmark(b, s)
			if mode != "cold" {
				cachedTwinPlan(b, s, true)
			}
			var held, maxHeld, waitTotal time.Duration
			txns := 0
			publish := func(ctx context.Context, e *int64, f func(corpusWriter) error) (time.Duration, error) {
				started := time.Now()
				wait, err := s.guardedReconcileWrite(ctx, e, f)
				duration := time.Since(started) - wait
				held += duration
				maxHeld = max(maxHeld, duration)
				waitTotal += wait
				txns++
				return wait, err
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if mode == "cold" || mode == "malformed" {
					b.StopTimer()
					query := `delete from twins_body_cache`
					if mode == "malformed" {
						query = `update twins_body_cache set payload=x'00'`
					}
					if _, err := s.db.Exec(query); err != nil {
						b.Fatal(err)
					}
					b.StartTimer()
				}
				if mode == "restart" {
					if err := s.Close(); err != nil {
						b.Fatal(err)
					}
					s, err = Open(path)
					if err != nil {
						b.Fatal(err)
					}
				}
				mutateBodyBenchmark(b, s, mode, first, i)
				plan, err := collapseTwinsContext(context.Background(), s, true, s.guardedReconcileWrite, publish)
				if err != nil || len(plan.Collapse) != 0 || plan.Retries != 0 {
					b.Fatalf("%+v %v", plan, err)
				}
			}
			b.StopTimer()
			var storage int64
			if err := s.db.QueryRow(`select coalesce(sum(length(payload)+length(input_digest)),0) from twins_body_cache`).Scan(&storage); err != nil {
				b.Fatal(err)
			}
			b.ReportMetric(float64(storage), "cache-bytes")
			b.ReportMetric(float64(held.Nanoseconds())/float64(b.N), "cache-held-ns/op")
			b.ReportMetric(float64(maxHeld.Nanoseconds()), "cache-max-held-ns")
			b.ReportMetric(float64(waitTotal.Nanoseconds())/float64(b.N), "cache-acquire-ns/op")
			b.ReportMetric(float64(txns)/float64(b.N), "cache-txn/op")
			_, warm := cachedTwinPlan(b, s, false)
			if warm.hits != 200 || warm.reductions != 0 {
				b.Fatalf("unexpected warm counts %+v", warm)
			}
		})
	}
}
