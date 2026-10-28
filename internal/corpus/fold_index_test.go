package corpus

import (
	"context"
	"fmt"
	"math/rand"
	"reflect"
	"testing"

	"github.com/zachpmanson/chainmail/internal/boiler"
)

// Fresh reduction deliberately bypasses the compatibility path's RAM cache.
func freshFoldEvidence(ctx context.Context, s *Store) ([]boiler.Message, []int64, error) {
	aliases, err := DomainAliases(s)
	if err != nil {
		return nil, nil, err
	}
	domains, err := soleDomains(s, aliases)
	if err != nil {
		return nil, nil, err
	}
	rows, err := s.db.QueryContext(ctx, `select e.id,coalesce(e.person_id,0),coalesce(d.from_addr,''),e.body_text,
 exists(select 1 from sightings g where g.entry_id=e.id and g.kind='direct')
 from entries e left join mail_detail d on d.entry_id=e.id
 where e.source='mail' and e.body_text is not null and e.body_text!=''`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var msgs []boiler.Message
	var ids []int64
	for rows.Next() {
		var m boiler.Message
		var from, text string
		var direct bool
		if err = rows.Scan(&m.ID, &m.Author, &from, &text, &direct); err != nil {
			return nil, nil, err
		}
		ids = append(ids, m.ID)
		m.Domain = canonicalDomain(from, aliases)
		if m.Domain == "" {
			m.Domain = domains[m.Author]
		}
		if raw, ok := boiler.Lines(text, direct); ok {
			m.Lines = boiler.Match(raw)
			if len(m.Lines) > 0 {
				msgs = append(msgs, m)
			}
		}
	}
	return msgs, ids, rows.Err()
}

func foldParity(t *testing.T, s *Store, r boiler.Rules) FoldWarm {
	t.Helper()
	ctx := context.Background()
	stats, err := s.RefreshFoldIndex(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	msgs, ids, err := freshFoldEvidence(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.SelectedFolds(ctx, ids, r)
	if err != nil {
		t.Fatal(err)
	}
	want := boiler.Detect(msgs, r)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("selected folds differ from fresh Detect: got %v want %v", got, want)
	}
	checkFoldLedger(t, s, msgs, r)
	return stats
}

func checkFoldLedger(t *testing.T, s *Store, msgs []boiler.Message, r boiler.Rules) {
	t.Helper()
	r = boiler.FilledRules(r)
	// Re-aggregate every published occurrence and sender reference independently
	// in SQL. No delta-maintenance code participates in these expectations.
	queries := []string{
		`with occurrences as (
 select 1 kind,cast(m.person_id as text) group_key,t.fp,count(*) count,1 senders
 from fold_members m join fold_tails t on t.entry_id=m.entry_id where m.person_id!=0 and m.ok=1
 group by m.person_id,t.fp
 union all select 2,m.domain,t.fp,count(*),count(distinct m.person_id)
 from fold_members m join fold_tails t on t.entry_id=m.entry_id where m.domain!='' and m.ok=1
 group by m.domain,t.fp)
 select count(*) from (select * from (select * from occurrences except select * from fold_counts)
 union all select * from (select * from fold_counts except select * from occurrences))`,
		`with expected as (select m.domain,t.fp,m.person_id,count(*) refs
 from fold_members m join fold_tails t on t.entry_id=m.entry_id where m.domain!='' and m.ok=1
 group by m.domain,t.fp,m.person_id)
 select count(*) from (select * from (select * from expected except select * from fold_sender_refs)
 union all select * from (select * from fold_sender_refs except select * from expected))`,
	}
	for _, q := range queries {
		var n int
		if err := s.db.QueryRow(q).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Fatalf("incremental ledger differs from full aggregation: %d", n)
		}
	}
	// Also independently aggregate fresh raw reductions, detecting stale/missing
	// memberships or tails that a self-consistent SQL ledger alone would miss.
	counts := map[foldCountKey]int{}
	refs := map[foldRefKey]int{}
	for _, m := range msgs {
		for _, tail := range boiler.Tails(m.Lines, r.Window) {
			if !tail.Valid {
				continue
			}
			fp := string(tail.Digest[:])
			if m.Author != 0 {
				counts[foldCountKey{1, fmt.Sprint(m.Author), fp}]++
			}
			if m.Domain != "" {
				counts[foldCountKey{2, m.Domain, fp}]++
				refs[foldRefKey{m.Domain, fp, m.Author}]++
			}
		}
	}
	actual := map[foldCountKey]int{}
	actualRefs := map[foldRefKey]int{}
	rows, err := s.db.Query(`select kind,group_key,fp,count from fold_counts`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var k foldCountKey
		var fp []byte
		var n int
		if err = rows.Scan(&k.kind, &k.group, &fp, &n); err != nil {
			t.Fatal(err)
		}
		k.fp = string(fp)
		actual[k] = n
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		t.Fatal(err)
	}
	rows, err = s.db.Query(`select domain,fp,person_id,refs from fold_sender_refs`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var k foldRefKey
		var fp []byte
		var n int
		if err = rows.Scan(&k.domain, &fp, &k.person, &n); err != nil {
			t.Fatal(err)
		}
		k.fp = string(fp)
		actualRefs[k] = n
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(counts, actual) || !reflect.DeepEqual(refs, actualRefs) {
		t.Fatal("published contributions differ from fresh raw-body aggregation")
	}
}
func foldExec(t *testing.T, s *Store, q string, args ...any) {
	t.Helper()
	if _, err := s.db.Exec(q, args...); err != nil {
		t.Fatal(err)
	}
}

func TestFoldIndexMutationParity(t *testing.T) {
	s := signedCorpus(t)
	r := boiler.Default()
	first := foldParity(t, s, r)
	t.Logf("initial: %+v", first)
	id := idOf(t, s, "mail:<ada-1@weave.example>")
	mutations := []struct {
		q    string
		args []any
	}{
		{`update entries set body_text=replace(body_text,'Regards,','Regards,  ') where id=?`, []any{id}},
		{`update entries set person_id=null where id=?`, []any{id}},
		{`update mail_detail set from_addr='Nobody <x@loom.example>' where entry_id=?`, []any{id}},
		{`insert into sightings(entry_id,kind) values(?,'direct')`, []any{id}},
		{`insert into sightings(entry_id,seen_in,kind) values(?,?,'direct')`, []any{id, id}},
		{`delete from sightings where entry_id=? and seen_in is null`, []any{id}},
		{`update sightings set kind='quoted' where entry_id=?`, []any{id}},
		{`insert into domain_aliases values('loom.example','weave.example','synthetic',0)`, nil},
		{`insert into domain_aliases values('weave.example','final.example','synthetic',0)`, nil},
		{`update mail_detail set from_addr=null where entry_id=?`, []any{id}},
		{`update entries set body_text=null where id=?`, []any{id}},
		{`update entries set body_text='Unchanged opening\nRegards,\nAda' where id=?`, []any{id}},
		{`update entries set source='slack' where id=?`, []any{id}},
		{`update entries set source='mail' where id=?`, []any{id}},
		{`delete from domain_aliases`, nil},
	}
	for i, m := range mutations {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			foldExec(t, s, m.q, m.args...)
			stats := foldParity(t, s, r)
			if i == 0 && (stats.Bodies != 1 || stats.CounterKeys > 4*r.Window) {
				t.Fatalf("body edit was not bounded: %+v", stats)
			}
			if i == 7 && stats.Bodies != 0 {
				t.Fatal("alias update normalized bodies")
			}
		})
	}
	for _, window := range []int{2, 1, 25, 30, 25} {
		r.Window = window
		stats := foldParity(t, s, r)
		if !stats.Rebuilt {
			t.Fatal("window change must rebuild")
		}
	}
	foldExec(t, s, `update fold_state set normalize_version=0`)
	if !foldParity(t, s, r).Rebuilt {
		t.Fatal("version change did not rebuild")
	}
	foldExec(t, s, `update fold_state set fingerprint_version=99`)
	if _, err := s.SelectedFolds(context.Background(), []int64{id}, r); err == nil {
		t.Fatal("newer version accepted")
	}
	if _, err := s.WarmFolds(); err == nil {
		t.Fatal("newer version downgraded")
	}
}

func TestFoldIndexRandomizedMutations(t *testing.T) {
	s := signedCorpus(t)
	rng := rand.New(rand.NewSource(184))
	r := boiler.Default()
	msgs, ids, err := freshFoldEvidence(context.Background(), s)
	if err != nil || len(msgs) == 0 {
		t.Fatal(err)
	}
	foldParity(t, s, r)
	for i := 0; i < 100; i++ {
		id := ids[rng.Intn(len(ids))]
		switch rng.Intn(8) {
		case 0:
			foldExec(t, s, `update entries set body_text=? where id=?`, []string{"Opening\nRegards,\nAda", "Only one line", "", "Opening\nNotice\nConfidential", "Opening\n<a@b.test>\nNotice"}[rng.Intn(5)], id)
		case 1:
			foldExec(t, s, `update entries set person_id=(select id from people order by id limit 1 offset ?) where id=?`, rng.Intn(4), id)
		case 2:
			foldExec(t, s, `update mail_detail set from_addr=? where entry_id=?`, []any{nil, "a@loom.example", "b@weave.example", "bad header"}[rng.Intn(4)], id)
		case 3:
			foldExec(t, s, `insert into sightings(entry_id,kind) values(?,'direct') on conflict do nothing`, id)
		case 4:
			foldExec(t, s, `delete from sightings where entry_id=? and kind='direct'`, id)
		case 5:
			foldExec(t, s, `update identities set value=? where kind='email' and person_id=(select person_id from entries where id=?)`, fmt.Sprintf("a%d@%s", i, []string{"other.example", "weave.example"}[rng.Intn(2)]), id)
		case 6:
			foldExec(t, s, `insert into domain_aliases values('weave.example',?,'',0) on conflict(from_domain) do update set to_domain=excluded.to_domain`, []string{"loom.example", "else.example"}[rng.Intn(2)])
		case 7:
			foldExec(t, s, `delete from domain_aliases`)
		}
		foldParity(t, s, r)
	}
}

func TestFoldIndexActualWriters(t *testing.T) {
	s := signedCorpus(t)
	r := boiler.Default()
	foldParity(t, s, r)
	person := sender(t, s, "new@weave.example", "Synthetic")
	e := entry("quote:synthetic-fold", "Short\nRegards,\nAda")
	e.PersonID = person
	id, _, err := s.PutQuoted(e)
	if err != nil {
		t.Fatal(err)
	}
	foldParity(t, s, r)
	e.BodyText = "A longer opening for the recovered body\nRegards,\nAda"
	if err = s.EnrichQuoted(id, e); err != nil {
		t.Fatal(err)
	}
	foldParity(t, s, r)
	e.ExtID = "mail:<actual@weave.example>"
	res, err := s.Put(e, &Mail{From: "new@weave.example"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	foldParity(t, s, r)
	if err = s.Sight(res.ID, 0, "direct", ""); err != nil {
		t.Fatal(err)
	}
	foldParity(t, s, r)
	for i := 0; i < 2; i++ {
		if _, err = s.Put(e, &Mail{From: "new@weave.example"}, nil); err != nil {
			t.Fatal(err)
		}
		if err = s.Sight(res.ID, 0, "direct", ""); err != nil {
			t.Fatal(err)
		}
	}
	if stats := foldParity(t, s, r); stats.Bodies != 0 || stats.Memberships != 0 || stats.Pending != 0 {
		t.Fatalf("identical Put/Sight did work: %+v", stats)
	}
	keep := sender(t, s, "keep@weave.example", "Keeper")
	if err = Merge(s, keep, person); err != nil {
		t.Fatal(err)
	}
	if stats := foldParity(t, s, r); stats.Bodies != 0 {
		t.Fatal("person merge normalized unchanged bodies")
	}
	// Delete and reuse the exact entry ID while published old contributions remain.
	foldExec(t, s, `delete from sightings where entry_id=? or seen_in=?`, id, id)
	foldExec(t, s, `delete from entries where id=?`, id)
	foldExec(t, s, `insert into entries(id,source,ext_id,ts,ingested_at,body_sha,body_text) values(?,'mail','quote:reused',0,0,'synthetic','Replacement\nNotice\nConfidential')`, id)
	foldParity(t, s, r)
}

func TestFoldIndexFailureRollsBack(t *testing.T) {
	s := signedCorpus(t)
	foldParity(t, s, boiler.Default())
	var generation int64
	s.db.QueryRow(`select generation from fold_state`).Scan(&generation)
	foldExec(t, s, `create trigger fold_test_failure before insert on fold_counts begin select raise(abort,'injected publication failure'); end`)
	foldExec(t, s, `update entries set body_text='New opening\nDifferent\nSignature'`)
	if _, err := s.WarmFolds(); err == nil {
		t.Fatal("injected failure ignored")
	}
	var got int64
	var pending int
	if err := s.db.QueryRow(`select generation,(select count(*) from fold_dirty_entries) from fold_state`).Scan(&got, &pending); err != nil {
		t.Fatal(err)
	}
	if got != generation || pending == 0 {
		t.Fatal("failed publication consumed durable work")
	}
	foldExec(t, s, `drop trigger fold_test_failure`)
	foldParity(t, s, boiler.Default())
}

func TestFoldIndexRequoteAndTwinAbsorption(t *testing.T) {
	s, _, _, _, _ := foldStored(t)
	foldParity(t, s, boiler.Default())
	rep, err := RepairQuotedBodies(s)
	if err != nil || rep.Fixed == 0 {
		t.Fatalf("requote: %+v %v", rep, err)
	}
	foldParity(t, s, boiler.Default())
	plan, err := CollapseTwins(s, true)
	if err != nil || plan.Removed == 0 {
		t.Fatalf("twins: %+v %v", plan, err)
	}
	foldParity(t, s, boiler.Default())
}
