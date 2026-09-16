package spec

import (
	"fmt"

	"github.com/zachpmanson/chainmail/internal/corpus"
)

// Rendered is one entry as a view that is not a page needs it: the body as HTML a
// page build would produce, the recipient line it would print under the bubble,
// and the address it came from.
//
// Three fields rather than one because they come out of the same load. A caller
// that asked for the HTML and then had to ask again for the recipients would pay
// twice for the rows, the host documents and the attachment attribution that
// decide both — and the second answer could disagree with the first.
type Rendered struct {
	// HTML is the body as rendered for reading, already sanitised.
	HTML string
	// To is the "to …" line: the recipients as the message stated them, names
	// deduplicated, cc marked, and empty where the entry stated none — which is
	// every recovered entry, since it has no headers of its own. Empty renders as
	// unknown, and nothing may guess at it.
	To string
	// FromEmail is the address the entry was sent from, lowercased, as the page's
	// own Entry carries it. Empty where the entry has no From header of its own —
	// a message recovered from someone else's quote — and empty is the answer: the
	// pane hangs it on the sender's name so a reader can see who they are actually
	// reading, and naming the wrong address would be worse than naming none.
	FromEmail string
}

// RenderTrail renders the named entries as the entries a page build would draw,
// keyed by ext id. An id the corpus does not hold is absent from the result
// rather than an error: a caller rendering a trail is rendering what it found,
// and refusing the whole trail over one missing entry would lose the messages it
// does have.
//
// This exists so a view that shows messages without building a page — the inbox's
// reading pane — can draw the same bubbles a page draws, from the same
// conversion, instead of growing a second renderer that drifts away from this
// one. What it leaves out is the page: no zone inference (a corpus-wide pass over
// every placement the corpus knows), no participation, no lanes, no minimap, no
// attachment preview budget. Those are what make a build take seconds and none of
// them are properties of a message; rendering a body is per-entry work, and a
// caller pays for the entries it asked about.
//
// The recipient line is made here rather than in the corpus because it is a
// presentation decision — names rather than addresses, cc folded in, duplicates
// dropped — and this is where the page makes it (recipientLine). The corpus keeps
// the header text as it arrived, which is the only thing it can honestly keep.
//
// It loads through the same `load` a build does, and attributes inline images
// before converting a body for the same reason a build does: the pass decides
// which message placed a cid image, and a chip under the wrong message is a claim
// about who sent what. Same rows, same order, same function — so the HTML here is
// the HTML there, which is the whole point.
func RenderTrail(store *corpus.Store, extIDs []string) (map[string]Rendered, error) {
	out := make(map[string]Rendered, len(extIDs))
	if len(extIDs) == 0 {
		return out, nil
	}
	db := store.DB()
	ph, args := placeholders(extIDs)
	q, err := db.Query(`select id, ext_id from entries where ext_id in (`+ph+`)`, args...)
	if err != nil {
		return nil, fmt.Errorf("resolving entries to render: %w", err)
	}
	defer q.Close()

	ids := make([]int64, 0, len(extIDs))
	extOf := make(map[int64]string, len(extIDs))
	for q.Next() {
		var id int64
		var ext string
		if err := q.Scan(&id, &ext); err != nil {
			return nil, err
		}
		ids = append(ids, id)
		extOf[id] = ext
	}
	if err := q.Err(); err != nil {
		return nil, err
	}

	rows, err := load(store, ids)
	if err != nil {
		return nil, err
	}
	// The recipient line comes from the same place a page build's does, which for
	// an entry with no headers of its own means the participants table. See
	// recipientsOf.
	part, _, err := loadParticipation(store.DB(), ids)
	if err != nil {
		return nil, err
	}
	attributeAttachments(rows)
	for _, r := range rows {
		out[extOf[r.ID]] = Rendered{
			HTML:      bodyHTML(r),
			To:        recipientsOf(r, part[r.ID]),
			FromEmail: parseAddr(r.From).Address,
		}
	}
	return out, nil
}
