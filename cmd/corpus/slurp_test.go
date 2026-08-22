package main

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/zachpmanson/chainmail/internal/mailingest"
)

// Everything here is invented: an archive that is an empty file, a query no
// mailbox is asked, a daemon nothing dials.

// recorder drives the sequence without a mailbox, a Slack archive or an
// embedding daemon. It records the order phases were called in, because the
// order is the property this command exists for — asserting only that each
// phase ran would pass on the one arrangement that leaves work behind.
type recorder struct {
	calls []string

	// dedupeApply is what slurp asked dedupe to do. Recorded rather than
	// assumed: an applied merge cannot be undone.
	dedupeApply []bool

	mail      mailingest.Result
	mailErr   error
	slackErr  error
	refresh   error
	embedWhy  string
	embedDown bool
	embedErr  error
	mailSeen  mailOpts
}

func (r *recorder) deps() slurpDeps {
	note := func(s string) { r.calls = append(r.calls, s) }
	return slurpDeps{
		refreshSlack: func(string) error { note("refresh"); return r.refresh },
		ingestSlack:  func(string) error { note("slack"); return r.slackErr },
		ingestMail: func(o mailOpts) (mailingest.Result, error) {
			note("mail")
			r.mailSeen = o
			return r.mail, r.mailErr
		},
		twins:  func(bool) error { note("twins"); return nil },
		repair: func() error { note("repair"); return nil },
		dedupe: func(apply bool) error {
			note("dedupe")
			r.dedupeApply = append(r.dedupeApply, apply)
			return nil
		},
		embedReady: func() (bool, string) {
			note("embed-ready")
			return !r.embedDown, r.embedWhy
		},
		embed: func() error { note("embed"); return r.embedErr },
	}
}

// archive writes an empty file where an archive would be, so the Slack phase
// gets past its existence check without a real slackdump database.
func archive(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "slackdump.sqlite")
	if err := os.WriteFile(p, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func opts(t *testing.T) slurpOpts {
	return slurpOpts{since: "2026-04-01", archive: archive(t), slackdump: true}
}

// covered is a walk that reached the end of its query.
var covered = mailingest.Result{Stop: mailingest.StopExhausted, Seen: 3}

func slurp(t *testing.T, o slurpOpts, r *recorder) (string, error) {
	t.Helper()
	var b strings.Builder
	err := runSlurp(&b, o, r.deps())
	return b.String(), err
}

func TestPhasesRunInTheOrderThatFinishesTheWork(t *testing.T) {
	r := &recorder{mail: covered}
	_, err := slurp(t, opts(t), r)
	if err != nil {
		t.Fatalf("runSlurp: %v", err)
	}
	want := []string{"refresh", "slack", "mail", "twins", "repair", "dedupe",
		"embed-ready", "embed"}
	if got := strings.Join(r.calls, ","); got != strings.Join(want, ",") {
		t.Errorf("phase order\n got %s\nwant %s", got, strings.Join(want, ","))
	}
}

// The assertion that matters. A merge weighs evidence and cannot be reversed —
// person_merges records that one happened, not how to undo it — so an unattended
// run may report the plan and never carry it out.
func TestDedupeIsReportedAndNeverApplied(t *testing.T) {
	r := &recorder{mail: covered}
	out, err := slurp(t, opts(t), r)
	if err != nil {
		t.Fatalf("runSlurp: %v", err)
	}
	if len(r.dedupeApply) != 1 {
		t.Fatalf("dedupe called %d times, want once", len(r.dedupeApply))
	}
	if r.dedupeApply[0] {
		t.Error("slurp applied dedupe")
	}
	if !strings.Contains(out, "dry run") {
		t.Errorf("the run does not say the plan is a dry run:\n%s", out)
	}
}

// And no flag can turn it on: the guarantee above is only as good as the one
// call site, so the call site is asserted as text too.
func TestNoCallSiteCanApplyDedupe(t *testing.T) {
	b, err := os.ReadFile("slurp.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range regexp.MustCompile(`d\.dedupe\(([^)]*)\)`).
		FindAllStringSubmatch(string(b), -1) {
		if m[1] != "false" {
			t.Errorf("dedupe is called with %q, and only false is safe unattended", m[1])
		}
	}
}

func TestMissingSlackArchiveIsASkipAndNotAFailure(t *testing.T) {
	o := opts(t)
	o.archive = filepath.Join(t.TempDir(), "absent", "slackdump.sqlite")
	r := &recorder{mail: covered}
	out, err := slurp(t, o, r)
	if err != nil {
		t.Fatalf("a missing archive failed the run: %v", err)
	}
	if strings.Contains(strings.Join(r.calls, ","), "slack") {
		t.Error("ingested an archive that is not there")
	}
	if !strings.Contains(out, o.archive) {
		t.Errorf("the skip does not name what is missing:\n%s", out)
	}
	if !strings.Contains(out, "slack   skipped") {
		t.Errorf("the summary does not report the skip:\n%s", out)
	}
}

// slackdump absent from PATH is not the same as slackdump broken: the first
// leaves the archive worth ingesting and the run whole, the second leaves it
// silently stale, which reads exactly like a quiet week.
func TestSlackdumpMissingIngestsAnyway(t *testing.T) {
	r := &recorder{mail: covered, refresh: errSlackdumpMissing}
	out, err := slurp(t, opts(t), r)
	if err != nil {
		t.Fatalf("runSlurp: %v", err)
	}
	if !strings.Contains(strings.Join(r.calls, ","), "slack") {
		t.Error("skipped an archive that is on disk")
	}
	if !strings.Contains(out, "not on PATH") {
		t.Errorf("the run does not say what is missing:\n%s", out)
	}
}

func TestSlackdumpFailingIsAFailureAndStillIngests(t *testing.T) {
	r := &recorder{mail: covered, refresh: errors.New("credentials expired")}
	out, err := slurp(t, opts(t), r)
	if err == nil {
		t.Error("a refresh that broke exited zero")
	}
	if !strings.Contains(strings.Join(r.calls, ","), "slack") {
		t.Error("did not ingest the archive it already had")
	}
	if !strings.Contains(out, "credentials expired") {
		t.Errorf("the summary does not say why:\n%s", out)
	}
}

// No daemon is a skip: vectors are a derived index and the backfill is
// resumable, so the next run embeds exactly what this one left.
func TestEmbedWithoutADaemonIsASkip(t *testing.T) {
	r := &recorder{mail: covered, embedDown: true,
		embedWhy: "no embedding daemon answering at http://127.0.0.1:1"}
	out, err := slurp(t, opts(t), r)
	if err != nil {
		t.Fatalf("a daemon that is down failed the run: %v", err)
	}
	if last := r.calls[len(r.calls)-1]; last != "embed-ready" {
		t.Errorf("embedded against a daemon that is not there: %v", r.calls)
	}
	if !strings.Contains(out, "embed   skipped") ||
		!strings.Contains(out, "no embedding daemon") {
		t.Errorf("the summary does not report the skip and its reason:\n%s", out)
	}
}

// The tail is what an operator reads out of a timer's log, so an incomplete walk
// has to be there and not only where it happened.
func TestIncompleteMailIsInTheSummary(t *testing.T) {
	r := &recorder{mail: mailingest.Result{Stop: mailingest.StopMax, Seen: 50}}
	o := opts(t)
	o.limit = 50
	out, err := slurp(t, o, r)
	if err != nil {
		t.Fatalf("a bound the operator set failed the run: %v", err)
	}
	tail := out[strings.LastIndex(out, "\nslurp\n"):]
	if !strings.Contains(tail, "INCOMPLETE") {
		t.Errorf("the summary does not say the run was incomplete:\n%s", tail)
	}
}

func TestAFailedPhaseIsNonZeroAndTheRestStillRun(t *testing.T) {
	r := &recorder{mailErr: errors.New("docket does not expose threading headers")}
	out, err := slurp(t, opts(t), r)
	if err == nil {
		t.Fatal("a broken phase exited zero")
	}
	if !strings.Contains(err.Error(), "mail") {
		t.Errorf("the error does not name the phase: %v", err)
	}
	if !strings.Contains(strings.Join(r.calls, ","), "twins,repair,dedupe") {
		t.Errorf("a failure stopped the phases that were still worth running: %v", r.calls)
	}
	if !strings.Contains(out, "threading headers") {
		t.Errorf("the summary does not carry the reason:\n%s", out)
	}
}

func TestOnlyAndSkipChoosePhases(t *testing.T) {
	for _, tc := range []struct {
		name       string
		only, skip []string
		want       string
	}{
		{name: "mail only", only: []string{"mail"}, want: "mail"},
		{name: "slack only", only: []string{"slack"}, want: "refresh,slack"},
		{name: "no embedding", skip: []string{"embed"},
			want: "refresh,slack,mail,twins,repair,dedupe"},
		{name: "settle is a group", only: []string{"settle"}, want: "twins,repair,dedupe"},
		{name: "skip the group", skip: []string{"settle", "embed"},
			want: "refresh,slack,mail"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &recorder{mail: covered}
			o := opts(t)
			o.only, o.skip = tc.only, tc.skip
			if _, err := slurp(t, o, r); err != nil {
				t.Fatalf("runSlurp: %v", err)
			}
			if got := strings.Join(r.calls, ","); got != tc.want {
				t.Errorf("phases run\n got %s\nwant %s", got, tc.want)
			}
		})
	}
}

func TestPhaseSelectionRefusesRatherThanGuesses(t *testing.T) {
	for _, tc := range []struct {
		name, want string
		only, skip []string
	}{
		{name: "both", only: []string{"mail"}, skip: []string{"embed"}, want: "one"},
		{name: "no such phase", only: []string{"settel"}, want: "no phase"},
		{name: "nothing left", skip: []string{"slack", "mail", "settle", "embed"},
			want: "nothing to do"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &recorder{mail: covered}
			o := opts(t)
			o.only, o.skip = tc.only, tc.skip
			_, err := slurp(t, o, r)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want one mentioning %q", err, tc.want)
			}
			if len(r.calls) != 0 {
				t.Errorf("a usage mistake wrote to the corpus anyway: %v", r.calls)
			}
		})
	}
}

// -since is a date because that is how a top-up is decided; Gmail wants slashes.
func TestSinceBecomesTheMailQuery(t *testing.T) {
	r := &recorder{mail: covered}
	o := opts(t)
	o.only = []string{"mail"}
	if _, err := slurp(t, o, r); err != nil {
		t.Fatalf("runSlurp: %v", err)
	}
	if r.mailSeen.query != "after:2026/04/01" {
		t.Errorf("query = %q", r.mailSeen.query)
	}
}

func TestMailWithNoQueryStopsBeforeAnythingRuns(t *testing.T) {
	r := &recorder{mail: covered}
	o := opts(t)
	o.since = ""
	_, err := slurp(t, o, r)
	if err == nil || !strings.Contains(err.Error(), "-since") {
		t.Fatalf("err = %v, want one naming the flags that would fix it", err)
	}
	if len(r.calls) != 0 {
		t.Errorf("the Slack phase ran before the mail query was known: %v", r.calls)
	}
}
