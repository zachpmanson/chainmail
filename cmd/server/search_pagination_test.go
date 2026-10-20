package main

import (
	"fmt"
	"net/url"
	"testing"
	"time"

	"github.com/zachpmanson/chainmail/internal/corpus"
)

func TestSearchCompoundCursorTraversesTiesAndSpanningChains(t *testing.T) {
	h := testServer(t)
	stamp := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	put := func(ext string, ts time.Time, label string) int64 {
		t.Helper()
		r, err := h.store.Put(corpus.Entry{Source: corpus.SourceMail, ExtID: ext, TS: ts, BodyText: ext}, &corpus.Mail{Labels: []string{label}}, nil)
		if err != nil {
			t.Fatal(err)
		}
		return r.ID
	}
	const tied = 105
	for i := range tied {
		put(fmt.Sprintf("mail:page-%03d", i), stamp, "PAGING")
	}
	put("mail:older", stamp.Add(-time.Hour), "PAGING")
	root := put("mail:spanning", stamp.Add(-2*time.Hour), "PAGING")
	// More than the entry candidate cap on one chain must not hide other roots.
	for i := range 501 {
		child := put(fmt.Sprintf("mail:child-%03d", i), stamp.Add(time.Hour), "")
		if err := h.store.SetParent(child, root); err != nil {
			t.Fatal(err)
		}
	}
	api := loadAPI(t)
	seen := map[string]bool{}
	cursor := ""
	for page := 0; ; page++ {
		if page > 4 {
			t.Fatal("pagination did not terminate")
		}
		res := h.do(t, "GET", "/v1/search?label=PAGING&limit=50&cursor="+url.QueryEscape(cursor), nil)
		if res.status != 200 {
			t.Fatalf("status=%d %s", res.status, res.body)
		}
		api.assert(t, "SearchResponse", res.body)
		got := decode[searchResponse](t, res)
		if got.Chains == nil {
			t.Fatal("missing chains")
		}
		if page == 0 && (*got.Chains)[0].RootExtID != "mail:spanning" {
			t.Fatalf("whole-chain recency ignored: %+v", (*got.Chains)[0])
		}
		for _, c := range *got.Chains {
			if seen[c.RootExtID] {
				t.Fatalf("repeated %s", c.RootExtID)
			}
			seen[c.RootExtID] = true
		}
		if got.NextCursor == "" {
			break
		}
		if got.NextCursor == cursor {
			t.Fatal("cursor did not advance")
		}
		cursor = got.NextCursor
	}
	if len(seen) != tied+2 || !seen["mail:older"] || !seen["mail:page-104"] {
		t.Fatalf("unreachable roots: seen=%d want=%d", len(seen), tied+2)
	}
}

func TestSearchRejectsInvalidOrIncompatibleChainCursors(t *testing.T) {
	h := testServer(t)
	valid := url.QueryEscape((corpus.ChainCursor{Last: time.Now(), RootExtID: "mail:test"}).String())
	for _, query := range []string{"cursor=invalid", "cursor=" + valid + "&entries=true", "cursor=" + valid + "&q=solar", "cursor=" + valid + "&before=2030-01-01T00:00:00Z"} {
		res := h.do(t, "GET", "/v1/search?"+query, nil)
		if res.status != 400 {
			t.Fatalf("%s: status=%d %s", query, res.status, res.body)
		}
	}
}
