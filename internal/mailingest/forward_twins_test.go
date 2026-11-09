package mailingest

import (
	"context"
	"html"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zachpmanson/chainmail/internal/corpus"
)

// Invented identities and prose; the two clients preserve different clocks and
// split an otherwise identical bare forward into different storage scopes.
const bulletinText = `Progress:
The community garden team has finished mapping the northern beds and checking the water supply. Next month we will plant beans beside the fence, repair the tool shed, and invite the neighbourhood to a weekend working bee. Please bring gloves and a spare watering can if you have one.`
const bulletinForward = "---------- Forwarded message ---------\nFrom: Alex Vale <alex@example.net>\nDate: Fri, 6 Nov 2026, 21:27\nSubject: Re: Fwd: Monthly bulletin\nTo: Robin Reed <robin@example.net>\n\n" + bulletinText
const bulletinReply = "Thanks for passing this along.\n\nFrom: Alex Vale <alex@example.net>\nDate: Sunday, 8 November 2026 at 4:10 pm\nTo: casey@example.org <casey@example.org>\nSubject: Fwd: Fwd: Monthly bulletin\n\n\n" + bulletinForward

func forwardMessages() []Message {
	return []Message{
		{Envelope: Envelope{ID: "synthetic-forward", MessageID: "<synthetic-forward@example.test>", ThreadID: "synthetic-bulletin", From: "Alex Vale <alex@example.net>", To: "Casey Lake <casey@example.org>", Subject: "Fwd: Fwd: Monthly bulletin", Date: "Sun, 8 Nov 2026 16:10:44 +1100"}, Body: bulletinForward},
		{Envelope: Envelope{ID: "synthetic-reply", MessageID: "<synthetic-reply@example.test>", ThreadID: "synthetic-bulletin", From: "Casey Lake <casey@example.org>", To: "Alex Vale <alex@example.net>", Subject: "Re: Fwd: Fwd: Monthly bulletin", Date: "Mon, 9 Nov 2026 09:00:00 +1100"}, Body: bulletinReply},
	}
}

func TestRecoveredEmptyForwardWrapper(t *testing.T) {
	for _, tc := range []struct {
		name          string
		reverse, html bool
	}{
		{"mailbox-first", false, false}, {"quote-first", true, false},
		{"mailbox-first-html", false, true}, {"quote-first-html", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := openTest(t)
			msgs := forwardMessages()
			if tc.html {
				msgs[0].BodyHTML = "<div>" + strings.ReplaceAll(html.EscapeString(bulletinForward), "\n", "<br>") + "</div>"
			}
			wantHTML := msgs[0].BodyHTML
			if tc.reverse {
				msgs[0], msgs[1] = msgs[1], msgs[0]
			}
			for _, m := range msgs {
				if _, err := Put(s, m); err != nil {
					t.Fatal(err)
				}
			}
			var keep, drop, inner, child, wrapperTS int64
			if err := s.DB().QueryRow(`select id from entries where ext_id='mail:<synthetic-forward@example.test>'`).Scan(&keep); err != nil {
				t.Fatal(err)
			}
			if err := s.DB().QueryRow(`select id, parent_id,ts from entries where quoted=1 and coalesce(body_text,'')=''`).Scan(&drop, &inner, &wrapperTS); err != nil {
				t.Fatalf("parser must recover an empty wrapper: %v", err)
			}
			if wrapperTS != time.Date(2026, 11, 8, 16, 10, 0, 0, time.UTC).Unix() {
				t.Fatalf("quoted clock was not naive UTC: %d", wrapperTS)
			}
			if err := s.DB().QueryRow(`select id from entries where ext_id='mail:<synthetic-reply@example.test>' and parent_id=?`, drop).Scan(&child); err != nil {
				t.Fatal(err)
			}
			// Simulate a previously reconciled corpus at the old algorithm version.
			if _, err := s.DB().Exec(`update reconcile_state set ready=1, covered_revision=source_revision, rules_version=2, covered_version=2`); err != nil {
				t.Fatal(err)
			}
			state, err := s.ReconciliationState()
			if err != nil {
				t.Fatal(err)
			}
			if state.Clean() {
				t.Error("old rules must schedule normal reconciliation after upgrade")
			}
			dry, err := corpus.CollapseTwins(s, false)
			if err != nil {
				t.Fatal(err)
			}
			if len(dry.Collapse) != 1 || dry.Collapse[0].Keep != keep || !reflect.DeepEqual(dry.Collapse[0].Drop, []int64{drop}) {
				t.Fatalf("empty wrapper not paired with mailbox: %+v", dry)
			}
			applied, err := corpus.CollapseTwins(s, true)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(dry.Collapse, applied.Collapse) {
				t.Fatalf("dry/apply mismatch: %+v / %+v", dry, applied)
			}
			var body, bodyHTML string
			var ts, parent int64
			if err := s.DB().QueryRow(`select body_text,coalesce(body_html,''),ts,parent_id from entries where id=?`, keep).Scan(&body, &bodyHTML, &ts, &parent); err != nil {
				t.Fatal(err)
			}
			wantTS := time.Date(2026, 11, 8, 5, 10, 44, 0, time.UTC).Unix()
			if body != bulletinForward || bodyHTML != wantHTML || ts != wantTS || parent != inner {
				t.Fatalf("mailbox changed: body=%q ts=%d parent=%d", body, ts, parent)
			}
			if err := s.DB().QueryRow(`select parent_id from entries where id=?`, child).Scan(&parent); err != nil || parent != keep {
				t.Fatalf("child parent=%d err=%v", parent, err)
			}
			var sightings, recipients int
			if err := s.DB().QueryRow(`select count(*) from sightings where entry_id=? and seen_in=? and kind='quoted'`, keep, child).Scan(&sightings); err != nil {
				t.Fatal(err)
			}
			if err := s.DB().QueryRow(`select count(*) from participants where entry_id=? and role='to'`, keep).Scan(&recipients); err != nil {
				t.Fatal(err)
			}
			if sightings != 1 || recipients != 1 {
				t.Fatalf("lost evidence: sightings=%d recipients=%d", sightings, recipients)
			}
			for _, m := range msgs {
				if _, err := Put(s, m); err != nil {
					t.Fatal(err)
				}
			}
			// Re-extraction may recreate the empty row before its graph is established;
			// the supported reconciliation pass must converge without changing the mail.
			if _, err := corpus.CollapseTwins(s, true); err != nil {
				t.Fatal(err)
			}
			start, err := s.ReconciliationState()
			if err != nil {
				t.Fatal(err)
			}
			stable, err := corpus.CollapseTwins(s, true)
			if err != nil || stable.Removed != 0 {
				t.Fatalf("not stable: %+v %v", stable, err)
			}
			repair, err := corpus.RepairQuotedBodies(s)
			if err != nil || repair.Fixed != 0 {
				t.Fatalf("repair: %+v %v", repair, err)
			}
			ok, err := s.CheckpointReconciliation(context.Background(), start)
			if err != nil || !ok {
				t.Fatalf("checkpoint: %v %v", ok, err)
			}
			end, err := s.ReconciliationState()
			if err != nil || !end.Clean() {
				t.Fatalf("not clean: %+v %v", end, err)
			}
		})
	}
}
