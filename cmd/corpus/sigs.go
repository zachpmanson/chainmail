package main

import (
	"fmt"
	"io"
	"sort"
	"text/tabwriter"

	"github.com/zachpmanson/chainmail/internal/boiler"
	"github.com/zachpmanson/chainmail/internal/corpus"
)

// sigsReport prints which trailing blocks the corpus says are appended rather
// than written, and how much of the page they take off screen.
//
// The detection is derived on every run rather than stored, which makes it
// invisible to anything that does not ask — so this is the thing that asks. It
// is also the only way to check the thresholds against a corpus: a fold on the
// page argues for itself in one tooltip, while the question of whether the
// thresholds are right is a question about the distribution.
func sigsReport(w io.Writer, s *corpus.Store, people bool, domains bool) error {
	msgs, err := s.MailBodies()
	if err != nil {
		return err
	}
	folds := boiler.Detect(msgs, boiler.Default())

	direct, err := directEntries(s)
	if err != nil {
		return err
	}
	lines, bytes := 0, 0
	byScope := map[boiler.Scope]int{}
	pool := map[bool]int{}
	hit := map[bool]int{}
	for _, m := range msgs {
		pool[direct[m.ID]]++
	}
	authors, doms := map[int64]int{}, map[string]int{}
	byID := make(map[int64]boiler.Message, len(msgs))
	for _, m := range msgs {
		byID[m.ID] = m
	}
	for id, f := range folds {
		lines += f.Lines
		byScope[f.Scope]++
		m := byID[id]
		for _, l := range m.Lines[len(m.Lines)-f.Lines:] {
			bytes += len(l) + 1
		}
		hit[direct[id]]++
		if f.Scope == boiler.Domain {
			doms[m.Domain]++
		} else {
			authors[m.Author]++
		}
	}
	fmt.Fprintf(w, "mail bodies with a visible line   %d\n", len(msgs))
	fmt.Fprintf(w, "  with a block folded             %d\n", len(folds))
	fmt.Fprintf(w, "    one sender's signature        %d\n", byScope[boiler.Author])
	fmt.Fprintf(w, "    one domain's notice           %d\n", byScope[boiler.Domain])
	fmt.Fprintf(w, "lines taken off screen            %d\n", lines)
	fmt.Fprintf(w, "bytes taken off screen            %d\n", bytes)
	// Split by provenance because the quoting client may already have stripped
	// the block: a recovered entry whose signature the quote dropped has nothing
	// to fold, and a rate far below the mailbox's own would say so.
	fmt.Fprintf(w, "  of mailbox messages               %d of %d\n", hit[true], pool[true])
	fmt.Fprintf(w, "  of entries recovered from quotes  %d of %d\n", hit[false], pool[false])
	fmt.Fprintf(w, "senders with a repeated block     %d\n", len(authors))
	fmt.Fprintf(w, "domains with a repeated notice    %d\n", len(doms))

	names, err := personNames(s)
	if err != nil {
		return err
	}
	if people {
		t := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintln(t, "\nsender\tentries folded")
		ids := make([]int64, 0, len(authors))
		for id := range authors {
			ids = append(ids, id)
		}
		sort.Slice(ids, func(i, j int) bool { return authors[ids[i]] > authors[ids[j]] })
		for _, id := range ids {
			fmt.Fprintf(t, "%s\t%d\n", names[id], authors[id])
		}
		if err := t.Flush(); err != nil {
			return err
		}
	}
	if domains {
		t := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintln(t, "\ndomain\tentries folded")
		ks := make([]string, 0, len(doms))
		for d := range doms {
			ks = append(ks, d)
		}
		sort.Slice(ks, func(i, j int) bool { return doms[ks[i]] > doms[ks[j]] })
		for _, d := range ks {
			fmt.Fprintf(t, "%s\t%d\n", d, doms[d])
		}
		return t.Flush()
	}
	return nil
}

// directEntries is the set of entries the mailbox itself holds, as against the
// ones recovered from inside somebody's quote.
func directEntries(s *corpus.Store) (map[int64]bool, error) {
	rows, err := s.DB().Query(`select entry_id from sightings where kind = 'direct'`)
	if err != nil {
		return nil, fmt.Errorf("reading sightings: %w", err)
	}
	defer rows.Close()
	out := map[int64]bool{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

func personNames(s *corpus.Store) (map[int64]string, error) {
	rows, err := s.DB().Query(`select id, coalesce(display_name, '') from people`)
	if err != nil {
		return nil, fmt.Errorf("reading people: %w", err)
	}
	defer rows.Close()
	out := map[int64]string{}
	for rows.Next() {
		var id int64
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		out[id] = name
	}
	return out, rows.Err()
}
