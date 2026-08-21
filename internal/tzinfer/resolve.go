package tzinfer

import (
	"fmt"
	"sort"
	"time"

	"github.com/zachpmanson/chainmail/internal/unnest"
)

// Node is one message as the resolver needs it: who sent it, when it says it was
// sent, and what it replies to.
//
// Wall is the clock the source stated, read as UTC and nothing more. For a
// mailbox message that clock came with an offset and Off holds it, so the
// instant is exact. For a message recovered from quoted text Off is nil and Wall
// is the sentinel's clock as written, which is not yet an instant at all.
type Node struct {
	ID     int64
	Parent int64 // the message this replies to; 0 when unknown or outside the set
	Person int64
	Wall   time.Time
	Off    *int
}

// State is what a rendered entry may say about its zone. The three the design
// requires are Stated, Inferred and Unknown; Ambiguous is Unknown with a reason
// worth printing, because "we had candidates and could not choose" is a
// different admission from "we had nothing".
type State int

const (
	Unknown State = iota
	Ambiguous
	Inferred
	Stated
)

func (s State) String() string {
	switch s {
	case Ambiguous:
		return "ambiguous"
	case Inferred:
		return "inferred"
	case Stated:
		return "stated"
	}
	return "unknown"
}

// Resolution is what one entry's clock turned out to mean.
//
// Off is minutes east of UTC and is only meaningful for Stated and Inferred.
// Note what it is the offset OF: the zone the wall clock was WRITTEN at, which
// for a recovered message is the quoting client's zone, not the sender's. Why is
// in the comment on quoterOffsets.
type Resolution struct {
	State    State
	Off      int
	Evidence string
}

// Stats is the distribution over a resolved set, which is the only honest
// summary — there is no ground truth here to score a percentage against.
type Stats struct {
	Stated, Inferred, Ambiguous, Unknown int
	// Rejected counts entries that had candidates, all of which were thrown out
	// for putting the message outside the window its neighbours allow. Every one
	// is an inference the evidence supported and the ordering refused.
	Rejected int
	// Selected counts inferences where placement offered more than one candidate
	// and the ordering picked exactly one. It is what the reply graph adds over
	// placement alone: without it these entries would be ambiguous, and a rule
	// that broke the tie by frequency or proximity would be guessing on every
	// one of them.
	Selected int
}

// Resolve reads every unlabelled wall clock it can, given where people were and
// which message quotes which.
//
// The two inputs pull in different directions and that is the point. Placement
// proposes: the zone of the person whose client wrote the sentinel says what the
// clock could be. Ordering disposes: a quoted message was sent before the
// message quoting it, which is a fact from the reply graph and owes nothing to
// any clock. Where placement offers several candidates and ordering admits
// exactly one, the result is an inference with evidence behind it. Where
// ordering admits several, or none, the answer is that we do not know — stated
// as such, never filled in with the most common or the nearest.
func Resolve(nodes []Node, places map[int64]Place) (map[int64]Resolution, Stats) {
	out := make(map[int64]Resolution, len(nodes))
	var st Stats

	byID := make(map[int64]*Node, len(nodes))
	kids := map[int64][]int64{}
	for i := range nodes {
		byID[nodes[i].ID] = &nodes[i]
	}
	for i := range nodes {
		if p := nodes[i].Parent; p != 0 && byID[p] != nil {
			kids[p] = append(kids[p], nodes[i].ID)
		}
	}

	// cand is the live candidate set per unresolved entry. Only entries that
	// stated no offset appear here; a stated offset is never reconsidered, since
	// the source's own Date header outranks anything inferred about it.
	cand := map[int64][]int{}
	for i := range nodes {
		n := &nodes[i]
		if n.Off != nil {
			out[n.ID] = Resolution{State: Stated, Off: *n.Off, Evidence: "stated by the source"}
			st.Stated++
			continue
		}
		if c := quoterOffsets(n, kids, byID, places); len(c) > 0 {
			cand[n.ID] = c
		}
	}

	// Tightening propagates: fixing one entry narrows its parent and its
	// children, which can fix them in turn. Iterating to a fixpoint is the whole
	// of the transitive reasoning, and on 34 entries with 33 edges it settles in
	// two or three passes. A cap because a parent cycle — which the store does
	// not forbid — would otherwise spin.
	offered := make(map[int64]int, len(cand))
	for id, c := range cand {
		offered[id] = len(c)
	}
	for pass := 0; pass <= len(nodes); pass++ {
		changed := false
		for _, id := range sortedKeys(cand) {
			lo, hi := bounds(id, byID, kids, out)
			var kept []int
			for _, off := range cand[id] {
				if from, to := instant(byID[id].Wall, off); !to.Before(lo) && !from.After(hi) {
					kept = append(kept, off)
				}
			}
			if len(kept) != len(cand[id]) {
				changed = true
			}
			cand[id] = kept
			if len(kept) == 1 {
				n := byID[id]
				out[id] = Resolution{
					State:    Inferred,
					Off:      kept[0],
					Evidence: inferredWhy(n, kids, byID, places, kept[0]),
				}
				delete(cand, id)
				st.Inferred++
				if offered[id] > 1 {
					st.Selected++
				}
				changed = true
			}
		}
		if !changed {
			break
		}
	}

	// Whatever is left never came down to one answer.
	for _, id := range sortedKeys(cand) {
		if len(cand[id]) == 0 {
			st.Rejected++
			st.Unknown++
			out[id] = Resolution{Evidence: "every zone the quoting client could have used puts this message " +
				"outside the window its neighbours allow, so the inference is wrong rather than the ordering"}
			continue
		}
		st.Ambiguous++
		out[id] = Resolution{
			State: Ambiguous,
			Evidence: fmt.Sprintf("the quoting client could have written this clock at %s, and nothing here chooses between them",
				offsetList(cand[id])),
		}
	}
	for i := range nodes {
		if _, ok := out[nodes[i].ID]; !ok {
			out[nodes[i].ID] = Resolution{Evidence: unknownWhy(&nodes[i], kids, byID, places)}
			st.Unknown++
		}
	}
	return out, st
}

// quoterOffsets is the candidate set for one unlabelled clock: the offsets the
// client that WROTE the sentinel could have been using.
//
// The sender's zone is the intuitive answer and it is the wrong one. An
// attribution sentinel is generated by the client of the person replying, and
// every mail client renders it in that person's own zone — Gmail's "On Tue, 14
// Apr 2026 at 16:35, Tom wrote:" is 16:35 where the replier sits, whatever Tom's
// clock said. Measured on the 583 recovered entries in this corpus that could be
// matched to the mailbox copy of the same message under a different author, the
// clock is at an offset the replier has been observed at 518 times and at one the
// sender has been observed at 216 times. Reading these clocks as sender-local is
// wrong about six times in seven.
//
// The replier is the child in the reply graph. Several children mean the same
// message was recovered from several forwards, each rendered by its own client,
// and the store keeps one clock without recording which sentinel it came from —
// so the union is taken and the ordering constraints are left to sort it out. A
// union that does not narrow to one is reported as ambiguous, which is the
// honest reading of "we cannot tell which forward this clock came from".
func quoterOffsets(n *Node, kids map[int64][]int64, byID map[int64]*Node, places map[int64]Place) []int {
	// The instant is unknown to within the width of the world's offsets, so a
	// daylight-saving transition can fall inside the window. Both sides are
	// offered rather than one chosen here.
	lo, hi := unnest.Span(unnest.Stamp{Wall: n.Wall})
	var out []int
	for _, k := range kids[n.ID] {
		q := byID[k]
		if q == nil {
			continue
		}
		for _, off := range places[q.Person].Candidates(lo, hi) {
			out = appendUnique(out, off)
		}
	}
	return out
}

// bounds is the window an entry's instant must fall in, given its neighbours.
//
// A quoted message was sent before the message quoting it; that is the reply
// graph, not a clock, and it holds however wrong every zone on the page is. So
// this entry cannot precede the earliest its parent could have been sent, nor
// follow the latest any of its children could have been.
//
// The neighbours' windows come from unnest.Span, which is the same definition
// -chrono uses to decide that an ordering is impossible: a neighbour with no
// known offset is 26 hours wide, and a candidate is only rejected when no zone
// assignment for that neighbour could have rescued it. Anything narrower would
// reject candidates over a Sydney reply to a London mail, which is a zone
// artefact and not a contradiction.
func bounds(id int64, byID map[int64]*Node, kids map[int64][]int64, out map[int64]Resolution) (lo, hi time.Time) {
	lo, hi = time.Unix(-1<<62, 0), time.Unix(1<<62-1, 0)
	n := byID[id]
	if p := byID[n.Parent]; p != nil {
		if l, _ := window(p, out); l.After(lo) {
			lo = l
		}
	}
	for _, k := range kids[id] {
		c := byID[k]
		if c == nil {
			continue
		}
		if _, h := window(c, out); h.Before(hi) {
			hi = h
		}
	}
	return lo, hi
}

// window is one node's span of possible instants, narrowing as its offset
// becomes known.
//
// Three widths, and the differences between them are the whole discipline:
//
//   - a stated offset means Wall already IS the instant, to the second, and
//     there is no window to widen;
//   - an inferred offset places the instant to the minute the sentinel wrote,
//     which is a minute wide because a sentinel truncates. Without that minute a
//     reply quoted as 10:00 would be ruled out by a parent whose Date header
//     says 10:00:45, and the rejection would look like evidence against the
//     zone when it is only evidence of rounding;
//   - no offset is the 26 hours of unnest.Span, which is the definition -chrono
//     uses and is not to be narrowed here. A candidate rejected against a
//     neighbour this wide is rejected because no zone on earth could have
//     rescued it.
func window(n *Node, out map[int64]Resolution) (lo, hi time.Time) {
	r, ok := out[n.ID]
	switch {
	case ok && r.State == Stated:
		return n.Wall, n.Wall
	case ok && r.State == Inferred:
		return instant(n.Wall, r.Off)
	}
	return unnest.Span(unnest.Stamp{Wall: n.Wall})
}

// instant is the window a wall clock read at a known offset denotes. A sentinel
// states whole minutes, so the second is unknown and the window is a minute
// wide; the caller must not treat either end as exact.
func instant(wall time.Time, off int) (from, to time.Time) {
	from = wall.Add(-time.Duration(off) * time.Minute)
	return from, from.Add(time.Minute - time.Second)
}

// inferredWhy names the offset and the evidence for it, so a reader can weigh
// the claim rather than take it. The evidence is about the quoting client's
// owner, because that is whose zone the clock is in.
func inferredWhy(n *Node, kids map[int64][]int64, byID map[int64]*Node, places map[int64]Place, off int) string {
	return fmt.Sprintf("%s: this clock was written by the client that quoted the message, and %s",
		FormatOffset(off), quoterEvidence(n, kids, byID, places))
}

func quoterEvidence(n *Node, kids map[int64][]int64, byID map[int64]*Node, places map[int64]Place) string {
	for _, k := range kids[n.ID] {
		q := byID[k]
		if q == nil {
			continue
		}
		if p, ok := places[q.Person]; ok && (p.Verdict == Placed || p.Verdict == Moved) {
			return "its owner " + p.Why()
		}
	}
	return "its owner's zone was the only candidate the ordering allowed"
}

func unknownWhy(n *Node, kids map[int64][]int64, byID map[int64]*Node, places map[int64]Place) string {
	if len(kids[n.ID]) == 0 {
		return "nothing in this set quotes this message, so no client rendered its clock for us"
	}
	for _, k := range kids[n.ID] {
		if q := byID[k]; q != nil {
			return "the client that quoted this message belongs to someone who " + places[q.Person].Why()
		}
	}
	return "the client that quoted this message belongs to someone who has never stated an offset"
}

func sortedKeys[V any](m map[int64]V) []int64 {
	out := make([]int64, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
