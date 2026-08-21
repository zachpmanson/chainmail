package eval

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// A judged set is queries plus the results a human decided answer them.
//
// # Why the real set is not in this repository
//
// The judgements are about real personal correspondence, and a judged set leaks
// it twice over. The query text is a paraphrase of what the mail is about, so a
// set of twenty queries is a topic index of somebody's inbox — who they deal
// with, what went wrong, what is being negotiated — even with no body text
// anywhere near it. The ext_ids are worse than they look: `mail:<Message-ID>`
// carries the sending domain, and `slack:<channel>:<ts>` names a workspace
// channel and the second a thing was said in it. An id is not content, but it is
// a durable pointer into a real account, and paired with a query that describes
// the topic it is a labelled map of it.
//
// So the real set stays untracked (fixtures/eval.local.json, already covered by
// the fixtures ignore rule) and the committed set is synthetic: invented people
// at example.com, invented topics, matched by a corpus the test builds. The
// committed set proves the harness computes what it claims; the local set is the
// one whose numbers mean anything, and it is reproducible by anyone holding the
// corpus it judges.

// Level is what a judgement names.
const (
	// LevelEntry judges individual entries by ext_id. Use it for ordering
	// questions: it is where a bad ranking within the right neighbourhood shows
	// up, because every near-miss is a separate result.
	LevelEntry = "entry"
	// LevelChain judges conversations by their root ext_id. Use it for "which
	// threads are about this", which is the question people actually ask —
	// and note that chain recall is the more forgiving measure, since any one
	// member entry surfacing is enough to bring the chain with it.
	LevelChain = "chain"
)

// Case is one query and the verdict on it.
type Case struct {
	// Query is what a person would type. It is deliberately not the words the
	// corpus uses: a query that shares its vocabulary with the answer tests the
	// keyword index, not the embeddings.
	Query string `json:"query"`

	// Relevant lists the ext_ids that answer it. Order is irrelevant; these are
	// unranked judgements.
	Relevant []string `json:"relevant,omitempty"`

	// ExpectEmpty marks a query the corpus has no answer to. It is not the
	// absence of judgements, it is a judgement — the assertion that any result
	// at all is a false positive — and it is the only thing keeping a change
	// that lowers the floor from looking like a pure win.
	ExpectEmpty bool `json:"expect_empty,omitempty"`

	// Why records what the case is testing, in a few words. Kept in the file
	// rather than in a reader's head, because the hard cases are hard for
	// specific reasons and a set of queries with no notes decays into a set of
	// queries nobody dares change.
	Why string `json:"why,omitempty"`
}

// Set is a judged collection.
type Set struct {
	// Level is LevelEntry or LevelChain; empty means LevelEntry.
	Level string `json:"level,omitempty"`
	Note  string `json:"note,omitempty"`
	Cases []Case `json:"cases"`
}

// LoadSet reads a judged set and rejects one that cannot mean anything.
func LoadSet(path string) (Set, error) {
	var set Set
	f, err := os.Open(path)
	if err != nil {
		return set, err
	}
	defer f.Close()
	dec := json.NewDecoder(f)
	// Unknown fields are an error rather than a shrug: a judged set is
	// hand-written, and a misspelled "relevent" that silently judged nothing
	// would report a confident zero.
	dec.DisallowUnknownFields()
	if err := dec.Decode(&set); err != nil {
		return set, fmt.Errorf("reading %s: %w", path, err)
	}
	if set.Level == "" {
		set.Level = LevelEntry
	}
	if set.Level != LevelEntry && set.Level != LevelChain {
		return set, fmt.Errorf("%s: level %q: want %q or %q", path, set.Level, LevelEntry, LevelChain)
	}
	if len(set.Cases) == 0 {
		return set, fmt.Errorf("%s: no cases", path)
	}
	for i, c := range set.Cases {
		switch {
		case strings.TrimSpace(c.Query) == "":
			return set, fmt.Errorf("%s: case %d has no query", path, i+1)
		case c.ExpectEmpty && len(c.Relevant) > 0:
			return set, fmt.Errorf("%s: case %q expects nothing and also lists %d relevant results",
				path, c.Query, len(c.Relevant))
		case !c.ExpectEmpty && len(c.Relevant) == 0:
			return set, fmt.Errorf("%s: case %q judges nothing: list relevant ids, or set expect_empty",
				path, c.Query)
		}
	}
	return set, nil
}

// relevantOf indexes a case's judgements for lookup.
func relevantOf(c Case) map[string]bool {
	m := make(map[string]bool, len(c.Relevant))
	for _, id := range c.Relevant {
		m[id] = true
	}
	return m
}
