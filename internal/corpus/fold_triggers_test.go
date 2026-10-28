package corpus

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zachpmanson/chainmail/internal/boiler"
)

func foldPending(t *testing.T, s *Store) (entries, people, aliases int) {
	t.Helper()
	if err := s.db.QueryRow(`select (select count(*) from fold_dirty_entries),(select count(*) from fold_dirty_people),aliases_dirty from fold_state`).Scan(&entries, &people, &aliases); err != nil {
		t.Fatal(err)
	}
	return
}
func TestFoldTriggersIgnoreIrrelevantAndIdenticalUpdates(t *testing.T) {
	s := signedCorpus(t)
	foldParity(t, s, boiler.Default())
	id := idOf(t, s, "mail:<ada-1@weave.example>")
	foldExec(t, s, `insert into sightings(entry_id,kind,detail) values(?,'direct','original')`, id)
	foldParity(t, s, boiler.Default())
	queries := []string{
		`update entries set body_text=body_text,source=source,person_id=person_id`,
		`update entries set body_html='<p>HTML only</p>',subject='Metadata',ts=ts+1,derived=1,quoted=0`,
		`update mail_detail set from_addr=from_addr,labels='unread',to_addr='x@else.example'`,
		`update sightings set detail='metadata',seen_in=entry_id`,
		`update sightings set entry_id=entry_id,kind=kind`,
		`update identities set value=value,person_id=person_id,kind=kind,rule='metadata'`,
		`update people set display_name='Renamed',org='Different'`,
		`insert into sightings(entry_id,kind) select id,'quoted' from entries`,
		`update sightings set kind='forwarded' where kind='quoted'`,
		`delete from sightings where kind='forwarded'`,
	}
	for _, q := range queries {
		foldExec(t, s, q)
		a, b, c := foldPending(t, s)
		if a+b+c != 0 {
			t.Fatalf("irrelevant statement queued work: %s: %d/%d/%d", q, a, b, c)
		}
	}
	foldExec(t, s, `insert into domain_aliases values('unused.example','else.example','',0)`)
	foldParity(t, s, boiler.Default())
	foldExec(t, s, `update domain_aliases set note='metadata',from_domain=from_domain,to_domain=to_domain`)
	a, b, c := foldPending(t, s)
	if a+b+c != 0 {
		t.Fatal("alias metadata queued work")
	}
	if stats, err := s.WarmFolds(); err != nil || stats.Bodies != 0 || stats.Memberships != 0 || stats.Generation == 0 {
		t.Fatalf("clean refresh: %+v %v", stats, err)
	}
}

func TestFoldDirectExistenceTransitions(t *testing.T) {
	s := signedCorpus(t)
	r := boiler.Default()
	foldParity(t, s, r)
	id := idOf(t, s, "mail:<ada-1@weave.example>")
	other := idOf(t, s, "mail:<ada-2@weave.example>")
	step := func(q string, want int, args ...any) {
		t.Helper()
		foldExec(t, s, q, args...)
		a, _, _ := foldPending(t, s)
		if a != want {
			t.Fatalf("direct transition queued %d want %d: %s", a, want, q)
		}
		foldParity(t, s, r)
	}
	step(`insert into sightings(entry_id,kind) values(?,'direct')`, 1, id)
	step(`insert into sightings(entry_id,seen_in,kind) values(?,?,'direct')`, 0, id, other)
	step(`update sightings set seen_in=? where entry_id=? and seen_in is null`, 0, id, id)
	step(`delete from sightings where entry_id=? and seen_in=?`, 0, id, other)
	step(`update sightings set entry_id=? where entry_id=?`, 2, other, id)
	step(`update sightings set kind='quoted' where entry_id=?`, 1, other)
	step(`update sightings set kind='direct' where entry_id=?`, 1, other)
	// Twin absorption's UPDATE OR IGNORE can drop a duplicate without changing
	// existence on either entry. Deleting its last old direct must still queue.
	step(`insert into sightings(entry_id,seen_in,kind) values(?,?,'direct')`, 1, id, id)
	step(`update or ignore sightings set entry_id=? where entry_id=?`, 0, other, id)
	step(`delete from sightings where entry_id=? and kind='direct'`, 1, id)
	step(`delete from sightings where entry_id=? and kind='direct'`, 1, other)
}

func TestFoldRestartAndEmptySelection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "restart.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	p := sender(t, s, "new@restart.example", "Synthetic")
	id := say(t, s, p, "new@restart.example", "mail:restart", "Opening\nRegards,\nSynthetic")
	first, err := s.WarmFolds()
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	clean, err := s.WarmFolds()
	if err != nil {
		t.Fatal(err)
	}
	if clean.Bodies != 0 || clean.Memberships != 0 || clean.Generation != first.Generation {
		t.Fatalf("clean restart did work: %+v", clean)
	}
	foldExec(t, s, `update entries set body_text='Changed\nRegards,\nSynthetic' where id=?`, id)
	got, err := s.SelectedFolds(context.Background(), nil, boiler.Default())
	if err != nil || len(got) != 0 {
		t.Fatal("empty selected lookup did work")
	}
	a, _, _ := foldPending(t, s)
	if a != 1 {
		t.Fatal("empty selection consumed queued work")
	}
	foldParity(t, s, boiler.Default())
}

func TestFoldIdentityFallbackAndNegativeReductionReuse(t *testing.T) {
	s := open(t)
	p := sender(t, s, "one@first.example", "Synthetic")
	id := say(t, s, p, "", "mail:fallback", "Opening\nRegards,\nSynthetic")
	// Direct reductions of HTML-like, absent and empty bodies are all retained
	// or removed correctly, even when they provide no usable candidates.
	neg := say(t, s, p, "", "mail:negative", "<html><script>ignored</script></html>")
	if err := s.Sight(neg, 0, "direct", ""); err != nil {
		t.Fatal(err)
	}
	foldParity(t, s, boiler.Default())
	for _, edit := range []PersonEdit{
		{Add: []string{"email:two@second.example"}},
		{Remove: []string{"email:one@first.example"}},
		{DisplayName: "Renamed", Remove: []string{"email:two@second.example"}},
	} {
		if _, err := UpdatePerson(s, p, edit); err != nil {
			t.Fatal(err)
		}
		stats := foldParity(t, s, boiler.Default())
		if stats.Bodies != 0 {
			t.Fatal("identity edit reread/normalized bodies")
		}
	}
	foldExec(t, s, `update entries set person_id=null where id=?`, id)
	foldParity(t, s, boiler.Default())
}

func TestFoldDirtyWorkAndLookupQueryPlans(t *testing.T) {
	s := signedCorpus(t)
	first := foldParity(t, s, boiler.Default())
	id := idOf(t, s, "mail:<ada-1@weave.example>")
	plans := []struct {
		name, query string
		args        []any
		required    []string
	}{
		{"dirty-work", foldWorkQuery(false), nil, []string{"SEARCH m USING INDEX fold_members_person"}},
		{"source-by-id", foldSourceQuery, []any{id}, []string{"SEARCH e USING INTEGER PRIMARY KEY"}},
		{"selected-counts", selectedFoldQuery("?"), []any{id}, []string{"SEARCH m USING INTEGER PRIMARY KEY", "SEARCH t USING INDEX sqlite_autoindex_fold_tails_1", "SEARCH a USING INDEX sqlite_autoindex_fold_counts_1", "SEARCH d USING INDEX sqlite_autoindex_fold_counts_1"}},
	}
	for _, p := range plans {
		t.Run(p.name, func(t *testing.T) {
			rows, err := s.db.Query("explain query plan "+p.query, p.args...)
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()
			var details []string
			for rows.Next() {
				var id, parent, unused int
				var detail string
				if err = rows.Scan(&id, &parent, &unused, &detail); err != nil {
					t.Fatal(err)
				}
				details = append(details, detail)
			}
			if err = rows.Err(); err != nil {
				t.Fatal(err)
			}
			joined := strings.Join(details, "\n")
			for _, required := range p.required {
				if !strings.Contains(joined, required) {
					t.Fatalf("missing indexed probe %q in %s", required, joined)
				}
			}
			for _, detail := range details {
				if strings.HasPrefix(detail, "SCAN m") || strings.HasPrefix(detail, "SCAN e") || strings.Contains(detail, "SCAN fold_members") || strings.Contains(detail, "SCAN entries") {
					t.Fatalf("ordinary refresh/read admitted a full member/body scan: %s", detail)
				}
			}
		})
	}
	// A body edit in a broader corpus considers only that one source row.
	foldExec(t, s, `update entries set body_text='New opening\nRegards,\nAda' where id=?`, id)
	changed := foldParity(t, s, boiler.Default())
	if changed.Rebuilt || changed.Bodies != 1 || changed.Pending != 1 || changed.Memberships != 1 || changed.CounterKeys > 4*boiler.Default().Window || changed.Generation != first.Generation+1 {
		t.Fatalf("body edit expanded dirty work: %+v", changed)
	}
	// Person metadata work reaches only that person's fallback members, not
	// explicit From memberships or other people's fallback members.
	var person int64
	if err := s.db.QueryRow(`select person_id from entries where id=?`, id).Scan(&person); err != nil {
		t.Fatal(err)
	}
	say(t, s, person, "", "mail:fallback-plan", "Opening\nRegards,\nAda")
	foldParity(t, s, boiler.Default())
	if _, err := UpdatePerson(s, person, PersonEdit{Add: []string{"email:another@else.example"}}); err != nil {
		t.Fatal(err)
	}
	changed = foldParity(t, s, boiler.Default())
	if changed.Bodies != 0 || changed.Pending != 1 || changed.Memberships != 1 {
		t.Fatalf("identity edit expanded dirty work: %+v", changed)
	}
}
