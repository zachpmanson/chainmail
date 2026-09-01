package main

// The phases the operating sequence is made of, one function each.
//
// Both callers reach the same function: the dispatch cases in main.go run one
// phase by hand, and `slurp` runs them in order. Two implementations of a phase
// would mean two reports of it, and the report is what an operator reads a run
// through — so the printing lives here with the work rather than in either
// caller.

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/zachpmanson/chainmail/internal/corpus"
	"github.com/zachpmanson/chainmail/internal/embed"
	"github.com/zachpmanson/chainmail/internal/mailingest"
	"github.com/zachpmanson/chainmail/internal/slackingest"
)

// defaultEmbedTimeout bounds one batch, not the run. Shared with `slurp`, which
// takes no timeout flag of its own: a batch that has stalled for five minutes
// against a local model has stalled, and the run is resumable anyway.
const defaultEmbedTimeout = "5m"

type embedOpts struct {
	model, url, timeout string
	dim, batch, limit   int
	prune               bool
}

// runEmbed backfills the vectors semantic search needs.
//
// Long-running and interruptible on purpose: this is minutes of work against a
// local model, and ^C has to be a pause rather than a setback.
func runEmbed(path string, o embedOpts) error {
	wait, err := time.ParseDuration(o.timeout)
	if err != nil {
		return fmt.Errorf("-timeout %q: %w", o.timeout, err)
	}
	s, err := corpus.Open(path)
	if err != nil {
		return err
	}
	defer s.Close()

	e := &embed.Ollama{BaseURL: o.url, Name: o.model, Dimension: o.dim,
		Client: &http.Client{Timeout: wait}}

	// Checked up front. The alternative is discovering a missing model on the
	// first batch, which is the same failure reported later and with a
	// partially-written table to explain.
	ready, err := e.Available(context.Background())
	if err != nil {
		return err
	}
	if !ready {
		return fmt.Errorf("%s is running but does not have %s: run `ollama pull %s`",
			o.url, o.model, o.model)
	}

	// ^C stops between batches, so the work already committed stays and the
	// run resumes where it left off.
	ctx, stop := signal.NotifyContext(context.Background(),
		os.Interrupt, syscall.SIGTERM)
	defer stop()

	started := time.Now()
	var progressed bool
	rep, err := s.BackfillEmbeddings(ctx, e, corpus.BackfillOptions{
		Batch: o.batch, Limit: o.limit,
		Progress: func(p corpus.BackfillProgress) {
			progressed = true
			// Rate and remaining, not a percentage: what a reader wants after
			// two minutes of this is whether to wait for it.
			elapsed := time.Since(started)
			var eta time.Duration
			if p.Done > 0 {
				eta = (elapsed / time.Duration(p.Done)) * time.Duration(p.Pending-p.Done)
			}
			fmt.Printf("\r%d/%d  %d embedded  %d without a topic  %s elapsed, ~%s left   ",
				p.Done, p.Pending, p.Embedded, p.Skipped,
				elapsed.Round(time.Second), eta.Round(time.Second))
		},
	})
	if progressed {
		fmt.Println()
	}
	if err != nil {
		if errors.Is(err, context.Canceled) {
			fmt.Printf("stopped: %d embedded, %d without a topic, %d still to do — "+
				"re-run to continue where this left off\n",
				rep.Embedded, rep.Skipped, rep.Pending-rep.Embedded-rep.Skipped)
			return nil
		}
		return err
	}
	fmt.Printf("%s at %dd: %d embedded, %d recorded as having no topic worth embedding\n",
		rep.Model, rep.Dim, rep.Embedded, rep.Skipped)
	if rep.Pending == 0 {
		fmt.Println("everything was already current")
	}
	if o.prune {
		n, err := s.PruneEmbeddings(rep.Model)
		if err != nil {
			return err
		}
		fmt.Printf("pruned %d %s from other models\n",
			n, plural(int(n), "vector", "vectors"))
	}
	return nil
}

// embedDaemon reports whether the daemon is up and holds the model, so a caller
// that has other work to do can decide before spending the setup on this one.
// The reason is returned as text because the two ways this answers no — nothing
// listening, or listening without the model — want different commands from the
// reader.
func embedDaemon(o embedOpts) (bool, string) {
	wait, err := time.ParseDuration(o.timeout)
	if err != nil {
		return false, err.Error()
	}
	e := &embed.Ollama{BaseURL: o.url, Name: o.model, Dimension: o.dim,
		Client: &http.Client{Timeout: wait}}
	ready, err := e.Available(context.Background())
	switch {
	case err != nil:
		return false, fmt.Sprintf("no embedding daemon answering at %s: "+
			"OLLAMA_KEEP_ALIVE=-1 ollama serve &", o.url)
	case !ready:
		return false, fmt.Sprintf("%s is running but does not have %s: "+
			"run `ollama pull %s`", o.url, o.model, o.model)
	}
	return true, ""
}

// runReindex rebuilds the search indexes from the entries table. This is the
// sanctioned wipe path for the external-content fts5 shadow tables — a raw
// DELETE corrupts them (snippet() throws malformed) while count/match still
// work, so the store always clears+rebuilds via the virtual table commands.
func runReindex(path string) error {
	s, err := corpus.Open(path)
	if err != nil {
		return err
	}
	defer s.Close()
	if err := s.ReindexFTS(); err != nil {
		return err
	}
	var n int
	if err := s.DB().QueryRow("select count(*) from entries_fts").Scan(&n); err != nil {
		return err
	}
	fmt.Printf("rebuilt the search indexes — %d entries indexed\n", n)
	return nil
}

// runRepair reduces the identities one mailbox split into.
func runRepair(path string) error {
	s, err := corpus.Open(path)
	if err != nil {
		return err
	}
	defer s.Close()
	r, err := corpus.RepairMailtoIdentities(s)
	if err != nil {
		return err
	}
	fmt.Printf("repaired %d %s, renamed %d %s, merged %d %s\n",
		r.Rewritten, plural(r.Rewritten, "identity", "identities"),
		r.Renamed, plural(r.Renamed, "person", "people"),
		r.Merged, plural(r.Merged, "person", "people"))
	// Reported rather than resolved: a value naming two addresses cannot be
	// reduced without guessing which human it belongs to.
	for _, v := range r.Ambiguous {
		fmt.Printf("  left alone, names more than one address: %s\n", v)
	}
	if n := len(r.Ambiguous); n > 0 {
		fmt.Printf("\n%d ambiguous %s — decide by hand, then `corpus merge`\n",
			n, plural(n, "value", "values"))
	}

	// One mailbox written with RFC 5233 tags splits into a person per tag, which
	// is the same kind of damage from a different cause, so it is repaired in the
	// same pass rather than behind a flag of its own: every one of these passes
	// is deterministic and refuses rather than guesses, and an operator who runs
	// half of them keeps a corpus that is still split. `dedupe` is the one that
	// weighs evidence, which is why that one waits for -apply.
	pr, err := corpus.RepairPlusAddresses(s)
	if err != nil {
		return err
	}
	fmt.Printf("recorded %d base %s, renamed %d %s, merged %d tagged %s\n",
		pr.Anchored, plural(pr.Anchored, "mailbox", "mailboxes"),
		pr.Renamed, plural(pr.Renamed, "person", "people"),
		pr.Merged, plural(pr.Merged, "person", "people"))
	for _, l := range pr.Left {
		fmt.Printf("  left alone, %s: %s\n", l.Reason, l.Value)
	}

	// The same fold that doubled those addresses also cut some of them off
	// entirely, so the two repairs run together: the mailto pass first, because
	// the people it reunifies are the targets this one matches names against,
	// and the tagged pass before both, so those targets are one person each.
	tr, err := corpus.RepairTruncatedNames(s)
	if err != nil {
		return err
	}
	fmt.Printf("cleaned %d truncated %s, split %d welded %s, renamed %d %s, merged %d %s\n",
		tr.Cleaned, plural(tr.Cleaned, "name", "names"),
		tr.Welded, plural(tr.Welded, "address", "addresses"),
		tr.Renamed, plural(tr.Renamed, "person", "people"),
		tr.Merged, plural(tr.Merged, "person", "people"))
	for _, d := range tr.Declined {
		fmt.Printf("  not merged, %s: %s\n", d.Reason, d.Value)
	}
	if n := len(tr.Declined); n > 0 {
		fmt.Printf("\n%d truncated %s left for review — `corpus candidates`\n",
			n, plural(n, "name", "names"))
	}

	// The reply graph is repaired last, because it is the damage the rest of
	// the phase can have made: twins-absorb adopts and derived re-parents each
	// close an edge that was honest alone, and a ring turns every walk that
	// enters it into an empty answer — a chain that exists reads as one that
	// does not. Repaired here rather than behind a flag of its own for the same
	// reason the identity repairs are: it is deterministic, refuses rather than
	// guesses, and an operator who runs half of the phase keeps a graph that is
	// still wrong. A healthy graph reports zero; that is the pass being cheap.
	gr, err := s.RepairGraph()
	if err != nil {
		return fmt.Errorf("repairing the reply graph: %w", err)
	}
	if gr.Edges > 0 {
		fmt.Printf("repair-graph: severed %d ring %s\n",
			gr.Edges, plural(gr.Edges, "edge", "edges"))
		for _, e := range gr.Severed {
			fmt.Printf("  %s -> %s (%s)\n", e.ChildExt, e.ParentExt, e.Why)
		}
	} else {
		fmt.Println("repair-graph: no rings in the reply graph")
	}
	return nil
}

// runTwins collapses the messages stored twice.
func runTwins(path string, apply, declined bool) error {
	s, err := corpus.Open(path)
	if err != nil {
		return err
	}
	defer s.Close()
	plan, err := corpus.CollapseTwins(s, apply)
	if err != nil {
		return err
	}
	printTwins(plan, declined)
	return nil
}

// runDedupe merges the duplicate identities three rules can prove, and only when
// apply says to. Every caller that is not a human at a prompt passes false: the
// merges weigh evidence and cannot be undone, since person_merges records that a
// merge happened and not how to reverse it.
func runDedupe(path string, apply bool) error {
	s, err := corpus.Open(path)
	if err != nil {
		return err
	}
	defer s.Close()
	plan, err := corpus.Dedupe(s, apply)
	if err != nil {
		return err
	}
	printDedupe(plan)
	return nil
}

type mailOpts struct {
	query string
	ids   []string
	bound mailingest.Bound
	bin   string // docket binary/shim; "" uses "docket" on PATH
	// twins ends the walk with the same sweep the slurp pipeline gives its own
	// mail phase: a late mailbox copy alongside a quote already recovered from
	// it is one message stored twice, so collapsing it now is part of ingesting
	// it, not a separate chore. Slurp leaves this false — its next phase is the
	// twins phase anyway, and running the sweep twice would be a waste, not a
	// mistake (the second pass finds planes with nothing on them).
	twins bool
}

// runIngestMail walks one query, or reads the ids it is given, and says how far
// it got. The Result is returned as well as reported: a caller running this as
// one phase of several has to carry an incomplete walk to the end of its own
// summary, where an operator reading a timer's log will see it.
func runIngestMail(path string, o mailOpts) (mailingest.Result, error) {
	var r mailingest.Result
	c := mailingest.Client{Bin: o.bin}
	ok, err := c.SupportsThreadingHeaders()
	if err != nil {
		return r, fmt.Errorf("checking docket: %w", err)
	}
	if !ok {
		return r, fmt.Errorf("the docket on PATH does not expose threading headers " +
			"(Message-ID/In-Reply-To/References) — the corpus would have no reply " +
			"graph; update docket first")
	}

	s, err := corpus.Open(path)
	if err != nil {
		return r, err
	}
	defer s.Close()

	if len(o.ids) > 0 {
		r, err = mailingest.IngestIDs(s, c, o.ids)
	} else {
		r, err = mailingest.Ingest(s, c, o.query, o.bound)
	}
	if err != nil {
		return r, err
	}
	fmt.Printf("saw %d over %d page(s), created %d, changed %d, resolved %d parent edges\n",
		r.Seen, r.Pages, r.Created, r.Changed, r.Resolved)
	switch r.Stop {
	case mailingest.StopExhausted:
		fmt.Println("complete: docket had no further page")
	case mailingest.StopFrontier:
		fmt.Println("complete: reached the frontier an earlier walk left, " +
			"so the mail below it is already in")
	default:
		// The point of the cursor: an incomplete walk says so, on stderr,
		// with the command that finishes it. A count alone reads as coverage.
		fmt.Fprintf(os.Stderr,
			"INCOMPLETE: stopped at the -limit of %d with more matching mail unread; "+
				"re-run the same -q to continue from the cursor\n", o.bound.Max)
	}
	if r.Resumed {
		fmt.Println("resumed from a cursor left by an earlier run")
	}
	if r.Truncated > 0 {
		fmt.Fprintf(os.Stderr,
			"warning: %d bodies came back truncated — quoted history was lost\n", r.Truncated)
	}
	if o.twins {
		plan, err := corpus.CollapseTwins(s, true)
		if err != nil {
			return r, fmt.Errorf("after the walk, collapsing twins: %w", err)
		}
		if len(plan.Collapse) > 0 {
			fmt.Printf("twins: collapsed %d %s into %d, measuring %d render %s; "+
				"%d onto the mailbox copy, %d recovered-only\n",
				plan.Removed+len(plan.Collapse),
				plural(plan.Removed+len(plan.Collapse), "copy", "copies"),
				len(plan.Collapse), plan.Measured,
				plural(plan.Measured, "offset", "offsets"),
				plan.WithMailbox, plan.QuotedOnly)
		} else {
			fmt.Println("twins: nothing to collapse")
		}
	}
	return r, nil
}

// runIngestSlack slurps a slackdump archive. The archive is a local file, so this
// is safe to re-run at will — no Slack API call is made anywhere in the path.
func runIngestSlack(path, archive string) error {
	a, err := slackingest.OpenArchive(archive)
	if err != nil {
		return err
	}
	defer a.Close()

	s, err := corpus.Open(path)
	if err != nil {
		return err
	}
	defer s.Close()

	r, err := slackingest.Ingest(s, a)
	if err != nil {
		return err
	}
	fmt.Printf("saw %d in %d channels, created %d, changed %d, skipped %d, "+
		"resolved %d thread edges\n",
		r.Seen, r.Channels, r.Created, r.Changed, r.Skipped, r.Resolved)
	fmt.Printf("users %d (%d bots), authors %d\n", r.Users, r.Bots, r.Authors)
	// Not a warning: an account with no profile email is normal, and the number
	// is the honest limit on how much of the Slack half joins up with mail.
	fmt.Printf("no email: %d of %d users, %d of %d authors — keyed on slack uid, "+
		"so they will not merge with a mail identity until aliased by hand\n",
		r.UsersWithoutEmail, r.Users, r.AuthorsWithoutEmail, r.Authors)
	if r.Unauthored > 0 {
		fmt.Printf("unauthored  %d messages named no author at all\n", r.Unauthored)
	}
	if r.IdentityConflicts > 0 {
		fmt.Fprintf(os.Stderr,
			"warning: %d slack uids already belong to a different person than their "+
				"email resolves to — review with `corpus candidates`\n", r.IdentityConflicts)
	}
	return nil
}
