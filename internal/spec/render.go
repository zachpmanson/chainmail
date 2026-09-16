package spec

import (
	"fmt"

	"github.com/zachpmanson/chainmail/internal/corpus"
)

// RenderBodies renders the named entries' bodies as the HTML a page build would
// put in them, keyed by ext id. An id the corpus does not hold is absent from the
// result rather than an error: a caller rendering a trail is rendering what it
// found, and refusing the whole trail over one missing entry would lose the
// messages it does have.
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
// It loads through the same `load` a build does, and attributes inline images
// before converting a body for the same reason a build does: the pass decides
// which message placed a cid image, and a chip under the wrong message is a claim
// about who sent what. Same rows, same order, same function — so the HTML here is
// the HTML there, which is the whole point.
func RenderBodies(store *corpus.Store, extIDs []string) (map[string]string, error) {
	out := make(map[string]string, len(extIDs))
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
	attributeAttachments(rows)
	for _, r := range rows {
		out[extOf[r.ID]] = bodyHTML(r)
	}
	return out, nil
}
