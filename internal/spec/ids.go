package spec

import (
	"fmt"
	"regexp"
	"strings"
)

// Ids are emitted explicitly, computed with the same formula the renderer would
// have used (src/lib/anchors.ts, entryId). The schema invites a collector to
// omit `id` and let the renderer derive one from date+time+sender, and that is
// the right default for a hand-written spec — but `parent` has to name the id
// the renderer settles on, so a generator has to know the formula either way.
// Given that, emitting it is the less fragile of the two:
//
//   - Derivation is not a pure function of one entry. entryId de-duplicates
//     collisions against every id seen so far, in array order, appending "-2",
//     "-3". Two messages from one sender in the same minute — a resend, a
//     calendar invite and its update — collide, and then a parent pointing at
//     the un-suffixed id silently attaches to the wrong one. To omit ids safely
//     we would have to replicate that stateful pass *and* guarantee our array
//     order matches what the renderer iterates. Emitting unique explicit ids
//     removes the ambiguity: the renderer's uniqueness loop becomes a no-op and
//     `parent` resolves exactly.
//   - It decouples the spec from a renderer implementation detail. If the
//     derivation formula is ever changed, a spec with explicit ids still has
//     intact reply edges; a spec that omitted them would have every `parent`
//     silently repoint or dangle.
//
// The formula is mirrored rather than invented so the anchors in a generated
// page match those in a hand-built one (fixtures/local.json), which is what
// makes existing deep links and a spec diff survive regeneration.

var reWord = regexp.MustCompile(`[A-Za-z]+`)
var reDigits = regexp.MustCompile(`\D`)

// entryID mirrors entryId in src/lib/anchors.ts for an entry we are about to
// emit. The caller owns uniqueness (see idAllocator).
func entryID(e Entry) string {
	day := "undated"
	if d := parseDisplayDate(e.Date); d != "" {
		day = d
	}
	if e.Kind == "note" {
		return fmt.Sprintf("m-%s-note", day)
	}
	t := reDigits.ReplaceAllString(e.Time, "")
	if len(t) > 4 {
		t = t[:4]
	}
	if t == "" {
		t = "0000"
	}
	who := ""
	for _, w := range reWord.FindAllString(e.Sender, -1) {
		who += strings.ToLower(w[:1])
	}
	if len(who) > 3 {
		who = who[:3]
	}
	if who == "" {
		who = "x"
	}
	return fmt.Sprintf("m-%s-%s-%s", day, t, who)
}

// parseDisplayDate reads back the "Mon 2 Jan 2006" rendering into the yyyymmdd
// that anchors.ts derives, or "" when the date is not in that shape.
func parseDisplayDate(date string) string {
	m := reAnchorDate.FindStringSubmatch(date)
	if m == nil {
		return ""
	}
	mon := monthIndex(m[2])
	if mon == 0 {
		return ""
	}
	return fmt.Sprintf("%s%02d%02d", m[3], mon, atoi(m[1]))
}

var reAnchorDate = regexp.MustCompile(`(\d{1,2})\s+([A-Za-z]{3})[a-z]*\s+(\d{4})`)

var months = []string{"jan", "feb", "mar", "apr", "may", "jun", "jul", "aug", "sep", "oct", "nov", "dec"}

func monthIndex(s string) int {
	s = strings.ToLower(s)
	for i, m := range months {
		if m == s {
			return i + 1
		}
	}
	return 0
}

func atoi(s string) int {
	n := 0
	for _, c := range s {
		n = n*10 + int(c-'0')
	}
	return n
}

// idAllocator hands out unique ids, suffixing collisions exactly as the
// renderer would ("-2", "-3", ...), so that a collision produces the same
// anchors whether or not the ids are emitted.
type idAllocator struct{ used map[string]bool }

func newIDAllocator() *idAllocator { return &idAllocator{used: map[string]bool{}} }

func (a *idAllocator) take(base string) string {
	id := base
	for n := 2; a.used[id]; n++ {
		id = fmt.Sprintf("%s-%d", base, n)
	}
	a.used[id] = true
	return id
}
