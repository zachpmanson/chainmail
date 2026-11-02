package mailingest

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/zachpmanson/chainmail/internal/corpus"
)

func TestQuotedIngestConservativeTwinsFallback(t *testing.T) {
	for _, depth := range []int{0, 1, 2} {
		t.Run(fmt.Sprint(depth), func(t *testing.T) {
			s := openTest(t)
			if _, err := corpus.ReconcileTwinsContext(context.Background(), s); err != nil {
				t.Fatal(err)
			}
			body := "A synthetic host response."
			if depth >= 1 {
				body += "\n\nOn 20 May 2026, at 11:38, Synthetic One <one@example.test> wrote:\nA distinct quoted message about an observatory and its instrumentation."
			}
			if depth >= 2 {
				body += "\n\nOn 19 May 2026, at 09:10, Synthetic Two <two@example.test> wrote:\nAn older independent message discussing library opening hours."
			}
			e := corpus.Entry{Source: corpus.SourceMail, ExtID: fmt.Sprintf("fallback-host-%d", depth), TS: time.Date(2026, 5, 21, 0, 0, 0, 0, time.UTC), BodyText: body}
			host, err := s.Put(e, &corpus.Mail{MessageID: e.ExtID}, nil)
			if err != nil {
				t.Fatal(err)
			}
			result, err := ExtractQuoted(s, host.ID, e, body)
			if err != nil {
				t.Fatal(err)
			}
			if result.Created != depth {
				t.Fatalf("depth=%d %+v", depth, result)
			}
			plan, err := corpus.ReconcileTwinsContext(context.Background(), s)
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("depth=%d created=%d edges=%d mode=%s fallback=%s authors=%d bodies=%d comparisons=%d", depth, result.Created, result.Edges, plan.Mode, plan.Fallback, plan.PeopleScanned, plan.BodiesScanned, plan.Comparisons)
			if depth == 0 {
				if plan.Mode != "current" || plan.BodiesScanned != 0 {
					t.Fatalf("independent authorless mail %+v", plan)
				}
			} else if plan.Mode != "full" || plan.Fallback != "graph-edge" || plan.BodiesScanned != depth {
				t.Fatalf("quoted graph fallback %+v", plan)
			}
		})
	}
}
