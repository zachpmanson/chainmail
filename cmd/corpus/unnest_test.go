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
		"mail:<reply@example.com>", true)
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
	if err := runUnnest(&out, unnestSource{show: s.Show, read: noDocket(t)}, "quote:9f2a1c", true); err != nil {
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
		"mail:<never-ingested@example.com>", false)
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
	if err := runUnnest(&out, unnestSource{show: s.Show, read: noDocket(t)}, "slack:C1:1754000000.001", true); err != nil {
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
	if err := runUnnest(&out, src, "18f2c3a4b5d6e7f8", false); err != nil {
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
