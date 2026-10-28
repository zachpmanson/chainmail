package boiler

import (
	"math/rand"
	"reflect"
	"testing"
)

// ExperimentDetect isolates either suffix reuse or selected-message evaluation.
// A nil selection means all messages. Evidence counting always uses every msg.
func ExperimentDetect(msgs []Message, r Rules, selected map[int64]bool, reuse bool) map[int64]Fold {
	r = r.filled()
	if !reuse {
		author := tallies(msgs, r.Window, authorGroup)
		domain := tallies(msgs, r.Window, domainGroup)
		out := map[int64]Fold{}
		for _, m := range msgs {
			if selected != nil && !selected[m.ID] {
				continue
			}
			var best Fold
			for n := r.MinLines; n <= r.Window && n < len(m.Lines); n++ {
				if f, ok := qualifies(author, authorGroup, m, n, r.AuthorRepeats, 1, Author); ok {
					best = f
				}
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
	type key struct {
		group string
		tail  [16]byte
	}
	type prepared struct {
		message        *Message
		tails          []Tail
		author, domain string
	}
	authors, domains := map[key]*tally{}, map[key]*tally{}
	preparedMessages := make([]prepared, 0, len(msgs))
	add := func(counts map[key]*tally, group string, tail Tail, author int64) {
		if group == "" || !tail.Valid {
			return
		}
		k := key{group, tail.Digest}
		e := counts[k]
		if e == nil {
			e = &tally{who: map[int64]bool{}}
			counts[k] = e
		}
		e.n++
		e.who[author] = true
	}
	for i := range msgs {
		m := &msgs[i]
		a, _ := authorGroup(*m)
		d, _ := domainGroup(*m)
		p := prepared{message: m, tails: Tails(m.Lines, r.Window), author: a, domain: d}
		for _, tail := range p.tails {
			add(authors, a, tail, m.Author)
			add(domains, d, tail, m.Author)
		}
		preparedMessages = append(preparedMessages, p)
	}
	out := map[int64]Fold{}
	for _, p := range preparedMessages {
		m := p.message
		if selected != nil && !selected[m.ID] {
			continue
		}
		var best Fold
		for n := r.MinLines; n <= len(p.tails) && n < len(m.Lines); n++ {
			tail := p.tails[n-1]
			if !tail.Valid {
				continue
			}
			if p.author != "" {
				if e := authors[key{p.author, tail.Digest}]; e != nil && e.n >= r.AuthorRepeats && len(e.who) >= 1 {
					best = Fold{Lines: n, Count: e.n, Senders: len(e.who), Scope: Author}
				}
			}
			if p.domain != "" {
				if e := domains[key{p.domain, tail.Digest}]; e != nil && e.n >= r.DomainRepeats && len(e.who) >= r.DomainSenders && n > best.Lines {
					best = Fold{Lines: n, Count: e.n, Senders: len(e.who), Scope: Domain}
				}
			}
		}
		if best.Scope != NoScope {
			out[m.ID] = best
		}
	}
	return out
}

func TestExperimentMatchingParity(t *testing.T) {
	rng := rand.New(rand.NewSource(701))
	pool := []string{"", "One", "Two words", "One Two words", "Regards,", "Alex", "Confidential notice", "line\ninside", " ", "unicode γ"}
	for trial := 0; trial < 80; trial++ {
		var msgs []Message
		selected := map[int64]bool{}
		for i := 0; i < 80; i++ {
			m := Message{ID: int64(i + 1), Author: int64(rng.Intn(7)), Domain: []string{"", "a.test", "b.test"}[rng.Intn(3)]}
			for n := rng.Intn(35); n > 0; n-- {
				m.Lines = append(m.Lines, pool[rng.Intn(len(pool))])
			}
			msgs = append(msgs, m)
			if i%7 == 0 {
				selected[m.ID] = true
			}
		}
		r := Rules{Window: 1 + rng.Intn(30), MinLines: 1 + rng.Intn(3), AuthorRepeats: 1 + rng.Intn(5), DomainRepeats: 1 + rng.Intn(5), DomainSenders: 1 + rng.Intn(3)}
		want := Detect(msgs, r)
		if got := ExperimentDetect(msgs, r, nil, true); !reflect.DeepEqual(want, got) {
			t.Fatalf("trial %d reused suffixes differ: want=%v got=%v", trial, want, got)
		}
		filtered := map[int64]Fold{}
		for id, f := range want {
			if selected[id] {
				filtered[id] = f
			}
		}
		for _, reuse := range []bool{false, true} {
			if got := ExperimentDetect(msgs, r, selected, reuse); !reflect.DeepEqual(filtered, got) {
				t.Fatalf("trial %d selected/reuse=%v differs", trial, reuse)
			}
		}
	}
}

func TestExperimentSuffixEquivalence(t *testing.T) {
	sets := [][]string{{"One", "Two words"}, {"One Two words"}, {"", "One", "", "Two words", ""}, {"", ""}, {"line\ninside", "unicode γ"}, {" ", "One"}}
	oldToNew := map[digest][16]byte{}
	newToOld := map[[16]byte]digest{}
	for _, lines := range sets {
		tails := Tails(lines, 25)
		for n, tail := range tails {
			old, ok := tailKey("same-group", lines, n+1)
			if ok != tail.Valid {
				t.Fatal("empty-tail mismatch")
			}
			if !ok {
				continue
			}
			if previous, exists := oldToNew[old]; exists && previous != tail.Digest {
				t.Fatal("previously equal tails split")
			}
			if previous, exists := newToOld[tail.Digest]; exists && previous != old {
				t.Fatal("previously distinct tails merged")
			}
			oldToNew[old] = tail.Digest
			newToOld[tail.Digest] = old
		}
	}
}
