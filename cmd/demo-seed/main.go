// A throwaway seeder for a demo corpus: three threads, two of them from one
// notification sender, so the per-person reading style can be watched staying
// put across threads rather than per message. Not part of the product; it exists
// so the corpus-backed switch can be looked at without a real mailbox.
package main

import (
	"fmt"
	"os"
	"time"

	"github.com/zachpmanson/chainmail/internal/corpus"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: demo-seed <corpus.db>")
		os.Exit(2)
	}
	path := os.Args[1]
	for _, suffix := range []string{"", "-wal", "-shm"} {
		os.Remove(path + suffix)
	}
	s, err := corpus.Open(path)
	if err != nil {
		panic(err)
	}
	defer s.Close()

	me, err := corpus.Resolve(s, corpus.KindEmail, "zach@example.com", "Zach Manson")
	if err != nil {
		panic(err)
	}
	if err := s.SetMePerson(me); err != nil {
		panic(err)
	}
	notify, err := corpus.Resolve(s, corpus.KindEmail, "notifications@loomworks.example", "Loomworks Notifications")
	if err != nil {
		panic(err)
	}
	ada, err := corpus.Resolve(s, corpus.KindEmail, "ada@loomworks.example", "Ada Okoye")
	if err != nil {
		panic(err)
	}

	html := func(title, line string) string {
		return `<div style="font-family:sans-serif;background:#f4f1ea;padding:24px">` +
			`<h2 style="color:#7a3b2e;margin:0 0 8px">` + title + `</h2>` +
			`<p style="color:#333">` + line + `</p>` +
			`<p style="color:#666;font-size:12px">Sent by a script, which is the point: ` +
			`the page's own rendering of this loses the background and the colours.</p></div>`
	}

	put := func(ext, subject, from string, ts time.Time, body, own string, who int64, to string) int64 {
		res, err := s.Put(corpus.Entry{
			Source: corpus.SourceMail, ExtID: ext, Kind: "message", TS: ts, TZ: "AEDT",
			TZOffset: ptr(660), Subject: subject, BodyText: body, BodyHTML: own, PersonID: who,
		}, &corpus.Mail{MessageID: ext[5:], From: from, To: to}, nil)
		if err != nil {
			panic(err)
		}
		if _, err := corpus.RecordHeader(s, res.ID, corpus.RoleTo, to); err != nil {
			panic(err)
		}
		return res.ID
	}

	// Two threads from the notification sender: the reader who has decided how to
	// read one of them has decided it for both.
	day := time.Date(2026, 3, 2, 9, 15, 0, 0, time.FixedZone("AEDT", 3600*11))
	first := put("mail:<demo-notify-1@loomworks.example>", "Loom cutover: Friday",
		"Loomworks Notifications <notifications@loomworks.example>", day,
		"Roof access is booked for Friday 9am.",
		html("Roof access booked", "Friday 9am, gate code 4482."), notify, "zach@example.com")
	second := put("mail:<demo-notify-2@loomworks.example>", "Loom cutover: Friday",
		"Loomworks Notifications <notifications@loomworks.example>", day.Add(3*time.Hour),
		"Reminder: the lift is out on Friday.",
		html("Lift out of service", "Use the stairwell by the loading dock."), notify, "zach@example.com")
	if err := s.SetParent(second, first); err != nil {
		panic(err)
	}
	third := put("mail:<demo-notify-3@loomworks.example>", "Your Tuesday digest",
		"Loomworks Notifications <notifications@loomworks.example>", day.Add(26*time.Hour),
		"Three new sites this week.",
		html("This week at Loomworks", "Three new sites, two new faces."), notify, "zach@example.com")

	// A different sender, whose mail is a conversation rather than a broadcast:
	// nothing the reader decides about the notifier touches her.
	adaFirst := put("mail:<demo-ada-1@loomworks.example>", "Re: fence panels",
		"Ada Okoye <ada@loomworks.example>", day.Add(time.Hour),
		"The panels arrive Tuesday. Can you be there?", "", ada, "zach@example.com")
	adaSecond := put("mail:<demo-ada-2@loomworks.example>", "Re: fence panels",
		"Ada Okoye <ada@loomworks.example>", day.Add(90*time.Minute),
		"Tuesday works. I'll bring the drill.", "", ada, "zach@example.com")
	if err := s.SetParent(adaSecond, adaFirst); err != nil {
		panic(err)
	}

	var entries, people int
	if err := s.DB().QueryRow(`select count(*) from entries`).Scan(&entries); err != nil {
		panic(err)
	}
	if err := s.DB().QueryRow(`select count(*) from people`).Scan(&people); err != nil {
		panic(err)
	}
	fmt.Printf("seeded %s: %d entries, %d people, roots %d %d %d\n",
		path, entries, people, first, third, adaFirst)
}

func ptr(v int) *int { return &v }
