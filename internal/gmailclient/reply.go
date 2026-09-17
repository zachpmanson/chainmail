package gmailclient

import (
	"errors"
	"fmt"

	"github.com/zachpmanson/docket/gmail/mail"
)

// The two ways a reply can fail, told apart because they demand opposite answers
// from the reader.
//
// A send is the one write in this program that cannot be undone or retried
// safely: it is not a label that can be put back. So the distinction is the whole
// of what a failure means — nothing went out, or the mailbox was asked and did not
// answer, in which case whether the message went out is not knowable from here and
// the reader has to look. Wrapped rather than returned as a bare error because a
// caller can only act on this by asking which of the two it was.
var (
	// ErrUnsent: the message was never handed to the mailbox, so a retry cannot
	// double-send.
	ErrUnsent = errors.New("nothing was sent")
	// ErrSendUnknown: the mailbox was asked to send and the answer did not arrive
	// cleanly. The message may have gone out; only Gmail can say.
	ErrSendUnknown = errors.New("the send did not complete")
)

// ReplyPlan is a reply as the mailbox sees it: who it goes to and what it says,
// and — once it has been sent — the id of the message that went out.
//
// To and Subject come from the message being answered rather than from the caller,
// which is the point of preparing one here: the recipient is the address that
// message arrived from and the subject is its own with one "Re:", both as the
// mailbox holds them. Body is what the caller handed in, already composed (see
// spec.ReplyBody) — this package quotes nothing.
type ReplyPlan struct {
	To      string
	Subject string
	Body    string
	// GmailID is empty until the message has been sent, and is what the caller
	// files the reply into the corpus by.
	GmailID string
}

// Reply prepares a reply to one message, and sends it when send is true.
//
// send=false is a plan and nothing else: the same metadata read a send begins
// with, answered instead of executed. That is what makes the two-step in the UI a
// preview of the message rather than of a draft — the recipient named is the one
// the mailbox will use, and the body is the body.
func (c Client) Reply(id, body string, send bool) (ReplyPlan, error) {
	plan, err := mail.PrepareReply(c.ctx, c.svc, id, body)
	if err != nil {
		// Nothing has been handed to the mailbox, so a retry is safe and this is
		// the one failure that can say so.
		return ReplyPlan{}, fmt.Errorf("%w: %v", ErrUnsent, err)
	}
	out := ReplyPlan{To: plan.To, Subject: plan.Subject, Body: plan.Body}
	if !send {
		return out, nil
	}
	env, err := plan.Execute(c.ctx, c.svc, c.labels)
	if err != nil {
		// The plan comes back with the error: what was being sent is the useful
		// half of the message when the answer was lost.
		return out, fmt.Errorf("%w: %v", ErrSendUnknown, err)
	}
	out.GmailID = env.ID
	return out, nil
}
