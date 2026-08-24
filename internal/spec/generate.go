package spec

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/zachpmanson/chainmail/internal/corpus"
	"github.com/zachpmanson/chainmail/internal/tzinfer"
)

// Options says what to render and supplies the few facts the corpus cannot
// know. Nothing here ranks or searches: a caller (the search-driven selector)
// decides which entries matter and hands them over.
type Options struct {
	// Containers are mail thread ids (entries.container); every entry in them is
	// selected.
	Containers []string
	// EntryIDs are corpus row ids, as an FTS or vector query returns them.
	EntryIDs []int64
	// ExtIDs are corpus natural keys ("mail:<message-id>"), the identity that
	// survives a rebuild — prefer these when the selection is persisted.
	ExtIDs []string

	// Title is the page title. Defaults to the subject of the earliest selected
	// entry, since the schema requires one.
	Title string
	// RunLabel names this collection pass, e.g. "pass 2, 20 Aug 2026".
	RunLabel string
	// Queries record the searches the selection came from, so that a hole in the
	// timeline is interpretable.
	Queries []Query
	// Params records the flags this selection was made with, so a later refresh
	// reproduces the same page rather than a differently-shaped one.
	Params *RunParams

	// Me lists the reader's own addresses, so their outbound messages can be
	// marked. Nothing in the corpus knows which mailbox it was collected from.
	Me []string
	// Orgs maps a mail domain to an organisation label, overriding the label
	// derived from the domain itself.
	Orgs map[string]string

	// UploadDir is the archive's upload root, where the downloader that fed the
	// corpus kept attachment bytes. Set it to embed image thumbnails; leave it
	// empty and every attachment stays a chip. Only the Slack archive keeps
	// bytes today, so only Slack attachments can gain a preview.
	UploadDir string
}

// Generate builds a timeline spec from the corpus.
//
// It fills every field that follows mechanically from what was ingested, and
// leaves the interpretive ones — openItems, cross-links, subtitle, the editorial
// gloss — empty for a later pass. `body` carries the message's own text
// converted to presentation HTML (see body.go), never a summary of it: nothing
// here writes a word the sender did not.
func Generate(store *corpus.Store, opts Options) (Spec, error) {
	if store == nil {
		return Spec{}, errors.New("spec: nil store")
	}
	db := store.DB()

	seedIDs, err := seeds(db, opts)
	if err != nil {
		return Spec{}, err
	}
	if len(seedIDs) == 0 {
		return Spec{}, errors.New("spec: nothing selected — pass containers, entry ids or ext ids")
	}
	ids, err := closure(db, seedIDs)
	if err != nil {
		return Spec{}, err
	}
	rows, err := load(store, ids)
	if err != nil {
		return Spec{}, err
	}
	if len(rows) == 0 {
		// The schema requires at least one message, so an empty timeline is an
		// error rather than a spec nothing will load.
		return Spec{}, errors.New("spec: selection resolved to no entries")
	}

	inferred, zoneStats, err := inferZones(store, rows)
	if err != nil {
		return Spec{}, err
	}

	me := map[string]bool{}
	for _, a := range opts.Me {
		me[strings.ToLower(strings.TrimSpace(a))] = true
	}

	part, addrs, err := loadParticipation(db, ids)
	if err != nil {
		return Spec{}, err
	}

	b := &builder{
		opts:        opts,
		me:          me,
		zones:       inferred,
		zoneStats:   zoneStats,
		ids:         newIDAllocator(),
		idOf:        map[int64]string{},
		subjOf:      map[int64]string{},
		rowByID:     map[int64]*entryRow{},
		cast:        newCast(),
		part:        part,
		addrs:       addrs,
		badZones:    map[string]int{},
		zoneWhy:     map[string]string{},
		orgByPerson: map[int64]string{},
		prev:        &previewer{dir: opts.UploadDir},
	}
	for _, r := range rows {
		b.rowByID[r.ID] = r
		// An org is inferred from a sender's mail domain, and a quote-recovered
		// entry has no mail_detail row to take an address from — so 41 of 57
		// entries on a real page had no org at all, while the same people's
		// direct entries did. The person is known either way, so their org is
		// carried across from wherever it was resolvable.
		if r.PersonID != 0 {
			if _, ok := b.orgByPerson[r.PersonID]; !ok {
				if org := orgOf(parseAddr(r.From).Address, opts.Orgs); org != "" {
					b.orgByPerson[r.PersonID] = org
				}
			}
		}
	}
	for _, r := range rows {
		b.add(r)
	}
	// A quoter's in-place change to a quoted message (a DERIVED copy) is drawn
	// as an edit inside the message that quoted it, never left floating as its
	// own node. The relation was decided at ingest; this only surfaces it.
	b.attachEdits(rows)

	spec := Spec{
		SpecVersion:  1,
		Title:        opts.Title,
		RunLabel:     opts.RunLabel,
		RunParams:    opts.Params,
		Queries:      opts.Queries,
		Participants: b.cast.people(),
		Threads:      b.threads(rows),
		SourceNotes:  b.notes(rows),
		Messages:     b.messages,
	}
	if spec.Title == "" {
		spec.Title = rows[0].Subject
	}
	if spec.Title == "" {
		return Spec{}, fmt.Errorf("spec: no title given and entry %s has no subject to borrow", rows[0].ExtID)
	}
	if err := checkCastCoversSenders(spec); err != nil {
		return Spec{}, err
	}
	return spec, nil
}

// builder accumulates the spec as entries are visited in chronological order,
// which is also first-appearance order for the cast and for org colour slots.
type builder struct {
	opts     Options
	me       map[string]bool
	ids      *idAllocator
	idOf     map[int64]string    // corpus id -> spec id, for parent edges
	rowByID  map[int64]*entryRow // every selected entry, for sighting lookups
	subjOf   map[int64]string    // corpus id -> subject, to spot a new chain
	messages []Entry

	// cast is the participants panel; part and addrs are the corpus's own
	// participation record, which is the only source for a Slack author or for
	// anyone on an entry recovered from quoted text.
	cast  *cast
	part  map[int64][]partRow
	addrs map[int64][]string

	badZones map[string]int // unresolvable zone label -> entries affected
	// orgByPerson carries an org across a person's entries: their direct mail
	// says which organisation they are at, and their quote-recovered entries
	// have no address to say it again.
	orgByPerson map[int64]string
	orphans     int // entries naming a parent that is not in this spec

	// zones is what each entry's clock turned out to mean, keyed by corpus id;
	// zoneStats is the distribution, which is what the source notes report. A
	// distribution and not a score: there is no ground truth here to measure an
	// accuracy against, and a percentage would only invite tuning towards it.
	zones     map[int64]tzinfer.Resolution
	zoneStats tzinfer.Stats
	// zoneWhy collects one line of evidence per inferred entry, deduplicated by
	// sender, so the page can be argued with rather than merely believed.
	zoneWhy map[string]string

	// prev encodes attachment thumbnails and holds the page's preview budget, so
	// the cap is across the whole page rather than per entry.
	prev *previewer
}

func (b *builder) add(r *entryRow) {
	from := parseAddr(r.From)
	to := parseAddrList(r.To)
	cc := parseAddrList(r.Cc)

	date, clock, tz, zoneOK := stamp(r.TS, r.TZ, r.TZOffset)
	tzSource := ""
	switch {
	case tz != "":
		// A label the source wrote is a stated zone even where no table can turn
		// it into an offset: the clock beside it is still the sender's own.
		tzSource = tzStated
		if !zoneOK {
			b.badZones[r.TZ]++
		}
	case b.zones[r.ID].State == tzinfer.Inferred:
		// The clock is NOT moved. For a recovered entry r.TS holds the sentinel's
		// wall clock as written, and the offset inferred for it is the zone that
		// clock was written IN, so the pair already agrees. Converting it to the
		// sender's local time would need a second inference about the sender and
		// would replace a figure the reader can check against the quoted text
		// with one they cannot.
		tz, tzSource = tzinfer.FormatOffset(b.zones[r.ID].Off), tzInferred
		who := firstNonEmpty(r.Person, parseAddr(r.From).Who())
		if _, seen := b.zoneWhy[who]; !seen {
			b.zoneWhy[who] = b.zones[r.ID].Evidence
		}
	}

	e := Entry{
		Date:      date,
		Time:      clock,
		Sender:    firstNonEmpty(r.Person, from.Who()),
		Org:       b.orgFor(r.PersonID, from.Address),
		FromEmail: from.Address,
		To:        recipientLine(to, cc),
		Body:      bodyHTML(r),
		// Empty means unknown, and the renderer shows it as unknown. Nothing
		// downstream may fill it in: a zone invented at render time is a claim
		// with no evidence attached and no way for a reader to audit it.
		TZ:       tz,
		TZSource: tzSource,
		Quoted:   !r.Direct,
		Me:       b.me[from.Address],
		Source:   b.source(r),
		GmailID:  r.GmailID,
		// A mail entry's container *is* its thread id; mail_detail has no column
		// of its own for it.
		ThreadID: r.Container,
	}
	if r.Kind == "note" {
		e.Kind = "note"
	}

	if p, ok := b.idOf[r.ParentID]; ok {
		e.Parent = p
	} else if r.ParentID != 0 || r.ParentRef != "" {
		b.orphans++
	}
	// A subject names a chain where it starts one: at an entry with no parent
	// here, or where the subject changed from the parent's.
	if e.Parent == "" || b.subjOf[r.ParentID] != r.Subject {
		e.Subject = r.Subject
	}

	for _, a := range r.Atts {
		att := Attachment{Name: a.Name, Kind: attachmentKind(a.Mime, a.Name), Size: humanSize(a.Size)}
		if r.Direct {
			// Only a real message can be opened in Gmail.
			att.GmailID = r.GmailID
		}
		if att.GmailID == "" {
			// A mail attachment's permalink is the same Gmail URL the id already
			// builds, so carrying both would put the same target in the spec
			// twice. This is what gives a Slack attachment somewhere to go.
			att.Link = a.Permalink
		}
		// Dedup before encoding: hasAttachment identifies a file by name and
		// size, so a duplicate would charge the page's preview budget for a
		// thumbnail that is then thrown away.
		if hasAttachment(e.Attachments, att) {
			continue
		}
		att.Preview, att.PreviewW, att.PreviewH = b.prev.preview(a)
		e.Attachments = append(e.Attachments, att)
	}

	e.ID = b.ids.take(entryID(e))
	b.idOf[r.ID] = e.ID
	b.subjOf[r.ID] = r.Subject
	b.messages = append(b.messages, e)

	b.meet(r, e.Sender, e.Org, from, to, cc)
}

// meet records everyone this entry involves, in the order the page reads: its
// author, then the people the corpus says were addressed on it, then anyone
// named in a header the corpus did not resolve to a person.
func (b *builder) meet(r *entryRow, sender, org string, from addr, to, cc []addr) {
	ref := b.ref(r.PersonID, sender, from.Address)
	// The sender's row is handed the org their own bubbles carry, rather than
	// resolving it a second time from the address the corpus happens to list
	// first: the panel is read as the key to the transcript's colours, so the two
	// must be the same value and not merely two attempts at the same answer.
	b.cast.sender(ref, org, r.Direct)
	for _, p := range b.part[r.ID] {
		if p.Role == corpus.RoleFrom {
			// Already recorded, under the name the transcript shows beside their
			// messages rather than the display name their own header carried.
			continue
		}
		ref := b.ref(p.Person, p.Name, "")
		b.cast.recipient(ref, b.orgFor(p.Person, ref.address), r.Direct)
	}
	// A header the ingest never turned into a person still names someone. It is
	// rare — 16 entries of 31k on the corpus this was measured against carry a To:
	// line with no participants rows behind it — but the panel is not the place to
	// lose them.
	for _, a := range append(append([]addr{}, to...), cc...) {
		ref := b.ref(0, a.Who(), a.Address)
		b.cast.recipient(ref, b.orgFor(0, ref.address), r.Direct)
	}
}

// ref assembles the identities one appearance is known by. An address is taken
// from the header where there is one and from the corpus otherwise, which is what
// gives a Slack author a mailbox to be reached at and an org to be grouped under.
//
// The address is what the org is resolved from where a header supplied none, so a
// Slack author and a quote-recovered sender are coloured by the mailbox the
// corpus knows them by. That set is every identity the person holds, not the
// subset of their mail in this selection, so their colour does not move when the
// selection does.
func (b *builder) ref(person int64, name, address string) castRef {
	known := b.addrs[person]
	if address == "" && len(known) > 0 {
		address = known[0]
	}
	return castRef{person: person, address: address, name: name, others: known}
}

// source records where an entry was found: the mailbox, or someone's quoted
// history. It is provenance, not prose — a later pass may make it read better.
//
// The shape is load-bearing: each host is "msg <gmail-id>" and hosts are joined
// with ", ", which is what lets the renderer count them, collapse the line and
// link each id, while showing a hand-written prose source verbatim. Change the
// prefix or the separator and the collapse degrades to prose without failing
// anything — TestSourceNamesEachHostAsMsgID is the pin.
func (b *builder) source(r *entryRow) string {
	if r.Direct {
		if r.GmailID != "" {
			return "msg " + r.GmailID
		}
		return r.ExtID
	}
	// The message that quoted this one is later in the trail, so it is named by
	// its own source identity rather than by a spec id that may not exist yet.
	var in []string
	named := map[string]bool{}
	for _, id := range r.SeenIn {
		host, ok := b.rowByID[id]
		if !ok {
			continue
		}
		name := host.ExtID
		if host.GmailID != "" {
			name = "msg " + host.GmailID
		}
		// A sighting is keyed by (entry, host, kind), so a host that both quoted
		// and forwarded this entry arrives twice. It is one message to open, and a
		// count that said two would be a claim about the trail that is not true.
		if named[name] {
			continue
		}
		named[name] = true
		in = append(in, name)
	}
	if len(in) > 0 {
		return "unspooled from " + strings.Join(in, ", ")
	}
	return "unspooled from quoted text"
}

// attachEdits surfaces a quoter's in-place change to a quoted message as an
// edit on the message that contained it (the host), anchored to the base it
// modified. Without this the derived copy — Charles's original with Jason's
// change woven in — renders as its own unspooled node above the original: a
// duplicate that reorders history (issue #42).
//
// Who and when come from the HOST, not the derived copy: Jason edited the
// quote, so the edit is Jason's at Jason's time, even though the copy still
// names Charles (the quoted author) as its sender.
//
// Both the base and the host must be in this selection. When either is not —
// the change is anchored outside the page, or the quoting message was never
// collected — the derived entry keeps its own unspooled row and link, and no
// edit is attached, so the trail is not silently lost.
func (b *builder) attachEdits(rows []*entryRow) {
	// rows[i] built b.messages[i] in b.add, so this maps a corpus id to the
	// entry it became, letting us mutate a host and read the base's id.
	at := map[int64]int{}
	for i, r := range rows {
		at[r.ID] = i
	}
	for _, r := range rows {
		if !r.Derived || r.ParentID == 0 {
			continue
		}
		if _, ok := at[r.ParentID]; !ok {
			continue // the base it changed is not on this page
		}
		// The host is whichever collected message the copy was sighted inside.
		var host *Entry
		for _, h := range r.SeenIn {
			if j, ok := at[h]; ok {
				host = &b.messages[j]
				break
			}
		}
		if host == nil {
			continue // the quoting message is not in this timeline
		}
		host.Edits = append(host.Edits, Edit{
			ID:   b.idOf[r.ID],
			Base: b.idOf[r.ParentID],
			Who:  host.Sender,
			Time: host.Time,
			Body: r.BodyText,
		})
	}
}

// checkCastCoversSenders fails a spec that would show a message from someone the
// participants panel does not list. The panel is the page's answer to "who was
// involved", and the transcript is the reader's way of checking it; a name in one
// and not the other reads as a person the tool lost rather than as a person who
// was never there. The comparison is by name because that is the only key a
// reader has — an address they cannot see would not help them.
//
// It is an error and not a note because the panel is what justifies the page not
// listing its senders anywhere else.
func checkCastCoversSenders(sp Spec) error {
	named := map[string]bool{}
	for _, p := range sp.Participants {
		named[p.Name] = true
	}
	for _, m := range sp.Messages {
		if m.Sender == "" || named[m.Sender] {
			continue
		}
		return fmt.Errorf(
			"spec: %q sent %s (%s) but is not in the participants panel — "+
				"the panel is the only place the page names the cast",
			m.Sender, m.ID, m.Date)
	}
	return nil
}

// threads summarises the containers the transcript was assembled from.
func (b *builder) threads(rows []*entryRow) []Thread {
	type acc struct {
		subject string
		count   int
		first   int
		last    int
	}
	byContainer := map[string]*acc{}
	var order []string
	for i, r := range rows {
		if r.Container == "" {
			continue
		}
		a, ok := byContainer[r.Container]
		if !ok {
			a = &acc{subject: r.Subject, first: i, last: i}
			byContainer[r.Container] = a
			order = append(order, r.Container)
		}
		a.count++
		a.last = i
	}
	out := make([]Thread, 0, len(order))
	for _, c := range order {
		a := byContainer[c]
		out = append(out, Thread{
			Subject: a.subject,
			ID:      c,
			Count:   a.count,
			Span:    spanOf(b.messages[a.first].Date, b.messages[a.last].Date),
		})
	}
	return out
}

// notes reports what the corpus could not supply, so that a gap in the page is
// legible as a gap rather than read as the whole story.
func (b *builder) notes(rows []*entryRow) []SourceNote {
	var items []string
	if b.orphans > 0 {
		items = append(items, fmt.Sprintf(
			"%d of %d entries reply to a message that is not in this timeline — "+
				"their chain starts above what was collected.", b.orphans, len(rows)))
	}
	if quoted := countQuoted(rows); quoted > 0 {
		items = append(items, fmt.Sprintf(
			"%d of %d entries were recovered from quoted text and never existed as "+
				"standalone messages here.", quoted, len(rows)))
	}
	items = append(items, foldNotes(rows)...)
	items = append(items, b.zoneNotes(len(rows))...)
	labels := make([]string, 0, len(b.badZones))
	for tz := range b.badZones {
		labels = append(labels, tz)
	}
	sort.Strings(labels)
	for _, tz := range labels {
		items = append(items, fmt.Sprintf(
			"Zone %q is not one this tool can turn into an offset, so the %d entries "+
				"stating it are shown at UTC. The label is reproduced as stated.",
			tz, b.badZones[tz]))
	}
	if len(items) == 0 {
		return nil
	}
	return []SourceNote{{Title: "Coverage", Items: items}}
}

func countQuoted(rows []*entryRow) int {
	n := 0
	for _, r := range rows {
		if !r.Direct {
			n++
		}
	}
	return n
}

func hasAttachment(as []Attachment, a Attachment) bool {
	for _, x := range as {
		if x.Name == a.Name && x.Size == a.Size {
			return true
		}
	}
	return false
}

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if v != "" {
			return v
		}
	}
	return ""
}

// orgFor names the organisation behind one appearance of a person, taking the
// strongest evidence available: the address on this appearance, then an address
// the same person used on another entry here, then any address the corpus holds
// for them.
//
// It is the only resolver, and both the entry's colour and their participants row
// go through it. Resolving a person's org twice is what drifts: the panel read
// the corpus and a bubble read the header, so on the reference trail two people
// were named at an org in the panel and left uncoloured in the transcript, and a
// panel that disagrees with the page it is a key to is worse than no colour.
//
// The corpus step is what a quote-recovered entry needs, having no header of its
// own. It never invents an org: every step reads a real address belonging to that
// same person. Someone with no resolvable address anywhere stays without one,
// which the renderer shows as the unknown slot.
//
// One person therefore has one org for the whole page. Someone who changed
// employer mid-trail is shown at whichever of the two their mail resolved to
// first, rather than switching colour partway down — an ordering artefact, and
// the cost of being able to read the panel as the key to the transcript.
func (b *builder) orgFor(person int64, address string) string {
	if org := orgOf(address, b.opts.Orgs); org != "" {
		return org
	}
	if org := b.orgByPerson[person]; org != "" {
		return org
	}
	for _, a := range b.addrs[person] {
		if org := orgOf(a, b.opts.Orgs); org != "" {
			return org
		}
	}
	return ""
}
