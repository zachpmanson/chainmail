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
// Two kinds of evidence, and they are the same claim: a client stating the zone
// it is in. A Date header states it about the message being sent; a sentinel
// states it about a message being quoted, and render_offsets holds the ones a
// twin collapse measured exactly. Neither is an inference.
//
// The rendered ones are the only evidence that reaches an Exchange account
// stamping +0000 on its own mail: 7 of the people measured here have never
// stated anything but UTC in a header, and 5 of them render at +1000, +1100 or
// +1200. Without these they stay UTCOnly, which costs the inference every
// message they ever quoted — and the two who really do render at +0000 are
// dropped by withoutBareUTC exactly as their headers already were.
func (s *Store) ZoneObservations() (map[int64][]tzinfer.Observation, error) {
	rows, err := s.db.Query(`
		select person_id, ts, tz_offset, 0 as measured from entries
		where tz_offset is not null and person_id is not null
		union all
		select person_id, at, off, 1 from render_offsets
		order by 1, 2`)
	if err != nil {
		return nil, fmt.Errorf("reading stated offsets: %w", err)
	}
	defer rows.Close()
	out := map[int64][]tzinfer.Observation{}
	for rows.Next() {
		var person int64
		var ts int64
		var off int
		var measured bool
		if err := rows.Scan(&person, &ts, &off, &measured); err != nil {
			return nil, err
		}
		out[person] = append(out[person], tzinfer.Observation{
			At: time.Unix(ts, 0).UTC(), Off: off, Measured: measured})
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
