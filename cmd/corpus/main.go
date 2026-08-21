// Command corpus builds and inspects the local mail/Slack corpus.
//
//	corpus init                       create or migrate the database
//	corpus ingest mail -q <query>     slurp a Gmail query
//	corpus ingest slack               slurp a slackdump archive
//	corpus stats                      what is in it, and what is missing
//	corpus people                     everyone involved, senders and recipients
//	corpus merge -keep <a> -drop <b>  same human, two addresses
//	corpus alias -from <d> -to <d>    a rebrand: fold one domain into another
//	corpus candidates                 pairs that may be one human, for review
//	corpus repair                     undo what a folded header did to identities
//	corpus search -q <text>           which chains are about this
//	corpus embed                      fill in the vectors semantic search needs
//	corpus spec -q <text> -o f.json   a timeline spec for those chains
//	corpus unnest <ext-id>            what extraction recovers from one message
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/zachpmanson/chainmail/internal/corpus"
	"github.com/zachpmanson/chainmail/internal/embed"
	"github.com/zachpmanson/chainmail/internal/mailingest"
	"github.com/zachpmanson/chainmail/internal/slackingest"
	"github.com/zachpmanson/chainmail/internal/spec"
	"github.com/zachpmanson/chainmail/internal/unnest"
)

func defaultPath() string {
	if p := os.Getenv("CHAINMAIL_CORPUS"); p != "" {
		return p
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "state", "chainmail", "corpus.db")
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "corpus:", err)
		os.Exit(1)
	}
}

// usage lists every subcommand the dispatch below accepts. Kept adjacent to it
// on purpose: a subcommand that exists but is not listed here is unreachable for
// anyone who did not read the source, which is how seven of these spent a while.
const usage = `usage: corpus <command> [flags]

  init                     create or migrate the corpus
  ingest mail   -q <query> ingest Gmail results, with their quoted history
  ingest slack  [-archive] ingest a slackdump archive
  search        -q <text>  ranked chains across every source; -mode chooses
                           lexical, semantic or hybrid retrieval, -topk how deep
                           the vector ranking votes, -dim -model -url -timeout
                           the model it asks
  embed         [-model -url -dim -batch -limit -timeout -prune]
                           embed the entries that have no current vector, so
                           semantic search has something to search; resumable,
                           so ^C is a pause
  show          <ext-id>   one entry in full, or -chain for the whole thread
  spec          -q <text>  write a timeline spec for the renderer
  unnest        <ext-id>   show the blocks one body contains, from the corpus;
                           -id <gmail-id> reads one from the mailbox instead;
                           -full prints each block whole; -chrono orders them by
                           the date each states and names what cannot be true
  stats                    counts, coverage and what is missing
  people                   everyone in the corpus, with their identities
  candidates               probable duplicate identities, unmerged
  merge         -keep -drop  fold one identity into another
  alias         [-from -to]  list or add a domain alias
  repair                   reduce addresses stored with a mailto: link, clean
                           display names cut off at a bracket, and fold the
                           people that split apart
`

// ingestUsage names the flags each source takes. "[flags]" told a reader only
// that flags exist, which is the same as telling them nothing.
const ingestUsage = `usage: corpus ingest <mail|slack> [flags]

  ingest mail   -q <gmail query> | -id <id,...>   [-limit N]
  ingest slack  [-archive <path to slackdump.sqlite>]
`

func run(args []string) error {
	if len(args) == 0 {
		return errors.New(usage)
	}
	switch args[0] {
	case "-h", "--help", "help":
		fmt.Print(usage)
		return nil
	}
	path := defaultPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}

	switch args[0] {
	case "init":
		s, err := corpus.Open(path)
		if err != nil {
			return err
		}
		defer s.Close()
		fmt.Println("corpus ready at", path)
		return nil

	case "people":
		s, err := corpus.Open(path)
		if err != nil {
			return err
		}
		defer s.Close()
		ps, err := corpus.People(s)
		if err != nil {
			return err
		}
		for _, p := range ps {
			ids := strings.Join(p.Identities, ", ")
			fmt.Printf("%4d  %-28s sent %3d  recv %3d  %s\n",
				p.PersonID, trunc(p.DisplayName, 28), p.Sent, p.Received, ids)
		}
		fmt.Printf("\n%d people\n", len(ps))
		return nil

	case "search":
		fs := flag.NewFlagSet("search", flag.ContinueOnError)
		q := fs.String("q", "", "text query")
		since := fs.String("since", "", "only entries on or after YYYY-MM-DD")
		person := fs.String("person", "", "involving this address, name or slack uid")
		limit := fs.Int("limit", 10, "chains to return")
		entries := fs.Bool("entries", false, "list matching entries instead of chains")
		mode := fs.String("mode", modeLexical,
			"retrieval: lexical | semantic | hybrid (the last two need `corpus embed`)")
		topk := fs.Int("topk", 0, "how deep the vector ranking votes (default 100)")
		model := fs.String("model", embed.DefaultModel, "embedding model, for -mode semantic|hybrid")
		dim := fs.Int("dim", embed.DefaultDim, "dimensions that model returns")
		url := fs.String("url", embed.DefaultBaseURL, "ollama endpoint")
		timeout := fs.String("timeout", "2m", "how long to wait for the model")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		s, err := corpus.Open(path)
		if err != nil {
			return err
		}
		defer s.Close()
		// An empty -q is meaningful alongside -person or -since ("everything
		// involving X"), but with no filters at all it silently returned an
		// arbitrary slice of the whole corpus, which reads like a ranked answer.
		if *q == "" && *person == "" && *since == "" {
			return errors.New("usage: corpus search -q <text> [-person X] [-since YYYY-MM-DD]\n" +
				"       [-limit N] [-entries] [-mode lexical|semantic|hybrid] [-topk N]\n" +
				"       [-model M] [-dim N] [-url U] [-timeout D]\n" +
				"       (-q may be empty only when -person or -since narrows it)")
		}
		query, err := buildQuery(*q, *since, *person, *limit)
		if err != nil {
			return err
		}
		query.Semantic, err = semanticFor(*mode, *q, *model, *url, *timeout, *dim, *topk)
		if err != nil {
			return err
		}
		if *entries {
			hits, err := s.SearchEntries(query)
			if err != nil {
				return err
			}
			for _, h := range hits {
				fmt.Printf("%-28s %s%s\n    %s\n", h.ExtID,
					h.TS.Format("2006-01-02 15:04"), why(h), oneLine(h.Snippet, 100))
			}
			fmt.Printf("\n%d entries\n", len(hits))
			return nil
		}
		chains, err := s.SearchChains(query)
		if err != nil {
			return err
		}
		for _, c := range chains {
			fmt.Printf("%-46s  %2d/%-3d matched  %s -> %s\n",
				trunc(orElse(c.Subject, c.RootExtID), 46), c.Matched, c.Entries,
				c.First.Format("2006-01-02"), c.Last.Format("2006-01-02"))
			fmt.Printf("    root %s\n", c.RootExtID)
		}
		fmt.Printf("\n%d chains\n", len(chains))
		return nil

	case "embed":
		// Backfill. Long-running and interruptible on purpose: this is minutes of
		// work against a local model, and ^C has to be a pause rather than a
		// setback.
		fs := flag.NewFlagSet("embed", flag.ContinueOnError)
		model := fs.String("model", embed.DefaultModel, "embedding model to use")
		url := fs.String("url", embed.DefaultBaseURL, "ollama endpoint")
		dim := fs.Int("dim", embed.DefaultDim, "dimensions the model returns")
		batch := fs.Int("batch", 0, "entries per request and per commit (default 32)")
		limit := fs.Int("limit", 0, "stop after this many entries")
		timeout := fs.String("timeout", "5m", "how long to wait for one batch")
		prune := fs.Bool("prune", false,
			"after the run, drop vectors from every other model")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		wait, err := time.ParseDuration(*timeout)
		if err != nil {
			return fmt.Errorf("-timeout %q: %w", *timeout, err)
		}
		s, err := corpus.Open(path)
		if err != nil {
			return err
		}
		defer s.Close()

		e := &embed.Ollama{BaseURL: *url, Name: *model, Dimension: *dim,
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
				*url, *model, *model)
		}

		// ^C stops between batches, so the work already committed stays and the
		// run resumes where it left off.
		ctx, stop := signal.NotifyContext(context.Background(),
			os.Interrupt, syscall.SIGTERM)
		defer stop()

		started := time.Now()
		var progressed bool
		rep, err := s.BackfillEmbeddings(ctx, e, corpus.BackfillOptions{
			Batch: *batch, Limit: *limit,
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
		if *prune {
			n, err := s.PruneEmbeddings(rep.Model)
			if err != nil {
				return err
			}
			fmt.Printf("pruned %d %s from other models\n",
				n, plural(int(n), "vector", "vectors"))
		}
		return nil

	case "spec":
		fs := flag.NewFlagSet("spec", flag.ContinueOnError)
		q := fs.String("q", "", "text query; the matching chains become the spec")
		since := fs.String("since", "", "only entries on or after YYYY-MM-DD")
		person := fs.String("person", "", "involving this address, name or slack uid")
		limit := fs.Int("limit", 10, "chains to include")
		out := fs.String("o", "", "write the spec here (default stdout)")
		title := fs.String("title", "", "page title")
		me := fs.String("me", "", "comma-separated addresses that are yours")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *q == "" {
			return errors.New("usage: corpus spec -q <query> [-person X] [-since YYYY-MM-DD]\n" +
				"                  [-limit N] [-o spec.json] [-title T] [-me <address>]")
		}
		s, err := corpus.Open(path)
		if err != nil {
			return err
		}
		defer s.Close()
		query, err := buildQuery(*q, *since, *person, *limit)
		if err != nil {
			return err
		}
		chains, err := s.SearchChains(query)
		if err != nil {
			return err
		}
		if len(chains) == 0 {
			return fmt.Errorf("no chains matched %q", *q)
		}
		// Select by chain root, not by matching entry: a trail is the whole
		// conversation, not only the messages that happened to contain the words.
		var roots []string
		for _, c := range chains {
			roots = append(roots, c.RootExtID)
		}
		sp, err := spec.Generate(s, spec.Options{
			ExtIDs:   roots,
			Title:    *title,
			Queries:  []spec.Query{{Q: *q, Note: "corpus search"}},
			Me:       splitList(*me),
			RunLabel: time.Now().Format("2 Jan 2006"),
		})
		if err != nil {
			return err
		}
		blob, err := json.MarshalIndent(sp, "", " ")
		if err != nil {
			return err
		}
		if *out == "" {
			fmt.Println(string(blob))
			return nil
		}
		if err := os.WriteFile(*out, append(blob, '\n'), 0o600); err != nil {
			return err
		}
		fmt.Printf("wrote %s — %d entries from %d chains\n", *out, len(sp.Messages), len(chains))
		return nil

	case "unnest":
		// Which blocks a body contains, and where it splits. The id is the one
		// search prints, and positional for the same reason `show` is: pasting a
		// result straight back in is the whole point of the command.
		fs := flag.NewFlagSet("unnest", flag.ContinueOnError)
		id := fs.String("id", "", "the entry to peel; also accepted positionally")
		full := fs.Bool("full", false, "print each block's text in full")
		chrono := fs.Bool("chrono", false, "order blocks by the date each states, and name the orderings that cannot be true")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		target := *id
		if target == "" {
			target = fs.Arg(0)
		}
		if target == "" {
			return errors.New("usage: corpus unnest <ext-id> [-full] [-chrono]\n" +
				"       ids are the ones search prints, e.g. mail:<...>, slack:C1:1.2, quote:<sha>;\n" +
				"       -id also takes a raw Gmail id, read live, for a message not yet ingested")
		}
		s, err := corpus.Open(path)
		if err != nil {
			return err
		}
		defer s.Close()
		return runUnnest(os.Stdout, unnestSource{
			show: s.Show,
			read: mailingest.Client{}.Read,
		}, target, unnestOpts{Full: *full, Chrono: *chrono})

	case "alias":
		fs := flag.NewFlagSet("alias", flag.ContinueOnError)
		from := fs.String("from", "", "old domain, e.g. old.example")
		to := fs.String("to", "", "current domain, e.g. new.example")
		note := fs.String("note", "", "why")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		s, err := corpus.Open(path)
		if err != nil {
			return err
		}
		defer s.Close()
		if *from == "" && *to == "" {
			existing, err := corpus.DomainAliases(s)
			if err != nil {
				return err
			}
			if len(existing) == 0 {
				fmt.Println("no domain aliases configured")
			}
			for f, t := range existing {
				fmt.Printf("%s -> %s\n", f, t)
			}
			return nil
		}
		if *from == "" || *to == "" {
			return fmt.Errorf("usage: corpus alias -from <old.domain> -to <new.domain> [-note why]")
		}
		merged, err := corpus.AddDomainAlias(s, *from, *to, *note)
		if err != nil {
			return err
		}
		fmt.Printf("alias %s -> %s recorded; merged %d already-split %s\n",
			*from, *to, merged, plural(merged, "person", "people"))
		return nil

	case "repair":
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

		// The same fold that doubled those addresses also cut some of them off
		// entirely, so the two repairs run together: the mailto pass first, because
		// the people it reunifies are the targets this one matches names against.
		tr, err := corpus.RepairTruncatedNames(s)
		if err != nil {
			return err
		}
		fmt.Printf("cleaned %d truncated %s, renamed %d %s, merged %d %s\n",
			tr.Cleaned, plural(tr.Cleaned, "name", "names"),
			tr.Renamed, plural(tr.Renamed, "person", "people"),
			tr.Merged, plural(tr.Merged, "person", "people"))
		for _, d := range tr.Declined {
			fmt.Printf("  not merged, %s: %s\n", d.Reason, d.Value)
		}
		if n := len(tr.Declined); n > 0 {
			fmt.Printf("\n%d truncated %s left for review — `corpus candidates`\n",
				n, plural(n, "name", "names"))
		}
		return nil

	case "candidates":
		s, err := corpus.Open(path)
		if err != nil {
			return err
		}
		defer s.Close()
		cs, err := corpus.MergeCandidates(s)
		if err != nil {
			return err
		}
		for _, c := range cs {
			fmt.Printf("%4d %-24s  ~  %4d %-24s  (%s)\n     %v  |  %v\n",
				c.AID, trunc(c.AName, 24), c.BID, trunc(c.BName, 24), c.Reason,
				c.AAddresses, c.BAddresses)
		}
		fmt.Printf("\n%d candidate %s — review, then `corpus merge -keep <a> -drop <b>`\n",
			len(cs), plural(len(cs), "pair", "pairs"))
		return nil

	case "merge":
		fs := flag.NewFlagSet("merge", flag.ContinueOnError)
		keep := fs.String("keep", "", "email address of the person to keep")
		drop := fs.String("drop", "", "email address of the person to merge away")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *keep == "" || *drop == "" {
			return fmt.Errorf("usage: corpus merge -keep <email> -drop <email>")
		}
		s, err := corpus.Open(path)
		if err != nil {
			return err
		}
		defer s.Close()
		id, err := corpus.MergeByEmail(s, *keep, *drop)
		if err != nil {
			return err
		}
		fmt.Printf("merged into person %d\n", id)
		return nil

	case "show":
		fs := flag.NewFlagSet("show", flag.ContinueOnError)
		chain := fs.Bool("chain", false, "show every entry in the thread, in time order")
		full := fs.Bool("full", false, "print bodies whole rather than clipped")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		// The id is positional: it is what search prints, and requiring a flag to
		// paste it back in is friction for the one thing this command is for.
		id := fs.Arg(0)
		if id == "" {
			return errors.New("usage: corpus show <ext-id> [-chain] [-full]\n" +
				"       ids are the ones search prints, e.g. mail:<...>, slack:C1:1.2, quote:<sha>")
		}
		s, err := corpus.Open(path)
		if err != nil {
			return err
		}
		defer s.Close()
		if *chain {
			items, err := s.Chain(id)
			if err != nil {
				return err
			}
			for i, it := range items {
				if i > 0 {
					fmt.Println()
				}
				printShown(it, *full)
			}
			fmt.Printf("\n%d entries in the thread\n", len(items))
			return nil
		}
		it, err := s.Show(id)
		if err != nil {
			return err
		}
		printShown(it, *full)
		return nil

	case "stats":
		s, err := corpus.Open(path)
		if err != nil {
			return err
		}
		defer s.Close()
		st, err := s.Stats()
		if err != nil {
			return err
		}
		fmt.Printf("entries      %d\n", st.Entries)
		for k, v := range st.BySource {
			fmt.Printf("  %-10s %d\n", k, v)
		}
		fmt.Printf("people       %d\n", st.People)
		fmt.Printf("chain roots  %d\n", st.Roots)
		fmt.Printf("unresolved   %d  (parent named but not present: known holes)\n", st.Unresolved)
		es, err := s.EmbedStats()
		if err != nil {
			return err
		}
		if len(es) == 0 {
			fmt.Println("embeddings   none  (`corpus embed` to enable semantic search)")
		}
		// Per model, because a half-finished model migration is a real state and
		// looks from the results alone like a corpus that lost recall.
		for _, m := range es {
			fmt.Printf("embeddings   %s at %dd: %d vectors, %d skipped as having no topic",
				m.Model, m.Dim, m.Vectors, m.Skipped)
			if m.Eligible > 0 {
				fmt.Printf(", %d to do", m.Eligible)
			}
			if m.Stale > 0 {
				fmt.Printf(" (%d stale)", m.Stale)
			}
			fmt.Println()
		}
		return nil

	case "ingest":
		// The source is a positional subcommand, so it must be consumed before
		// flag parsing: flag.Parse stops at the first non-flag argument.
		if len(args) < 2 {
			return errors.New(ingestUsage)
		}
		if args[1] == "slack" {
			return ingestSlack(path, args[2:])
		}
		if args[1] != "mail" {
			return errors.New(ingestUsage)
		}
		fs := flag.NewFlagSet("ingest mail", flag.ContinueOnError)
		query := fs.String("q", "", "Gmail search query")
		limit := fs.Int("limit", 50, "maximum messages to walk")
		ids := fs.String("id", "", "comma-separated message ids, instead of a query")
		if err := fs.Parse(args[2:]); err != nil {
			return err
		}
		if *query == "" && *ids == "" {
			return errors.New("usage: corpus ingest mail -q <gmail query> | -id <id,...>  [-limit N]")
		}

		c := mailingest.Client{}
		ok, err := c.SupportsThreadingHeaders()
		if err != nil {
			return fmt.Errorf("checking docket: %w", err)
		}
		if !ok {
			return fmt.Errorf("the docket on PATH does not expose threading headers " +
				"(Message-ID/In-Reply-To/References) — the corpus would have no reply " +
				"graph; update docket first")
		}

		s, err := corpus.Open(path)
		if err != nil {
			return err
		}
		defer s.Close()

		var r mailingest.Result
		if *ids != "" {
			r, err = mailingest.IngestIDs(s, c, strings.Split(*ids, ","))
		} else {
			r, err = mailingest.Ingest(s, c, *query, *limit)
		}
		if err != nil {
			return err
		}
		fmt.Printf("saw %d, created %d, changed %d, resolved %d parent edges\n",
			r.Seen, r.Created, r.Changed, r.Resolved)
		if r.Truncated > 0 {
			fmt.Fprintf(os.Stderr,
				"warning: %d bodies came back truncated — quoted history was lost\n", r.Truncated)
		}
		return nil
	}
	return fmt.Errorf("unknown command %q", args[0])
}

// ingestSlack slurps a slackdump archive. The archive is a local file, so this
// is safe to re-run at will — no Slack API call is made anywhere in the path.
func ingestSlack(path string, args []string) error {
	fs := flag.NewFlagSet("ingest slack", flag.ContinueOnError)
	archive := fs.String("archive", defaultSlackArchive(),
		"slackdump sqlite archive to read")
	if err := fs.Parse(args); err != nil {
		return err
	}

	a, err := slackingest.OpenArchive(*archive)
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

func defaultSlackArchive() string {
	if p := os.Getenv("CHAINMAIL_SLACK_ARCHIVE"); p != "" {
		return p
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "state", "chainmail", "slack", "slackdump.sqlite")
}

// buildQuery turns CLI flags into a corpus query.
func buildQuery(text, since, person string, limit int) (corpus.Query, error) {
	q := corpus.Query{Text: text, Limit: limit}
	if since != "" {
		t, err := time.Parse("2006-01-02", since)
		if err != nil {
			return q, fmt.Errorf("-since %q: want YYYY-MM-DD", since)
		}
		q.Since = t
	}
	if person != "" {
		// Involving, not People: a cc-only participant is invisible to an
		// author-only filter, and they are often the point.
		q.Involving = []string{person}
	}
	return q, nil
}

// Retrieval modes for `search -mode`.
const (
	modeLexical  = "lexical"
	modeSemantic = "semantic"
	modeHybrid   = "hybrid"
)

// semanticFor embeds the query text, or returns nil for a lexical search.
//
// The embedding happens here, at the point the user typed the query, rather than
// inside search. A down daemon then surfaces as "ollama is not running" against
// the command that needed it, instead of as an empty result set from a function
// whose entire job is to return few results.
func semanticFor(mode, text, model, url, timeout string, dim, topK int) (*corpus.SemanticQuery, error) {
	switch mode {
	case modeLexical, "":
		return nil, nil
	case modeSemantic, modeHybrid:
	default:
		return nil, fmt.Errorf("-mode %q: want lexical, semantic or hybrid", mode)
	}
	if strings.TrimSpace(text) == "" {
		return nil, fmt.Errorf("-mode %s needs -q: there is nothing to be similar to", mode)
	}
	wait, err := time.ParseDuration(timeout)
	if err != nil {
		return nil, fmt.Errorf("-timeout %q: %w", timeout, err)
	}
	// The expected width is stated rather than inferred, so a model that is not
	// the one whose vectors are stored fails here with a dimension mismatch
	// instead of matching no rows and reading as an empty corpus.
	e := &embed.Ollama{BaseURL: url, Name: model, Dimension: dim,
		Client: &http.Client{Timeout: wait}}
	ctx, cancel := context.WithTimeout(context.Background(), wait)
	defer cancel()
	vs, err := e.Embed(ctx, []string{text})
	if err != nil {
		return nil, fmt.Errorf("embedding the query: %w", err)
	}
	return &corpus.SemanticQuery{
		Vector: vs[0], Model: model, TopK: topK, Only: mode == modeSemantic,
	}, nil
}

// why names the rankings that found a hit, so a result with no visible keyword
// in it is explicable rather than mysterious.
func why(h corpus.EntryHit) string {
	var parts []string
	if h.ProseRank > 0 {
		parts = append(parts, fmt.Sprintf("prose %d", h.ProseRank))
	}
	if h.IdentRank > 0 {
		parts = append(parts, fmt.Sprintf("id %d", h.IdentRank))
	}
	if h.SemRank > 0 {
		parts = append(parts, fmt.Sprintf("similar %d @ %.2f", h.SemRank, h.Similarity))
	}
	if len(parts) == 0 {
		return ""
	}
	return "  [" + strings.Join(parts, ", ") + "]"
}

func splitList(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

func oneLine(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	return trunc(s, n)
}

func orElse(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func kindName(k unnest.Kind) string {
	switch k {
	case unnest.KindAttribution:
		return "attribution"
	case unnest.KindHeaderBlock:
		return "header block"
	case unnest.KindForwardRule:
		return "forward rule"
	default:
		return "visible message"
	}
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// trunc clips s to at most n runes, spending one of them on the ellipsis that
// marks the cut rather than adding it past the budget.
//
// Runes, not bytes. A byte-bounded cut can land inside a multi-byte rune, and a
// single invalid fragment makes the whole stream undecodable to whatever is
// reading it — these are columns in a CLI, but the CLI is also an API. A byte
// budget is the wrong width unit besides: fmt's own %-28s pads by runes, so
// counting bytes gives accented and CJK text a third of the column Latin text
// gets and the columns stop lining up.
//
// Runes, and not grapheme clusters. A cut inside a flag or a skin-toned emoji
// renders the base character instead of the composed one: valid UTF-8, one
// wrong glyph, in the final cell of a line that already says it is clipped.
// Buying that back means a golang.org/x/text dependency and its Unicode tables,
// which is a poor trade for a corpus whose multi-byte content is overwhelmingly
// apostrophes and dashes. The one artefact worth stdlib-only handling is a
// trailing joiner, which would otherwise sit between the text and the ellipsis
// modifying nothing.
func trunc(s string, n int) string {
	if n <= 0 {
		return ""
	}
	rs := []rune(s)
	if len(rs) <= n {
		return s
	}
	return string(trimTrailingJoiners(rs[:n-1])) + "…"
}

// trimTrailingJoiners drops zero-width joiners and variation selectors from the
// end of a clipped run. They only ever modify the rune that follows them, and
// what followed them is what got cut.
func trimTrailingJoiners(rs []rune) []rune {
	for len(rs) > 0 {
		r := rs[len(rs)-1]
		if r != '\u200d' && !(r >= '\ufe00' && r <= '\ufe0f') {
			break
		}
		rs = rs[:len(rs)-1]
	}
	return rs
}

// printShown renders one entry for reading.
//
// Bodies are clipped by default: a chain of thirty forwarded messages is
// thousands of lines, and the point of the default view is to see the shape of a
// conversation. -full is there for when the text itself is the question.
func printShown(e corpus.Shown, full bool) {
	kind := "message"
	if e.Quoted {
		kind = "recovered from quoted text"
	}
	fmt.Printf("%s  [%s, %s]\n", e.ExtID, e.Source, kind)
	when := e.TS.Format("Mon 2 Jan 2006 15:04")
	if e.TZ != "" {
		when += " " + e.TZ
	}
	fmt.Printf("  when    %s\n", when)
	if e.Author != "" {
		fmt.Printf("  from    %s\n", e.Author)
	}
	if e.Subject != "" {
		fmt.Printf("  subject %s\n", e.Subject)
	}
	if e.Container != "" {
		fmt.Printf("  in      %s\n", e.Container)
	}
	if e.Parent != "" {
		fmt.Printf("  replies %s\n", e.Parent)
	} else if e.ParentRef != "" {
		// A named parent that is not present is a known hole, not a root.
		fmt.Printf("  replies %s  (not in the corpus)\n", e.ParentRef)
	}
	for _, g := range e.Sightings {
		line := "  seen    " + g.Kind
		if g.SeenIn != "" {
			line += " in " + g.SeenIn
		}
		if g.Detail != "" {
			line += "  (" + g.Detail + ")"
		}
		fmt.Println(line)
	}
	if len(e.Participants) > 0 {
		var to, cc []string
		for _, p := range e.Participants {
			switch p.Role {
			case corpus.RoleTo:
				to = append(to, p.DisplayName)
			case corpus.RoleCc:
				cc = append(cc, p.DisplayName)
			}
		}
		if len(to) > 0 {
			fmt.Printf("  to      %s\n", strings.Join(to, ", "))
		}
		if len(cc) > 0 {
			fmt.Printf("  cc      %s\n", strings.Join(cc, ", "))
		}
	}
	if e.Permalink != "" {
		fmt.Printf("  link    %s\n", e.Permalink)
	}
	body := strings.TrimSpace(e.Body)
	if body == "" {
		return
	}
	if !full {
		if lines := strings.Split(body, "\n"); len(lines) > 12 {
			body = strings.Join(lines[:12], "\n") +
				fmt.Sprintf("\n... %d more lines (-full)", len(lines)-12)
		}
	}
	fmt.Println()
	for _, l := range strings.Split(body, "\n") {
		fmt.Println("  " + l)
	}
}
