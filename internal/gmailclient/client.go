// Package gmailclient runs the Gmail reads directly through the shared docket
// library instead of shelling out to the docket binary.
//
// This is the in-process replacement for the subprocess seam in
// internal/mailingest/docket.go. Same Mailbox surface, same envelope shapes —
// only the transport differs: auth.TokenSource + mail.NewService open a live
// REST client in this process instead of exec'ing `docket-work`.
package gmailclient

import (
	"context"
	"fmt"

	"google.golang.org/api/gmail/v1"

	"github.com/zachpmanson/docket/gmail/auth"
	"github.com/zachpmanson/docket/gmail/mail"

	"github.com/zachpmanson/chainmail/internal/mailingest"
)

// Client is a mailingest.Mailbox backed by the docket gmail library.
type Client struct {
	ctx    context.Context
	svc    *gmail.Service
	labels *mail.LabelCache
}

// New opens the auth store and a Gmail service.
//
// The client is read-most: everything the ingest, the media pull and the
// refresh ask of it is a read, and the one write it offers (SetUnread) is a
// single label change a reader asked for. The scope that permits it is the
// grant's own — docket's default provider registers https://mail.google.com/,
// which is write-capable — so the read-only posture this package used to claim
// was never enforced here. It is enforced where it belongs: the server refuses
// to reach this path without -mark-read, and this client is constructed only
// when it is asked for.
func New() (*Client, error) {
	ctx := context.Background()
	cfg, err := auth.LoadConfig()
	if err != nil {
		return nil, fmt.Errorf("loading docket config: %w", err)
	}
	src, err := auth.TokenSource(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("docket auth: %w", err)
	}
	svc, err := mail.NewService(ctx, src)
	if err != nil {
		return nil, fmt.Errorf("building gmail service: %w", err)
	}
	labels, err := mail.LoadLabels(ctx, svc)
	if err != nil {
		return nil, fmt.Errorf("loading labels: %w", err)
	}
	return &Client{ctx: ctx, svc: svc, labels: labels}, nil
}

// Search runs a Gmail query and returns one page of envelopes plus the paging
// block. Pass the previous page's NextPageToken to continue; pass "" to start.
func (c Client) Search(query string, limit int, pageToken string) ([]mailingest.Envelope, mailingest.Page, error) {
	l := int64(limit)
	if l <= 0 {
		l = int64(mail.MaxLimit)
	}
	res, err := mail.List(c.ctx, c.svc, c.labels, mail.ListOptions{
		Query:     query,
		Limit:     l,
		PageToken: pageToken,
	})
	if err != nil {
		return nil, mailingest.Page{}, err
	}
	envs, page := pageOf(res, l)
	return envs, page, nil
}

// Read fetches one message in full, with its HTML part, at full size — see the
// maxBytes reasoning in docket.go.
func (c Client) Read(id string) (mailingest.Message, error) {
	msg, err := mail.Read(c.ctx, c.svc, c.labels, id, mail.ReadOptions{
		MaxBytes:    0, // 0 = unlimited; see mailingest.maxBytes
		IncludeHTML: true,
	})
	if err != nil {
		return mailingest.Message{}, err
	}
	return convertMessage(msg), nil
}

// UnreadMessageIDs reads the mailbox's complete unread set, paging to
// exhaustion.
//
// Complete is the whole contract, and the reason this returns an error rather
// than a prefix: the corpus reconciles its own UNREAD labels against this list,
// and an id missing from it is indistinguishable from a message somebody has
// read. A truncated read would therefore clear good labels wholesale, so every
// failure — a page that fails, a cursor that will not advance, more pages than
// any mailbox has — is returned rather than reported as a smaller answer.
//
// in:anywhere, because the ingest reads in:anywhere: Gmail's own default scope
// for a search excludes Spam and Trash, and a message the corpus holds from
// either folder would then look read the moment it was reconciled.
func (c Client) UnreadMessageIDs() ([]string, error) {
	var ids []string
	var token string
	// A mailbox with more unread pages than this has stopped being mail and
	// started being an incident; the bound is here so a cursor that loops cannot
	// become an unbounded run against a personal account.
	const maxPages = 200
	for page := 0; page < maxPages; page++ {
		res, err := mail.List(c.ctx, c.svc, c.labels, mail.ListOptions{
			Query:     "in:anywhere is:unread",
			Limit:     mail.MaxLimit,
			PageToken: token,
		})
		if err != nil {
			return nil, fmt.Errorf("listing unread mail: %w", err)
		}
		for _, e := range res.Envelopes {
			if e.ID != "" {
				ids = append(ids, e.ID)
			}
		}
		if res.NextPageToken == "" {
			return ids, nil
		}
		if res.NextPageToken == token {
			return nil, fmt.Errorf("gmail returned the same page token twice (%q) after %d messages", token, len(ids))
		}
		token = res.NextPageToken
	}
	return nil, fmt.Errorf("unread mail did not finish after %d pages: refusing to reconcile against a partial set", maxPages)
}

// unreadLabel is the mailbox's own name for "nobody has opened this yet".
//
// Spelled here rather than imported from the corpus: this package is a
// transport and the corpus is a store, and a label name is the mailbox's
// vocabulary, not either of theirs. The two spellings are the same system label
// Gmail defines, which is why they cannot drift.
const unreadLabel = "UNREAD"

// SetUnread marks one message read or unread, and returns the labels the
// mailbox reports afterwards.
//
// The returned labels are the point: the caller stores exactly what Gmail said
// rather than editing its own copy by hand, so the local mirror cannot drift
// from the mailbox on the one message it just wrote. A label removed by some
// other client in the same second is carried back with it.
func (c Client) SetUnread(id string, unread bool) ([]string, error) {
	var add, remove []string
	if unread {
		add = []string{unreadLabel}
	} else {
		remove = []string{unreadLabel}
	}
	plan, err := mail.PrepareLabel(c.labels, id, add, remove)
	if err != nil {
		return nil, fmt.Errorf("preparing the label change on message %q: %w", id, err)
	}
	env, err := plan.Execute(c.ctx, c.svc, c.labels)
	if err != nil {
		return nil, err
	}
	return env.Labels, nil
}

// pageOf maps one library list page onto chainmail's paging block. HasMore
// follows the token directly, exactly as docket's CLI pageOf does: Gmail's
// next_page_token is the only truncation signal, so over-reporting one more
// page that turns out empty costs one call, whereas under-reporting loses
// results silently.
func pageOf(res *mail.ListResult, limit int64) ([]mailingest.Envelope, mailingest.Page) {
	envs := make([]mailingest.Envelope, len(res.Envelopes))
	for i, e := range res.Envelopes {
		envs[i] = convertEnvelope(e)
	}
	page := mailingest.Page{Returned: len(envs), Limit: int(limit)}
	if res.NextPageToken != "" {
		page.HasMore = true
		page.NextPageToken = res.NextPageToken
	}
	return envs, page
}

// convertEnvelope maps the library's envelope onto chainmail's. Same twelve
// fields, same Gmail ids — the two types exist because the subprocess protocol
// carries JSON while the library call is in-process.
func convertEnvelope(e mail.Envelope) mailingest.Envelope {
	return mailingest.Envelope{
		ID:         e.ID,
		ThreadID:   e.ThreadID,
		From:       e.From,
		To:         e.To,
		Cc:         e.Cc,
		Subject:    e.Subject,
		Date:       e.Date,
		MessageID:  e.MessageID,
		InReplyTo:  e.InReplyTo,
		References: e.References,
		Labels:     e.Labels,
		Snippet:    e.Snippet,
	}
}

// convertMessage maps the library's full message onto chainmail's: the same
// envelope, plus body/html and the attachment metadata chainmail records.
func convertMessage(m *mail.Message) mailingest.Message {
	atts := make([]struct {
		Filename string `json:"filename"`
		MimeType string `json:"mime_type"`
		Size     int64  `json:"size"`
		PartID   string `json:"part_id"`
	}, len(m.Attachments))
	for i, a := range m.Attachments {
		atts[i].Filename = a.Filename
		atts[i].MimeType = a.MimeType
		atts[i].Size = a.Size
		atts[i].PartID = a.PartID
	}
	return mailingest.Message{
		Envelope:      convertEnvelope(m.Envelope),
		Body:          m.Body,
		Truncated:     m.Truncated,
		BodyHTML:      m.BodyHTML,
		HTMLStatus:    m.HTMLStatus,
		HTMLTruncated: m.HTMLTruncated,
		Attachments:   atts,
	}
}
