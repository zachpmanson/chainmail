package corpus

import (
	"github.com/zachpmanson/chainmail/internal/unnest"
	"testing"
	"time"
)

func TestAccountProjectionMalformedClockKeepsExactBodyAndDateCaveat(t *testing.T) {
	s := open(t)
	// The extractor's existing malformed English clock fixture, not a fabricated date.
	body := "On Thu, Sep 26, 2024 at 16:33 PM, Alex <alex@example.test> wrote:\n\nSynthetic bulletin."
	blocks := unnest.Peel(body)
	if len(blocks) != 1 {
		t.Fatalf("blocks=%d", len(blocks))
	}
	r := unnest.Parse(blocks[0])
	if !r.Sent.IsZero() {
		t.Fatal("fixture no longer has an unknown date")
	}
	ext := unnest.TextKey(blocks[0].Text)
	q, _, err := s.PutQuoted(Entry{Source: SourceMail, ExtID: ext, TS: time.Unix(1, 0), BodyText: "B private global text"})
	if err != nil {
		t.Fatal(err)
	}
	host, err := s.Put(Entry{Source: SourceMail, ExtID: "mail:host", TS: time.Unix(200, 0), BodyText: body}, &Mail{GmailID: "host"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Sight(q, host.ID, "quoted", "selected host"); err != nil {
		t.Fatal(err)
	}
	sh, err := s.ShowForAccount(ext, "legacy")
	if err != nil {
		t.Fatal(err)
	}
	if sh.Body != blocks[0].Text || sh.DateNote != "Original date unavailable; placement uses an accessible host's timestamp." || sh.TS.Unix() != 200 || sh.TZ != "" {
		t.Fatalf("unsafe undated projection: %+v", sh)
	}
	stored, err := s.Show(ext)
	if err != nil || stored.TS.Unix() != 1 || stored.Body != "B private global text" || stored.DateNote != "" {
		t.Fatalf("projection persisted: %+v %v", stored, err)
	}
}

func TestAccountProjectionDoesNotDedupConflictingExplicitMessageIDDates(t *testing.T) {
	for _, conflicting := range []bool{false, true} {
		t.Run(map[bool]string{false: "agreeing", true: "conflicting"}[conflicting], func(t *testing.T) {
			s := open(t)
			const ext = "mail:quoted-shared@example.test"
			id, _, err := s.PutQuoted(Entry{Source: SourceMail, ExtID: ext, TS: time.Unix(1, 0), BodyText: "B private text"})
			if err != nil {
				t.Fatal(err)
			}
			var longest string
			var original time.Time
			for n := 0; n < 2; n++ {
				date := "Thu, 26 Sep 2024 16:33:00 +0000"
				if n == 1 && conflicting {
					date = "Thu, 26 Sep 2024 17:33:00 +0000"
				}
				text := "Selected host quote."
				if n == 1 {
					text += " Fuller text from another selected host."
				}
				body := "From: Alex <alex@example.test>\nDate: " + date + "\nMessage-ID: <quoted-shared@example.test>\n\n" + text
				blocks := unnest.Peel(body)
				if len(blocks) != 1 {
					t.Fatalf("blocks=%d", len(blocks))
				}
				r := unnest.Parse(blocks[0])
				if r.Key != ext || r.Sent.IsZero() {
					t.Fatalf("explicit ID fixture changed: %+v", r)
				}
				longest = blocks[0].Text
				original = r.Sent
				host, err := s.Put(Entry{Source: SourceMail, ExtID: []string{"mail:host-1", "mail:host-2"}[n], TS: time.Unix(int64(300-n*100), 0), BodyText: body}, &Mail{GmailID: []string{"host-1", "host-2"}[n]}, nil)
				if err != nil {
					t.Fatal(err)
				}
				if err = s.Sight(id, host.ID, "quoted", "selected host"); err != nil {
					t.Fatal(err)
				}
			}
			sh, err := s.ShowForAccount(ext, "legacy")
			if err != nil {
				t.Fatal(err)
			}
			if sh.Body != longest {
				t.Fatalf("date caveat changed selected-host text: %+v", sh)
			}
			if conflicting {
				if sh.TS.Unix() != 200 || sh.TZ != "" || sh.DateNote != "Original date unknown or conflicting; placement uses an accessible host's timestamp." {
					t.Fatalf("dedup invented date consensus: %+v", sh)
				}
			} else if !sh.TS.Equal(original) || sh.DateNote != "" {
				t.Fatalf("genuine consensus lost: %+v", sh)
			}
			stored, err := s.Show(ext)
			if err != nil || stored.TS.Unix() != 1 || stored.Body != "B private text" || stored.DateNote != "" {
				t.Fatalf("projection persisted: %+v %v", stored, err)
			}
		})
	}
}
