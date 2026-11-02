package corpus

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

type twinsWorkMetadata struct {
	Revision            int64
	Full                sql.NullInt64
	Rules, CoveredRules int
	Ready               bool
	Reason              string
}

// TwinsWorkState is independent of the coarse twins -> repair checkpoint.
// Its counter is owned by these triggers, never by source-trigger ordering.
type TwinsWorkState struct {
	twinsWorkMetadata
	PendingAuthors int
}

func (r TwinsWorkState) Clean() bool {
	return r.Ready && r.Rules == reconciliationVersion && r.CoveredRules == reconciliationVersion && !r.Full.Valid && r.PendingAuthors == 0
}

func readTwinsWork(q corpusQuery) (TwinsWorkState, error) {
	var r TwinsWorkState
	err := q.QueryRow(`select work_revision,full_revision,rules_version,covered_version,ready,coalesce(full_reason,''),(select count(*) from twins_dirty_people) from twins_work_state where id=1`).Scan(&r.Revision, &r.Full, &r.Rules, &r.CoveredRules, &r.Ready, &r.Reason, &r.PendingAuthors)
	if err == nil && (r.Rules > reconciliationVersion || r.CoveredRules > reconciliationVersion) {
		err = fmt.Errorf("twins work version is newer than supported version %d", reconciliationVersion)
	}
	return r, err
}

func readTwinsWorkMetadata(q corpusQuery) (twinsWorkMetadata, error) {
	var r twinsWorkMetadata
	err := q.QueryRow(`select work_revision,full_revision,rules_version,covered_version,ready,coalesce(full_reason,'') from twins_work_state where id=1`).Scan(&r.Revision, &r.Full, &r.Rules, &r.CoveredRules, &r.Ready, &r.Reason)
	if err == nil && (r.Rules > reconciliationVersion || r.CoveredRules > reconciliationVersion) {
		err = fmt.Errorf("twins work version is newer than supported version %d", reconciliationVersion)
	}
	return r, err
}

func (s *Store) TwinsWorkState() (TwinsWorkState, error) {
	r, err := readTwinsWork(s.db)
	if errors.Is(err, sql.ErrNoRows) {
		_, err = s.db.Exec(`insert into twins_work_state(id,work_revision,full_revision,full_reason) select 1,coalesce(max(revision),0)+1,coalesce(max(revision),0)+1,'missing-state' from twins_dirty_people where true on conflict do nothing`)
		if err == nil {
			return readTwinsWork(s.db)
		}
	}
	return r, err
}

type twinToken struct{ person, revision int64 }
type twinWork struct {
	state  TwinsWorkState
	tokens []twinToken
}

func captureTwinWork(q corpusQuery) (twinWork, error) {
	var work twinWork
	var err error
	work.state.twinsWorkMetadata, err = readTwinsWorkMetadata(q)
	if err != nil {
		return work, err
	}
	rows, err := q.Query(`select person_id,revision from twins_dirty_people order by person_id`)
	if err != nil {
		return work, err
	}
	defer rows.Close()
	for rows.Next() {
		var token twinToken
		if err := rows.Scan(&token.person, &token.revision); err != nil {
			return work, err
		}
		work.tokens = append(work.tokens, token)
	}
	work.state.PendingAuthors = len(work.tokens)
	return work, rows.Err()
}

func (work twinWork) acknowledge(w corpusWriter, full bool) error {
	current, err := readTwinsWorkMetadata(w)
	if err != nil {
		return err
	}
	if current.Rules != work.state.Rules || current.CoveredRules != work.state.CoveredRules || current.Ready != work.state.Ready || current.Revision < work.state.Revision {
		return ErrReconcileConflict
	}
	for start := 0; start < len(work.tokens); start += 100 {
		end := min(start+100, len(work.tokens))
		pairs, args := make([]string, 0, end-start), make([]any, 0, 2*(end-start))
		for _, token := range work.tokens[start:end] {
			pairs = append(pairs, "(?,?)")
			args = append(args, token.person, token.revision)
		}
		if _, err := w.Exec(`delete from twins_dirty_people where (person_id,revision) in (`+strings.Join(pairs, ",")+`)`, args...); err != nil {
			return err
		}
	}
	if full {
		// A newly raised full epoch and all maintenance-created author tokens survive.
		if _, err := w.Exec(`update twins_work_state set ready=1,rules_version=?,covered_version=?,full_reason=case when full_revision is ? then null else full_reason end,full_revision=case when full_revision is ? then null else full_revision end where id=1`, reconciliationVersion, reconciliationVersion, work.state.Full, work.state.Full); err != nil {
			return err
		}
	}
	return nil
}

// ReconcileTwinsContext consumes captured work only. The public CollapseTwins
// APIs remain exhaustive and never acknowledge this queue, including dry runs.
func ReconcileTwinsContext(ctx context.Context, s *Store) (TwinPlan, error) {
	_, err := readTwinsWorkMetadata(s.db)
	if errors.Is(err, sql.ErrNoRows) {
		_, err = s.TwinsWorkState()
	}
	if err != nil {
		return TwinPlan{}, err
	}
	return runTwinsContext(ctx, s, true, true, s.guardedReconcileWrite)
}

func twinsWorkMigration() string {
	sql := `create table twins_work_state (
 id integer primary key check(id=1),work_revision integer not null default 1,
 full_revision integer default 1,rules_version integer not null default 3,
 covered_version integer not null default 0,ready integer not null default 0,full_reason text default 'initial');
 insert into twins_work_state(id) values(1);
 create table twins_dirty_people(person_id integer primary key,revision integer not null);
 create index sightings_seen_in on sightings(seen_in,entry_id);
 `
	enqueue := func(selects []string) string {
		// DISTINCT also prevents SQLite flattening a lone branch across the gate.
		if len(selects) == 1 {
			selects[0] = strings.Replace(selects[0], "select ", "select distinct ", 1)
		}
		// CROSS JOIN keeps the singleton gate outside the author UNION/fanout.
		return `insert into twins_dirty_people(person_id,revision) select person_id,gate.work_revision from twins_work_state gate cross join (` + strings.Join(selects, " union ") + `) where gate.id=1 and gate.full_revision is null and person_id is not null on conflict(person_id) do update set revision=excluded.revision;`
	}
	endpoints := func(ids string) []string {
		return []string{`select person_id from entries where source='mail' and id in (` + ids + `)`, `select e.person_id from sightings s join entries e on e.id=s.entry_id where e.source='mail' and s.seen_in in (` + ids + `)`}
	}
	trigger := func(name, when, table, guard, structural, reason, body string) {
		sql += `create trigger twins_` + name + ` ` + when + ` on ` + table
		if guard != "" {
			sql += ` when ` + guard
		}
		// All RHS expressions see the old singleton; counter and epoch advance together.
		bump := `insert into twins_work_state(id) values(1) on conflict(id) do update set work_revision=work_revision+1,full_revision=case when full_revision is not null then work_revision+1 when ` + structural + ` then work_revision+1 else null end,full_reason=case when ` + structural + ` then ` + reason + ` else full_reason end;`
		sql += ` begin ` + bump + body + ` end;`
	}
	oldAuthor := `select old.person_id as person_id where old.source='mail'`
	newAuthor := `select new.person_id as person_id where new.source='mail'`
	trigger("entries_insert", "after insert", "entries", "", "new.parent_id is not null or exists(select 1 from entries where parent_id=new.id)", "'graph-edge'", enqueue(append(endpoints("new.id"), newAuthor)))
	trigger("entries_delete", "before delete", "entries", "", "1", "'entry-delete'", enqueue(append(endpoints("old.id"), oldAuthor)))
	columns := []string{"id", "source", "ext_id", "ts", "person_id", "container", "parent_id", "subject", "body_html", "body_text", "quoted", "derived"}
	guards := make([]string, 0, len(columns))
	for _, col := range columns {
		guards = append(guards, "old."+col+" is not new."+col)
	}
	trigger("entries_update", "before update of "+strings.Join(columns, ","), "entries", strings.Join(guards, " or "), "old.id is not new.id or old.parent_id is not new.parent_id", "case when old.id is not new.id then 'entry-id' else 'graph-edge' end", enqueue(append(endpoints("old.id,new.id"), oldAuthor, newAuthor)))
	for _, op := range []string{"insert", "delete", "update"} {
		var refs []string
		if op != "insert" {
			refs = append(refs, "old.entry_id", "old.seen_in")
		}
		if op != "delete" {
			refs = append(refs, "new.entry_id", "new.seen_in")
		}
		when := "before " + op
		if op == "insert" {
			when = "after insert"
		}
		guard := ""
		if op == "update" {
			when += " of entry_id,seen_in,kind"
			guard = "old.entry_id is not new.entry_id or old.seen_in is not new.seen_in or old.kind is not new.kind"
		}
		trigger("sightings_"+op, when, "sightings", guard, "0", "null", enqueue(endpoints(strings.Join(refs, ","))))
		refs = nil
		if op != "insert" {
			refs = append(refs, "old.entry_id")
		}
		if op != "delete" {
			refs = append(refs, "new.entry_id")
		}
		when = "before " + op
		if op == "insert" {
			when = "after insert"
		}
		guard = ""
		if op == "update" {
			when += " of entry_id"
			guard = "old.entry_id is not new.entry_id"
		}
		trigger("attachments_"+op, when, "attachments", guard, "0", "null", enqueue([]string{`select person_id from entries where source='mail' and id in (` + strings.Join(refs, ",") + `)`}))
	}
	trigger("people_delete", "before delete", "people", "", "0", "null", enqueue([]string{`select old.id as person_id`}))
	trigger("people_update", "before update of id", "people", "old.id is not new.id", "0", "null", enqueue([]string{`select old.id as person_id`, `select new.id as person_id`}))
	return sql
}

func planPendingTwins(q corpusQuery, work twinWork) (TwinPlan, bool, error) {
	r := work.state
	reason := ""
	switch {
	case !r.Ready:
		reason = "initial"
	case r.Rules != reconciliationVersion || r.CoveredRules != reconciliationVersion:
		reason = "version"
	case r.Full.Valid:
		switch r.Reason {
		case "initial", "graph-edge", "entry-id", "entry-delete", "missing-state":
			reason = r.Reason
		default:
			reason = "pending-full"
		}
	}
	if reason != "" {
		plan, err := planTwins(q)
		plan.Mode, plan.Fallback = "full", reason
		return plan, true, err
	}
	people := make([]int64, 0, len(work.tokens))
	selected := map[int64]bool{}
	for _, token := range work.tokens {
		people = append(people, token.person)
		selected[token.person] = true
	}
	plan, err := planTwinsPeople(q, people)
	plan.Mode = "authors"
	if len(people) == 0 {
		plan.Mode = "current"
	}
	if err != nil || (len(plan.Collapse) == 0 && len(plan.Derived) == 0) {
		return plan, false, err
	}
	// Expand in the SAME snapshot, retaining selected plans and normalization.
	all, err := peopleWithQuotedEntries(q)
	if err != nil {
		return plan, true, err
	}
	rest := make([]int64, 0, len(all))
	for _, person := range all {
		if !selected[person] {
			rest = append(rest, person)
		}
	}
	extra, err := planQuotedTwinsPeople(q, rest, true)
	plan.PeopleScanned += extra.PeopleScanned
	plan.BodiesScanned += extra.BodiesScanned
	plan.Comparisons += extra.Comparisons
	plan.Removed += extra.Removed
	plan.Measured += extra.Measured
	plan.WithMailbox += extra.WithMailbox
	plan.QuotedOnly += extra.QuotedOnly
	plan.Annotated += extra.Annotated
	plan.Collapse = append(plan.Collapse, extra.Collapse...)
	plan.Declined = append(plan.Declined, extra.Declined...)
	plan.Derived = append(plan.Derived, extra.Derived...)
	sortTwinPlan(&plan)
	plan.Mode, plan.Fallback = "full", "structural-plan"
	return plan, true, err
}
