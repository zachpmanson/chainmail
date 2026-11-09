package corpus

const reactionAgreedSQL = ` select r.source_ext_id,r.entry_id as source_id,min(i.emoji) as emoji,min(i.target_ref) as target_ref
 from reaction_sources r join reaction_inspections i on i.source_ext_id=r.source_ext_id
 where r.entry_id is not null group by r.source_ext_id
 having min(i.candidate)=1 and count(distinct i.semantic)=1 and min(i.snapshot)=r.current_snapshot`

const reactionResolutionSQL = ` select a.source_ext_id,a.source_id,min(t.entry_id) as target_id,count(distinct t.entry_id) as targets
 from reaction_agreed a join reaction_message_refs t on t.reference=a.target_ref
 join gmail_copies tc on tc.entry_id=t.entry_id
 join reaction_inspections i on i.source_ext_id=a.source_ext_id and i.account_id=tc.account_id
 join gmail_copies sc on sc.account_id=i.account_id and sc.gmail_id=i.gmail_id and sc.entry_id=a.source_id
 join gmail_accounts ga on ga.id=sc.account_id
 group by a.source_ext_id`

const reactionResolvedSQL = `select source_ext_id,source_id,target_id from (` + reactionResolutionSQL + `) where targets=1 and target_id!=source_id`

const reactionActiveSQL = ` select a.* from reaction_resolved a
 where not exists(select 1 from sightings g where g.seen_in=a.source_id)
 -- A reaction targeting another reaction cannot disappear into a hidden node.
 and not exists(select 1 from reaction_agreed b where b.source_id=a.target_id)`

// Appended sidecars only; source bodies, mailbox labels and FTS contents remain
// intact. Search filters active sources instead of destroying their source text.
func reactionsMigration() string {
	sql := `create table reaction_applies(digest text primary key,manifest text not null,withdrawn_sightings text not null,applied_at integer not null);
 create table reaction_sources(
 source_ext_id text primary key, entry_id integer references entries(id) on delete set null,current_snapshot text not null);
 create index reaction_sources_entry on reaction_sources(entry_id);
 create table reaction_inspections(
 account_id text not null, gmail_id text not null, source_ext_id text not null,
 digest text not null, evidence text not null, semantic text not null,snapshot text not null,
 candidate integer not null, emoji text not null, target_ref text not null,
 claimed_from text not null, source_date text not null,
 primary key(account_id,gmail_id,source_ext_id,digest,semantic,snapshot));
 create index reaction_inspections_source on reaction_inspections(source_ext_id);
 create table reaction_message_refs(reference text not null,entry_id integer not null references entries(id) on delete cascade,
 primary key(reference,entry_id));
 insert into reaction_message_refs select message_id,entry_id from mail_detail where message_id is not null and message_id!='';
 create view reaction_agreed as ` + reactionAgreedSQL + `;
 create view reaction_resolved as ` + reactionResolvedSQL + `;
 create view reaction_active as ` + reactionActiveSQL + `;
 `
	// Eligibility changes even when BodySHA does not. Dirty fold membership and
	// retire vectors on evidence/target/copy changes, including target deletion.
	for _, table := range []string{"reaction_inspections", "reaction_message_refs", "gmail_copies", "reaction_sources", "sightings"} {
		for _, op := range []string{"insert", "update", "delete"} {
			guard := ""
			if table == "reaction_inspections" {
				side := "new"
				if op == "delete" {
					side = "old"
				}
				guard = " when exists(select 1 from reaction_sources r where r.source_ext_id=" + side + ".source_ext_id)"
			}
			if op == "update" && table == "gmail_copies" {
				guard = " when old.entry_id is not new.entry_id or old.account_id is not new.account_id or old.gmail_id is not new.gmail_id"
			}
			if op == "update" && table == "sightings" {
				guard = " when old.entry_id is not new.entry_id or old.seen_in is not new.seen_in"
			}
			if op == "update" && table == "reaction_sources" {
				guard = " when old.entry_id is not new.entry_id or old.current_snapshot is not new.current_snapshot"
			}
			sql += `create trigger reactions_` + table + `_` + op + ` after ` + op + ` on ` + table + guard + ` begin
 insert into fold_dirty_entries select entry_id from reaction_sources where entry_id is not null on conflict do nothing;
 delete from entry_embeddings where entry_id in (select entry_id from reaction_sources);
 update reconcile_state set source_revision=source_revision+1 where id=1;
 end;`
		}
	}
	return sql
}

const ordinaryEntrySQL = `not exists(select 1 from reaction_active ra where ra.source_id=e.id)`

// Source notification identities must never enter sender/body twin heuristics,
// including unresolved and conflicting candidates that remain ordinary cards.
const nonReactionTwinSQL = `not exists(select 1 from reaction_sources rs join reaction_inspections ri on ri.source_ext_id=rs.source_ext_id where rs.entry_id=e.id and ri.candidate=1)`

// activeReactionForAccountSQL is the same inspected-copy proof used for both
// suppression and activity remapping. An unknown mailbox copy stays ordinary.
func activeReactionForAccountSQL(alias, accountID string) (string, []any) {
	if accountID == "" {
		return "1", nil
	}
	return `exists(select 1 from reaction_inspections ri
 join gmail_copies sc on sc.account_id=ri.account_id and sc.gmail_id=ri.gmail_id and sc.entry_id=` + alias + `.source_id
 join gmail_copies tc on tc.account_id=sc.account_id and tc.entry_id=` + alias + `.target_id
 where ri.source_ext_id=` + alias + `.source_ext_id and sc.account_id=?)`, []any{accountID}
}

func ordinaryForAccountSQL(accountID string) (string, []any) {
	proof, args := activeReactionForAccountSQL("ra", accountID)
	return `not exists(select 1 from reaction_active ra where ra.source_id=e.id and ` + proof + `)`, args
}
