package spec

// derivedEdit is a quoter's in-place change to a quoted message, located in the
// rows it was found among: the COPY the quoter actually pasted (a DERIVED entry,
// decided at ingest — see twins.go), the BASE it modified, and the HOST it was
// sighted inside — the message that re-quoted and therefore carries the change.
//
// All three are the same three rows the relation already names; what this adds
// is only the resolution of the host, which the corpus records as a sighting
// rather than as a column.
type derivedEdit struct {
	Copy *entryRow
	Base *entryRow
	Host *entryRow
}

// derivedEdits finds every quoter's edited copy of a quoted message among rows.
//
// The relation is DECIDED at ingest (FindDerived classifies a modified re-quote
// as derived, not a twin and not unrelated); this only surfaces it, and it is
// shared so that the two renderers of one corpus surface it the same way. A page
// build draws the edit inside the host's bubble (see generate.go's attachEdits);
// the reading pane draws the same bubble over /v1/chains, and a second copy of
// these rules is how the two would come to disagree about whether a message
// carries an edit at all.
//
// Both the base and the host must be among rows. When either is not — the change
// is anchored outside this selection, or the quoting message was never collected
// — the derived copy keeps its own row and link and no edit is attached, so the
// trail is not silently lost. The host is the FIRST sighting that is present,
// which is what makes the choice stable across reads: rows are walked in the
// same order every time, and a copy sighted in several messages is shown in one
// of them rather than in all.
func derivedEdits(rows []*entryRow) []derivedEdit {
	at := map[int64]*entryRow{}
	for _, r := range rows {
		at[r.ID] = r
	}
	var out []derivedEdit
	for _, r := range rows {
		if !r.Derived || r.ParentID == 0 {
			continue // not an edited copy of anything
		}
		base, ok := at[r.ParentID]
		if !ok {
			continue // the base it changed is not here
		}
		var host *entryRow
		for _, h := range r.SeenIn {
			if x, ok := at[h]; ok && x != r {
				host = x
				break
			}
		}
		if host == nil {
			continue // the quoting message is not here either
		}
		out = append(out, derivedEdit{Copy: r, Base: base, Host: host})
	}
	return out
}
