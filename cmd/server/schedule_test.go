package main

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/zachpmanson/chainmail/internal/corpus"
)

// The schedule is the cadence in force deciding whether a tick ingests, so what
// these tests count is runs: the fake ingest below reaches no mailbox, and every
// assertion is about how many times it was asked — or whether a value the page
// could send is accepted at all.

// sweepable is the fixture with an ingest instead of a mailbox, and a cadence
// already stored where the test wants one. A caller that wants the default
// leaves every as "".
func sweepable(t *testing.T, every string) (*harness, *int) {
	t.Helper()
	srv := testServer(t)
	srv.slurpEnabled = true
	srv.slurpTimeout = 5 * time.Second
	if every != "" {
		if err := srv.store.PutSetting(corpus.SettingSlurpEvery, every); err != nil {
			t.Fatalf("storing the cadence: %v", err)
		}
	}
	runs := 0
	srv.runSweep = func(context.Context, string) ([]byte, error) {
		runs++
		return []byte("slurp\nmail: 0 new\n"), nil
	}
	return srv, &runs
}

// swept makes the last ingest ago-old. The cadence is measured from the last
// time the mailbox was reached, so a test that wants a tick to run has to give
// the loop something to measure from — a corpus that has never been swept is not
// due until a cadence after the server came up.
func swept(t *testing.T, srv *harness, ago time.Duration) {
	t.Helper()
	if err := srv.store.PutSetting(corpus.SettingSlurpAt,
		time.Now().Add(-ago).UTC().Format(time.RFC3339)); err != nil {
		t.Fatalf("storing the last sweep: %v", err)
	}
}

// The pulse: a cadence has elapsed since the last ingest, so the tick runs one —
// and having run it, does not run another the next time the loop looks.
func TestASweepRunsWhenItsCadenceHasElapsed(t *testing.T) {
	srv, runs := sweepable(t, "")
	swept(t, srv, 11*time.Minute)

	srv.sweepIfDue(context.Background())
	if *runs != 1 {
		t.Fatalf("%d sweeps, want one: the last ingest was a whole cadence ago", *runs)
	}
	if got := srv.lastSlurp(); got.IsZero() {
		t.Error("the sweep left no stamp, so the next tick would sweep again immediately")
	}

	srv.sweepIfDue(context.Background())
	if *runs != 1 {
		t.Errorf("%d sweeps, want one: a sweep was just made, and the cadence has not elapsed again", *runs)
	}
}

// The cadence decides, measured from the last attempt rather than from the last
// tick — a host that restarts, or a loop that misses a beat, still ingests on
// its schedule rather than on the loop's.
func TestTheCadenceDecidesWhenTheNextSweepIsDue(t *testing.T) {
	srv, runs := sweepable(t, "")
	now := time.Now()

	for _, c := range []struct {
		ago  time.Duration
		want int
	}{
		{ago: 9 * time.Minute, want: 0},  // inside the cadence
		{ago: 11 * time.Minute, want: 1}, // past it
		{ago: 0, want: 1},                // just swept, so nothing now
		{ago: 10 * time.Minute, want: 2}, // exactly the cadence: due
	} {
		if err := srv.store.PutSetting(corpus.SettingSlurpAt,
			now.Add(-c.ago).UTC().Format(time.RFC3339)); err != nil {
			t.Fatalf("storing the last sweep: %v", err)
		}
		srv.sweepIfDue(context.Background())
		if *runs != c.want {
			t.Errorf("last sweep %s ago: %d runs, want %d", c.ago, *runs, c.want)
		}
	}
}

// A sweep that failed still happened: the stamp is written before the walk, so a
// mailbox refusing logins is asked again in ten minutes rather than every thirty
// seconds. The run is reported, not swallowed — the loop logs it.
func TestASweepThatFailedStillHoldsTheNextOneOff(t *testing.T) {
	srv, runs := sweepable(t, "")
	swept(t, srv, 11*time.Minute)
	srv.runSweep = func(context.Context, string) ([]byte, error) {
		*runs++
		return nil, context.DeadlineExceeded
	}

	srv.sweepIfDue(context.Background())
	if *runs != 1 {
		t.Fatalf("%d sweeps, want one", *runs)
	}
	srv.sweepIfDue(context.Background())
	if *runs != 1 {
		t.Errorf("%d sweeps, want one: a failure is an attempt, and the cadence counts from it", *runs)
	}
}

// Off is a real cadence: the mailbox is then read only when someone asks, which
// is what a reader who wants to decide for themselves sets. Nothing sweeps, and
// nothing claims a next sweep.
func TestACadenceOfOffNeverSweeps(t *testing.T) {
	srv, runs := sweepable(t, slurpEveryOff)
	swept(t, srv, 11*time.Minute) // due by the clock, and still not swept

	srv.sweepIfDue(context.Background())
	if *runs != 0 {
		t.Errorf("%d sweeps with the cadence off, want none", *runs)
	}
	if got := srv.nextSlurpAt(); got != "" {
		t.Errorf("nextSlurpAt = %q with the cadence off, want nothing scheduled", got)
	}
	if got := srv.slurpEveryWord(); got != slurpEveryOff {
		t.Errorf("served cadence = %q, want %q", got, slurpEveryOff)
	}
}

// One ingest at a time. The button and the schedule write the same tables from
// the same mailbox, so a press that lands while a sweep is running is answered
// with that fact (409) rather than starting a second walk of the same query.
func TestASecondIngestWhileOneIsRunningIsRefused(t *testing.T) {
	srv, _ := sweepable(t, "")
	srv.runSlurp = func(context.Context, string) ([]byte, error) {
		return []byte("slurp\n"), nil
	}
	srv.sweeping.Store(true)
	t.Cleanup(func() { srv.sweeping.Store(false) })

	res := srv.do(t, "POST", "/v1/slurp", nil)
	if res.status != 409 {
		t.Fatalf("status = %d, want 409 while an ingest is running: %s", res.status, res.body)
	}
	if got := res.errText(t); !strings.Contains(got, "already running") {
		t.Errorf("error = %q, want it to say the mailbox is being ingested right now", got)
	}

	// And the loop drops its tick rather than queueing it: the running ingest has
	// already stamped its start, so the tick after it decides on its own.
	srv.sweepIfDue(context.Background())
}

// The button is the schedule's other caller, so a press counts as the ingest the
// cadence measures from — otherwise the next tick would start a second walk
// seconds after the first finished.
func TestAManualIngestCountsAsTheSweepTheCadenceMeasuresFrom(t *testing.T) {
	srv, runs := sweepable(t, "")
	swept(t, srv, 11*time.Minute)
	srv.runSlurp = func(context.Context, string) ([]byte, error) {
		*runs++
		return []byte("slurp\nmail: 0 new\n"), nil
	}

	res := srv.do(t, "POST", "/v1/slurp", nil)
	if res.status != 200 {
		t.Fatalf("status = %d: %s", res.status, res.body)
	}
	if *runs != 1 {
		t.Fatalf("%d ingests, want the one that was asked for", *runs)
	}
	srv.sweepIfDue(context.Background())
	if *runs != 1 {
		t.Errorf("%d ingests, want one: the press reset the cadence", *runs)
	}
}

// The cadence on the wire: always served (a host sweeps at some cadence whether
// or not anyone chose one), written in the one spelling the page offers, and
// cleared back to the default by an empty value the way the folder is.
func TestSlurpEveryIsServedWrittenAndCleared(t *testing.T) {
	srv, api := testServer(t), loadAPI(t)

	res := srv.do(t, "GET", "/v1/settings", nil)
	if res.status != 200 {
		t.Fatalf("status = %d: %s", res.status, res.body)
	}
	api.assert(t, "SettingsResponse", res.body)
	if got := readSettings(t, srv).SlurpEvery; got != "10m" {
		t.Errorf("served cadence = %q before anything was chosen, want the default in its canonical spelling", got)
	}

	for _, c := range []struct{ sent, want string }{
		{sent: "30m", want: "30m"},
		{sent: "1h", want: "1h"},
		// Two spellings of one cadence, stored and served as one: the page's
		// control can only render the vocabulary the server hands back.
		{sent: "600s", want: "10m"},
		{sent: "2h30m", want: "2h30m"}, {sent: " never ", want: slurpEveryOff},
	} {
		res := srv.do(t, "POST", "/v1/settings", []byte(`{"slurpEvery":`+strconv.Quote(c.sent)+`}`))
		if res.status != 200 {
			t.Fatalf("setting %q: status = %d: %s", c.sent, res.status, res.body)
		}
		api.assert(t, "SettingsResponse", res.body)
		if got := readSettings(t, srv).SlurpEvery; got != c.want {
			t.Errorf("setting %q served %q, want %q", c.sent, got, c.want)
		}
	}

	// An empty value clears the choice, and the default is in force again —
	// "not chosen" and "chosen as ten minutes" are one state on this host.
	if res := srv.do(t, "POST", "/v1/settings", []byte(`{"slurpEvery":""}`)); res.status != 200 {
		t.Fatalf("clearing: status = %d: %s", res.status, res.body)
	}
	if got := readSettings(t, srv).SlurpEvery; got != "10m" {
		t.Errorf("after clearing, cadence = %q, want the default", got)
	}
	if _, ok, err := srv.store.Setting(corpus.SettingSlurpEvery); err != nil || ok {
		t.Errorf("clearing left a row behind (ok=%v, err=%v): no choice and a default are one state", ok, err)
	}
}

// A value the server will not honour is refused where the caller can see why: a
// cadence below the floor walks the whole mailbox more often than anyone reads
// it, one beyond the ceiling is a sweep that never happens, and neither is
// something to store and quietly default.
func TestACadenceTheServerWillNotHonourIsRefused(t *testing.T) {
	srv, api := testServer(t), loadAPI(t)
	for _, c := range []struct{ sent, want string }{
		{sent: "7s", want: "too short"},
		{sent: "48h", want: "never happens"},
		{sent: "banana", want: "not one"},
		{sent: "90s", want: "whole number of minutes"},
		{sent: "30s", want: "too short"},
		{sent: "1h30s", want: "whole number of minutes"},
	} {
		res := srv.do(t, "POST", "/v1/settings", []byte(`{"slurpEvery":`+strconv.Quote(c.sent)+`}`))
		if res.status != 400 {
			t.Errorf("setting %q: status = %d, want 400: %s", c.sent, res.status, res.body)
			continue
		}
		api.assert(t, "Error", res.body)
		if got := res.errText(t); !strings.Contains(got, c.want) {
			t.Errorf("setting %q refused with %q, want it to say %q", c.sent, got, c.want)
		}
	}
	// A refused value is not a stored one, and the cadence in force is untouched.
	if got := readSettings(t, srv).SlurpEvery; got != "10m" {
		t.Errorf("a refused cadence left the server at %q", got)
	}
}

// Every word the parser accepts, and what each becomes. The canonical spelling
// is load-bearing rather than cosmetic: it is what the page's control is handed
// back and has to find among its own options.
func TestCadencesParseIntoOneSpelling(t *testing.T) {
	for _, c := range []struct {
		word      string
		want      time.Duration
		canonical string
	}{
		{word: "10m", want: 10 * time.Minute, canonical: "10m"},
		{word: " 5m ", want: 5 * time.Minute, canonical: "5m"},
		{word: "1h", want: time.Hour, canonical: "1h"},
		{word: "600s", want: 10 * time.Minute, canonical: "10m"},
		{word: "90m", want: 90 * time.Minute, canonical: "1h30m"},
		{word: "2h30m", want: 150 * time.Minute, canonical: "2h30m"},
		{word: "off", want: 0, canonical: slurpEveryOff},
		{word: "NEVER", want: 0, canonical: slurpEveryOff},
		{word: "24h", want: 24 * time.Hour, canonical: "24h"},
	} {
		got, canonical, err := parseSlurpEvery(c.word)
		if err != nil {
			t.Errorf("%q: %v", c.word, err)
			continue
		}
		if got != c.want || canonical != c.canonical {
			t.Errorf("%q = %s (%q), want %s (%q)", c.word, got, canonical, c.want, c.canonical)
		}
	}
}

// The next sweep is the last one plus the cadence, computed per request: a
// corpus that was ingested eight minutes ago is due in two, however long this
// process has been up.
func TestTheNextSweepIsTheLastOnePlusTheCadence(t *testing.T) {
	srv, _ := sweepable(t, "10m")
	api := loadAPI(t)
	last := time.Date(2026, 9, 16, 22, 0, 0, 0, time.UTC)
	if err := srv.store.PutSetting(corpus.SettingSlurpAt, last.Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}

	res := srv.do(t, "GET", "/v1/status", nil)
	if res.status != 200 {
		t.Fatalf("status = %d: %s", res.status, res.body)
	}
	api.assert(t, "StatusResponse", res.body)
	want := last.Add(10 * time.Minute).Format(time.RFC3339)
	if got := decode[statusResponse](t, res).NextSlurpAt; got != want {
		t.Errorf("nextSlurpAt = %q, want %q (the last sweep plus the cadence)", got, want)
	}
}

// A corpus that has never been swept has nothing to count from, and the one base
// that makes a fresh host predictable is its own start: due one cadence after it
// came up, rather than the moment it boots. The loop and the served answer are
// held to the same rule, so this is asserted on both.
func TestAFreshCorpusIsDueOneCadenceAfterTheServerStarted(t *testing.T) {
	srv, runs := sweepable(t, "")
	// The fixture's start is a fixed clock a day behind, which is a server that has
	// been up for ages rather than one that has just come up; the case under test
	// is the latter, so the base is moved to now.
	srv.startedAt = time.Now()
	want := srv.startedAt.Add(DefaultSlurpEvery).UTC().Format(time.RFC3339)

	if got := srv.nextSlurpAt(); got != want {
		t.Errorf("nextSlurpAt = %q on a corpus that has never been swept, want %q", got, want)
	}

	srv.sweepIfDue(context.Background())
	if *runs != 0 {
		t.Errorf("%d sweeps on a corpus that has never been swept, want none before the cadence", *runs)
	}
}

// A host without the grant has no schedule to serve: -slurp is what lets this
// process reach the mailbox at all, so a page must not be told a sweep is coming
// on a server that cannot make one.
func TestAHostWithoutTheSlurpGrantHasNoNextSweep(t *testing.T) {
	srv := testServer(t) // the fixture's posture: no -slurp, no scheduler
	if got := srv.nextSlurpAt(); got != "" {
		t.Errorf("nextSlurpAt = %q on a server without -slurp, want nothing scheduled", got)
	}
}
