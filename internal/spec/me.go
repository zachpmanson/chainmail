package spec

import (
	"fmt"
	"strings"

	"github.com/zachpmanson/chainmail/internal/corpus"
)

// meSet answers "did the reader write this?" for one entry, from the addresses
// the reader named.
//
// It is one type because two surfaces ask the question and must not answer it
// differently: a page build marks the entries it draws (Options.Me) and the
// reading pane marks a trail read over the API (the stored setting; see
// RenderTrail). The alternative rejected is the rule written once per surface —
// the two would be edited one at a time and drift, and the disagreement would
// surface as a bubble that is tinted in the pane and plain on the page, which a
// reader reads as a rendering bug rather than as a claim about a person.
type meSet struct {
	// addresses is what the reader typed, lowercased: an address is
	// case-insensitive, and the corpus stores every one of them that way.
	addresses map[string]bool
	// people is the same list as the humans the corpus resolved them to.
	people map[int64]bool
}

// markedAs resolves what a build marks with: the addresses the caller named, or
// — when it named none — the corpus's own stored answer.
//
// The second half is not a fallback for a caller that forgot. The stored setting
// is the only answer, and the addresses in a request are a copy of it taken at
// request time by an intermediary that can hold an old one or none at all: a
// browser braiding a page reads the setting, a CLI build reads the flags it was
// given, and a page built from either used to have whatever that caller happened
// to know that day. The pane has always marked from the store, so a page that
// marked from a copy answered the same question differently — and the difference
// is exactly what a reader reports as "my own messages are not coloured on this
// page": the entries were drawn plain because the request carried no list,
// while the same thread in the pane was tinted.
//
// Naming addresses therefore overrides, for the one caller that means it (a
// build on a host whose corpus has no setting: `corpus build --me`).
func markedAs(store *corpus.Store, named []string) ([]string, error) {
	if len(named) > 0 {
		return named, nil
	}
	return store.MeAddresses()
}

// newMeSet resolves the reader's addresses: the strings themselves, and the
// people the corpus has already decided they belong to.
func newMeSet(store *corpus.Store, addresses []string) (meSet, error) {
	me := meSet{addresses: map[string]bool{}, people: map[int64]bool{}}
	for _, a := range addresses {
		a = strings.TrimSpace(a)
		// An empty entry is not an address and must not become one. It would
		// match the empty From of every entry recovered from a quote, so a
		// stray comma in the list would mark the reader's whole quoted history
		// as their own outbound mail — the loudest possible reading of a typo.
		if a == "" {
			continue
		}
		me.addresses[strings.ToLower(a)] = true
	}
	people, err := corpus.PeopleForAddresses(store, addresses)
	if err != nil {
		return meSet{}, fmt.Errorf("resolving the reader's addresses: %w", err)
	}
	me.people = people
	return me, nil
}

// wrote answers "did the reader write this?" for one entry: its author's person
// id and the address it was sent from, both as the corpus holds them.
//
// Two ways to say yes. The From address is one of the addresses the reader
// named, or the corpus has already resolved this entry's author to a human the
// reader named.
//
// The second is not a convenience. A reader's mail arrives from addresses they
// never list — every `+tag` of their own mailbox, each work address, each alias
// — and the corpus merged all of them into one person when it ingested them,
// which is exactly the claim a reader makes when they say those addresses are
// theirs. Marking on the string alone meant that naming one address marked the
// mail sent from that one address and left the rest of their own mail unmarked,
// so the page contradicted the corpus about who the reader is.
//
// It is also the only way a recovered entry can ever be marked. An entry
// reconstructed from a quote has no From header of its own, so its address is
// empty and no list of addresses can match it — while the corpus knows
// perfectly well who wrote it, because the quoting client's attribution said
// so. That is the entry a reader is most likely to spot and least likely to
// believe was written by someone else.
//
// An address the corpus has never seen resolves to nobody, and an entry whose
// author is unknown has person 0, which is nobody's: both fall back to the
// address test, so a reader naming a stranger's address marks that stranger's
// mail as before.
func (m meSet) wrote(person int64, address string) bool {
	if m.addresses[strings.ToLower(strings.TrimSpace(address))] {
		return true
	}
	if person == 0 {
		return false
	}
	return m.people[person]
}
