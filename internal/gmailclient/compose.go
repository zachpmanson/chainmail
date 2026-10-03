package gmailclient

import (
	"fmt"
	"strings"

	"github.com/zachpmanson/docket/gmail/mail"
)

// ComposePlan is the exact plain-text message the mailbox will receive. GmailID
// is empty for a preview and set only after Execute has returned successfully.
type ComposePlan struct {
	To      string
	Subject string
	Body    string
	GmailID string
}

// Compose validates and prepares an explicitly addressed plain-text message.
// With send=false it performs no write; with send=true it executes the same
// plan returned by the preparation step.
func (c Client) Compose(to, subject, body string, send bool) (ComposePlan, error) {
	if strings.TrimSpace(to) == "" {
		return ComposePlan{}, fmt.Errorf("%w: to: at least one recipient is required", ErrUnsent)
	}
	if strings.TrimSpace(subject) == "" {
		return ComposePlan{}, fmt.Errorf("%w: subject: a subject is required", ErrUnsent)
	}
	if strings.TrimSpace(body) == "" {
		return ComposePlan{}, fmt.Errorf("%w: body: a plain-text body is required", ErrUnsent)
	}
	plan, err := mail.PrepareSend(to, subject, mail.Body{Text: body})
	if err != nil {
		return ComposePlan{}, fmt.Errorf("%w: preparing composed message: %v", ErrUnsent, err)
	}
	out := ComposePlan{To: plan.To, Subject: plan.Subject, Body: plan.Body}
	if !send {
		return out, nil
	}
	env, err := plan.Execute(c.ctx, c.svc, c.labels)
	if err != nil {
		return out, fmt.Errorf("%w: %v", ErrSendUnknown, err)
	}
	out.GmailID = env.ID
	return out, nil
}
