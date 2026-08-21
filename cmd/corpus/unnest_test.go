package main

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zachpmanson/chainmail/internal/corpus"
	"github.com/zachpmanson/chainmail/internal/mailingest"
)

// A reply with one quoted message under an attribution: two blocks, and the
// second exists nowhere else once the mailbox drops the thread.
const replyBody = "Approved, go ahead.\r\n" +
	"\r\n" +
	"On Mon, 3 Aug 2026 at 09:12, Dana Fowler <dana@example.com> wrote:\r\n" +
	"> Can you sign off on the depot roster before Friday?\r\n" +
	"> The night shift needs it.\r\n"

// noDocket fails the test if the mailbox is reached. Asserting the happy path
// alone would pass just as well when unnest quietly went to the network for a
// body it already had.
func noDocket(t *testing.T) func(string) (mailingest.Message, error) {
	t.Helper()
	return func(id string) (mailingest.Message, error) {
		t.Errorf("docket was read for %q; the corpus already holds this body", id)
		return mailingest.Message{}, errors.New("docket must not be called")
	}
}

func seedReply(t *testing.T, s *corpus.Store) {
	t.Helper()
	if _, err := s.Put(corpus.Entry{
		Source: corpus.SourceMail, ExtID: "mail:<reply@example.com>", Kind: "message",
		TS: time.Date(2026, 8, 3, 10, 4, 0, 0, time.UTC), TZ: "AEST",
		Subject: "Depot roster", BodyText: replyBody,
	}, &corpus.Mail{MessageID: "<reply@example.com>"}, nil); err != nil {
		t.Fatal(err)
	}
}

func openStore(t *testing.T) *corpus.Store {
	t.Helper()
	s, err := corpus.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestExtIDIsPeeledFromTheCorpusWithoutReadingTheMailbox(t *testing.T) {
	s := openStore(t)
	seedReply(t, s)

	var out strings.Builder
	err := runUnnest(&out, unnestSource{show: s.Show, read: noDocket(t)},
		"mail:<reply@example.com>", unnestOpts{Full: true})
	if err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{
		"mail:<reply@example.com>  [corpus]",
		"Depot roster",
		"2 blocks",
		"Approved, go ahead.",
		"depot roster before Friday",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output is missing %q:\n%s", want, got)
		}
	}
}

// An entry recovered from someone's quoted text has no Gmail id in existence, so
// this was previously not inspectable by any means.
func TestQuotedEntryIsShownAsTheOneBlockItAlreadyIs(t *testing.T) {
	s := openStore(t)
	// Prose that names a sender and a recipient. Peeling this again would read
	// the pair as a boundary and split an entry that was never nested.
	const body = "Forwarding as discussed.\n" +
		"From: the depot, not the office\n" +
		"To: whoever is on shift\n" +
		"Please keep the roster as it stands.\n"
	if _, _, err := s.PutQuoted(corpus.Entry{
		Source: corpus.SourceMail, ExtID: "quote:9f2a1c", Kind: "message",
		TS:      time.Date(2026, 7, 30, 8, 0, 0, 0, time.UTC),
		Subject: "Depot roster", BodyText: body,
	}); err != nil {
		t.Fatal(err)
	}

	var out strings.Builder
	if err := runUnnest(&out, unnestSource{show: s.Show, read: noDocket(t)}, "quote:9f2a1c", unnestOpts{Full: true}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "quote:9f2a1c  [corpus]") {
		t.Errorf("output does not name the entry or its source:\n%s", got)
	}
	if !strings.Contains(got, "not peeled: recovered from quoted text") {
		t.Errorf("output does not say why it was not peeled:\n%s", got)
	}
	// Every line of the stored text survives: nothing was split off at the
	// hand-typed header pair.
	for _, want := range []string{"Forwarding as discussed.", "From: the depot", "keep the roster"} {
		if !strings.Contains(got, want) {
			t.Errorf("output dropped %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "block 1") {
		t.Errorf("prose was read as a quoted boundary and split:\n%s", got)
	}
}

// The third id shape: ext_id-like, and absent. The old path handed it to docket
// and reported the subprocess's exit status, which names nothing the reader can
// act on.
func TestAbsentExtIDNamesTheProblemRatherThanFailingASubprocess(t *testing.T) {
	s := openStore(t)
	err := runUnnest(io.Discard, unnestSource{show: s.Show, read: noDocket(t)},
		"mail:<never-ingested@example.com>", unnestOpts{})
	if err == nil {
		t.Fatal("an id that is in neither space must be an error")
	}
	msg := err.Error()
	for _, want := range []string{"not in the corpus", "not a Gmail id", "corpus ingest mail"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q does not mention %q", msg, want)
		}
	}
	if strings.Contains(msg, "exit status") {
		t.Errorf("error is a subprocess failure, not a diagnosis: %q", msg)
	}
}

// A Slack message has no quoted history, and a newline in it is a key the author
// pressed rather than a client's wrap, so there is nothing to peel out of one.
func TestSlackEntryIsNotPeeled(t *testing.T) {
	s := openStore(t)
	const body = "shipping it now\nOn Mon, 3 Aug 2026 at 09:12, Dana Fowler <dana@example.com> wrote:\npasted from mail above"
	if _, err := s.Put(corpus.Entry{
		Source: corpus.SourceSlack, ExtID: "slack:C1:1754000000.001", Kind: "message",
		TS: time.Date(2026, 8, 3, 11, 0, 0, 0, time.UTC), BodyText: body,
	}, nil, nil); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	if err := runUnnest(&out, unnestSource{show: s.Show, read: noDocket(t)}, "slack:C1:1754000000.001", unnestOpts{Full: true}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "not peeled: a slack message") {
		t.Errorf("output does not say why it was not peeled:\n%s", got)
	}
	// The pasted attribution is prose here: splitting on it would attribute the
	// author's own words to Dana.
	if strings.Contains(got, "block 1") {
		t.Errorf("a pasted attribution was read as a boundary:\n%s", got)
	}
	if !strings.Contains(got, "pasted from mail above") {
		t.Errorf("output dropped the tail of the message:\n%s", got)
	}
}

func TestRawGmailIDStillReachesDocket(t *testing.T) {
	s := openStore(t)
	var read []string
	src := unnestSource{
		show: s.Show,
		read: func(id string) (mailingest.Message, error) {
			read = append(read, id)
			return mailingest.Message{
				Envelope: mailingest.Envelope{Subject: "Depot roster", Date: "Mon, 3 Aug 2026 10:04:00 +1000"},
				Body:     replyBody,
			}, nil
		},
	}
	var out strings.Builder
	if err := runUnnest(&out, src, "18f2c3a4b5d6e7f8", unnestOpts{}); err != nil {
		t.Fatal(err)
	}
	if len(read) != 1 || read[0] != "18f2c3a4b5d6e7f8" {
		t.Fatalf("docket reads = %v, want the one raw id", read)
	}
	got := out.String()
	if !strings.Contains(got, "18f2c3a4b5d6e7f8  [docket]") || !strings.Contains(got, "2 blocks") {
		t.Errorf("a live read did not peel:\n%s", got)
	}
}

// Both id forms go through the real dispatch, so the plumbing in main.go is
// covered too, not only runUnnest.
func TestPositionalAndFlagFormsAgree(t *testing.T) {
	path := filepath.Join(t.TempDir(), "corpus.db")
	s, err := corpus.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	seedReply(t, s)
	s.Close()
	t.Setenv("CHAINMAIL_CORPUS", path)

	positional := capture(t, func() {
		if err := run([]string{"unnest", "mail:<reply@example.com>"}); err != nil {
			t.Error(err)
		}
	})
	flagged := capture(t, func() {
		if err := run([]string{"unnest", "-id", "mail:<reply@example.com>"}); err != nil {
			t.Error(err)
		}
	})
	if positional != flagged {
		t.Errorf("the two forms disagree:\npositional:\n%s\n-id:\n%s", positional, flagged)
	}
	if !strings.Contains(positional, "2 blocks") {
		t.Errorf("the id search prints did not resolve:\n%s", positional)
	}
}

func capture(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	saved := os.Stdout
	os.Stdout = w
	done := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	fn()
	os.Stdout = saved
	w.Close()
	out := <-done
	r.Close()
	return out
}

// -chrono fixtures. Each is a whole body rather than a fragment, because the
// visible message's own date is half of every comparison: it is the one date in a
// body that came from a real Date header.

// A quoted message stating a clock 86 minutes past the reply that quotes it.
// Ordinary: the two clocks belong to different zones and no more than that.
const zoneShiftedBody = "Approved, go ahead.\r\n" +
	"\r\n" +
	"On Mon, 3 Aug 2026 at 11:30, Dana Fowler <dana@example.com> wrote:\r\n" +
	"> Sign off on the depot roster before Friday?\r\n" +
	"\r\n" +
	"On Sat, 1 Aug 2026 at 07:00, Ravi Oyelaran <ravi@example.com> wrote:\r\n" +
	"> Roster draft attached.\r\n"

// A quoted message stating a date eleven days past everything that quotes it.
// Nothing places it there: a sending clock was wrong, or the sentinel was
// misparsed.
const impossibleBody = "Approved, go ahead.\r\n" +
	"\r\n" +
	"On Sat, 1 Aug 2026 at 07:00, Ravi Oyelaran <ravi@example.com> wrote:\r\n" +
	"> Roster draft attached.\r\n" +
	">\r\n" +
	"> On Fri, 14 Aug 2026 at 08:15, Mira Halloway <mira@example.com> wrote:\r\n" +
	"> > The depot needs three extra nights.\r\n"

// A sentinel naming a weekday and no clock, which is a date nothing can order.
const undatedBody = "Approved, go ahead.\r\n" +
	"\r\n" +
	"On Sat, 1 Aug 2026 at 07:00, Ravi Oyelaran <ravi@example.com> wrote:\r\n" +
	"> Roster draft attached.\r\n" +
	">\r\n" +
	"> On Tuesday, Ola Brenn <ola@example.com> wrote:\r\n" +
	"> > Who is covering the yard?\r\n" +
	"> > The yard has been short all week.\r\n" +
	"> > The overnight crew keeps asking who signs the sheet.\r\n" +
	"> > Tell me before Friday.\r\n"

// docketed peels a body through the live-read path, where the message's own Date
// is a literal this test controls. The stored path formats the head from the
// machine's clock settings, which is not a thing to write assertions against.
func docketed(t *testing.T, body string, o unnestOpts) string {
	t.Helper()
	var out strings.Builder
	src := unnestSource{
		show: func(string) (corpus.Shown, error) { return corpus.Shown{}, corpus.ErrNotFound },
		read: func(string) (mailingest.Message, error) {
			return mailingest.Message{
				Envelope: mailingest.Envelope{
					Subject: "Depot roster", Date: "Mon, 3 Aug 2026 10:04:00 +1000",
				},
				Body: body,
			}, nil
		},
	}
	if err := runUnnest(&out, src, "18f2c3a4b5d6e7f8", o); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

// The regression that matters: -chrono is a view, and the view it is not must
// come out exactly as it did before the flag existed. Anything appended to a
// block header, or to the byte count line, breaks the source ranges a reader
// checks a parse against.
const defaultGolden = `18f2c3a4b5d6e7f8  [docket]
Depot roster
Mon, 3 Aug 2026 10:04:00 +1000
236 bytes, 3 blocks

── block 0  depth 0  visible message  lines 0-2
     Approved, go ahead.

── block 1  depth 0  attribution  lines 2-5
   ⌐ On Mon, 3 Aug 2026 at 11:30, Dana Fowler <dana@example.com> wrote:
     Sign off on the depot roster before Friday?

── block 2  depth 0  attribution  lines 5-8
   ⌐ On Sat, 1 Aug 2026 at 07:00, Ravi Oyelaran <ravi@example.com> wrote:
     Roster draft attached.

`

func TestDefaultOutputIsByteIdenticalWithoutTheFlag(t *testing.T) {
	if got := docketed(t, zoneShiftedBody, unnestOpts{}); got != defaultGolden {
		t.Errorf("default output changed:\ngot:\n%s\nwant:\n%s", got, defaultGolden)
	}
	// -full is the other view, and it must not have acquired a stated date either.
	full := docketed(t, zoneShiftedBody, unnestOpts{Full: true})
	if strings.Contains(full, "dated") || strings.Contains(full, "by stated date") {
		t.Errorf("-full leaked the chronological view:\n%s", full)
	}
}

func TestChronoOrdersBlocksByStatedDate(t *testing.T) {
	got := docketed(t, zoneShiftedBody, unnestOpts{})
	chrono := docketed(t, zoneShiftedBody, unnestOpts{Chrono: true})
	if got == chrono {
		t.Fatal("-chrono changed nothing")
	}
	// Block 1 states 11:30 against the message's own 10:04, so it leads.
	want := []string{"block 1", "block 0", "block 2"}
	var seen []string
	for _, line := range strings.Split(chrono, "\n") {
		if f := strings.Fields(line); len(f) > 2 && f[0] == "──" && f[1] == "block" {
			seen = append(seen, f[1]+" "+f[2])
		}
	}
	if strings.Join(seen, ",") != strings.Join(want, ",") {
		t.Errorf("block order = %v, want %v:\n%s", seen, want, chrono)
	}
	if !strings.Contains(chrono, "dated Mon 3 Aug 2026 11:30") {
		t.Errorf("a block header does not carry the date it was ordered by:\n%s", chrono)
	}
	// 86 minutes is what two zones look like, not what a wrong clock looks like.
	if strings.Contains(chrono, "contradiction") {
		t.Errorf("an ordering difference a zone explains was called a contradiction:\n%s", chrono)
	}
}

// The quiet case, and the one that decides whether the noisy case is worth
// reading.
func TestChronoSaysNothingWhenTheTwoOrdersAgree(t *testing.T) {
	s := openStore(t)
	seedReply(t, s)
	var out strings.Builder
	if err := runUnnest(&out, unnestSource{show: s.Show, read: noDocket(t)},
		"mail:<reply@example.com>", unnestOpts{Chrono: true}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, unwanted := range []string{"contradiction", "sit elsewhere", "undated"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("a body with nothing wrong reported %q:\n%s", unwanted, got)
		}
	}
	if !strings.Contains(got, "2 blocks, by stated date") {
		t.Errorf("the head does not say the order it printed:\n%s", got)
	}
}

func TestChronoReportsAQuotedBlockDatedAfterTheBlockQuotingIt(t *testing.T) {
	got := docketed(t, impossibleBody, unnestOpts{Chrono: true})
	// Findings wrap, so they are read as prose rather than as lines.
	flat := strings.Join(strings.Fields(got), " ")
	if !strings.Contains(flat, "contradiction: block 2") {
		t.Fatalf("the misdated block was not named:\n%s", got)
	}
	for _, want := range []string{
		"nested inside block 1",
		"11d 23h after that block's latest",
		"One of the two dates is wrong",
	} {
		if !strings.Contains(flat, want) {
			t.Errorf("the finding does not say %q:\n%s", want, got)
		}
	}
}

func TestChronoKeepsAnUndatedBlockAndSaysItCannotPlaceIt(t *testing.T) {
	got := docketed(t, undatedBody, unnestOpts{Chrono: true})
	if !strings.Contains(got, "── undated 1 block whose sentinel stated no date") {
		t.Fatalf("the undated block was not accounted for:\n%s", got)
	}
	// Kept, not dropped: this block is a message that exists nowhere else.
	if !strings.Contains(got, "Who is covering the yard?") {
		t.Errorf("an undated block's text was dropped:\n%s", got)
	}
	// And after the order rather than inside it, so its position claims nothing.
	if strings.Index(got, "undated") > strings.Index(got, "Roster draft attached") {
		return
	}
	t.Errorf("an undated block was placed among the dated ones:\n%s", got)
}

func TestChronoComposesWithFull(t *testing.T) {
	clipped := docketed(t, undatedBody, unnestOpts{Chrono: true})
	full := docketed(t, undatedBody, unnestOpts{Chrono: true, Full: true})
	if !strings.Contains(clipped, "… 1 more lines") {
		t.Errorf("the clipped chronological view did not clip:\n%s", clipped)
	}
	if strings.Contains(full, "more lines") {
		t.Errorf("-full did not print the block whole under -chrono:\n%s", full)
	}
	if !strings.Contains(full, "Tell me before Friday") {
		t.Errorf("-full dropped the tail of a block:\n%s", full)
	}
	// Both views order the same way; -full is about how much text, not which.
	if !strings.Contains(full, "── undated") {
		t.Errorf("-full lost the undated section:\n%s", full)
	}
}

// A body that was never peeled has one block, so there is no order to state.
// Erroring here would make -chrono unusable as a default in a shell alias.
func TestChronoOnAnUnpeeledEntrySaysThereIsNothingToOrder(t *testing.T) {
	s := openStore(t)
	if _, _, err := s.PutQuoted(corpus.Entry{
		Source: corpus.SourceMail, ExtID: "quote:9f2a1c", Kind: "message",
		TS: time.Date(2026, 7, 30, 8, 0, 0, 0, time.UTC), BodyText: "Keep the roster.\n",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Put(corpus.Entry{
		Source: corpus.SourceSlack, ExtID: "slack:C1:1754000000.001", Kind: "message",
		TS: time.Date(2026, 8, 3, 11, 0, 0, 0, time.UTC), BodyText: "shipping it now\n",
	}, nil, nil); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"quote:9f2a1c", "slack:C1:1754000000.001"} {
		var out strings.Builder
		if err := runUnnest(&out, unnestSource{show: s.Show, read: noDocket(t)}, id,
			unnestOpts{Chrono: true}); err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		got := out.String()
		if !strings.Contains(got, "one block, so stated dates order nothing") {
			t.Errorf("%s: -chrono said nothing about having nothing to do:\n%s", id, got)
		}
		if strings.Contains(got, "contradiction") || strings.Contains(got, "undated") {
			t.Errorf("%s: an unpeeled body produced findings:\n%s", id, got)
		}
	}
}
