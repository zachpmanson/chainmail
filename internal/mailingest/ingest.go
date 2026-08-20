package mailingest

import (
	"fmt"
	"net/mail"
	"regexp"
	"strings"
	"time"

	"github.com/zachpmanson/chainmail/internal/corpus"
)

// Result summarises one ingest run.
type Result struct {
	Seen      int
	Created   int
	Changed   int
	Resolved  int64 // parent edges linked after this batch
	Truncated int   // bodies docket still had to cut — should always be zero
}

// Ingest walks the results of a Gmail query, reads each message in full, and
// upserts it. Idempotent: re-running over the same query changes nothing unless
// a message's body actually changed.
func Ingest(store *corpus.Store, c Client, query string, limit int) (Result, error) {
	var r Result

	envs, err := c.Search(query, limit)
	if err != nil {
		return r, err
	}
	for _, env := range envs {
		msg, err := c.Read(env.ID)
		if err != nil {
			return r, fmt.Errorf("reading %s: %w", env.ID, err)
		}
		r.Seen++
		if msg.Truncated {
			// Loud, not tolerated: a truncated body means the oldest quoted
			// history was dropped, which is the material extraction depends on.
			r.Truncated++
		}
		res, err := Put(store, msg)
		if err != nil {
			return r, err
		}
		switch {
		case res.Created:
			r.Created++
		case res.Changed:
			r.Changed++
		}
	}

	n, err := store.ResolveParents()
	if err != nil {
		return r, err
	}
	r.Resolved = n
	return r, nil
}

// IngestIDs ingests specific message ids. Used for targeted top-ups and for
// checking the header-derived graph against a known trail.
func IngestIDs(store *corpus.Store, c Client, ids []string) (Result, error) {
	var r Result
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		msg, err := c.Read(id)
		if err != nil {
			return r, fmt.Errorf("reading %s: %w", id, err)
		}
		r.Seen++
		if msg.Truncated {
			r.Truncated++
		}
		res, err := Put(store, msg)
		if err != nil {
			return r, err
		}
		switch {
		case res.Created:
			r.Created++
		case res.Changed:
			r.Changed++
		}
	}
	n, err := store.ResolveParents()
	if err != nil {
		return r, err
	}
	r.Resolved = n
	return r, nil
}

// Put converts one docket message into an entry and stores it.
func Put(store *corpus.Store, msg Message) (corpus.PutResult, error) {
	ts, tz := parseDate(msg.Date)

	// A message with no Message-ID is unusual but legal; fall back to the Gmail
	// id so the entry still has a stable natural key.
	ext := msg.MessageID
	if ext == "" {
		ext = "gmail:" + msg.ID
	}

	person, err := resolvePerson(store, msg.From)
	if err != nil {
		return corpus.PutResult{}, err
	}

	e := corpus.Entry{
		Source:    corpus.SourceMail,
		ExtID:     "mail:" + ext,
		Kind:      "message",
		TS:        ts,
		TZ:        tz,
		PersonID:  person,
		Container: msg.ThreadID,
		ParentRef: msg.InReplyTo,
		Subject:   cleanSubject(msg.Subject),
		BodyText:  msg.Body,
		BodyHTML:  "", // filled by the spec generator, which owns presentation
		Permalink: "https://mail.google.com/mail/u/0/#all/" + msg.ID,
	}

	m := &corpus.Mail{
		GmailID:    msg.ID,
		MessageID:  msg.MessageID,
		InReplyTo:  msg.InReplyTo,
		References: msg.References,
		From:       msg.From,
		To:         msg.To,
		Cc:         msg.Cc,
		Labels:     msg.Labels,
	}

	var atts []corpus.Attachment
	for _, a := range msg.Attachments {
		atts = append(atts, corpus.Attachment{
			Name: a.Filename, Mime: a.MimeType, Size: a.Size,
			Permalink: e.Permalink, SourceRef: a.PartID,
		})
	}

	res, err := store.Put(e, m, atts)
	if err != nil {
		return res, err
	}
	// A message read straight from the mailbox was seen directly, as opposed to
	// recovered from someone's quoted history.
	if err := store.Sight(res.ID, 0, "direct", ""); err != nil {
		return res, err
	}
	return res, nil
}

// resolvePerson finds or creates the person behind a From header, keyed on the
// address. Display names vary ("Tom", "Bo Vantel", "tom"); the address does
// not, so it is the identity that gets stored.
func resolvePerson(store *corpus.Store, from string) (int64, error) {
	addr, name := splitAddress(from)
	if addr == "" {
		return 0, nil
	}
	db := store.DB()

	var id int64
	err := db.QueryRow(
		`select person_id from identities where kind='email' and value=?`, addr).Scan(&id)
	if err == nil {
		return id, nil
	}

	res, err := db.Exec(`insert into people (display_name) values (?)`, orElse(name, addr))
	if err != nil {
		return 0, fmt.Errorf("creating person for %s: %w", addr, err)
	}
	id, err = res.LastInsertId()
	if err != nil {
		return 0, err
	}
	if _, err := db.Exec(
		`insert into identities (person_id, kind, value, rule) values (?,?,?,?)`,
		id, "email", addr, "from-header"); err != nil {
		return 0, fmt.Errorf("recording identity %s: %w", addr, err)
	}
	return id, nil
}

// splitAddress pulls the address and display name out of a From/To header.
func splitAddress(s string) (addr, name string) {
	if a, err := mail.ParseAddress(strings.TrimSpace(s)); err == nil {
		return strings.ToLower(a.Address), a.Name
	}
	// Headers in the wild are not always parseable; fall back to the bracketed
	// form, then to the whole string if it looks like a bare address.
	if i, j := strings.Index(s, "<"), strings.LastIndex(s, ">"); i >= 0 && j > i {
		return strings.ToLower(strings.TrimSpace(s[i+1 : j])), strings.TrimSpace(s[:i])
	}
	if strings.Contains(s, "@") {
		return strings.ToLower(strings.TrimSpace(s)), ""
	}
	return "", strings.TrimSpace(s)
}

var rePrefix = regexp.MustCompile(`(?i)^((re|fw|fwd|aw|sv|vs)\s*:\s*)+`)

// cleanSubject strips Re:/Fwd: chains. They describe the forward, not the
// thread, and a chain is named by its subject.
func cleanSubject(s string) string {
	return strings.TrimSpace(rePrefix.ReplaceAllString(strings.TrimSpace(s), ""))
}

// parseDate reads an RFC5322 Date header, returning the instant and the zone
// label as stated. The label is kept because the renderer shows the sender's own
// zone and marks inferred ones differently; the instant is what ordering uses.
func parseDate(s string) (time.Time, string) {
	t, err := mail.ParseDate(s)
	if err != nil {
		return time.Time{}, ""
	}
	name, _ := t.Zone()
	// Go reports an unnamed zone as e.g. "+1000"; keep that, it is still what the
	// source said.
	if name == "" {
		name = t.Format("-0700")
	}
	return t, name
}

func orElse(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
