package corpus

import (
	"database/sql"
	"fmt"
	"sort"
	"time"
)

// The reply graph is one parent_id edge per entry, and every writer of an edge
// has to keep it acyclic. It stays acyclic on its own; the corruption is that
// each edge was honest from where it was written and only becomes a ring when
// another edge lands beside it:
//
//   - a quoted copy sits INSIDE the message that quoted it, so recovery points
//     the copy at its host, and the host's own reply edge can point back at
//     whatever the copy is a copy of — two edges, both right, a ring;
//   - a twins collapse adopts the dropped copy's parent onto the survivor (or
//     repoints the dropped copy's children), and where the dropped copy was a
//     quote of the survivor inside a reply to it, that adopt closes the same
//     loop;
//   - a derived copy is re-parented onto its base from a later sighting, and
//     the same reply-back pattern applies.
//
// A ring is not a local nuisance: Store.Chain walks up to a root and down
// again, and a cycle has no root, so a walk that enters one returns nothing —
// a chain that exists reads as one that does not. The guards below decline an
// edge that would close a cycle at write time, and RepairGraph severs the
// edges a ring is already made of.

// graphQuery is the smallest reader the guards need, satisfied by both *Store
// (via its db handle) and *sql.Tx, whichever side is writing the edge.
type graphQuery interface {
	Query(query string, args ...any) (*sql.Rows, error)
}

// closesCycle reports whether writing child -> parent would close a cycle:
// parent is the child itself, or already one of the child's ancestors. Any
// such edge makes the walk that follows parent return to child and then to
// parent again, so it is refused rather than written. Bounded because a graph
// that already has a ring has no root to stop on; the recursive union already
// dedupes, and the depth bound is only a belt against a pathological walk.
func closesCycle(q graphQuery, child, parent int64) (bool, error) {
	if child == 0 || parent == 0 || child == parent {
		return true, nil
	}
	rows, err := q.Query(`
		with recursive up(id, depth) as (
		  select ?1, 0
		  union all
		  select e.parent_id, up.depth + 1 from entries e join up on e.id = up.id
		    where e.parent_id is not null and up.depth < 200
		)
		select 1 from up where id = ?2 limit 1`,
		parent, child)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	return rows.Next(), rows.Err()
}

// GraphSevered is one edge a repair cut, named by the entries either side and
// the reason its edge was the one to go.
type GraphSevered struct {
	Child, Parent int64
	ChildExt      string
	ParentExt     string
	// Why is the reason this edge was chosen over the ring's other edges.
	Why string
}

// GraphRepair is what RepairGraph did: every ring edge it severed.
type GraphRepair struct {
	Edges   int
	Severed []GraphSevered
}

// RepairGraph severs one edge in every ring of the reply graph, so that no
// walk of parent_id can loop. Deterministic: each ring's seam goes, the edge
// whose parent is newest relative to its child — the edge drawn against the
// direction of time, where an adopt or a resolution closed a loop. The scores
// of a ring telescope to zero, so the edge is a theorem rather than a guess.
// Every entry has one parent, so the rings are disjoint and each needs exactly
// one sever.
func (s *Store) RepairGraph() (GraphRepair, error) {
	var rep GraphRepair

	type node struct {
		parent int64
		ts     time.Time
		ext    string
	}
	rows, err := s.db.Query(
		`select id, coalesce(parent_id,0), ts, ext_id from entries`)
	if err != nil {
		return rep, err
	}
	nodes := map[int64]node{}
	for rows.Next() {
		var id, parent int64
		var ts int64
		var ext string
		if err := rows.Scan(&id, &parent, &ts, &ext); err != nil {
			rows.Close()
			return rep, err
		}
		nodes[id] = node{parent: parent, ts: time.Unix(ts, 0), ext: ext}
	}
	if err := rows.Close(); err != nil {
		return rep, err
	}

	// One ring at a time: walk up from any node not yet known acyclic, and when
	// the walk revisits a node, that node onwards is a ring. prove follows a
	// walk that reached a root, so each node is walked at most once per pass.
	for {
		path := []int64{}
		pos := map[int64]int{}
		proven := map[int64]bool{}
		var ring []int64
		for start := range nodes {
			if proven[start] {
				continue
			}
			cur := start
			for cur != 0 {
				if proven[cur] {
					break
				}
				if i, ok := pos[cur]; ok {
					ring = append([]int64(nil), path[i:]...)
					break
				}
				pos[cur] = len(path)
				path = append(path, cur)
				cur = nodes[cur].parent
			}
			if ring != nil {
				break
			}
			for _, n := range path {
				proven[n] = true
			}
			path = path[:0]
		}
		if ring == nil {
			break
		}

		// The ring is child -> parent around the loop; cut the edge whose parent
		// is newest relative to its child (max(parent.ts - child.ts)). That edge
		// is the seam: a reply edge points backwards in time (parent older than
		// child), so every edge of a healthy chain scores negative, and the
		// scores of a ring telescope to zero — the seam is the one edge that
		// must score non-negative, and the most positive of them is where the
		// ring was drawn against the direction of time. Ties broken by the
		// larger parent id, so the choice is a function of the graph alone.
		worst := ring[0]
		worstScore := nodes[nodes[worst].parent].ts.Sub(nodes[worst].ts)
		worstParent := nodes[worst].parent
		for _, c := range ring[1:] {
			p := nodes[c].parent
			score := nodes[p].ts.Sub(nodes[c].ts)
			pick := score > worstScore || (score == worstScore && p > worstParent)
			if pick {
				worst, worstScore, worstParent = c, score, p
			}
		}
		why := "an anachronistic ring edge"
		if worstScore == 0 {
			why = "a ring edge between same-instant messages"
		}
		rep.Severed = append(rep.Severed, GraphSevered{
			Child: worst, Parent: nodes[worst].parent,
			ChildExt: nodes[worst].ext, ParentExt: nodes[nodes[worst].parent].ext,
			Why: why,
		})
		n := nodes[worst]
		n.parent = 0
		nodes[worst] = n
	}
	rep.Edges = len(rep.Severed)
	if rep.Edges == 0 {
		return rep, nil
	}
	sort.Slice(rep.Severed, func(i, j int) bool {
		return rep.Severed[i].Child < rep.Severed[j].Child
	})
	tx, err := s.db.Begin()
	if err != nil {
		return rep, err
	}
	defer tx.Rollback()
	for _, e := range rep.Severed {
		if _, err := tx.Exec(
			`update entries set parent_id = null where id = ?`, e.Child); err != nil {
			return rep, fmt.Errorf("severing %s -> %s: %w", e.ChildExt, e.ParentExt, err)
		}
	}
	return rep, tx.Commit()
}
