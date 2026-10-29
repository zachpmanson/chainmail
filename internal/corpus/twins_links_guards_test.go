package corpus

import (
	"testing"
	"time"
)

func linkCopy(text string, quoted bool) twinCopy {
	return twinCopy{quoted: quoted, words: copyWords(text, "", quoted), prose: proseWords(text, "", quoted), links: copyLinks(text, "", quoted)}
}

func TestQuoteLinkReplacementIsNarrow(t *testing.T) {
	text := "Please review the inventory spreadsheet for the upcoming quarterly stock take."
	oldURL, newURL := "https://docs.example.test/Sheet?key=One", "https://security.example.test/opaque-token"
	for _, tc := range []struct {
		name, a, b string
		want       bool
	}{
		{"final URL", text + " " + oldURL, text + " " + newURL, true},
		{"initial URL", oldURL + " " + text, newURL + " " + text, true},
		{"visible image URL", text + " <https://old.example.test/first.png>", text + " <https://new.example.test/second.jpg>", true},
		{"angle wrapper is not an edit", text + " " + oldURL, text + " <" + oldURL + ">", false},
		{"same URL", text + " " + oldURL, text + " " + oldURL, false},
		{"path case matters", text + " " + oldURL, text + " https://docs.example.test/sheet?key=One", true},
		{"query case matters", text + " " + oldURL, text + " https://docs.example.test/Sheet?key=one", true},
		{"query punctuation matters", text + " " + oldURL + "?", text + " " + oldURL, true},
		{"added URL", text, text + " " + newURL, false},
		{"removed URL", text + " " + oldURL, text, false},
		{"extra tracking URL", text + " " + oldURL, text + " " + oldURL + " " + newURL, false},
		{"moved URL", text + " " + oldURL, newURL + " " + text, false},
		{"different prose", text + " " + oldURL, "Different message signed by the same person about another topic. " + newURL, false},
		{"new footer", text + " " + oldURL, text + " " + newURL + " This is confidential", false},
		{"changed email", "first@example.test " + text + " " + oldURL, "second@example.test " + text + " " + newURL, false},
		{"changed filename", "different-opening-one-two-three-four-five-six-seven-eight.pdf " + text + " " + oldURL, "other-opening-nine-ten-eleven-twelve-thirteen-fourteen-fifteen.pdf " + text + " " + newURL, false},
		{"URL only", oldURL, newURL, false},
		{"tiny common reply", "Thanks see " + oldURL, "Thanks see " + newURL, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, b := linkCopy(tc.a, false), linkCopy(tc.b, true)
			if got := replacedQuoteLinks(a, b); got != tc.want {
				t.Errorf("replacement=%v, want %v", got, tc.want)
			}
		})
	}
}

func TestQuoteLinkReplacementNeedsIdentityAndDirection(t *testing.T) {
	tc := replacedLinkCases(t)[0]
	a, b := linkCopy(tc.Original, false), linkCopy(tc.Quoted, true)
	a.wall = time.Date(2026, 3, 4, 1, 0, 0, 0, time.UTC)
	b.wall = a.wall.Add(12 * time.Hour)
	if _, ok := twinnable(a, b); ok {
		t.Fatal("unvouched short prose matched through URL tokens")
	}
	a.container, b.container = "thread", "thread"
	if _, ok := twinnable(a, b); !ok {
		t.Fatal("same-thread mailbox witness did not vouch short link replacement")
	}
	a.quoted = true
	if _, ok := twinnable(a, b); ok {
		t.Fatal("two short recovered copies cannot vouch for each other")
	}
	if _, inline := annotates(a, b); inline {
		t.Fatal("recovered URL length cannot establish edit direction")
	}
	a.quoted = false
	b.wall = a.wall.Add(12*time.Hour + 5*time.Minute)
	if _, ok := twinnable(a, b); ok {
		t.Fatal("link match bypassed offset lattice")
	}
	b.wall = a.wall.Add(12 * time.Hour)
	b.quoted = false
	if _, ok := twinnable(a, b); ok {
		t.Fatal("two mailbox originals matched")
	}
}

func TestQuoteLinkCandidatesStayAmbiguousBeforeAnnotation(t *testing.T) {
	tc := replacedLinkCases(t)[1]
	a, b := linkCopy(tc.Original, false), linkCopy(tc.Quoted, false)
	a.id, b.id = 1, 2
	a.wall = time.Date(2026, 3, 4, 1, 0, 0, 0, time.UTC)
	b.wall = a.wall
	c := linkCopy(tc.Quoted, true)
	c.wall = a.wall.Add(12 * time.Hour)
	if _, ok := uniqueCopyBase(c, []twinCopy{a, b}); ok {
		t.Fatal("clean mailbox copy won over conflicting edited mailbox copy")
	}
	if _, ok := uniqueCopyBase(c, []twinCopy{b, a}); ok {
		t.Fatal("candidate order changed ambiguity")
	}
	a.quoted, b.quoted = true, true
	if _, ok := uniqueCopyBase(c, []twinCopy{a, b}); ok {
		t.Fatal("several recovered bases were guessed")
	}

	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	author := person(t, s, "rowan@example.test", "Rowan Example")
	for i, body := range []string{tc.Original, tc.Quoted} {
		ext := []string{"mail:<first@example.test>", "mail:<second@example.test>"}[i]
		if _, err := s.Put(Entry{Source: SourceMail, ExtID: ext, TS: a.wall, PersonID: author,
			Subject: tc.Subject, Container: "pilot", BodyText: body}, &Mail{MessageID: ext}, nil); err != nil {
			t.Fatal(err)
		}
	}
	if _, ok, err := FindTwin(s, author, c.wall, tc.Quoted, tc.Subject, "pilot"); err != nil || ok {
		t.Fatalf("ambiguous clean candidate converged: ok=%v err=%v", ok, err)
	}
	if _, ok, err := FindDerived(s, author, c.wall, tc.Quoted, tc.Subject, "pilot"); err != nil || ok {
		t.Fatalf("ambiguous edited candidate selected: ok=%v err=%v", ok, err)
	}
	plan, err := CollapseTwins(s, true)
	if err != nil || plan.Removed != 0 || len(plan.Derived) != 0 {
		t.Fatalf("mailbox originals were changed: plan=%+v err=%v", plan, err)
	}
}
