// Package boiler finds the block a sender appends to their messages rather than
// writes in them: a signature, an organisation's confidentiality notice.
//
// The signal is repetition and nothing else. A run of lines that ends many of
// one person's messages verbatim is a block they append; a run that ends one is
// prose. No line is read, no pattern is matched, no phone number or job title is
// recognised — which is what makes this portable to a corpus in any language and
// safe on a body that happens to end in an address. talon needs a trained
// classifier to make the same call from a single message; a corpus does not have
// to guess, because it has the other 40 messages that person sent.
//
// Two scopes, because the two kinds of appended block belong to different
// things. A signature is one person's, and repeats across their messages. A
// confidentiality notice is the organisation's, and repeats across every sender
// at its domain — including senders who appear once and whose own messages
// therefore prove nothing. Detecting only per-person would leave exactly those
// notices in view, and they are the longest blocks on the page.
//
// Nothing here deletes. Detect says which trailing lines are boilerplate; what
// the caller does with them is the caller's decision, and in this repository it
// is to fold them behind a disclosure (internal/spec/boilerplate.go).
package boiler

import (
	"crypto/sha256"
	"strconv"
	"strings"
)

// Scope is what a repeated block was found to belong to.
type Scope int

const (
	// NoScope: nothing about this message's tail repeats enough to be boilerplate.
	NoScope Scope = iota
	// Author: the block repeats across one person's own messages.
	Author
	// Domain: the block repeats across several senders at one mail domain, which
	// is the shape of an org-level notice rather than a personal sign-off.
	Domain
)

func (s Scope) String() string {
	switch s {
	case Author:
		return "author"
	case Domain:
		return "domain"
	}
	return "none"
}

// Rules are the thresholds a repeated tail has to clear. Zero fields take the
// value Default gives them, so a caller can override one without restating the
// rest.
type Rules struct {
	// Window is the longest tail considered, in visible lines. It bounds the work
	// and nothing else: a block longer than this folds only in part, which is a
	// smaller loss than the alternative — an unbounded window lets one
	// wholly-templated automated message tally a key per line of itself.
	Window int
	// MinLines is the shortest tail that may be folded.
	//
	// Two, because a disclosure control occupies a line of its own: folding one
	// line trades a line of text for a line of chrome and cannot pay for itself.
	// At two it can, and the two-line case is real — a great many people sign off
	// with a valediction and their first name and nothing else. That block is
	// boilerplate by the same definition as a ten-line one and is treated the
	// same way.
	MinLines int
	// AuthorRepeats is how many of one person's messages a tail must end before
	// it counts as appended rather than written.
	//
	// Three. Two is one repetition, and one repetition of a short tail is
	// ordinary coincidence — two brief replies that both end "Thanks" and a first
	// name say nothing about either. Three is that coincidence happening twice
	// more. On this corpus the difference between two and three is 50 entries out
	// of 1,300, so the stricter threshold is nearly free; what it costs is that
	// somebody who sent two messages keeps their signature in view, which is the
	// correct outcome of there being no evidence yet.
	AuthorRepeats int
	// DomainRepeats and DomainSenders are the same test for an organisation, plus
	// the requirement that the block appear over more than one sender.
	//
	// The second requirement is what makes the domain pass mean something. A
	// tail seen three times at a domain but only from one mailbox is that
	// person's signature, already found by the author pass; requiring two senders
	// is what separates "the org appends this" from "one employee does".
	DomainRepeats int
	DomainSenders int
}

// Default is the ruleset this repository runs. Every value is argued in Rules.
func Default() Rules {
	return Rules{Window: 25, MinLines: 2, AuthorRepeats: 3, DomainRepeats: 3, DomainSenders: 2}
}

func (r Rules) filled() Rules {
	d := Default()
	if r.Window <= 0 {
		r.Window = d.Window
	}
	if r.MinLines <= 0 {
		r.MinLines = d.MinLines
	}
	if r.AuthorRepeats <= 0 {
		r.AuthorRepeats = d.AuthorRepeats
	}
	if r.DomainRepeats <= 0 {
		r.DomainRepeats = d.DomainRepeats
	}
	if r.DomainSenders <= 0 {
		r.DomainSenders = d.DomainSenders
	}
	return r
}

// Message is one body offered as evidence, reduced to what a tail is compared
// on.
//
// Lines holds the visible lines only, blanks dropped and each one normalised for
// comparison: clients disagree about how much vertical space a signature gets
// and about how to spell a link inside it, and neither disagreement makes a
// different signature. See Lines, Visible and Match for the reduction. One entry
// per visible line, so a Fold's line count means the same thing to the caller
// that folds the sender's own text — a tail counted against one reduction and
// applied against another folds the wrong number of lines.
type Message struct {
	ID     int64
	Author int64  // corpus person id; 0 where the sender is unidentified
	Domain string // the sender's canonical mail domain; "" where there is none
	Lines  []string
}

// Fold is the trailing block of one message that is boilerplate, and the
// evidence for saying so.
type Fold struct {
	// Lines is how many of the message's trailing visible lines the block covers.
	Lines int
	// Count is how many messages the block ends, this one included.
	Count int
	// Senders is how many distinct people those messages came from. It is 1 for
	// an Author fold by construction and is the interesting number for a Domain
	// one.
	Senders int
	Scope   Scope
}

// Detect returns the boilerplate tail of each message that has one.
//
// The evidence is every message passed in, which the caller is expected to make
// wider than what it is about to render: a person's signature is a fact about
// them and not about the page they appear on, so restricting the evidence to a
// selection would fold a block on a page of forty messages and leave the same
// block in view on a page of six. internal/corpus.Boilerplate reads the whole
// corpus for exactly that reason.
//
// The longest qualifying tail wins, rather than a fixed window. A fixed window
// is wrong in both directions at once: shorter than the block, it leaves the top
// of a signature in view; longer, it takes the sender's closing sentence with
// it. Growing the tail until the repetition stops finds each block's own length,
// and finds two different lengths for one person who changed signature.
//
// The tail never takes the last visible line. A message can be entirely
// boilerplate — an automated alert whose whole body is a template — and folding
// all of it would leave a bubble whose only content is a closed disclosure,
// which reads as a page that failed to render rather than as a message. Keeping
// the first line and folding the rest states the same thing and still shows
// something.
func Detect(msgs []Message, r Rules) map[int64]Fold {
	r = r.filled()
	author := tallies(msgs, r.Window, authorGroup)
	domain := tallies(msgs, r.Window, domainGroup)

	out := map[int64]Fold{}
	for _, m := range msgs {
		var best Fold
		// Ascending, so a longer qualifying tail supersedes a shorter one: a
		// shorter tail of the same block is a piece of it, not a rival answer.
		for n := r.MinLines; n <= r.Window && n < len(m.Lines); n++ {
			if f, ok := qualifies(author, authorGroup, m, n, r.AuthorRepeats, 1, Author); ok {
				best = f
			}
			// Strictly longer to take the domain answer: where both scopes explain
			// the same tail, the author pass is the more specific claim and the two
			// agree about what to fold anyway.
			if f, ok := qualifies(domain, domainGroup, m, n, r.DomainRepeats, r.DomainSenders, Domain); ok && f.Lines > best.Lines {
				best = f
			}
		}
		if best.Scope != NoScope {
			out[m.ID] = best
		}
	}
	return out
}

func qualifies(t map[digest]*tally, group func(Message) (string, bool),
	m Message, n, repeats, senders int, sc Scope) (Fold, bool) {
	g, ok := group(m)
	if !ok {
		return Fold{}, false
	}
	k, ok := tailKey(g, m.Lines, n)
	if !ok {
		return Fold{}, false
	}
	e := t[k]
	if e == nil || e.n < repeats || len(e.who) < senders {
		return Fold{}, false
	}
	return Fold{Lines: n, Count: e.n, Senders: len(e.who), Scope: sc}, true
}

type tally struct {
	n   int
	who map[int64]bool
}

// tallies counts every candidate tail in one pass. Each message contributes one
// key per tail length, so the count for "the last n lines of this message" is a
// single lookup later rather than a comparison against every other message.
func tallies(msgs []Message, window int, group func(Message) (string, bool)) map[digest]*tally {
	out := map[digest]*tally{}
	for _, m := range msgs {
		g, ok := group(m)
		if !ok {
			continue
		}
		for n := 1; n <= window && n <= len(m.Lines); n++ {
			k, ok := tailKey(g, m.Lines, n)
			if !ok {
				continue
			}
			e := out[k]
			if e == nil {
				e = &tally{who: map[int64]bool{}}
				out[k] = e
			}
			e.n++
			e.who[m.Author] = true
		}
	}
	return out
}

func authorGroup(m Message) (string, bool) {
	if m.Author == 0 {
		return "", false
	}
	return "p" + strconv.FormatInt(m.Author, 10), true
}

func domainGroup(m Message) (string, bool) {
	if m.Domain == "" {
		return "", false
	}
	return "d" + m.Domain, true
}

// digest is a tail's identity. Keying the tallies on a hash rather than on the
// joined text is what keeps the pass over a whole corpus in memory: the text of
// every tail of every length is quadratic in the window, while a digest is 16
// bytes whatever the block. Sixteen bytes of SHA-256 over the ~10^5 distinct
// tails a corpus this size produces makes a collision — which would fold one
// person's prose as another's signature — a 10^-28 event.
type digest [16]byte

// tailKey is the identity of a message's last n lines, and false where those
// lines carry no words to identify.
//
// The lines are joined on a space and not on a newline, which deliberately makes
// the key blind to where the wrapping fell: one sender's notice arrives
// hard-wrapped at one width in the mailbox copy and at another in somebody's
// quote of it, and those are the same appended block seen twice, not two blocks.
// A line-sensitive key splits them, and splitting them is what drops a sender to
// their colleagues' shorter block. The cost is that a two-line tail can key the
// same as the one-line tail that spells the same words — which is the intent,
// since it is one block — so a tally counts messages and not line counts, and
// Fold.Lines stays each message's own count.
//
// An empty key is not evidence. A tail of nothing but placeholders normalises
// away, and every such tail in the corpus would otherwise agree with every
// other and fold on a match about no words at all.
func tailKey(group string, lines []string, n int) (digest, bool) {
	var b strings.Builder
	for _, l := range lines[len(lines)-n:] {
		if l == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(l)
	}
	if b.Len() == 0 {
		return digest{}, false
	}
	h := sha256.New()
	h.Write([]byte(group))
	h.Write([]byte{0})
	h.Write([]byte(b.String()))
	var d digest
	copy(d[:], h.Sum(nil))
	return d, true
}
