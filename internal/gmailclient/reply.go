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

// ReplyBody is a reply's body in the two forms it is sent in: the plain text, and
// the same message as HTML (see spec.ComposeReply, which produces both from one
// reading of the words and the quote).
//
// They travel together rather than as two arguments because they are one message:
// a caller that could pass a text body and an unrelated HTML one would be able to
// send two messages in one envelope, and the parts of a multipart/alternative are
// only meaningful as two renderings of the same thing. **An empty HTML is how a
// caller sends the words alone** — the reader who asked for plain text gets the
// text part by itself rather than a message with an empty second half in it.
type ReplyBody struct {
	Text string
	HTML string
}

// Recipient is one address on a reply, with the display name the message being
// answered gave it. It is the address rather than the header text: the header
// the reply will carry is rendered from these (see ReplyPlan.To/Cc), so a client
// that shows a reader the addresses it can use is showing it the same set the
// mailbox will use.
type Recipient struct {
	Name    string `json:"name,omitempty"`
	Address string `json:"address"`
}

// ReplyPlan is a reply as the mailbox sees it: who it goes to and what it says,
// and — once it has been sent — the id of the message that went out.
//
// To, Cc and Subject come from the message being answered rather than from the
// caller, which is the point of preparing one here: the recipients are the
// addresses that message was addressed to and the subject is its own with one
// "Re:", both as the mailbox holds them. Body is what the caller handed in,
// already composed (see spec.ComposeReply) — this package quotes nothing, and it
// renders nothing either: the two forms are the caller's, and docket writes both
// into the message as they were given.
//
// ToRecipients and CcRecipients are those same two lists as addresses rather than
// as the header they will be written into, each with the name the message gave
// it. They are what a client offers a reader to arrange the audience with, and
// they are the set the request's `to`/`cc` start from: a caller that names
// nothing sends these, and one that names an address the message did not carry
// sends that too (see ReplyOptions.To/Cc).
//
// Cc is empty either when the caller asked for the sender alone or when there was
// nobody else on the message to begin with, and the server leaves it out of the
// contract entirely when it is (see its sendResponse): "nobody else" and "a field
// nobody filled in" are the same fact about a reply.
type ReplyPlan struct {
	To           string
	Cc           string
	Subject      string
	Body         string
	ToRecipients []Recipient
	CcRecipients []Recipient
	// GmailID is empty until the message has been sent, and is what the caller
	// files the reply into the corpus by.
	GmailID string
}

// ReplyOptions is how a reply is asked for: the audience it starts from, whether
// this call sends or only plans, and the audience it is to carry.
type ReplyOptions struct {
	// All answers the message's whole audience — the sender in To, the rest of it
	// in Cc — rather than the sender alone. It decides the set the reply starts
	// from when the caller names no list below.
	All bool
	// Send false is a plan and nothing else; true sends it.
	Send bool
	// To and Cc are the addresses the reply is to carry, in the list each is to
	// be carried in. A nil slice leaves that list as the plan assembled it, so a
	// caller changing one list need not restate the other. A non-nil Cc may be
	// empty, which drops everyone on it; an empty To is refused, because a message
	// with nobody on it is not a reply.
	//
	// **An entry may name an address the answered message did not carry, and that
	// is the point of these fields rather than a hole in a check.** The reply box
	// offers an address field — a reader types an address, or picks a person out of
	// the corpus, and the chip becomes a recipient — so a surface that could only
	// carry the addresses the message already had could not send what the reader
	// wrote. This is where that becomes sendable: docket accepts an address outside
	// the plan's own recipients (see mail.SendPlan.WithRecipients), and this package
	// passes the caller's lists through rather than filtering them.
	//
	// What that widens is stated where the decision is made (see Reply), and what
	// these fields still refuse is the shape of the list: an address that is not one,
	// and a repeated address — in one list or in both.
	To []string
	Cc []string
}

// Reply prepares an answer to one message, and sends it when opts.Send is true.
//
// It answers the message's whole audience when opts.All is true — the sender in
// To, the rest of it in Cc — and the sender alone when it is false. Either way who
// that is is not the caller's to decide: the recipients come from the message's own
// headers, and the addresses belonging to this mailbox are left out of them by the
// mailbox itself (docket reads the account's profile and its send-as aliases, which
// is the only place the answer to "which of these addresses is the reader" exists —
// mail is usually addressed to an alias rather than to the account's own name).
//
// opts.To and opts.Cc name the audience the reply is to carry, and they are the
// reader's arrangement of it rather than a subset of the message's own: an address
// neither the message nor this mailbox's profile has ever seen is accepted, because
// the surface in front of this is one a person types addresses into. That is a
// deliberate widening, and the reason it is not a hole is that what bounds a send
// was never this check: it is that the server is bound to loopback with no
// authentication, and that answering mail at all needs the host to have been started
// with -send-mail. A reply can now reach an address the reader typed; it still cannot
// reach one nobody at this host named.
//
// opts.Send=false is a plan and nothing else: the same reads a send begins with,
// answered instead of executed. That is what makes the two-step in the UI a
// preview of the message rather than of a draft — the recipients named are the
// ones the mailbox will use, and the body is the body.
func (c Client) Reply(id string, body ReplyBody, opts ReplyOptions) (ReplyPlan, error) {
	prepare := mail.PrepareReply
	if opts.All {
		prepare = mail.PrepareReplyAll
	}
	plan, err := prepare(c.ctx, c.svc, id, mail.Body{Text: body.Text, HTML: body.HTML})
	if err != nil {
		// Nothing has been handed to the mailbox, so a retry is safe and this is
		// the one failure that can say so.
		return ReplyPlan{}, fmt.Errorf("%w: %v", ErrUnsent, err)
	}
	// The caller's audience is applied to the plan the mailbox assembled: the plan's
	// own recipients keep the names the message gave them, and an address the caller
	// names that the message did not carry is carried as the caller wrote it. What
	// is refused here is a repeated address and an empty To, not an address the
	// message never had (see ReplyOptions.To/Cc).
	if opts.To != nil || opts.Cc != nil {
		to, cc := opts.To, opts.Cc
		if to == nil {
			to = addressesOf(plan.ToRecipients)
		}
		if cc == nil {
			cc = addressesOf(plan.CcRecipients)
		}
		chosen, err := plan.WithRecipients(to, cc)
		if err != nil {
			return ReplyPlan{}, fmt.Errorf("%w: choosing the reply's audience: %v", ErrUnsent, err)
		}
		plan = chosen
	}
	out := planOut(plan)
	if !opts.Send {
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

// planOut is docket's plan as this package's own: the wire shape is stated here
// rather than borrowed, so a field docket grows is a change this package makes a
// decision about (see cmd/server's wire types for the same split one layer up).
func planOut(plan *mail.SendPlan) ReplyPlan {
	return ReplyPlan{
		To: plan.To, Cc: plan.Cc, Subject: plan.Subject, Body: plan.Body,
		ToRecipients: recipientsIn(plan.ToRecipients),
		CcRecipients: recipientsIn(plan.CcRecipients),
	}
}

func recipientsIn(list []mail.Recipient) []Recipient {
	out := make([]Recipient, 0, len(list))
	for _, r := range list {
		out = append(out, Recipient{Name: r.Name, Address: r.Address})
	}
	return out
}

// addressesOf is a plan's recipient list as the bare addresses a request names
// them by: the display name is a rendering of the address, not part of it.
func addressesOf(list []mail.Recipient) []string {
	out := make([]string, 0, len(list))
	for _, r := range list {
		out = append(out, r.Address)
	}
	return out
}
