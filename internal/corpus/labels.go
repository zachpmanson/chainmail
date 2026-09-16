package corpus

import (
	"sort"
	"strings"
)

// LabelCount is one folder: a label the mailbox put on a message, and how many
// messages carry it.
//
// Messages rather than chains, deliberately. A chain count for a label is a walk
// over the reply graph — the same pass that makes the list, run again for the
// sidebar — and a mail client's own sidebar counts messages. The number here is
// the one a reader already expects next to INBOX.
type LabelCount struct {
	Name     string
	Messages int
}

// Labels lists every label on a mailbox message, busiest first.
//
// The labels are the mailbox's own — the Gmail system labels (INBOX, SENT,
// UNREAD, STARRED, IMPORTANT, the CATEGORY_* buckets) and whatever Zach filed
// things under — because they are the only honest answer to "what folders are
// there": a folder list invented here would be a fiction about his mail, while
// this is what he already sees in the mailbox itself.
//
// One scan of one small column, counted in Go: mail_detail.labels is a
// comma-joined string, so SQL can count rows but not the labels inside them, and
// a labels table would be a schema change to answer a question this reads in a
// few milliseconds.
func (s *Store) Labels() ([]LabelCount, error) {
	rows, err := s.db.Query(
		`select labels from mail_detail where labels is not null and labels <> ''`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	counts := map[string]int{}
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		for _, l := range strings.Split(raw, ",") {
			if l = strings.TrimSpace(l); l != "" {
				counts[l]++
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := make([]LabelCount, 0, len(counts))
	for name, n := range counts {
		out = append(out, LabelCount{Name: name, Messages: n})
	}
	// A fixed order, so two reads of an unchanged corpus agree: busiest first,
	// and the name breaks a tie. Nothing here is a judgement about the labels —
	// only about how to print them.
	sort.Slice(out, func(i, j int) bool {
		if out[i].Messages != out[j].Messages {
			return out[i].Messages > out[j].Messages
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}
