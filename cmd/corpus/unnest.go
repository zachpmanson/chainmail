package main

import (
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/zachpmanson/chainmail/internal/corpus"
	"github.com/zachpmanson/chainmail/internal/mailingest"
	"github.com/zachpmanson/chainmail/internal/unnest"
)

// unnestSource is the two places a body can come from.
//
// Both are functions rather than the concrete Store and Client so that a test
// can assert the mailbox was never reached: "prefers the corpus" is otherwise
// unobservable, since a docket read of an already-ingested message returns the
// same bytes and the preference would only show up as speed.
type unnestSource struct {
	show func(string) (corpus.Shown, error)
	read func(string) (mailingest.Message, error)
}

// unnestTarget is one body to peel, with the provenance a reader needs to judge
// what they are looking at.
type unnestTarget struct {
	ID      string
	Origin  string // corpus | docket
	Subject string
	When    string
	Body    string
	// Whole is why this body is one block rather than something to peel, empty
	// when peeling applies. Peeling text that was never a nested mail body cannot
	// recover anything, and can lose: a boundary matched in prose splits an entry
	// that no message ever nested.
	Whole string
}

// reGmailID matches docket's id space: a bare hex token.
//
// Every ext_id the corpus emits carries a scheme — "mail:", "slack:", "quote:" —
// so the two shapes cannot collide, and an id that is in neither space is a
// mistake worth naming rather than handing to a subprocess whose only reply is
// an exit status.
var reGmailID = regexp.MustCompile(`^[0-9a-f]{8,24}$`)

// resolveUnnest finds the body behind an id.
//
// The corpus is asked first because it is the only source that can answer for
// every id: it holds the bodies of messages the mailbox has since lost, and it
// holds the quoted-history entries that have no Gmail id in existence. docket is
// the fallback for a raw Gmail id, which is the pre-ingest case — inspecting
// what a message contains before deciding to keep it.
//
// A stored body is a byte-for-byte copy of what was read (mailingest reads
// uncapped, see mailingest.maxBytes), so preferring it costs nothing in
// fidelity. What it does not reflect is an edit or a resend since ingest; pass
// the Gmail id to force a fresh read.
func resolveUnnest(id string, src unnestSource) (unnestTarget, error) {
	e, err := src.show(id)
	switch {
	case err == nil:
		when := e.TS.Format("Mon 2 Jan 2006 15:04")
		if e.TZ != "" {
			when += " " + e.TZ
		}
		return unnestTarget{
			ID: e.ExtID, Origin: "corpus", Subject: e.Subject, When: when, Body: e.Body,
			Whole: wholeReason(e),
		}, nil
	case !errors.Is(err, corpus.ErrNotFound):
		return unnestTarget{}, err
	}
	if !reGmailID.MatchString(id) {
		return unnestTarget{}, fmt.Errorf(
			"%s is not in the corpus, and is not a Gmail id (those are bare hex, "+
				"e.g. 18f2c3a4b5d6e7f8) — if it is a message you have not kept yet, "+
				"ingest it first: corpus ingest mail -id <gmail-id>", id)
	}
	msg, err := src.read(id)
	if err != nil {
		return unnestTarget{}, err
	}
	return unnestTarget{
		ID: id, Origin: "docket", Subject: msg.Subject, When: msg.Date, Body: msg.Body,
	}, nil
}

// wholeReason decides whether an entry's stored text is a nested mail body at
// all. The two exclusions are the ones internal/spec/body.go styleFor makes, for
// the same reasons: a quoted entry's text is one block that peeling already
// produced, and a newline in Slack is a key the author pressed.
func wholeReason(e corpus.Shown) string {
	switch {
	case e.Quoted:
		return "recovered from quoted text, so already peeled once"
	case e.Source != corpus.SourceMail:
		return "a " + e.Source + " message, not a nested mail body"
	}
	return ""
}

func runUnnest(w io.Writer, src unnestSource, id string, full bool) error {
	t, err := resolveUnnest(id, src)
	if err != nil {
		return err
	}
	printUnnest(w, t, full)
	return nil
}

// printUnnest reports the blocks a body contains.
//
// Block text is clipped to three lines unless -full: the default view answers
// "what is in here and where does it split", which a thirty-message forward
// buries if every block prints whole. -full means the same thing whichever
// source the body came from — it is a property of this printer, not of the read.
func printUnnest(w io.Writer, t unnestTarget, full bool) {
	if t.Whole != "" {
		printUnnestHead(w, t, "not peeled: "+t.Whole)
		fmt.Fprintf(w, "── block 0  depth 0  whole body\n")
		printBlockText(w, strings.TrimSpace(t.Body), full)
		return
	}
	blocks := unnest.Peel(t.Body)
	printUnnestHead(w, t, fmt.Sprintf("%d blocks", len(blocks)))
	for i, b := range blocks {
		fmt.Fprintf(w, "── block %d  depth %d  %s  lines %d-%d\n",
			i, b.Depth, kindName(b.Kind), b.Start, b.End)
		if b.Sentinel != "" {
			for _, l := range strings.Split(b.Sentinel, "\n") {
				fmt.Fprintf(w, "   ⌐ %s\n", trunc(l, 96))
			}
		}
		printBlockText(w, b.Text, full)
	}
}

func printBlockText(w io.Writer, text string, full bool) {
	if !full {
		if lines := strings.Split(text, "\n"); len(lines) > 3 {
			text = strings.Join(lines[:3], "\n") + fmt.Sprintf("\n   … %d more lines", len(lines)-3)
		}
	}
	for _, l := range strings.Split(text, "\n") {
		fmt.Fprintf(w, "     %s\n", trunc(l, 96))
	}
	fmt.Fprintln(w)
}

// printUnnestHead names the entry and where the body came from. A subject line is
// printed only when there is one: a quoted entry often carries none, and an empty
// line reads as a subject that is blank rather than absent.
func printUnnestHead(w io.Writer, t unnestTarget, tail string) {
	fmt.Fprintf(w, "%s  [%s]\n", t.ID, t.Origin)
	if t.Subject != "" {
		fmt.Fprintln(w, t.Subject)
	}
	if t.When != "" {
		fmt.Fprintln(w, t.When)
	}
	fmt.Fprintf(w, "%d bytes, %s\n\n", len(t.Body), tail)
}
