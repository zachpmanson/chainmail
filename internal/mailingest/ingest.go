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
	ts, tz, off := parseDate(msg.Date)

	// A message with no Message-ID is unusual but legal; fall back to the Gmail
	// id so the entry still has a stable natural key.
	ext := msg.MessageID
	if ext == "" {
		ext = "gmail:" + msg.ID
	}

	person, err := resolveSender(store, msg.From)
	if err != nil {
		return corpus.PutResult{}, err
	}

	e := corpus.Entry{
		Source:    corpus.SourceMail,
		ExtID:     "mail:" + ext,
		Kind:      "message",
		TS:        ts,
		TZ:        tz,
		TZOffset:  off,
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
	// Recipients, not just the author. Someone who only ever appears in To:/Cc:
	// used to be absent from the corpus entirely, which turned "routed to four
	// cc'd people" into "sent to nobody".
	if person != 0 {
		if err := corpus.Participate(store, res.ID, person, corpus.RoleFrom); err != nil {
			return res, err
		}
	}
	for _, h := range []struct{ role, header string }{
		{corpus.RoleTo, msg.To},
		{corpus.RoleCc, msg.Cc},
	} {
		if _, err := corpus.RecordHeader(store, res.ID, h.role, h.header); err != nil {
			return res, fmt.Errorf("%s header of %s: %w", h.role, ext, err)
		}
	}

	// The quoted history is the larger half of the conversation, so extraction is
	// part of ingest rather than a later pass: a corpus holding only mailbox
	// messages is a corpus of a different, smaller thing.
	e.ID = res.ID
	if _, err := ExtractQuoted(store, res.ID, e, msg.Body); err != nil {
		return res, fmt.Errorf("extracting quoted history of %s: %w", ext, err)
	}
	return res, nil
}

// resolveSender finds or creates the person behind a From header. Display names
// vary ("Tom", "Bo Vantel", "tom"); the address does not, so the address is
// the identity that gets stored — with the name carried along, since a corpus of
// bare addresses is unreadable. A From header with no parseable address still
// yields a person, keyed on the display name.
func resolveSender(store *corpus.Store, from string) (int64, error) {
	addrs := corpus.ParseAddresses(from)
	if len(addrs) == 0 {
		return 0, nil
	}
	// A From header holding several addresses is malformed; treat the first as
	// the author rather than inventing co-authors.
	id, err := corpus.ResolveAddress(store, addrs[0], "mail:from-header")
	if err != nil {
		// An unusable From is a message with no known author, not a failed
		// ingest: the body is still evidence.
		return 0, nil
	}
	return id, nil
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
// parseDate reads an RFC5322 Date header, returning the instant, the zone label
// as stated, and the offset in minutes east of UTC. The label is kept because the
// renderer shows the sender's own zone and marks inferred ones differently; the
// instant is what ordering uses; the offset is what lets the stated clock be
// rendered without a label lookup table.
func parseDate(s string) (time.Time, string, *int) {
	t, err := mail.ParseDate(s)
	if err != nil {
		return time.Time{}, "", nil
	}
	name, secs := t.Zone()
	if name == "" {
		name = t.Format("-0700")
	}
	mins := secs / 60
	return t, name, &mins
}
