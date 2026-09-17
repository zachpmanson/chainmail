package corpus

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ReplyTarget is one message as a reply to it needs it: the mailbox id an answer
// threads against, and everything the quote under the reader's own words is made
// of.
//
// It is a query of its own rather than a field on Shown because it is read for a
// different purpose: Shown answers "what is this entry", and this answers "what
// does a reply to it say and where does it go". Half of these columns are on
// mail_detail, which Shown does not read at all, and the reader of this row is a
// writer rather than a drawing.
//
// GmailID is the point of the type. An entry with none — a message recovered from
// somebody's quote, a Slack post — is a real part of a chain with no mailbox
// message behind it, and a reply to it is not a smaller version of the same
// thing: there is nothing to thread against. So the caller is handed the empty
// string rather than an error, and says so in its own words (see cmd/server's
// send, where it is a refusal naming the entry).
type ReplyTarget struct {
	ExtID   string
	GmailID string
	// Author is the person the corpus resolved this entry to; From is the header
	// as it arrived, and is what an attribution names when there is no person, or
	// when the address is the more honest half of the pair.
	Author  string
	From    string
	Subject string
	Body    string
	// HTML is the sender's own text/html part for this message, exactly as their
	// client wrote it, and empty when the message had none. A reply quotes it in
	// place of the text when it is there (see spec.ComposeReply): the message being
	// answered had a markup form and the answer carries it, which is what makes the
	// quote under a reply to an HTML message look like the message rather than like
	// a transcript of it.
	HTML     string
	TS       time.Time
	TZ       string
	TZOffset *int
}

// ReplyTarget reads the message a reply would answer: the entry at extID, with
// what a quote of it and a send threaded against it need.
//
// An entry the corpus does not hold is ErrNotFound, which is the caller's 404;
// an entry with no mailbox copy comes back with an empty GmailID, which is the
// caller's 400. The two are different facts about the request and only the caller
// can spell them.
func (s *Store) ReplyTarget(extID string) (ReplyTarget, error) {
	var t ReplyTarget
	var ts int64
	var off sql.NullInt64
	var author, from, subject, body, bodyHTML, tz, gmail sql.NullString
	err := s.db.QueryRow(`
		select e.ext_id, e.ts, e.tz, e.tz_offset,
		       p.display_name, md.from_addr, e.subject, e.body_text, e.body_html, md.gmail_id
		from entries e
		left join people p on p.id = e.person_id
		left join mail_detail md on md.entry_id = e.id
		where e.ext_id = ?`, extID).
		Scan(&t.ExtID, &ts, &tz, &off, &author, &from, &subject, &body, &bodyHTML, &gmail)
	if errors.Is(err, sql.ErrNoRows) {
		return ReplyTarget{}, fmt.Errorf("%q: %w", extID, ErrNotFound)
	}
	if err != nil {
		return ReplyTarget{}, fmt.Errorf("reading the entry to reply to: %w", err)
	}
	t.TS = time.Unix(ts, 0)
	if off.Valid {
		m := int(off.Int64)
		t.TZOffset = &m
	}
	t.TZ, t.Author, t.From = tz.String, author.String, from.String
	t.Subject, t.Body, t.GmailID = subject.String, body.String, gmail.String
	t.HTML = bodyHTML.String
	return t, nil
}

// Who names the sender for an attribution line, e.g.
// "Ada Okoye <ada@loomworks.example>", and never invents one.
//
// The header is read with the same parser the ingest reads it with, so the name
// and the address are taken apart once and in one place: a From header written
// "Ada Okoye <ada@loomworks.example>" names a person, and wrapping that string in
// a second pair of brackets would be a heading claiming somebody called "Ada
// Okoye <ada@loomworks.example>". The display name the corpus resolved wins where
// there is one — that is the name the message's own bubble wears, and a heading
// that called the same person something else would be a second answer about who
// wrote this.
//
// What it will not do is reach for an address the entry never stated: a person row
// with no address behind it names the person alone, and an entry nothing names is
// the empty string (the caller's word for it), exactly as the sender hover treats
// the same absence.
func (t ReplyTarget) Who() string {
	name := strings.TrimSpace(t.Author)
	addr := ""
	if a, ok := ParseAddress(t.From); ok {
		addr = a.Addr
		if name == "" {
			name = a.Name
		}
	}
	switch {
	case name == "":
		return addr
	case addr == "":
		return name
	default:
		return name + " <" + addr + ">"
	}
}
