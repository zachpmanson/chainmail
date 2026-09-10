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

// New opens the auth store and a Gmail service. Chainmail never writes, so the
// client it returns is read-only by construction: the OAuth scope is what
// enforces it, not an env var.
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
