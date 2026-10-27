package mailingest

import (
	"errors"
	"fmt"
	"sort"
	"strconv"

	"github.com/zachpmanson/chainmail/internal/corpus"
)

var (
	ErrHistoryExpired = errors.New("Gmail history ID expired")
	ErrMessageGone    = errors.New("Gmail message no longer exists")
)

// HistoryMailbox is optional: the legacy subprocess and custom query walks
// retain their query cursors. History IDs are mailbox-wide, never query-local.
type HistoryMailbox interface {
	Mailbox
	HistoryID() (string, error)
	History(startID, pageToken string) (HistoryPage, error)
}

type HistoryPage struct {
	ID            string // high-water mark, committed only after the final page is applied
	NextPageToken string
	Changed       []string // messagesAdded, labelsAdded and labelsRemoved
	Deleted       []string
}

func ingestHistory(s *corpus.Store, c HistoryMailbox, b Bound, account string) (Result, error) {
	r := Result{History: true, Stop: StopExhausted}
	committed, bootstrap, err := s.GmailHistory(account)
	if err != nil {
		return r, err
	}
	// At most one restart if a long bootstrap outlives Google's history window.
	// Repeated expiry is an error, never silent coverage or an infinite scan.
	for attempt := 0; attempt < 2; attempt++ {
		if committed == "" && bootstrap == "" {
			bootstrap, err = beginBootstrap(s, c, account)
			if err != nil {
				return r, err
			}
		}
		start := committed
		if bootstrap != "" {
			start = bootstrap
			scan, err := ingestScan(s, c, "in:anywhere", b, account, bootstrap)
			addResult(&r, scan)
			if err != nil {
				return r, err
			}
			if !scan.Stop.Covered() {
				r.Stop = scan.Stop
				return r, nil
			}
			if err := s.ReconcileMissingGmailCopies(account, bootstrap); err != nil {
				return r, err
			}
		}
		end, ids, deleted, pages, err := historyChanges(c, start)
		r.Pages += pages
		if errors.Is(err, ErrHistoryExpired) && attempt == 0 {
			bootstrap, err = beginBootstrap(s, c, account)
			if err != nil {
				return r, err
			}
			continue
		}
		if err != nil {
			return r, err
		}
		batch, err := ingestIDs(s, c, ids, account, true)
		addResult(&r, batch)
		if err != nil {
			return r, err
		}
		for _, id := range deleted {
			if err := s.ForgetGmailCopyLabels(account, id); err != nil {
				return r, err
			}
		}
		// All reads, upserts and repairs succeeded. An interrupted replay leaves
		// the old checkpoint intact, so every event is safe to retry.
		return r, s.CommitGmailHistory(account, end)
	}
	return r, fmt.Errorf("Gmail history recovery did not finish")
}

func beginBootstrap(s *corpus.Store, c HistoryMailbox, account string) (string, error) {
	id, err := c.HistoryID()
	if err != nil {
		return "", err
	}
	if _, err := strconv.ParseUint(id, 10, 64); err != nil || id == "0" {
		return "", fmt.Errorf("invalid Gmail profile history ID %q", id)
	}
	return id, s.BeginGmailBootstrap(account, id)
}

// Collect before reading: a repeated ID anywhere in the history costs one
// full read, and a failed page cannot commit a prefix's high-water mark.
func historyChanges(c HistoryMailbox, start string) (end string, ids, deleted []string, pages int, err error) {
	changed, gone := map[string]bool{}, map[string]bool{}
	tokens := map[string]bool{"": true}
	token := ""
	for {
		var page HistoryPage
		pages++
		page, err = c.History(start, token)
		if err != nil {
			return
		}
		for _, id := range page.Changed {
			if id != "" {
				changed[id] = true
			}
		}
		for _, id := range page.Deleted {
			if id != "" {
				gone[id] = true
			}
		}
		if page.NextPageToken == "" {
			end = page.ID
			n, parseErr := strconv.ParseUint(end, 10, 64)
			first, startErr := strconv.ParseUint(start, 10, 64)
			if parseErr != nil || startErr != nil || n < first || n == 0 {
				err = fmt.Errorf("invalid Gmail history checkpoint %q (start %q)", end, start)
				return
			}
			break
		}
		if tokens[page.NextPageToken] {
			err = fmt.Errorf("Gmail history repeated page token")
			return
		}
		token = page.NextPageToken
		tokens[token] = true
	}
	for id := range changed {
		ids = append(ids, id)
	}
	for id := range gone {
		// A changed-and-deleted ID is read once: 404 clears labels, but a
		// successful read is newer evidence than any intermediate delete event.
		if !changed[id] {
			deleted = append(deleted, id)
		}
	}
	sort.Strings(ids)
	sort.Strings(deleted)
	return
}

func addResult(dst *Result, src Result) {
	dst.Seen += src.Seen
	dst.Created += src.Created
	dst.Changed += src.Changed
	dst.Drafts += src.Drafts
	dst.Resolved += src.Resolved
	dst.Reasserted += src.Reasserted
	dst.QuotedParents += src.QuotedParents
	dst.Truncated += src.Truncated
	dst.Pages += src.Pages
	dst.Reads += src.Reads
	dst.Resumed = dst.Resumed || src.Resumed
}
