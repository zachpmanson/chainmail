package spec

import (
	"strings"
	"testing"
	"time"

	"github.com/zachpmanson/chainmail/internal/corpus"
)

// at is the instant the fixture's message was sent: 2026-03-03T10:00:00+11:00,
// which is the entry's own stated clock rather than a conversion of it.
func at(t *testing.T) time.Time {
	t.Helper()
	ts, err := time.Parse(time.RFC3339, "2026-03-03T10:00:00+11:00")
	if err != nil {
		t.Fatalf("bad ts: %v", err)
	}
	return ts
}

// The quote a reply carries: the reader's own words, then the message being
// answered, attributed with the same clock and the same name its own bubble
// prints. This is the whole of what a reply says that the reader did not type,
// so every part of it is asserted rather than the shape of the string.
func TestAReplyQuotesTheMessageItAnswers(t *testing.T) {
	off := 660
	got := ReplyBody("The 14th works.\n\nI'll confirm with the fitters.\n",
		corpus.ReplyTarget{
			ExtID: "mail:<c0ffee-2@example.com>", GmailID: "g-2",
			Author: "Bo Halvorsen", From: "bo@fjordline.example",
			Subject: "Solar install quote",
			Body:    "Two days of roof access.\n\nBo\n",
			TS:      at(t), TZ: "AEDT", TZOffset: &off,
		})

	// The heading names the instant in the page's own words — the same date, clock
	// and label the entry's own bubble carries — and the sender the way a client
	// writes a person, rather than as an address the corpus reached by name.
	head := "On Tue 3 Mar 2026 10:00 AEDT, Bo Halvorsen <bo@fjordline.example> wrote:"
	if !strings.Contains(got, head) {
		t.Errorf("quote has no heading %q:\n%s", head, got)
	}
	// The reader's own words are kept whole, and the quote stands under them rather
	// than replacing anything they typed. Their trailing newline is not theirs to
	// see duplicated: the body of a message is what it says, not its whitespace.
	if !strings.HasPrefix(got, "The 14th works.\n\nI'll confirm with the fitters.\n\nOn Tue") {
		t.Errorf("the reader's own words are not kept as the opening:\n%q", got)
	}
	// One line at a time, one level in, and the blank line inside the quoted
	// message is the bare mark every client writes for it.
	if !strings.Contains(got, "> Two days of roof access.\n>\n> Bo\n") {
		t.Errorf("the answered message is not quoted line by line:\n%q", got)
	}
}

// A quoted message that already carries quoted lines keeps them, one level deeper:
// a reply that dropped them would be an edit of the message it claims to be. The
// heading is the second case of the same rule — the quote of a quote a Google
// Calendar invite arrives wrapped in is the reader's own trail.
func TestAReplyNestsTheQuotesItsHostAlreadyCarried(t *testing.T) {
	got := ReplyBody("Right.", corpus.ReplyTarget{
		Author: "Bo Halvorsen",
		Body:   "See below.\n\n> the earlier message\n> and its second line\n",
		TS:     at(t),
	})
	if !strings.Contains(got, "> See below.\n>\n> > the earlier message\n> > and its second line\n") {
		t.Errorf("nested quote lines were not kept at one level deeper:\n%q", got)
	}
}

// An instant nothing placed is spelled rather than passed off as the sender's
// clock: stamp then shows UTC, and a page build records the same caveat in its
// source notes. A quote has nowhere to put a note, so the heading says the zone.
func TestAQuoteNeverPresentsAnUnplacedClockAsTheSenders(t *testing.T) {
	got := ReplyBody("ok", corpus.ReplyTarget{
		Author: "Bo Halvorsen", Body: "hello", TS: at(t), TZ: "XYZ",
	})
	if !strings.Contains(got, "On Mon 2 Mar 2026 23:00 UTC, Bo Halvorsen wrote:") {
		t.Errorf("an unplaced clock is not named as UTC:\n%q", got)
	}
}

// What an attribution can honestly name: a person the corpus resolved, the address
// the header stated, or the one without the other. A name with no address gets no
// invented address — the same rule the sender hover follows — and a message the
// corpus cannot name at all says so rather than starting a heading with a comma.
func TestAnAttributionNamesOnlyWhatTheCorpusHolds(t *testing.T) {
	for _, tc := range []struct {
		name          string
		author, from  string
		want, notWant string
	}{
		{name: "both", author: "Ada Okoye", from: "ada@loomworks.example",
			want: "Ada Okoye <ada@loomworks.example>"},
		{name: "name only", author: "Ada Okoye", want: "Ada Okoye wrote:"},
		{name: "address only", from: "ada@loomworks.example",
			want: "ada@loomworks.example wrote:"},
		{name: "neither", want: "somebody wrote:", notWant: ",  wrote:"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := ReplyBody("ok", corpus.ReplyTarget{
				Author: tc.author, From: tc.from, Body: "hello", TS: at(t),
			})
			if !strings.Contains(got, tc.want) {
				t.Errorf("heading does not name %q:\n%q", tc.want, got)
			}
			if tc.notWant != "" && strings.Contains(got, tc.notWant) {
				t.Errorf("heading contains %q:\n%q", tc.notWant, got)
			}
		})
	}
}

// A message with no text of its own is answered under a heading that still names
// it, and no mark stands under a heading with nothing behind it.
func TestAMessageWithNoBodyIsStillAttributed(t *testing.T) {
	got := ReplyBody("Understood.", corpus.ReplyTarget{
		Author: "Bo Halvorsen", Body: "\n\n", TS: at(t),
	})
	if !strings.HasSuffix(got, "Bo Halvorsen wrote:\n") {
		t.Errorf("an empty message was not attributed:\n%q", got)
	}
	if strings.Contains(got, ">") {
		t.Errorf("an empty message was quoted as nothing rather than left unattributed:\n%q", got)
	}
}
