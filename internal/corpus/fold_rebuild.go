package corpus

import (
	"context"
	"crypto/sha256"
	"database/sql"

	"github.com/zachpmanson/chainmail/internal/boiler"
)

// Rebuilds have no old contributions to subtract. Stream source bodies once,
// use prepared inserts, then aggregate every per-length occurrence in SQL. The
// same immediate writer transaction still publishes the entire generation.
func rebuildFoldIndex(ctx context.Context, conn *sql.Conn, r boiler.Rules, aliases map[string]string, stats *FoldWarm) error {
	if _, err := conn.ExecContext(ctx, `delete from fold_tails;delete from fold_members;
 delete from fold_counts;delete from fold_sender_refs;`); err != nil {
		return err
	}
	member, err := conn.PrepareContext(ctx, `insert into fold_members values(?,?,?,?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer member.Close()
	tail, err := conn.PrepareContext(ctx, `insert into fold_tails values(?,?,?)`)
	if err != nil {
		return err
	}
	defer tail.Close()
	rows, err := conn.QueryContext(ctx, `select e.id,coalesce(e.person_id,0),coalesce(d.from_addr,''),e.body_text,
 exists(select 1 from sightings g where g.entry_id=e.id and g.kind='direct')
 from entries e left join mail_detail d on d.entry_id=e.id
 where e.source='mail' and e.body_text is not null and e.body_text!=''`)
	if err != nil {
		return err
	}
	defer rows.Close()
	domains := map[int64]string{}
	for rows.Next() {
		if err = ctx.Err(); err != nil {
			return err
		}
		var id, person int64
		var from, text string
		var direct bool
		if err = rows.Scan(&id, &person, &from, &text, &direct); err != nil {
			return err
		}
		digest := sha256.Sum256([]byte(text))
		rawDomain := canonicalDomain(from, nil)
		domain := canonicalDomain(from, aliases)
		if domain == "" {
			var cached bool
			domain, cached = domains[person]
			if !cached {
				domain, err = foldSoleDomain(ctx, conn, person, aliases)
				if err != nil {
					return err
				}
				domains[person] = domain
			}
		}
		stats.Bodies++
		stats.Pending++
		var lines []string
		raw, ok := boiler.Lines(text, direct)
		if ok {
			lines = boiler.Match(raw)
			stats.Lines += len(lines)
		}
		if _, err = member.ExecContext(ctx, id, digest[:], direct, person, rawDomain, domain, ok, len(lines)); err != nil {
			return err
		}
		stats.Memberships++
		for i, t := range boiler.Tails(lines, r.Window) {
			if !t.Valid {
				continue
			}
			if _, err = tail.ExecContext(ctx, id, i+1, t.Digest[:]); err != nil {
				return err
			}
			stats.Tails++
		}
	}
	if err = rows.Err(); err != nil {
		return err
	}
	if err = rows.Close(); err != nil {
		return err
	}
	// Never DISTINCT entry_id: placeholders can contribute the same fingerprint
	// at several lengths. Person zero remains a distinct domain sender.
	_, err = conn.ExecContext(ctx, `insert into fold_sender_refs(domain,fp,person_id,refs)
 select m.domain,t.fp,m.person_id,count(*)
 from fold_members m join fold_tails t on t.entry_id=m.entry_id
 where m.ok=1 and m.domain!='' group by m.domain,t.fp,m.person_id;
 insert into fold_counts(kind,group_key,fp,count,senders)
 select 1,cast(m.person_id as text),t.fp,count(*),1
 from fold_members m join fold_tails t on t.entry_id=m.entry_id
 where m.ok=1 and m.person_id!=0 group by m.person_id,t.fp;
 insert into fold_counts(kind,group_key,fp,count,senders)
 select 2,domain,fp,sum(refs),count(*) from fold_sender_refs group by domain,fp;`)
	if err != nil {
		return err
	}
	return conn.QueryRowContext(ctx, `select count(*) from fold_counts`).Scan(&stats.CounterKeys)
}
