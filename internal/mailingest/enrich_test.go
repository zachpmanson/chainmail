package mailingest

import (
	"testing"
	"time"
)

// Host A quotes the original as a full header block, with a Subject and two Cc'd
// people. Host B quotes the SAME original as a bare Gmail attribution, which
// carries no recipients and no subject at all.
func TestEnrichmentAcrossHosts(t *testing.T) {
	s := openTest(t)
	rich := "---------- Forwarded message ---------\r\n" +
		"From: Ro Laren <ro.laren@x.fed>\r\n" +
		"Date: Wed, 19 Aug 2026 at 07:52\r\n" +
		"Subject: the levy column\r\n" +
		"To: Ana <ana@x.fed>\r\n" +
		"Cc: Bea <bea@x.fed>; Cyd <cyd@x.fed>\r\n" +
		"\r\nthe levy column needs a decision\r\n"
	poor := "On Wed, 19 Aug 2026 at 07:52, Ro Laren <ro.laren@x.fed> wrote:\r\n" +
		"> the levy column needs a decision\r\n"

	put := func(id, body string) {
		msg := Message{Envelope: Envelope{
			ID: id, MessageID: "<" + id + "@h>", ThreadID: "t",
			From: "Zora <zora@x.fed>", Subject: "Re: x",
			Date: time.Date(2026, 8, 20, 9, 0, 0, 0, time.UTC).Format(time.RFC1123Z),
		}, Body: body}
		if _, err := Put(s, msg); err != nil {
			t.Fatal(err)
		}
	}

	for _, order := range []struct {
		name          string
		first, second string
	}{
		{"rich then poor", rich, poor},
		{"poor then rich", poor, rich},
	} {
		t.Run(order.name, func(t *testing.T) {
			s = openTest(t)
			put("a", order.first)
			put("b", order.second)

			var n int
			if err := s.DB().QueryRow(`select count(*) from entries where quoted=1`).Scan(&n); err != nil {
				t.Fatal(err)
			}
			if n != 1 {
				t.Fatalf("quoted entries = %d, want 1", n)
			}
			var subject string
			var cc int
			s.DB().QueryRow(`select coalesce(subject,'') from entries where quoted=1`).Scan(&subject)
			s.DB().QueryRow(`select count(*) from participants p join entries e on e.id=p.entry_id
			                 where e.quoted=1 and p.role='cc'`).Scan(&cc)
			t.Logf("subject=%q cc_participants=%d", subject, cc)
			if subject != "the levy column" {
				t.Errorf("subject = %q, want %q — the richer copy's subject was lost", subject, "the levy column")
			}
			if cc != 2 {
				t.Errorf("cc participants = %d, want 2 — the richer copy's Cc was lost", cc)
			}
		})
	}
}

// Two forwards of the same original, each showing a DIFFERENT subset of the
// recipients — which happens because a quoting client truncates a long list, or
// the forwarder stripped someone. Replacing the role wholesale would let the
// last-seen copy decide who was involved; the union is the honest answer.
func TestRecipientSubsetsUnionRatherThanReplace(t *testing.T) {
	s := openTest(t)
	hdr := func(cc string) string {
		return "---------- Forwarded message ---------\r\n" +
			"From: Ro Laren <ro.laren@x.fed>\r\n" +
			"Date: Wed, 19 Aug 2026 at 07:52\r\n" +
			"Subject: the levy column\r\n" +
			"To: Ana <ana@x.fed>\r\n" +
			"Cc: " + cc + "\r\n\r\nthe levy column needs a decision\r\n"
	}
	for i, cc := range []string{"Bea <bea@x.fed>", "Cyd <cyd@x.fed>"} {
		msg := Message{Envelope: Envelope{
			ID: string(rune('a' + i)), MessageID: "<h" + string(rune('a'+i)) + "@h>",
			ThreadID: "t", From: "Zora <zora@x.fed>", Subject: "Re: x",
			Date: time.Date(2026, 8, 20, 9, 0, 0, 0, time.UTC).Format(time.RFC1123Z),
		}, Body: hdr(cc)}
		if _, err := Put(s, msg); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := s.DB().Query(`
		select p.display_name from participants pa
		join entries e on e.id = pa.entry_id
		join people p on p.id = pa.person_id
		where e.quoted = 1 and pa.role = 'cc' order by p.display_name`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		got = append(got, n)
	}
	if len(got) != 2 {
		t.Fatalf("cc participants = %v, want both Bea and Cyd — a subset replaced the union", got)
	}
}
