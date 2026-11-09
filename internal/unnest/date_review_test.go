package unnest

import (
	"testing"
	"time"

	"golang.org/x/text/unicode/norm"
)

func TestNumericZonePlacementThroughPipeline(t *testing.T) {
	for _, tc := range []struct{ clock, zone, reason string }{
		{"22:07+0200", "+0200", DateParsed},
		{"22:07:15+02:00", "+02:00", DateParsed},
		{"22:07, +0200", "+0200", DateParsed},
		{"22:07,+0200,", "+0200", DateParsed},
		{"22:07+2460", "", DateInvalidZone},
		{"22:07, +2460", "", DateInvalidZone},
		{"22:07+02:99", "", DateInvalidZone},
		{"22:07+2", "", DateInvalidZone},
		{"22:07+oops", "", DateInvalidZone},
		{"22:07 +0200 -0300", "", DateContradictory},
		{"22:07, +0200, -0300", "", DateContradictory},
		{"25:61+0200", "", DateInvalidClock},
	} {
		for _, header := range []bool{false, true} {
			name := tc.clock
			body := "On 5 Oct 2026 at " + tc.clock + " Alex Vale <alex@example.test> wrote:\n\nPayload."
			if header {
				name += "/header"
				body = "From: Alex Vale <alex@example.test>\nDate: 5 Oct 2026 " + tc.clock + "\nSubject: Bulletin\n\nPayload."
			}
			t.Run(name, func(t *testing.T) {
				blocks := Peel(body)
				if len(blocks) != 1 || blocks[0].Sentinel == "" {
					t.Fatalf("boundary: %+v", blocks)
				}
				r := Parse(blocks[0])
				if r.DateReason != tc.reason || r.TZ != tc.zone {
					t.Fatalf("reason=%q zone=%q: %+v", r.DateReason, r.TZ, r)
				}
				if tc.reason == DateParsed {
					if r.Sent.Year() != 2026 || r.Sent.Month() != time.October || r.Sent.Day() != 5 || r.Sent.Hour() != 22 || r.Sent.Minute() != 7 {
						t.Fatalf("wrong wall clock: %s", r.Sent)
					}
					if r.Address != "alex@example.test" || r.Sender != "Alex Vale" {
						t.Fatalf("zone leaked into author: %+v", r)
					}
				} else if !r.Sent.IsZero() {
					t.Fatalf("fabricated time: %s", r.Sent)
				}
			})
		}
	}
}

func TestAdjacentForwardHeaderCannotBecomeFold(t *testing.T) {
	blocks := Peel("From: Outer Vale <outer@example.test>\nDate: 6 Oct 2026 09:00\nSubject: Outer bulletin\n---------- Forwarded message ---------\nDa: Alex Vale <alex@example.test>\nDate: lun 5 ott 2026, 22:07\nOggetto: Inner bulletin\n\nPayload.")
	if len(blocks) != 2 {
		t.Fatalf("forward swallowed into headers: %+v", blocks)
	}
	if a, b := Parse(blocks[0]), Parse(blocks[1]); a.Subject != "Outer bulletin" || b.Subject != "Inner bulletin" || b.DateReason != DateParsed || a.Address != "outer@example.test" || b.Address != "alex@example.test" {
		t.Fatalf("mismatched headers: %+v / %+v", a, b)
	}
}

func TestAttributionStructuralNormalizationPreservesAuthorBytes(t *testing.T) {
	const author = "Andre\u0301 Vale"
	for _, tc := range []struct{ name, opener, date, closer string }{
		{"Swedish", "På", "mån 5 okt 2026 kl 22:07", "skrev:"},
		{"French", "Le", "lun 5 oct 2026 à 22:07", "a écrit :"},
	} {
		for _, decomposed := range []bool{false, true} {
			name := tc.name + "/NFC"
			opener, closer := norm.NFC.String(tc.opener), norm.NFC.String(tc.closer)
			if decomposed {
				name = tc.name + "/NFD"
				opener, closer = norm.NFD.String(opener), norm.NFD.String(closer)
			}
			t.Run(name, func(t *testing.T) {
				body := opener + " " + tc.date + " " + author + " <alex@example.test> " + closer + "\n\nPayload."
				blocks := Peel(body)
				if len(blocks) != 1 || blocks[0].Kind != KindAttribution {
					t.Fatalf("boundary: %+v", blocks)
				}
				r := Parse(blocks[0])
				if r.DateReason != DateParsed || r.Sent != time.Date(2026, 10, 5, 22, 7, 0, 0, time.UTC) || r.Sender != author || r.Address != "alex@example.test" {
					t.Fatalf("structural normalization altered result: %+v", r)
				}
			})
		}
	}
}
