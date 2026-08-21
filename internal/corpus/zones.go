package corpus

import (
	"fmt"
	"time"

	"github.com/zachpmanson/chainmail/internal/tzinfer"
)

// ZoneObservations reads every instant at which someone's own client stated its
// offset, keyed by person.
//
// Corpus-wide on purpose, however small the selection being rendered. A person's
// zone is a fact about them and not about the page they happen to appear on: the
// 34-entry chain this was built for holds 7 stated offsets, while the same people
// state 3,000 across the corpus. Restricting the evidence to the selection would
// make the same message resolve differently depending on what else was rendered
// beside it.
func (s *Store) ZoneObservations() (map[int64][]tzinfer.Observation, error) {
	rows, err := s.db.Query(`
		select person_id, ts, tz_offset from entries
		where tz_offset is not null and person_id is not null
		order by person_id, ts`)
	if err != nil {
		return nil, fmt.Errorf("reading stated offsets: %w", err)
	}
	defer rows.Close()
	out := map[int64][]tzinfer.Observation{}
	for rows.Next() {
		var person int64
		var ts int64
		var off int
		if err := rows.Scan(&person, &ts, &off); err != nil {
			return nil, err
		}
		out[person] = append(out[person], tzinfer.Observation{At: time.Unix(ts, 0).UTC(), Off: off})
	}
	return out, rows.Err()
}

// Places fits every person the corpus has any offset evidence for.
//
// Derived on each call rather than stored in a column. The fit is a fold over
// every message a person ever sent, so one new mail can change it; a stored
// verdict would be correct at ingest and quietly wrong afterwards, and nothing
// would say which rows were computed under which evidence. `people.org` is the
// warning in this schema: declared in migration 4, written by nothing, read by
// nothing. The cost of deriving is that the answer is invisible to anything that
// does not ask, which is what `corpus zones` exists to fix.
func (s *Store) Places() (map[int64]tzinfer.Place, error) {
	obs, err := s.ZoneObservations()
	if err != nil {
		return nil, err
	}
	out := make(map[int64]tzinfer.Place, len(obs))
	for person, o := range obs {
		out[person] = tzinfer.Fit(o)
	}
	return out, nil
}
