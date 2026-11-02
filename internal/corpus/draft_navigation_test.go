package corpus

import (
	"fmt"
	"testing"
)

func TestResolveDraftParent(t *testing.T) {
	for _, tc := range []struct {
		name string
		mode string
		want string
	}{
		{"nonroot recovered ancestor", "normal", "resolved"},
		{"current partial corpus root", "partial", "resolved"},
		{"other account only", "other", "missing-parent"},
		{"same RFC across accounts", "across", "resolved"},
		{"exact case", "case", "missing-parent"},
		{"missing", "missing", "missing-parent"},
		{"duplicate same root", "same", "ambiguous-parent"},
		{"duplicate different root", "different", "ambiguous-parent"},
		{"self cycle", "self", "unresolved-root"},
		{"two node cycle", "cycle", "unresolved-root"},
		{"dangling parent", "dangling", "unresolved-root"},
		{"cap exhausted", "cap", "unresolved-root"},
		{"64-node root", "boundary", "resolved"},
		{"65-node limit", "beyond", "unresolved-root"},
		{"missing legacy copy", "legacy", "missing-parent"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := open(t)
			exec := func(q string, args ...any) {
				t.Helper()
				if _, err := s.db.Exec(q, args...); err != nil {
					t.Fatal(err)
				}
			}
			exec(`insert into gmail_accounts(id,email,display_name,created_at) values ('stable-app-id','owner@example.test','Owner',0),('other-id','other@example.test','Other',0)`)
			node := func(id int, ext string, parent any) {
				exec(`insert into entries(id,source,ext_id,kind,ts,parent_id,body_sha,ingested_at) values (?,'mail',?,'message',0,?,'synthetic',0)`, id, ext, parent)
			}
			node(1, "recovered:root", nil)
			node(2, "mail:exact-parent", 1)
			exec(`insert into mail_detail(entry_id,message_id) values (2,'<Parent@Example.test>')`)
			account := "stable-app-id"
			if tc.mode == "other" {
				account = "other-id"
			}
			exec(`insert into gmail_copies(account_id,gmail_id,entry_id,labels) values (?,'g-parent',2,'[]'),('other-id','g-root',1,'[]')`, account)
			switch tc.mode {
			case "across":
				node(3, "mail:other-account", nil)
				exec(`insert into mail_detail(entry_id,message_id) values (3,'<Parent@Example.test>')`)
				exec(`insert into gmail_copies(account_id,gmail_id,entry_id,labels) values ('other-id','other-parent',3,'[]')`)
			case "partial":
				exec(`update entries set parent_ref='<external@absent.test>' where id=1`)
			case "same", "different":
				var p any = 1
				if tc.mode == "different" {
					p = nil
				}
				node(3, "mail:duplicate", p)
				exec(`insert into mail_detail(entry_id,message_id) values (3,'<Parent@Example.test>')`)
				exec(`insert into gmail_copies(account_id,gmail_id,entry_id,labels) values ('stable-app-id','duplicate',3,'[]')`)
			case "self":
				exec(`update entries set parent_id=2 where id=2`)
			case "cycle":
				exec(`update entries set parent_id=2 where id=1`)
			case "dangling":
				exec(`pragma foreign_keys=off`)
				exec(`update entries set parent_id=99999 where id=1`)
			case "cap", "boundary", "beyond":
				last := walkDepthCap + 3
				if tc.mode == "boundary" {
					last = walkDepthCap
				} else if tc.mode == "beyond" {
					last = walkDepthCap + 1
				}
				for i := 3; i <= last; i++ {
					node(i, fmt.Sprintf("recovered:%d", i), i-1)
				}
				exec(`update entries set parent_id=? where id=2`, last)
				exec(`update entries set parent_id=1 where id=3`)
			}
			messageID := "<Parent@Example.test>"
			if tc.mode == "case" {
				messageID = "<parent@example.test>"
			}
			if tc.mode == "missing" {
				messageID = "<missing@example.test>"
			}
			// SQLite rejects any accidental writes after fixture setup.
			exec(`pragma query_only=on`)
			lookupAccount := "stable-app-id"
			if tc.mode == "legacy" {
				lookupAccount = "legacy"
			}
			got, err := s.ResolveDraftParent(lookupAccount, messageID)
			if err != nil {
				t.Fatal(err)
			}
			if got.Resolution != tc.want {
				t.Fatalf("got %+v want %s", got, tc.want)
			}
			if tc.want == "resolved" {
				if got.ReplyToEntry != "mail:exact-parent" || got.RootID != "recovered:root" {
					t.Fatalf("handles %+v", got)
				}
				meta, err := s.ChainMeta("mail:exact-parent")
				if err != nil || meta.RootExtID != got.RootID {
					t.Fatalf("chain parity %+v %v", meta, err)
				}
			} else if got.ReplyToEntry != "" || got.RootID != "" {
				t.Fatalf("unresolved handles %+v", got)
			}
		})
	}
}

func TestResolveDraftParentDatabaseFailure(t *testing.T) {
	s := open(t)
	_ = s.Close()
	if _, err := s.ResolveDraftParent("stable-app-id", "<p@e.test>"); err == nil {
		t.Fatal("database failure hidden")
	}
}
