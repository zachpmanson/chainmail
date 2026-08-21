package spec

import (
	"fmt"
	"sort"

	"github.com/zachpmanson/chainmail/internal/corpus"
	"github.com/zachpmanson/chainmail/internal/tzinfer"
)

// TZ source values published on an Entry, so a reader can tell a fact from an
// inference without reading the source notes.
const (
	tzStated   = "stated"
	tzInferred = "inferred"
)

// inferZones works out what the unlabelled clocks in this selection mean.
//
// The evidence about where people were is corpus-wide; the ordering constraints
// are local, because only the selected entries have their reply edges to hand.
// That asymmetry is deliberate: widening the placement evidence can only sharpen
// an answer, whereas the ordering constraints are what makes an answer refutable
// and they need the graph, not a sample of it. An entry whose parent or children
// were left out of the selection simply has looser bounds and is likelier to
// come back ambiguous, which is the correct consequence of having looked at less.
func inferZones(store *corpus.Store, rows []*entryRow) (map[int64]tzinfer.Resolution, tzinfer.Stats, error) {
	places, err := store.Places()
	if err != nil {
		return nil, tzinfer.Stats{}, err
	}
	nodes := make([]tzinfer.Node, 0, len(rows))
	for _, r := range rows {
		nodes = append(nodes, tzinfer.Node{
			ID:     r.ID,
			Parent: r.ParentID,
			Person: r.PersonID,
			// TS is the instant for a mailbox message and the sentinel's wall
			// clock read as UTC for a recovered one. Which of the two it is is
			// exactly what Off being nil says, so no branch is needed here.
			Wall: r.TS,
			Off:  r.TZOffset,
		})
	}
	res, stats := tzinfer.Resolve(nodes, places)
	return res, stats, nil
}

// zoneNotes reports what happened to the clocks whose zone the source did not
// state, as a distribution and the evidence behind it.
//
// Every state is named, including the ones that failed. A page that quietly
// dropped its unknowns would read as though every time on it were equally
// trustworthy, which is the defect this whole path exists to remove.
func (b *builder) zoneNotes(total int) []string {
	s := b.zoneStats
	var items []string
	if s.Inferred > 0 {
		items = append(items, fmt.Sprintf(
			"%d of %d entries state no zone. Their clocks are shown at an offset "+
				"inferred from the client that quoted them — an attribution sentinel is "+
				"written in the zone of the person replying, not the sender's, so the "+
				"clock is that person's local time and is marked as inferred.",
			s.Inferred, total))
		for _, who := range sortedNames(b.zoneWhy) {
			items = append(items, fmt.Sprintf("Inferred for %s — %s", who, b.zoneWhy[who]))
		}
	}
	if s.Ambiguous > 0 {
		items = append(items, fmt.Sprintf(
			"%d of %d entries state no zone and more than one candidate survives the "+
				"order the quotes establish. No zone is shown for them: their clock is a "+
				"wall clock of unknown origin and cannot be compared with the others.",
			s.Ambiguous, total))
	}
	if s.Unknown > 0 {
		items = append(items, fmt.Sprintf(
			"%d of %d entries state no zone and nothing here can place them, so no zone "+
				"is shown. Their clocks are wall clocks as quoted; ordering around them "+
				"rests on the reply graph rather than on the times.",
			s.Unknown, total))
	}
	if s.Rejected > 0 {
		items = append(items, fmt.Sprintf(
			"%d of those had a candidate zone that the order the quotes establish rules "+
				"out, so the inference was dropped rather than the ordering overridden.",
			s.Rejected))
	}
	return items
}

func sortedNames(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
