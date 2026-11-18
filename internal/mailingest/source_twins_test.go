package mailingest

import (
	"fmt"
	"html"
	"reflect"
	"strings"
	"testing"

	"github.com/zachpmanson/chainmail/internal/corpus"
)

// Entirely invented inventory conversation, including every identifier and date.
func inventoryMessages() []Message {
	authors := []string{"Mira Wren <mira@stock.example.test>", "Mira Wren <mira@stock.example.test>", "Oren Fern <oren@stock.example.test>", "Oren Fern <oren@stock.example.test>", "Lio Reed <lio@stock.example.test>", "Tavi Elm <tavi@stock.example.test>", "Una Birch <una@stock.example.test>", "Vera Ash <vera@stock.example.test>", "Wes Pine <wes@stock.example.test>"}
	own := []string{
		"The west storeroom count is complete and the spare baskets are now on the upper shelf. Please reserve the blue crates for the school workshop while we check the remaining seed packets against the planning sheet.",
		"A second inventory check found enough wooden trays for the weekend planting session. We should move the empty pots into the covered area before Friday and leave the labelled samples beside the packing bench for collection.",
		"@Neri <neri@stock.example.test> confirmed; reserve seven baskets. @Sora <sora@stock.example.test> @Toma <toma@stock.example.test> please review our inventory plan.\n\nThe greenhouse shelves need a careful count before the volunteer session. Keep the damaged containers separate and record the replacement labels on the shared planning sheet.\n\nItem Quantity Shelf\nSeed trays 47 North\nClay pots 83 South",
		"@Neri <neri@stock.example.test> confirmed; reserve seven baskets.",
		"The reserved crates will stay beside the workshop door until the collection team arrives.",
		"I have added the covered area to our volunteer checklist and marked the samples for pickup.",
		"The revised shelf plan looks ready for the weekend inventory team to use.",
		"Seven baskets are marked for the afternoon group and will not be moved.",
		"Our final planning note is to bring spare labels and pencils for the counting team.",
	}
	dates := []string{"Thu, 14 May 2037 09:03:35 +0000", "Thu, 14 May 2037 11:03:48 +0000", "Thu, 14 May 2037 10:00:00 +0000", "Thu, 14 May 2037 12:00:00 +0000", "Fri, 15 May 2037 09:00:00 +0000", "Fri, 15 May 2037 10:00:00 +0000", "Fri, 15 May 2037 11:00:00 +0000", "Fri, 15 May 2037 12:00:00 +0000", "Fri, 15 May 2037 13:00:00 +0000"}
	msgs := make([]Message, 9)
	for i := range msgs {
		msgs[i] = Message{Envelope: Envelope{ID: fmt.Sprintf("invented-stock-%d", i), MessageID: fmt.Sprintf("<invented-stock-%d@stock.example.test>", i), ThreadID: "invented-shelf-planning", From: authors[i], To: "Crew <crew@stock.example.test>", Subject: "Re: Greenhouse shelf inventory", Date: dates[i]}, Body: own[i], BodyHTML: "<div>" + html.EscapeString(own[i]) + "</div>"}
	}
	initial := "We are preparing the greenhouse inventory for the spring workshop. Please count the trays and baskets before allocating shelf space, and keep a separate list of items that need repair."
	initialText := "---------- Forwarded message ---------\nFrom: Neri Vale <neri@stock.example.test>\nDate: Wed, 13 May 2037 at 08:00\nSubject: Greenhouse shelf inventory\nTo: Crew <crew@stock.example.test>\n\n" + initial
	msgs[0].Body += "\n\n" + initialText
	msgs[0].BodyHTML += `<div class="gmail_quote">` + html.EscapeString(initialText) + `</div>`
	msgs[1].InReplyTo = msgs[2].MessageID
	msgs[2].InReplyTo = msgs[0].MessageID
	msgs[3].InReplyTo = msgs[1].MessageID
	msgs[8].InReplyTo = msgs[7].MessageID
	msgs[2].BodyHTML = `<div style="font-family:serif"><p><a href="mailto:neri@stock.example.test">@Neri</a> confirmed; reserve seven baskets. <a href="mailto:sora@stock.example.test">@Sora</a> <a href="mailto:toma@stock.example.test">@Toma</a> please review our inventory plan.</p><p>The greenhouse shelves need a careful count before the volunteer session. Keep the damaged containers separate and record the replacement labels on the shared planning sheet.</p><table style="border-collapse:collapse"><colgroup><col width="120"><col style="width:80px"><col span="1"><col><col><col><col><col></colgroup><tr><th>Item</th><th>Quantity</th><th>Shelf</th></tr><tr><td>Seed trays</td><td>47</td><td>North</td></tr><tr><td>Clay pots</td><td>83</td><td>South</td></tr></table></div>`
	msgs[3].BodyHTML = `<div><a href="mailto:neri@stock.example.test">@Neri</a> confirmed; reserve seven baskets.</div>`
	clocks := []string{"15:00", "17:00", "18:00", "20:00"}
	previous := []int{-1, 2, 0, 1}
	var trail func(int) (string, string)
	trail = func(i int) (string, string) {
		text, rich := msgs[i].Body, msgs[i].BodyHTML
		if previous[i] >= 0 {
			p, h := trail(previous[i])
			text += "\n\n" + p
			rich += h
		}
		lines := strings.Split(text, "\n")
		for j := range lines {
			lines[j] = "> " + lines[j]
		}
		sentinel := "On Thu, 14 May 2037 at " + clocks[i] + ", " + authors[i] + " wrote:"
		return sentinel + "\n" + strings.Join(lines, "\n"), `<div class="gmail_quote"><div class="gmail_attr">` + html.EscapeString(sentinel) + `</div><blockquote class="gmail_quote">` + rich + `</blockquote></div>`
	}
	// Four, three, two and one sightings respectively: only the outermost
	// block in each host is immediate; the rest are genuine nested requotes.
	for j, i := range []int{0, 2, 1, 3} {
		p, h := trail(i)
		msgs[j+4].InReplyTo = msgs[i].MessageID
		msgs[j+4].Body += "\n\n" + p
		msgs[j+4].BodyHTML += h
	}
	return msgs
}

func TestSourceBackedInventoryRegression(t *testing.T) {
	fixture := inventoryMessages()
	if fixture[2].From != fixture[3].From || !strings.HasPrefix(fixture[2].Body, fixture[3].Body) || len(strings.Fields(fixture[3].Body)) >= 25 {
		t.Fatal("mention replies must share an author and opening, with a short reply below 25 tokens")
	}
	for _, tc := range []struct {
		name       string
		order      []int
		quoteFirst bool
	}{
		{"mailbox-first", []int{0, 2, 1, 3, 4, 5, 6, 7, 8}, false},
		{"quote-first-existing-duplicates", []int{4, 5, 6, 7, 0, 2, 1, 3, 8}, true},
		{"reverse", []int{8, 7, 6, 5, 4, 3, 1, 2, 0}, true},
		{"interleaved", []int{0, 4, 2, 5, 1, 6, 3, 7, 8}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := openTest(t)
			msgs := inventoryMessages()
			ids := make([]int64, 9)
			for _, i := range tc.order {
				r, err := Put(s, msgs[i])
				if err != nil {
					t.Fatal(err)
				}
				ids[i] = r.ID
			}
			// Attachments on the mailbox are authoritative and must survive unchanged.
			if _, err := s.DB().Exec(`insert into attachments(entry_id,name,mime,size) values(?,'invented-shelf.csv','text/csv',147)`, ids[2]); err != nil {
				t.Fatal(err)
			}
			var before int
			if err := s.DB().QueryRow(`select count(*) from entries`).Scan(&before); err != nil {
				t.Fatal(err)
			}
			wantBefore := 10
			if tc.quoteFirst {
				wantBefore = 14
			}
			if before != wantBefore {
				t.Errorf("before=%d want=%d (nine mailbox messages plus genuine missing original)", before, wantBefore)
			}
			dry, err := corpus.CollapseTwins(s, false)
			if err != nil {
				t.Fatal(err)
			}
			wantRemoved := 0
			if tc.quoteFirst {
				wantRemoved = 4
			}
			if dry.Removed != wantRemoved {
				t.Fatalf("dry removed=%d want=%d; declines=%+v", dry.Removed, wantRemoved, dry.Declined)
			}
			applied, err := corpus.CollapseTwins(s, true)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(dry.Collapse, applied.Collapse) {
				t.Fatal("dry/apply plans differ")
			}
			if tc.quoteFirst && applied.Measured != 1 {
				t.Fatalf("off-lattice identities must not create offsets: %+v", applied)
			}
			again, err := corpus.CollapseTwins(s, true)
			if err != nil || again.Removed != 0 {
				t.Fatalf("repeat=%+v err=%v", again, err)
			}
			var after, atts int
			if err := s.DB().QueryRow(`select count(*) from entries`).Scan(&after); err != nil {
				t.Fatal(err)
			}
			if after != 10 {
				t.Fatalf("after=%d want=10", after)
			}
			if err := s.DB().QueryRow(`select count(*) from attachments where entry_id=? and name='invented-shelf.csv' and size=147`, ids[2]).Scan(&atts); err != nil || atts != 1 {
				t.Fatalf("attachment lost: %d %v", atts, err)
			}
			for i, m := range msgs {
				var body, rich string
				if err := s.DB().QueryRow(`select coalesce(body_text,''),coalesce(body_html,'') from entries where id=?`, ids[i]).Scan(&body, &rich); err != nil {
					t.Fatal(err)
				}
				if body != m.Body || rich != m.BodyHTML {
					t.Fatalf("mailbox %d bytes changed", i)
				}
			}
			for i, count := range []int{4, 2, 3, 1} {
				var n int
				if err := s.DB().QueryRow(`select count(*) from sightings where entry_id=? and kind='quoted'`, ids[i]).Scan(&n); err != nil || n != count {
					t.Fatalf("lost sightings %d: %d want %d err=%v", i, n, count, err)
				}
			}
			// Replay nested-first too: rediscover live mailbox sightings rather
			// than relying on an unvalidated quote-key redirect.
			for _, i := range []int{7, 6, 5, 4, 3, 1, 2, 0, 8} {
				if _, err := Put(s, msgs[i]); err != nil {
					t.Fatal(err)
				}
			}
			replay, err := corpus.CollapseTwins(s, false)
			if err != nil || replay.Removed != 0 {
				t.Fatalf("nested replay recreated duplicates: %+v %v", replay, err)
			}
			if err := s.DB().QueryRow(`select count(*) from entries`).Scan(&after); err != nil || after != 10 {
				t.Fatalf("nested replay entries=%d err=%v", after, err)
			}
			if _, err := s.ResolveParents(); err != nil {
				t.Fatal(err)
			}
			for _, start := range ids {
				seen := map[int64]bool{}
				for id := start; id != 0; {
					if seen[id] {
						t.Fatal("reply cycle")
					}
					seen[id] = true
					if err := s.DB().QueryRow(`select coalesce(parent_id,0) from entries where id=?`, id).Scan(&id); err != nil {
						t.Fatal(err)
					}
				}
			}
		})
	}
}

func TestSourceTwinUndatedImmediateAndNestedReplay(t *testing.T) {
	s := openTest(t)
	msgs := inventoryMessages()
	base, host, nested := msgs[3], msgs[7], msgs[8]
	base.InReplyTo = ""
	base.Body = "Confirmed; reserve seven baskets."
	base.BodyHTML = "<div>" + base.Body + "</div>"
	host.Body = "The baskets are labelled for the crew.\n\nOn Thu, Oren Fern <oren@stock.example.test> wrote:\n> " + base.Body
	host.BodyHTML = ""
	quotes := corpus.RecoverQuotes(host.Body).Distinct
	if len(quotes) != 1 || !quotes[0].Sent.IsZero() {
		t.Fatalf("fixture must recover an actually undated immediate attribution: %+v", quotes)
	}
	baseResult, err := Put(s, base)
	if err != nil {
		t.Fatal(err)
	}
	hostResult, err := Put(s, host)
	if err != nil {
		t.Fatal(err)
	}
	var count int
	if err := s.DB().QueryRow(`select count(*) from entries`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("undated ingest created a duplicate: %d %v", count, err)
	}
	nested.Body = "The afternoon team has the list.\n\nOn Fri, 15 May 2037 at 12:00, Vera Ash <vera@stock.example.test> wrote:\n> " + strings.ReplaceAll(host.Body, "\n", "\n> ")
	nested.BodyHTML = ""
	quotes = corpus.RecoverQuotes(nested.Body).Distinct
	if len(quotes) != 2 || !quotes[1].Sent.IsZero() || quotes[1].Block.Depth == 0 {
		t.Fatalf("fixture lost nested undated block: %+v", quotes)
	}
	// Immediate source proof is now attached to the mailbox; nested replay
	// must rediscover it without treating its own parent as the nested base.
	for _, msg := range []Message{nested, host, nested} {
		if _, err := Put(s, msg); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.DB().QueryRow(`select count(*) from entries`).Scan(&count); err != nil || count != 3 {
		t.Fatalf("undated nested replay recreated a duplicate: %d %v", count, err)
	}
	if err := s.DB().QueryRow(`select count(*) from sightings where entry_id=? and kind='quoted'`, baseResult.ID).Scan(&count); err != nil || count != 2 {
		t.Fatalf("undated sightings lost: %d %v", count, err)
	}
	var detail string
	if err := s.DB().QueryRow(`select detail from sightings where entry_id=? and seen_in=? and kind='quoted'`, baseResult.ID, hostResult.ID).Scan(&detail); err != nil || !strings.Contains(detail, "time inferred from host") {
		t.Fatalf("undated sighting marker lost: %q %v", detail, err)
	}
	plan, err := corpus.CollapseTwins(s, true)
	if err != nil || plan.Removed != 0 || plan.Measured != 0 {
		t.Fatalf("undated identity invented clock evidence: %+v %v", plan, err)
	}
}

func TestSourceTwinOffLatticeHTMLAnnotationIngestRepairParity(t *testing.T) {
	for _, quoteFirst := range []bool{false, true} {
		t.Run(fmt.Sprintf("quote-first=%v", quoteFirst), func(t *testing.T) {
			s := openTest(t)
			msgs := inventoryMessages()
			base, host := msgs[0], msgs[4]
			base.Body = "Please reserve the wooden seed trays for the greenhouse inventory team. We will count the remaining pots after lunch and leave the labelled baskets beside the covered workshop bench for tomorrow morning."
			base.BodyHTML = "<div>" + strings.ReplaceAll(base.Body, "the remaining pots after lunch and leave the labelled baskets beside the covered workshop bench ", "") + "</div>"
			host.Body = "The covered bench is ready.\n\nOn Thu, 14 May 2037 at 15:00, Mira Wren <mira@stock.example.test> wrote:\n> " + base.Body
			host.BodyHTML = ""
			order := []Message{base, host}
			if quoteFirst {
				order = []Message{host, base}
			}
			for _, msg := range order {
				if _, err := Put(s, msg); err != nil {
					t.Fatal(err)
				}
			}
			var baseID, copyID, parent int64
			var derived bool
			var text string
			if err := s.DB().QueryRow(`select id from entries where ext_id=?`, "mail:"+base.MessageID).Scan(&baseID); err != nil {
				t.Fatal(err)
			}
			readCopy := func() {
				t.Helper()
				if err := s.DB().QueryRow(`select id,derived,coalesce(parent_id,0),body_text from entries where quoted=1`).Scan(&copyID, &derived, &parent, &text); err != nil {
					t.Fatal(err)
				}
			}
			readCopy()
			if !quoteFirst && (!derived || parent != baseID) {
				t.Fatalf("ingest lost off-lattice proven base: derived=%v parent=%d want=%d", derived, parent, baseID)
			}
			dry, err := corpus.CollapseTwins(s, false)
			if err != nil || dry.Removed != 0 || dry.Measured != 0 || (quoteFirst && len(dry.Derived) != 1) {
				t.Fatalf("repair should retain and classify annotation: %+v %v", dry, err)
			}
			if _, err := corpus.CollapseTwins(s, true); err != nil {
				t.Fatal(err)
			}
			readCopy()
			if !derived || parent != baseID || text != base.Body {
				t.Fatalf("annotation/base lost: id=%d derived=%v parent=%d text=%q", copyID, derived, parent, text)
			}
			var body, rich string
			if err := s.DB().QueryRow(`select coalesce(body_text,''),coalesce(body_html,'') from entries where id=?`, baseID).Scan(&body, &rich); err != nil || body != base.Body || rich != base.BodyHTML {
				t.Fatalf("mailbox bytes changed: %v", err)
			}
			var sightings, count int
			if err := s.DB().QueryRow(`select count(*) from sightings where entry_id=? and kind='quoted'`, copyID).Scan(&sightings); err != nil || sightings != 1 {
				t.Fatalf("annotation sighting lost: %d %v", sightings, err)
			}
			if err := s.DB().QueryRow(`select count(*) from entries`).Scan(&count); err != nil || count != 3 {
				t.Fatalf("annotation was dropped: %d %v", count, err)
			}
		})
	}
}

func inventoryRenderedRequote() Message {
	msgs := inventoryMessages()
	body := msgs[7].Body
	for _, name := range []string{"Neri", "Sora", "Toma"} {
		body = strings.ReplaceAll(body, "@"+name+" <"+strings.ToLower(name)+"@stock.example.test>", "@"+name)
	}
	for _, cells := range [][2]string{{"Item Quantity Shelf", "ItemQuantityShelf"}, {"Seed trays 47 North", "Seed trays47North"}, {"Clay pots 83 South", "Clay pots83South"}} {
		body = strings.ReplaceAll(body, cells[0], cells[1])
	}
	return Message{Envelope: Envelope{ID: "invented-rendered-requote", MessageID: "<invented-rendered-requote@stock.example.test>", ThreadID: msgs[7].ThreadID, From: "Xena Juniper <xena@stock.example.test>", To: msgs[7].To, Subject: msgs[7].Subject, Date: "Sat, 16 May 2037 13:00:00 +0000", InReplyTo: msgs[7].MessageID}, Body: "The morning crew has a copy of this plan.\n\nOn Fri, 15 May 2037 at 12:00, Vera Ash <vera@stock.example.test> wrote:\n> " + strings.ReplaceAll(body, "\n", "\n> ")}
}

func TestSourceBackedInventoryRenderedNestedRequote(t *testing.T) {
	for _, quoteFirst := range []bool{false, true} {
		t.Run(fmt.Sprintf("quote-first=%v", quoteFirst), func(t *testing.T) {
			s := openTest(t)
			msgs := append(inventoryMessages(), inventoryRenderedRequote())
			order := []int{0, 2, 1, 3, 4, 5, 6, 7, 8, 9}
			if quoteFirst {
				order = []int{4, 5, 6, 7, 9, 0, 2, 1, 3, 8}
			}
			ids := make([]int64, len(msgs))
			for _, i := range order {
				r, err := Put(s, msgs[i])
				if err != nil {
					t.Fatal(err)
				}
				ids[i] = r.ID
			}
			before, removals := 11, 0
			if quoteFirst {
				before, removals = 15, 4
			}
			var count int
			if err := s.DB().QueryRow(`select count(*) from entries`).Scan(&count); err != nil || count != before {
				t.Fatalf("before=%d want=%d err=%v", count, before, err)
			}
			dry, err := corpus.CollapseTwins(s, false)
			if err != nil || dry.Removed != removals {
				t.Fatalf("rendered nested claims blocked exact original sources: %+v %v", dry, err)
			}
			applied, err := corpus.CollapseTwins(s, true)
			if err != nil || !reflect.DeepEqual(dry.Collapse, applied.Collapse) {
				t.Fatalf("dry/apply differ: %+v %v", applied, err)
			}
			// Rendered nested-first replay needs to discover the original via
			// HTML, not by globally deleting address expansions in quote text.
			for _, i := range []int{9, 7, 6, 5, 4, 9} {
				if _, err := Put(s, msgs[i]); err != nil {
					t.Fatal(err)
				}
			}
			again, err := corpus.CollapseTwins(s, true)
			if err != nil || again.Removed != 0 {
				t.Fatalf("replay recreated duplicates: %+v %v", again, err)
			}
			if err := s.DB().QueryRow(`select count(*) from entries`).Scan(&count); err != nil || count != 11 {
				t.Fatalf("after=%d want=11 err=%v", count, err)
			}
			for i, m := range msgs {
				var body, rich string
				if err := s.DB().QueryRow(`select coalesce(body_text,''),coalesce(body_html,'') from entries where id=?`, ids[i]).Scan(&body, &rich); err != nil || body != m.Body || rich != m.BodyHTML {
					t.Fatalf("mailbox %d bytes changed: %v", i, err)
				}
			}
			for i, want := range []int{5, 3, 4, 2} {
				if err := s.DB().QueryRow(`select count(*) from sightings where entry_id=? and kind='quoted'`, ids[i]).Scan(&count); err != nil || count != want {
					t.Fatalf("sightings %d=%d want=%d err=%v", i, count, want, err)
				}
			}
		})
	}
}
