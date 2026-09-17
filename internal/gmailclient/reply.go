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
// To, Cc and Subject come from the message being answered rather than from the
// caller, which is the point of preparing one here: the recipients are the
// addresses that message was addressed to and the subject is its own with one
// "Re:", both as the mailbox holds them. Body is what the caller handed in,
// already composed (see spec.ReplyBody) — this package quotes nothing.
//
// Cc is empty for a message the reader was the only recipient of, and the server
// leaves it out of the contract entirely when it is (see its sendResponse):
// "nobody else" and "a field nobody filled in" are the same fact about a reply.
type ReplyPlan struct {
	To      string
	Cc      string
	Subject string
	Body    string
	// GmailID is empty until the message has been sent, and is what the caller
	// files the reply into the corpus by.
	GmailID string
}

// Reply prepares an answer to one message, and sends it when send is true.
//
// It answers EVERYONE the message was addressed to — the sender in To, the rest of
// its audience in Cc — and who that is is not the caller's to decide: the
// recipients come from the message's own headers, and the addresses belonging to
// this mailbox are left out of them by the mailbox itself (docket reads the
// account's profile and its send-as aliases, which is the only place the answer to
// "which of these addresses is the reader" exists — mail is usually addressed to an
// alias rather than to the account's own name). A caller cannot name a recipient
// here, and that is deliberate: a server bound to loopback with no authentication
// must not be an outbound channel to anywhere, and there is no address on this
// surface that the reader's own correspondence did not carry first.
//
// send=false is a plan and nothing else: the same reads a send begins with,
// answered instead of executed. That is what makes the two-step in the UI a
// preview of the message rather than of a draft — the recipients named are the
// ones the mailbox will use, and the body is the body.
func (c Client) Reply(id, body string, send bool) (ReplyPlan, error) {
	plan, err := mail.PrepareReplyAll(c.ctx, c.svc, id, body)
	if err != nil {
		// Nothing has been handed to the mailbox, so a retry is safe and this is
		// the one failure that can say so.
		return ReplyPlan{}, fmt.Errorf("%w: %v", ErrUnsent, err)
	}
	out := ReplyPlan{To: plan.To, Cc: plan.Cc, Subject: plan.Subject, Body: plan.Body}
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
