package unnest

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"
)

// Recovered is one block with its sentinel parsed: everything needed to store it
// as an entry, plus the key that collapses the copies.
type Recovered struct {
	Block Block
	// Sender and Address, whichever the sentinel gave.
	Sender, Address string
	// Sent is the stated send time, zero when the sentinel gave none.
	Sent time.Time
	TZ   string
	// Subject, To and Cc come only from header blocks; an attribution has none.
	Subject, To, Cc string
	// MessageID if the quoting client wrote one.
	MessageID string
	// Key identifies the underlying message across every place it was quoted.
	Key string
}

// Parse turns a block into a Recovered, reading whichever sentinel form it has.
func Parse(b Block) Recovered {
	r := Recovered{Block: b}
	switch b.Kind {
	case KindAttribution:
		a := ParseAttribution(b.Sentinel)
		r.Sender, r.Address, r.Sent, r.TZ = a.Sender, a.Address, a.Sent, a.TZ
	case KindHeaderBlock, KindForwardRule:
		h := ParseHeaderBlock(b.Sentinel)
		r.Sender, r.Address, r.Sent, r.TZ = h.Sender, h.Address, h.Sent, h.TZ
		r.Subject, r.To, r.Cc, r.MessageID = h.Subject, h.To, h.Cc, h.MessageID
	}
	r.Key = quoteKey(r)
	return r
}

// quoteKey identifies the message a block is a copy of.
//
// Three tiers, most trustworthy first:
//
//  1. A quoted Message-ID. Globally unique by construction. Almost never
//     present, but free to honour when it is.
//  2. Sender address plus stated send time. This is preferred over hashing the
//     text because each client REWRITES the text it quotes — rewrapping to its
//     own column width, eliding with "...", dropping [image: ] placeholders —
//     so the same original hashes differently at each level of nesting, which
//     is exactly the duplication the key exists to collapse. Address and
//     timestamp survive requoting unchanged.
//  3. A hash of whitespace-collapsed text, when there is no address or no time.
//     Whitespace is collapsed because rewrapping is the most common rewrite; the
//     remaining rewrites make this tier weaker, which is why it is last.
//
// Two distinct messages from one sender in the same minute would collide under
// tier 2. That is rarer than the same message quoted twice, and the cost is
// asymmetric: a false merge loses one message, while a missed merge multiplies
// every message in the corpus by its forward count.
func quoteKey(r Recovered) string {
	if r.MessageID != "" {
		return "mail:" + r.MessageID
	}
	if r.Address != "" && !r.Sent.IsZero() {
		return "quote:" + sum(strings.ToLower(r.Address), r.Sent.UTC().Format(time.RFC3339))
	}
	return "quote:t:" + sum(strings.Join(strings.Fields(r.Block.Text), " "))
}

func sum(parts ...string) string {
	h := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(h[:])[:32]
}

// Dedup collapses blocks that are copies of one message, preserving first
// appearance order. The survivor of a collision is the copy with the most text,
// since a deeper quote is the one that has been elided.
func Dedup(rs []Recovered) []Recovered {
	seen := map[string]int{}
	var out []Recovered
	for _, r := range rs {
		if i, ok := seen[r.Key]; ok {
			if len(r.Block.Text) > len(out[i].Block.Text) {
				// Keep the position, take the fuller text and any fields the
				// shorter copy lacked.
				r.Block.Depth = out[i].Block.Depth
				out[i] = merge(out[i], r)
			} else {
				out[i] = merge(out[i], r)
			}
			continue
		}
		seen[r.Key] = len(out)
		out = append(out, r)
	}
	return out
}

// merge fills gaps in a from b without overwriting what a already knows. One
// quoting client writes a Subject and another does not, so the union across
// sightings knows more than any single copy.
func merge(a, b Recovered) Recovered {
	if len(b.Block.Text) > len(a.Block.Text) {
		a.Block.Text = b.Block.Text
	}
	if a.Sender == "" {
		a.Sender = b.Sender
	}
	if a.Address == "" {
		a.Address = b.Address
	}
	if a.Sent.IsZero() {
		a.Sent = b.Sent
	}
	if a.TZ == "" {
		a.TZ = b.TZ
	}
	if a.Subject == "" {
		a.Subject = b.Subject
	}
	if a.To == "" {
		a.To = b.To
	}
	if a.Cc == "" {
		a.Cc = b.Cc
	}
	if a.MessageID == "" {
		a.MessageID = b.MessageID
	}
	return a
}
