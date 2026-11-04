package corpus

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

// Identical fixture and full pipeline also run against the pinned read-only
// baseline via go test -overlay. Setup is excluded; cold means unsettled source,
// not flushed OS caches. Warm full is forced maintenance, not the clean gate.
func BenchmarkReconcileFull(b *testing.B) {
	for _, warm := range []bool{false, true} {
		b.Run(fmt.Sprint("warm=", warm), func(b *testing.B) {
			var removed int
			b.ReportAllocs()
			for n := 0; n < b.N; n++ {
				b.StopTimer()
				s, err := Open(filepath.Join(b.TempDir(), "full.db"))
				if err != nil {
					b.Fatal(err)
				}
				for i := 0; i < 25; i++ {
					p, err := Resolve(s, KindEmail, fmt.Sprintf("author-%d@example.test", i), fmt.Sprintf("Synthetic Author %d", i))
					if err != nil {
						b.Fatal(err)
					}
					at := time.Unix(1700000000, 0)
					_, err = s.Put(Entry{Source: SourceMail, ExtID: fmt.Sprint("mail:", i), PersonID: p, TS: at, BodyText: askBody}, &Mail{MessageID: fmt.Sprint(i)}, nil)
					if err != nil {
						b.Fatal(err)
					}
					if _, _, err = s.PutQuoted(Entry{Source: SourceMail, ExtID: fmt.Sprint("quote:", i), PersonID: p, TS: at.Add(10 * time.Hour), BodyText: askQuoted}); err != nil {
						b.Fatal(err)
					}
				}
				if warm {
					if _, err := benchmarkFullRound(s); err != nil {
						b.Fatal(err)
					}
				}
				b.StartTimer()
				count, err := benchmarkFullRound(s)
				b.StopTimer()
				if err != nil {
					b.Fatal(err)
				}
				removed += count
				s.Close()
			}
			b.ReportMetric(float64(removed)/float64(b.N), "removed/op")
		})
	}
}

func benchmarkFullRound(s *Store) (int, error) {
	plan, err := CollapseTwins(s, true)
	if err != nil {
		return 0, err
	}
	if _, err = RepairQuotedBodies(s); err != nil {
		return 0, err
	}
	if _, err = RepairMailtoIdentities(s); err != nil {
		return 0, err
	}
	if _, err = RepairPlusAddresses(s); err != nil {
		return 0, err
	}
	if _, err = RepairTruncatedNames(s); err != nil {
		return 0, err
	}
	if _, err = s.RepairGraph(); err != nil {
		return 0, err
	}
	if _, err = s.ReassertParents(); err != nil {
		return 0, err
	}
	if _, err = s.RepairDanglingQuoteParents(); err != nil {
		return 0, err
	}
	return plan.Removed, nil
}
