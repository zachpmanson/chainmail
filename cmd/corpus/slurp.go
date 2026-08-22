package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/zachpmanson/chainmail/internal/mailingest"
)

// slurp is the operating sequence: everything that has to happen, in the order
// it has to happen in, so a machine holding the binaries and no checkout can run
// it. It used to live in a Makefile, which does not ship in the package.
//
// slack, mail, twins, repair, dedupe (reported, never applied), embed:
//
//   - twins before repair, because twins removes duplicate rows before repair
//     reads the identities on them.
//   - repair before dedupe, because repair settles identities before anything
//     weighs evidence about them.
//   - embed last, because it embeds whatever the earlier phases left behind, and
//     it is the only phase that needs a daemon this process does not start.
//
// Out of order is not destructive. Every phase is idempotent and refuses rather
// than guesses, so the cost of a bad order is work left for the following run —
// the ordering is about how much one run finishes, not about safety.
type slurpPhase string

const (
	phaseSlack  slurpPhase = "slack"
	phaseMail   slurpPhase = "mail"
	phaseTwins  slurpPhase = "twins"
	phaseRepair slurpPhase = "repair"
	phaseDedupe slurpPhase = "dedupe"
	phaseEmbed  slurpPhase = "embed"
)

// slurpOrder is the sequence, written down once. A phase is added here and
// nowhere else; -only and -skip choose a subset of it and never reorder it,
// because the order is the correctness argument above and not a preference.
var slurpOrder = []slurpPhase{
	phaseSlack, phaseMail, phaseTwins, phaseRepair, phaseDedupe, phaseEmbed,
}

// phaseGroups name a run of phases that operators think of as one thing, so
// -skip settle does not have to be spelled as three phases whose relationship a
// reader would then have to reconstruct.
var phaseGroups = map[string][]slurpPhase{
	"settle": {phaseTwins, phaseRepair, phaseDedupe},
}

type slurpOpts struct {
	query, since  string
	limit, pageSz int
	archive       string
	bin           string // docket binary/shim for the mail phase
	slackdump     bool
	only, skip    []string
	embedURL      string
	embedModel    string
	embedDim      int
}

// outcome is how a phase ended. Four, not two: a phase that could not run for
// want of something this machine does not have is a different fact from a phase
// that ran and broke, and an operator reading a timer's log has to be able to
// tell them apart without knowing which phases their host is set up for.
type outcome string

const (
	outcomeDone       outcome = "complete"
	outcomeIncomplete outcome = "INCOMPLETE"
	outcomeSkipped    outcome = "skipped"
	outcomeFailed     outcome = "FAILED"
)

type phaseResult struct {
	Phase   slurpPhase
	Outcome outcome
	Note    string
}

// errSlackdumpMissing distinguishes "no such program" from a program that ran
// and failed. The first is a host that never had slackdump; the second is a
// refresh that broke, and only one of them is worth an operator's attention.
var errSlackdumpMissing = errors.New("slackdump is not on PATH")

// slurpDeps is the seam every phase is reached through. Each field is the phase
// function a subcommand already calls, so slurp adds sequencing and reporting
// and no second implementation of any phase — and a test can drive the sequence
// without a mailbox, a Slack archive or an embedding daemon.
type slurpDeps struct {
	refreshSlack func(dir string) error
	ingestSlack  func(archive string) error
	ingestMail   func(o mailOpts) (mailingest.Result, error)
	twins        func(apply bool) error
	repair       func() error
	dedupe       func(apply bool) error
	// embedReady answers before the work starts, and says what is missing when
	// the answer is no.
	embedReady func() (bool, string)
	embed      func() error
}

func defaultSlurpDeps(path string, o slurpOpts) slurpDeps {
	eo := embedOpts{model: o.embedModel, url: o.embedURL, timeout: defaultEmbedTimeout,
		dim: o.embedDim}
	return slurpDeps{
		refreshSlack: refreshSlackArchive,
		ingestSlack:  func(archive string) error { return runIngestSlack(path, archive) },
		ingestMail:   func(m mailOpts) (mailingest.Result, error) { return runIngestMail(path, m) },
		twins:        func(apply bool) error { return runTwins(path, apply, false) },
		repair:       func() error { return runRepair(path) },
		dedupe:       func(apply bool) error { return runDedupe(path, apply) },
		embedReady:   func() (bool, string) { return embedDaemon(eo) },
		embed:        func() error { return runEmbed(path, eo) },
	}
}

// refreshSlackArchive brings the local archive up to date. `resume` continues
// from slackdump's own checkpoint, so this is incremental rather than a re-dump.
func refreshSlackArchive(dir string) error {
	bin, err := exec.LookPath("slackdump")
	if err != nil {
		return errSlackdumpMissing
	}
	cmd := exec.Command(bin, "resume", dir)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	return cmd.Run()
}

// selectPhases resolves -only and -skip against the order, keeping the order.
func selectPhases(only, skip []string) ([]slurpPhase, error) {
	if len(only) > 0 && len(skip) > 0 {
		return nil, errors.New("-only and -skip name the same decision twice: pass one")
	}
	expand := func(names []string) (map[slurpPhase]bool, error) {
		set := map[slurpPhase]bool{}
		for _, n := range names {
			n = strings.TrimSpace(n)
			if g, ok := phaseGroups[n]; ok {
				for _, p := range g {
					set[p] = true
				}
				continue
			}
			p := slurpPhase(n)
			if !hasPhase(slurpOrder, p) {
				return nil, fmt.Errorf("no phase %q: want %s or %s", n,
					strings.Join(phaseNames(), ", "), strings.Join(groupNames(), ", "))
			}
			set[p] = true
		}
		return set, nil
	}
	keep, err := expand(only)
	if err != nil {
		return nil, err
	}
	drop, err := expand(skip)
	if err != nil {
		return nil, err
	}
	var sel []slurpPhase
	for _, p := range slurpOrder {
		if len(keep) > 0 && !keep[p] {
			continue
		}
		if drop[p] {
			continue
		}
		sel = append(sel, p)
	}
	if len(sel) == 0 {
		return nil, errors.New("every phase was excluded, so there is nothing to do")
	}
	return sel, nil
}

func hasPhase(ps []slurpPhase, want slurpPhase) bool {
	for _, p := range ps {
		if p == want {
			return true
		}
	}
	return false
}

func phaseNames() []string {
	out := make([]string, 0, len(slurpOrder))
	for _, p := range slurpOrder {
		out = append(out, string(p))
	}
	return out
}

func groupNames() []string {
	out := make([]string, 0, len(phaseGroups))
	for g := range phaseGroups {
		out = append(out, g)
	}
	return out
}

// mailQuery is the one thing slurp builds rather than passes through: -since is
// a date because that is how an operator thinks about a top-up, and Gmail wants
// it as after:YYYY/MM/DD.
func mailQuery(o slurpOpts) (string, error) {
	if o.query != "" {
		return o.query, nil
	}
	if o.since == "" {
		return "", errors.New("the mail phase needs -q <query> or -since YYYY-MM-DD " +
			"(or -skip mail)")
	}
	return "after:" + strings.ReplaceAll(o.since, "-", "/"), nil
}

// runSlurp runs the selected phases in order and reports each, then all of them.
//
// A phase that fails does not stop the run: the phases are independent enough
// that a broken mailbox does not make settling the identities already in the
// corpus any less worth doing, and a run that stopped at the first failure would
// leave an operator to discover the rest one re-run at a time. The failures are
// carried to the summary and to the exit status.
func runSlurp(w io.Writer, o slurpOpts, d slurpDeps) error {
	sel, err := selectPhases(o.only, o.skip)
	if err != nil {
		return err
	}
	// Resolved before anything runs: a usage mistake should not be discovered
	// after the Slack phase has already written to the corpus.
	var query string
	if hasPhase(sel, phaseMail) {
		if query, err = mailQuery(o); err != nil {
			return err
		}
	}

	var results []phaseResult
	report := func(p slurpPhase, oc outcome, note string) {
		results = append(results, phaseResult{Phase: p, Outcome: oc, Note: note})
	}

	for i, p := range sel {
		fmt.Fprintf(w, "\n[%d/%d] %s\n", i+1, len(sel), p)
		switch p {
		case phaseSlack:
			oc, note := slurpSlack(w, o, d)
			report(p, oc, note)

		case phaseMail:
			r, err := d.ingestMail(mailOpts{query: query, bin: o.bin,
				bound: mailingest.Bound{Max: o.limit, PageSize: o.pageSz}})
			switch {
			case err != nil:
				// Including a docket that cannot report threading headers, which
				// fails closed inside the phase: a corpus with no reply graph is
				// worse than no ingest at all, and slurp does not soften that.
				report(p, outcomeFailed, err.Error())
			case r.Stop.Covered():
				report(p, outcomeDone, fmt.Sprintf("%s: created %d, changed %d",
					query, r.Created, r.Changed))
			default:
				report(p, outcomeIncomplete, fmt.Sprintf(
					"stopped at the -limit of %d; re-run the same query to continue "+
						"from the cursor", o.limit))
			}

		case phaseTwins:
			if err := d.twins(true); err != nil {
				report(p, outcomeFailed, err.Error())
				continue
			}
			report(p, outcomeDone, "duplicate copies collapsed")

		case phaseRepair:
			if err := d.repair(); err != nil {
				report(p, outcomeFailed, err.Error())
				continue
			}
			report(p, outcomeDone, "identities reduced")

		case phaseDedupe:
			// A DRY RUN, always. dedupe's merges weigh evidence and CANNOT be
			// undone — person_merges records that a merge happened, not how to
			// reverse it — so nothing unattended may apply them. slurp passes
			// apply=false and has no flag that would change it: a -dedupe-apply
			// flag would put an irreversible merge one typo away from a cron line.
			fmt.Fprintln(w, "a dry run: review it, then `corpus dedupe -apply`")
			if err := d.dedupe(false); err != nil {
				report(p, outcomeFailed, err.Error())
				continue
			}
			report(p, outcomeDone, "reported only; apply it by hand")

		case phaseEmbed:
			// A skip, not a failure: vectors are a derived index, the backfill is
			// resumable, and the next run picks up exactly what this one left. A
			// non-zero exit here would train an operator to ignore the exit code
			// of a nightly run whose ollama is simply not always up.
			if ok, why := d.embedReady(); !ok {
				fmt.Fprintln(w, why)
				report(p, outcomeSkipped, why)
				continue
			}
			if err := d.embed(); err != nil {
				report(p, outcomeFailed, err.Error())
				continue
			}
			report(p, outcomeDone, "vectors current")
		}
	}
	return summarise(w, results)
}

// slurpSlack refreshes the archive if it can, then ingests it.
func slurpSlack(w io.Writer, o slurpOpts, d slurpDeps) (outcome, string) {
	var warn string
	if o.slackdump {
		switch err := d.refreshSlack(filepath.Dir(o.archive)); {
		case errors.Is(err, errSlackdumpMissing):
			// Not a failure. A host that ingests an archive somebody else dumped
			// is a normal setup, and the archive on disk is still worth reading.
			fmt.Fprintln(w, "slackdump is not on PATH: ingesting the archive as it stands")
		case err != nil:
			// A refresh that broke — expired credentials, most often. Reported as
			// a failure because a silently stale archive looks exactly like a
			// quiet week, but the ingest still runs: what is on disk is real.
			warn = fmt.Sprintf("slackdump resume failed (%v); ingested the archive as it stood", err)
			fmt.Fprintln(w, warn)
		}
	}
	if _, err := os.Stat(o.archive); err != nil {
		// Skipped, not failed. Slack is one source of two, and a mail-only corpus
		// is a supported setup rather than a broken one.
		return outcomeSkipped, fmt.Sprintf("no slackdump archive at %s — "+
			"pass -archive, or -skip slack", o.archive)
	}
	if err := d.ingestSlack(o.archive); err != nil {
		return outcomeFailed, err.Error()
	}
	if warn != "" {
		return outcomeFailed, warn
	}
	return outcomeDone, "archive ingested"
}

// summarise prints the tail an operator actually reads.
//
// A timer's log is read from the end, so every outcome is restated here rather
// than left where it happened, mid-scroll and several phases up. The counts come
// last so the shape of the run survives being read in a notification.
func summarise(w io.Writer, results []phaseResult) error {
	fmt.Fprintln(w, "\nslurp")
	var failed, skipped, incomplete []string
	for _, r := range results {
		fmt.Fprintf(w, "  %-7s %-10s %s\n", r.Phase, r.Outcome, r.Note)
		switch r.Outcome {
		case outcomeFailed:
			failed = append(failed, string(r.Phase))
		case outcomeSkipped:
			skipped = append(skipped, string(r.Phase))
		case outcomeIncomplete:
			incomplete = append(incomplete, string(r.Phase))
		}
	}
	fmt.Fprintf(w, "%d %s: %d complete, %d incomplete, %d skipped, %d failed\n",
		len(results), plural(len(results), "phase", "phases"),
		len(results)-len(failed)-len(skipped)-len(incomplete),
		len(incomplete), len(skipped), len(failed))
	if len(incomplete) > 0 {
		fmt.Fprintf(w, "INCOMPLETE: %s left work unread — re-run to continue from the cursor\n",
			strings.Join(incomplete, ", "))
	}
	if len(skipped) > 0 {
		fmt.Fprintf(w, "skipped for want of a prerequisite: %s\n", strings.Join(skipped, ", "))
	}
	if len(failed) > 0 {
		// The only non-zero exit. A skip is a decision about this host and an
		// incomplete walk is a bound the operator set; neither is a fault, and a
		// systemd unit that goes red for either teaches everyone to ignore red.
		return fmt.Errorf("%s failed", strings.Join(failed, ", "))
	}
	return nil
}
