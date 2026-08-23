package refresh

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/zachpmanson/chainmail/internal/corpus"
	"github.com/zachpmanson/chainmail/internal/mailingest"
	"github.com/zachpmanson/chainmail/internal/spec"
)

// Invented mail only. Nothing from a real mailbox belongs in a committed test.

// fakeMailbox is a mailbox whose contents a test states outright. Search matches
// on words, the way a mail search does, which is what makes the interesting case
// expressible: a reply the search cannot see.
type fakeMailbox struct {
	byID    map[string]mailingest.Message
	threads map[string][]string // thread id -> message ids, in order
	reads   []string
	fail    error
}

func newMailbox() *fakeMailbox {
	return &fakeMailbox{byID: map[string]mailingest.Message{}, threads: map[string][]string{}}
}

func (f *fakeMailbox) add(m mailingest.Message) {
	f.byID[m.ID] = m
	f.threads[m.ThreadID] = append(f.threads[m.ThreadID], m.ID)
}

func (f *fakeMailbox) Search(query string, limit int, _ string) ([]mailingest.Envelope, mailingest.Page, error) {
	if f.fail != nil {
		return nil, mailingest.Page{}, f.fail
	}
	// Drop the date narrowing before matching: the fake has no clock, and every
	// message it holds is meant to be in range.
	var terms []string
	for _, w := range strings.Fields(strings.ToLower(query)) {
		if !strings.HasPrefix(w, "after:") {
			terms = append(terms, w)
		}
	}
	var out []mailingest.Envelope
	for _, ids := range f.threads {
		for _, id := range ids {
			m := f.byID[id]
			hay := strings.ToLower(m.Subject + " " + m.Body)
			all := true
			for _, t := range terms {
				if !strings.Contains(hay, t) {
					all = false
					break
				}
			}
			if all && len(out) < limit {
				out = append(out, m.Envelope)
			}
		}
	}
	return out, mailingest.Page{}, nil
}

func (f *fakeMailbox) Read(id string) (mailingest.Message, error) {
	if f.fail != nil {
		return mailingest.Message{}, f.fail
	}
	f.reads = append(f.reads, id)
	m, ok := f.byID[id]
	if !ok {
		return m, fmt.Errorf("no message %s", id)
	}
	return m, nil
}

func (f *fakeMailbox) Thread(id string) (mailingest.ThreadResult, error) {
	if f.fail != nil {
		return mailingest.ThreadResult{}, f.fail
	}
	res := mailingest.ThreadResult{ThreadID: id}
	for _, mid := range f.threads[id] {
		res.Messages = append(res.Messages, f.byID[mid].Envelope)
	}
	return res, nil
}

type msgOpt func(*mailingest.Message)

func inThread(t string) msgOpt { return func(m *mailingest.Message) { m.ThreadID = t } }
func replyTo(id string) msgOpt {
	return func(m *mailingest.Message) { m.InReplyTo = "<" + id + "@example.com>" }
}
func on(date string) msgOpt   { return func(m *mailingest.Message) { m.Date = date } }
func from(addr string) msgOpt { return func(m *mailingest.Message) { m.From = addr } }
func subject(s string) msgOpt { return func(m *mailingest.Message) { m.Subject = s } }

// msg builds one message of the trail. The subject deliberately shares no word
// with the queries these tests use, and the query's words live in a body: a reply
// inherits its parent's subject, so a subject that matched would let the query
// pass find every reply and the thread pass would never be the only way in.
func msg(id, body string, opts ...msgOpt) mailingest.Message {
	m := mailingest.Message{Body: body}
	m.ID = id
	m.MessageID = "<" + id + "@example.com>"
	m.ThreadID = "thread-hedge"
	m.Date = "Mon, 02 Feb 2026 09:15:00 +1100"
	m.Subject = "Fernlea site access"
	m.From = "Bo Vantel <bo@fernlea.example.com>"
	m.To = "Ilma Reko <ilma@example.fed>"
	for _, o := range opts {
		o(&m)
	}
	return m
}

func store(t *testing.T) *corpus.Store {
	t.Helper()
	s, err := corpus.Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func put(t *testing.T, s *corpus.Store, msgs ...mailingest.Message) {
	t.Helper()
	for _, m := range msgs {
		if _, err := mailingest.Put(s, m); err != nil {
			t.Fatalf("Put %s: %v", m.ID, err)
		}
	}
	if _, err := s.ResolveParents(); err != nil {
		t.Fatalf("ResolveParents: %v", err)
	}
}

// generate is the previous run: the spec a page was rendered from.
func generate(t *testing.T, s *corpus.Store, opts spec.Options) spec.Spec {
	t.Helper()
	sp, err := spec.Generate(s, opts)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	return sp
}

func prevRun(t *testing.T, s *corpus.Store, container, query string) spec.Spec {
	t.Helper()
	return generate(t, s, spec.Options{
		Containers: []string{container},
		Title:      "The paddock survey",
		RunLabel:   "2 Feb 2026",
		Queries:    []spec.Query{{Q: query, Note: "corpus search"}},
		Me:         []string{"ilma@example.fed"},
		Params:     &spec.RunParams{Me: []string{"ilma@example.fed"}, Limit: 7},
	})
}

func run(t *testing.T, s *corpus.Store, mb Mailbox, prev spec.Spec, opts Options) (Report, spec.Spec) {
	t.Helper()
	if opts.RunLabel == "" {
		opts.RunLabel = "3 Feb 2026"
	}
	rep, next, err := Run(s, mb, prev, opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return rep, next
}

// The case the command exists for. "sounds good, friday then" carries none of
// the query's words, so no re-run of the search can find it; only fetching the
// chain by id does.
func TestThreadPassPicksUpAReplyNoQueryMatches(t *testing.T) {
	s := store(t)
	first := msg("m1", "here is the paddock survey we agreed")
	put(t, s, first)
	prev := prevRun(t, s, "thread-hedge", "paddock survey")

	mb := newMailbox()
	mb.add(first)
	mb.add(msg("m2", "sounds good, friday then",
		replyTo("m1"), on("Tue, 03 Feb 2026 08:00:00 +1100"),
		from("Ilma Reko <ilma@example.fed>")))

	rep, next := run(t, s, mb, prev, Options{Fetch: true})

	if got := rep.Queries[0].Fetched; got != 0 {
		t.Errorf("the query pass read %d messages; the new reply matches no query, "+
			"so it must arrive by the thread pass or not at all", got)
	}
	if rep.Threads[0].Created != 1 {
		t.Errorf("thread pass: %+v", rep.Threads[0])
	}
	if len(rep.ChainsGrown) != 1 || rep.ChainsGrown[0].After != 2 {
		t.Errorf("chains grown = %+v", rep.ChainsGrown)
	}
	if rep.NothingNew() {
		t.Error("a new reply arrived, so this is not a nothing-new refresh")
	}
	if len(next.Messages) != 2 {
		t.Errorf("refreshed spec has %d entries, want 2", len(next.Messages))
	}
}

// The thread pass is not conditional on the query pass finding anything: a query
// that returns only what the corpus already has must not stop the chains from
// being fetched.
func TestThreadPassRunsWhenTheQueryFindsNothingNew(t *testing.T) {
	s := store(t)
	first := msg("m1", "here is the paddock survey we agreed")
	put(t, s, first)
	prev := prevRun(t, s, "thread-hedge", "paddock survey")

	mb := newMailbox()
	mb.add(first) // the search returns this, and the corpus has it already
	mb.add(msg("m2", "one more thing about the fence line",
		replyTo("m1"), on("Tue, 03 Feb 2026 08:00:00 +1100")))

	rep, _ := run(t, s, mb, prev, Options{Fetch: true})

	if rep.Queries[0].Seen == 0 {
		t.Fatal("the query pass saw nothing at all, so this proves nothing")
	}
	if rep.Queries[0].Created != 0 {
		t.Errorf("query pass created %d; everything it saw was already stored", rep.Queries[0].Created)
	}
	if rep.Threads[0].Created != 1 {
		t.Errorf("the thread pass must run regardless: %+v", rep.Threads[0])
	}
}

// A chain the query newly finds is a proposal, not an inclusion: the chain list
// is a curation decision and refresh does not overrule it.
func TestANewChainIsProposedRatherThanIncluded(t *testing.T) {
	s := store(t)
	first := msg("m1", "here is the paddock survey we agreed")
	put(t, s, first)
	prev := prevRun(t, s, "thread-hedge", "paddock survey")

	other := msg("m9", "a second paddock survey, for the north block",
		subject("North block access"), inThread("thread-north"),
		on("Tue, 03 Feb 2026 11:00:00 +1100"))
	mb := newMailbox()
	mb.add(first)
	mb.add(other)

	rep, next := run(t, s, mb, prev, Options{Fetch: true})

	if len(rep.ChainsProposed) != 1 {
		t.Fatalf("proposed = %+v", rep.ChainsProposed)
	}
	c := rep.ChainsProposed[0]
	if c.Container != "thread-north" || c.RootExtID == "" || c.Entries == 0 || c.Span == "" {
		t.Errorf("a proposal must be judgeable without opening anything: %+v", c)
	}
	if c.Query != "paddock survey" {
		t.Errorf("a proposal names the query that found it: %+v", c)
	}
	if len(next.Threads) != 1 {
		t.Errorf("the proposal must stay off the page until accepted: %+v", next.Threads)
	}
	if rep.NothingNew() {
		t.Error("a proposal is something new; a reader has a decision to make")
	}

	// And accepting it by the handle the report prints takes it, without the
	// search being re-run against the mailbox.
	rep2, next2 := run(t, s, mb, prev, Options{Accept: []string{c.RootExtID}})
	if len(next2.Threads) != 2 {
		t.Errorf("accepted chain missing: %+v", next2.Threads)
	}
	if len(rep2.ChainsAdded) != 1 || len(rep2.ChainsProposed) != 0 {
		t.Errorf("added = %+v, proposed = %+v", rep2.ChainsAdded, rep2.ChainsProposed)
	}
}

// Without -fetch the corpus is the only source, which is the ordinary case: a
// plain `corpus ingest` filled it and refresh re-derives the page from it.
func TestRefreshWithoutFetchStillGrowsFromTheCorpus(t *testing.T) {
	s := store(t)
	first := msg("m1", "here is the paddock survey we agreed")
	put(t, s, first)
	prev := prevRun(t, s, "thread-hedge", "paddock survey")

	mb := newMailbox()
	put(t, s, msg("m2", "sounds good, friday then",
		replyTo("m1"), on("Tue, 03 Feb 2026 08:00:00 +1100")))

	rep, next := run(t, s, mb, prev, Options{})

	if len(mb.reads) != 0 {
		t.Errorf("the mailbox was read without -fetch: %v", mb.reads)
	}
	if rep.Fetched {
		t.Error("the report claims a fetch that did not happen")
	}
	if len(rep.ChainsGrown) != 1 || len(next.Messages) != 2 {
		t.Errorf("grown = %+v, entries = %d", rep.ChainsGrown, len(next.Messages))
	}
	if rep.Threads[0].Skipped == "" {
		t.Error("a chain regenerated without asking the mailbox has to say so")
	}
}

// Nothing-new is an outcome, not a failure: Run returns no error and the report
// says so in one place a caller can branch on.
func TestNothingNewIsReportedNotErrored(t *testing.T) {
	s := store(t)
	first := msg("m1", "here is the paddock survey we agreed")
	put(t, s, first)
	prev := prevRun(t, s, "thread-hedge", "paddock survey")

	mb := newMailbox()
	mb.add(first)

	rep, next := run(t, s, mb, prev, Options{Fetch: true})

	if !rep.NothingNew() {
		t.Errorf("nothing arrived, so NothingNew must hold: %+v", rep)
	}
	if len(rep.Queries) != 1 || len(rep.Threads) != 1 {
		t.Error("the report still names every pass, because that is the proof it looked")
	}
	if len(next.Messages) != len(prev.Messages) {
		t.Errorf("entries %d -> %d", len(prev.Messages), len(next.Messages))
	}

	// A mailbox that cannot be reached is the other thing entirely.
	mb.fail = fmt.Errorf("docket: token expired")
	if _, _, err := Run(s, mb, prev, Options{Fetch: true, RunLabel: "3 Feb 2026"}); err == nil {
		t.Fatal("an unreachable mailbox must be an error, not a quiet nothing-new")
	}
}

func TestParametersSurviveTheRoundTrip(t *testing.T) {
	s := store(t)
	put(t, s, msg("m1", "here is the paddock survey we agreed"),
		msg("m2", "received, thank you", replyTo("m1"), from("Ilma Reko <ilma@example.fed>"),
			on("Mon, 02 Feb 2026 10:00:00 +1100")))
	prev := prevRun(t, s, "thread-hedge", "paddock survey")

	_, next := run(t, s, newMailbox(), prev, Options{})

	if next.Title != prev.Title {
		t.Errorf("title %q -> %q", prev.Title, next.Title)
	}
	if next.RunParams == nil {
		t.Fatal("the refreshed spec records no parameters, so the next refresh cannot reproduce it")
	}
	if got, want := next.RunParams.Limit, prev.RunParams.Limit; got != want {
		t.Errorf("limit %d -> %d", want, got)
	}
	if got, want := strings.Join(next.RunParams.Me, ","), "ilma@example.fed"; got != want {
		t.Errorf("me %q, want %q", got, want)
	}
	if len(next.Queries) != 1 || next.Queries[0].Q != "paddock survey" {
		t.Errorf("queries = %+v", next.Queries)
	}
	// An outbound message is still marked as such, which is what -me is for.
	var outbound int
	for _, m := range next.Messages {
		if m.Me {
			outbound++
		}
	}
	if outbound == 0 {
		t.Error("no entry is marked outbound, so -me was lost in the round trip")
	}
	if len(next.Runs) != 1 || next.Runs[0] != "2 Feb 2026" {
		t.Errorf("runs = %v, want the superseded pass named", next.Runs)
	}
}

func TestRefreshingTwiceAddsNothingTheSecondTime(t *testing.T) {
	s := store(t)
	first := msg("m1", "here is the paddock survey we agreed")
	put(t, s, first)
	prev := prevRun(t, s, "thread-hedge", "paddock survey")

	mb := newMailbox()
	mb.add(first)
	mb.add(msg("m2", "sounds good, friday then",
		replyTo("m1"), on("Tue, 03 Feb 2026 08:00:00 +1100")))

	_, once := run(t, s, mb, prev, Options{Fetch: true})
	rep, twice := run(t, s, mb, once, Options{Fetch: true})

	if !rep.NothingNew() {
		t.Errorf("the second refresh found something: %+v", rep)
	}
	if len(twice.Messages) != len(once.Messages) {
		t.Errorf("entries %d -> %d", len(once.Messages), len(twice.Messages))
	}
	reads := len(mb.reads)
	run(t, s, mb, twice, Options{Fetch: true})
	if len(mb.reads) != reads {
		t.Errorf("a third refresh re-read %d messages the corpus already holds",
			len(mb.reads)-reads)
	}
}

// The bug issue #20 describes: a mailbox copy that arrived long after a quote
// was recovered from it is one message stored twice, and a refresh is where a
// page re-derived over them would otherwise show it twice. Run's first step is
// the same sweep the slurp pipeline runs, and the report counts what it did.
func TestRefreshCollapsesStoredTwinsBeforeRedrawing(t *testing.T) {
	s := store(t)

	// First the mailbox original, as the slurper would store it.
	put(t, s, msg("orig-1",
		`Hi Ilma,

I have reviewed the north paddock survey and the boundary markers all check out
against the cadastre layer you sent last week. The crown land strip on the
western edge needs a separate approval before we fence it, so I am holding the
quote at the current rate until that clears.

Regards,
Bo Vantel | Fernlea Surveying`,
		inThread("thread-twins")))

	// Then the same message as it was recovered from a later reply, before the
	// original had arrived: a quote: entry with the quoter's wall clock (off by
	// the zone offset the quoter's client applied) and no zone of its own.
	bo, err := corpus.ResolveAddress(s, corpus.ParseAddresses("bo@fernlea.example.com")[0], "twins test")
	if err != nil {
		t.Fatalf("ResolveAddress: %v", err)
	}
	quoteExt := "quote:twins"
	_, created, err := s.PutQuoted(corpus.Entry{
		Source: corpus.SourceMail, ExtID: quoteExt, Kind: "message",
		TS:        time.Date(2026, 2, 2, 9, 15, 0, 0, time.UTC),
		PersonID:  bo,
		Container: "thread-twins",
		Subject:   "Fernlea site access",
		BodyText: `Hi Ilma, I have reviewed the north paddock survey and the boundary
markers all check out against the cadastre layer you sent last week. The crown
land strip on the western edge needs a separate approval before we fence it, so
I am holding the quote at the current rate. Regards, Bo Vantel`,
	})
	if err != nil {
		t.Fatalf("PutQuoted: %v", err)
	}
	if !created {
		t.Fatalf("quote copy already existed")
	}

	// The page contains both copies, so a refresh starts at 2 and ends at 1.
	prev := prevRun(t, s, "thread-twins", "paddock survey")
	if n := len(prev.Messages); n != 2 {
		t.Fatalf("prev run has %d entries, want the two copies", n)
	}
	rep, next := run(t, s, newMailbox(), prev, Options{})
	if rep.TwinsCollapsed != 1 {
		t.Errorf("TwinsCollapsed = %d, want 1", rep.TwinsCollapsed)
	}
	if rep.EntriesBefore != 2 || rep.EntriesAfter != 1 {
		t.Errorf("entries %d -> %d, want 2 -> 1", rep.EntriesBefore, rep.EntriesAfter)
	}
	if rep.NothingNew() {
		t.Error("a refresh that collapsed a twin reports nothing new")
	}
	if len(next.Messages) != 1 || next.Messages[0].Quoted {
		t.Errorf("survivor = %+v, want exactly the mailbox copy", next.Messages[0])
	}
}

// A spec recording nothing to re-run is a clear refusal rather than an empty
// page: there is no way to tell such a refresh from one that found nothing.
func TestASpecWithNothingToReRunIsRefused(t *testing.T) {
	s := store(t)
	put(t, s, msg("m1", "here is the paddock survey we agreed"))
	prev := spec.Spec{Title: "no provenance", Messages: []spec.Entry{{Date: "2 Feb 2026", Body: "x"}}}
	_, _, err := Run(s, newMailbox(), prev, Options{})
	if err == nil || !strings.Contains(err.Error(), "neither a query nor a thread") {
		t.Fatalf("err = %v", err)
	}
}

func TestAfterDateNarrowsToThePreviousRun(t *testing.T) {
	for label, want := range map[string]string{
		"2 Feb 2026":     "2026/02/02",
		"21 August 2026": "2026/08/21",
		"2026-02-02":     "2026/02/02",
		"pass 2, 20 Aug": "",
		"":               "",
	} {
		if got := AfterDate(label); got != want {
			t.Errorf("AfterDate(%q) = %q, want %q", label, got, want)
		}
	}
}
