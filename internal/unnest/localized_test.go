package unnest

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

type localeFixture struct {
	Name      string
	Body      string
	When      string
	TZ        string
	Precision string
	Reason    string
}

func localizedFixtures(t *testing.T) []localeFixture {
	t.Helper()
	b, err := os.ReadFile("testdata/localized/formats.json")
	if err != nil {
		t.Fatal(err)
	}
	var fs []localeFixture
	if err := json.Unmarshal(b, &fs); err != nil {
		t.Fatal(err)
	}
	return fs
}

func TestLocalizedPipeline(t *testing.T) {
	for _, f := range localizedFixtures(t) {
		t.Run(f.Name, func(t *testing.T) {
			blocks := Peel(f.Body)
			if len(blocks) != 1 {
				t.Fatalf("blocks=%d, want 1", len(blocks))
			}
			r := Parse(blocks[0])
			if r.Address != "alex@example.test" || r.Block.Text != "Synthetic bulletin." {
				t.Errorf("author/body not retained: %+v", r)
			}
			got := ""
			if !r.Sent.IsZero() {
				got = r.Sent.Format("2006-01-02 15:04:05")
			}
			if got != f.When || r.TZ != f.TZ {
				t.Errorf("date=%q zone=%q, want %q %q", got, r.TZ, f.When, f.TZ)
			}
			if r.DateReason != f.Reason || r.DatePrecision != f.Precision {
				t.Errorf("reason/precision=%s/%s, want %s/%s", r.DateReason, r.DatePrecision, f.Reason, f.Precision)
			}
		})
	}
}

func TestLocalizedNestedForward(t *testing.T) {
	body := "On Tue, 6 Oct 2026 at 09:12, Robin Reed <robin@example.test> wrote:\n" +
		"> ---------- Forwarded message ---------\n> Da: Alex Vale <alex@example.test>\n" +
		"> Date: lun 5 ott 2026, 22:07\n> Subject: Monthly bulletin\n> A: Robin <robin@example.test>\n\n> Synthetic bulletin."
	blocks := Peel(body)
	if len(blocks) != 2 {
		t.Fatalf("blocks=%d, want wrapper + nested item", len(blocks))
	}
	wrapper, nested := Parse(blocks[0]), Parse(blocks[1])
	if wrapper.Block.Text != "" || wrapper.Subject != "" || wrapper.To != "" {
		t.Fatal("invented wrapper identity evidence")
	}
	if nested.Sent.Format("2006-01-02 15:04") != "2026-10-05 22:07" || nested.To != "Robin <robin@example.test>" || nested.Block.Depth != 1 {
		t.Errorf("nested metadata not recovered: %+v", nested)
	}
}

func TestLocalizedMalformedClockNeverMidnight(t *testing.T) {
	for _, clock := range []string{"25:61", "12:99", "12:00:99", "12:00:00:01", "12:00oops", "112:00", "12:00.5", "0:12 PM", "13:12 PM", "16:33 PM"} {
		t.Run(clock, func(t *testing.T) {
			blocks := Peel("From: Alex <alex@example.test>\nDate: 5 Oct 2026 " + clock + "\n\nSynthetic bulletin.")
			r := Parse(blocks[0])
			if !r.Sent.IsZero() {
				t.Errorf("malformed clock became stated time: %s", r.Sent)
			}
		})
	}
}

func TestLocalizedOneLetterAndBoldAliases(t *testing.T) {
	for _, line := range []string{"A: Robin", "Onderwerp: Bulletin", "*Envoyé:* lun. 5 oct. 2026 à 22:07", "*Ämne:* Bulletin"} {
		blocks := Peel("From: Alex <alex@example.test>\n" + line + "\n\nSynthetic bulletin.")
		if len(blocks) != 1 || strings.Contains(blocks[0].Text, line) {
			t.Errorf("alias lost: %q", line)
		}
	}
}

func TestLocalizedMixedLanguageChronology(t *testing.T) {
	body := "From: Robin <robin@example.test>\nDate: Tue, 6 Oct 2026 09:12\nSubject: Bulletin\nTo: Casey <casey@example.test>\n\n" +
		"> Il lun 5 ott 2026 alle 22:07, Alex <alex@example.test> ha scritto:\n" +
		"> > De: Casey <casey@example.test>\n> > Envoyé: dim. 4 oct. 2026 à 20:15:30\n> > Objet: Bulletin\n\n> > Synthetic bulletin."
	blocks := Peel(body)
	if len(blocks) != 3 {
		t.Fatalf("blocks=%d, want three mixed-language boundaries", len(blocks))
	}
	var stamps []Stamp
	for i, b := range blocks {
		r := Parse(b)
		if r.DateReason != DateParsed || r.TZ != "" {
			t.Errorf("block %d lost stated date or invented a zone: %+v", i, r)
		}
		stamps = append(stamps, Stamp{Wall: r.Sent})
	}
	c := Chrono(stamps, 0)
	if len(c.Undated) != 0 || len(c.Impossible) != 0 || len(c.Order) != 3 || c.Order[0] != 0 || c.Order[1] != 1 || c.Order[2] != 2 {
		t.Errorf("localized stated times did not order newest first: %+v", c)
	}
	if blocks[1].Text != "" || blocks[1].Depth != 1 || blocks[2].Depth != 2 || blocks[2].Text != "Synthetic bulletin." {
		t.Errorf("mixed-language nesting/content changed: %+v", blocks)
	}
}

func TestExistingDetectionOnlyLanguages(t *testing.T) {
	for _, tc := range []struct {
		name, line string
		detected   bool
		reason     string
	}{
		{"Vietnamese", "Vào Thứ Hai, 5 tháng 10 năm 2026 lúc 22:07, Alex <alex@example.test> đã viết:", true, DateUnsupported},
		{"Romanian opener only", "Pe luni, 5 octombrie 2026 la 22:07, Alex <alex@example.test> a scris:", false, ""},
		{"Romanian mixed closer", "Pe luni, 5 octombrie 2026 la 22:07, Alex <alex@example.test> wrote:", true, DateUnsupported},
		{"Finnish closer only", "maanantaina 5. lokakuuta 2026 klo 22:07, Alex <alex@example.test> kirjoitti:", false, ""},
		{"Finnish mixed opener", "On maanantaina 5. lokakuuta 2026 klo 22:07, Alex <alex@example.test> kirjoitti:", true, DateUnsupported},
		{"Chinese spaced boundary", "在 2026年10月5日 22:07, Alex <alex@example.test> 写道：", true, DateUnsupported},
		{"Korean closer only", "2026년 10월 5일 22:07, Alex <alex@example.test> 작성:", false, ""},
		{"Korean mixed opener", "On 2026년 10월 5일 22:07, Alex <alex@example.test> 작성:", true, DateUnsupported},
	} {
		t.Run(tc.name, func(t *testing.T) {
			blocks := Peel(tc.line + "\n\nSynthetic bulletin.")
			if len(blocks) != 1 {
				t.Fatalf("blocks=%d", len(blocks))
			}
			r := Parse(blocks[0])
			if (r.Block.Sentinel != "") != tc.detected || !r.Sent.IsZero() {
				t.Errorf("unsupported grammar claimed a date/boundary: %+v", r)
			}
			if tc.detected && (r.DateReason != tc.reason || r.Address != "alex@example.test") {
				t.Errorf("detected unsupported date lost reason/author: %+v", r)
			}
		})
	}
}
