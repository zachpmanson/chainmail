package main

import (
	"time"

	"github.com/zachpmanson/chainmail/internal/corpus"
)

// The wire types are separate from the corpus structs on purpose: they are the
// half of api/openapi.json a client reads, so a field cannot appear or change
// name because an internal struct grew one. Every omitempty here corresponds to
// a field the OpenAPI marks absent-able, and openapi_test.go checks both
// directions.

type searchResponse struct {
	Mode string `json:"mode"`
	// Exactly one of these is non-nil, decided by the `entries` parameter.
	// Pointers, so that "nothing matched" is an empty array rather than a
	// missing key — a client cannot tell an omitted key from an unsupported one.
	Chains  *[]chainHit `json:"chains,omitempty"`
	Entries *[]entryHit `json:"entries,omitempty"`
}

type chainHit struct {
	RootExtID string     `json:"rootExtId"`
	Subject   string     `json:"subject,omitempty"`
	Container string     `json:"container,omitempty"`
	Sources   []string   `json:"sources,omitempty"`
	Entries   int        `json:"entries"`
	Matched   int        `json:"matched"`
	First     string     `json:"first"`
	Last      string     `json:"last"`
	Score     float64    `json:"score"`
	Best      []entryHit `json:"best,omitempty"`
}

type entryHit struct {
	ExtID     string  `json:"extId"`
	Source    string  `json:"source"`
	TS        string  `json:"ts"`
	PersonID  int64   `json:"personId"`
	Person    string  `json:"person,omitempty"`
	Container string  `json:"container,omitempty"`
	Subject   string  `json:"subject,omitempty"`
	Permalink string  `json:"permalink,omitempty"`
	Snippet   string  `json:"snippet,omitempty"`
	Score     float64 `json:"score"`
	// The three ranks are always emitted, never omitempty: 0 means "that
	// ranking did not find this entry", which is the answer to why a result with
	// no visible keyword in it is here.
	ProseRank  int      `json:"proseRank"`
	IdentRank  int      `json:"identRank"`
	SemRank    int      `json:"semRank"`
	Similarity *float64 `json:"similarity,omitempty"`
}

type corpusEntry struct {
	ExtID           string        `json:"extId"`
	Source          string        `json:"source"`
	Quoted          bool          `json:"quoted"`
	TS              string        `json:"ts"`
	TZ              string        `json:"tz,omitempty"`
	TZOffsetMinutes *int          `json:"tzOffsetMinutes,omitempty"`
	Author          string        `json:"author,omitempty"`
	Subject         string        `json:"subject,omitempty"`
	Body            string        `json:"body,omitempty"`
	Container       string        `json:"container,omitempty"`
	Permalink       string        `json:"permalink,omitempty"`
	Parent          string        `json:"parent,omitempty"`
	ParentRef       string        `json:"parentRef,omitempty"`
	Sightings       []sighting    `json:"sightings,omitempty"`
	Participants    []participant `json:"participants,omitempty"`
}

type sighting struct {
	Kind   string `json:"kind"`
	SeenIn string `json:"seenIn,omitempty"`
	Detail string `json:"detail,omitempty"`
}

type participant struct {
	PersonID int64  `json:"personId"`
	Name     string `json:"name"`
	Role     string `json:"role"`
}

type chainResponse struct {
	RootExtID string        `json:"rootExtId"`
	Entries   []corpusEntry `json:"entries"`
}

type statsResponse struct {
	Entries    int64             `json:"entries"`
	BySource   map[string]int64  `json:"bySource"`
	People     int64             `json:"people"`
	ChainRoots int64             `json:"chainRoots"`
	Unresolved int64             `json:"unresolved"`
	Embeddings []embedModelStats `json:"embeddings"`
}

type embedModelStats struct {
	Model    string `json:"model"`
	Dim      int    `json:"dim"`
	Vectors  int    `json:"vectors"`
	Skipped  int    `json:"skipped"`
	Stale    int    `json:"stale"`
	Eligible int    `json:"eligible"`
}

type peopleResponse struct {
	People []personSummary `json:"people"`
}

type personSummary struct {
	PersonID    int64    `json:"personId"`
	DisplayName string   `json:"displayName"`
	Identities  []string `json:"identities,omitempty"`
	Sent        int64    `json:"sent"`
	Received    int64    `json:"received"`
}

// stamp renders a corpus timestamp. UTC, always: the corpus stores unix
// seconds, so a local rendering would say whatever zone the server happens to
// run in and mean nothing to the client. Per-entry wall clocks live in
// corpusEntry.tz and in the timeline spec, which is where they belong.
func stamp(t time.Time) string { return t.UTC().Format(time.RFC3339) }

func toEntryHit(h corpus.EntryHit) entryHit {
	e := entryHit{
		ExtID: h.ExtID, Source: h.Source, TS: stamp(h.TS),
		PersonID: h.PersonID, Person: h.Person, Container: h.Container,
		Subject: h.Subject, Permalink: h.Permalink, Snippet: h.Snippet,
		Score: h.Score, ProseRank: h.ProseRank, IdentRank: h.IdentRank, SemRank: h.SemRank,
	}
	// Similarity is meaningful only where the vector ranking found the entry;
	// emitting 0.0 otherwise would read as "orthogonal to the query" rather than
	// "not measured".
	if h.SemRank > 0 {
		sim := h.Similarity
		e.Similarity = &sim
	}
	return e
}

func toChainHit(c corpus.ChainHit) chainHit {
	out := chainHit{
		RootExtID: c.RootExtID, Subject: c.Subject, Container: c.Container,
		Sources: c.Sources, Entries: c.Entries, Matched: c.Matched,
		First: stamp(c.First), Last: stamp(c.Last), Score: c.Score,
	}
	for _, b := range c.Best {
		out.Best = append(out.Best, toEntryHit(b))
	}
	return out
}

func toCorpusEntry(s corpus.Shown) corpusEntry {
	e := corpusEntry{
		ExtID: s.ExtID, Source: s.Source, Quoted: s.Quoted, TS: stamp(s.TS),
		TZ: s.TZ, TZOffsetMinutes: s.TZOffset, Author: s.Author, Subject: s.Subject,
		Body: s.Body, Container: s.Container, Permalink: s.Permalink,
		Parent: s.Parent, ParentRef: s.ParentRef,
	}
	for _, g := range s.Sightings {
		e.Sightings = append(e.Sightings, sighting{Kind: g.Kind, SeenIn: g.SeenIn, Detail: g.Detail})
	}
	for _, p := range s.Participants {
		e.Participants = append(e.Participants,
			participant{PersonID: p.PersonID, Name: p.DisplayName, Role: p.Role})
	}
	return e
}
