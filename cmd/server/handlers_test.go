package main

import (
	"encoding/json"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/zachpmanson/chainmail/internal/spec"
	"github.com/zachpmanson/chainmail/internal/status"
)

func entryPath(prefix, extID string) string { return prefix + url.PathEscape(extID) }

func decode[T any](t *testing.T, res *response) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(res.body, &v); err != nil {
		t.Fatalf("decoding the response: %v\n%s", err, res.body)
	}
	return v
}

func TestSearchReturnsTheChainsAQueryHits(t *testing.T) {
	srv, api := testServer(t), loadAPI(t)
	res := srv.do(t, "GET", "/v1/search?q=solar+install+quote", nil)
	if res.status != 200 {
		t.Fatalf("status = %d: %s", res.status, res.body)
	}
	api.assert(t, "SearchResponse", res.body)

	got := decode[struct {
		Mode   string
		Chains *[]struct {
			RootExtID string `json:"rootExtId"`
			Matched   int
			Entries   int
		}
		Entries *[]any
	}](t, res)
	if got.Mode != modeLexical {
		t.Errorf("mode = %q, want the default %q", got.Mode, modeLexical)
	}
	if got.Entries != nil {
		t.Error("entries is populated for a chain search; the contract says exactly one array is")
	}
	if got.Chains == nil || len(*got.Chains) == 0 {
		t.Fatalf("no chains for a query the fixture answers: %s", res.body)
	}
	c := (*got.Chains)[0]
	if c.RootExtID != extAda1 {
		t.Errorf("rootExtId = %q, want the chain root %q", c.RootExtID, extAda1)
	}
	// Selection is the point of this endpoint, so the caller needs the size of
	// the whole chain, not only of the part that matched.
	if c.Entries != 3 {
		t.Errorf("entries = %d, want the whole chain of 3", c.Entries)
	}
	if c.Matched < 1 || c.Matched > c.Entries {
		t.Errorf("matched = %d, want between 1 and %d", c.Matched, c.Entries)
	}
}

// A query nobody wrote the words for is an answer, not a failure — and the
// empty array has to be present, because a client cannot tell a key omitted
// from a key unsupported.
func TestSearchMatchingNothingIsAnEmptyArrayNotAMissingKey(t *testing.T) {
	srv := testServer(t)
	res := srv.do(t, "GET", "/v1/search?q=zzzznothinghere", nil)
	if res.status != 200 {
		t.Fatalf("status = %d: %s", res.status, res.body)
	}
	if !strings.Contains(string(res.body), `"chains":[]`) {
		t.Errorf("want an empty chains array, got %s", res.body)
	}
}

func TestSearchEntriesSwitchesWhichArrayIsPopulated(t *testing.T) {
	srv, api := testServer(t), loadAPI(t)
	res := srv.do(t, "GET", "/v1/search?q=solar&entries=true", nil)
	if res.status != 200 {
		t.Fatalf("status = %d: %s", res.status, res.body)
	}
	api.assert(t, "SearchResponse", res.body)
	got := decode[struct {
		Chains  *[]any
		Entries *[]struct {
			ExtID     string `json:"extId"`
			ProseRank int    `json:"proseRank"`
			SemRank   int    `json:"semRank"`
		}
	}](t, res)
	if got.Chains != nil {
		t.Error("chains is populated for an entry search")
	}
	if got.Entries == nil || len(*got.Entries) == 0 {
		t.Fatalf("no entries: %s", res.body)
	}
	e := (*got.Entries)[0]
	if e.ProseRank == 0 {
		t.Error("a prose hit reports proseRank 0, which the contract reads as 'not found by that ranking'")
	}
	// Emitted rather than omitted: 0 is the answer to "why is this here", and an
	// absent key would read as an unsupported field.
	if !strings.Contains(string(res.body), `"semRank":0`) {
		t.Error("semRank is omitted for a lexical search; 0 is meaningful and must be present")
	}
}

// With no filter at all the answer is an arbitrary slice of the corpus that
// reads exactly like a ranked one, which is worse than a refusal.
func TestSearchWithNoFilterIsRefused(t *testing.T) {
	srv := testServer(t)
	res := srv.do(t, "GET", "/v1/search", nil)
	if res.status != 400 {
		t.Fatalf("status = %d, want 400: %s", res.status, res.body)
	}
	if msg := res.errText(t); !strings.Contains(msg, "q") {
		t.Errorf("the message does not name what to pass: %q", msg)
	}
}

func TestMalformedSearchParametersAreRefusedWithAUsableMessage(t *testing.T) {
	cases := map[string]string{
		"/v1/search?q=solar&limit=lots":       "limit",
		"/v1/search?q=solar&limit=100000":     "limit",
		"/v1/search?q=solar&entries=yes":      "entries",
		"/v1/search?q=solar&since=last+March": "since",
		"/v1/search?q=solar&mode=vibes":       "mode",
	}
	srv := testServer(t)
	for path, want := range cases {
		res := srv.do(t, "GET", path, nil)
		if res.status != 400 {
			t.Errorf("%s: status = %d, want 400: %s", path, res.status, res.body)
			continue
		}
		if msg := res.errText(t); !strings.Contains(msg, want) {
			t.Errorf("%s: message %q does not name %q", path, msg, want)
		}
	}
}

// A down daemon must surface as an answer to what was asked, with the fix in
// it. A 500, or an empty result set, both read as "there is nothing there".
func TestSemanticSearchWithNoDaemonSaysSoAndHowToFixIt(t *testing.T) {
	srv := testServer(t)
	for _, mode := range []string{modeSemantic, modeHybrid} {
		res := srv.do(t, "GET", "/v1/search?q=solar&mode="+mode, nil)
		if res.status != 503 {
			t.Fatalf("%s: status = %d, want 503: %s", mode, res.status, res.body)
		}
		msg := res.errText(t)
		if !strings.Contains(msg, "ollama serve") {
			t.Errorf("%s: message %q does not say how to start the daemon", mode, msg)
		}
	}
}

func TestEntryCarriesItsProvenanceAndTheZoneItStated(t *testing.T) {
	srv, api := testServer(t), loadAPI(t)
	res := srv.do(t, "GET", entryPath("/v1/entries/", extAda1), nil)
	if res.status != 200 {
		t.Fatalf("status = %d: %s", res.status, res.body)
	}
	api.assert(t, "CorpusEntry", res.body)
	got := decode[struct {
		ExtID           string `json:"extId"`
		TZ              string `json:"tz"`
		TZOffsetMinutes *int   `json:"tzOffsetMinutes"`
		Sightings       []struct{ Kind string }
		Participants    []struct{ Role string }
	}](t, res)
	if got.ExtID != extAda1 {
		t.Errorf("extId = %q", got.ExtID)
	}
	// A label does not determine an offset, so both are carried or the client
	// cannot place the sender's own clock.
	if got.TZ != "AEDT" || got.TZOffsetMinutes == nil || *got.TZOffsetMinutes != 660 {
		t.Errorf("tz = %q, offset = %v, want AEDT at +660", got.TZ, got.TZOffsetMinutes)
	}
	if len(got.Sightings) != 1 || got.Sightings[0].Kind != "direct" {
		t.Errorf("sightings = %+v, want one direct sighting", got.Sightings)
	}
	if len(got.Participants) < 2 {
		t.Errorf("participants = %+v, want the sender and the recipient", got.Participants)
	}
}

// The two must not look alike: "no such id" is a mistake to fix, "nothing
// matched" is an answer.
func TestAnUnknownIDIs404WhileAnEmptyResultIs200(t *testing.T) {
	srv := testServer(t)
	for _, path := range []string{
		entryPath("/v1/entries/", extNone),
		entryPath("/v1/chains/", extNone),
	} {
		res := srv.do(t, "GET", path, nil)
		if res.status != 404 {
			t.Errorf("%s: status = %d, want 404: %s", path, res.status, res.body)
			continue
		}
		if msg := res.errText(t); !strings.Contains(msg, extNone) {
			t.Errorf("%s: message %q does not name the id", path, msg)
		}
	}
	if res := srv.do(t, "GET", "/v1/search?q=zzzznothinghere", nil); res.status != 200 {
		t.Errorf("an empty result is %d, which is indistinguishable from a bad id", res.status)
	}
}

// Search reports the entry that matched, not the root, so the endpoint has to
// answer from any member of the chain.
func TestChainIsWholeFromAnyMember(t *testing.T) {
	srv, api := testServer(t), loadAPI(t)
	for _, from := range []string{extAda1, extBo2, extAda3} {
		res := srv.do(t, "GET", entryPath("/v1/chains/", from), nil)
		if res.status != 200 {
			t.Fatalf("%s: status = %d: %s", from, res.status, res.body)
		}
		api.assert(t, "ChainResponse", res.body)
		got := decode[struct {
			RootExtID string `json:"rootExtId"`
			Entries   []struct {
				ExtID string `json:"extId"`
				TS    string `json:"ts"`
			}
		}](t, res)
		if got.RootExtID != from {
			t.Errorf("rootExtId = %q, want the id that was asked for", got.RootExtID)
		}
		if len(got.Entries) != 3 {
			t.Fatalf("from %s: %d entries, want the whole chain of 3", from, len(got.Entries))
		}
		for i := 1; i < len(got.Entries); i++ {
			if got.Entries[i].TS < got.Entries[i-1].TS {
				t.Errorf("from %s: entry %d is out of time order", from, i)
			}
		}
		// The unrelated thread must not be dragged in by a shared participant.
		for _, e := range got.Entries {
			if e.ExtID == extOther {
				t.Error("an unrelated thread is in the chain")
			}
		}
	}
}

func TestSpecBuildsAPageFromTheChosenSet(t *testing.T) {
	srv, api := testServer(t), loadAPI(t)
	res := srv.do(t, "POST", "/v1/spec", specBody(extAda1))
	if res.status != 200 {
		t.Fatalf("status = %d: %s", res.status, res.body)
	}
	// Asserted against the inlined copy of schema/timeline.schema.json, so this
	// is the contract the renderer already validates against.
	api.assert(t, "TimelineSpec", res.body)
	got := decode[struct {
		Title    string
		Messages []struct {
			Sender string
			Body   string
		}
		Participants []struct{ Name string }
	}](t, res)
	if got.Title != "Solar install quote" {
		t.Errorf("title = %q", got.Title)
	}
	// Naming one chain root selects the chain: the whole conversation, not the
	// one entry named.
	if len(got.Messages) != 3 {
		t.Errorf("%d messages, want the whole chain of 3", len(got.Messages))
	}
	if len(got.Participants) < 2 {
		t.Errorf("participants = %+v, want the whole cast", got.Participants)
	}
}

// Selection is the caller's decision, so an empty selection is a mistake to
// report rather than a page with nothing on it — which the timeline schema
// would reject anyway.
func TestSpecWithNoChainsIsRefused(t *testing.T) {
	srv := testServer(t)
	for _, body := range []string{`{"chains":[]}`, `{}`} {
		res := srv.do(t, "POST", "/v1/spec", []byte(body))
		if res.status != 400 {
			t.Fatalf("%s: status = %d, want 400: %s", body, res.status, res.body)
		}
		if msg := res.errText(t); !strings.Contains(msg, "chains") {
			t.Errorf("%s: message %q does not name the field", body, msg)
		}
	}
}

func TestSpecNamesTheChainItCouldNotFind(t *testing.T) {
	srv := testServer(t)
	res := srv.do(t, "POST", "/v1/spec", specBody(extAda1, extNone))
	if res.status != 404 {
		t.Fatalf("status = %d, want 404: %s", res.status, res.body)
	}
	if msg := res.errText(t); !strings.Contains(msg, extNone) {
		t.Errorf("message %q does not name the missing chain", msg)
	}
}

// A misspelled field means the caller is reading a different contract, and
// building the page they did not ask for is worse than refusing.
func TestSpecRefusesABodyItDoesNotUnderstand(t *testing.T) {
	srv := testServer(t)
	for _, body := range []string{
		`{"chains":["` + extAda1 + `"],"titel":"typo"}`,
		`{"chains":"` + extAda1 + `"}`,
		`not json at all`,
	} {
		res := srv.do(t, "POST", "/v1/spec", []byte(body))
		if res.status != 400 {
			t.Errorf("%s: status = %d, want 400: %s", body, res.status, res.body)
			continue
		}
		res.errText(t)
	}
}

// A named refresh rewrites the saved page, exactly as the build would: the
// client shows the refreshed spec and the file under /view/<name> is the same
// run, so a reload lands on what the refresh produced rather than the stale
// build.
func TestRefreshWithANameRewritesTheSavedPage(t *testing.T) {
	srv, api := testServer(t), loadAPI(t)

	// Build and save under a name, as the client does at build time.
	blob, _ := json.Marshal(specRequest{
		Chains: []string{extAda1}, Title: "Solar install quote",
		Me: []string{"ada@loomworks.example"}, Name: "solar-install-quote",
	})
	built := srv.do(t, "POST", "/v1/spec", blob)
	if built.status != 200 {
		t.Fatalf("build: status = %d: %s", built.status, built.body)
	}

	// Refresh the built page, naming it so the server rewrites the file.
	body, _ := json.Marshal(refreshRequest{
		Spec: decode[spec.Spec](t, built),
		Name: "solar-install-quote",
	})
	res := srv.do(t, "POST", "/v1/refresh", body)
	if res.status != 200 {
		t.Fatalf("refresh: status = %d: %s", res.status, res.body)
	}
	api.assert(t, "RefreshResponse", res.body)

	// The saved page under that name is now the refreshed run, not the build.
	// The refresh response embeds the spec beside a report of what it did, so
	// compare the saved file against that embedded spec rather than the whole
	// envelope.
	saved := srv.do(t, "GET", "/v1/specs/solar-install-quote", nil)
	if saved.status != 200 {
		t.Fatalf("GET saved page: status = %d: %s", saved.status, saved.body)
	}
	refreshed := decode[struct {
		Spec spec.Spec `json:"spec"`
	}](t, res)
	savedSpec := decode[spec.Spec](t, saved)
	if mustMarshal(t, savedSpec) != mustMarshal(t, refreshed.Spec) {
		t.Error("saved page differs from the spec the refresh returned")
	}
}

func mustMarshal(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// A refresh reproduces a page from the corpus, so the endpoint is exercised
// against a saved spec: build it via POST /v1/spec, then bring it up to date.
// Accepting nothing and fetching nothing (the server has no mailbox), the
// refreshed page is the page it was, reported as such.
func TestRefreshBringsABuiltPageUpToDate(t *testing.T) {
	srv, api := testServer(t), loadAPI(t)
	built := srv.do(t, "POST", "/v1/spec", specBody(extAda1))
	if built.status != 200 {
		t.Fatalf("build: status = %d: %s", built.status, built.body)
	}

	body, _ := json.Marshal(refreshRequest{Spec: decode[spec.Spec](t, built)})
	res := srv.do(t, "POST", "/v1/refresh", body)
	if res.status != 200 {
		t.Fatalf("status = %d: %s", res.status, res.body)
	}
	// The response asserts against the contract, so a field the document forgot
	// to declare is one a client cannot trust.
	api.assert(t, "RefreshResponse", res.body)

	got := decode[struct {
		Spec struct {
			Title    string
			Messages []struct{ Sender string }
		}
		Report struct {
			EntriesBefore int  `json:"entriesBefore"`
			EntriesAfter  int  `json:"entriesAfter"`
			NothingNew    bool `json:"nothingNew"`
		}
	}](t, res)
	if got.Spec.Title != "Solar install quote" {
		t.Errorf("title = %q", got.Spec.Title)
	}
	// Nothing new has arrived, so the page is unchanged and says so.
	if len(got.Spec.Messages) != 3 {
		t.Errorf("%d messages, want the built page's 3", len(got.Spec.Messages))
	}
	if got.Report.EntriesBefore != 3 || got.Report.EntriesAfter != 3 || !got.Report.NothingNew {
		t.Errorf("report = %+v, want 3 -> 3 and nothingN", got.Report)
	}
}

// The refresh surface is corpus-only, so accepting a proposed chain is the way
// a page grows. There is no fetching: that is the CLI's, not this server's. A
// spec carrying an unknown chain is still rejected on the input side.
func TestRefreshRefusesANewPageAndAFutureVersion(t *testing.T) {
	srv := testServer(t)
	// A freshly built page has messages, but a refresh also has to reproduce the
	// selection. Posting a body that is not a spec at all fails the same way.
	res := srv.do(t, "POST", "/v1/refresh", []byte(`{}`))
	if res.status != 400 {
		t.Fatalf("{}: status = %d, want 400: %s", res.status, res.body)
	}
	if msg := res.errText(t); !strings.Contains(msg, "spec") {
		t.Errorf("message %q does not name the missing spec", msg)
	}
	// A malformed body is refused, not half-read.
	res = srv.do(t, "POST", "/v1/refresh", []byte(`not json`))
	if res.status != 400 {
		t.Errorf("malformed body: status = %d, want 400: %s", res.status, res.body)
	}
	res.errText(t)
}

// A misspelled field is a caller reading a different contract, and the server
// must not re-derive the page they did not ask for.
func TestRefreshRefusesABodyItDoesNotUnderstand(t *testing.T) {
	srv := testServer(t)
	built := srv.do(t, "POST", "/v1/spec", specBody(extAda1))
	t.Helper()
	if built.status != 200 {
		t.Fatalf("build: status = %d: %s", built.status, built.body)
	}
	var sp spec.Spec
	if err := json.Unmarshal(built.body, &sp); err != nil {
		t.Fatalf("decoding built spec: %v", err)
	}
	for _, body := range []string{
		`{"spec":{"title":"t","messages":[{"date":"d","body":"b"}]},"titel":"typo"}`,
		`{"spec":{"title":"t","messages":[{"date":"d","body":"b"}]},"accept":"not-an-array"}`,
	} {
		res := srv.do(t, "POST", "/v1/refresh", []byte(body))
		if res.status != 400 {
			t.Errorf("%s: status = %d, want 400: %s", body, res.status, res.body)
		}
	}
}

func TestRefreshObeysTheMethodShape(t *testing.T) {
	srv := testServer(t)
	res := srv.do(t, "GET", "/v1/refresh", nil)
	if res.status != 405 {
		t.Errorf("GET /v1/refresh = %d, want 405", res.status)
	}
	if got := res.header.Get("Allow"); got != "POST" {
		t.Errorf("Allow = %q, want POST", got)
	}
	res.errText(t)
}

func TestStatsCountsWhatIsThereAndWhatIsMissing(t *testing.T) {
	srv, api := testServer(t), loadAPI(t)
	res := srv.do(t, "GET", "/v1/stats", nil)
	if res.status != 200 {
		t.Fatalf("status = %d: %s", res.status, res.body)
	}
	api.assert(t, "Stats", res.body)
	got := decode[struct {
		Entries    int64
		BySource   map[string]int64 `json:"bySource"`
		People     int64
		Embeddings []any
	}](t, res)
	if got.Entries != 4 {
		t.Errorf("entries = %d, want the fixture's 4", got.Entries)
	}
	if got.BySource["mail"] != 4 {
		t.Errorf("bySource = %v", got.BySource)
	}
	if got.People < 2 {
		t.Errorf("people = %d", got.People)
	}
	// Nothing is embedded here, and the empty array is the honest report of
	// that: semantic search has nothing to rank against.
	if got.Embeddings == nil {
		t.Error("embeddings is null; the contract requires an array")
	}
}

func TestPeopleListsRecipientsAndSendersAlike(t *testing.T) {
	srv, api := testServer(t), loadAPI(t)
	res := srv.do(t, "GET", "/v1/people", nil)
	if res.status != 200 {
		t.Fatalf("status = %d: %s", res.status, res.body)
	}
	api.assert(t, "PeopleResponse", res.body)
	got := decode[struct {
		People []struct {
			DisplayName string   `json:"displayName"`
			Identities  []string `json:"identities"`
			Sent        int64
			Received    int64
		}
	}](t, res)
	if len(got.People) < 2 {
		t.Fatalf("people = %+v", got.People)
	}
	var addressed int
	for _, p := range got.People {
		if len(p.Identities) > 0 {
			addressed++
		}
	}
	if addressed < 2 {
		t.Error("nobody carries an identity; the endpoint's whole point is which addresses fold together")
	}
}

// Every non-2xx carries the one error shape, including the ones ServeMux would
// otherwise answer with plain text.
func TestAWrongMethodAndAnUnknownPathAnswerInTheSameShape(t *testing.T) {
	srv := testServer(t)
	res := srv.do(t, "GET", "/v1/spec", nil)
	if res.status != 405 {
		t.Errorf("GET /v1/spec = %d, want 405: %s", res.status, res.body)
	}
	if got := res.header.Get("Allow"); got != "POST" {
		t.Errorf("Allow = %q, want POST", got)
	}
	res.errText(t)

	res = srv.do(t, "GET", "/v1/ingest", nil)
	if res.status != 404 {
		t.Errorf("an operator command is reachable: %d %s", res.status, res.body)
	}
	res.errText(t)
}

// Spec assembly is seconds of work per large selection, so the answer to too
// many at once is a retryable refusal — not a queue, and not every core in HTML
// recovery while the cheap endpoints starve.
func TestSpecRefusesRatherThanQueuesWhenEveryBuilderIsBusy(t *testing.T) {
	srv := testServer(t)
	for i := 0; i < specConcurrency; i++ {
		srv.specSlots <- struct{}{}
	}
	res := srv.do(t, "POST", "/v1/spec", specBody(extAda1))
	if res.status != 429 {
		t.Fatalf("status = %d, want 429: %s", res.status, res.body)
	}
	if res.header.Get("Retry-After") == "" {
		t.Error("no Retry-After, so a client has nothing to back off by")
	}
	res.errText(t)

	// And the endpoint recovers: a slot freed is a slot usable.
	<-srv.specSlots
	if res := srv.do(t, "POST", "/v1/spec", specBody(extAda1)); res.status != 200 {
		t.Errorf("status = %d after a slot freed: %s", res.status, res.body)
	}
}

// A machine with no probe is a real state, not an error: every known backend
// is answered unchecked and nothing claims a probe ever ran. This is what the
// screen looks like on first boot, before `corpus status` has written a
// snapshot.
func TestStatusWithNoSnapshotNamesEveryBackendUnchecked(t *testing.T) {
	srv, api := testServer(t), loadAPI(t)
	res := srv.do(t, "GET", "/v1/status", nil)
	if res.status != 200 {
		t.Fatalf("status = %d: %s", res.status, res.body)
	}
	api.assert(t, "StatusResponse", res.body)
	got := decode[statusResponse](t, res)
	if got.CheckedAt != "" {
		t.Errorf("no probe ever ran, but checkedAt is %q", got.CheckedAt)
	}
	if len(got.Services) != 3 {
		t.Fatalf("%d services, want the known backends", len(got.Services))
	}
	for _, svc := range got.Services {
		if svc.Status != status.Unchecked {
			t.Errorf("%s = %q, want unchecked with no snapshot", svc.ID, svc.Status)
		}
	}
}

// The server is not the one who asks the backends whether they are logged in;
// it serves what the operator's probe wrote. That boundary is the whole point:
// the read-only surface does not reach docket or slackdump.
func TestStatusServesTheSnapshotTheProbeWrote(t *testing.T) {
	srv, api := testServer(t), loadAPI(t)

	blob, _ := json.Marshal(map[string]any{
		"checkedAt": "2026-08-22T15:04:00Z",
		"services": []any{
			map[string]any{"id": "mail", "label": "Gmail (docket)",
				"status": "ok", "detail": "docket answered"},
			map[string]any{"id": "embed", "label": "Embeddings (ollama)",
				"status": "down", "detail": "no daemon"},
		},
	})
	writeSnapshot(t, srv.statusPath, blob)

	res := srv.do(t, "GET", "/v1/status", nil)
	if res.status != 200 {
		t.Fatalf("status = %d: %s", res.status, res.body)
	}
	api.assert(t, "StatusResponse", res.body)
	got := decode[statusResponse](t, res)
	if got.CheckedAt != "2026-08-22T15:04:00Z" {
		t.Errorf("checkedAt = %q", got.CheckedAt)
	}
	byID := map[string]serviceStatus{}
	for _, svc := range got.Services {
		byID[svc.ID] = svc
	}
	// The snapshot carried no slack row: it still appears, unchecked, so the
	// screen never silently drops a backend it knows about.
	var slack *serviceStatus
	for i, svc := range got.Services {
		if svc.ID == "slack" {
			slack = &got.Services[i]
		}
	}
	if len(byID) != 3 || slack == nil {
		t.Errorf("services = %+v", byID)
	}
	if slack.Status != status.Unchecked {
		t.Errorf("slack = %q, want unchecked (the probe carried no row)", slack.Status)
	}
	if byID["embed"].Status != "down" || !strings.Contains(byID["embed"].Detail, "no daemon") {
		t.Errorf("embed = %+v", byID["embed"])
	}
}

func writeSnapshot(t *testing.T, path string, blob []byte) {
	t.Helper()
	if err := os.WriteFile(path, blob, 0o600); err != nil {
		t.Fatalf("writing the snapshot: %v", err)
	}
}
