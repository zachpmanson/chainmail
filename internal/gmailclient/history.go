package gmailclient

import (
	"errors"
	"fmt"
	"strconv"

	"google.golang.org/api/googleapi"

	"github.com/zachpmanson/chainmail/internal/mailingest"
)

// HistoryID captures the mailbox high-water mark before a bootstrap scan. The
// existing mail.google.com grant permits profile and history reads; no new
// scope or OAuth consent is needed.
func (c Client) HistoryID() (string, error) {
	profile, err := c.svc.Users.GetProfile("me").Context(c.ctx).Do()
	if err != nil {
		return "", fmt.Errorf("reading Gmail profile history ID: %w", err)
	}
	return strconv.FormatUint(profile.HistoryId, 10), nil
}

// History deliberately supplies no label/type filters: labels removed from
// already-ingested mail matter just as much as newly received messages.
func (c Client) History(startID, pageToken string) (mailingest.HistoryPage, error) {
	id, err := strconv.ParseUint(startID, 10, 64)
	if err != nil || id == 0 {
		return mailingest.HistoryPage{}, fmt.Errorf("invalid Gmail history ID %q", startID)
	}
	res, err := c.svc.Users.History.List("me").StartHistoryId(id).
		MaxResults(500).PageToken(pageToken).Context(c.ctx).Do()
	if err != nil {
		if gmailNotFound(err) {
			return mailingest.HistoryPage{}, fmt.Errorf("%w: %w", mailingest.ErrHistoryExpired, err)
		}
		return mailingest.HistoryPage{}, err
	}
	page := mailingest.HistoryPage{ID: strconv.FormatUint(res.HistoryId, 10), NextPageToken: res.NextPageToken}
	for _, h := range res.History {
		// Ignore h.Messages: it is redundant and less specific than the typed
		// events, and includes permanently deleted messages we cannot read.
		for _, e := range h.MessagesAdded {
			if e.Message != nil {
				page.Changed = append(page.Changed, e.Message.Id)
			}
		}
		for _, e := range h.LabelsAdded {
			if e.Message != nil {
				page.Changed = append(page.Changed, e.Message.Id)
			}
		}
		for _, e := range h.LabelsRemoved {
			if e.Message != nil {
				page.Changed = append(page.Changed, e.Message.Id)
			}
		}
		for _, e := range h.MessagesDeleted {
			if e.Message != nil {
				page.Deleted = append(page.Deleted, e.Message.Id)
			}
		}
	}
	return page, nil
}

func gmailNotFound(err error) bool {
	var apiErr *googleapi.Error
	return errors.As(err, &apiErr) && apiErr.Code == 404
}
