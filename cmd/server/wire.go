package main

import (
	"time"

	"github.com/zachpmanson/chainmail/internal/corpus"
	"github.com/zachpmanson/chainmail/internal/refresh"
	"github.com/zachpmanson/chainmail/internal/spec"
	"github.com/zachpmanson/chainmail/internal/status"
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
	People    int        `json:"people"`
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

// specListResponse is the answer to GET /v1/specs: every page POST /v1/spec
// saved, newest first.
type specListResponse struct {
	Specs []savedSpecSummary `json:"specs"`
}

// savedSpecSummary is one row of the index: enough to list and reopen a page
// without fetching it whole. The name IS the /view/<name> URL, the savedAt is
// the disambiguation the page's own title cannot always give (distinct saved
// pages routinely share a title), and title is what the links spell out.
type savedSpecSummary struct {
	Name    string `json:"name"`
	Title   string `json:"title"`
	SavedAt string `json:"savedAt"`
}

// stamp renders a UTC timestamp. UTC on purpose: the corpus stores unix
// seconds, so a local rendering would say whatever zone the server happens to
// run in and mean nothing to the client. Per-message wall clocks live in
// corpusEntry and the timeline spec, which is where they belong; a page's
// saved-at is a property of the file, best pinned where the file's mtime is.
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
		People: c.People, First: stamp(c.First), Last: stamp(c.Last), Score: c.Score,
	}
	for _, b := range c.Best {
		out.Best = append(out.Best, toEntryHit(b))
	}
	return out
}

// refreshRequest is the previous run being brought up to date, plus the
// overrides the CLI would take. The spec itself is authoritative for
// membership; title, person, since, limit and me only narrow or rename how
// that membership is reproduced. accept accepts proposed chains by root ext
// id, the same handle POST /v1/spec takes. name, when set, saves the
// refreshed page back under /view/<name> so a reload lands on the new run.
type refreshRequest struct {
	Spec       spec.Spec `json:"spec"`
	Title      string    `json:"title,omitempty"`
	Person     string    `json:"person,omitempty"`
	Since      string    `json:"since,omitempty"`
	Limit      int       `json:"limit,omitempty"`
	Me         []string  `json:"me,omitempty"`
	IncludeNew bool      `json:"includeNew,omitempty"`
	Accept     []string  `json:"accept,omitempty"`
	Name       string    `json:"name,omitempty"`
}

// refreshResponse is the regenerated spec alongside what the refresh decided.
// The spec is what the renderer consumes; the report is what a client shows to
// explain it — a chain grew, a chain appeared, a chain was proposed.
type refreshResponse struct {
	Spec   spec.Spec     `json:"spec"`
	Report refreshReport `json:"report"`
}

// refreshReport is the delta between the previous run and this one.
//
// The lists are omitted when empty, so a refresh with nothing new reads as {
// entriesBefore, entriesAfter, nothingNew }. A chain cannot be in two lists:
// added means it was not on the page before, grown means it was and gained
// entries, proposed means it was found but not accepted, unranked means it is
// kept but its query no longer returns it.
type refreshReport struct {
	EntriesBefore  int               `json:"entriesBefore"`
	EntriesAfter   int               `json:"entriesAfter"`
	TwinsCollapsed int               `json:"twinsCollapsed,omitempty"`
	ChainsAdded    []chainGrowth     `json:"chainsAdded,omitempty"`
	ChainsGrown    []chainGrowth     `json:"chainsGrown,omitempty"`
	ChainsProposed []candidateReport `json:"chainsProposed,omitempty"`
	ChainsUnranked []string          `json:"chainsUnranked,omitempty"`
	NothingNew     bool              `json:"nothingNew"`
}

// growthReport is one chain whose membership changed. before is absent when
// the chain is new to the page through an accepted candidate or a container
// newly attached to the reply graph; after is what the page now holds.
type chainGrowth struct {
	ID      string `json:"id"`
	Subject string `json:"subject,omitempty"`
	Before  int    `json:"before,omitempty"`
	After   int    `json:"after"`
}

// candidateReport is a chain the queries found that the page does not yet
// include, with what is needed to judge it before accepting: the id that
// accepts it, its size, and which query found it. Similarity and the two flags
// tell how the discovery explained it — a semantic-only proposal has cleared
// the chain floor, so the number a reader would want to see is carried here.
type candidateReport struct {
	RootExtID string `json:"rootExtId"`
	Subject   string `json:"subject,omitempty"`
	Container string `json:"container,omitempty"`
	Entries   int    `json:"entries"`
	Matched   int    `json:"matched"`
	Span      string `json:"span,omitempty"`
	Query     string `json:"query"`
	// Similarity is the chain's best cosine to the query; Semantic says the
	// vectors found it, Lexical that words did. All absent for a lexical-only
	// refresh, which is the pre-hybrid answer.
	Similarity float64 `json:"similarity,omitempty"`
	Semantic   bool    `json:"semantic,omitempty"`
	Lexical    bool    `json:"lexical,omitempty"`
}

func toRefreshReport(r refresh.Report) refreshReport {
	out := refreshReport{
		EntriesBefore:  r.EntriesBefore,
		EntriesAfter:   r.EntriesAfter,
		TwinsCollapsed: r.TwinsCollapsed,
		NothingNew:     r.NothingNew(),
	}
	for _, g := range r.ChainsAdded {
		out.ChainsAdded = append(out.ChainsAdded, chainGrowth{
			ID: g.ID, Subject: g.Subject, Before: g.Before, After: g.After})
	}
	for _, g := range r.ChainsGrown {
		out.ChainsGrown = append(out.ChainsGrown, chainGrowth{
			ID: g.ID, Subject: g.Subject, Before: g.Before, After: g.After})
	}
	for _, c := range r.ChainsProposed {
		out.ChainsProposed = append(out.ChainsProposed, candidateReport{
			RootExtID: c.RootExtID, Subject: c.Subject, Container: c.Container,
			Entries: c.Entries, Matched: c.Matched, Span: c.Span, Query: c.Query,
			Similarity: c.Similarity, Semantic: c.Semantic, Lexical: c.Lexical})
	}
	for _, id := range r.ChainsUnranked {
		out.ChainsUnranked = append(out.ChainsUnranked, id)
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

// statusResponse is the connection snapshot the server serves from the file
// the operator's probe wrote. CheckedAt is the probe's UTC stamp, omitted when
// no probe has ever run; the services list is always present, each backend
// answered "unchecked" rather than absent, so the screen degrades instead of
// 404ing. NextSlurpAt is the next scheduled pulse, computed live (never
// stored), so it stays right however long ago the last probe or slurp ran.
type statusResponse struct {
	CheckedAt   string          `json:"checkedAt,omitempty"`
	NextSlurpAt string          `json:"nextSlurpAt,omitempty"`
	Services    []serviceStatus `json:"services"`
}

type serviceStatus struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

func toStatusResponse(s status.Snapshot) statusResponse {
	out := statusResponse{
		NextSlurpAt: nextSlurpAt(),
		Services:    make([]serviceStatus, 0, len(s.Services)),
	}
	if s.CheckedAt != "" {
		out.CheckedAt = s.CheckedAt
	}
	for _, svc := range s.Services {
		out.Services = append(out.Services, serviceStatus{
			ID: svc.ID, Label: svc.Label, Status: svc.Status, Detail: svc.Detail,
		})
	}
	return out
}

// nextSlurpAt is when the scheduled slurp pulse next fires, as a UTC RFC3339
// stamp. The deployed timer runs OnCalendar="*:0" (naboo's chainmail-slurp
// timer: on the hour, every hour), so the next one is the next top of the hour
// in local time. Computed rather than stored so it never goes stale between
// runs; if the timer's cadence ever changes, this must follow it.
func nextSlurpAt() string {
	now := time.Now()
	next := time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), 0, 0, 0, now.Location()).Add(time.Hour)
	return next.UTC().Format(time.RFC3339)
}
