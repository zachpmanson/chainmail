package mailingest

import (
	"fmt"
	"net/mail"
	"regexp"
	"strings"
	"time"

	"github.com/zachpmanson/chainmail/internal/corpus"
)

// Stop says why a walk ended. The distinction the corpus depends on is
// Covered() — a walk that stopped because the caller bounded it has left mail
// behind, and reporting that as a finished ingest is how a corpus comes to be
// trusted for coverage it does not have.
type Stop string

const (
	// StopExhausted: docket reported no further page. The query is fully read.
	StopExhausted Stop = "exhausted"
	// StopFrontier: the walk reached ground a previous completed walk covered.
	StopFrontier Stop = "frontier"
	// StopMax: the caller's Bound.Max ran out. MESSAGES REMAIN UNREAD.
	StopMax Stop = "max"
)

// Covered reports whether everything matching the query is now in the corpus.
// False means the walk stopped short and the query must be re-run to finish.
func (s Stop) Covered() bool { return s == StopExhausted || s == StopFrontier }

// Bound is what the caller will allow one walk to spend.
type Bound struct {
	// Max messages to walk. Zero is unbounded, which is the right setting for a
	// backfill: a bound exists to cap a run's cost, and any bound that stops a
	// walk early is reported as StopMax rather than absorbed.
	Max int
	// PageSize per docket request. Zero uses defaultPageSize.
	PageSize int
}

// defaultPageSize is docket's own cap. A smaller page is not cheaper — the cost
// is one API round trip per page either way — so paging at the cap minimises
// round trips for a backfill of any size.
const defaultPageSize = 500

// frontierLookback overlaps the floor a completed walk established.
//
// Paging runs in the mailbox's receipt order, but the only timestamp an envelope
// carries is the sender's Date header, and the two disagree whenever mail was
// delayed or a sender's clock was wrong. A floor set exactly at the frontier
// would step over such a message permanently. Re-reading a message is free —
// body_sha makes the write a no-op — so the overlap costs a few reads and buys
// the guarantee that the floor is a floor.
const frontierLookback = 48 * time.Hour

// Result summarises one ingest run.
type Result struct {
	Seen      int
	Created   int
	Changed   int
	Resolved  int64 // parent edges linked after this batch
	Truncated int   // bodies docket still had to cut — should always be zero
	// Stop is why the walk ended. Read it, not Seen: a run that saw exactly its
	// limit looks identical to a run that saw everything.
	Stop Stop
	// Pages is docket requests made, so a walk that paged can be told from one
	// that took a single page.
	Pages int
	// Resumed is true when this run continued a cursor left by an earlier one.
	Resumed bool
	// NextPage is the token a StopMax run left behind, and is what the stored
	// cursor holds. Empty for a covered walk.
	NextPage string
}

// Mailbox is the part of docket an ingest uses. An interface so the walk can be
// driven by a fake: paging and bound behaviour are the logic worth testing, and
// they are untestable against a binary that talks to Gmail.
type Mailbox interface {
	Search(query string, limit int, pageToken string) ([]Envelope, Page, error)
	Read(id string) (Message, error)
}

// Ingest walks every page of a Gmail query, reads each message in full, and
// upserts it, recording progress against a cursor keyed on the query.
//
// Restartable rather than duplicate-avoiding: body_sha already makes a re-read
// harmless, so the cursor exists to answer "where had we got to", not "have we
// seen this". Two things follow. A run killed between pages resumes at the page
// it had reached. A run over a query whose last walk completed stops as soon as
// it reaches that walk's frontier, so a top-up reads the new mail and not the
// archive behind it.
func Ingest(store *corpus.Store, c Mailbox, query string, b Bound) (Result, error) {
	var r Result
	if b.PageSize <= 0 {
		b.PageSize = defaultPageSize
	}

	cur, err := corpus.LoadCursor(store, corpus.SourceMail, query)
	if err != nil {
		return r, err
	}
	token := ""
	if cur.Exists && !cur.Complete && cur.Position != "" {
		token = cur.Position
		r.Resumed = true
	}
	var floor time.Time
	if !cur.Frontier.IsZero() {
		floor = cur.Frontier.Add(-frontierLookback)
	}
	newest := cur.Frontier

	r.Stop = StopExhausted
walk:
	for {
		size := b.PageSize
		if b.Max > 0 && b.Max-r.Seen < size {
			size = b.Max - r.Seen
		}
		envs, page, err := c.Search(query, size, token)
		if err != nil {
			// A page token is server-side state and can expire. A cursor that
			// cannot be resumed must not wedge the query forever, so one restart
			// from the beginning is preferable to a permanently failing ingest —
			// re-reading is free, and the frontier still bounds the walk.
			if token == "" || r.Pages > 0 {
				return r, err
			}
			token, r.Resumed = "", false
			if err := corpus.SaveProgress(store, corpus.SourceMail, query, "", cur.Walked); err != nil {
				return r, err
			}
			envs, page, err = c.Search(query, size, "")
			if err != nil {
				return r, err
			}
		}
		r.Pages++

		for _, env := range envs {
			ts, _, _ := parseDate(env.Date)
			if !floor.IsZero() && !ts.IsZero() && !ts.After(floor) {
				r.Stop = StopFrontier
				break walk
			}
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
			if ts.After(newest) {
				newest = ts
			}
		}

		if !page.HasMore {
			break
		}
		// docket promises a token with every has_more. Without one there is no
		// way to continue, and calling that "exhausted" would record coverage of
		// a set we demonstrably did not finish.
		if page.NextPageToken == "" {
			return r, fmt.Errorf("docket reported more results for %q but sent no page token", query)
		}
		token = page.NextPageToken
		if b.Max > 0 && r.Seen >= b.Max {
			r.Stop, r.NextPage = StopMax, token
			break
		}
		// Between pages, not after: this write is what a kill resumes from.
		if err := corpus.SaveProgress(store, corpus.SourceMail, query, token, cur.Walked+r.Seen); err != nil {
			return r, err
		}
	}

	if r.Stop.Covered() {
		if err := corpus.SaveComplete(store, corpus.SourceMail, query, newest, 0); err != nil {
			return r, err
		}
	} else if err := corpus.SaveProgress(store, corpus.SourceMail, query, r.NextPage, cur.Walked+r.Seen); err != nil {
		return r, err
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
func IngestIDs(store *corpus.Store, c Mailbox, ids []string) (Result, error) {
	// An explicit list is its own bound: there is no page after the last id, so
	// the walk is complete by construction. No cursor either — a list of ids is
	// not a container anything could later top up.
	r := Result{Stop: StopExhausted}
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
		BodyHTML:  msg.BodyHTML,
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
