package mailingest

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/zachpmanson/chainmail/internal/corpus"
	"github.com/zachpmanson/chainmail/internal/unnest"
)

const localizedOwn = "Hello, here is the community garden bulletin."
const localizedForward = "---------- Forwarded message ---------\nDa: Alex Vale <alex@example.test>\nDate: lun 5 ott 2026, 22:07\nSubject: Monthly bulletin\nTo: Robin Reed <robin@example.test>\n\n" + localizedOwn

func TestLocalizedLegacyReingestKeepsIdentityAndFallbackUntilApprovedApply(t *testing.T) {
	s := openTest(t)
	a, _ := corpus.ParseAddress("Alex Vale <alex@example.test>")
	pid, err := corpus.ResolveAddress(s, a, "synthetic")
	if err != nil {
		t.Fatal(err)
	}
	host := corpus.Entry{Source: corpus.SourceMail, ExtID: "mail:localized-host", TS: time.Date(2026, 10, 7, 1, 2, 0, 0, time.UTC), Container: "synthetic", BodyText: localizedForward}
	h, err := s.Put(host, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Model an old textkey/inferred row without invoking the corrected parser.
	old, err := s.Put(corpus.Entry{Source: corpus.SourceMail, ExtID: unnest.TextKey(localizedOwn), TS: host.TS, PersonID: pid, BodyText: localizedOwn, Container: host.Container}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB().Exec(`update entries set quoted=1 where id=?`, old.ID); err != nil {
		t.Fatal(err)
	}
	if err = s.Sight(old.ID, h.ID, "quoted", "depth 0, time inferred from host"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		r, err := ExtractQuoted(s, h.ID, host, localizedForward)
		if err != nil || r.Created != 0 || r.Merged != 1 {
			t.Fatal(r, err)
		}
	}
	var ts int64
	var basis, detail string
	if err = s.DB().QueryRow(`select ts,coalesce(quote_time_basis,'') from entries where id=?`, old.ID).Scan(&ts, &basis); err != nil {
		t.Fatal(err)
	}
	if ts != host.TS.Unix() || basis != "" {
		t.Fatal("reingest promoted legacy clock")
	}
	s.DB().QueryRow(`select detail from sightings where entry_id=?`, old.ID).Scan(&detail)
	if !strings.Contains(detail, "time inferred") {
		t.Fatal("reingest erased inference")
	}
	p, err := s.PlanQuotedMetadata(context.Background(), corpus.QuoteSelection{ExtID: unnest.TextKey(localizedOwn)}, corpus.DefaultQuoteLimits)
	if err != nil || len(p.Changes) != 1 {
		t.Fatal(p, err)
	}
	if _, err = s.ApplyQuotedMetadata(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	r, err := ExtractQuoted(s, h.ID, host, localizedForward)
	if err != nil || r.Created != 0 {
		t.Fatal(r, err)
	}
	var count int
	s.DB().QueryRow(`select count(*) from entries where quoted=1`).Scan(&count)
	if count != 1 {
		t.Fatal("duplicate legacy quote", count)
	}
	changed := strings.Replace(localizedForward, "22:07", "22:08", 1)
	if _, err = s.DB().Exec(`update entries set body_text=? where id=?`, changed, h.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = ExtractQuoted(s, h.ID, host, changed); err == nil {
		t.Fatal("changed source date retargeted stated record")
	}
	s.DB().QueryRow(`select ts from entries where id=?`, old.ID).Scan(&ts)
	if ts != time.Date(2026, 10, 5, 22, 7, 0, 0, time.UTC).Unix() {
		t.Fatal("changed stated clock")
	}
}

func TestLocalizedExtractionStatesBasisAndRetainsAttributionOnlyWrapper(t *testing.T) {
	s := openTest(t)
	body := "On Tue, 6 Oct 2026 at 23:08, Robin Reed <robin@example.test> wrote:\n\n" + localizedForward
	host := corpus.Entry{Source: corpus.SourceMail, ExtID: "mail:outer-host", TS: time.Date(2026, 10, 7, 1, 2, 0, 0, time.UTC), Container: "synthetic", BodyText: body}
	h, err := s.Put(host, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	result, err := ExtractQuoted(s, h.ID, host, body)
	if err != nil || result.Created != 2 || result.Undated != 0 {
		t.Fatal(result, err)
	}
	var stated int
	if err = s.DB().QueryRow(`select count(*) from entries where quoted=1 and quote_time_basis='stated' and quote_time_precision='minute'`).Scan(&stated); err != nil || stated != 2 {
		t.Fatal(stated, err)
	}
	var wrappers int
	if err = s.DB().QueryRow(`select count(*) from entries where quoted=1 and coalesce(body_text,'')='' and coalesce(subject,'')='' and not exists(select 1 from participants where entry_id=entries.id and role='to')`).Scan(&wrappers); err != nil || wrappers != 1 {
		t.Fatal("lost missing-evidence wrapper", wrappers, err)
	}
	// The corresponding mailbox wrapper does not supply the recovered wrapper's
	// missing subject/To; the existing conservative forward guards stay in force.
	a, _ := corpus.ParseAddress("Robin Reed <robin@example.test>")
	pid, err := corpus.ResolveAddress(s, a, "synthetic")
	if err != nil {
		t.Fatal(err)
	}
	mb, err := s.Put(corpus.Entry{Source: corpus.SourceMail, ExtID: "mail:wrapper", TS: time.Date(2026, 10, 6, 22, 8, 0, 0, time.UTC), PersonID: pid, Subject: "Monthly bulletin", Container: "synthetic", BodyText: localizedForward}, &corpus.Mail{MessageID: "wrapper", To: "reader@example.test"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var nested int64
	s.DB().QueryRow(`select id from entries where quoted=1 and body_text=?`, localizedOwn).Scan(&nested)
	if err = s.SetParent(mb.ID, nested); err != nil {
		t.Fatal(err)
	}
	plan, err := corpus.CollapseTwins(s, true)
	if err != nil || plan.Removed != 0 {
		t.Fatal("unsafe missing-evidence collapse", plan, err)
	}
	// An unsupported date uses an explicit inferred state, never PutQuoted's
	// compatibility default for manually supplied stated clocks.
	unknown := strings.Replace(localizedForward, "lun 5 ott 2026, 22:07", "03/04/2026 22:07", 1)
	unknown = strings.Replace(unknown, localizedOwn, "Different undated bulletin.", 1)
	host.ExtID = "mail:unknown-host"
	host.BodyText = unknown
	u, err := s.Put(host, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	result, err = ExtractQuoted(s, u.ID, host, unknown)
	if err != nil || result.Undated != 1 {
		t.Fatal(result, err)
	}
	var inferred int
	if err = s.DB().QueryRow(`select count(*) from entries where quoted=1 and quote_time_basis='inferred' and quote_time_precision='unknown'`).Scan(&inferred); err != nil || inferred != 1 {
		t.Fatal(inferred, err)
	}
}

func TestLocalizedFullHeaderWrapperStillReconcilesWithAffirmativeEvidence(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		t.Run(map[bool]string{false: "mailbox-first", true: "quote-first"}[reverse], func(t *testing.T) {
			s := openTest(t)
			msgs := forwardMessages()
			for i := range msgs {
				msgs[i].Body = strings.ReplaceAll(msgs[i].Body, "Date: Fri, 6 Nov 2026, 21:27", "Date: ven 6 nov 2026, 21:27")
			}
			if reverse {
				msgs[0], msgs[1] = msgs[1], msgs[0]
			}
			for _, m := range msgs {
				if _, err := Put(s, m); err != nil {
					t.Fatal(err)
				}
			}
			plan, err := corpus.CollapseTwins(s, true)
			if err != nil || plan.Removed != 1 {
				t.Fatal(plan, err)
			}
			again, err := corpus.CollapseTwins(s, true)
			if err != nil || again.Removed != 0 {
				t.Fatal(again, err)
			}
		})
	}
}
