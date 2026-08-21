package main

import (
	"fmt"
	"io"

	"github.com/zachpmanson/chainmail/internal/refresh"
)

// printRefresh says what the two passes did, then what changed.
//
// The per-pass lines are printed even when nothing came of them, because they are
// the proof the refresh looked: "nothing new" is only trustworthy next to the
// list of what was asked. A refresh that could not ask returns an error instead
// and never reaches here.
func printRefresh(w io.Writer, r refresh.Report) {
	where := "corpus only"
	if r.Fetched {
		where = "mailbox and corpus"
	}
	fmt.Fprintf(w, "refresh: %s — %s, %s\n", where,
		plural(len(r.Queries), "query", "queries"), plural(len(r.Threads), "chain", "chains"))

	for _, p := range r.Queries {
		sent := p.Sent
		if sent == "" {
			sent = p.Q
		}
		fmt.Fprintf(w, "  query  %-40s %s\n", trunc(sent, 40), queryOutcome(p))
	}
	for _, p := range r.Threads {
		name := orElse(oneLine(p.Subject, 40), p.ID)
		fmt.Fprintf(w, "  chain  %-40s %s\n", trunc(name, 40), threadOutcome(p))
	}

	for _, g := range r.ChainsGrown {
		fmt.Fprintf(w, "grew    %s: %d -> %d entries\n", orElse(oneLine(g.Subject, 60), g.ID),
			g.Before, g.After)
	}
	for _, g := range r.ChainsAdded {
		fmt.Fprintf(w, "added   %s: %d entries\n", orElse(oneLine(g.Subject, 60), g.ID), g.After)
	}
	for _, c := range r.ChainsProposed {
		fmt.Fprintf(w, "propose %s — %d entries, %d matched, %s\n",
			orElse(oneLine(c.Subject, 60), c.Container), c.Entries, c.Matched, c.Span)
		fmt.Fprintf(w, "        matched %q; accept it with -accept %s\n", c.Query, c.RootExtID)
	}
	for _, id := range r.ChainsUnranked {
		fmt.Fprintf(w, "kept    %s is no longer returned by any recorded query, and stays on the "+
			"page: dropping it would delete entries somebody has already read\n", id)
	}

	if r.NothingNew() {
		fmt.Fprintf(w, "nothing new — %d entries, unchanged\n", r.EntriesAfter)
		return
	}
	fmt.Fprintf(w, "entries %d -> %d; stored %d new, %d altered\n",
		r.EntriesBefore, r.EntriesAfter, r.Created(), r.Changed())
}

func queryOutcome(p refresh.QueryPass) string {
	s := fmt.Sprintf("proposes %d", p.Proposed)
	if p.Sent == "" {
		return s + ", mailbox not asked"
	}
	return fmt.Sprintf("saw %d, read %d, created %d, changed %d, %s",
		p.Seen, p.Fetched, p.Created, p.Changed, s)
}

func threadOutcome(p refresh.ThreadPass) string {
	if p.Skipped != "" {
		return "regenerated; " + p.Skipped
	}
	return fmt.Sprintf("saw %d, read %d, created %d, changed %d",
		p.Seen, p.Fetched, p.Created, p.Changed)
}
