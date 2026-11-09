package mailingest

import (
	"fmt"
	"html"
	"strings"
	"testing"
	"time"

	"github.com/zachpmanson/chainmail/internal/corpus"
)

func TestEmptyForwardRequiresAffirmativeEvidence(t *testing.T) {
	for _, tc := range []struct{ name, sql string }{
		{"different nested content", `update entries set body_text=replace(body_text,'northern beds','southern fields') where quoted=0 and ext_id='mail:<synthetic-forward@example.test>'`},
		{"HTML preferred over matching plain", `update entries set body_html='<div>A different message without any forward boundary.</div>' where ext_id='mail:<synthetic-forward@example.test>'`},
		{"reply is not forward", `update entries set body_text=replace(body_text,'---------- Forwarded message ---------','________________________________') where ext_id='mail:<synthetic-forward@example.test>'`},
		{"not bare forward", `update entries set body_text='Please read this carefully.' || char(10) || body_text where ext_id='mail:<synthetic-forward@example.test>'`},
		{"missing nested header", `update entries set body_text=substr(body_text,1,instr(body_text,char(10))) || substr(body_text,instr(body_text,'Progress:')) where ext_id='mail:<synthetic-forward@example.test>'`},
		{"missing nested date", `update entries set body_text=replace(body_text,'Date: Fri, 6 Nov 2026, 21:27','') where ext_id='mail:<synthetic-forward@example.test>'`},
		{"missing mailbox body", `update entries set body_text='' where ext_id='mail:<synthetic-forward@example.test>'`},
		{"missing wrapper parent", `update entries set parent_id=null where quoted=1 and coalesce(body_text,'')=''`},
		{"missing mailbox parent", `update entries set parent_id=null where ext_id='mail:<synthetic-forward@example.test>'`},
		{"different parent", `update entries set parent_id=(select id from entries where ext_id='mail:<synthetic-reply@example.test>') where quoted=1 and coalesce(body_text,'')=''`},
		{"different sender", `update entries set person_id=(select person_id from entries where ext_id='mail:<synthetic-reply@example.test>') where quoted=1 and coalesce(body_text,'')=''`},
		{"different subject", `update entries set subject='Other bulletin' where quoted=1 and coalesce(body_text,'')=''`},
		{"missing subject", `update entries set subject=null where quoted=1 and coalesce(body_text,'')=''`},
		{"different container", `update entries set container='other-synthetic-thread' where quoted=1 and coalesce(body_text,'')=''`},
		{"missing container", `update entries set container=null where quoted=1 and coalesce(body_text,'')=''`},
		{"no stated subject after prefixes", `update entries set subject='Fwd:' where quoted=1 and coalesce(body_text,'')=''`},
		{"different recipients", `update participants set person_id=(select person_id from identities where value='robin@example.net') where role='to' and entry_id in (select id from entries where quoted=1 and coalesce(body_text,'')='')`},
		{"missing recipients", `delete from participants where role='to' and entry_id in (select id from entries where quoted=1 and coalesce(body_text,'')='')`},
		{"missing mailbox recipients", `delete from participants where role='to' and entry_id in (select id from entries where ext_id='mail:<synthetic-forward@example.test>')`},
		{"derived parent edge", `update entries set derived=1 where quoted=1 and coalesce(body_text,'')=''`},
		{"derived mailbox", `update entries set derived=1 where ext_id='mail:<synthetic-forward@example.test>'`},
		{"not actually empty", `update entries set body_text='!!!' where quoted=1 and coalesce(body_text,'')=''`},
		{"incompatible clock", `update entries set ts=ts+300 where quoted=1 and coalesce(body_text,'')=''`},
		{"attachment veto", `insert into attachments(entry_id,name) select id,'synthetic-document.pdf' from entries where quoted=1 and coalesce(body_text,'')=''`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := openTest(t)
			for _, m := range forwardMessages() {
				if _, err := Put(s, m); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := s.DB().Exec(tc.sql); err != nil {
				t.Fatal(err)
			}
			for _, apply := range []bool{false, true} {
				plan, err := corpus.CollapseTwins(s, apply)
				if err != nil || plan.Removed != 0 {
					t.Fatalf("apply=%v unsafe collapse: %+v %v", apply, plan, err)
				}
			}
		})
	}
}

func TestEmptyForwardRejectsContradictoryNestedHeaders(t *testing.T) {
	for _, tc := range []struct{ name, old, replacement string }{
		{"different known sender", "From: Alex Vale <alex@example.net>", "From: Casey Lake <casey@example.org>"},
		{"unknown sender", "From: Alex Vale <alex@example.net>", "From: Unknown Person <unknown@example.test>"},
		{"conflicting message ID", "Subject:", "Message-ID: <synthetic-reply@example.test>\nSubject:"},
		{"different subject", "Subject: Re: Fwd: Monthly bulletin", "Subject: Other bulletin"},
		{"different date", "Date: Fri, 6 Nov 2026, 21:27", "Date: Fri, 13 Nov 2026, 21:27"},
		{"incompatible minute", "Date: Fri, 6 Nov 2026, 21:27", "Date: Fri, 6 Nov 2026, 21:32"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := openTest(t)
			for _, m := range forwardMessages() {
				if _, err := Put(s, m); err != nil {
					t.Fatal(err)
				}
			}
			var mail, parent, quoteParent int64
			if err := s.DB().QueryRow(`select id,parent_id from entries where ext_id='mail:<synthetic-forward@example.test>'`).Scan(&mail, &parent); err != nil {
				t.Fatal(err)
			}
			if err := s.DB().QueryRow(`select parent_id from entries where quoted=1 and coalesce(body_text,'')=''`).Scan(&quoteParent); err != nil || parent != quoteParent {
				t.Fatalf("shared graph parent: %d / %d, %v", parent, quoteParent, err)
			}
			// Change only a nested header, not the already-resolved graph edge or
			// the nested prose. The parent may have come from In-Reply-To instead.
			body := strings.Replace(bulletinForward, tc.old, tc.replacement, 1)
			if _, err := s.DB().Exec(`update entries set body_text=? where id=?`, body, mail); err != nil {
				t.Fatal(err)
			}
			for _, apply := range []bool{false, true} {
				plan, err := corpus.CollapseTwins(s, apply)
				if err != nil || plan.Removed != 0 {
					t.Fatalf("apply=%v contradictory header collapsed: %+v %v", apply, plan, err)
				}
			}
		})
	}
}

func TestEmptyForwardHTMLPreservesNestedIdentity(t *testing.T) {
	for _, tc := range []struct {
		name, old, replacement string
		removed                int
	}{
		{"consistent", "", "", 1},
		{"contradictory sender", "alex@example.net", "casey@example.org", 0},
		{"contradictory message ID", "synthetic-inner@example.test", "other-inner@example.test", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := openTest(t)
			for _, m := range forwardMessages() {
				if _, err := Put(s, m); err != nil {
					t.Fatal(err)
				}
			}
			plain := strings.Replace(bulletinForward, "Subject:", "Message-ID: <synthetic-inner@example.test>\nSubject:", 1)
			htmlText := plain
			if tc.old != "" {
				htmlText = strings.Replace(htmlText, tc.old, tc.replacement, 1)
			}
			markup := "<div>" + strings.ReplaceAll(html.EscapeString(htmlText), "\n", "<br>") + "</div>"
			if _, err := s.DB().Exec(`update entries set ext_id='mail:<synthetic-inner@example.test>' where id=(select parent_id from entries where ext_id='mail:<synthetic-forward@example.test>')`); err != nil {
				t.Fatal(err)
			}
			if _, err := s.DB().Exec(`update entries set body_text=?,body_html=? where ext_id='mail:<synthetic-forward@example.test>'`, plain, markup); err != nil {
				t.Fatal(err)
			}
			for _, apply := range []bool{false, true} {
				plan, err := corpus.CollapseTwins(s, apply)
				if err != nil || plan.Removed != tc.removed {
					t.Fatalf("apply=%v removed=%d want=%d: %v", apply, plan.Removed, tc.removed, err)
				}
			}
		})
	}
}

func TestEmptyForwardAcceptsConsistentNestedHeaders(t *testing.T) {
	for _, tc := range []struct{ name, old, replacement, setup string }{
		{"case folded address", "alex@example.net", "ALEX@EXAMPLE.NET", ""},
		{"resolved sender alias", "alex@example.net", "alex.alias@example.test", `insert into identities(person_id,kind,value) select person_id,'email','alex.alias@example.test' from identities where value='alex@example.net'`},
		{"matching message ID", "Subject:", "Message-ID: <synthetic-inner@example.test>\nSubject:", `update entries set ext_id='mail:<synthetic-inner@example.test>' where id=(select parent_id from entries where ext_id='mail:<synthetic-forward@example.test>')`},
		{"matching mail detail ID", "Subject:", "Message-ID: <synthetic-inner@example.test>\nSubject:", `insert into mail_detail(entry_id,message_id) select parent_id,'<synthetic-inner@example.test>' from entries where ext_id='mail:<synthetic-forward@example.test>'`},
		{"equivalent subject", "Subject: Re: Fwd: Monthly bulletin", "Subject: Monthly bulletin", ""},
		{"compatible quoted offset", "Date: Fri, 6 Nov 2026, 21:27", "Date: Fri, 6 Nov 2026, 10:27", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := openTest(t)
			for _, m := range forwardMessages() {
				if _, err := Put(s, m); err != nil {
					t.Fatal(err)
				}
			}
			if tc.setup != "" {
				if _, err := s.DB().Exec(tc.setup); err != nil {
					t.Fatal(err)
				}
			}
			body := strings.Replace(bulletinForward, tc.old, tc.replacement, 1)
			if _, err := s.DB().Exec(`update entries set body_text=? where ext_id='mail:<synthetic-forward@example.test>'`, body); err != nil {
				t.Fatal(err)
			}
			for _, apply := range []bool{false, true} {
				plan, err := corpus.CollapseTwins(s, apply)
				if err != nil || plan.Removed != 1 {
					t.Fatalf("apply=%v consistent header refused: %+v %v", apply, plan, err)
				}
			}
		})
	}
}

func TestEmptyForwardAmbiguity(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		for _, recovered := range []bool{false, true} {
			t.Run(fmt.Sprintf("reverse=%v/recovered=%v", reverse, recovered), func(t *testing.T) {
				s := openTest(t)
				msgs := forwardMessages()
				if !recovered {
					competing := msgs[0]
					competing.ID = "synthetic-competing-forward"
					competing.MessageID = "<synthetic-competing-forward@example.test>"
					competing.Date = "Sun, 8 Nov 2026 16:11:00 +1100"
					msgs = append(msgs, competing)
				}
				if reverse {
					for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
						msgs[i], msgs[j] = msgs[j], msgs[i]
					}
				}
				for _, m := range msgs {
					if _, err := Put(s, m); err != nil {
						t.Fatal(err)
					}
				}
				if recovered {
					// The mailbox's broad To set must not bridge two wrappers whose own
					// known recipients are disjoint, despite sharing the same inner message.
					var mail, quote, alex, robin, parent int64
					if err := s.DB().QueryRow(`select id from entries where ext_id='mail:<synthetic-forward@example.test>'`).Scan(&mail); err != nil {
						t.Fatal(err)
					}
					if err := s.DB().QueryRow(`select id,person_id,parent_id from entries where quoted=1 and coalesce(body_text,'')=''`).Scan(&quote, &alex, &parent); err != nil {
						t.Fatal(err)
					}
					if err := s.DB().QueryRow(`select person_id from identities where value='robin@example.net'`).Scan(&robin); err != nil {
						t.Fatal(err)
					}
					if err := corpus.Participate(s, mail, robin, corpus.RoleTo); err != nil {
						t.Fatal(err)
					}
					id, _, err := s.PutQuoted(corpus.Entry{Source: corpus.SourceMail, ExtID: "quote:synthetic-competing-wrapper", TS: time.Date(2026, 11, 8, 16, 10, 0, 0, time.UTC), PersonID: alex, Container: "synthetic-bulletin", Subject: "Monthly bulletin"})
					if err != nil {
						t.Fatal(err)
					}
					if err := s.SetParent(id, parent); err != nil {
						t.Fatal(err)
					}
					if err := corpus.Participate(s, id, robin, corpus.RoleTo); err != nil {
						t.Fatal(err)
					}
				}
				for _, apply := range []bool{false, true} {
					plan, err := corpus.CollapseTwins(s, apply)
					if err != nil || plan.Removed != 0 {
						t.Fatalf("ambiguous collapse: %+v %v", plan, err)
					}
					found := false
					for _, d := range plan.Declined {
						if strings.Contains(d.Reason, "more than one mailbox") || strings.Contains(d.Reason, "disagree about recipients") {
							found = true
						}
					}
					if !found {
						t.Fatalf("missing ambiguity refusal: %+v", plan)
					}
				}
			})
		}
	}
}

func TestEmptyForwardRecipientUnionAndFreshCacheEvidence(t *testing.T) {
	s := openTest(t)
	msgs := forwardMessages()
	msgs[1].Body = strings.Replace(msgs[1].Body, "To: casey@example.org <casey@example.org>", "To: casey@example.org <casey@example.org>, Rowan Moss <rowan@example.test>", 1)
	for _, m := range msgs {
		if _, err := Put(s, m); err != nil {
			t.Fatal(err)
		}
	}
	var quote, mail, casey int64
	if err := s.DB().QueryRow(`select id from entries where quoted=1 and coalesce(body_text,'')=''`).Scan(&quote); err != nil {
		t.Fatal(err)
	}
	if err := s.DB().QueryRow(`select id from entries where ext_id='mail:<synthetic-forward@example.test>'`).Scan(&mail); err != nil {
		t.Fatal(err)
	}
	if err := s.DB().QueryRow(`select person_id from identities where value='casey@example.org'`).Scan(&casey); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB().Exec(`delete from participants where entry_id=? and person_id=? and role='to'`, quote, casey); err != nil {
		t.Fatal(err)
	}
	// Publish reductions while disjoint routing evidence prevents a collapse.
	primed, err := corpus.CollapseTwins(s, true)
	if err != nil || primed.Removed != 0 {
		t.Fatalf("prime: %+v %v", primed, err)
	}
	if err := corpus.Participate(s, quote, casey, corpus.RoleTo); err != nil {
		t.Fatal(err)
	}
	dry, err := corpus.CollapseTwins(s, false)
	if err != nil || dry.Removed != 1 {
		t.Fatalf("fresh dry run: %+v %v", dry, err)
	}
	applied, err := corpus.CollapseTwins(s, true)
	if err != nil || applied.Removed != 1 {
		t.Fatalf("warm cache ignored fresh participants: %+v %v", applied, err)
	}
	var recipients int
	if err := s.DB().QueryRow(`select count(*) from participants where entry_id=? and role='to'`, mail).Scan(&recipients); err != nil {
		t.Fatal(err)
	}
	if recipients != 2 {
		t.Fatalf("recipient union lost: %d", recipients)
	}
}
