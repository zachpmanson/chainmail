package mailingest

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/zachpmanson/chainmail/internal/corpus"
	"github.com/zachpmanson/chainmail/internal/unnest"
)

// QuotedResult summarises one body's extraction.
type QuotedResult struct {
	Blocks   int // sentinel-bearing blocks found
	Distinct int // after dedup within this body
	Created  int // new entries
	Merged   int // matched an entry already present
	Twinned  int // matched a stored copy of the same message under a different clock
	Derived  int // a modified copy of a stored message, not a twin and not unrelated
	Enriched int // merged entries that gained a field from this sighting
	Inline   int // inline replies typed into a quoted block, stored as messages of their own
	Edges    int // reply edges linked from positional nesting
	Undated  int // blocks whose time had to be inferred from the host
}

// ExtractQuoted stores the messages quoted inside one host message.
//
// This is where a mailbox becomes a conversation. On one real trail, 13 mailbox
// messages contained 45 further messages that exist nowhere else — so skipping
// this step understates what is known by more than three to one.
//
// The first block is the host's own text and is skipped: it was already stored
// as the host entry.
func ExtractQuoted(store *corpus.Store, hostID int64, host corpus.Entry, body string) (QuotedResult, error) {
	var r QuotedResult

	// Skip only blocks with no sentinel: those are the host's own words, already
	// stored as the host entry. Position is NOT the test — a body that opens
	// directly with a quote (Forward pressed, nothing typed) has its first block
	// carrying a sentinel, and skipping by index would discard the entire
	// forwarded message.
	var rs []unnest.Recovered
	for _, b := range unnest.Peel(body) {
		if b.Sentinel == "" {
			continue
		}
		rs = append(rs, unnest.Parse(b))
	}
	r.Blocks = len(rs)
	rs = unnest.Dedup(rs)
	r.Distinct = len(rs)

	// An inline reply is a run the host coloured inside a quoted message — the
	// quoter's answer to a question they quoted, rather than another quote of the
	// original. It is a live reply edge to the quoted message, so it must be its
	// own entry with a parent to the message it answered, not text silently
	// attributed to the quoted author. The colour signal lives only in the host's
	// text/html part; the flattened text this body is peeled from loses it, so
	// the runs are recovered here and matched back onto the blocks.
	inlines := []unnest.InlineRun{}
	if host.BodyHTML != "" {
		inlines = unnest.InlineRuns(host.BodyHTML)
	}

	ids := make([]int64, len(rs))
	for i, rec := range rs {
		// Remove the host's inline replies from this quoted block first, so the
		// block that remains is the quoted original and the answers are stored
		// separately below. A run inside a quoted block is an interjection; a run
		// at the top of the body is the host's own words and never reaches here
		// (the first block is skipped above).
		var answers []string
		if len(inlines) > 0 {
			var kept string
			kept, answers = stripInlines(rec.Block.Text, inlines)
			if len(answers) > 0 {
				rec.Block.Text = kept
			}
		}

		person, err := resolveRecovered(store, rec)
		if err != nil {
			return r, err
		}

		ts, inferred := rec.Sent, false
		if ts.IsZero() {
			// A block with no stated time still happened before the message that
			// quoted it. The host's timestamp is a true upper bound, and recording
			// that is better than dropping a real message — but it is marked, so a
			// reader never mistakes it for a stated time.
			ts, inferred = host.TS, true
			r.Undated++
		}

		e := corpus.Entry{
			Source:    corpus.SourceMail,
			ExtID:     rec.Key,
			Kind:      "message",
			TS:        ts,
			TZ:        rec.TZ,
			PersonID:  person,
			Container: host.Container,
			Subject:   cleanSubject(rec.Subject),
			BodyText:  rec.Block.Text,
			// No permalink: a quoted message has no URL of its own. The host's
			// permalink belongs to the host, and reusing it would send a reader to
			// the wrong message.
		}
		// A message already in the corpus is not stored again. Its ext_id only
		// collapses onto this block's key when the quoting client wrote a
		// Message-ID into the sentinel, which almost none do, so the copies are
		// reconciled by their clocks instead — see corpus.FindTwin. Only a stated
		// clock qualifies: an inferred one is the host's instant, and the gap to
		// the copy would be an artefact of that substitution.
		var twin int64
		var derived corpus.DerivedMatch
		if !inferred {
			// The subject is the thread that vouches for a short block: without it
			// the same few words on two threads would collapse into one message.
			t, ok, err := corpus.FindTwin(store, person, ts, rec.Block.Text, rec.Subject)
			if err != nil {
				return r, err
			}
			if ok {
				twin = t
			} else {
				// Not a twin; ask whether it is the SAME message line, edited.
				d, ok2, err := corpus.FindDerived(store, person, ts, rec.Block.Text, rec.Subject)
				if err != nil {
					return r, err
				}
				if ok2 {
					derived = d
				}
			}
		}
		id, created := twin, false
		if twin == 0 {
			id, created, err = store.PutQuoted(e)
			if err != nil {
				return r, fmt.Errorf("storing quoted block %d of %s: %w", i, host.ExtID, err)
			}
			if derived.Base != 0 {
				r.Derived++
				// The modified quote's parent is the message it was edited INSIDE,
				// not the host: it is what the quoter was answering back. SetParent
				// only fills a NULL parent, so this survives the positional nesting
				// below rather than being clobbered by a structural guess.
				if err := store.SetParent(id, derived.Base); err != nil {
					return r, fmt.Errorf("linking modified block %d to its base %d: %w",
						id, derived.Base, err)
				}
			}
		} else {
			r.Twinned++
		}
		ids[i] = id
		if created {
			r.Created++
		} else {
			// A later sighting of the same message may know things the first did
			// not: one client quotes a full header block with Subject and
			// recipients, the next re-quotes it as a bare "On ... wrote:" with
			// neither. Whichever arrived first must not decide what is known.
			if err := store.EnrichQuoted(id, e); err != nil {
				return r, fmt.Errorf("enriching %s: %w", rec.Key, err)
			}
			r.Merged++
			r.Enriched++
		}

		detail := fmt.Sprintf("depth %d", rec.Block.Depth)
		if inferred {
			detail += ", time inferred from host"
		}
		if err := store.Sight(id, hostID, "quoted", detail); err != nil {
			return r, err
		}
		if person != 0 {
			if err := corpus.Participate(store, id, person, corpus.RoleFrom); err != nil {
				return r, err
			}
		}
		// Recipients, where the quoting client wrote them. Only a header block
		// carries these; an attribution never does.
		for _, h := range []struct{ role, header string }{
			{corpus.RoleTo, rec.To}, {corpus.RoleCc, rec.Cc},
		} {
			if h.header == "" {
				continue
			}
			// Additive, not wholesale: each forward shows a different subset of
			// the recipients, so replacing the role would let the narrowest copy
			// seen last decide who was involved. The union is the better answer.
			if _, err := corpus.AddHeader(store, id, h.role, h.header); err != nil {
				return r, err
			}
		}

		// Each inline answer is a message of its own: the host typed it into a
		// reply to the block above, so it is a child of that block (id) — the
		// answer to the very message it sits inside. It carries the host's
		// identity and clock, not the quoted author's.
		for _, answer := range answers {
			if strings.TrimSpace(answer) == "" {
				continue
			}
			okID, _, err := store.PutQuoted(corpus.Entry{
				Source:    corpus.SourceMail,
				ExtID:     "inline:" + corpus.BodySHA(host.ExtID, answer),
				Kind:      "message",
				TS:        host.TS,
				TZ:        host.TZ,
				PersonID:  host.PersonID,
				Container: host.Container,
				Subject:   cleanSubject(rec.Subject),
				BodyText:  answer,
			})
			if err != nil {
				return r, fmt.Errorf("storing inline reply %q of %s: %w", answer, host.ExtID, err)
			}
			if err := store.Sight(okID, hostID, "quoted", "inline reply"); err != nil {
				return r, err
			}
			// The answer's parent is the message it was stitched into — the
			// block's resolved id, the quoted original or its base.
			if err := store.SetParent(okID, id); err != nil {
				return r, err
			}
			if host.PersonID != 0 {
				if err := corpus.Participate(store, okID, host.PersonID, corpus.RoleFrom); err != nil {
					return r, err
				}
			}
			r.Inline++
		}
	}

	// Positional nesting IS the reply graph, and its direction is fixed: you quote
	// what you are replying TO. Blocks arrive outermost (newest) first, so the
	// host replies to ids[0], ids[0] replies to ids[1], and the deepest block has
	// no parent here — whatever it replied to was not quoted in this body.
	//
	// SetParent only fills a NULL parent, so a header-derived edge on the host
	// always wins over this structural guess. In-Reply-To is authoritative;
	// nesting is the fallback for the messages that have no headers at all.
	for i, id := range ids {
		child := hostID
		if i > 0 {
			child = ids[i-1]
		}
		if err := store.SetParent(child, id); err != nil {
			return r, err
		}
		r.Edges++
	}
	return r, nil
}

// resolveRecovered finds the person behind a recovered block. An address is a
// real identity; a bare display name is not, but is still worth a person so the
// message is attributed to someone rather than to nobody.
func resolveRecovered(store *corpus.Store, rec unnest.Recovered) (int64, error) {
	if rec.Address != "" {
		a, ok := corpus.ParseAddress(rec.Address)
		if ok {
			if rec.Sender != "" {
				a.Name = rec.Sender
			}
			id, err := corpus.ResolveAddress(store, a, "quote:attribution")
			if err == nil {
				return id, nil
			}
		}
	}
	if rec.Sender != "" {
		id, err := corpus.Resolve(store, "display_name", rec.Sender, rec.Sender)
		if err == nil {
			return id, nil
		}
	}
	return 0, nil
}

// stripInlines removes a host's inline replies from a quoted block's text,
// returning the reduced original and the answers that were taken out.
//
// A run is matched when its words appear contiguously in the block (whitespace
// collapsed, so a rewrap or client-added divider does not hide it), and any
// leading "– " the quoter typed before the answer goes with it so the reduced
// block reads as the question it was. A block may hold several answers; each is
// found independently.
func stripInlines(text string, inlines []unnest.InlineRun) (string, []string) {
	var answers []string
	rest := text
	for _, run := range inlines {
		if run.Text == "" {
			continue
		}
		m := inlinePattern(run).FindStringIndex(rest)
		if m == nil {
			continue
		}
		answers = append(answers, run.Text)
		rest = rest[:m[0]] + rest[m[1]:]
	}
	return rest, answers
}

// inlinePattern is the whitespace-tolerating shape of one answer run, with the
// quoter's leading dash (the way an answer joins the line it answers) folded in.
func inlinePattern(run unnest.InlineRun) *regexp.Regexp {
	if re, ok := inlinePats[run.Text]; ok {
		return re
	}
	var words []string
	for _, w := range strings.Fields(run.Text) {
		words = append(words, regexp.QuoteMeta(w))
	}
	pat := `(?:[-—]\s+)?` + strings.Join(words, `\s+`)
	re := regexp.MustCompile(`(?i)` + pat)
	inlinePats[run.Text] = re
	return re
}

var inlinePats = map[string]*regexp.Regexp{}
