package corpus

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/zachpmanson/chainmail/internal/boiler"
)

// SelectedFolds returns only requested entries; empty means no work. Unlike the
// legacy BoilerplateFor scope map, evidence always comes from persisted counts.
func (s *Store) SelectedFolds(ctx context.Context, ids []int64, r boiler.Rules) (map[int64]boiler.Fold, error) {
	out := map[int64]boiler.Fold{}
	err := s.WithFoldSnapshot(ctx, ids, r, func(_ *sql.Tx, folds map[int64]boiler.Fold) error { out = folds; return nil })
	return out, err
}

// WithFoldSnapshot loads selected folds and runs read inside the same clean
// source snapshot. Refresh never upgrades a deferred read transaction. The
// callback must not write, commit, or retain the transaction.
func (s *Store) WithFoldSnapshot(ctx context.Context, ids []int64, r boiler.Rules, read func(*sql.Tx, map[int64]boiler.Fold) error) error {
	if len(ids) == 0 {
		return nil
	}
	r = boiler.FilledRules(r)
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("waiting for clean fold snapshot: %w", err)
		}
		tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
		if err != nil {
			return err
		}
		st, err := readFoldState(ctx, tx, r.Window)
		if err != nil {
			tx.Rollback()
			return err
		}
		if st.dirty {
			tx.Rollback()
			if _, err = s.RefreshFoldIndex(ctx, r); err != nil {
				return err
			}
			continue
		}
		folds, err := selectedFoldRows(ctx, tx, ids, r)
		if err == nil {
			err = read(tx, folds)
		}
		tx.Rollback()
		return err
	}
}

func selectedFoldRows(ctx context.Context, tx *sql.Tx, ids []int64, r boiler.Rules) (map[int64]boiler.Fold, error) {
	ph, args := placeholders(ids)
	// The group and fingerprint primary keys are probed once per selected tail.
	rows, err := tx.QueryContext(ctx, selectedFoldQuery(ph), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]boiler.Fold{}
	for rows.Next() {
		var id int64
		var n, ac, as, dc, ds int
		if err = rows.Scan(&id, &n, &ac, &as, &dc, &ds); err != nil {
			return nil, err
		}
		if n < r.MinLines {
			continue
		}
		if as > 1 {
			return nil, errors.New("invalid author fold sender count")
		}
		if ac >= r.AuthorRepeats && as == 1 {
			out[id] = boiler.Fold{Lines: n, Count: ac, Senders: as, Scope: boiler.Author}
		}
		if dc >= r.DomainRepeats && ds >= r.DomainSenders && n > out[id].Lines {
			out[id] = boiler.Fold{Lines: n, Count: dc, Senders: ds, Scope: boiler.Domain}
		}
	}
	return out, rows.Err()
}

func selectedFoldQuery(ph string) string {
	return `select m.entry_id,t.n,
 coalesce(a.count,0),coalesce(a.senders,0),coalesce(d.count,0),coalesce(d.senders,0)
 from fold_members m join fold_tails t on t.entry_id=m.entry_id
 left join fold_counts a on a.kind=1 and a.group_key=cast(m.person_id as text) and a.fp=t.fp
 left join fold_counts d on d.kind=2 and d.group_key=m.domain and d.fp=t.fp
 where m.entry_id in (` + ph + `) and m.ok=1 and t.n<m.line_count
 order by m.entry_id,t.n`
}
