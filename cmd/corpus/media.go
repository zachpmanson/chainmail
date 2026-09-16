package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"sort"

	"github.com/zachpmanson/chainmail/internal/corpus"
	"github.com/zachpmanson/chainmail/internal/gmailclient"
	"github.com/zachpmanson/chainmail/internal/media"
)

const mediaUsage = `usage: corpus media <pull|stats|prune> [flags]

  pull  [-entry <ext-id>] [-container <thread-id>] [-source mail|slack]
        [-limit N] [-max-bytes N] [-images] [-why]
                           fetch the attachment bytes a scope covers, so the
                           renderer can show them instead of sending the reader
                           to Gmail. Nothing is pulled by ` + "`slurp`" + `: this is the
                           deliberate pass, one message or one thread at a time.
                           -entry is one message (the chips you clicked),
                           -container is every message in a mail thread or Slack
                           channel, -source narrows to one backend, -limit bounds
                           a sweep, -max-bytes refuses a file larger than N
                           (default 10 MB; not a truncation), -images pulls only
                           pictures. -why reports what would be pulled and what
                           would be skipped, and writes NOTHING — no bytes, no
                           skip records — and is the half of this that needs no
                           mailbox. A scope is required except with -source
                           slack, whose bytes are already on disk.
  stats                    what is filed: blobs, bytes, and why the rest is not
  prune                    delete blobs nothing points at any more; reports what
                           went. The only destructive media operation, so it is
                           never part of anything unattended.`

func runMedia(path string, args []string) error {
	if len(args) == 0 {
		return errors.New(mediaUsage)
	}
	switch args[0] {
	case "pull":
		return runMediaPull(path, args[1:])
	case "stats":
		return runMediaStats(path)
	case "prune":
		return runMediaPrune(path)
	}
	return fmt.Errorf("unknown media command %q\n\n%s", args[0], mediaUsage)
}

func runMediaPull(path string, args []string) error {
	fs := flag.NewFlagSet("media pull", flag.ContinueOnError)
	entry := fs.String("entry", "", "one message's ext-id, e.g. mail:<...>")
	container := fs.String("container", "", "a mail thread id or Slack channel id")
	source := fs.String("source", "", "restrict to one backend: mail or slack")
	limit := fs.Int("limit", 0, "stop after N attachments; 0 is no bound")
	maxBytes := fs.Int64("max-bytes", media.DefaultMaxBytes,
		"refuse a file larger than this many bytes; 0 is no cap")
	images := fs.Bool("images", false, "pull only parts whose type says image")
	dry := fs.Bool("why", false, "report what would be pulled, and why not, without writing anything")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *maxBytes < 0 {
		return errors.New("-max-bytes must be 0 (no cap) or a positive byte count")
	}
	if *source != "" && *source != corpus.SourceMail && *source != corpus.SourceSlack {
		return fmt.Errorf("-source %q: want %q or %q", *source, corpus.SourceMail, corpus.SourceSlack)
	}
	if *entry == "" && *container == "" && *source != corpus.SourceSlack {
		return errors.New("usage: corpus media pull -entry <ext-id> | -container <thread-id>\n" +
			"       [-source mail|slack] [-limit N] [-max-bytes N] [-images] [-why]\n" +
			"       a whole-corpus sweep is deliberately not the default: pulling media\n" +
			"       is a per-conversation decision, and a mail part costs a Gmail round trip.\n" +
			"       -source slack is the exception — those bytes are already on disk, so a\n" +
			"       sweep of them spends nothing but disk")
	}

	s, err := corpus.Open(path)
	if err != nil {
		return err
	}
	defer s.Close()

	opts := media.Options{
		Store:      s,
		Uploads:    defaultUploadDir(),
		MaxBytes:   *maxBytes,
		OnlyImages: *images,
		DryRun:     *dry,
		Logf:       func(format string, args ...any) { fmt.Printf(format+"\n", args...) },
	}
	// A dry run never opens a transport: -why has to work on a host that is not
	// logged in, and it must not spend a Gmail call to answer.
	if !*dry {
		opts.Fetcher = media.Deferred(func() (media.Fetcher, error) {
			c, err := gmailclient.New()
			if err != nil {
				return nil, fmt.Errorf("opening gmail: %w", err)
			}
			return c, nil
		})
	}

	r, err := media.Pull(context.Background(), opts, corpus.MediaScope{
		Entry:     *entry,
		Container: *container,
		Source:    *source,
		Limit:     *limit,
	})
	if err != nil {
		return err
	}

	verb := "pulled"
	if *dry {
		verb = "would pull"
	}
	fmt.Printf("\n%s %d of %d attachments (%s), skipped %d, failed %d\n",
		verb, r.Pulled, r.Wanted, humanBytes(r.Bytes), r.Skipped, r.Failed)

	// The failures are listed again at the end, and only they are: everything else
	// was already on its own line above, and a pull's real output is "what did not
	// work". These are the ones a later run will retry, since nothing was recorded
	// for them.
	for _, it := range r.Items {
		if it.Err != nil {
			fmt.Printf("failed   %s (%s): %v\n", it.Name, it.ExtID, it.Err)
		}
	}
	if *dry && r.Pulled > 0 {
		fmt.Println("\nnothing was written — re-run without -why to pull")
	}
	return nil
}

func runMediaStats(path string) error {
	s, err := corpus.Open(path)
	if err != nil {
		return err
	}
	defer s.Close()

	t, err := s.MediaStats()
	if err != nil {
		return err
	}
	fmt.Printf("%d blobs, %s, linked to %d attachments\n", t.Blobs, humanBytes(t.Bytes), t.PulledRows)
	if t.SkippedRows == 0 {
		fmt.Println("nothing has been declined yet")
		return nil
	}
	// Reasons, not just a count: "twelve files too large" and "twelve parts that
	// no longer exist" call for opposite responses, and the number alone cannot
	// tell an operator which one they have.
	reasons := make([]string, 0, len(t.Skips))
	for reason := range t.Skips {
		reasons = append(reasons, reason)
	}
	sort.Strings(reasons)
	fmt.Printf("%d attachments have a reason instead of bytes:\n", t.SkippedRows)
	for _, reason := range reasons {
		fmt.Printf("  %-16s %d\t%s\n", reason, t.Skips[reason], skipExplanation(reason))
	}
	return nil
}

// skipExplanation says what each recorded reason means, because the words
// themselves are terse to the point of ambiguity: "unavailable" reads like "we
// could not reach it", and it means the opposite.
func skipExplanation(reason string) string {
	switch reason {
	case corpus.MediaSkipTooLarge:
		return "bigger than the cap it was pulled with"
	case corpus.MediaSkipUnavailable:
		return "the part exists and carries no content"
	case corpus.MediaSkipNoPart:
		return "the message no longer has that part; re-read the message"
	case corpus.MediaSkipNoSourceRef:
		return "no handle to fetch it by (recovered from quoted text)"
	case corpus.MediaSkipNoMessage:
		return "no Gmail message id"
	case corpus.MediaSkipNoBytes:
		return "the archive does not have it on disk"
	}
	return ""
}

func runMediaPrune(path string) error {
	s, err := corpus.Open(path)
	if err != nil {
		return err
	}
	defer s.Close()

	n, bytes, err := s.PruneMedia()
	if err != nil {
		return err
	}
	if n == 0 {
		fmt.Println("nothing to prune: every blob is still referenced")
		return nil
	}
	fmt.Printf("pruned %d unreferenced blobs, freeing %s\n", n, humanBytes(bytes))
	return nil
}

// humanBytes renders a byte count for the summary. Sizes here orient an operator
// — how much quota or disk a pull cost — so one decimal below ten units is as
// much precision as one of them is worth.
func humanBytes(n int64) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	units := []string{"KB", "MB", "GB", "TB"}
	v := float64(n) / 1024
	for i, u := range units {
		if v < 1024 || i == len(units)-1 {
			if v < 10 {
				return fmt.Sprintf("%.1f %s", v, u)
			}
			return fmt.Sprintf("%.0f %s", v, u)
		}
		v /= 1024
	}
	return fmt.Sprintf("%d B", n)
}
