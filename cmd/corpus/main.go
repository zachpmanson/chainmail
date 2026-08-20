// Command corpus builds and inspects the local mail/Slack corpus.
//
//	corpus init                       create or migrate the database
//	corpus ingest mail -q <query>     slurp a Gmail query
//	corpus stats                      what is in it, and what is missing
//	corpus people                     everyone involved, senders and recipients
//	corpus merge -keep <a> -drop <b>  same human, two addresses
//	corpus alias -from <d> -to <d>    a rebrand: fold one domain into another
//	corpus candidates                 pairs that may be one human, for review
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/zachpmanson/chainmail/internal/corpus"
	"github.com/zachpmanson/chainmail/internal/mailingest"
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

func run(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: corpus <init|ingest|stats> [flags]")
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
		return nil

	case "ingest":
		// The source is a positional subcommand, so it must be consumed before
		// flag parsing: flag.Parse stops at the first non-flag argument.
		if len(args) < 2 || args[1] != "mail" {
			return fmt.Errorf("usage: corpus ingest mail -q <query> [-limit n]")
		}
		fs := flag.NewFlagSet("ingest mail", flag.ContinueOnError)
		query := fs.String("q", "", "Gmail search query")
		limit := fs.Int("limit", 50, "maximum messages to walk")
		ids := fs.String("id", "", "comma-separated message ids, instead of a query")
		if err := fs.Parse(args[2:]); err != nil {
			return err
		}
		if *query == "" && *ids == "" {
			return fmt.Errorf("ingest needs -q <query> or -id <id,...>")
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

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
