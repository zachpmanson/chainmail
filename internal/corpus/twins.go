package corpus

import (
	"database/sql"
	"fmt"
	"math"
	"regexp"
	"sort"
	"time"

	"github.com/zachpmanson/chainmail/internal/textsim"
	"github.com/zachpmanson/chainmail/internal/zone"
)

// A twin is one message stored twice: once as the mailbox copy and once as an
// entry recovered from somebody's quote of it. unnest.quoteKey exists to stop
// that happening and cannot, because neither identity it can offer meets the
// mailbox copy's:
//
//   - a quoted Message-ID keys straight onto 'mail:<id>' and collapses for free,
//     but a quoting client almost never writes one into the sentinel it
//     generates;
//   - otherwise the key is (address, stated send time), and the stated time is a
//     wall clock in the QUOTER's zone while the mailbox copy's timestamp is the
//     true instant. The two therefore disagree by exactly the quoter's UTC
//     offset — ten hours between an Australian mailbox and its New Zealand
//     reply — and no loosening of the key makes a +1200 clock hash to an
//     instant.
//
// So the reconciliation is arithmetic rather than a key: a gap that IS a civil
// offset, corroborated by the words. The same arithmetic collapses two quoted
// copies of one message rendered by two different quoters, where the gap is the
// difference of their offsets and no mailbox copy exists at all.
//
// That gap is also a measurement worth keeping — see measureOffset.

// Twin identification, and why each gate is set where it is. A false collapse is
// not reversible: person_merges records a people merge, and entries have no
// equivalent, so a wrongly dropped entry is a message the corpus no longer
// holds. Every gate therefore declines rather than guesses.
const (
	// twinLattice: a civil offset is a whole quarter of an hour. Quarters rather
	// than half hours because the people here include Asia/Kathmandu at +0545 and
	// Pacific/Chatham at +1245, and a half-hour lattice would refuse their twins;
	// anything finer would admit gaps no clock on earth produces.
	twinLattice = 15
	// twinSlack: a sentinel states whole minutes and clients disagree about which
	// way the seconds go — one measured pair renders 04:40:38 as 16:41 — so a real
	// offset can miss the lattice by a minute.
	twinSlack = 1
	// minTwinTokens is the shortest message worth identifying. "Thanks, will do"
	// is every second message in a mailbox, and picking the wrong one of two
	// identical replies is undetectable once the other is gone.
	minTwinTokens = 25
	// minTwinRecall: nearly every word of the shorter copy must appear in the
	// fuller one. One-directional on purpose — the shorter copy is the elided one,
	// since quoting drops words rather than inventing them, and requiring
	// containment both ways would refuse every copy a client truncated.
	minTwinRecall = 0.85
	// twinHeadRun, twinHeadWindow, minTwinHead: the ordered opening test, applied
	// in both directions. This is the gate that separates two different messages
	// from one person who signs both the same way; textsim.HeadSimilarity records
	// the wrong answer that established it. Both directions because in a
	// quote-to-quote pair neither copy is the privileged one.
	twinHeadRun    = 8
	twinHeadWindow = 48
	minTwinHead    = 0.75
	// minAnnotationRun: the shortest run of words inserted into the middle of the
	// survivor's own text to read as somebody answering inline rather than as a
	// client rendering something differently. Five, measured: with the residue
	// proseWords takes out, every interior run below five on this corpus is a
	// renumbered list item or a link label, and the one real annotation is
	// twenty-one words, so nothing between two and twenty-one is at stake here.
	// What five costs is a two-word inline "yes, agreed", which still collapses
	// and is still lost; what a lower figure costs is a duplicate left in view for
	// every requote whose client renumbered a list.
	minAnnotationRun = 5
)

// TwinCollapse is one group of copies read as one message, with the copy that
// survives named and the reason it was chosen.
type TwinCollapse struct {
	Keep    int64
	KeepExt string
	Drop    []int64
	Why     string
	// Mailbox says the survivor is the mailbox copy rather than the fullest of
	// several recovered ones.
	Mailbox bool
	// Offsets are the quoters' render offsets this collapse establishes, in
	// minutes east of UTC. Only a group holding the mailbox copy yields one:
	// without the true instant there is nothing to subtract from.
	Offsets []RenderOffset
}

// RenderOffset is one client's offset, measured rather than inferred: the wall
// clock it wrote for a message it quoted, less that message's true instant.
type RenderOffset struct {
	Person int64 // whose client rendered it
	Host   int64 // the entry it was rendered in
	At     time.Time
	Off    int
	From   int64 // the quoted copy the measurement came from, which the collapse deletes
}

// TwinDecline is a candidate the pass would not collapse. Not an error — the
// point is that an unresolved duplicate stays two visible entries, where a wrong
// collapse is a message that silently no longer exists.
type TwinDecline struct {
	Entry  int64
	ExtID  string
	Reason string
	// Annotated marks the decline that is not an unresolved duplicate but a
	// second copy the corpus needs: one whose quoter answered inside the text
	// they quoted, so it holds words no other entry does. Worth reporting on
	// every run rather than behind the flag the others sit behind — it is the one
	// decline that would otherwise have destroyed something.
	Annotated bool
}

// TwinPlan is what CollapseTwins decided, and whether it carried it out.
type TwinPlan struct {
	Collapse []TwinCollapse
	Declined []TwinDecline
	Applied  bool
	// Removed is how many entries the plan deletes: what a before-and-after entry
	// count should differ by.
	Removed int
	// WithMailbox and QuotedOnly split the groups by whether the mailbox copy is
	// among them. They are different findings: the first is a duplicate of a
	// message the corpus holds properly, the second is one message recovered from
	// two forwards and never received, where nothing states the true instant.
	WithMailbox, QuotedOnly int
	// Measured is how many render offsets the plan establishes.
	Measured int
	// Annotated is how many copies were kept because their quoter answered inside
	// the text they quoted. Counted separately from the rest of Declined because
	// it is the only decline that says the corpus would have lost something: the
	// others are duplicates nothing could resolve.
	Annotated int
}

// twinCopy is one stored copy of a message as the twin test needs it.
//
// Wall is entries.ts as stored, and what it means differs by copy: for a mailbox
// entry the true instant, for a quoted one the wall clock a sentinel stated.
// Holding both in one field is deliberate — the difference between them is the
// whole measurement.
type twinCopy struct {
	id     int64
	ext    string
	quoted bool
	wall   time.Time
	words  []string
	// prose is words again with the markup residue removed: what the positional
	// test needs, and what the identity gates must not use. See proseWords.
	prose []string
	atts  int
}

// CollapseTwins folds together the entries that are one message stored more than
// once, and reports every candidate it would not decide.
//
// The plan is computed in full before anything is written, so a dry run and an
// apply of the same corpus produce the same plan: the grouping is a partition
// under a symmetric relation and no gate reads state an earlier collapse would
// have changed. That is what makes reviewing the dry run worth anything, and it
// is also what makes a triplicate converge — three copies of one message are one
// group with two entries dropped, whatever order the pairs are considered in.
func CollapseTwins(s *Store, apply bool) (TwinPlan, error) {
	var plan TwinPlan
	people, err := peopleWithQuotedEntries(s)
	if err != nil {
		return plan, err
	}
	for _, person := range people {
		copies, err := twinCopies(s, person)
		if err != nil {
			return plan, err
		}
		groups, declined := groupTwins(copies)
		plan.Declined = append(plan.Declined, declined...)
		for _, g := range groups {
			c, kept, why, ok := resolveTwins(g)
			if !ok {
				for _, m := range g {
					plan.Declined = append(plan.Declined,
						TwinDecline{Entry: m.id, ExtID: m.ext, Reason: why})
				}
				continue
			}
			// A group can hold both an annotated copy and a clean one — one quoter
			// answered inline, another requoted the same message untouched. The
			// clean copies still collapse, so the decline is per copy rather than
			// per group; refusing the whole group would leave a duplicate standing
			// for the sake of a copy it has nothing to do with.
			plan.Declined = append(plan.Declined, kept...)
			for _, k := range kept {
				if k.Annotated {
					plan.Annotated++
				}
			}
			if len(c.Drop) == 0 {
				continue
			}
			for _, drop := range c.Drop {
				off, ok, err := measureOffset(s, c.Keep, drop)
				if err != nil {
					return plan, err
				}
				if ok {
					c.Offsets = append(c.Offsets, off)
				}
			}
			plan.Collapse = append(plan.Collapse, c)
			plan.Removed += len(c.Drop)
			plan.Measured += len(c.Offsets)
			if c.Mailbox {
				plan.WithMailbox++
			} else {
				plan.QuotedOnly++
			}
		}
	}
	sort.Slice(plan.Collapse, func(i, j int) bool {
		return plan.Collapse[i].Keep < plan.Collapse[j].Keep
	})
	sort.Slice(plan.Declined, func(i, j int) bool {
		return plan.Declined[i].Entry < plan.Declined[j].Entry
	})
	if !apply {
		return plan, nil
	}
	plan.Applied = true
	for _, c := range plan.Collapse {
		if err := collapseGroup(s, c); err != nil {
			return plan, fmt.Errorf("collapsing into %s: %w", c.KeepExt, err)
		}
	}
	return plan, nil
}

func peopleWithQuotedEntries(s *Store) ([]int64, error) {
	rows, err := s.db.Query(`
		select distinct person_id from entries
		where quoted = 1 and person_id is not null and source = ?
		order by person_id`, SourceMail)
	if err != nil {
		return nil, fmt.Errorf("listing people with recovered entries: %w", err)
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// twinCopies reads one person's mail entries, oldest first.
//
// Per person, because the relation cannot cross authors: two copies of a message
// agree about who sent it, both having been written from the same From line or
// the same attribution. Two people are two messages however alike the words are.
// Grouping by person is also what keeps this a windowed sweep rather than a
// 31,000-entry cross join.
func twinCopies(s *Store, person int64) ([]twinCopy, error) {
	rows, err := s.db.Query(`
		select e.id, e.ext_id, e.quoted, e.ts, coalesce(e.body_text,''),
		       (select count(*) from attachments a where a.entry_id = e.id)
		from entries e
		where e.person_id = ? and e.source = ?
		order by e.ts, e.id`, person, SourceMail)
	if err != nil {
		return nil, fmt.Errorf("reading the entries of person %d: %w", person, err)
	}
	defer rows.Close()
	var out []twinCopy
	for rows.Next() {
		var c twinCopy
		var ts int64
		var body string
		if err := rows.Scan(&c.id, &c.ext, &c.quoted, &ts, &body, &c.atts); err != nil {
			return nil, err
		}
		c.wall = time.Unix(ts, 0).UTC()
		c.words = copyWords(body, c.quoted)
		c.prose = proseWords(body, c.quoted)
		out = append(out, c)
	}
	return out, rows.Err()
}

// copyWords is the tokens of what the sender wrote themselves, with the history
// they quoted left out.
//
// The two copies are stored at different scopes: a recovered entry's body is one
// peeled block and holds that message alone, while a mailbox entry's body is the
// whole part, message and trail together. Compared as stored, a two-line reply
// would match any long message that quoted it. Peeling the mailbox copy — with
// ownWords, the same reduction the embedder uses, including its refusal to
// mistake a bare forward's first block for the sender's own — puts both sides at
// the same scope, which is what makes the words a test of identity rather than of
// containment.
func copyWords(body string, quoted bool) []string {
	if quoted {
		return textsim.Tokens(body)
	}
	return textsim.Tokens(ownWords(body))
}

// proseWords is copyWords again with the residue two renditions of one message
// disagree about taken out: links, addresses, inline-image placeholders and
// attachment names.
//
// The positional test cannot use the plain token list. Each of those things is
// written differently by every client — the same inline image arrives as
// "[image: shot.png]" from one and bracketed from the next, the same link as bare
// anchor text in one rendition and as the text followed by the whole URL in the
// other — and each lands as a run of tokens INSIDE the shared text, which is the
// exact shape an inline answer has. On the raw tokens 73 of 556 collapses looked
// annotated and one was; with the residue out, one looks annotated and it is the
// same one.
//
// Not used by the identity gates, which are right to be tolerant of it: recall
// and the ordered opening ask whether two copies are one message, and a link
// rendered two ways does not make them two.
func proseWords(body string, quoted bool) []string {
	if !quoted {
		body = ownWords(body)
	}
	for _, re := range markupResidue {
		body = re.ReplaceAllString(body, " ")
	}
	return textsim.Tokens(body)
}

// markupResidue is what a client writes and a person does not.
//
// The bracketed form is deliberately wide: in a plain-text rendition angle
// brackets hold URLs, addresses and image placeholders and little else, and the
// alternative — an expression per client convention — would be a list that goes
// stale the first time somebody's mail comes from a client not on it.
var markupResidue = []*regexp.Regexp{
	regexp.MustCompile(`(?i)https?://\S+`),
	regexp.MustCompile(`(?i)mailto:\S*`),
	regexp.MustCompile(`(?i)\[cid:[^\]]*\]`),
	regexp.MustCompile(`(?i)\[image:[^\]]*\]`),
	// Same line only: a lone ">" opening a quoted line has no partner, and this
	// must not swallow the rest of the body looking for one.
	regexp.MustCompile(`<[^>\n]*>`),
	regexp.MustCompile(`\S+@\S+`),
	regexp.MustCompile(`(?i)\S+\.(?:png|jpe?g|gif|bmp|webp|pdf|csv|xlsx?|docx?)\b`),
}

// groupTwins partitions one person's copies into groups that are one message,
// and reports the recovered entries it could not place.
//
// A recovered entry is reported once, with the most informative reason found
// against it: "shares the words but not the opening" from a candidate that
// cleared the clock says more than "nothing was near it".
func groupTwins(copies []twinCopy) ([][]twinCopy, []TwinDecline) {
	parent := make([]int, len(copies))
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(i int) int {
		for parent[i] != i {
			parent[i] = parent[parent[i]]
			i = parent[i]
		}
		return i
	}
	union := func(a, b int) {
		ra, rb := find(a), find(b)
		if ra != rb {
			parent[max(ra, rb)] = min(ra, rb)
		}
	}

	reason := map[int]string{}
	matched := map[int]bool{}
	for i := range copies {
		if !copies[i].quoted {
			continue
		}
		if len(copies[i].words) < minTwinTokens {
			reason[i] = "too short to identify: fewer than 25 words of its own"
			continue
		}
		for j := range copies {
			// The widest gap any pair could legitimately show: a mailbox copy sits
			// at most 14 hours behind its quoted clock and 12 ahead of it, and two
			// quoted copies at most 26 hours apart when their quoters sit at
			// opposite ends of the world.
			if i == j || copies[j].wall.Sub(copies[i].wall).Abs() > 26*time.Hour {
				continue
			}
			why, ok := twinnable(copies[i], copies[j])
			if !ok {
				if why != "" && reason[i] == "" {
					reason[i] = why
				}
				continue
			}
			matched[i], matched[j] = true, true
			union(i, j)
		}
		if !matched[i] && reason[i] == "" {
			reason[i] = "no other copy within a plausible offset of its stated clock"
		}
	}

	byRoot := map[int][]twinCopy{}
	for i := range copies {
		if matched[i] {
			byRoot[find(i)] = append(byRoot[find(i)], copies[i])
		}
	}
	roots := make([]int, 0, len(byRoot))
	for r := range byRoot {
		roots = append(roots, r)
	}
	sort.Ints(roots)
	groups := make([][]twinCopy, 0, len(roots))
	for _, r := range roots {
		groups = append(groups, byRoot[r])
	}

	var declined []TwinDecline
	for i := range copies {
		if matched[i] || reason[i] == "" {
			continue
		}
		declined = append(declined,
			TwinDecline{Entry: copies[i].id, ExtID: copies[i].ext, Reason: reason[i]})
	}
	return groups, declined
}

// twinnable reports whether two copies are one message, and when they are not,
// what stopped it — empty for a pair that was never a candidate, so a decline
// names the gate that refused rather than the width of the corpus.
//
// At least one side must be a recovered entry. Two mailbox copies are two
// messages: each carries its own Message-ID and a mail server does not issue one
// twice, so however alike they read, collapsing them would delete a message that
// really was sent.
func twinnable(a, b twinCopy) (string, bool) {
	if !a.quoted && !b.quoted {
		return "", false
	}
	off, ok := twinOffset(a, b)
	if !ok {
		return "", false
	}
	if len(b.words) < minTwinTokens {
		return "the copy at a plausible offset has fewer than 25 words of its own: " +
			offsetLabel(off), false
	}
	short, long := a.words, b.words
	if len(long) < len(short) {
		short, long = long, short
	}
	if recall := float64(textsim.Overlap(short, long)) / float64(len(short)); recall < minTwinRecall {
		return fmt.Sprintf("a copy at a plausible offset says something else: %s, "+
			"holding %.0f%% of the shorter one's words", offsetLabel(off), recall*100), false
	}
	if textsim.HeadSimilarity(a.words, b.words, twinHeadRun, twinHeadWindow) < minTwinHead ||
		textsim.HeadSimilarity(b.words, a.words, twinHeadRun, twinHeadWindow) < minTwinHead {
		return "a copy at a plausible offset shares this one's words but not its " +
			"opening, which is what a shared signature looks like: " + offsetLabel(off), false
	}
	return "", true
}

// twinOffset reads the gap between two copies as a UTC offset, and reports
// whether it can be one.
//
// The direction matters when a mailbox copy is involved: its timestamp is the
// true instant and the quoted clock is that instant rendered somewhere, so the
// gap IS that somewhere's offset and has to lie in the range civil offsets
// occupy. Between two quoted copies the gap is the difference of two offsets,
// which can be anything from -26 to +26 hours and says nothing about either, so
// only the lattice applies there.
func twinOffset(a, b twinCopy) (int, bool) {
	mins := int(b.wall.Sub(a.wall).Round(time.Minute) / time.Minute)
	if a.quoted && !b.quoted {
		mins = -mins // stated clock minus true instant, whichever side it is on
	}
	if !a.quoted || !b.quoted {
		if mins < zone.Min-twinSlack || mins > zone.Max+twinSlack {
			return 0, false
		}
	}
	near := nearestOffset(mins)
	if abs(mins-near) > twinSlack {
		return 0, false
	}
	return near, true
}

// nearestOffset rounds a gap in minutes to the nearest offset the lattice
// admits. The caller decides whether the distance to it is small enough to
// believe.
func nearestOffset(mins int) int {
	return int(math.Round(float64(mins)/twinLattice)) * twinLattice
}

// resolveTwins chooses the copy that survives a group, or refuses the group with
// the reason.
//
// The mailbox copy wins whenever there is one, and it is not a tiebreak: it
// holds the true instant, the zone its own client stated, its Message-ID, its
// attachments and its body_html, while every quoted copy has been rewrapped and
// elided by each client that passed it along. Attachments are the visible proof
// of the asymmetry — they are only ever recorded on a message that was actually
// fetched — which is why a recovered copy carrying one is a contradiction and
// refuses the group rather than being dropped.
//
// With no mailbox copy the fullest text wins, on unnest.Dedup's reasoning that
// quoting elides rather than invents, and the lowest id breaks a tie so the
// choice cannot depend on the order rows came back in.
//
// The copies it will not drop come back alongside the collapse: a copy whose
// quoter answered inside the text they quoted holds words that exist nowhere else
// as text, the quoter's own markup being the only other place they survive, and a
// collapse would delete them. That is a worse outcome than the duplicate it
// removes, so the copy stays.
func resolveTwins(g []twinCopy) (TwinCollapse, []TwinDecline, string, bool) {
	sort.Slice(g, func(i, j int) bool { return g[i].id < g[j].id })
	keep, real := -1, 0
	for i := range g {
		if !g[i].quoted {
			real++
			keep = i
		}
	}
	if real > 1 {
		// The relation says two messages that each carry their own Message-ID are
		// one message. A gate is wrong about this group and nothing here can say
		// which, so all of it stays.
		return TwinCollapse{}, nil, "group holds more than one mailbox copy", false
	}
	mail := true
	why := fmt.Sprintf("the mailbox copy, against %d recovered", len(g)-1)
	if real == 0 {
		mail = false
		for i := range g {
			if keep < 0 || len(g[i].words) > len(g[keep].words) {
				keep = i
			}
		}
		why = fmt.Sprintf("the fullest of %d recovered copies; no mailbox copy exists", len(g))
	}
	c := TwinCollapse{Keep: g[keep].id, KeepExt: g[keep].ext, Why: why, Mailbox: mail}
	var kept []TwinDecline
	for i := range g {
		if i == keep {
			continue
		}
		if g[i].atts > 0 {
			return TwinCollapse{}, nil, fmt.Sprintf("a copy to be dropped holds attachments, "+
				"so it was fetched rather than quoted: entry %d", g[i].id), false
		}
		if why, inline := annotates(g[keep], g[i]); why != "" {
			kept = append(kept, TwinDecline{
				Entry: g[i].id, ExtID: g[i].ext, Reason: why, Annotated: inline})
			continue
		}
		c.Drop = append(c.Drop, g[i].id)
	}
	return c, kept, "", true
}

// annotates reports whether dropping a copy would delete words only it holds,
// says so in the terms the decline is printed in, and separates the copy somebody
// annotated from the one that merely cannot be placed.
//
// Direction rather than chronology, and they are the same statement: quoting
// elides rather than invents, so words present in a requote and absent from the
// copy the corpus would keep were typed by whoever requoted it, after the fact.
// Reading it off the pair instead of off the timestamps also keeps the test
// honest in a quoted-only group, where no copy states a true instant and the
// clocks say only where they were rendered.
//
// Position is what separates an answer from chrome. A signature, a legal notice
// and a tracking blob land after the shared text and are appended by the client
// on every requote; an inline answer lands between two of the survivor's own
// adjacent words, which is a place no client puts anything. Only the longest
// single run counts: scattered insertions of a word each are what a renderer
// produces, and summing them would read a renumbered list as a paragraph.
func annotates(keep, drop twinCopy) (string, bool) {
	d := textsim.Divergences(keep.prose, drop.prose)
	if !d.Measured {
		// Too long to align — a machine-generated report rather than anything a
		// person annotated, but position cannot be measured to say so. Containment
		// can, in linear time and without an alignment: a copy holding no word the
		// survivor lacks has nothing to lose wherever its words sit. Only a long
		// copy that does hold one declines, because the collapse is the
		// irreversible move and nothing here can place it.
		if textsim.Overlap(drop.prose, keep.prose) == len(drop.prose) {
			return "", false
		}
		return fmt.Sprintf("a copy to be dropped holds words the survivor lacks and is "+
			"too long to place them: entry %d", drop.id), false
	}
	if d.LongestInside < minAnnotationRun {
		return "", false
	}
	return fmt.Sprintf("its quoter answered inside the text they quoted, so a "+
		"collapse would delete words no other entry holds: %d inserted into entry %d, "+
		"in a run of %d", d.Inside, keep.id, d.LongestInside), true
}

// collapseGroup carries one collapse out.
//
// Everything hanging off a dropped copy moves to the survivor before the row
// goes, because none of it is about the copy: a sighting is a place the MESSAGE
// was seen, a participant is somebody it was routed to, and a reply edge is one
// message answering another. Losing an edge would sever a chain at exactly the
// point the duplicate made hard to read.
func collapseGroup(s *Store, c TwinCollapse) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, drop := range c.Drop {
		if err := absorb(tx, s, c.Keep, drop); err != nil {
			return err
		}
	}
	for _, o := range c.Offsets {
		if err := recordOffset(tx, o); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// absorb moves one dropped copy's evidence onto the survivor and deletes it.
func absorb(tx *sql.Tx, s *Store, keep, drop int64) error {
	// The survivor may already hold a sighting or a participation the dropped copy
	// also holds, and both are keyed on the entry — so repoint what does not
	// collide and clear what does, as a person merge does.
	for _, q := range []string{
		`update or ignore sightings set entry_id=? where entry_id=?`,
		`update or ignore sightings set seen_in=? where seen_in=?`,
		`update or ignore participants set entry_id=? where entry_id=?`,
	} {
		if _, err := tx.Exec(q, keep, drop); err != nil {
			return fmt.Errorf("moving the evidence of %d: %w", drop, err)
		}
	}
	// A child of the dropped copy replied to the MESSAGE, so it now replies to the
	// survivor — except the survivor itself, which quoted the copy of its own
	// words and would otherwise become its own parent.
	if _, err := tx.Exec(
		`update entries set parent_id=? where parent_id=? and id<>?`, keep, drop, keep); err != nil {
		return fmt.Errorf("repointing the children of %d: %w", drop, err)
	}
	// A message can be quoted inside itself — a resend whose body carries the
	// "Sent:" header block of the message being resent — which leaves the mailbox
	// copy pointing at the copy of its own words. That edge goes rather than
	// travelling: nothing replied to anything.
	if _, err := tx.Exec(
		`update entries set parent_id=null where id=? and parent_id=?`, keep, drop); err != nil {
		return fmt.Errorf("clearing the self-edge of %d: %w", keep, err)
	}
	// And the survivor's own parent, where it has none and the dropped copy was
	// quoted somewhere that showed one.
	if _, err := tx.Exec(`
		update entries set parent_id = (select parent_id from entries where id=?)
		where id=? and parent_id is null
		  and coalesce((select parent_id from entries where id=?), ?) not in (?, ?)`,
		drop, keep, drop, keep, keep, drop); err != nil {
		return fmt.Errorf("adopting the parent of %d: %w", drop, err)
	}

	// Subject, zone and author fill gaps on a surviving quoted copy exactly as a
	// later sighting does. A mailbox survivor keeps its own, which is what
	// EnrichQuoted refuses to touch and for the same reason: its headers are
	// authoritative and a forward's rendition of them is not.
	var e Entry
	if err := tx.QueryRow(`
		select coalesce(subject,''), coalesce(tz,''), coalesce(person_id,0), coalesce(body_text,'')
		from entries where id=?`, drop).
		Scan(&e.Subject, &e.TZ, &e.PersonID, &e.BodyText); err != nil {
		return err
	}
	var keepQuoted int
	if err := tx.QueryRow(`select quoted from entries where id=?`, keep).Scan(&keepQuoted); err != nil {
		return err
	}
	if keepQuoted == 1 {
		if err := s.enrich(tx, keep, e); err != nil {
			return err
		}
	}

	// The dropped row's terms have to leave both external-content indexes before
	// the row does: FTS5 keeps no copy of the text, so afterwards there is nothing
	// left to tell it which terms to retract and the index would go on matching a
	// message the corpus no longer holds.
	var dropSubject, dropBody string
	if err := tx.QueryRow(
		`select coalesce(subject,''), coalesce(body_text,'') from entries where id=?`, drop).
		Scan(&dropSubject, &dropBody); err != nil {
		return err
	}
	if _, err := tx.Exec(
		`insert into entries_fts(entries_fts, rowid, subject, body_text) values ('delete', ?, ?, ?)`,
		drop, dropSubject, dropBody); err != nil {
		return fmt.Errorf("un-indexing %d: %w", drop, err)
	}
	if _, err := tx.Exec(
		`insert into entries_ident(entries_ident, rowid, body_text) values ('delete', ?, ?)`,
		drop, dropBody); err != nil {
		return fmt.Errorf("un-indexing %d for identifiers: %w", drop, err)
	}
	for _, q := range []string{
		// Both directions, because "or ignore" above leaves a row behind wherever
		// the survivor already recorded the same fact — and a sighting that still
		// points AT the dropped copy would hold the delete on a foreign key.
		`delete from sightings where entry_id=?`,
		`delete from sightings where seen_in=?`,
		`delete from participants where entry_id=?`,
		`delete from attachments where entry_id=?`,
		`delete from entry_embeddings where entry_id=?`,
		`delete from mail_detail where entry_id=?`,
		`delete from entries where id=?`,
	} {
		if _, err := tx.Exec(q, drop); err != nil {
			return fmt.Errorf("dropping %d: %w", drop, err)
		}
	}
	return nil
}

// measureOffset works out the offset the client that quoted a message rendered
// its clock at.
//
// This is the one thing a collapse establishes that nothing else in the corpus
// can: the mailbox copy states the true instant, the sentinel states a wall
// clock, and the difference is the quoting client's offset exactly — no
// placement, no ordering, no inference. tzinfer otherwise has to derive it from
// where the quoter's own mail says they are, which is why the measurement is
// worth writing down: the collapse destroys the evidence, since the quoted row is
// the only place that wall clock exists.
//
// Attributed only where the dropped copy was seen in exactly one host. Several
// hosts mean several clients rendered this message and the store keeps one clock
// without saying whose, so the offset is real and its owner is not known — and a
// measurement filed against the wrong person is worse than one not filed at all.
func measureOffset(s *Store, keep, drop int64) (RenderOffset, bool, error) {
	var keepQuoted int
	var keepTS, dropTS int64
	if err := s.db.QueryRow(`select quoted, ts from entries where id=?`, keep).
		Scan(&keepQuoted, &keepTS); err != nil {
		return RenderOffset{}, false, err
	}
	if keepQuoted != 0 {
		// No mailbox copy in this group, so there is no true instant to subtract.
		return RenderOffset{}, false, nil
	}
	if err := s.db.QueryRow(`select ts from entries where id=?`, drop).Scan(&dropTS); err != nil {
		return RenderOffset{}, false, err
	}
	rows, err := s.db.Query(`
		select h.id, coalesce(h.person_id,0), h.ts from sightings g
		join entries h on h.id = g.seen_in
		where g.entry_id = ? and g.kind = 'quoted'`, drop)
	if err != nil {
		return RenderOffset{}, false, err
	}
	defer rows.Close()
	var hosts []RenderOffset
	for rows.Next() {
		var o RenderOffset
		var at int64
		if err := rows.Scan(&o.Host, &o.Person, &at); err != nil {
			return RenderOffset{}, false, err
		}
		o.At, o.From = time.Unix(at, 0).UTC(), drop
		o.Off = nearestOffset(int((dropTS - keepTS) / 60))
		hosts = append(hosts, o)
	}
	if err := rows.Err(); err != nil {
		return RenderOffset{}, false, err
	}
	if len(hosts) != 1 || hosts[0].Person == 0 {
		return RenderOffset{}, false, nil
	}
	return hosts[0], true, nil
}

func recordOffset(tx *sql.Tx, o RenderOffset) error {
	if _, err := tx.Exec(`
		insert into render_offsets (person_id, entry_id, at, off, measured_from, measured_at)
		values (?,?,?,?,?,?)
		on conflict(person_id, entry_id, off) do nothing`,
		o.Person, o.Host, o.At.Unix(), o.Off, o.From, time.Now().Unix()); err != nil {
		return fmt.Errorf("recording the render offset of entry %d: %w", o.Host, err)
	}
	return nil
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// offsetLabel writes minutes east of UTC the way a Date header does. Stated here
// rather than borrowed from tzinfer, which is a consumer of this package's
// output and should not become a dependency of it.
func offsetLabel(mins int) string {
	sign := "+"
	if mins < 0 {
		sign, mins = "-", -mins
	}
	return fmt.Sprintf("%s%02d%02d", sign, mins/60, mins%60)
}

// FindTwin returns the entry a newly recovered block is a second copy of, if
// the corpus already holds one.
//
// This is the same test the repair pass applies, asked at extraction so the twin
// is never created in the first place. Both are needed and neither is enough: an
// extraction-only fix leaves every twin already stored, and a repair-only fix
// lets them come back on the next ingest. The refusals are the same at both ends
// too, including the annotated block.
//
// The caller must have a stated send time. A block whose clock was inferred from
// its host carries the host's instant rather than a rendered wall clock, so the
// gap to a mailbox copy would be an artefact of that substitution and would mean
// nothing as an offset.
func FindTwin(s *Store, person int64, wall time.Time, text string) (int64, bool, error) {
	if person == 0 {
		return 0, false, nil
	}
	c := twinCopy{quoted: true, wall: wall,
		words: copyWords(text, true), prose: proseWords(text, true)}
	if len(c.words) < minTwinTokens {
		return 0, false, nil
	}
	rows, err := s.db.Query(`
		select e.id, e.ext_id, e.quoted, e.ts, coalesce(e.body_text,''),
		       (select count(*) from attachments a where a.entry_id = e.id)
		from entries e
		where e.person_id = ? and e.source = ? and e.ts between ? and ?
		order by e.id`, person, SourceMail,
		wall.Add(-26*time.Hour).Unix(), wall.Add(26*time.Hour).Unix())
	if err != nil {
		return 0, false, fmt.Errorf("looking for a twin of a recovered block: %w", err)
	}
	defer rows.Close()
	var mailbox, all []int64
	for rows.Next() {
		var o twinCopy
		var ts int64
		var body string
		if err := rows.Scan(&o.id, &o.ext, &o.quoted, &ts, &body, &o.atts); err != nil {
			return 0, false, err
		}
		o.wall = time.Unix(ts, 0).UTC()
		o.words = copyWords(body, o.quoted)
		o.prose = proseWords(body, o.quoted)
		if _, ok := twinnable(c, o); !ok {
			continue
		}
		// Converging here would store nothing but a sighting, so an inline answer
		// in this block would never reach the corpus at all — the same loss the
		// repair pass declines, one ingest earlier. The block becomes its own
		// entry instead, which is what the caller does with a refusal.
		if why, _ := annotates(o, c); why != "" {
			continue
		}
		all = append(all, o.id)
		if !o.quoted {
			mailbox = append(mailbox, o.id)
		}
	}
	if err := rows.Err(); err != nil {
		return 0, false, err
	}
	// The mailbox copy is the one to converge on when there is exactly one, for
	// the reasons resolveTwins gives. Two of them, or several recovered copies
	// with none, is the ambiguous case: the block is stored as its own entry and
	// left for the repair pass, which reports what it will not decide instead of
	// deciding it here, one block at a time, with no view of the group.
	switch {
	case len(mailbox) == 1:
		return mailbox[0], true, nil
	case len(mailbox) == 0 && len(all) == 1:
		return all[0], true, nil
	}
	return 0, false, nil
}
