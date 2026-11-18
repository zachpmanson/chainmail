package corpus

import (
	"context"
	"fmt"
	"html"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zachpmanson/chainmail/internal/unnest"
)

const sourceProse = "Please reserve the wooden seed trays for the greenhouse inventory team. We will count the remaining pots after lunch and leave the labelled baskets beside the covered workshop bench for tomorrow morning."

type sourceFixture struct {
	author, other, target, host, quote int64
	rec                                unnest.Recovered
	text, body                         string
}

func seedSourceTwin(t *testing.T, s *Store, text string) sourceFixture {
	t.Helper()
	f := sourceFixture{text: text}
	f.author = person(t, s, "fen@invented.example.test", "Fen Larch")
	f.other = person(t, s, "kel@invented.example.test", "Kel Aspen")
	put := func(e Entry, m *Mail) int64 {
		t.Helper()
		r, err := s.Put(e, m, nil)
		if err != nil {
			t.Fatal(err)
		}
		return r.ID
	}
	at := time.Date(2037, 5, 14, 9, 3, 35, 0, time.UTC)
	f.target = put(Entry{Source: SourceMail, ExtID: "mail:<invented-reservation@invented.example.test>", TS: at, PersonID: f.author, Container: "invented-reservation", BodyText: text}, &Mail{MessageID: "<invented-reservation@invented.example.test>", From: "Fen Larch <fen@invented.example.test>"})
	lines := strings.Split(text, "\n")
	for i := range lines {
		lines[i] = "> " + lines[i]
	}
	f.body = "The covered bench is ready.\n\nOn Thu, 14 May 2037 at 15:00, Fen Larch <fen@invented.example.test> wrote:\n" + strings.Join(lines, "\n")
	f.host = put(Entry{Source: SourceMail, ExtID: "mail:<invented-bench@invented.example.test>", TS: at.Add(24 * time.Hour), PersonID: f.other, Container: "invented-reservation", BodyText: f.body}, &Mail{MessageID: "<invented-bench@invented.example.test>", InReplyTo: "<invented-reservation@invented.example.test>", From: "Kel Aspen <kel@invented.example.test>"})
	for _, id := range []int64{f.target, f.host} {
		if err := s.PutGmailCopy(GmailCopy{AccountID: "legacy", GmailID: fmt.Sprintf("invented-%d", id), EntryID: id}); err != nil {
			t.Fatal(err)
		}
	}
	f.rec = unnest.Parse(unnest.Peel(f.body)[1])
	var err error
	f.quote, _, err = s.PutQuoted(Entry{Source: SourceMail, ExtID: f.rec.Key, TS: f.rec.Sent, PersonID: f.author, Container: "invented-reservation", BodyText: f.rec.Block.Text, QuoteTimeBasis: "stated", QuoteTimePrecision: f.rec.DatePrecision})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Sight(f.quote, f.host, "quoted", "depth 0"); err != nil {
		t.Fatal(err)
	}
	return f
}

func assertSourceMatch(t *testing.T, s *Store, f sourceFixture, want bool) {
	t.Helper()
	id, _, err := sourceTwinDecision(s.db, f.host, f.author, f.rec, "", "invented-reservation")
	ok := id != 0
	if err != nil || (want && (!ok || id != f.target)) || (!want && ok) {
		t.Fatalf("ingest source match=%d,%v,%v want=%v target=%d", id, ok, err, want, f.target)
	}
	proof, err := sourceTwinTarget(s.db, []int64{f.host}, f.rec.Key, f.rec.Block.Text, "invented-reservation", f.author)
	if err != nil || (proof.target != 0) != want {
		t.Fatalf("source proof=%+v err=%v want=%v", proof, err, want)
	}
	p, err := CollapseTwins(s, false)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	if want {
		n = 1
	}
	if p.Removed != n || p.Measured != 0 {
		t.Fatalf("repair=%+v want removed=%d and no off-lattice measurement", p, n)
	}
	if want && (len(p.Collapse) != 1 || p.Collapse[0].Keep != f.target || !reflect.DeepEqual(p.Collapse[0].Drop, []int64{f.quote})) {
		t.Fatalf("wrong target: %+v", p)
	}
}

func TestSourceTwinCurrentEvidenceRefusals(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*testing.T, *Store, sourceFixture)
	}{
		{"missing raw parent despite inferred edge", func(t *testing.T, s *Store, f sourceFixture) {
			sourceExec(t, s, `update mail_detail set in_reply_to='' where entry_id=?; update entries set parent_id=? where id=?`, f.host, f.target, f.host)
		}},
		{"References is not immediate parent", func(t *testing.T, s *Store, f sourceFixture) {
			sourceExec(t, s, `update mail_detail set in_reply_to='',refs='<invented-reservation@invented.example.test>' where entry_id=?`, f.host)
		}},
		{"contradictory raw parent", func(t *testing.T, s *Store, f sourceFixture) {
			sourceExec(t, s, `update mail_detail set in_reply_to='<unknown@invented.example.test>' where entry_id=?`, f.host)
		}},
		{"different author", func(t *testing.T, s *Store, f sourceFixture) {
			sourceExec(t, s, `update entries set person_id=? where id=?`, f.other, f.target)
		}},
		{"different thread", func(t *testing.T, s *Store, f sourceFixture) {
			sourceExec(t, s, `update entries set container='other-invented-thread' where id=?`, f.target)
		}},
		{"different account", func(t *testing.T, s *Store, f sourceFixture) {
			if err := s.PutGmailAccount(GmailAccount{ID: "invented-other", DisplayName: "Invented other"}); err != nil {
				t.Fatal(err)
			}
			sourceExec(t, s, `update gmail_copies set account_id='invented-other' where entry_id=?`, f.target)
		}},
		{"NULL sighting host", func(t *testing.T, s *Store, f sourceFixture) {
			sourceExec(t, s, `update sightings set seen_in=null where entry_id=?`, f.quote)
		}},
		{"missing sighting host", func(t *testing.T, s *Store, f sourceFixture) {
			sourceExec(t, s, `update entries set body_text='The quotation has been removed.' where id=?`, f.host)
		}},
		{"edited source quote", func(t *testing.T, s *Store, f sourceFixture) {
			sourceExec(t, s, `update entries set body_text=? where id=?`, strings.ReplaceAll(f.body, "wooden", "steel"), f.host)
		}},
		{"edited source author", func(t *testing.T, s *Store, f sourceFixture) {
			sourceExec(t, s, `update entries set body_text=? where id=?`, strings.ReplaceAll(f.body, "fen@invented.example.test", "kel@invented.example.test"), f.host)
		}},
		{"nested attribution", func(t *testing.T, s *Store, f sourceFixture) {
			lines := strings.Split(f.body, "\n")
			for i := range lines {
				lines[i] = "> " + lines[i]
			}
			sourceExec(t, s, `update entries set body_text=? where id=?`, strings.Join(lines, "\n"), f.host)
		}},
		{"forwarded header", func(t *testing.T, s *Store, f sourceFixture) {
			sourceExec(t, s, `update entries set body_text=? where id=?`, "---------- Forwarded message ---------\nFrom: Fen Larch <fen@invented.example.test>\nDate: Thu, 14 May 2037 at 15:00\nSubject: Invented reservation\n\n"+f.text, f.host)
		}},
		{"duplicate sibling attribution", func(t *testing.T, s *Store, f sourceFixture) {
			sourceExec(t, s, `update entries set body_text=? where id=?`, f.body+"\n\n"+strings.Join(strings.Split(f.body, "\n")[2:], "\n"), f.host)
		}},
		{"ambiguous raw message ID", func(t *testing.T, s *Store, f sourceFixture) {
			r, err := s.Put(Entry{Source: SourceMail, ExtID: "mail:<invented-collision@invented.example.test>", TS: f.rec.Sent, PersonID: f.author, Container: "invented-reservation", BodyText: "This competing copy deliberately has different words."}, &Mail{MessageID: "<invented-reservation@invented.example.test>"}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if err = s.PutGmailCopy(GmailCopy{AccountID: "legacy", GmailID: "invented-collision", EntryID: r.ID}); err != nil {
				t.Fatal(err)
			}
		}},
		{"stale second source claim", func(t *testing.T, s *Store, f sourceFixture) {
			r, err := s.Put(Entry{Source: SourceMail, ExtID: "mail:<invented-stale@invented.example.test>", TS: f.rec.Sent, PersonID: f.other, Container: "invented-reservation", BodyText: "No quotation remains here."}, &Mail{MessageID: "<invented-stale@invented.example.test>", InReplyTo: "<invented-reservation@invented.example.test>"}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if err = s.Sight(f.quote, r.ID, "quoted", ""); err != nil {
				t.Fatal(err)
			}
			// Ingest evaluates its own source; invalidate that too for the shared assertion.
			sourceExec(t, s, `update mail_detail set in_reply_to='' where entry_id=?`, f.host)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := open(t)
			f := seedSourceTwin(t, s, sourceProse)
			assertSourceMatch(t, s, f, true)
			cachedTwinPlan(t, s, true)
			checkpoint(t, s)
			before := reconcileState(t, s)
			tc.mutate(t, s, f)
			after := reconcileState(t, s)
			if after.Source == before.Source || after.Clean() {
				t.Fatal("evidence mutation did not invalidate reconciliation")
			}
			assertSourceMatch(t, s, f, false)
			assertCachedOracle(t, s)
		})
	}
}

func sourceExec(t *testing.T, s *Store, query string, args ...any) {
	t.Helper()
	if _, err := s.db.Exec(query, args...); err != nil {
		t.Fatal(err)
	}
}

func TestSourceTwinAllSightingsAgree(t *testing.T) {
	s := open(t)
	f := seedSourceTwin(t, s, sourceProse)
	r, err := s.Put(Entry{Source: SourceMail, ExtID: "mail:<invented-second-host@invented.example.test>", TS: f.rec.Sent.Add(time.Hour), PersonID: f.other, Container: "invented-reservation", BodyText: f.body}, &Mail{MessageID: "<invented-second-host@invented.example.test>", InReplyTo: "<invented-reservation@invented.example.test>"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.PutGmailCopy(GmailCopy{AccountID: "legacy", GmailID: "invented-second-host", EntryID: r.ID}); err != nil {
		t.Fatal(err)
	}
	if err = s.Sight(f.quote, r.ID, "quoted", ""); err != nil {
		t.Fatal(err)
	}
	assertSourceMatch(t, s, f, true)
	sourceExec(t, s, `update mail_detail set in_reply_to='' where entry_id=?`, r.ID)
	p, err := CollapseTwins(s, false)
	if err != nil || p.Removed != 0 {
		t.Fatalf("one good host overruled conflicting source: %+v %v", p, err)
	}
}

func TestSourceTwinRepeatedShortRepliesAndClockIndependence(t *testing.T) {
	for _, basis := range []string{"stated", "inferred", ""} {
		t.Run(basis, func(t *testing.T) {
			s := open(t)
			f := seedSourceTwin(t, s, "Confirmed; reserve seven baskets.")
			r, err := s.Put(Entry{Source: SourceMail, ExtID: "mail:<invented-repeat@invented.example.test>", TS: time.Date(2037, 5, 14, 9, 3, 35, 0, time.UTC), PersonID: f.author, Container: "invented-reservation", BodyText: f.text}, &Mail{MessageID: "<invented-repeat@invented.example.test>"}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if err = s.PutGmailCopy(GmailCopy{AccountID: "legacy", GmailID: "invented-repeat", EntryID: r.ID}); err != nil {
				t.Fatal(err)
			}
			if basis == "" {
				sourceExec(t, s, `update entries set quote_time_basis=null where id=?`, f.quote)
			} else {
				sourceExec(t, s, `update entries set quote_time_basis=? where id=?`, basis, f.quote)
			}
			assertSourceMatch(t, s, f, true)
			p, err := CollapseTwins(s, true)
			if err != nil || p.Removed != 1 || p.Measured != 0 {
				t.Fatalf("apply=%+v %v", p, err)
			}
		})
	}
}

func TestSourceTwinDataLossVetoes(t *testing.T) {
	for _, kind := range []string{"inline MIME addition", "HTML-derived inline addition", "changed URL", "changed visible image URL", "HTML-derived changed URL", "HTML-derived changed visible image URL", "dropped attachment"} {
		t.Run(kind, func(t *testing.T) {
			s := open(t)
			text := sourceProse
			if strings.Contains(kind, "URL") {
				if strings.Contains(kind, "visible image") {
					text += " <https://inventory.example.test/original.png>"
				} else {
					text += " https://inventory.example.test/original.png"
				}
			}
			f := seedSourceTwin(t, s, text)
			switch kind {
			case "inline MIME addition":
				changed := strings.ReplaceAll(text, "We will count", "Please keep these extra donated tools separate. We will count")
				sourceExec(t, s, `update entries set body_text=? where id=?; update entries set body_text=? where id=?`, strings.ReplaceAll(f.body, text, changed), f.host, changed, f.quote)
				f.rec.Block.Text = changed
			case "HTML-derived inline addition":
				// MIME text agrees exactly, but the original HTML-derived positional veto
				// sees a run absent from the survivor's rendered own content.
				sourceExec(t, s, `update entries set body_html=? where id=?`, "<div>"+strings.ReplaceAll(text, "the remaining pots after lunch and leave the labelled baskets beside the covered workshop bench ", "")+"</div>", f.target)
			case "HTML-derived changed URL", "HTML-derived changed visible image URL":
				sourceExec(t, s, `update entries set body_html=? where id=?`, "<div>"+html.EscapeString(strings.ReplaceAll(text, "original.png", "html-destination.jpg"))+"</div>", f.target)
			case "changed URL", "changed visible image URL":
				changed := strings.ReplaceAll(text, "original.png", "replacement.jpg")
				sourceExec(t, s, `update entries set body_text=? where id=?; update entries set body_text=? where id=?`, strings.ReplaceAll(f.body, "original.png", "replacement.jpg"), f.host, changed, f.quote)
				f.rec.Block.Text = changed
			case "dropped attachment":
				sourceExec(t, s, `insert into attachments(entry_id,name) values(?,'invented-reservation.csv')`, f.quote)
			}
			p, err := CollapseTwins(s, true)
			if err != nil || p.Removed != 0 {
				t.Fatalf("veto lost: %+v %v", p, err)
			}
			if kind != "dropped attachment" {
				id, _, err := sourceTwinDecision(s.db, f.host, f.author, f.rec, "", "invented-reservation")
				ok := id != 0
				if err != nil || (ok && id == f.target) {
					t.Fatalf("ingest ignored veto: %v %v", ok, err)
				}
			}
		})
	}
}

func TestSourceTwinColdWarmReopenedAndAccountTriggers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "invented-source.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	f := seedSourceTwin(t, s, sourceProse)
	cold, c := cachedTwinPlan(t, s, true)
	warm, w := cachedTwinPlan(t, s, true)
	if !reflect.DeepEqual(cold, warm) || c.reductions == 0 || w.reductions != 0 {
		t.Fatal("cold/warm mismatch")
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	reopened, r := cachedTwinPlan(t, s, true)
	if !reflect.DeepEqual(cold, reopened) || r.reductions != 0 {
		t.Fatal("reopened mismatch")
	}
	assertSourceMatch(t, s, f, true)
	for _, tc := range []struct {
		query string
		args  []any
		dirty bool
	}{
		{`update gmail_copies set labels='INBOX',seen_history_id='invented-history' where entry_id=?`, []any{f.host}, false},
		{`update gmail_copies set account_id=account_id,entry_id=entry_id where entry_id=?`, []any{f.host}, false},
		{`delete from gmail_copies where entry_id=?`, []any{f.host}, true},
		{`insert into gmail_copies(account_id,gmail_id,entry_id) values('legacy','invented-return',?)`, []any{f.host}, true},
		{`update gmail_copies set entry_id=? where entry_id=?`, []any{f.quote, f.host}, true},
	} {
		checkpoint(t, s)
		before := reconcileState(t, s)
		sourceExec(t, s, tc.query, tc.args...)
		after := reconcileState(t, s)
		if (before.Source != after.Source) != tc.dirty || after.Clean() == tc.dirty {
			t.Fatalf("revision mismatch for %s", tc.query)
		}
		assertCachedOracle(t, s)
	}
	// Snapshot guards must see an account membership edit even with warm bodies.
	state := reconcileState(t, s)
	sourceExec(t, s, `delete from gmail_copies where entry_id=?`, f.target)
	_, err = s.guardedReconcileWrite(context.Background(), &state.Source, func(corpusWriter) error { t.Fatal("stale writer executed"); return nil })
	if err != ErrReconcileConflict {
		t.Fatalf("account edit did not guard writer: %v", err)
	}
}

func TestSourceTwinNestedWitnessDiscovery(t *testing.T) {
	s := open(t)
	f := seedSourceTwin(t, s, sourceProse)
	lines := strings.Split(f.body, "\n")
	for i := range lines {
		lines[i] = "> " + lines[i]
	}
	nested := "A later planning update.\n\nOn Fri, 15 May 2037 at 17:00, Kel Aspen <kel@invented.example.test> wrote:\n" + strings.Join(lines, "\n")
	r, err := s.Put(Entry{Source: SourceMail, ExtID: "mail:<invented-nested@invented.example.test>", TS: f.rec.Sent.Add(48 * time.Hour), PersonID: f.other, Container: "invented-reservation", BodyText: nested}, &Mail{MessageID: "<invented-nested@invented.example.test>", InReplyTo: "<invented-unrelated-parent@invented.example.test>"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.PutGmailCopy(GmailCopy{AccountID: "legacy", GmailID: "invented-nested", EntryID: r.ID}); err != nil {
		t.Fatal(err)
	}
	// The immediate witness was previously ingested and merged into the mailbox.
	sourceExec(t, s, `update sightings set entry_id=? where entry_id=? and seen_in=?`, f.target, f.quote, f.host)
	if err = s.Sight(f.quote, r.ID, "quoted", ""); err != nil {
		t.Fatal(err)
	}
	f.host = r.ID
	assertSourceMatch(t, s, f, true)
	cachedTwinPlan(t, s, true)
	sourceExec(t, s, `update entries set body_text='The earlier quote was removed.' where id=(select seen_in from sightings where entry_id=? and kind='quoted')`, f.target)
	assertSourceMatch(t, s, f, false)
	assertCachedOracle(t, s)
}

func TestSourceTwinCompetingImmediateWitnesses(t *testing.T) {
	s := open(t)
	f := seedSourceTwin(t, s, sourceProse)
	r, err := s.Put(Entry{Source: SourceMail, ExtID: "mail:<invented-competitor@invented.example.test>", TS: f.rec.Sent.Add(-5*time.Hour - 4*time.Minute), PersonID: f.author, Container: "invented-reservation", BodyText: f.text}, &Mail{MessageID: "<invented-competitor@invented.example.test>"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	h, err := s.Put(Entry{Source: SourceMail, ExtID: "mail:<invented-conflict-host@invented.example.test>", TS: f.rec.Sent.Add(time.Hour), PersonID: f.other, Container: "invented-reservation", BodyText: f.body}, &Mail{MessageID: "<invented-conflict-host@invented.example.test>", InReplyTo: "<invented-competitor@invented.example.test>"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{r.ID, h.ID} {
		if err = s.PutGmailCopy(GmailCopy{AccountID: "legacy", GmailID: fmt.Sprintf("invented-conflict-%d", id), EntryID: id}); err != nil {
			t.Fatal(err)
		}
	}
	if err = s.Sight(f.quote, h.ID, "quoted", ""); err != nil {
		t.Fatal(err)
	}
	assertSourceMatch(t, s, f, false)
}

func TestSourceTwinOnlyWhitespaceNormalization(t *testing.T) {
	for _, text := range []string{"", " \n\t ", strings.ToLower(sourceProse), strings.ReplaceAll(sourceProse, "wooden", "WOODEN"), strings.ReplaceAll(sourceProse, "trays", "boxes")} {
		t.Run(fmt.Sprintf("bytes=%d/%s", len(text), text), func(t *testing.T) {
			s := open(t)
			f := seedSourceTwin(t, s, sourceProse)
			sourceExec(t, s, `update entries set body_text=? where id=?`, text, f.target)
			assertSourceMatch(t, s, f, false)
		})
	}
	s := open(t)
	f := seedSourceTwin(t, s, sourceProse)
	sourceExec(t, s, `update entries set body_text=? where id=?`, "\t"+strings.ReplaceAll(sourceProse, " ", "  \n")+"\n", f.target)
	assertSourceMatch(t, s, f, true)
}

func TestSourceTwinRepeatedShortOtherDatedSightings(t *testing.T) {
	s := open(t)
	f := seedSourceTwin(t, s, "Confirmed; reserve seven baskets.")
	r, err := s.Put(Entry{Source: SourceMail, ExtID: "mail:<invented-other-short@invented.example.test>", TS: f.rec.Sent.Add(-24 * time.Hour), PersonID: f.author, Container: "invented-reservation", BodyText: f.text}, &Mail{MessageID: "<invented-other-short@invented.example.test>"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	otherBody := strings.ReplaceAll(f.body, "14 May 2037 at 15:00", "13 May 2037 at 15:00")
	h, err := s.Put(Entry{Source: SourceMail, ExtID: "mail:<invented-other-dated-host@invented.example.test>", TS: f.rec.Sent, PersonID: f.other, Container: "invented-reservation", BodyText: otherBody}, &Mail{MessageID: "<invented-other-dated-host@invented.example.test>", InReplyTo: "<invented-other-short@invented.example.test>"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{r.ID, h.ID} {
		if err = s.PutGmailCopy(GmailCopy{AccountID: "legacy", GmailID: fmt.Sprintf("invented-other-clock-%d", id), EntryID: id}); err != nil {
			t.Fatal(err)
		}
	}
	if err = s.Sight(r.ID, h.ID, "quoted", ""); err != nil {
		t.Fatal(err)
	}
	assertSourceMatch(t, s, f, true)
	cachedTwinPlan(t, s, true)
	// Once that discovered claim uses the SAME key, its competing immediate
	// target becomes a real ambiguity instead of an unrelated dated sighting.
	sourceExec(t, s, `update entries set body_text=? where id=?`, f.body, h.ID)
	assertSourceMatch(t, s, f, false)
	assertCachedOracle(t, s)
}

func TestAttachSourceTwinRechecksSnapshotBeforeSighting(t *testing.T) {
	for _, mutation := range []string{"account membership", "raw parent", "raw host", "ambiguous Message-ID"} {
		t.Run(mutation, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "source-ingest.db")
			s, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			f := seedSourceTwin(t, s, sourceProse)
			other, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer other.Close()
			calls := 0
			write := func(ctx context.Context, expected *int64, apply func(corpusWriter) error) (time.Duration, error) {
				calls++
				if calls != 1 {
					t.Fatal("invalid proof reached writer again")
				}
				// The read snapshot has proved identity and closed; another
				// connection changes evidence before the guarded attachment.
				switch mutation {
				case "account membership":
					sourceExec(t, other, `delete from gmail_copies where entry_id=?`, f.target)
				case "raw parent":
					sourceExec(t, other, `update mail_detail set in_reply_to='' where entry_id=?`, f.host)
				case "raw host":
					sourceExec(t, other, `update entries set body_text='The quote was removed.' where id=?`, f.host)
				case "ambiguous Message-ID":
					r, err := other.Put(Entry{Source: SourceMail, ExtID: "mail:invented-collision", TS: f.rec.Sent, PersonID: f.author, Container: "invented-reservation", BodyText: "Different competing evidence."}, &Mail{MessageID: "<invented-reservation@invented.example.test>"}, nil)
					if err != nil {
						t.Fatal(err)
					}
					if err := other.PutGmailCopy(GmailCopy{AccountID: "legacy", GmailID: "invented-collision", EntryID: r.ID}); err != nil {
						t.Fatal(err)
					}
				}
				return s.guardedReconcileWrite(ctx, expected, func(w corpusWriter) error {
					t.Fatal("stale attachment executed")
					return apply(w)
				})
			}
			e := Entry{Source: SourceMail, ExtID: f.rec.Key, TS: f.rec.Sent, PersonID: f.author, Container: "invented-reservation", BodyText: f.rec.Block.Text}
			id, created, derived, err := attachSourceTwin(s, f.host, f.rec, e, "depth 0", write)
			if err != nil || id != 0 || created || derived || calls != 1 {
				t.Fatalf("retry must rederive and refuse: id=%d created=%v derived=%v calls=%d err=%v", id, created, derived, calls, err)
			}
			var n int
			if err := s.db.QueryRow(`select count(*) from sightings where entry_id=? and seen_in=? and kind='quoted'`, f.target, f.host).Scan(&n); err != nil || n != 0 {
				t.Fatalf("stale sighting persisted: %d %v", n, err)
			}
		})
	}
}

func TestSourceTwinOversizedEvidenceRefused(t *testing.T) {
	limits := DefaultQuoteLimits
	DefaultQuoteLimits.Bytes = 2048
	t.Cleanup(func() { DefaultQuoteLimits = limits })
	for _, kind := range []string{"candidate plaintext", "raw header candidate", "target HTML"} {
		t.Run(kind, func(t *testing.T) {
			s := open(t)
			f := seedSourceTwin(t, s, sourceProse)
			// Character length is below the limit; its UTF-8 byte length is not.
			large := strings.Repeat("é", 1100)
			switch kind {
			case "candidate plaintext":
				sourceExec(t, s, `update entries set body_text=? where id=?`, large, f.target)
			case "raw header candidate":
				r, err := s.Put(Entry{Source: SourceMail, ExtID: "mail:invented-oversized", TS: f.rec.Sent, PersonID: f.other, Container: "other-invented-thread", BodyText: "Temporary."}, &Mail{MessageID: "<invented-reservation@invented.example.test>"}, nil)
				if err != nil {
					t.Fatal(err)
				}
				if err := s.PutGmailCopy(GmailCopy{AccountID: "legacy", GmailID: "invented-oversized", EntryID: r.ID}); err != nil {
					t.Fatal(err)
				}
				sourceExec(t, s, `update entries set body_text=? where id=?`, large, r.ID)
			case "target HTML":
				sourceExec(t, s, `update entries set body_html=? where id=?`, large, f.target)
			}
			id, d, err := sourceTwinDecision(s.db, f.host, f.author, f.rec, "", "invented-reservation")
			if err != nil || id != 0 || d.Base != 0 {
				t.Fatalf("oversized evidence accepted: id=%d derived=%+v err=%v", id, d, err)
			}
		})
	}
}

func TestAttachSourceTwinRetriesAndCommitsFreshSighting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "source-retry.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	f := seedSourceTwin(t, s, sourceProse)
	other, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	calls := 0
	write := func(ctx context.Context, expected *int64, apply func(corpusWriter) error) (time.Duration, error) {
		calls++
		if calls == 1 {
			// Invalidate the snapshot without changing this address-backed proof.
			sourceExec(t, other, `update people set display_name='Fen Larch (inventory)' where id=?`, f.author)
		}
		return s.guardedReconcileWrite(ctx, expected, apply)
	}
	e := Entry{Source: SourceMail, ExtID: f.rec.Key, TS: f.rec.Sent, PersonID: f.author, Container: "invented-reservation", BodyText: f.rec.Block.Text}
	id, created, derived, err := attachSourceTwin(s, f.host, f.rec, e, "depth 0", write)
	if err != nil || id != f.target || created || derived || calls != 2 {
		t.Fatalf("fresh retry failed: id=%d created=%v derived=%v calls=%d err=%v", id, created, derived, calls, err)
	}
	var n int
	if err := s.db.QueryRow(`select count(*) from sightings where entry_id=? and seen_in=? and kind='quoted'`, f.target, f.host).Scan(&n); err != nil || n != 1 {
		t.Fatalf("fresh sighting not committed exactly once: %d %v", n, err)
	}
}
