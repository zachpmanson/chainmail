package main

import (
	"sort"
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
	ExtID           string `json:"extId"`
	Source          string `json:"source"`
	Quoted          bool   `json:"quoted"`
	TS              string `json:"ts"`
	TZ              string `json:"tz,omitempty"`
	TZOffsetMinutes *int   `json:"tzOffsetMinutes,omitempty"`
	Author          string `json:"author,omitempty"`
	Subject         string `json:"subject,omitempty"`
	Body            string `json:"body,omitempty"`
	// HTML is the body rendered for reading, by the same conversion a page build
	// uses (spec.RenderTrail); Body stays the plain text, which is what the
	// corpus holds and what anything matching text should read.
	HTML string `json:"html,omitempty"`
	// To is the recipient line a page build prints under the bubble, e.g.
	// "Bo Halvorsen, cc Cy Okafor". Absent where the entry stated no recipients,
	// which is every entry recovered from someone else's quote.
	To string `json:"to,omitempty"`
	// FromEmail is the address the entry came from, so a client can name the
	// sender fully on hover. Absent where the entry has no From header of its own.
	// The same expression a page build uses, so the two cannot name two addresses
	// for one message.
	FromEmail string `json:"fromEmail,omitempty"`
	// Org is the sender's organisation, resolved by the same function a page build
	// uses, so a bubble in the pane and a bubble on the page cannot disagree about
	// one sender. Absent where nothing established one, which is drawn as the
	// unknown colour rather than as a group of its own.
	Org string `json:"org,omitempty"`
	// QuotedBy is the person whose message this entry was recovered from, written
	// as a client writes a person on hover ("Ada Okoye <ada@loomworks.example>"),
	// with several joined by ", ". It is what the pane says where FromEmail is
	// absent: a recovered entry has no address of its own, and the quoter is where
	// it came from rather than a guess at who sent it.
	QuotedBy     string        `json:"fromQuotedBy,omitempty"`
	Container    string        `json:"container,omitempty"`
	Permalink    string        `json:"permalink,omitempty"`
	Parent       string        `json:"parent,omitempty"`
	ParentRef    string        `json:"parentRef,omitempty"`
	Sightings    []sighting    `json:"sightings,omitempty"`
	Participants []participant `json:"participants,omitempty"`
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

type authStatusResponse struct {
	SignedIn bool `json:"signed_in"`
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

// labelsResponse is the folder list a mailbox-style sidebar opens: every label
// on a mailbox message, with how many messages carry it. Messages rather than
// chains — a chain count for a label is a walk over the reply graph, and a mail
// client's own sidebar counts messages — so a client showing this next to a
// folder name should say nothing more specific than a number.
type labelsResponse struct {
	Labels []labelSummary `json:"labels"`
}

// versionResponse is the deploy stamp the header shows: the revision this
// process was built from, and when it started. The two are served together
// because neither answers the question alone — a hash with no date cannot say
// whether it is the build from a minute ago or from last week, and a date with
// no hash cannot say what is running.
type versionResponse struct {
	// rev is the revision this binary was built from. Empty where nobody told the
	// process which one it is — a `go run` under the devshell, a build outside
	// nix — and empty is served as absent rather than as a guess: a stamp naming
	// the wrong commit is worse than no stamp, because it is the thing a reader
	// checks to decide whether a fix is live.
	Rev string `json:"rev,omitempty"`
	// StartedAt is when this process came up (RFC 3339). For a deploy that is
	// when it went live, and for a restart it is when it restarted — which is why
	// a changed date with an unchanged rev reads as a restart rather than as a
	// deploy. The reader is shown the day this falls on in their own zone: a stamp
	// is read at a desk, not on a server.
	StartedAt string `json:"startedAt"`
}

// settingsResponse is the reader's own choices, which are not facts about the
// mail. Absent means the choice has not been made — a folder the home page opens
// in by default, or no such folder — so a client cannot mistake "unset" for a
// default of nothing.
type settingsResponse struct {
	DefaultFolder *string `json:"defaultFolder,omitempty"`
}

// settingsRequest is the same shape written back. A pointer so a missing field
// clears the setting: there is one writer, and defaulting an absent field to
// "leave it alone" leaves no way to unset it.
type settingsRequest struct {
	DefaultFolder *string `json:"defaultFolder,omitempty"`
}

type labelSummary struct {
	Name     string `json:"name"`
	Messages int    `json:"messages"`
}

func toLabelsResponse(ls []corpus.LabelCount) labelsResponse {
	out := labelsResponse{Labels: make([]labelSummary, 0, len(ls))}
	for _, l := range ls {
		out.Labels = append(out.Labels, labelSummary{Name: l.Name, Messages: l.Messages})
	}
	return out
}

// opsPlanResponse is everything the ops screen shows to review people merges,
// in one read-only shot: the dedupe plan the CLI's dry run prints (merges and
// refusals), the pairs MergeCandidates offers a human glance at, the twins
// pass's declined entries aggregated by reason, and the person_merges trail of
// merges so far. People is the current person count, so a screen can state how
// much the merges below would shrink the corpus.
type opsPlanResponse struct {
	People        int64            `json:"people"`
	Merges        []opsMerge       `json:"merges"`
	Refusals      []opsRefusal     `json:"refusals"`
	Candidates    []opsCandidate   `json:"candidates"`
	TwinsDeclined []twinsDecline   `json:"twinsDeclined"`
	Trail         []opsMergeRecord `json:"trail"`
}

// opsMerge is one pair the dedupe pass would fold. Applicable is the boundary
// the review UI is drawn to: the same-name/same-thread tiers may be posted to
// POST /v1/ops/merge, everything else is shown and read-only. The identities
// are what a reviewer judges — a name-only placeholder carries none, which is
// the whole finding.
type opsMerge struct {
	Rule           string   `json:"rule"`
	KeepID         int64    `json:"keepId"`
	KeepName       string   `json:"keepName"`
	KeepIdentities []string `json:"keepIdentities,omitempty"`
	DropID         int64    `json:"dropId"`
	DropName       string   `json:"dropName"`
	DropIdentities []string `json:"dropIdentities,omitempty"`
	Evidence       string   `json:"evidence,omitempty"`
	Applicable     bool     `json:"applicable"`
}

// opsRefusal is a group the dedupe pass would not decide, exactly as the CLI's
// dry run prints it — read-only in every UI, on purpose.
type opsRefusal struct {
	Rule    string  `json:"rule"`
	Subject string  `json:"subject"`
	Reason  string  `json:"reason"`
	People  []int64 `json:"people"`
}

// opsCandidate is a pair worth a human glance that nothing proved one way (the
// CLI's `corpus candidates`), with the command that would settle it.
type opsCandidate struct {
	AID        int64    `json:"aId"`
	AName      string   `json:"aName"`
	AAddresses []string `json:"aAddresses,omitempty"`
	BID        int64    `json:"bId"`
	BName      string   `json:"bName"`
	BAddresses []string `json:"bAddresses,omitempty"`
	Reason     string   `json:"reason"`
	Suggest    string   `json:"suggest,omitempty"`
}

// twinsDecline is one reason the twins pass left entries alone, with how many
// entries it did. Aggregated rather than listed: on this corpus the pass
// declines hundreds of entries a run, and the per-entry list is what the CLI's
// `corpus twins -declined` flag is for.
type twinsDecline struct {
	Reason string `json:"reason"`
	Count  int    `json:"count"`
}

// orgRuleRequest is one reader's answer about one domain, in the three states a
// grouping can be in: named (and so grouped with every other domain of that
// name), empty — an organisation-less domain, which is a decision and not an
// absence — or absent, which drops the rule and puts the domain back to being
// read from its own name. A pointer for the same reason settingsRequest has one:
// "I did not mention it" and "I want no rule" are different answers, and the
// difference is the whole of what the reader is saying.
type orgRuleRequest struct {
	Domain string  `json:"domain"`
	Org    *string `json:"org,omitempty"`
}

// orgRuleResponse is one domain as the Ops screen shows it: how much mail is
// drawn from it and what that mail is drawn as.
type orgRuleResponse struct {
	Domain string `json:"domain"`
	// Messages and People are the mail behind the rule — the entries whose own
	// From header names this domain, and the distinct senders they came from. Mail
	// recovered from a quote has no domain of its own and so is counted under its
	// sender, not here; a change to this domain's grouping still moves it, which is
	// what the preview counts.
	Messages int `json:"messages"`
	People   int `json:"people"`
	// Org is the label this domain's mail is drawn under — the stored rule, or the
	// name the domain itself gives. Absent where there is no label at all, which is
	// both "the reader said it is not an organisation" and "nobody has looked at a
	// domain whose own name gives nothing"; Stored tells the two apart, because a
	// screen has to be able to say which of them it is showing.
	Org    string `json:"org,omitempty"`
	Stored bool   `json:"stored"`
	// Guess is what the domain would be called with no rule at all, so that an
	// undecided domain can be shown as itself rather than as a decision nobody
	// made. Absent when the guess is nothing (freemail, a hostname, an address
	// nothing can be read out of), which is the honest answer rather than "".
	Guess string `json:"guess,omitempty"`
}

type orgsResponse struct {
	Domains []orgRuleResponse `json:"domains"`
}

// orgShiftResponse is what one proposed rule would redraw. The counts come from
// the resolver that will apply the rule, not from a second estimate of it — see
// spec.OrgShiftFor — so a reader agreeing to a number here is agreeing to the
// change the corpus will actually make.
type orgShiftResponse struct {
	Domain string `json:"domain"`
	// Messages is how many entries would be drawn under a different organisation.
	Messages int `json:"messages"`
	// People is how many distinct senders those entries belong to.
	People int `json:"people"`
	// Ambiguous is the entries left out of both counts: their sender's own mail
	// names two organisations and they have no address of their own, so which one
	// colours them depends on the order a trail is read in. Reported rather than
	// silently dropped, because the sentence above reads as a claim about all of
	// them otherwise.
	Ambiguous int `json:"ambiguous"`
}

// opsMergeRecord is one row of the person_merges trail: a merge that happened,
// who it folded into whom, and on what evidence. This is the audit record, not
// an undo handle — a merge is not reversible.
type opsMergeRecord struct {
	KeepID   int64  `json:"keepId"`
	KeepName string `json:"keepName,omitempty"`
	DropID   int64  `json:"dropId"`
	DropName string `json:"dropName,omitempty"`
	Reason   string `json:"reason,omitempty"`
	MergedAt string `json:"mergedAt"`
}

// opsMergeRequest names a pair to merge, as the shown plan names it: the
// keeper first. The pair must be in the current dedupe plan AND in an
// applicable tier — the server re-derives the plan at apply time, so a stale
// screen cannot merge a pair the plan no longer makes.
type opsMergeRequest struct {
	KeepID int64 `json:"keepId"`
	DropID int64 `json:"dropId"`
}

// opsMergeResponse is the person_merges row the merge wrote, so a client can
// show the same record the trail will list. The plan must be refetched after;
// this response deliberately carries no updated plan.
type opsMergeResponse struct {
	Merge opsMergeRecord `json:"merge"`
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
// id, the same handle POST /v1/spec takes, and queries records the search a
// chain came from when the spec does not record it yet — the page's own
// add-email search, which nobody else can name. name, when set, saves the
// refreshed page back under /view/<name> so a reload lands on the new run.
type refreshRequest struct {
	Spec       spec.Spec    `json:"spec"`
	Title      string       `json:"title,omitempty"`
	Person     string       `json:"person,omitempty"`
	Since      string       `json:"since,omitempty"`
	Limit      int          `json:"limit,omitempty"`
	Me         []string     `json:"me,omitempty"`
	IncludeNew bool         `json:"includeNew,omitempty"`
	Accept     []string     `json:"accept,omitempty"`
	Queries    []spec.Query `json:"queries,omitempty"`
	Name       string       `json:"name,omitempty"`
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
// kept but its query no longer returns it. queriesRecorded is not a chain but a
// change to the page's record, and says why a refresh that only recorded a
// search is not a nothing-new one.
type refreshReport struct {
	EntriesBefore   int               `json:"entriesBefore"`
	EntriesAfter    int               `json:"entriesAfter"`
	TwinsCollapsed  int               `json:"twinsCollapsed,omitempty"`
	QueriesRecorded []string          `json:"queriesRecorded,omitempty"`
	ChainsAdded     []chainGrowth     `json:"chainsAdded,omitempty"`
	ChainsGrown     []chainGrowth     `json:"chainsGrown,omitempty"`
	ChainsProposed  []candidateReport `json:"chainsProposed,omitempty"`
	ChainsUnranked  []string          `json:"chainsUnranked,omitempty"`
	NothingNew      bool              `json:"nothingNew"`
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

// topTwinsDeclines sorts the reason counts the way the CLI's count line would
// be read: most frequent first.
func topTwinsDeclines(byReason map[string]int) []twinsDecline {
	out := make([]twinsDecline, 0, len(byReason))
	for reason := range byReason {
		out = append(out, twinsDecline{Reason: reason, Count: byReason[reason]})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Count > out[j].Count ||
			(out[i].Count == out[j].Count && out[i].Reason < out[j].Reason)
	})
	return out
}

func toPeopleResponse(ps []corpus.PersonSummary) peopleResponse {
	out := peopleResponse{People: make([]personSummary, 0, len(ps))}
	for _, p := range ps {
		out.People = append(out.People, personSummary{
			PersonID: p.PersonID, DisplayName: p.DisplayName,
			Identities: p.Identities, Sent: p.Sent, Received: p.Received,
		})
	}
	return out
}

func toRefreshReport(r refresh.Report) refreshReport {
	out := refreshReport{
		EntriesBefore:   r.EntriesBefore,
		EntriesAfter:    r.EntriesAfter,
		TwinsCollapsed:  r.TwinsCollapsed,
		QueriesRecorded: r.QueriesRecorded,
		NothingNew:      r.NothingNew(),
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

func toCorpusEntry(s corpus.Shown, r spec.Rendered) corpusEntry {
	e := corpusEntry{
		ExtID: s.ExtID, Source: s.Source, Quoted: s.Quoted, TS: stamp(s.TS),
		TZ: s.TZ, TZOffsetMinutes: s.TZOffset, Author: s.Author, Subject: s.Subject,
		Body: s.Body, HTML: r.HTML, To: r.To, FromEmail: r.FromEmail,
		Org: r.Org, QuotedBy: r.QuotedBy,
		Container: s.Container,
		Permalink: s.Permalink,
		Parent:    s.Parent, ParentRef: s.ParentRef,
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
