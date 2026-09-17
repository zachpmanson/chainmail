package spec

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/zachpmanson/chainmail/internal/corpus"
)

// Rendered is one entry as a view that is not a page needs it: the body as HTML a
// page build would produce, the recipient line it would print under the bubble,
// whether the reader wrote it, the address it came from, and — where it has no
// address of its own — the person it was recovered from.
//
// One struct rather than a call per field, because they come out of the same
// load. A caller that asked for the HTML and then had to ask again for the
// recipients would pay twice for the rows, the host documents and the attachment
// attribution that decide both — and the second answer could disagree with the
// first.
type Rendered struct {
	// HTML is the body as rendered for reading, already sanitised.
	HTML string
	// To is the "to …" line: the recipients as the message stated them, names
	// deduplicated, cc marked, and empty where the entry stated none — which is
	// every recovered entry, since it has no headers of its own. Empty renders as
	// unknown, and nothing may guess at it.
	To string
	// Mine is whether the reader wrote this, resolved by the same rule a page
	// build applies (me.go): the corpus's own idea of which human the addresses
	// the reader named belong to, so a message sent from another address of
	// theirs is theirs, and a recovered entry — which has no From header for a
	// list of addresses to match — is theirs too. False for a message that is not
	// the reader's.
	//
	// The mark itself is the page's, and deliberately: the pane draws the same
	// Message component and passes this as `me`, which is the class the
	// stylesheet already tints (`.msg.me .bub`). A second vocabulary for one fact
	// — a pane-only border, say — would be a second thing to keep in step with
	// what "sent by you" means.
	Mine bool
	// FromEmail is the address the entry was sent from, lowercased, as the page's
	// own Entry carries it. Empty where the entry has no From header of its own —
	// a message recovered from someone else's quote — and empty is the answer: the
	// pane hangs it on the sender's name so a reader can see who they are actually
	// reading, and naming the wrong address would be worse than naming none. What
	// the pane says in its place is QuotedBy.
	FromEmail string
	// Org is the sender's organisation, resolved by the same function a page build
	// uses, so the pane's colours and the page's cannot disagree about one sender.
	// Empty where nothing established one, which is drawn as the unknown slot.
	Org string
	// QuotedBy names the person whose message this entry was recovered from,
	// written the way the pane writes a person on hover ("Ada Okoye
	// <ada@loomworks.example>"), with several of them joined by ", ".
	//
	// It answers the question FromEmail raises and cannot answer. An empty address
	// on every other bubble in a thread is a fact the reader can see, and it reads
	// like a failure rather than like a message recovered from someone's quote; the
	// quoter is where that entry actually came from, and it is evidence the corpus
	// holds rather than a guess. What it is not is the sender's address: the corpus
	// resolves people by address, so an entry whose author was matched by name
	// alone would otherwise be handed a stranger's address, which is the one claim
	// this hover exists to avoid. Empty for an entry that has an address of its
	// own, which has nothing to explain away.
	QuotedBy string
	// Edits are a quoter's in-place changes to a message this one quoted (issue
	// #42): the same relation a page build draws inline inside the quoting bubble,
	// carried here so the reading pane can draw it too rather than floating the
	// derived copy as its own node. `id` and `base` are ext ids — the names the
	// pane's own entries carry — and the renderer resolves them against the thread
	// it holds, exactly as a page resolves a spec id against its rows.
	//
	// Who and when are left empty: the pane draws the host's own clock and name on
	// its bubble from the same stampOf and author, and stating them again here
	// would be a second answer to a question the bubble already answers. A page
	// build fills both in because a spec entry has no other place to keep them.
	Edits []Edit
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
//
// The org rules are read here rather than passed in: there is one answer to what
// a domain is — the reader's, or the domain's own name — and reading it once per
// trail is what keeps the colours inside one trail consistent with each other. A
// page build reads the same rules the same way (see Generate), so the pane and
// the page cannot colour one sender two ways.
//
// A quoter's edit to a quoted message is attached here too, through the same
// function a page build uses (derivedEdits), so a thread read in the pane carries
// the same edits the same page does.
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
	idOf := make(map[string]int64, len(extIDs))
	for q.Next() {
		var id int64
		var ext string
		if err := q.Scan(&id, &ext); err != nil {
			return nil, err
		}
		ids = append(ids, id)
		extOf[id] = ext
		idOf[ext] = id
	}
	if err := q.Err(); err != nil {
		return nil, err
	}

	rows, err := load(store, ids)
	if err != nil {
		return nil, err
	}
	orgs, err := store.OrgRules()
	if err != nil {
		return nil, fmt.Errorf("reading the org rules: %w", err)
	}
	// The recipient line comes from the same place a page build's does, which for
	// an entry with no headers of its own means the participants table. See
	// recipientsOf. The addresses it loads back are also what a recovered entry's
	// org is resolved from, so this one call answers both.
	part, addrs, err := loadParticipation(store.DB(), ids)
	if err != nil {
		return nil, err
	}
	// The org rules resolve in the order the caller asked for the entries in,
	// because that is the trail's own order: a person who appears at two
	// organisations is coloured at whichever the trail reached first, exactly as a
	// page build colours them at whichever its own order reached first.
	resolver := newOrgResolver(orgs, addrs)
	byID := make(map[int64]*entryRow, len(rows))
	for _, r := range rows {
		byID[r.ID] = r
	}
	for _, ext := range extIDs {
		r, ok := byID[idOf[ext]]
		if !ok || r.PersonID == 0 {
			continue
		}
		resolver.note(r.PersonID, parseAddr(r.From).Address)
	}
	attributeAttachments(rows)
	quoters, err := loadQuoters(db, rows)
	if err != nil {
		return nil, err
	}
	// The reader's own addresses are read here, from the stored setting, rather
	// than taken as an argument. The setting is where the reader said who they
	// are — the pane has no surface of its own for saying it — and reading it here
	// keeps this call the same shape for every caller, which is the point of the
	// trail render existing at all. A caller resolving the addresses itself would
	// need a second copy of the rule, and the two would answer differently the
	// first time one of them was edited.
	addresses, err := store.MeAddresses()
	if err != nil {
		return nil, fmt.Errorf("reading the reader's addresses: %w", err)
	}
	me, err := newMeSet(store, addresses)
	if err != nil {
		return nil, err
	}
	// A quoter's edit is pinned to the message that carries it before the entries
	// are drawn, through the same function a page build uses: the relation is the
	// corpus's, and the two renderers have to reach the same verdict about which
	// bubble holds an edit. The ids on the wire are ext ids because that is what
	// this read's own entries carry; the base is resolved by the client against
	// the entries it was handed, exactly as a page resolves a spec id.
	edited := map[int64][]Edit{}
	for _, d := range derivedEdits(rows) {
		edited[d.Host.ID] = append(edited[d.Host.ID], Edit{
			ID:   extOf[d.Copy.ID],
			Base: extOf[d.Base.ID],
			Body: d.Copy.BodyText,
		})
	}
	for _, r := range rows {
		// The address is parsed once and used for every fact it carries: a page
		// build reads the same expression for the same ones, and asking for it
		// repeatedly is how the mark, the hover and the colour would come to name
		// different addresses.
		from := parseAddr(r.From)
		out[extOf[r.ID]] = Rendered{
			HTML:      bodyHTML(r),
			To:        recipientsOf(r, part[r.ID]),
			Mine:      me.wrote(r.PersonID, from.Address),
			FromEmail: from.Address,
			Org:       resolver.org(r.PersonID, from.Address),
			QuotedBy:  quoters[r.ID],
			Edits:     edited[r.ID],
		}
	}
	return out, nil
}

// loadQuoters names the messages each recovered entry was found inside, keyed by
// the entry's own id.
//
// A host is usually outside the trail being rendered: the pane draws one entry,
// or one reply chain, and the message that quoted this one sits beside or above
// it rather than in it — so this is its own query over the host ids rather than a
// join against the rows already loaded. loadHostHTML asks the same question about
// the same ids for the same reason, and the two are not folded together because
// each wants a handful of columns the other has no use for: a host with no markup
// to give is still a host that can be named, and vice versa.
//
// Only entries with no address of their own are asked about. A direct entry has
// its own From header, so a quoter named beside it would be a second answer to a
// question already answered.
func loadQuoters(db *sql.DB, rows []*entryRow) (map[int64]string, error) {
	var recovered []*entryRow
	want := map[int64]bool{}
	for _, r := range rows {
		if parseAddr(r.From).Address != "" {
			continue
		}
		recovered = append(recovered, r)
		for _, id := range r.SeenIn {
			want[id] = true
		}
	}
	if len(want) == 0 {
		return nil, nil
	}
	ph, args := placeholders(keys(want))
	q, err := db.Query(`select e.id, coalesce(p.display_name, ''), coalesce(d.from_addr, '')
		from entries e
		left join people p      on p.id = e.person_id
		left join mail_detail d on d.entry_id = e.id
		where e.id in (`+ph+`)`, args...)
	if err != nil {
		return nil, fmt.Errorf("loading quoters: %w", err)
	}
	defer q.Close()
	who := map[int64]string{}
	for q.Next() {
		var id int64
		var name, from string
		if err := q.Scan(&id, &name, &from); err != nil {
			return nil, err
		}
		who[id] = quoterTitle(name, parseAddr(from).Address)
	}
	if err := q.Err(); err != nil {
		return nil, err
	}

	out := make(map[int64]string, len(recovered))
	for _, r := range recovered {
		seen := map[int64]bool{}
		var names []string
		for _, id := range r.SeenIn {
			// A sighting is keyed by (entry, host, kind), so a host that both quoted
			// and forwarded this entry arrives twice. It is one person, and a hover
			// that named them twice would read as two of them — the same reason
			// builder.source drops the repeat from the page's provenance line.
			if seen[id] {
				continue
			}
			seen[id] = true
			// A host the corpus holds no person or From header for names nobody, so
			// it contributes nothing rather than an empty slot in the list.
			if who[id] != "" {
				names = append(names, who[id])
			}
		}
		if len(names) > 0 {
			out[r.ID] = strings.Join(names, ", ")
		}
	}
	return out, nil
}

// quoterTitle names a quoter the way the pane names a person on hover: the
// corpus person's display name, and the address their mail came from, e.g.
// "Ada Okoye <ada@loomworks.example>".
//
// Either half may be missing and the other then stands alone. That is the same
// fallback ChainMessages.tsx's senderTitle makes, and deliberately so: the pane
// names a quoter and a sender through one expression, and a reader told two
// different things about the same person by two hovers would have no way to tell
// which one to believe. Neither half is filled in from the other source — the
// name comes from the corpus person, the address from the host's own From header
// — because a name guessed into an address is exactly the wrong address this
// hover is written to avoid.
func quoterTitle(name, address string) string {
	switch {
	case name == "":
		return address
	case address == "":
		return name
	}
	return name + " <" + address + ">"
}
