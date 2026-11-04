package spec

import (
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// A fork is independent authored content, not another wrapper around the same
// words. Cells own only text outside nested tables; child tables own the rest.
type analysisTableLayout struct {
	children    []*analysisTableLayout
	cells       []originalFacts
	facts       originalFacts
	start       float64
	forks       int
	valid, fork bool
}

// Three nested content forks distinguish composed layouts from a data table in
// a wrapper. Coverage and the early primary branch keep a small appended widget
// from borrowing a plain enclosing table's vote. This remains one global vote.
func (t *analysisTree) complexNestedTable(body *html.Node) bool {
	var tables []*analysisTableLayout
	area := 0.0
	var walk func(*html.Node, *analysisTableLayout, *originalFacts) originalFacts
	walk = func(n *html.Node, owner *analysisTableLayout, cell *originalFacts) originalFacts {
		d := t.nodes[n]
		if n.DataAtom == atom.Footer || contactShape(d.facts) || n.DataAtom == atom.A && (d.facts.contacts > 0 || d.facts.actions > 0) {
			return originalFacts{}
		}
		var table *analysisTableLayout
		if n.DataAtom == atom.Table {
			table = &analysisTableLayout{start: area, valid: true}
			tables = append(tables, table)
			if owner != nil {
				owner.children = append(owner.children, table)
			}
			owner, cell = table, nil
		}
		// Check retained native table semantics, never prepare's pre-pruning
		// cell count. Explicit non-table display cannot establish these forks.
		expected := ""
		switch n.DataAtom {
		case atom.Table:
			expected = "table"
		case atom.Tr:
			expected = "table-row"
		case atom.Td, atom.Th:
			expected = "table-cell"
		case atom.Tbody:
			expected = "table-row-group"
		case atom.Thead:
			expected = "table-header-group"
		case atom.Tfoot:
			expected = "table-footer-group"
		}
		if owner != nil && expected != "" {
			display := d.style["display"]
			if display != "" && display != expected && !(n.DataAtom == atom.Table && display == "inline-table") {
				owner.valid = false
			}
			if n.DataAtom == atom.Td || n.DataAtom == atom.Th {
				if n.Parent == nil || n.Parent.DataAtom != atom.Tr || attr(n, "rowspan") != "" && attr(n, "rowspan") != "1" {
					owner.valid = false
				}
				cell = &originalFacts{}
			}
		}
		f := originalFacts{}
		if n.Type == html.TextNode {
			f = d.facts
			area += f.area
			if cell != nil {
				cell.add(f, false)
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			f.add(walk(c, owner, cell), false)
		}
		if owner != nil && (n.DataAtom == atom.Td || n.DataAtom == atom.Th) {
			owner.cells = append(owner.cells, *cell)
		}
		if table != nil {
			table.facts = f
			branches, words := 0, 0
			add := func(f originalFacts) {
				if f.words >= 4 {
					branches++
					words += f.words
				}
			}
			for _, cell := range table.cells {
				add(cell)
			}
			for _, child := range table.children {
				if child.valid {
					add(child.facts)
					table.forks = max(table.forks, child.forks)
				}
			}
			table.fork = table.valid && branches >= 2 && words >= 16
			if table.fork {
				table.forks++
			}
		}
		return f
	}
	walk(body, nil, nil)
	total := t.nodes[body].facts
	for _, table := range tables {
		f := table.facts
		if !table.valid || !table.fork || table.forks < 3 || f.words < 40 || f.words*2 < total.words || f.area*2 < total.area {
			continue
		}
		for _, child := range table.children {
			if child.valid && child.forks >= 2 && child.facts.words*3 >= f.words && child.facts.area*3 >= f.area && (child.start-table.start)*2 < f.area {
				return true
			}
		}
	}
	return false
}
