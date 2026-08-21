package main

import (
	"errors"
	"fmt"
	"io"
	"net/mail"
	"regexp"
	"strings"
	"time"

	"github.com/zachpmanson/chainmail/internal/corpus"
	"github.com/zachpmanson/chainmail/internal/mailingest"
	"github.com/zachpmanson/chainmail/internal/unnest"
	"github.com/zachpmanson/chainmail/internal/zone"
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
	// Sent and Off are the entry's own send time: a wall clock as the source
	// stated it, and the offset that places it, in minutes east of UTC. This is
	// the one instant in a body that is known exactly rather than parsed out of a
	// sentinel, so it anchors every quoted date judged against it.
	Sent time.Time
	Off  int
	// TZ is the zone label to print beside Sent, as the source stated it.
	TZ string
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
		sent, off := statedClock(e)
		return unnestTarget{
			ID: e.ExtID, Origin: "corpus", Subject: e.Subject, When: when, Body: e.Body,
			Sent: sent, Off: off, TZ: e.TZ, Whole: wholeReason(e),
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
	sent, off := parsedClock(msg.Date)
	return unnestTarget{
		ID: id, Origin: "docket", Subject: msg.Subject, When: msg.Date, Body: msg.Body,
		Sent: sent, Off: off, TZ: offsetLabel(off),
	}, nil
}

// statedClock recovers a stored entry's wall clock and the offset that places it.
//
// entries.ts is the absolute instant, so the pair is exact whatever is returned:
// the offset only decides which wall clock that instant is written as. tz_offset
// comes from the Date header and wins; the label is a fallback because an
// abbreviation does not determine an offset; with neither, the clock shown is UTC
// and the instant it stands for is still right.
func statedClock(e corpus.Shown) (time.Time, int) {
	off := 0
	if e.TZOffset != nil {
		off = *e.TZOffset
	} else if m, ok := zone.Minutes(e.TZ); ok {
		off = m
	}
	return e.TS.UTC().Add(time.Duration(off) * time.Minute), off
}

// parsedClock reads a live message's Date header. A header that will not parse
// leaves the visible message undated, which is honest: nothing else in a body
// states when the outermost message was sent.
func parsedClock(date string) (time.Time, int) {
	t, err := mail.ParseDate(date)
	if err != nil {
		return time.Time{}, 0
	}
	_, secs := t.Zone()
	return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(),
		0, time.UTC), secs / 60
}

// offsetLabel writes minutes east of UTC the way a Date header does. A live read
// has a real offset and no abbreviation, and the numeric form is the honest way
// to show it — an abbreviation inferred from an offset would be a guess between
// several zones sharing it.
func offsetLabel(mins int) string {
	sign := "+"
	if mins < 0 {
		sign, mins = "-", -mins
	}
	return fmt.Sprintf("%s%02d%02d", sign, mins/60, mins%60)
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

// unnestOpts is how the two view flags travel together. They are independent:
// -full changes how much of a block is printed, -chrono changes the order the
// blocks are printed in, and either applies whichever source the body came from.
type unnestOpts struct {
	Full   bool
	Chrono bool
}

func runUnnest(w io.Writer, src unnestSource, id string, o unnestOpts) error {
	t, err := resolveUnnest(id, src)
	if err != nil {
		return err
	}
	printUnnest(w, t, o)
	return nil
}

// printUnnest reports the blocks a body contains.
//
// Block text is clipped to three lines unless -full: the default view answers
// "what is in here and where does it split", which a thirty-message forward
// buries if every block prints whole. -full means the same thing whichever
// source the body came from — it is a property of this printer, not of the read.
func printUnnest(w io.Writer, t unnestTarget, o unnestOpts) {
	if t.Whole != "" {
		printUnnestHead(w, t, "not peeled: "+t.Whole)
		fmt.Fprintf(w, "── block 0  depth 0  whole body\n")
		printBlockText(w, strings.TrimSpace(t.Body), o.Full)
		if o.Chrono {
			// Not an error: asking for chronological order of one block is a
			// reasonable thing to ask, and the answer is that there is no order.
			fmt.Fprintln(w, "one block, so stated dates order nothing")
		}
		return
	}
	blocks := unnest.Peel(t.Body)
	if !o.Chrono {
		printUnnestHead(w, t, fmt.Sprintf("%d blocks", len(blocks)))
		for i, b := range blocks {
			printBlock(w, i, b, "", o.Full)
		}
		return
	}
	printChrono(w, t, blocks, o)
}

// printChrono lists the blocks by the date each one states, and names the
// orderings that cannot be true.
//
// Document order stays the default because it is what a reader checks the parse
// against. This view answers the other question: whether the dates in a body
// agree with the nesting that produced them. A body where they agree prints no
// footer at all — the value of the anomaly lines depends entirely on their being
// rare enough to read.
func printChrono(w io.Writer, t unnestTarget, blocks []unnest.Block, o unnestOpts) {
	ss := stampBlocks(t, blocks)
	stamps := make([]unnest.Stamp, len(ss))
	for i, s := range ss {
		stamps[i] = s.Stamp
	}
	c := unnest.Chrono(stamps, t.Off)

	printUnnestHead(w, t, fmt.Sprintf("%d blocks, by stated date", len(blocks)))
	for _, i := range c.Order {
		printBlock(w, i, blocks[i], ss[i].Label, o.Full)
	}
	if len(c.Undated) > 0 {
		// Printed, and printed under a heading that says why they are here rather
		// than in the order. Dropping them would lose messages that exist nowhere
		// else, and letting them fall silently to the end would read as a claim
		// that they are the oldest.
		wrapNote(w, fmt.Sprintf("── undated  %d %s whose sentinel stated no date; "+
			"nothing places them in the order, so they follow it, in nesting order",
			len(c.Undated), plural(len(c.Undated), "block", "blocks")))
		fmt.Fprintln(w)
		for _, i := range c.Undated {
			printBlock(w, i, blocks[i], "", o.Full)
		}
	}
	printChronoNotes(w, c, ss)
}

// maxInversionsShown caps the footer. One misdated block in a deep forward
// contradicts every block that quotes it, and a page of the same finding buries
// the second, different one.
const maxInversionsShown = 8

func printChronoNotes(w io.Writer, c unnest.Chronology, ss []blockStamp) {
	if c.Moved > 0 {
		// Stated as a count and nothing more. A difference between the two orders
		// is worth knowing about, but on its own it is not evidence of anything:
		// two people in two zones produce it every time they reply to each other.
		wrapNote(w, fmt.Sprintf("%d of %d dated %s sit elsewhere by stated date than by nesting",
			c.Moved, len(c.Order), plural(len(c.Order), "block", "blocks")))
	}
	for n, inv := range c.Impossible {
		if n == maxInversionsShown {
			wrapNote(w, fmt.Sprintf("… and %d more %s dated after a block it is nested inside",
				len(c.Impossible)-n, plural(len(c.Impossible)-n, "block", "blocks")))
			break
		}
		wrapNote(w, fmt.Sprintf("contradiction: block %d (%s) is nested inside block %d (%s), "+
			"so it was sent first, yet its earliest possible instant is %s after that "+
			"block's latest — more than any unstated zone accounts for. One of the two "+
			"dates is wrong, a sentinel was misparsed, or this body was not written "+
			"newest-first.",
			inv.Inner, ss[inv.Inner].Label, inv.Outer, ss[inv.Outer].Label,
			roughly(inv.Excess)))
	}
}

// wrapNote prints a footer note wrapped to the width the block text is truncated
// to, continuation lines indented so a two-line note cannot be misread as two
// findings.
func wrapNote(w io.Writer, text string) {
	const width = 92
	line := ""
	for _, word := range strings.Fields(text) {
		switch {
		case line == "":
			line = word
		case len(line)+1+len(word) <= width:
			line += " " + word
		default:
			fmt.Fprintln(w, line)
			line = "  " + word
		}
	}
	if line != "" {
		fmt.Fprintln(w, line)
	}
}

// blockStamp is one block's stated send time, ready both to order by and to print.
type blockStamp struct {
	Stamp unnest.Stamp
	Label string
}

// stampBlocks reads the send time each block states.
//
// A block with no sentinel is the visible message, and its date is the entry's
// own — the only one in a body that came from a real Date header rather than from
// whatever a quoting client chose to write. Without it the newest message in
// every body would be undateable, and the check that matters most, a quoted block
// dated after the message quoting it, would have nothing to anchor to.
func stampBlocks(t unnestTarget, blocks []unnest.Block) []blockStamp {
	out := make([]blockStamp, len(blocks))
	for i, b := range blocks {
		if b.Sentinel == "" {
			off := t.Off
			out[i] = blockStamp{
				Stamp: unnest.Stamp{Wall: t.Sent, Off: &off},
				Label: stampLabel(t.Sent, t.TZ),
			}
			continue
		}
		r := unnest.Parse(b)
		st := unnest.Stamp{Wall: r.Sent}
		if m, ok := zone.Minutes(r.TZ); ok {
			st.Off = &m
		}
		out[i] = blockStamp{Stamp: st, Label: stampLabel(r.Sent, r.TZ)}
	}
	return out
}

// stampLabel renders a stated date with the zone label as stated, never resolved
// to an offset: the label is what the source claimed, and a reader comparing two
// dates needs to see which of them was placed at all.
func stampLabel(wall time.Time, tz string) string {
	if wall.IsZero() {
		return "no stated date"
	}
	s := wall.Format("Mon 2 Jan 2006 15:04")
	if tz = strings.TrimSpace(tz); tz != "" {
		s += " " + tz
	}
	return s
}

// roughly renders a duration at the coarsest unit that still says how bad it is.
// Minutes matter for an hours-long contradiction and are noise for a month-long
// one.
func roughly(d time.Duration) string {
	switch {
	case d >= 48*time.Hour:
		return fmt.Sprintf("%dd %dh", int(d.Hours())/24, int(d.Hours())%24)
	case d >= time.Hour:
		return fmt.Sprintf("%dh %dm", int(d.Hours()), int(d.Minutes())%60)
	default:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
}

// printBlock names a block and prints its text. dated is appended to the header
// when the caller has a stated date to show, and is empty in the default view,
// where the line ends at the source range a reader checks the parse against.
func printBlock(w io.Writer, i int, b unnest.Block, dated string, full bool) {
	fmt.Fprintf(w, "── block %d  depth %d  %s  lines %d-%d",
		i, b.Depth, kindName(b.Kind), b.Start, b.End)
	if dated != "" {
		fmt.Fprintf(w, "  dated %s", dated)
	}
	fmt.Fprintln(w)
	if b.Sentinel != "" {
		for _, l := range strings.Split(b.Sentinel, "\n") {
			fmt.Fprintf(w, "   ⌐ %s\n", trunc(l, 96))
		}
	}
	printBlockText(w, b.Text, full)
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
