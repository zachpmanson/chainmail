package main

import (
	"fmt"
	"io"
	"sort"
	"text/tabwriter"
	"time"

	"github.com/zachpmanson/chainmail/internal/corpus"
	"github.com/zachpmanson/chainmail/internal/tzinfer"
)

// zonesReport prints what the corpus knows about where people were, and what
// that lets it say about the clocks nobody labelled.
//
// The placement is derived on every run rather than stored, which makes it
// invisible to anything that does not ask — so this is the thing that asks. A
// stored column would be queryable but would go stale the moment new mail
// arrived for anyone, and `people.org` in migration 4 is what that looks like
// after a while: declared, written by nothing, read by nothing.
func zonesReport(w io.Writer, s *corpus.Store, people bool, chain string) error {
	places, err := s.Places()
	if err != nil {
		return err
	}
	nodes, names, err := zoneNodes(s)
	if err != nil {
		return err
	}
	res, st := tzinfer.Resolve(nodes, places)

	byVerdict := map[tzinfer.Verdict]int{}
	for _, p := range places {
		byVerdict[p.Verdict]++
	}
	fmt.Fprintf(w, "people with any stated offset   %d\n", len(places))
	fmt.Fprintf(w, "  placed in one zone            %d\n", byVerdict[tzinfer.Placed])
	fmt.Fprintf(w, "  moved (no zone explains them) %d\n", byVerdict[tzinfer.Moved])
	fmt.Fprintf(w, "  only ever +0000               %d\n", byVerdict[tzinfer.UTCOnly])
	fmt.Fprintf(w, "\nentries %d\n", len(nodes))
	fmt.Fprintf(w, "  stated by the source          %d\n", st.Stated)
	fmt.Fprintf(w, "  inferred                      %d\n", st.Inferred)
	fmt.Fprintf(w, "    of which the ordering chose between several candidates  %d\n", st.Selected)
	fmt.Fprintf(w, "  ambiguous, so left unknown    %d\n", st.Ambiguous)
	fmt.Fprintf(w, "  unknown                       %d\n", st.Unknown)
	fmt.Fprintf(w, "    of which the ordering rejected every candidate        %d\n", st.Rejected)

	if chain != "" {
		if err := chainDetail(w, s, nodes, names, res, chain); err != nil {
			return err
		}
	}
	if !people {
		return nil
	}
	t := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(t, "\nperson\tverdict\tzone\tobserved")
	ids := make([]int64, 0, len(places))
	for id := range places {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return names[ids[i]] < names[ids[j]] })
	for _, id := range ids {
		p := places[id]
		zone := "—"
		if len(p.Zones) > 0 {
			zone = p.Zones[0]
			if len(p.Zones) > 1 {
				zone += fmt.Sprintf(" (+%d that also fit)", len(p.Zones)-1)
			}
		}
		offs := make([]string, len(p.Seen))
		for i, o := range p.Seen {
			offs[i] = tzinfer.FormatOffset(o)
		}
		fmt.Fprintf(t, "%s\t%s\t%s\t%v\n", names[id], p.Verdict, zone, offs)
	}
	return t.Flush()
}

// chainDetail prints one chain entry by entry, which is how a verdict is argued
// with: a distribution says how often the inference declines to answer, and only
// the per-entry reasoning says whether it was right to.
func chainDetail(w io.Writer, s *corpus.Store, nodes []tzinfer.Node, names map[int64]string,
	res map[int64]tzinfer.Resolution, root string) error {
	ids, err := chainIDs(s, root)
	if err != nil {
		return err
	}
	in := map[int64]bool{}
	for _, id := range ids {
		in[id] = true
	}
	t := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintf(t, "\nchain %s — %d entries\n", root, len(ids))
	fmt.Fprintln(t, "id\tstated clock\tsender\tstate\tzone\twhy")
	for _, n := range nodes {
		if !in[n.ID] {
			continue
		}
		r := res[n.ID]
		zone := "—"
		if r.State == tzinfer.Stated || r.State == tzinfer.Inferred {
			zone = tzinfer.FormatOffset(r.Off)
		}
		fmt.Fprintf(t, "%d\t%s\t%s\t%s\t%s\t%s\n", n.ID, n.Wall.Format("2006-01-02 15:04"),
			names[n.Person], r.State, zone, r.Evidence)
	}
	return t.Flush()
}

// chainIDs is the whole connected component of one entry, by reply edges.
func chainIDs(s *corpus.Store, root string) ([]int64, error) {
	rows, err := s.DB().Query(`
		with recursive
		  seed(id) as (select id from entries where ext_id = ?),
		  up(id) as (
		    select id from seed
		    union
		    select e.parent_id from entries e join up on e.id = up.id where e.parent_id is not null
		  ),
		  down(id) as (
		    select id from up
		    union
		    select e.id from entries e join down on e.parent_id = down.id
		  )
		select id from down`, root)
	if err != nil {
		return nil, fmt.Errorf("walking the chain from %s: %w", root, err)
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

// zoneNodes reads the whole corpus as a graph for the resolver, with display
// names alongside for the report.
func zoneNodes(s *corpus.Store) ([]tzinfer.Node, map[int64]string, error) {
	rows, err := s.DB().Query(`
		select e.id, coalesce(e.parent_id, 0), coalesce(e.person_id, 0), e.ts, e.tz_offset,
		       coalesce(p.display_name, '')
		from entries e left join people p on p.id = e.person_id
		order by e.id`)
	if err != nil {
		return nil, nil, fmt.Errorf("reading entries for zone inference: %w", err)
	}
	defer rows.Close()
	var nodes []tzinfer.Node
	names := map[int64]string{}
	for rows.Next() {
		var n tzinfer.Node
		var ts int64
		var off *int
		var name string
		if err := rows.Scan(&n.ID, &n.Parent, &n.Person, &ts, &off, &name); err != nil {
			return nil, nil, err
		}
		n.Wall, n.Off = time.Unix(ts, 0).UTC(), off
		nodes = append(nodes, n)
		if n.Person != 0 {
			names[n.Person] = name
		}
	}
	return nodes, names, rows.Err()
}
