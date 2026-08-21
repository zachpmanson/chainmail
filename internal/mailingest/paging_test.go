package mailingest

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zachpmanson/chainmail/internal/corpus"
)

// fakeBox serves fixed pages. Invented ids and example.com addresses only.
type fakeBox struct {
	pages    [][]Envelope
	byToken  map[string]int // token -> page index
	searches int
	reads    int
	// failAfter aborts the next Search once that many searches have been served,
	// standing in for the process being killed mid-walk.
	failAfter int
}

// newFake builds a walk of n pages of size each, ordered newest first, one day
// apart, so a frontier stop has timestamps to compare.
func newFake(n, size int) *fakeBox {
	f := &fakeBox{byToken: map[string]int{}, failAfter: -1}
	day := 28
	for p := 0; p < n; p++ {
		var page []Envelope
		for i := 0; i < size; i++ {
			id := fmt.Sprintf("p%dm%d", p, i)
			page = append(page, Envelope{
				ID:        id,
				ThreadID:  "t" + id,
				From:      "alice@example.com",
				To:        "bob@example.com",
				Subject:   "the plan",
				MessageID: "<" + id + "@example.com>",
				Date:      fmt.Sprintf("Mon, %02d Jan 2026 15:04:05 +0000", day),
			})
			day--
		}
		f.pages = append(f.pages, page)
		if p > 0 {
			f.byToken[fmt.Sprintf("tok%d", p)] = p
		}
	}
	return f
}

func (f *fakeBox) Search(query string, limit int, token string) ([]Envelope, Page, error) {
	if f.failAfter >= 0 && f.searches >= f.failAfter {
		return nil, Page{}, fmt.Errorf("killed")
	}
	f.searches++
	idx, ok := f.byToken[token]
	if token != "" && !ok {
		return nil, Page{}, fmt.Errorf("unknown page token %q", token)
	}
	page := f.pages[idx]
	if limit > 0 && limit < len(page) {
		page = page[:limit]
	}
	p := Page{Returned: len(page), Limit: limit, HasMore: idx+1 < len(f.pages)}
	if p.HasMore {
		p.NextPageToken = fmt.Sprintf("tok%d", idx+1)
	}
	return page, p, nil
}

func (f *fakeBox) Read(id string) (Message, error) {
	f.reads++
	m := msg(id, "alice@example.com", "bob@example.com", "")
	m.ThreadID = "t" + id
	m.Date = "Mon, 02 Jan 2026 15:04:05 +0000"
	return m, nil
}

func TestIngestWalksEveryPage(t *testing.T) {
	s, f := store(t), newFake(3, 4)
	r, err := Ingest(s, f, "q", Bound{PageSize: 4})
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if r.Seen != 12 || r.Pages != 3 {
		t.Fatalf("got %d messages over %d pages, want 12 over 3", r.Seen, r.Pages)
	}
	if !r.Stop.Covered() || r.Stop != StopExhausted {
		t.Fatalf("stop: got %q, want exhausted and covered", r.Stop)
	}
}

func TestASinglePageWithoutMoreStops(t *testing.T) {
	s, f := store(t), newFake(1, 3)
	r, err := Ingest(s, f, "q", Bound{PageSize: 3})
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if f.searches != 1 {
		t.Fatalf("searches: got %d, want 1 — has_more false must not be paged past", f.searches)
	}
	if r.Stop != StopExhausted {
		t.Fatalf("stop: got %q", r.Stop)
	}
}

// The issue's central complaint: a bound took 40 of 90 and said nothing. The
// bound is still allowed; being unable to tell it apart from a finished walk is
// not.
func TestABoundStopsShortAndSaysSo(t *testing.T) {
	s, f := store(t), newFake(3, 4)
	r, err := Ingest(s, f, "q", Bound{Max: 5, PageSize: 4})
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if r.Stop != StopMax || r.Stop.Covered() {
		t.Fatalf("stop: got %q covered=%v, want max and not covered", r.Stop, r.Stop.Covered())
	}
	if r.Seen < 4 || r.Seen > 5 {
		t.Fatalf("seen: got %d, want the bound of 5 or the page below it", r.Seen)
	}
	if r.NextPage == "" {
		t.Fatal("a bounded walk must leave the token that continues it")
	}
	cur, err := corpus.LoadCursor(s, corpus.SourceMail, "q")
	if err != nil {
		t.Fatal(err)
	}
	if cur.Complete || cur.Position == "" {
		t.Fatalf("cursor after a bounded walk: %+v", cur)
	}
	if !cur.Frontier.IsZero() {
		t.Fatal("a bounded walk must not advance the frontier: the pages below it were never read")
	}
}

// A second run over a query whose first run finished must read the new mail and
// not the archive behind it.
func TestASecondRunResumesRatherThanRepeating(t *testing.T) {
	s, f := store(t), newFake(3, 4)
	if _, err := Ingest(s, f, "q", Bound{PageSize: 4}); err != nil {
		t.Fatalf("first Ingest: %v", err)
	}
	first := f.reads

	f.reads, f.searches = 0, 0
	r, err := Ingest(s, f, "q", Bound{PageSize: 4})
	if err != nil {
		t.Fatalf("second Ingest: %v", err)
	}
	if r.Stop != StopFrontier {
		t.Fatalf("stop: got %q, want frontier", r.Stop)
	}
	if !r.Stop.Covered() {
		t.Fatal("a frontier stop is covered: the mail below it is already in")
	}
	if f.reads >= first {
		t.Fatalf("second run read %d of %d — a cursor that saves no work is not a cursor",
			f.reads, first)
	}
	if f.searches != 1 {
		t.Fatalf("second run made %d searches, want 1", f.searches)
	}
}

func TestAKillMidWalkLeavesAResumableCursor(t *testing.T) {
	s := store(t)
	f := newFake(3, 4)
	f.failAfter = 2 // two pages served, then the third request dies
	if _, err := Ingest(s, f, "q", Bound{PageSize: 4}); err == nil {
		t.Fatal("want an error from the aborted walk")
	}
	cur, err := corpus.LoadCursor(s, corpus.SourceMail, "q")
	if err != nil {
		t.Fatal(err)
	}
	if cur.Complete {
		t.Fatal("an aborted walk must not record completion")
	}
	if cur.Position == "" {
		t.Fatal("no position to resume from")
	}

	g := newFake(3, 4)
	r, err := Ingest(s, g, "q", Bound{PageSize: 4})
	if err != nil {
		t.Fatalf("resumed Ingest: %v", err)
	}
	if !r.Resumed {
		t.Fatal("the second run did not report resuming")
	}
	if g.searches != 1 {
		t.Fatalf("resumed run made %d searches, want the 1 page the kill left", g.searches)
	}
	if r.Stop != StopExhausted {
		t.Fatalf("stop: got %q, want exhausted", r.Stop)
	}
}

// A page can come back empty while has_more is still set. Treating that as the
// end of the query would record coverage of a set never read.
func TestAnEmptyPageIsNotTheEndOfTheQuery(t *testing.T) {
	s := store(t)
	f := newFake(3, 2)
	f.pages[1] = nil
	r, err := Ingest(s, f, "q", Bound{PageSize: 2})
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if r.Pages != 3 {
		t.Fatalf("pages: got %d, want 3 — an empty middle page must be paged past", r.Pages)
	}
	if r.Seen != 4 {
		t.Fatalf("seen: got %d, want 4", r.Seen)
	}
}

// A wholly empty query is complete, and says so without a frontier to show for
// it: there was nothing to establish one from.
func TestAnEmptyQueryIsCompleteWithNoFrontier(t *testing.T) {
	s := store(t)
	f := &fakeBox{pages: [][]Envelope{nil}, byToken: map[string]int{}, failAfter: -1}
	r, err := Ingest(s, f, "q", Bound{})
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if r.Seen != 0 || r.Stop != StopExhausted {
		t.Fatalf("got %d seen, stop %q", r.Seen, r.Stop)
	}
	cur, err := corpus.LoadCursor(s, corpus.SourceMail, "q")
	if err != nil {
		t.Fatal(err)
	}
	if !cur.Complete || !cur.Frontier.IsZero() {
		t.Fatalf("cursor: %+v", cur)
	}
}

// has_more with no token is unusable: there is no way to continue, so the walk
// must fail rather than call itself finished.
func TestMoreResultsWithNoTokenIsAnError(t *testing.T) {
	s := store(t)
	f := newFake(2, 2)
	f.byToken = map[string]int{} // the token the first page hands out is now unknown
	if _, err := Ingest(s, f, "q", Bound{PageSize: 2}); err == nil {
		t.Fatal("want an error, not a silently short walk")
	}
}

// fakeDocket writes a docket-shaped envelope and counts its own invocations.
func fakeDocket(t *testing.T) (bin, tally string) {
	t.Helper()
	dir := t.TempDir()
	bin = filepath.Join(dir, "docket")
	tally = filepath.Join(dir, "calls")
	script := "#!/bin/sh\necho x >> " + tally + `
cat <<'JSON'
{"ok":true,"data":[{"id":"m1","message_id":"<m1@example.com>"}],
 "page":{"returned":1,"limit":1,"has_more":false}}
JSON
`
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin, tally
}

// The probe is one live API call. A cron top-up walking a dozen containers in
// one process must not pay for a dozen identical answers.
func TestTheProbeIsCalledOncePerProcess(t *testing.T) {
	bin, tally := fakeDocket(t)
	c := Client{Bin: bin}
	for i := 0; i < 4; i++ {
		ok, err := c.SupportsThreadingHeaders()
		if err != nil {
			t.Fatalf("probe %d: %v", i, err)
		}
		if !ok {
			t.Fatal("the fake exposes a message_id")
		}
	}
	b, err := os.ReadFile(tally)
	if err != nil {
		t.Fatal(err)
	}
	if n := len(strings.Fields(string(b))); n != 1 {
		t.Fatalf("docket ran %d times, want 1", n)
	}
}

// A docket old enough to predate paging sends no `page` block. It has no token
// to offer, so the only truthful reading is a single complete page — the walk
// must not spin waiting for a continuation that cannot arrive.
func TestASearchWithoutAPageBlockIsOnePage(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "docket")
	args := filepath.Join(dir, "args")
	script := "#!/bin/sh\necho \"$@\" >> " + args + `
cat <<'JSON'
{"ok":true,"data":[{"id":"m1","message_id":"<m1@example.com>"}]}
JSON
`
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	c := Client{Bin: bin}
	envs, page, err := c.Search("q", 10, "tok")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(envs) != 1 || page.HasMore || page.NextPageToken != "" {
		t.Fatalf("envs %d, page %+v", len(envs), page)
	}
	b, err := os.ReadFile(args)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "--page-token tok") {
		t.Fatalf("a token must be passed through: %q", b)
	}
}
