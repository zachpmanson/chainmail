package corpus

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Bump when matching, quote parsing, or repair rules change. Catch-up is lazy.
const reconciliationVersion = 1

var ErrReconcileConflict = errors.New("reconciliation inputs changed; work remains pending")

type corpusQuery interface {
	Query(string, ...any) (*sql.Rows, error)
	QueryRow(string, ...any) *sql.Row
}

type corpusWriter interface {
	corpusQuery
	Exec(string, ...any) (sql.Result, error)
}

// ReconcileState is a constant-size durable checkpoint, independent of folds
// and embeddings. A complete unchanged twins -> repair round alone covers it.
type ReconcileState struct {
	Source, Covered     int64
	Rules, CoveredRules int
	Ready               bool
}

func (r ReconcileState) Clean() bool {
	return r.Ready && r.Source == r.Covered && r.Rules == reconciliationVersion && r.CoveredRules == reconciliationVersion
}

func readReconcileState(q corpusQuery) (ReconcileState, error) {
	var r ReconcileState
	err := q.QueryRow(`select source_revision, covered_revision, rules_version, covered_version, ready from reconcile_state where id=1`).Scan(&r.Source, &r.Covered, &r.Rules, &r.CoveredRules, &r.Ready)
	if err == nil && (r.Rules > reconciliationVersion || r.CoveredRules > reconciliationVersion) {
		err = fmt.Errorf("reconciliation version is newer than supported version %d", reconciliationVersion)
	}
	return r, err
}

func (s *Store) ReconciliationState() (ReconcileState, error) {
	r, err := readReconcileState(s.db)
	if errors.Is(err, sql.ErrNoRows) {
		// A restored/missing checkpoint is never evidence of coverage.
		if _, err = s.db.Exec(`insert into reconcile_state(id) values(1) on conflict do nothing`); err == nil {
			return readReconcileState(s.db)
		}
	}
	return r, err
}

// CheckpointReconciliation must only follow a successful complete pair. Source
// writes by maintenance itself remain invalidating, just like outside writes.
func (s *Store) CheckpointReconciliation(ctx context.Context, start ReconcileState) (bool, error) {
	w, err := s.beginWriter(ctx, writerBusyMS*time.Millisecond)
	if err != nil {
		return false, err
	}
	defer w.close()
	current, err := readReconcileState(w)
	if err != nil {
		return false, err
	}
	if current.Source != start.Source || current.Rules != start.Rules {
		return false, nil
	}
	res, err := w.ExecContext(ctx, `update reconcile_state set ready=1, covered_revision=?, rules_version=?, covered_version=? where id=1 and source_revision=? and rules_version=?`, start.Source, reconciliationVersion, reconciliationVersion, start.Source, start.Rules)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n == 1, w.commit(ctx)
}

// guardedReconcileWrite validates a snapshot before applying any destructive
// decisions. Carry only this transaction's revisions into the remaining plan.
func (s *Store) guardedReconcileWrite(ctx context.Context, expected *int64, apply func(corpusWriter) error) (time.Duration, error) {
	at := time.Now()
	w, err := s.beginWriter(ctx, writerBusyMS*time.Millisecond)
	wait := time.Since(at)
	if err != nil {
		return wait, err
	}
	defer w.close()
	r, err := readReconcileState(w)
	if err != nil {
		return wait, err
	}
	if r.Source != *expected {
		return wait, ErrReconcileConflict
	}
	if err = apply(w); err != nil {
		return wait, err
	}
	r, err = readReconcileState(w)
	if err != nil {
		return wait, err
	}
	if err = w.commit(ctx); err != nil {
		return wait, err
	}
	*expected = r.Source
	return wait, nil
}

func reconcileMigration() string {
	sql := `create table reconcile_state (
 id integer primary key check(id=1), source_revision integer not null default 1,
 covered_revision integer not null default 0, rules_version integer not null default 1,
 covered_version integer not null default 0, ready integer not null default 0);
 insert into reconcile_state(id) values(1);`
	// Only actual inputs, not hashes, ingest timestamps, labels, hydration or
	// derived-index progress. All sources count: shared people and hosts cross mail.
	inputs := []struct{ table, columns string }{
		{"entries", "id,source,ext_id,kind,ts,tz,tz_offset,person_id,container,parent_id,parent_ref,subject,body_html,body_text,quoted,derived"},
		{"mail_detail", "entry_id,gmail_id,message_id,in_reply_to,refs,from_addr,to_addr,cc_addr"},
		{"sightings", "entry_id,seen_in,kind"},
		{"attachments", "entry_id"},
		{"people", "id,display_name"},
		{"identities", "person_id,kind,value"},
		{"participants", "entry_id,person_id,role"},
		{"domain_aliases", "from_domain,to_domain"},
	}
	for _, in := range inputs {
		bump := ` insert into reconcile_state(id) values(1) on conflict(id) do update set source_revision=source_revision+1; `
		for _, op := range []string{"insert", "delete"} {
			sql += fmt.Sprintf("create trigger reconcile_%s_%s after %s on %s begin %s end;", in.table, op, op, in.table, bump)
		}
		var guards []string
		for _, col := range strings.Split(in.columns, ",") {
			guards = append(guards, "old."+col+" is not new."+col)
		}
		sql += fmt.Sprintf("create trigger reconcile_%s_update after update of %s on %s when %s begin %s end;", in.table, in.columns, in.table, strings.Join(guards, " or "), bump)
	}
	return sql
}
